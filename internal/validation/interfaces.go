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

// FilenameValidator defines the interface for filename validation.
type FilenameValidator interface {
	// ValidateFilename validates a filename for safety and correctness.
	ValidateFilename(filename string) error

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
const (
	safeMinInt64   = -9223372036854775807    // Safe int64 min (leaving room for calculations)
	safeMaxInt64   = 9223372036854775806     // Safe int64 max (leaving room for calculations)
	safeMinFloat64 = -1.7976931348623157e307 // Safe float64 min
	safeMaxFloat64 = 1.7976931348623157e307  // Safe float64 max
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
