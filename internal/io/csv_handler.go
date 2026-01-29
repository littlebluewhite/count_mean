// Package io provides file input/output operations for the EMG data analysis
// application, including CSV reading, writing, and streaming support for large files.
package io

import (
	"bufio"
	"encoding/csv"
	stderrors "errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"count_mean/internal/config"
	"count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/security"
	"count_mean/internal/validation"
)

// File permission constants.
const (
	dirPermission     = 0o750 // Directory permission mode.
	csvFilePermission = 0o600 // File permission mode.
)

// Static errors for err113 compliance.
var errInvalidCSVFile = stderrors.New("不是有效的 CSV 檔案")

// CSVHandler 處理 CSV 檔案讀寫.
type CSVHandler struct {
	config           *config.AppConfig
	pathValidator    *security.PathValidator
	validator        *validation.InputValidator
	logger           *logging.Logger
	largeFileHandler *LargeFileHandler
	pathBuilder      *FilePathBuilder
	converter        *CSVConverter
}

// NewCSVHandler 創建新的 CSV 處理器.
func NewCSVHandler(cfg *config.AppConfig) *CSVHandler {
	// Initialize path validator with allowed directories
	allowedPaths := []string{
		cfg.InputDir,
		cfg.OutputDir,
		cfg.OperateDir,
	}

	pathValidator := security.NewPathValidator(allowedPaths)
	scalingMultiplier := math.Pow10(cfg.ScalingFactor)

	return &CSVHandler{
		config:           cfg,
		pathValidator:    pathValidator,
		validator:        validation.NewInputValidator(),
		logger:           logging.GetLogger("csv_handler"),
		largeFileHandler: NewLargeFileHandler(cfg),
		pathBuilder:      NewFilePathBuilder(cfg, pathValidator),
		converter:        NewCSVConverter(scalingMultiplier, cfg.Precision),
	}
}

// BOMBytes UTF-8 BOM.
//
//nolint:gochecknoglobals // BOMBytes is a constant-like byte slice for UTF-8 BOM
var BOMBytes = []byte{0xEF, 0xBB, 0xBF}

// listOptions specifies options for listing directory entries.
type listOptions struct {
	dirPath       string
	filesOnly     bool
	dirsOnly      bool
	csvFilesOnly  bool
	errorMsgParam string
}

// listEntries lists directory entries based on the given options.
func (*CSVHandler) listEntries(opts listOptions) ([]string, error) {
	files, err := os.ReadDir(opts.dirPath)
	if err != nil {
		return nil, fmt.Errorf("無法讀取%s %s: %w", opts.errorMsgParam, opts.dirPath, err)
	}

	var result []string

	for _, file := range files {
		result = appendEntryIfMatches(result, file, opts)
	}

	return result, nil
}

// appendEntryIfMatches appends the file name to result if it matches the options.
func appendEntryIfMatches(result []string, file os.DirEntry, opts listOptions) []string {
	if opts.dirsOnly && file.IsDir() {
		return append(result, file.Name())
	}

	if opts.filesOnly && !file.IsDir() {
		return appendFileIfMatches(result, file, opts)
	}

	return result
}

// appendFileIfMatches appends the file name to result if it matches CSV filter options.
func appendFileIfMatches(result []string, file os.DirEntry, opts listOptions) []string {
	if !opts.csvFilesOnly {
		return append(result, file.Name())
	}

	if strings.HasSuffix(strings.ToLower(file.Name()), ".csv") {
		return append(result, file.Name())
	}

	return result
}

// ListInputFiles 列出輸入目錄中的CSV文件.
func (h *CSVHandler) ListInputFiles() ([]string, error) {
	return h.listEntries(listOptions{
		dirPath:       h.config.InputDir,
		filesOnly:     true,
		csvFilesOnly:  true,
		errorMsgParam: "輸入目錄",
	})
}

// ListInputDirectories 列出輸入目錄中的子目錄.
func (h *CSVHandler) ListInputDirectories() ([]string, error) {
	return h.listEntries(listOptions{
		dirPath:       h.config.InputDir,
		dirsOnly:      true,
		errorMsgParam: "輸入目錄",
	})
}

