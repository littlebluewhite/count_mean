package validation

import (
	"count_mean/internal/errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// InputValidator provides comprehensive input validation functionality
type InputValidator struct {
	// Configuration constraints
	maxFileSize       int64
	allowedExtensions []string
	maxWindowSize     int
	maxPrecision      int
}

// NewInputValidator creates a new input validator with default constraints
func NewInputValidator() *InputValidator {
	return &InputValidator{
		maxFileSize:       100 * 1024 * 1024, // 100MB
		allowedExtensions: []string{".csv"},
		maxWindowSize:     10000,
		maxPrecision:      15,
	}
}

// WithMaxFileSize sets the maximum allowed file size
func (v *InputValidator) WithMaxFileSize(size int64) *InputValidator {
	v.maxFileSize = size
	return v
}

// WithAllowedExtensions sets the allowed file extensions
func (v *InputValidator) WithAllowedExtensions(extensions []string) *InputValidator {
	v.allowedExtensions = extensions
	return v
}

// ValidateFilename validates a filename for safety and correctness
func (v *InputValidator) ValidateFilename(filename string) error {
	if filename == "" {
		return errors.NewValidationError("filename", filename, "檔案名稱不能為空")
	}

	// Remove leading/trailing whitespace
	filename = strings.TrimSpace(filename)

	// Check for null bytes and control characters
	for _, r := range filename {
		if r == 0 || (unicode.IsControl(r) && r != '\t') {
			return errors.NewValidationError("filename", filename, "檔案名稱包含非法字符")
		}
	}

	// Check for dangerous characters
	dangerousChars := []string{"<", ">", ":", "\"", "|", "?", "*", "\x00"}
	for _, char := range dangerousChars {
		if strings.Contains(filename, char) {
			return errors.NewValidationError("filename", filename,
				fmt.Sprintf("檔案名稱包含非法字符: %s", char))
		}
	}

	// Check for reserved names (Windows)
	reservedNames := []string{
		"CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
	}

	baseName := strings.ToUpper(strings.TrimSuffix(filename, filepath.Ext(filename)))
	for _, reserved := range reservedNames {
		if baseName == reserved {
			return errors.NewValidationError("filename", filename,
				fmt.Sprintf("檔案名稱不能使用保留字: %s", reserved))
		}
	}

	// Check length
	if len(filename) > 255 {
		return errors.NewValidationError("filename", filename, "檔案名稱過長 (最大 255 字符)")
	}

	// Check extension
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" {
		validExt := false
		for _, allowedExt := range v.allowedExtensions {
			if ext == allowedExt {
				validExt = true
				break
			}
		}
		if !validExt {
			return errors.NewValidationError("filename", filename,
				fmt.Sprintf("不支援的檔案副檔名: %s", ext))
		}
	}

	return nil
}

// ValidateWindowSize validates the window size parameter
func (v *InputValidator) ValidateWindowSize(windowSizeStr string) (int, error) {
	if windowSizeStr == "" {
		return 0, errors.NewValidationError("window_size", windowSizeStr, "窗口大小不能為空")
	}

	// Clean the input
	windowSizeStr = strings.TrimSpace(windowSizeStr)

	// Parse the integer
	windowSize, err := strconv.Atoi(windowSizeStr)
	if err != nil {
		return 0, errors.NewValidationError("window_size", windowSizeStr,
			"窗口大小必須是有效的整數")
	}

	// Validate range
	if windowSize <= 0 {
		return 0, errors.NewValidationError("window_size", windowSize,
			"窗口大小必須大於 0")
	}

	if windowSize > v.maxWindowSize {
		return 0, errors.NewValidationError("window_size", windowSize,
			fmt.Sprintf("窗口大小不能超過 %d", v.maxWindowSize))
	}

	return windowSize, nil
}

