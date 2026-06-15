// Package validation provides input validation and security checks for the application.
package validation

// InjectionDetector defines the interface for detecting various injection attacks.
type InjectionDetector interface {
	// DetectFormula checks for CSV formula injection patterns.
	DetectFormula(content string) (bool, string)

	// DetectSQL checks for SQL injection patterns.
	DetectSQL(content string) (bool, string)

	// DetectScript checks for script injection patterns (XSS).
	DetectScript(content string) (bool, string)

	// DetectCommand checks for command injection patterns.
	DetectCommand(content string) (bool, string)

	// DetectAll runs all injection detection and returns the first match.
	DetectAll(content string) (bool, string, string)
}

// FilenameValidator defines the read-only interface for filename validation.
//
// L16 — 拆分:讀（ValidateFilename）與寫（WithAllowedExtensions）解耦,讓單純需要
// 「驗證 filename」的 caller 不必依賴 mutator 方法（也避免 mock 起來要 noop 寫實）。
// 大多 production caller 只需 ValidateFilename — interface 接口面收窄到剛好夠用。
type FilenameValidator interface {
	// ValidateFilename validates a filename for safety and correctness.
	ValidateFilename(filename string) error
}

// MutableFilenameValidator 為支援動態變更 allowed extension 的 filename validator。
//
// L16 — 從原 FilenameValidator 拆出 mutator 子集。新增 caller 應優先 depend on
// 上方 FilenameValidator(read-only); 只有「動態調整副檔名白名單」的 caller 才
// 需要這條更大的 interface。標準實作 `filename.Validator` 自動滿足兩條 interface。
type MutableFilenameValidator interface {
	FilenameValidator
	// WithAllowedExtensions sets the allowed file extensions.
	WithAllowedExtensions(extensions []string)
}

// CSVValidator defines the interface for CSV data validation.
type CSVValidator interface {
	// ValidateCSVData validates CSV data structure and detects malicious content.
	ValidateCSVData(records [][]string, filename string) error
}
