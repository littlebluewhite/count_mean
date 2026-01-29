// Package patterns provides centralized malicious pattern detection for security validation.
package patterns

import (
	"sync"
)

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

	r.patterns[DangerousFunctions] = []string{
		"cmd", "powershell", "bash", "sh", "system",
		"exec", "eval", "spawn", "shell",
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

	r.patterns[CommandInjection] = []string{
		"curl ", "wget ", "nc ", "netcat ", "telnet ", "ssh ", "scp ", "rsync ",
		"cat /", "ls /", "pwd", "whoami", "id", "uname", "ps aux", "netstat",
		"iptables", "firewall", "sudo", "su -", "chmod", "chown", "mount",
		"umount", "kill", "killall", "pkill", "service", "systemctl",
		"crontab", "at ", "batch", "nohup", "screen", "tmux", "disown",
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
