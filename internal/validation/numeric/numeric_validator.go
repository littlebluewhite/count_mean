// Package numeric provides numeric validation functionality with overflow protection.
package numeric

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"count_mean/internal/errors"
	"count_mean/internal/validation/patterns"
)

// Safe numeric range constants to prevent overflow attacks.
const (
	safeMaxInt64   = 9223372036854775806     // Safe int64 max (leaving room for calculations)
	safeMaxFloat64 = 1.7976931348623157e+307 // Safe float64 max

	// maxIntegerInputLength is the maximum allowed length for integer input strings.
	maxIntegerInputLength = 20
	// maxFloatInputLength is the maximum allowed length for float input strings.
	maxFloatInputLength = 50
)

// Validator provides numeric validation functionality.
type Validator struct {
	detector *patterns.InjectionDetectorImpl
}

// NewValidator creates a new numeric validator.
func NewValidator() *Validator {
	return &Validator{
		detector: patterns.NewInjectionDetector(),
	}
}

// ValidateInteger validates integer input with overflow protection.
func (v *Validator) ValidateInteger(value, fieldName string, minValue, maxValue int64) (int64, error) {
	if value == "" {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 不能為空", fieldName))
	}

	value = strings.TrimSpace(value)

	// Check for malicious patterns (excluding valid scientific notation)
	if detected, pattern := v.detector.DetectMaliciousNumeric(value); detected {
		// For integers, scientific notation is suspicious
		if pattern == "e+" || pattern == "E+" || pattern == "e-" || pattern == "E-" {
			return 0, errors.NewValidationError(fieldName, value,
				fmt.Sprintf("%s 包含可疑的數值模式: %s", fieldName, pattern))
		}
		// Check other malicious patterns
		if !strings.HasPrefix(pattern, "e") && !strings.HasPrefix(pattern, "E") {
			return 0, errors.NewValidationError(fieldName, value,
				fmt.Sprintf("%s 包含可疑的數值模式: %s", fieldName, pattern))
		}
	}

	// Check for excessive length (potential DoS attack)
	if len(value) > maxIntegerInputLength {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 數值過長 (最大 %d 字符)", fieldName, maxIntegerInputLength))
	}

	intValue, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 必須是有效的整數", fieldName))
	}

	if intValue < minValue || intValue > maxValue {
		return 0, errors.NewValidationError(fieldName, intValue,
			fmt.Sprintf("%s 必須在 %d 到 %d 範圍內", fieldName, minValue, maxValue))
	}

	return intValue, nil
}

// ValidateFloat validates float input with overflow protection.
func (v *Validator) ValidateFloat(value, fieldName string, minValue, maxValue float64) (float64, error) {
	if value == "" {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 不能為空", fieldName))
	}

	value = strings.TrimSpace(value)

	// Check for malicious patterns (but allow valid scientific notation for floats)
	if detected, pattern := v.detector.DetectMaliciousNumeric(value); detected {
		// Allow valid scientific notation (e+ or e-)
		if pattern != "e+" && pattern != "E+" && pattern != "e-" && pattern != "E-" {
			return 0, errors.NewValidationError(fieldName, value,
				fmt.Sprintf("%s 包含可疑的數值模式: %s", fieldName, pattern))
		}
	}

	// Check for excessive length
	if len(value) > maxFloatInputLength {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 數值過長 (最大 %d 字符)", fieldName, maxFloatInputLength))
	}

	// Count decimal points
	if strings.Count(value, ".") > 1 {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 包含多個小數點", fieldName))
	}

	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 必須是有效的浮點數", fieldName))
	}

	if math.IsInf(floatValue, 0) {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 不能是無窮大", fieldName))
	}

	if math.IsNaN(floatValue) {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 不能是 NaN", fieldName))
	}

	if floatValue < minValue || floatValue > maxValue {
		return 0, errors.NewValidationError(fieldName, floatValue,
			fmt.Sprintf("%s 必須在 %f 到 %f 範圍內", fieldName, minValue, maxValue))
	}

	return floatValue, nil
}

// ValidatePositiveInteger validates positive integer with safe bounds.
func (v *Validator) ValidatePositiveInteger(value, fieldName string, maxValue int64) (int64, error) {
	if maxValue <= 0 || maxValue > safeMaxInt64 {
		maxValue = safeMaxInt64
	}

	result, err := v.ValidateInteger(value, fieldName, 1, maxValue)
	if err != nil {
		return 0, err
	}

	if result <= 0 {
		return 0, errors.NewValidationError(fieldName, result,
			fmt.Sprintf("%s 必須是正整數", fieldName))
	}

	return result, nil
}

// ValidatePositiveFloat validates positive float with safe bounds.
func (v *Validator) ValidatePositiveFloat(value, fieldName string, maxValue float64) (float64, error) {
	if maxValue <= 0 || maxValue > safeMaxFloat64 {
		maxValue = safeMaxFloat64
	}

	result, err := v.ValidateFloat(value, fieldName, 0.0, maxValue)
	if err != nil {
		return 0, err
	}

	if result <= 0 {
		return 0, errors.NewValidationError(fieldName, result,
			fmt.Sprintf("%s 必須是正數", fieldName))
	}

	return result, nil
}
