package io

import (
	"bufio"
	"count_mean/internal/config"
	"count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/security"
	"count_mean/internal/validation"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// CSVHandler 處理 CSV 檔案讀寫
type CSVHandler struct {
	config           *config.AppConfig
	pathValidator    *security.PathValidator
	validator        *validation.InputValidator
	logger           *logging.Logger
	largeFileHandler *LargeFileHandler
}

// NewCSVHandler 創建新的 CSV 處理器
func NewCSVHandler(config *config.AppConfig) *CSVHandler {
	// Initialize path validator with allowed directories
	allowedPaths := []string{
		config.InputDir,
		config.OutputDir,
		config.OperateDir,
	}

	return &CSVHandler{
		config:           config,
		pathValidator:    security.NewPathValidator(allowedPaths),
		validator:        validation.NewInputValidator(),
		logger:           logging.GetLogger("csv_handler"),
		largeFileHandler: NewLargeFileHandler(config),
	}
}

// BOMBytes UTF-8 BOM
var BOMBytes = []byte{0xEF, 0xBB, 0xBF}

// ListInputFiles 列出輸入目錄中的CSV文件
func (h *CSVHandler) ListInputFiles() ([]string, error) {
	files, err := os.ReadDir(h.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("無法讀取輸入目錄 %s: %w", h.config.InputDir, err)
	}

	var csvFiles []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".csv") {
			csvFiles = append(csvFiles, file.Name())
		}
	}

	return csvFiles, nil
}

// ListInputDirectories 列出輸入目錄中的子目錄
func (h *CSVHandler) ListInputDirectories() ([]string, error) {
	files, err := os.ReadDir(h.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("無法讀取輸入目錄 %s: %w", h.config.InputDir, err)
	}

	var directories []string
	for _, file := range files {
		if file.IsDir() {
			directories = append(directories, file.Name())
		}
	}

	return directories, nil
}

// ListCSVFilesInDirectory 列出指定目錄中的CSV文件
func (h *CSVHandler) ListCSVFilesInDirectory(dirName string) ([]string, error) {
	dirPath := filepath.Join(h.config.InputDir, dirName)
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("無法讀取目錄 %s: %w", dirPath, err)
	}

	var csvFiles []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".csv") {
			csvFiles = append(csvFiles, file.Name())
		}
	}

	return csvFiles, nil
}

// ReadCSVFromDirectory 從指定目錄讀取CSV檔案
func (h *CSVHandler) ReadCSVFromDirectory(dirName, fileName string) ([][]string, error) {
	// 如果沒有副檔名，添加.csv
	if !strings.HasSuffix(fileName, ".csv") {
		fileName += ".csv"
	}

	// 建構完整路徑
	fullPath := filepath.Join(h.config.InputDir, dirName, fileName)

	return h.ReadCSV(fullPath)
}

// WriteCSVToOutputDirectory 寫入CSV文件到輸出目錄的子目錄
func (h *CSVHandler) WriteCSVToOutputDirectory(dirName, filename string, data [][]string) error {
	// 確保輸出目錄存在
	outputDir := filepath.Join(h.config.OutputDir, dirName)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("無法創建輸出目錄: %w", err)
	}

	// 建構完整輸出路徑
	fullPath := filepath.Join(outputDir, filename)
	return h.WriteCSV(fullPath, data)
}

// ReadCSVFromPrompt 從使用者輸入讀取 CSV 檔案
func (h *CSVHandler) ReadCSVFromPrompt(prompt string) ([][]string, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	fileName, _ := reader.ReadString('\n')
	fileName = strings.TrimSpace(fileName)

	// 如果沒有副檔名，添加.csv
	if !strings.HasSuffix(fileName, ".csv") {
		fileName += ".csv"
	}

	// 建構完整路徑
	fullPath := filepath.Join(h.config.InputDir, fileName)

	return h.ReadCSV(fullPath)
}

