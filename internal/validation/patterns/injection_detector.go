package patterns

import (
	"strings"
	"unicode"
)

// InjectionDetectorImpl implements InjectionDetector interface.
type InjectionDetectorImpl struct {
	registry *PatternRegistry
}

// NewInjectionDetector creates a new injection detector with default patterns.
func NewInjectionDetector() *InjectionDetectorImpl {
	return &InjectionDetectorImpl{
		registry: DefaultRegistry(),
	}
}

// NewInjectionDetectorWithRegistry creates an injection detector with custom registry.
func NewInjectionDetectorWithRegistry(registry *PatternRegistry) *InjectionDetectorImpl {
	return &InjectionDetectorImpl{
		registry: registry,
	}
}

// DetectFormula checks for CSV formula injection patterns.
func (d *InjectionDetectorImpl) DetectFormula(content string) (bool, string) {
	// Check formula starters — getRef:只讀遍歷,不 mutate,省去防禦性拷貝。
	starters := d.registry.getRef(FormulaInjection)
	for _, starter := range starters {
		if strings.HasPrefix(content, starter) {
			return true, starter
		}
	}

	// Check dangerous functions — 只在「被呼叫」(後接 `(`)時才算命中。
	// 危險函式僅在被呼叫時危險;naive substring 比對會把 `shoulder`(⊃`sh`)、
	// `systemic`(⊃`system`)、`evaluation`(⊃`eval`)等合法 EMG 內容誤殺。gate 在
	// 呼叫語法上,並掃描所有出現位置 → 早期良性子串不會 shadow 後面真正的呼叫。
	contentLower := strings.ToLower(content)

	// getRef:只讀遍歷,不 mutate,省去防禦性拷貝。
	functions := d.registry.getRef(DangerousFunctions)
	for _, fn := range functions {
		if isFunctionCall(contentLower, strings.ToLower(fn)) {
			return true, fn
		}
	}

	return false, ""
}

// isFunctionCall 回報 fn 是否在 content 中以「呼叫」形式出現 — 即某個 fn 出現位置
// 之後(略過所有空白:空白 / tab / 換行 / CR / Unicode 空白)緊接 `(`。content 與
// fn 須皆已 lower-case。掃描每個出現位置,因此早期良性子串(如 `wash` 裡的 `sh`)
// 不會 shadow 後面真正的呼叫(`system(`)。
//
// 略過「全部」空白而非僅空白 / tab:試算表在函式名與 `(` 之間容忍各種空白(含換行),
// 且 quoted CSV multiline cell 可合法夾帶 `\n`/`\r`(cell_validator 的 checkControlChars
// 放行 `\t`/`\n`/`\r`)— 只略過空白 / tab 會讓 `system\n(1)` 繞過危險函式偵測。
func isFunctionCall(content, fn string) bool {
	from := 0
	for {
		idx := strings.Index(content[from:], fn)
		if idx < 0 {
			return false
		}

		rest := strings.TrimLeftFunc(content[from+idx+len(fn):], unicode.IsSpace)
		if strings.HasPrefix(rest, "(") {
			return true
		}

		from += idx + 1
	}
}

// DetectSQL checks for SQL injection patterns.
func (d *InjectionDetectorImpl) DetectSQL(content string) (bool, string) {
	contentLower := strings.ToLower(content)
	// getRef:只讀遍歷,不 mutate,省去防禦性拷貝。
	patterns := d.registry.getRef(SQLInjection)

	for _, pattern := range patterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			return true, pattern
		}
	}

	return false, ""
}

// DetectScript checks for script injection patterns (XSS).
func (d *InjectionDetectorImpl) DetectScript(content string) (bool, string) {
	contentLower := strings.ToLower(content)
	// getRef:只讀遍歷,不 mutate,省去防禦性拷貝。
	patterns := d.registry.getRef(ScriptInjection)

	for _, pattern := range patterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			return true, pattern
		}
	}

	return false, ""
}

