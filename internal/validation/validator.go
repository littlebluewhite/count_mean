package validation

import (
	"fmt"
	"strings"

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
	maxPathLength        = 4096
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

// ValidateCSVData validates CSV data structure and detects malicious content.
func (v *InputValidator) ValidateCSVData(records [][]string, fn string) error {
	if err := v.csvValidator.ValidateCSVData(records, fn); err != nil {
		return fmt.Errorf("CSV 資料驗證失敗: %w", err)
	}

	return nil
}

// ValidateCSVRow runs cell-level validation on a single CSV body row for streaming
// consumers that cannot materialize the entire [][]string in memory.
//
// 填補 ValidateCSVData 的 streaming-path 盲點。expectedColumns >= 0 時
// enforce row 欄位數；傳 -1 表示「caller 自己擋欄位數，這裡只做 cell 內容防護」
// （large_file_handler 已有 len(record) != len(headers) 檢查並選擇 skip-and-continue，
// 與 csv_handler bulk 路徑的 fail-fast 契約不一樣，因此 streaming 端傳 -1）。
//
// 本 API 跑「body row」full守門。header row 請改呼 ValidateCSVHeaderRow，
// 否則 EMG header（如 `Subject ID`、`Frame ID`、`Mid Foot`）會被 SQL/Command
// injection substring 比對誤判。
func (v *InputValidator) ValidateCSVRow(record []string, rowNum, expectedColumns int, fn string) error {
	if err := v.csvValidator.ValidateRow(record, rowNum, expectedColumns, fn); err != nil {
		return fmt.Errorf("CSV 資料驗證失敗: %w", err)
	}

	return nil
}

// ValidateCSVHeaderRow runs cell-level validation on a single CSV header row.
//
// 與 ValidateCSVRow 同 streaming 用途但專門給 header row。內部 scoped 跳過
// SQL / Command / DangerousFunctions 比對（這些規則 substring 比對對 EMG header
// 假陽性嚴重），但仍跑 formula starter / script injection / control char / UTF-8
// / suspicious extension 守門 — 後者在 Excel 開啟 header 時仍會 trigger。
func (v *InputValidator) ValidateCSVHeaderRow(record []string, rowNum, expectedColumns int, fn string) error {
	if err := v.csvValidator.ValidateHeaderRow(record, rowNum, expectedColumns, fn); err != nil {
		return fmt.Errorf("CSV 資料驗證失敗: %w", err)
	}

	return nil
}