// ReadCSVFromPromptWithName 從使用者輸入讀取 CSV 檔案並返回檔名
func (h *CSVHandler) ReadCSVFromPromptWithName(prompt string) ([][]string, string, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	fileName, _ := reader.ReadString('\n')
	fileName = strings.TrimSpace(fileName)

	// 保存原始檔名（不含副檔名）
	originalName := fileName

	// 如果沒有副檔名，添加.csv
	if !strings.HasSuffix(fileName, ".csv") {
		fileName += ".csv"
	} else {
		// 如果已有.csv副檔名，移除它以獲得原始名稱
		originalName = strings.TrimSuffix(fileName, ".csv")
	}

	// 建構完整路徑
	fullPath := filepath.Join(h.config.InputDir, fileName)

	records, err := h.ReadCSV(fullPath)
	return records, originalName, err
}

// ReadCSVFromInput 從輸入目錄讀取CSV檔案
func (h *CSVHandler) ReadCSVFromInput(filename string) ([][]string, error) {
	// 使用安全路徑構建
	fullPath, err := h.pathValidator.GetSafePath(h.config.InputDir, filename)
	if err != nil {
		return nil, fmt.Errorf("無法構建安全路徑: %w", err)
	}
	return h.ReadCSV(fullPath)
}

// ReadCSVExternal 讀取外部 CSV 檔案（跳過路徑驗證，用於批量處理外部目錄）
func (h *CSVHandler) ReadCSVExternal(filename string) ([][]string, error) {
	h.logger.Debug("開始讀取外部 CSV 檔案", map[string]interface{}{
		"filename": filename,
	})

	// 檢查文件大小並決定處理方式
	fileInfo, err := h.largeFileHandler.GetFileInfo(filename)
	if err != nil {
		// 如果無法獲取文件信息，使用傳統方式
		h.logger.Warn("無法獲取文件信息，使用傳統讀取方式", map[string]interface{}{
			"filename": filename,
			"error":    err.Error(),
		})
	} else if fileInfo.IsLarge {
		h.logger.Info("檢測到大文件，使用流式讀取", map[string]interface{}{
			"filename":   filename,
			"file_size":  fileInfo.Size,
			"line_count": fileInfo.LineCount,
		})
		// 對於大文件，返回錯誤提示用戶使用專門的大文件處理方法
		return nil, errors.NewAppErrorWithDetails(
			errors.ErrCodeFileTooLarge,
			"文件過大，請使用大文件處理功能",
			fmt.Sprintf("文件 %s 過大 (%d bytes)，建議使用流式處理", filename, fileInfo.Size),
		)
	}

	// 清理路徑（基本清理，不進行路徑驗證）
	cleanPath := filepath.Clean(filename)

	// Check if it's a CSV file
	if !strings.HasSuffix(strings.ToLower(cleanPath), ".csv") {
		err := errors.NewAppErrorWithDetails(
			errors.ErrCodeFileFormat,
			"檔案格式無效",
			fmt.Sprintf("檔案 '%s' 不是有效的 CSV 檔案", cleanPath),
		)
		h.logger.Error("檔案格式驗證失敗", err, map[string]interface{}{
			"path": cleanPath,
		})
		return nil, err
	}

	file, err := os.Open(cleanPath)
	if err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeFileNotFound, "無法開啟檔案")
		h.logger.Error("檔案開啟失敗", appErr, map[string]interface{}{
			"path": cleanPath,
		})
		return nil, appErr
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉檔案時發生錯誤", map[string]interface{}{
				"file":  file.Name(),
				"error": closeErr.Error(),
			})
		}
	}()

	r := csv.NewReader(file)
	records, err := r.ReadAll()
	if err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeDataParsing, "無法讀取 CSV 資料")
		h.logger.Error("CSV 資料讀取失敗", appErr, map[string]interface{}{
			"path": cleanPath,
		})
		return nil, appErr
	}

	// 驗證數據
	if len(records) < 2 {
		err := errors.NewAppErrorWithDetails(
			errors.ErrCodeInsufficientData,
			"資料不足",
			"檔案至少需要包含標題行和一行數據",
		)
		h.logger.Error("CSV 資料驗證失敗", err, map[string]interface{}{
			"path":         cleanPath,
			"record_count": len(records),
		})
		return nil, err
	}

	// 驗證 CSV 資料結構
	if err := h.validator.ValidateCSVData(records, cleanPath); err != nil {
		h.logger.Error("CSV 資料結構驗證失敗", err, map[string]interface{}{
			"path": cleanPath,
		})
		return nil, err
	}

	h.logger.Info("外部 CSV 檔案讀取成功", map[string]interface{}{
		"path":         cleanPath,
		"record_count": len(records),
		"column_count": len(records[0]),
	})

	return records, nil
}

