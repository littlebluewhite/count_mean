package validation

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"count_mean/internal/errors"
	csvvalidator "count_mean/internal/validation/csv"
	"count_mean/internal/validation/filename"
	"count_mean/internal/validation/numeric"
)

// Validation constants.
const (
	defaultMaxFileSize   = 100 * 1024 * 1024 // 100MB
	defaultMaxWindowSize = 10000
	defaultMaxPrecision  = 15
	maxTimeRangeValue    = 1e10
	maxScalingFactor     = 20
	maxPhaseLabels       = 50
	maxPathLength        = 4096
	maxEmailLength       = 254
)

// InputValidator provides comprehensive input validation functionality.
// It acts as a facade delegating to specialized validators.
type InputValidator struct {
	// Configuration constraints
	maxFileSize       int64
	allowedExtensions []string
	maxWindowSize     int
	maxPrecision      int

	// Specialized validators
	numericValidator  *numeric.Validator
	filenameValidator *filename.Validator
	csvValidator      *csvvalidator.Validator
}

// NewInputValidator creates a new input validator with default constraints.
func NewInputValidator() *InputValidator {
	v := &InputValidator{
		maxFileSize:       defaultMaxFileSize,
		allowedExtensions: []string{".csv"},
		maxWindowSize:     defaultMaxWindowSize,
		maxPrecision:      defaultMaxPrecision,
		numericValidator:  numeric.NewValidator(),
		filenameValidator: filename.NewValidator(),
		csvValidator:      csvvalidator.NewValidator(),
	}

	return v
}

// WithMaxFileSize sets the maximum allowed file size.
func (v *InputValidator) WithMaxFileSize(size int64) *InputValidator {
	v.maxFileSize = size

	return v
}

// WithAllowedExtensions sets the allowed file extensions.
func (v *InputValidator) WithAllowedExtensions(extensions []string) *InputValidator {
	v.allowedExtensions = extensions
	v.filenameValidator.WithAllowedExtensions(extensions)

	return v
}

// ValidateInteger validates integer input with overflow protection.
func (v *InputValidator) ValidateInteger(value, fieldName string, minValue, maxValue int64) (int64, error) {
	result, err := v.numericValidator.ValidateInteger(value, fieldName, minValue, maxValue)
	if err != nil {
		return 0, fmt.Errorf("整數驗證失敗: %w", err)
	}

	return result, nil
}

// ValidateFloat validates float input with overflow protection.
func (v *InputValidator) ValidateFloat(value, fieldName string, minValue, maxValue float64) (float64, error) {
	result, err := v.numericValidator.ValidateFloat(value, fieldName, minValue, maxValue)
	if err != nil {
		return 0, fmt.Errorf("浮點數驗證失敗: %w", err)
	}

	return result, nil
}

// ValidatePositiveInteger validates positive integer with safe bounds.
func (v *InputValidator) ValidatePositiveInteger(value, fieldName string, maxValue int64) (int64, error) {
	result, err := v.numericValidator.ValidatePositiveInteger(value, fieldName, maxValue)
	if err != nil {
		return 0, fmt.Errorf("正整數驗證失敗: %w", err)
	}

	return result, nil
}

// ValidatePositiveFloat validates positive float with safe bounds.
func (v *InputValidator) ValidatePositiveFloat(value, fieldName string, maxValue float64) (float64, error) {
	result, err := v.numericValidator.ValidatePositiveFloat(value, fieldName, maxValue)
	if err != nil {
		return 0, fmt.Errorf("正浮點數驗證失敗: %w", err)
	}

	return result, nil
}

// ValidateFilename validates a filename for safety and correctness.
func (v *InputValidator) ValidateFilename(fn string) error {
	if err := v.filenameValidator.ValidateFilename(fn); err != nil {
		return fmt.Errorf("檔案名稱驗證失敗: %w", err)
	}

	return nil
}

// ValidateWindowSize validates the window size parameter.
func (v *InputValidator) ValidateWindowSize(windowSizeStr string) (int, error) {
	windowSize64, err := v.ValidatePositiveInteger(windowSizeStr, "window_size", int64(v.maxWindowSize))
	if err != nil {
		return 0, err
	}

	return int(windowSize64), nil
}

// ValidateTimeRange validates time range parameters.
func (v *InputValidator) ValidateTimeRange(startRangeStr, endRangeStr string) (float64, float64, bool, error) {
	var (
		startRange, endRange float64
		useCustomRange       bool
		err                  error
	)

	if startRangeStr != "" {
		startRange, err = v.ValidatePositiveFloat(startRangeStr, "start_range", maxTimeRangeValue)
		if err != nil {
			return 0, 0, false, err
		}

		useCustomRange = true
	}

	if endRangeStr != "" {
		endRange, err = v.ValidatePositiveFloat(endRangeStr, "end_range", maxTimeRangeValue)
		if err != nil {
			return 0, 0, false, err
		}

		useCustomRange = true
	}

	if useCustomRange && startRangeStr != "" && endRangeStr != "" {
		if startRange >= endRange {
			return 0, 0, false, errors.NewValidationError("time_range",
				map[string]float64{"start": startRange, "end": endRange},
				"開始範圍必須小於結束範圍")
		}
	}

	return startRange, endRange, useCustomRange, nil
}