// DetectCommand checks for command injection patterns.
//
// 原本 case-sensitive 比對，與 DetectFormula/DetectSQL/DetectScript 不一致 —
// `WHOAMI`、`Curl` 等大小寫變體會繞過偵測。改成 case-insensitive 與其他 detector 對齊。
// 路徑 pattern（`/etc/`、`C:\`）case 在類 Unix 系統有語意意義，但 attacker 控制的
// 字串依然會被 lowercase 化偵測 — 標準路徑（`/Etc/`、`c:\`）很少出現在 EMG cell content，
// 一致化的 false positive 風險可忽略。
//
// 兩段比對：
//
//  1. 先跑 word-boundary 短 token（`id`、`sh`、`at`、`su -`），避免 substring 在
//     `david`、`subject_id`、`Mid Foot`、`Cash`、`shoulder` 上爆炸誤判。
//  2. 再跑長 token substring list（`curl `、`whoami`、`netcat `、`/etc/` 等），
//     這些子串本身已足夠 specific，不會在合法 EMG cell 上誤命中。
func (d *InjectionDetectorImpl) DetectCommand(content string) (bool, string) {
	// (1) word-boundary 短 token — 直接遍歷包級 var,只讀、勿 mutate;省去
	// CommandInjectionWordTokens() 每次呼叫的防禦性拷貝。
	for _, re := range commandInjectionWordTokens {
		if loc := re.FindStringIndex(content); loc != nil {
			return true, content[loc[0]:loc[1]]
		}
	}

	// (2) substring patterns — 長度 / specificity 足夠，case-insensitive substring 即可。
	// getRef:只讀遍歷,不 mutate,省去防禦性拷貝。
	contentLower := strings.ToLower(content)
	patterns := d.registry.getRef(CommandInjection)

	for _, pattern := range patterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			return true, pattern
		}
	}

	return false, ""
}

// DetectAll runs all injection detection and returns the first match.
func (d *InjectionDetectorImpl) DetectAll(content string) (bool, string, string) {
	if detected, pattern := d.DetectFormula(content); detected {
		return true, "formula", pattern
	}

	if detected, pattern := d.DetectScript(content); detected {
		return true, "script", pattern
	}

	if detected, pattern := d.DetectSQL(content); detected {
		return true, "sql", pattern
	}

	if detected, pattern := d.DetectCommand(content); detected {
		return true, "command", pattern
	}

	return false, "", ""
}

// DetectMaliciousNumeric checks for malicious patterns in numeric strings.
func (d *InjectionDetectorImpl) DetectMaliciousNumeric(value string) (bool, string) {
	valueLower := strings.ToLower(value)
	// getRef:只讀遍歷,不 mutate,省去防禦性拷貝。
	patterns := d.registry.getRef(NumericMalicious)

	for _, pattern := range patterns {
		if strings.Contains(valueLower, strings.ToLower(pattern)) {
			return true, pattern
		}
	}

	return false, ""
}

// DetectSuspiciousExtension checks for suspicious file extensions.
func (d *InjectionDetectorImpl) DetectSuspiciousExtension(content string) (bool, string) {
	contentLower := strings.ToLower(content)
	// getRef:只讀遍歷,不 mutate,省去防禦性拷貝。
	extensions := d.registry.getRef(SuspiciousExtensions)

	for _, ext := range extensions {
		if strings.Contains(contentLower, ext) {
			return true, ext
		}
	}

	return false, ""
}

// DetectDangerousChars checks for dangerous characters in filenames.
func (d *InjectionDetectorImpl) DetectDangerousChars(content string) (bool, string) {
	contentLower := strings.ToLower(content)
	// getRef:只讀遍歷,不 mutate,省去防禦性拷貝。
	chars := d.registry.getRef(DangerousChars)

	for _, char := range chars {
		if strings.Contains(contentLower, strings.ToLower(char)) {
			return true, char
		}
	}

	return false, ""
}

// IsReservedName checks if the name is a Windows reserved name, honoring this
// detector's (possibly custom) registry — consistent with the other Detect* methods.
func (d *InjectionDetectorImpl) IsReservedName(name string) bool {
	return isReservedNameIn(d.registry, name)
}