// ReadCSV 讀取 CSV 檔案（自動檢測大文件並使用相應處理方式）
func (h *CSVHandler) ReadCSV(filename string) ([][]string, error) {
	h.logger.Debug("開始讀取 CSV 檔案", map[string]interface{}{
		"filename": filename,
	})

	// 檢查文件大小並決定處理方式
	fileInfo, err := h.largeFileHandler.GetFileInfo(filename)
	if err != nil {
		// 如果無法獲取文件信息，使用傳統方式
		h.logger.Warn("無法獲取文件信息，使用傳統讀取方式", map[string]interface{}{
			"filename": filename,
			"error":    err.Error(),
		})
	} else if fileInfo.IsLarge {
		h.logger.Info("檢測到大文件，使用流式讀取", map[string]interface{}{
			"filename":   filename,
			"file_size":  fileInfo.Size,
			"line_count": fileInfo.LineCount,
		})
		// 對於大文件，返回錯誤提示用戶使用專門的大文件處理方法
		return nil, errors.NewAppErrorWithDetails(
			errors.ErrCodeFileTooLarge,
			"文件過大，請使用大文件處理功能",
			fmt.Sprintf("文件 %s 過大 (%d bytes)，建議使用流式處理", filename, fileInfo.Size),
		)
	}

	// Validate and sanitize the file path
	sanitizedPath := h.pathValidator.SanitizePath(filename)
	if err := h.pathValidator.ValidateFilePath(sanitizedPath); err != nil {
		h.logger.Error("路徑驗證失敗", err, map[string]interface{}{
			"original_path":  filename,
			"sanitized_path": sanitizedPath,
		})
		return nil, errors.WrapError(err, errors.ErrCodePathValidation, "路徑驗證失敗")
	}

	// Check if it's a CSV file
	if !h.pathValidator.IsCSVFile(sanitizedPath) {
		err := errors.NewAppErrorWithDetails(
			errors.ErrCodeFileFormat,
			"檔案格式無效",
			fmt.Sprintf("檔案 '%s' 不是有效的 CSV 檔案", sanitizedPath),
		)
		h.logger.Error("檔案格式驗證失敗", err, map[string]interface{}{
			"path": sanitizedPath,
		})
		return nil, err
	}

	file, err := os.Open(sanitizedPath)
	if err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeFileNotFound, "無法開啟檔案")
		h.logger.Error("檔案開啟失敗", appErr, map[string]interface{}{
			"path": sanitizedPath,
		})
		return nil, appErr
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉檔案時發生錯誤", map[string]interface{}{
				"file":  file.Name(),
				"error": closeErr.Error(),
			})
		}
	}()

	r := csv.NewReader(file)
	records, err := r.ReadAll()
	if err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeDataParsing, "無法讀取 CSV 資料")
		h.logger.Error("CSV 資料讀取失敗", appErr, map[string]interface{}{
			"path": sanitizedPath,
		})
		return nil, appErr
	}

	// 驗證數據
	if len(records) < 2 {
		err := errors.NewAppErrorWithDetails(
			errors.ErrCodeInsufficientData,
			"資料不足",
			"檔案至少需要包含標題行和一行數據",
		)
		h.logger.Error("CSV 資料驗證失敗", err, map[string]interface{}{
			"path":         sanitizedPath,
			"record_count": len(records),
		})
		return nil, err
	}

	// 驗證 CSV 資料結構
	if err := h.validator.ValidateCSVData(records, sanitizedPath); err != nil {
		h.logger.Error("CSV 資料結構驗證失敗", err, map[string]interface{}{
			"path": sanitizedPath,
		})
		return nil, err
	}

	h.logger.Info("CSV 檔案讀取成功", map[string]interface{}{
		"path":         sanitizedPath,
		"record_count": len(records),
		"column_count": len(records[0]),
	})

	return records, nil
}