// ValidateScalingFactor validates the scaling factor parameter.
func (v *InputValidator) ValidateScalingFactor(scalingFactorStr string) (int, error) {
	scalingFactor64, err := v.ValidatePositiveInteger(scalingFactorStr, "scaling_factor", maxScalingFactor)
	if err != nil {
		return 0, err
	}

	return int(scalingFactor64), nil
}

// ValidatePrecision validates the precision parameter.
func (v *InputValidator) ValidatePrecision(precisionStr string) (int, error) {
	precision64, err := v.ValidateInteger(precisionStr, "precision", 0, int64(v.maxPrecision))
	if err != nil {
		return 0, err
	}

	return int(precision64), nil
}

// ValidatePhaseLabels validates phase label input.
func (*InputValidator) ValidatePhaseLabels(phaseLabelsText string) ([]string, error) {
	if strings.TrimSpace(phaseLabelsText) == "" {
		return nil, errors.NewValidationError("phase_labels", phaseLabelsText,
			"階段標籤不能為空")
	}

	lines := strings.Split(phaseLabelsText, "\n")
	cleanLabels := make([]string, 0, len(lines))

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if err := validateSinglePhaseLabel(trimmed, i+1); err != nil {
			return nil, err
		}

		cleanLabels = append(cleanLabels, trimmed)
	}

	if len(cleanLabels) == 0 {
		return nil, errors.NewValidationError("phase_labels", phaseLabelsText,
			"至少需要一個有效的階段標籤")
	}

	if len(cleanLabels) > maxPhaseLabels {
		return nil, errors.NewValidationError("phase_labels", cleanLabels,
			fmt.Sprintf("階段標籤數量不能超過 %d 個", maxPhaseLabels))
	}

	return cleanLabels, nil
}

// validateSinglePhaseLabel validates a single phase label.
func validateSinglePhaseLabel(label string, lineNum int) error {
	if len(label) > 100 {
		return errors.NewValidationError("phase_label", label,
			fmt.Sprintf("第 %d 行的階段標籤過長 (最大 100 字符)", lineNum))
	}

	for _, r := range label {
		if unicode.IsControl(r) && r != '\t' {
			return errors.NewValidationError("phase_label", label,
				fmt.Sprintf("第 %d 行的階段標籤包含非法字符", lineNum))
		}
	}

	return nil
}

// ValidateDirectoryPath validates directory path input.
func (*InputValidator) ValidateDirectoryPath(path string) error {
	if path == "" {
		return errors.NewValidationError("directory_path", path, "目錄路徑不能為空")
	}

	path = strings.TrimSpace(path)

	if strings.Contains(path, "\x00") {
		return errors.NewValidationError("directory_path", path, "路徑包含非法字符")
	}

	if len(path) > maxPathLength {
		return errors.NewValidationError("directory_path", path, "路徑過長")
	}

	return nil
}

// SanitizeString removes dangerous characters from string input.
func (*InputValidator) SanitizeString(input string) string {
	input = strings.ReplaceAll(input, "\x00", "")

	var result strings.Builder

	for _, r := range input {
		if !unicode.IsControl(r) || r == '\t' || r == '\n' || r == '\r' {
			_, _ = result.WriteRune(r)
		}
	}

	return result.String()
}

// ValidateOutputFormat validates output format selection.
func (*InputValidator) ValidateOutputFormat(format string) error {
	if format == "" {
		return errors.NewValidationError("output_format", format, "輸出格式不能為空")
	}

	validFormats := map[string]bool{
		"csv":  true,
		"json": true,
		"xlsx": true,
	}

	if !validFormats[strings.ToLower(format)] {
		return errors.NewValidationError("output_format", format,
			fmt.Sprintf("不支援的輸出格式: %s", format))
	}

	return nil
}

// ValidateCSVData validates CSV data structure and detects malicious content.
func (v *InputValidator) ValidateCSVData(records [][]string, fn string) error {
	if err := v.csvValidator.ValidateCSVData(records, fn); err != nil {
		return fmt.Errorf("CSV 資料驗證失敗: %w", err)
	}

	return nil
}

// emailPattern is the regex pattern for email validation.
var emailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// ValidateEmail validates email address format.
func (*InputValidator) ValidateEmail(email string) error {
	if email == "" {
		return errors.NewValidationError("email", email, "電子郵件地址不能為空")
	}

	email = strings.TrimSpace(email)

	if !emailPattern.MatchString(email) {
		return errors.NewValidationError("email", email, "無效的電子郵件地址格式")
	}

	if len(email) > maxEmailLength {
		return errors.NewValidationError("email", email, "電子郵件地址過長")
	}

	return nil
}