// ListCSVFilesInDirectory 列出指定目錄中的CSV文件.
func (h *CSVHandler) ListCSVFilesInDirectory(dirName string) ([]string, error) {
	dirPath := filepath.Join(h.config.InputDir, dirName)

	return h.listEntries(listOptions{
		dirPath:       dirPath,
		filesOnly:     true,
		csvFilesOnly:  true,
		errorMsgParam: "目錄",
	})
}

// ReadCSVFromDirectory 從指定目錄讀取CSV檔案.
func (h *CSVHandler) ReadCSVFromDirectory(dirName, fileName string) ([][]string, error) {
	fileName = h.pathBuilder.EnsureCSVExtension(fileName)
	fullPath := filepath.Join(h.config.InputDir, dirName, fileName)

	return h.ReadCSV(fullPath)
}

// WriteCSVToOutputDirectory 寫入CSV文件到輸出目錄的子目錄.
func (h *CSVHandler) WriteCSVToOutputDirectory(dirName, filename string, data [][]string) error {
	outputDir := filepath.Join(h.config.OutputDir, dirName)
	if err := os.MkdirAll(outputDir, dirPermission); err != nil {
		return fmt.Errorf("無法創建輸出目錄: %w", err)
	}

	fullPath := filepath.Join(outputDir, filename)

	return h.WriteCSV(fullPath, data)
}

// ReadCSVFromPrompt 從使用者輸入讀取 CSV 檔案.
func (h *CSVHandler) ReadCSVFromPrompt(prompt string) ([][]string, error) {
	if _, err := fmt.Fprint(os.Stdout, prompt); err != nil {
		return nil, fmt.Errorf("無法輸出提示: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)

	fileName, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("無法讀取使用者輸入: %w", err)
	}

	fileName = strings.TrimSpace(fileName)
	fileName = h.pathBuilder.EnsureCSVExtension(fileName)
	fullPath := filepath.Join(h.config.InputDir, fileName)

	return h.ReadCSV(fullPath)
}

// ReadCSVFromPromptWithName 從使用者輸入讀取 CSV 檔案並返回檔名.
func (h *CSVHandler) ReadCSVFromPromptWithName(prompt string) ([][]string, string, error) {
	if _, err := fmt.Fprint(os.Stdout, prompt); err != nil {
		return nil, "", fmt.Errorf("無法輸出提示: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)

	fileName, err := reader.ReadString('\n')
	if err != nil {
		return nil, "", fmt.Errorf("無法讀取使用者輸入: %w", err)
	}

	fileName = strings.TrimSpace(fileName)

	originalName := h.pathBuilder.StripCSVExtension(fileName)
	fileName = h.pathBuilder.EnsureCSVExtension(fileName)
	fullPath := filepath.Join(h.config.InputDir, fileName)

	records, err := h.ReadCSV(fullPath)

	return records, originalName, err
}

// ReadCSVFromInput 從輸入目錄讀取CSV檔案.
func (h *CSVHandler) ReadCSVFromInput(filename string) ([][]string, error) {
	fullPath, err := h.pathValidator.GetSafePath(h.config.InputDir, filename)
	if err != nil {
		return nil, fmt.Errorf("無法構建安全路徑: %w", err)
	}

	return h.ReadCSV(fullPath)
}

// readOptions specifies options for reading CSV files.
type readOptions struct {
	logPrefix string
}

// checkFileSizeAndFormat validates file size and format before reading.
func (h *CSVHandler) checkFileSizeAndFormat(filename string, opts readOptions) (string, error) {
	fileInfo, err := h.largeFileHandler.GetFileInfo(filename)
	if err != nil {
		h.logger.Error(opts.logPrefix+"檔案路徑驗證失敗", err, map[string]interface{}{"filename": filename})

		return "", err
	}

	if fileInfo.IsLarge {
		h.logger.Info("檢測到大文件，使用流式讀取", map[string]interface{}{
			"filename": filename, "file_size": fileInfo.Size, "line_count": fileInfo.LineCount,
		})

		return "", errors.NewAppErrorWithDetails(
			errors.ErrCodeFileTooLarge, "文件過大，請使用大文件處理功能",
			fmt.Sprintf("文件 %s 過大 (%d bytes)，建議使用流式處理", filename, fileInfo.Size),
		)
	}

	cleanPath := fileInfo.Path

	if !h.isCSVFile(cleanPath) {
		err := errors.NewAppErrorWithDetails(
			errors.ErrCodeFileFormat, "檔案格式無效",
			fmt.Sprintf("檔案 '%s' 不是有效的 CSV 檔案", cleanPath),
		)
		h.logger.Error("檔案格式驗證失敗", err, map[string]interface{}{"path": cleanPath})

		return "", err
	}

	return cleanPath, nil
}

// isCSVFile checks if the file has a CSV extension.
func (h *CSVHandler) isCSVFile(path string) bool {
	return h.pathValidator.IsCSVFile(path)
}

// readAndParseCSV opens and parses a CSV file.
func (h *CSVHandler) readAndParseCSV(cleanPath string) ([][]string, error) {
	file, err := os.Open(cleanPath) //nolint:gosec // cleanPath is sanitized and validated
	if err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeFileNotFound, "無法開啟檔案")
		h.logger.Error("檔案開啟失敗", appErr, map[string]interface{}{"path": cleanPath})

		return nil, appErr
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉檔案時發生錯誤", map[string]interface{}{
				"file": file.Name(), "error": closeErr.Error(),
			})
		}
	}()

	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeDataParsing, "無法讀取 CSV 資料")
		h.logger.Error("CSV 資料讀取失敗", appErr, map[string]interface{}{"path": cleanPath})

		return nil, appErr
	}

	return records, nil
}

