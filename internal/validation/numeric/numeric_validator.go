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
//
// The intentional headroom between these constants and the language-level
// math.MaxInt64 / math.MaxFloat64 limits is documented per-line below — both
// are *below* the absolute max so callers can perform follow-up arithmetic
// (add, multiply, etc.) without immediately overflowing or producing Inf.
// The naming ("safe…") signals this — the actual value is intentionally
// smaller than the type's literal maximum (TODO_2 #P1-A8-5).
const (
	// safeMaxInt64 = math.MaxInt64 - 1 (9223372036854775807 - 1). The 1-unit
	// reserve lets validators add small offsets (e.g. +1 for boundary checks,
	// length adjustments) without wrapping to negative.
	safeMaxInt64 = 9223372036854775806
	// safeMaxFloat64 is intentionally one decimal order of magnitude below
	// math.MaxFloat64 (≈1.7976931348623157e+308). The 10x reserve lets
	// downstream callers compute `value * 10` / `value + small` without
	// flipping to +Inf. Anything larger than 1e307 is treated as suspect input.
	safeMaxFloat64 = 1.7976931348623157e+307

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

	// Reject any malicious numeric pattern up-front. The historical "allow e+/e-
	// for integers" sub-branches were dead code — patterns/patterns.go registers
	// only `e+e` / `E+E` / `e-e` / `E-E` (double-letter forms), so
	// DetectMaliciousNumeric never returns a standalone `e+` / `e-` / `E+` /
	// `E-` pattern, and the legitimate scientific notation case is handled by
	// strconv.ParseInt (which rejects "1e2" for integers anyway). Removing the
	// dead branch (TODO_2 #P1-A8-4) simplifies the control flow without
	// changing observable behavior — covered by
	// internal/validation/numeric/numeric_validator_test.go.
	if detected, pattern := v.detector.DetectMaliciousNumeric(value); detected {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 包含可疑的數值模式: %s", fieldName, pattern))
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

	// Reject any malicious numeric pattern up-front. The historical "allow
	// e+/e- scientific notation" branch was dead — patterns/patterns.go only
	// registers `e+e` / `E+E` / `e-e` / `E-E` (double-letter forms) plus
	// `++`, `--`, `0x`, `Infinity`, `NaN`, etc. None of those is a standalone
	// `e+` / `e-` / `E+` / `E-`, so `pattern != "e+"` was always true and the
	// branch unreachable. Legitimate scientific notation (`1.5e10`,
	// `2.0e-3`, `1.5e+10`) never matches any registered substring so it
	// passes through this guard untouched and is parsed by strconv.ParseFloat
	// downstream. Simplification tracked under TODO_2 #P1-A8-4; locked in by
	// numeric_validator_test.go.
	if detected, pattern := v.detector.DetectMaliciousNumeric(value); detected {
		return 0, errors.NewValidationError(fieldName, value,
			fmt.Sprintf("%s 包含可疑的數值模式: %s", fieldName, pattern))
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
