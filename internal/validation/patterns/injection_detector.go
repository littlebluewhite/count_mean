package patterns

import (
	"strings"
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
	// Check formula starters
	starters := d.registry.Get(FormulaInjection)
	for _, starter := range starters {
		if strings.HasPrefix(content, starter) {
			return true, starter
		}
	}

	// Check dangerous functions (case-insensitive)
	contentLower := strings.ToLower(content)

	functions := d.registry.Get(DangerousFunctions)
	for _, fn := range functions {
		if strings.Contains(contentLower, strings.ToLower(fn)) {
			return true, fn
		}
	}

	return false, ""
}

// DetectSQL checks for SQL injection patterns.
func (d *InjectionDetectorImpl) DetectSQL(content string) (bool, string) {
	contentLower := strings.ToLower(content)
	patterns := d.registry.Get(SQLInjection)

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
	patterns := d.registry.Get(ScriptInjection)

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
	// (1) word-boundary 短 token — 用 regex \btoken\b 避免子字串誤判。
	for _, re := range CommandInjectionWordTokens() {
		if loc := re.FindStringIndex(content); loc != nil {
			return true, content[loc[0]:loc[1]]
		}
	}

	// (2) substring patterns — 長度 / specificity 足夠，case-insensitive substring 即可。
	contentLower := strings.ToLower(content)
	patterns := d.registry.Get(CommandInjection)

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
	patterns := d.registry.Get(NumericMalicious)

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
	extensions := d.registry.Get(SuspiciousExtensions)

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
	chars := d.registry.Get(DangerousChars)

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