// validateCSVRecords validates CSV records have sufficient data.
func (h *CSVHandler) validateCSVRecords(records [][]string, cleanPath string) error {
	if len(records) < 2 {
		err := errors.NewAppErrorWithDetails(
			errors.ErrCodeInsufficientData, "資料不足", "檔案至少需要包含標題行和一行數據",
		)
		h.logger.Error("CSV 資料驗證失敗", err, map[string]interface{}{
			"path": cleanPath, "record_count": len(records),
		})

		return err
	}

	if err := h.validator.ValidateCSVData(records, cleanPath); err != nil {
		h.logger.Error("CSV 資料結構驗證失敗", err, map[string]interface{}{"path": cleanPath})

		return fmt.Errorf("CSV 資料驗證失敗: %w", err)
	}

	return nil
}

// readCSVCore is the internal method that handles CSV reading logic.
func (h *CSVHandler) readCSVCore(filename string, opts readOptions) ([][]string, error) {
	h.logger.Debug("開始讀取"+opts.logPrefix+" CSV 檔案", map[string]interface{}{"filename": filename})

	cleanPath, err := h.checkFileSizeAndFormat(filename, opts)
	if err != nil {
		return nil, err
	}

	records, err := h.readAndParseCSV(cleanPath)
	if err != nil {
		return nil, err
	}

	if err := h.validateCSVRecords(records, cleanPath); err != nil {
		return nil, err
	}

	h.logger.Info(opts.logPrefix+"CSV 檔案讀取成功", map[string]interface{}{
		"path": cleanPath, "record_count": len(records), "column_count": len(records[0]),
	})

	return records, nil
}

// ReadCSVExternal 讀取外部 CSV 檔案（添加基本路徑驗證以提升安全性）.
func (h *CSVHandler) ReadCSVExternal(filename string) ([][]string, error) {
	return h.readCSVCore(filename, readOptions{
		logPrefix: "外部 ",
	})
}

// ReadCSV 讀取 CSV 檔案（自動檢測大文件並使用相應處理方式）.
func (h *CSVHandler) ReadCSV(filename string) ([][]string, error) {
	return h.readCSVCore(filename, readOptions{
		logPrefix: "",
	})
}

// WriteCSVToOutput 寫入CSV文件到輸出目錄.
func (h *CSVHandler) WriteCSVToOutput(filename string, data [][]string) error {
	if err := os.MkdirAll(h.config.OutputDir, dirPermission); err != nil {
		return fmt.Errorf("無法創建輸出目錄: %w", err)
	}

	fullPath, err := h.pathValidator.GetSafePath(h.config.OutputDir, filename)
	if err != nil {
		return fmt.Errorf("無法構建安全輸出路徑: %w", err)
	}

	return h.WriteCSV(fullPath, data)
}