// WriteCSVToOutput 寫入CSV文件到輸出目錄
func (h *CSVHandler) WriteCSVToOutput(filename string, data [][]string) error {
	// 確保輸出目錄存在
	if err := os.MkdirAll(h.config.OutputDir, 0755); err != nil {
		return fmt.Errorf("無法創建輸出目錄: %w", err)
	}

	// 使用安全路徑構建
	fullPath, err := h.pathValidator.GetSafePath(h.config.OutputDir, filename)
	if err != nil {
		return fmt.Errorf("無法構建安全輸出路徑: %w", err)
	}
	return h.WriteCSV(fullPath, data)
}

// WriteCSV 寫入 CSV 檔案
func (h *CSVHandler) WriteCSV(filename string, data [][]string) error {
	h.logger.Debug("開始寫入 CSV 檔案", map[string]interface{}{
		"filename":    filename,
		"row_count":   len(data),
		"bom_enabled": h.config.BOMEnabled,
	})

	// Validate and sanitize the file path
	sanitizedPath := h.pathValidator.SanitizePath(filename)
	if err := h.pathValidator.ValidateFilePath(sanitizedPath); err != nil {
		h.logger.Error("寫入路徑驗證失敗", err, map[string]interface{}{
			"original_path":  filename,
			"sanitized_path": sanitizedPath,
		})
		return fmt.Errorf("路徑驗證失敗: %w", err)
	}

	// Check if it's a CSV file
	if !h.pathValidator.IsCSVFile(sanitizedPath) {
		err := fmt.Errorf("檔案 '%s' 不是有效的 CSV 檔案", sanitizedPath)
		h.logger.Error("檔案格式驗證失敗", err, map[string]interface{}{
			"path": sanitizedPath,
		})
		return err
	}

	file, err := os.Create(sanitizedPath)
	if err != nil {
		h.logger.Error("無法建立輸出檔案", err, map[string]interface{}{
			"path": sanitizedPath,
		})
		return fmt.Errorf("無法建立檔案 %s: %w", sanitizedPath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉輸出檔案時發生錯誤", map[string]interface{}{
				"file":  file.Name(),
				"error": closeErr.Error(),
			})
		}
	}()

	// 寫入 BOM（如果啟用）
	if h.config.BOMEnabled {
		if _, err := file.Write(BOMBytes); err != nil {
			return fmt.Errorf("無法寫入 BOM 到 %s: %w", filename, err)
		}
	}

	w := csv.NewWriter(file)
	if err := w.WriteAll(data); err != nil {
		h.logger.Error("CSV 資料寫入失敗", err, map[string]interface{}{
			"path":     sanitizedPath,
			"filename": filename,
		})
		return fmt.Errorf("無法寫入資料到 %s: %w", filename, err)
	}

	h.logger.Info("CSV 檔案寫入成功", map[string]interface{}{
		"path":      sanitizedPath,
		"row_count": len(data),
		"bom_used":  h.config.BOMEnabled,
	})

	return nil
}