// ValidateTimeRange validates time range parameters
func (v *InputValidator) ValidateTimeRange(startRangeStr, endRangeStr string) (float64, float64, bool, error) {
	var startRange, endRange float64
	var useCustomRange bool
	var err error

	// Validate start range
	if startRangeStr != "" {
		startRangeStr = strings.TrimSpace(startRangeStr)
		startRange, err = strconv.ParseFloat(startRangeStr, 64)
		if err != nil {
			return 0, 0, false, errors.NewValidationError("start_range", startRangeStr,
				"開始範圍必須是有效的數字")
		}

		if startRange < 0 {
			return 0, 0, false, errors.NewValidationError("start_range", startRange,
				"開始範圍不能為負數")
		}

		if math.IsInf(startRange, 0) || math.IsNaN(startRange) {
			return 0, 0, false, errors.NewValidationError("start_range", startRange,
				"開始範圍必須是有限數字")
		}

		useCustomRange = true
	}

	// Validate end range
	if endRangeStr != "" {
		endRangeStr = strings.TrimSpace(endRangeStr)
		endRange, err = strconv.ParseFloat(endRangeStr, 64)
		if err != nil {
			return 0, 0, false, errors.NewValidationError("end_range", endRangeStr,
				"結束範圍必須是有效的數字")
		}

		if endRange < 0 {
			return 0, 0, false, errors.NewValidationError("end_range", endRange,
				"結束範圍不能為負數")
		}

		if math.IsInf(endRange, 0) || math.IsNaN(endRange) {
			return 0, 0, false, errors.NewValidationError("end_range", endRange,
				"結束範圍必須是有限數字")
		}

		useCustomRange = true
	}

	// Validate range consistency
	if useCustomRange && startRangeStr != "" && endRangeStr != "" {
		if startRange >= endRange {
			return 0, 0, false, errors.NewValidationError("time_range",
				map[string]float64{"start": startRange, "end": endRange},
				"開始範圍必須小於結束範圍")
		}
	}

	return startRange, endRange, useCustomRange, nil
}

// ValidateScalingFactor validates the scaling factor parameter
func (v *InputValidator) ValidateScalingFactor(scalingFactorStr string) (int, error) {
	if scalingFactorStr == "" {
		return 0, errors.NewValidationError("scaling_factor", scalingFactorStr,
			"縮放因子不能為空")
	}

	scalingFactorStr = strings.TrimSpace(scalingFactorStr)

	scalingFactor, err := strconv.Atoi(scalingFactorStr)
	if err != nil {
		return 0, errors.NewValidationError("scaling_factor", scalingFactorStr,
			"縮放因子必須是有效的整數")
	}

	if scalingFactor <= 0 {
		return 0, errors.NewValidationError("scaling_factor", scalingFactor,
			"縮放因子必須大於 0")
	}

	if scalingFactor > 20 {
		return 0, errors.NewValidationError("scaling_factor", scalingFactor,
			"縮放因子不能超過 20 (避免數值溢出)")
	}

	return scalingFactor, nil
}

// ValidatePrecision validates the precision parameter
func (v *InputValidator) ValidatePrecision(precisionStr string) (int, error) {
	if precisionStr == "" {
		return 0, errors.NewValidationError("precision", precisionStr,
			"精度不能為空")
	}

	precisionStr = strings.TrimSpace(precisionStr)

	precision, err := strconv.Atoi(precisionStr)
	if err != nil {
		return 0, errors.NewValidationError("precision", precisionStr,
			"精度必須是有效的整數")
	}

	if precision < 0 {
		return 0, errors.NewValidationError("precision", precision,
			"精度不能為負數")
	}

	if precision > v.maxPrecision {
		return 0, errors.NewValidationError("precision", precision,
			fmt.Sprintf("精度不能超過 %d", v.maxPrecision))
	}

	return precision, nil
}

