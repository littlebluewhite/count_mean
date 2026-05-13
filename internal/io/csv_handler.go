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
	"count_mean/internal/csvutil"
	"count_mean/internal/security/fsperm"
)

// Static errors for err113 compliance.
var errInvalidCSVFile = stderrors.New("不是有效的 CSV 檔案")

// csvReaderBufSize 是 bufio.Reader 的初始 buffer 大小（64 KiB）。
// 與 internal/parsers 套件的同名常數對齊；保留 unexported 在各套件各自定義，
// 避免讓 io 套件直接 reach into parsers 內部。
const csvReaderBufSize = 64 * 1024

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
	if err := os.MkdirAll(outputDir, fsperm.DirPerm); err != nil {
		return fmt.Errorf("無法創建輸出目錄: %w", err)
	}

	fullPath := filepath.Join(outputDir, filename)

	return h.WriteCSV(fullPath, data)
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
//
// external 區分兩種來源：false 走嚴格 allowedBasePaths 白名單（內部設定路徑），
// true 走 lenient performBasicSecurityChecks（使用者透過 file-dialog 選的任意檔
// — 仍會擋 /etc、/root、C:\Windows 等系統敏感路徑與 path traversal）。
type readOptions struct {
	logPrefix string
	external  bool
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
	file, err := os.OpenFile(cleanPath, fsperm.ReadFlags, 0) //nolint:gosec // cleanPath sanitized and validated; fsperm.ReadFlags adds O_NOFOLLOW (symmetric with WriteFlags)
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

	// 用 bufio 包 *os.File 避免 csv.Reader 每次 Read 都觸發 syscall（大檔差異明顯）。
	// BOM 處理：Excel 匯出的 UTF-8 CSV 常帶 0xEF 0xBB 0xBF 前綴。若不剝除，
	// records[0][0] 會帶 U+FEFF，會在 GetCSVHeaders 直接回前端時造成 user-visible
	// 怪字元，並污染後續以 header 字串比對 channel 名稱的路徑。
	// 與 internal/parsers/csv_reader.go 對稱：先 bufio.NewReaderSize 再 PeekBOM
	// 再 csv.NewReader（PeekBOM 在 < 3 bytes 輸入時視為「無 BOM」回 nil，
	// 不會把空檔的 EOF 提前丟出）。
	bufReader := bufio.NewReaderSize(file, csvReaderBufSize)
	if _, err := csvutil.PeekBOM(bufReader); err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeDataParsing, "BOM 偵測失敗")
		h.logger.Error("BOM 偵測失敗", appErr, map[string]interface{}{"path": cleanPath})

		return nil, appErr
	}

	records, err := csv.NewReader(bufReader).ReadAll()
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

	// 路徑驗證：external 走 lenient，預設走嚴格白名單。
	// 之前 readCSVCore 完全不驗證路徑，導致 GetCSVHeaders 可透過 raw absolute
	// path 讀任意檔（multi-agent code review 確認的漏洞）。
	if opts.external {
		if err := h.pathValidator.ValidateExternalPath(filename); err != nil {
			h.logger.Error(opts.logPrefix+"外部路徑驗證失敗", err, map[string]interface{}{"filename": filename})

			return nil, fmt.Errorf("路徑驗證失敗: %w", err)
		}
	} else {
		if err := h.pathValidator.ValidateFilePath(filename); err != nil {
			h.logger.Error(opts.logPrefix+"路徑驗證失敗", err, map[string]interface{}{"filename": filename})

			return nil, fmt.Errorf("路徑驗證失敗: %w", err)
		}
	}

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

// ReadCSVExternal 讀取外部 CSV 檔案（使用 lenient 路徑驗證以容納使用者選檔，
// 仍會擋 path traversal 與系統敏感目錄）.
func (h *CSVHandler) ReadCSVExternal(filename string) ([][]string, error) {
	return h.readCSVCore(filename, readOptions{
		logPrefix: "外部 ",
		external:  true,
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
	if err := os.MkdirAll(h.config.OutputDir, fsperm.DirPerm); err != nil {
		return fmt.Errorf("無法創建輸出目錄: %w", err)
	}

	fullPath, err := h.pathValidator.GetSafePath(h.config.OutputDir, filename)
	if err != nil {
		return fmt.Errorf("無法構建安全輸出路徑: %w", err)
	}

	return h.WriteCSV(fullPath, data)
}

// WriteCSV 寫入 CSV 檔案.
// 採用 named return 以便在 defer 中捕獲 file.Close() 錯誤：NFS 等遠端檔案
// 系統的延遲寫入失敗只會在 Close 時浮現，先前僅 log 不傳播會讓 caller 誤以為
// 寫入成功。
func (h *CSVHandler) WriteCSV(filename string, data [][]string) (err error) {
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

	// 直接打開而不先 Stat：先前的 Stat→OpenFile 是 TOCTOU 結構，攻擊者可在兩
	// 個 syscall 之間以 symlink swap 把輸出導向任意檔案。改用 fsperm.WriteFlags
	// (含 O_NOFOLLOW on unix) 由 OS 在 open 階段直接拒絕 symlink。
	//nolint:gosec // sanitizedPath is sanitized and validated
	file, err := os.OpenFile(sanitizedPath, fsperm.WriteFlags, fsperm.FilePerm)
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
			if err == nil {
				err = fmt.Errorf("關閉輸出檔案 %s 失敗: %w", filename, closeErr)
			}
		}
	}()

	if h.config.BOMEnabled {
		if err := csvutil.WriteBOM(file); err != nil {
			return fmt.Errorf("無法寫入 BOM 到 %s: %w", filename, err)
		}
	}

	// Single chokepoint sanitize: csv_converter sanitizes headers; this catches
	// body-row labels (e.g. result.PhaseName from config.json) that bypass the
	// converter-level guard. SanitizeCellForWrite is idempotent so doubling up
	// is harmless. Closes the formula-injection vector noted by review wave 7
	// security/QA agents (PhaseName="=cmd|/c calc!A1" landing in row[0]).
	w := csv.NewWriter(file)
	if err := w.WriteAll(csvutil.SanitizeAllRows(data)); err != nil {
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