// ConvertMaxMeanResultsToCSV 將最大平均值結果轉換為 CSV 格式
func (h *CSVHandler) ConvertMaxMeanResultsToCSV(headers []string, results []models.MaxMeanResult, startRange, endRange float64) [][]string {
	data := make([][]string, 0, 6)

	// 添加標題
	data = append(data, headers)

	// 創建結果行
	startRangeTimes := make([]string, 1, len(headers))
	endRangeTimes := make([]string, 1, len(headers))
	startTimes := make([]string, 1, len(headers))
	endTimes := make([]string, 1, len(headers))
	maxMeans := make([]string, 1, len(headers))

	startRangeTimes[0] = "開始範圍秒數"
	endRangeTimes[0] = "結束範圍秒數"
	startTimes[0] = "開始計算秒數"
	endTimes[0] = "結束計算秒數"
	maxMeans[0] = "最大平均值"

	// 填充結果
	for _, result := range results {
		precision := fmt.Sprintf("%%.%df", h.config.Precision)

		startRangeTimes = append(startRangeTimes, fmt.Sprintf(precision, startRange))
		endRangeTimes = append(endRangeTimes, fmt.Sprintf(precision, endRange))
		startTimes = append(startTimes, fmt.Sprintf(precision, result.StartTime/math.Pow10(h.config.ScalingFactor)))
		endTimes = append(endTimes, fmt.Sprintf(precision, result.EndTime/math.Pow10(h.config.ScalingFactor)))
		maxMeans = append(maxMeans, fmt.Sprintf(precision, result.MaxMean/math.Pow10(h.config.ScalingFactor)))
	}

	data = append(data, startRangeTimes)
	data = append(data, endRangeTimes)
	data = append(data, startTimes)
	data = append(data, endTimes)
	data = append(data, maxMeans)

	return data
}

// ConvertNormalizedDataToCSV 將標準化數據轉換為 CSV 格式
func (h *CSVHandler) ConvertNormalizedDataToCSV(dataset *models.EMGDataset) [][]string {
	data := make([][]string, 0, len(dataset.Data)+1)

	// 添加標題
	data = append(data, dataset.Headers)

	// 時間列使用原始精度，數據列使用配置精度
	timePrecision := fmt.Sprintf("%%.%df", dataset.OriginalTimePrecision)
	dataPrecision := fmt.Sprintf("%%.%df", h.config.Precision)

	for _, emgData := range dataset.Data {
		row := make([]string, 0, len(dataset.Headers))

		// 時間列 - 使用原始檔案的時間精度
		row = append(row, fmt.Sprintf(timePrecision, emgData.Time/math.Pow10(h.config.ScalingFactor)))

		// 數據列 - 使用配置的精度
		for _, val := range emgData.Channels {
			row = append(row, fmt.Sprintf(dataPrecision, val))
		}

		data = append(data, row)
	}

	return data
}