// ValidatePhaseLabels validates phase label input
func (v *InputValidator) ValidatePhaseLabels(phaseLabelsText string) ([]string, error) {
	if strings.TrimSpace(phaseLabelsText) == "" {
		return nil, errors.NewValidationError("phase_labels", phaseLabelsText,
			"階段標籤不能為空")
	}

	lines := strings.Split(phaseLabelsText, "\n")
	var cleanLabels []string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue // Skip empty lines
		}

		// Validate individual label
		if err := v.validateSinglePhaseLabel(trimmed, i+1); err != nil {
			return nil, err
		}

		cleanLabels = append(cleanLabels, trimmed)
	}

	if len(cleanLabels) == 0 {
		return nil, errors.NewValidationError("phase_labels", phaseLabelsText,
			"至少需要一個有效的階段標籤")
	}

	if len(cleanLabels) > 50 {
		return nil, errors.NewValidationError("phase_labels", cleanLabels,
			"階段標籤數量不能超過 50 個")
	}

	return cleanLabels, nil
}

// validateSinglePhaseLabel validates a single phase label
func (v *InputValidator) validateSinglePhaseLabel(label string, lineNum int) error {
	// Check length
	if len(label) > 100 {
		return errors.NewValidationError("phase_label", label,
			fmt.Sprintf("第 %d 行的階段標籤過長 (最大 100 字符)", lineNum))
	}

	// Check for control characters
	for _, r := range label {
		if unicode.IsControl(r) && r != '\t' {
			return errors.NewValidationError("phase_label", label,
				fmt.Sprintf("第 %d 行的階段標籤包含非法字符", lineNum))
		}
	}

	return nil
}

// ValidateDirectoryPath validates directory path input
func (v *InputValidator) ValidateDirectoryPath(path string) error {
	if path == "" {
		return errors.NewValidationError("directory_path", path, "目錄路徑不能為空")
	}

	path = strings.TrimSpace(path)

	// Check for null bytes and dangerous characters
	if strings.Contains(path, "\x00") {
		return errors.NewValidationError("directory_path", path, "路徑包含非法字符")
	}

	// Check length
	if len(path) > 4096 {
		return errors.NewValidationError("directory_path", path, "路徑過長")
	}

	return nil
}

// SanitizeString removes dangerous characters from string input
func (v *InputValidator) SanitizeString(input string) string {
	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")

	// Remove other control characters except tab, newline, and carriage return
	var result strings.Builder
	for _, r := range input {
		if !unicode.IsControl(r) || r == '\t' || r == '\n' || r == '\r' {
			result.WriteRune(r)
		}
	}

	return result.String()
}

// ValidateOutputFormat validates output format selection
func (v *InputValidator) ValidateOutputFormat(format string) error {
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

// ValidateCSVData validates CSV data structure
func (v *InputValidator) ValidateCSVData(records [][]string, filename string) error {
	if len(records) == 0 {
		return errors.NewValidationError("csv_data", len(records),
			"CSV 檔案為空")
	}

	if len(records) < 2 {
		return errors.NewValidationError("csv_data", len(records),
			"CSV 檔案至少需要包含標題行和一行資料")
	}

	// Validate header row
	if len(records[0]) == 0 {
		return errors.NewValidationError("csv_data", records[0],
			"CSV 標題行為空")
	}

	expectedColumns := len(records[0])

	// Validate data consistency
	for i, record := range records[1:] {
		if len(record) != expectedColumns {
			return errors.NewValidationError("csv_data",
				map[string]interface{}{
					"row":           i + 2,
					"expected_cols": expectedColumns,
					"actual_cols":   len(record),
					"filename":      filename,
				},
				fmt.Sprintf("第 %d 行的欄位數量不一致", i+2))
		}
	}

	return nil
}

// EmailPattern for email validation (if needed for future features)
var EmailPattern = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

// ValidateEmail validates email address format
func (v *InputValidator) ValidateEmail(email string) error {
	if email == "" {
		return errors.NewValidationError("email", email, "電子郵件地址不能為空")
	}

	email = strings.TrimSpace(email)

	if !EmailPattern.MatchString(email) {
		return errors.NewValidationError("email", email, "無效的電子郵件地址格式")
	}

	if len(email) > 254 {
		return errors.NewValidationError("email", email, "電子郵件地址過長")
	}

	return nil
}
