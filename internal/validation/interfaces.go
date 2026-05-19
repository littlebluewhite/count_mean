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

// NumericValidator defines the interface for numeric validation.
type NumericValidator interface {
	// ValidateInteger validates integer input with overflow protection.
	ValidateInteger(value, fieldName string, minValue, maxValue int64) (int64, error)

	// ValidateFloat validates float input with overflow protection.
	ValidateFloat(value, fieldName string, minValue, maxValue float64) (float64, error)

	// ValidatePositiveInteger validates positive integer with safe bounds.
	ValidatePositiveInteger(value, fieldName string, maxValue int64) (int64, error)

	// ValidatePositiveFloat validates positive float with safe bounds.
	ValidatePositiveFloat(value, fieldName string, maxValue float64) (float64, error)
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

// NumericRange defines validation ranges for different numeric types.
type NumericRange struct {
	MinInt64   int64
	MaxInt64   int64
	MinFloat64 float64
	MaxFloat64 float64
}

// Safe numeric range constants to prevent overflow attacks.
//
// Each constant is intentionally narrower than the language-level max
// (math.MinInt64 / math.MaxInt64 / -math.MaxFloat64 / math.MaxFloat64) to
// preserve a calculation buffer — callers can do `value ± 1` for integers or
// `value * 10` for floats without immediately flipping to overflow / Inf. The
// "safe…" prefix names this property; the actual value being smaller than the
// literal type max is intentional and documented (TODO_2 #P1-A8-5).
const (
	// safeMinInt64 = math.MinInt64 + 1 (-9223372036854775808 + 1). The 1-unit
	// reserve lets validators subtract small offsets without wrapping.
	safeMinInt64 = -9223372036854775807
	// safeMaxInt64 = math.MaxInt64 - 1 (9223372036854775807 - 1). Symmetric
	// 1-unit reserve for adding small offsets.
	safeMaxInt64 = 9223372036854775806
	// safeMinFloat64 is intentionally one decimal order of magnitude greater
	// than -math.MaxFloat64 (≈-1.7976931348623157e+308). The 10x reserve
	// gives a safety buffer for downstream arithmetic.
	safeMinFloat64 = -1.7976931348623157e307
	// safeMaxFloat64 is intentionally one decimal order of magnitude below
	// math.MaxFloat64 (≈1.7976931348623157e+308). Same 10x reserve rationale
	// as safeMinFloat64. Anything outside [-1e307, +1e307] is treated as
	// suspect input.
	safeMaxFloat64 = 1.7976931348623157e307
)

// GetSafeNumericRanges returns predefined safe ranges to prevent overflow attacks.
func GetSafeNumericRanges() *NumericRange {
	return &NumericRange{
		MinInt64:   safeMinInt64,
		MaxInt64:   safeMaxInt64,
		MinFloat64: safeMinFloat64,
		MaxFloat64: safeMaxFloat64,
	}
}