// ConvertPhaseAnalysisToCSV 將階段分析結果轉換為 CSV 格式
func (h *CSVHandler) ConvertPhaseAnalysisToCSV(headers []string, result *models.PhaseAnalysisResult, maxTimeIndex map[int]float64) [][]string {
	data := make([][]string, 0, len(result.MaxValues)+len(result.MeanValues)+2)

	// 添加標題
	data = append(data, headers)

	precision := fmt.Sprintf("%%.%df", h.config.Precision)
	scalingFactor := float64(h.config.ScalingFactor)

	// 最大值行
	for i, phaseResult := range []models.PhaseAnalysisResult{*result} {
		maxRow := make([]string, 1, len(headers))
		maxRow[0] = phaseResult.PhaseName + " 最大值"

		for j := 1; j < len(headers); j++ {
			channelIdx := j - 1
			if maxVal, exists := phaseResult.MaxValues[channelIdx]; exists {
				maxRow = append(maxRow, fmt.Sprintf(precision, maxVal/math.Pow10(int(scalingFactor))))
			} else {
				maxRow = append(maxRow, "N/A")
			}
		}
		data = append(data, maxRow)

		// 平均值行
		meanRow := make([]string, 1, len(headers))
		meanRow[0] = phaseResult.PhaseName + " 平均值"

		for j := 1; j < len(headers); j++ {
			channelIdx := j - 1
			if meanVal, exists := phaseResult.MeanValues[channelIdx]; exists {
				meanRow = append(meanRow, fmt.Sprintf(precision, meanVal/math.Pow10(int(scalingFactor))))
			} else {
				meanRow = append(meanRow, "N/A")
			}
		}
		data = append(data, meanRow)

		// 只處理第一個結果（這個函數設計為處理單個階段）
		if i == 0 {
			break
		}
	}

	// 最大值時間行
	if len(maxTimeIndex) > 0 {
		timeRow := make([]string, 1, len(headers))
		timeRow[0] = "整個階段最大值出現在_秒"

		for j := 1; j < len(headers); j++ {
			channelIdx := j - 1
			if timeVal, exists := maxTimeIndex[channelIdx]; exists {
				timeRow = append(timeRow, fmt.Sprintf(fmt.Sprintf("%%.%df", h.config.Precision), timeVal/math.Pow10(h.config.ScalingFactor)))
			} else {
				timeRow = append(timeRow, "N/A")
			}
		}
		data = append(data, timeRow)
	}

	return data
}

// GetFileInfo 獲取文件信息
func (h *CSVHandler) GetFileInfo(filename string) (*FileInfo, error) {
	return h.largeFileHandler.GetFileInfo(filename)
}

// ProcessLargeFile 處理大文件
func (h *CSVHandler) ProcessLargeFile(filename string, windowSize int, callback ProgressCallback) (*StreamingResult, error) {
	h.logger.Info("開始處理大文件", map[string]interface{}{
		"filename":    filename,
		"window_size": windowSize,
	})

	return h.largeFileHandler.ProcessLargeFileInChunks(filename, windowSize, callback)
}

// ReadLargeCSVStreaming 流式讀取大 CSV 文件
func (h *CSVHandler) ReadLargeCSVStreaming(filename string, callback ProgressCallback) (*StreamingResult, error) {
	h.logger.Info("開始流式讀取大 CSV 文件", map[string]interface{}{
		"filename": filename,
	})

	return h.largeFileHandler.ReadCSVStreaming(filename, callback)
}

// WriteLargeCSVStreaming 流式寫入大 CSV 文件
func (h *CSVHandler) WriteLargeCSVStreaming(filename string, data [][]string, callback ProgressCallback) error {
	h.logger.Info("開始流式寫入大 CSV 文件", map[string]interface{}{
		"filename":  filename,
		"row_count": len(data),
	})

	return h.largeFileHandler.WriteCSVStreaming(filename, data, callback)
}

// detectTimePrecision 檢測時間欄位的小數位數
func (h *CSVHandler) detectTimePrecision(records [][]string) int {
	if len(records) < 2 {
		return 2 // 預設精度
	}

	maxPrecision := 0
	// 檢查前幾行數據來確定時間精度
	for i := 1; i < len(records) && i <= 10; i++ {
		if len(records[i]) > 0 {
			timeStr := records[i][0]
			precision := h.getDecimalPrecision(timeStr)
			if precision > maxPrecision {
				maxPrecision = precision
			}
		}
	}

	// 如果檢測不到小數位數，預設為 2
	if maxPrecision == 0 {
		maxPrecision = 2
	}

	return maxPrecision
}

// getDecimalPrecision 獲取字串中小數點後的位數
func (h *CSVHandler) getDecimalPrecision(numStr string) int {
	// 移除空白字元
	numStr = strings.TrimSpace(numStr)

	// 找到小數點位置
	dotIndex := strings.Index(numStr, ".")
	if dotIndex == -1 {
		return 0 // 沒有小數點
	}

	// 計算小數點後的位數
	return len(numStr) - dotIndex - 1
}
