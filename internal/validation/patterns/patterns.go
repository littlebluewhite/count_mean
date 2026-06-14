// Package patterns provides centralized malicious pattern detection for security validation.
package patterns

import (
	"regexp"
	"strings"
	"sync"
)

// commandInjectionWordTokens 是 command-injection 短 token 的 word-boundary 比對組。
//
// 這些 token 過短/過 generic,做 substring 比對會把任何含子串 `id` / `sh` /
// `at ` / `su -` 的合法字串都當成 command injection 命中(實測 6/15 EMG cell 中招)。
// 改成 \btoken\b 強制 word boundary 後,只在「真的是獨立 token」的時候命中,合法
// cell（`david`、`Mid Foot`、`subject_id`、`Cash`、`shoulder`、`Subject ID`）才會放行。
//
// 仍保留 detection 能力:`id`、`sh`、`at`、`su -` 真正單獨出現時(如 `; id`、`& sh`、
// `| at now`、`& su - root`)會被 \b 視為 whole-word 命中而 reject。case-insensitive
// 與其他 detector 對齊（attacker 用 `ID`、`Sh` 變體也擋）。
//
//nolint:gochecknoglobals // compiled-once regex set for hot-path use
var commandInjectionWordTokens = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bid\b`),
	regexp.MustCompile(`(?i)\bsh\b`),
	regexp.MustCompile(`(?i)\bat\b`),
	regexp.MustCompile(`(?i)\bsu\b\s*-`),
	// medium-length unix command tokens 過去放 substring list,在
	// 「amounted」、「paramount」、「dispatched」、「killed」、「answered」、
	// 「chmoderator」等合法字串假陽性。改 word-boundary regex 仍可偵測
	// 真實 attack pattern(`; mount /dev/sda` / `& kill -9`),但不再吞合法
	// 子串。case-insensitive 與其他 detector 對齊。
	regexp.MustCompile(`(?i)\bpwd\b`),
	regexp.MustCompile(`(?i)\bkill\b`),
	regexp.MustCompile(`(?i)\bmount\b`),
	regexp.MustCompile(`(?i)\bchmod\b`),
	regexp.MustCompile(`(?i)\bbatch\b`),
	// shell / code-exec token,原在 DangerousFunctions 用 call-syntax(後接 `(`)比對,
	// 但 `; powershell script`、`run cmd here`、`use system now` 等 bare 命令不接 `(`
	// → 被漏判(call-syntax gate 引入的 P1 回歸)。這些 token bare 出現即為攻擊,改用
	// \bword\b 與其他命令 token 一致(case-insensitive)。`sh` 已在上方(\bsh\b);此處
	// 補 cmd/powershell/bash/exec/eval/spawn/shell/system。word-boundary 不誤命中
	// systemic/evaluation/execution/shoulder 等合法子串(無 \b)。
	regexp.MustCompile(`(?i)\bcmd\b`),
	regexp.MustCompile(`(?i)\bpowershell\b`),
	regexp.MustCompile(`(?i)\bbash\b`),
	regexp.MustCompile(`(?i)\bexec\b`),
	regexp.MustCompile(`(?i)\beval\b`),
	regexp.MustCompile(`(?i)\bspawn\b`),
	regexp.MustCompile(`(?i)\bshell\b`),
	regexp.MustCompile(`(?i)\bsystem\b`),
}

// isReservedNameIn reports whether name is a Windows reserved device name
// (case-insensitive) within the given registry's ReservedNames set.
func isReservedNameIn(reg *PatternRegistry, name string) bool {
	nameUpper := strings.ToUpper(name)
	for _, r := range reg.Get(ReservedNames) {
		if nameUpper == r {
			return true
		}
	}
	return false
}

// IsReservedName reports whether name is a Windows reserved device name
// (case-insensitive), consulting the default registry. Use this from callers
// without a detector instance (e.g. filename.Sanitize).
func IsReservedName(name string) bool {
	return isReservedNameIn(DefaultRegistry(), name)
}

// CommandInjectionWordTokens 回傳 word-boundary command-injection regex 切片副本。
// detector 用此切片做第二段比對(substring list 之外)。
func CommandInjectionWordTokens() []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(commandInjectionWordTokens))
	copy(out, commandInjectionWordTokens)

	return out
}

// PatternCategory represents different categories of patterns.
type PatternCategory string

const (
	// NumericMalicious patterns for detecting malicious numeric inputs.
	NumericMalicious PatternCategory = "numeric_malicious"

	// FormulaInjection patterns for CSV formula injection detection.
	FormulaInjection PatternCategory = "formula_injection"

	// DangerousFunctions patterns for dangerous spreadsheet functions.
	DangerousFunctions PatternCategory = "dangerous_functions"

	// ScriptInjection patterns for XSS and script injection.
	ScriptInjection PatternCategory = "script_injection"

	// SQLInjection patterns for SQL injection detection.
	SQLInjection PatternCategory = "sql_injection"

	// CommandInjection patterns for command injection detection.
	CommandInjection PatternCategory = "command_injection"

	// DangerousChars patterns for dangerous filename characters.
	DangerousChars PatternCategory = "dangerous_chars"

	// SuspiciousExtensions patterns for suspicious file extensions.
	SuspiciousExtensions PatternCategory = "suspicious_extensions"

	// ReservedNames patterns for Windows reserved filenames.
	ReservedNames PatternCategory = "reserved_names"
)

// PatternRegistry manages all validation patterns.
type PatternRegistry struct {
	mu       sync.RWMutex
	patterns map[PatternCategory][]string
}

//nolint:gochecknoglobals // Singleton pattern for pattern registry.
var (
	defaultRegistry *PatternRegistry
	once            sync.Once
)

// DefaultRegistry returns the singleton default pattern registry.
func DefaultRegistry() *PatternRegistry {
	once.Do(func() {
		defaultRegistry = newRegistry()
	})

	return defaultRegistry
}

// newRegistry creates a new pattern registry with all patterns initialized.
func newRegistry() *PatternRegistry {
	r := &PatternRegistry{
		patterns: make(map[PatternCategory][]string),
	}
	r.initializePatterns()

	return r
}

// Get returns the patterns for a given category.
func (r *PatternRegistry) Get(category PatternCategory) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if patterns, ok := r.patterns[category]; ok {
		result := make([]string, len(patterns))
		copy(result, patterns)

		return result
	}

	return nil
}

// getRef 回傳 category 對應的內部 pattern slice,**不拷貝**(熱路徑只讀用)。
// 呼叫端只可遍歷、**絕不可 mutate** 回傳的 slice — registry 在 initializePatterns
// 之後不可變,共享 backing array 對並發只讀安全。公共 Get 仍回拷貝供外部 caller。
func (r *PatternRegistry) getRef(category PatternCategory) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.patterns[category]
}

// initializePatterns populates all pattern categories.
func (r *PatternRegistry) initializePatterns() {
	r.patterns[NumericMalicious] = []string{
		"0x", "0X", "0b", "0B", "0o", "0O", // Different base encodings
		"++", "--", "+-", "-+", // Multiple signs
		"ee", "EE", "e+e", "E+E", "e-e", "E-E", // Invalid scientific notation
		"...", "..", // Multiple decimal points
		"Infinity", "infinity", "INFINITY", "+Infinity", "-Infinity",
		"NaN", "nan", "NAN", "+NaN", "-NaN",
		"inf", "INF",
	}

	r.patterns[FormulaInjection] = []string{
		"=", "@", "\t=", "\r=", "\n=",
	}

	// 只留「試算表函式」—— 危險僅在被呼叫(後接 `(`)時,DetectFormula 以 call-syntax
	// 比對(避免 document/query/offset 等常見英文字 bare 誤命中)。shell / code-exec
	// token(cmd/powershell/bash/sh/system/exec/eval/spawn/shell)bare 即危險,已移到
	// commandInjectionWordTokens 用 \bword\b 比對(見上方),否則 call-syntax gate 會
	// 漏掉不接 `(` 的 bare 命令。
	r.patterns[DangerousFunctions] = []string{
		"hyperlink", "importxml", "importhtml",
		"importrange", "importdata", "importfeed",
		"webservice", "filterxml", "document",
		"indirect", "offset", "choose",
		"dde", "msqry", "odbc", "query",
	}

	r.patterns[ScriptInjection] = []string{
		"<script", "</script", "javascript:", "vbscript:", "data:text/html",
		"data:text/javascript", "data:application/javascript", "data:text/vbscript",
		"onload=", "onerror=", "onclick=", "onmouseover=", "onfocus=", "onblur=",
		"eval(", "setTimeout(", "setInterval(", "Function(", "constructor(",
		"alert(", "confirm(", "prompt(", "document.write", "document.writeln",
		"innerHTML", "outerHTML", "insertAdjacentHTML", "execScript",
	}

	r.patterns[SQLInjection] = []string{
		"union select", "drop table", "delete from",
		"insert into", "update set", "create table",
		"alter table", "truncate table", "grant all",
		"revoke all", "show tables", "show databases",
		"information_schema", "sys.tables", "sysobjects",
		"xp_cmdshell", "sp_executesql", "openrowset",
		"bulk insert", "load_file", "into outfile",
		"'or'1'='1", "\"or\"1\"=\"1", "'or 1=1", "\"or 1=1",
		"admin'--", "admin\"--",
		"'; drop", "\"; drop", "'; delete", "\"; delete",
		"'; insert", "\"; insert", "'; update", "\"; update",
		"'; create", "\"; create", "'; alter", "\"; alter",
	}

	// H11 / P1-7 — Command-injection substring patterns。
	//
	// 短到 2-3 char 的 unix command token (`id`、`at `、`sh`、`su -`) 與
	// 中等長度但常出現在合法字串內的 token (`pwd`、`kill`、`mount`、`chmod`、`batch`)
	// 都已移到 commandInjectionWordTokens 用 \bword\b regex 比對(見 patterns.go 上方)。
	//
	// 這條 substring list 只留長度與 specificity 都足夠的 token,確保子串
	// 比對在真實 EMG cell 上不會誤命中。例如 `killall` / `umount` 仍在這裡 —
	// 它們是合成複合詞(killall 不是 kill, umount 不是 mount),不被 word-boundary
	// 守門吃掉,但本身就是 specific 的攻擊 token。
	//
	// `chown` 留 substring(沒搬到 word-boundary)— 雖然原則上也算中等長度,
	// 但「chown」出現在合法 EMG cell 的機率極低(實測無誤判);若未來見到
	// false-positive,可再搬到 word-boundary 組。
	r.patterns[CommandInjection] = []string{
		"curl ", "wget ", "nc ", "netcat ", "telnet ", "ssh ", "scp ", "rsync ",
		"cat /", "ls /", "whoami", "uname", "ps aux", "netstat",
		"iptables", "firewall", "sudo", "chown",
		"umount", "killall", "pkill", "service", "systemctl",
		"crontab", "nohup", "screen", "tmux", "disown",
		"../", "..\\", "/etc/", "C:\\", "/bin/", "/usr/", "/var/", "/tmp/",
		"/home/", "/root/", "C:\\Windows\\", "C:\\Users\\", "C:\\Program Files\\",
	}

	r.patterns[DangerousChars] = []string{
		"<", ">", ":", "\"", "|", "?", "*", "\x00",
		";", "&", "&&", "||", "`", "$", "{", "}",
		"'", "--", "/*", "*/", "@@", "xp_", "sp_",
		"<script", "</script", "javascript:", "vbscript:", "onload=", "onerror=",
		"../", "..\\", ".../", "...\\", "....//", "....\\\\",
		"\u0000",
		"\x01", "\x02", "\x03", "\x04", "\x05", "\x06", "\x07", "\x08",
		"\x0b", "\x0c", "\x0e", "\x0f",
		"\x10", "\x11", "\x12", "\x13", "\x14", "\x15", "\x16", "\x17",
		"\x18", "\x19", "\x1a", "\x1b", "\x1c", "\x1d", "\x1e", "\x1f",
	}

	r.patterns[SuspiciousExtensions] = []string{
		".exe", ".bat", ".cmd", ".com", ".pif", ".scr", ".vbs", ".vbe", ".js", ".jse",
		".wsf", ".wsh", ".msi", ".msp", ".reg", ".scf", ".lnk", ".inf", ".dll", ".sys",
		".drv", ".ocx", ".cpl", ".jar", ".app", ".deb", ".rpm", ".dmg", ".pkg",
		".ps1", ".psm1", ".psd1", ".ps1xml", ".pssc", ".psrc", ".cdxml", ".sh",
		".bash", ".zsh", ".fish", ".ksh", ".tcsh", ".py", ".rb", ".pl", ".php",
	}

	r.patterns[ReservedNames] = []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	}
}