// WriteCSV 寫入 CSV 檔案.
func (h *CSVHandler) WriteCSV(filename string, data [][]string) error {
	h.logger.Debug("開始寫入 CSV 檔案", map[string]interface{}{
		"filename":    filename,
		"row_count":   len(data),
		"bom_enabled": h.config.BOMEnabled,
	})

	sanitizedPath := h.pathValidator.SanitizePath(filename)
	if err := h.pathValidator.ValidateFilePath(sanitizedPath); err != nil {
		h.logger.Error("寫入路徑驗證失敗", err, map[string]interface{}{
			"original_path":  filename,
			"sanitized_path": sanitizedPath,
		})

		return fmt.Errorf("路徑驗證失敗: %w", err)
	}

	if !h.pathValidator.IsCSVFile(sanitizedPath) {
		err := fmt.Errorf("檔案 '%s': %w", sanitizedPath, errInvalidCSVFile)
		h.logger.Error("檔案格式驗證失敗", err, map[string]interface{}{
			"path": sanitizedPath,
		})

		return err
	}

	if _, err := os.Stat(sanitizedPath); err == nil {
		h.logger.Warn("檔案已存在，將被覆蓋", map[string]interface{}{
			"path": sanitizedPath,
		})
	}

	//nolint:gosec // sanitizedPath is sanitized and validated
	file, err := os.OpenFile(sanitizedPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, csvFilePermission)
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

// ConvertMaxMeanResultsToCSV 將最大平均值結果轉換為 CSV 格式.
func (h *CSVHandler) ConvertMaxMeanResultsToCSV(
	headers []string,
	results []models.MaxMeanResult,
	startRange, endRange float64,
) [][]string {
	return h.converter.ConvertMaxMeanResults(headers, results, startRange, endRange)
}

// ConvertNormalizedDataToCSV 將標準化數據轉換為 CSV 格式.
func (h *CSVHandler) ConvertNormalizedDataToCSV(dataset *models.EMGDataset) [][]string {
	return h.converter.ConvertNormalizedData(dataset)
}

// ConvertPhaseAnalysisToCSV 將階段分析結果轉換為 CSV 格式.
func (h *CSVHandler) ConvertPhaseAnalysisToCSV(
	headers []string,
	result *models.PhaseAnalysisResult,
	maxTimeIndex map[int]float64,
) [][]string {
	return h.converter.ConvertPhaseAnalysis(headers, result, maxTimeIndex)
}

// GetFileInfo 獲取文件信息.
func (h *CSVHandler) GetFileInfo(filename string) (*FileInfo, error) {
	return h.largeFileHandler.GetFileInfo(filename)
}

// ProcessLargeFile 處理大文件.
func (h *CSVHandler) ProcessLargeFile(
	filename string,
	windowSize int,
	callback ProgressCallback,
) (*StreamingResult, error) {
	h.logger.Info("開始處理大文件", map[string]interface{}{
		"filename":    filename,
		"window_size": windowSize,
	})

	return h.largeFileHandler.ProcessLargeFileInChunks(filename, windowSize, callback)
}

// ReadLargeCSVStreaming 流式讀取大 CSV 文件.
func (h *CSVHandler) ReadLargeCSVStreaming(filename string, callback ProgressCallback) (*StreamingResult, error) {
	h.logger.Info("開始流式讀取大 CSV 文件", map[string]interface{}{
		"filename": filename,
	})

	return h.largeFileHandler.ReadCSVStreaming(filename, callback)
}

// WriteLargeCSVStreaming 流式寫入大 CSV 文件.
func (h *CSVHandler) WriteLargeCSVStreaming(filename string, data [][]string, callback ProgressCallback) error {
	h.logger.Info("開始流式寫入大 CSV 文件", map[string]interface{}{
		"filename":  filename,
		"row_count": len(data),
	})

	return h.largeFileHandler.WriteCSVStreaming(filename, data, callback)
}
