package io

import (
	"bufio"
	"encoding/csv"
	stderrors "errors"
	"fmt"
	stdio "io" // alias to avoid name shadow with package io
	"os"
	"path/filepath"

	"count_mean/internal/config"
	"count_mean/internal/csvutil"
	"count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/security"
	"count_mean/internal/security/fsperm"
	csvvalidator "count_mean/internal/validation/csv"
)

// Buffer size constants.
const (
	kilobyte            = 1024
	defaultBufferSizeKB = 64
)

// isLargeFileThreshold 是「整檔 ReadAll OOM 風險邊界」 — 超過此尺寸,GetFileInfo
// 標 IsLarge=true,csv_handler.checkFileSizeAndFormat 會拒絕 ReadAll path。
//
// 設計考量:
//   - csv.Reader.ReadAll 把整檔 records materialize 在 [][]string 切片,實測 memory
//     使用約是 source bytes 的 4-10x(Go string header 16B + 每 record [][]string
//     header + GC overhead);200MB source → 800MB-2GB heap 對 GUI process 是 OOM 風險。
//   - 早期實作 200MB(`maxFileSize/10` = 2GB/10)是 streaming threshold,但同時被 ReadAll
//     path 沿用,導致 200MB CSV 走 ReadAll 路徑時 GUI 可能 OOM crash。
//   - 改為 100MB 對 GUI deployment 是合理 trade-off:典型 EMG CSV 1-50MB,100MB 已是
//     極端 outlier;100MB+ 的檔案目前一律拒絕(streaming 路徑已刪,見 ADR-0033)。
//
// 對應上限:maxFileSize(2GB)維持不變 — 那是「絕對拒絕」的上限。
const isLargeFileThreshold = 100 * kilobyte * kilobyte

// scanWarnSampleLimit是 scanFileStructure 對 malformed row 的 per-row
// warning 上限。原本每筆 malformed row 都 emit 一筆 Warn log,惡意輸入(連續
// 數百萬筆 quote-mismatch row)能在幾秒內把 log 檔灌爆(每筆 ~200B,百萬筆 ~200MB,
// CI/operator 的 log shipping 又把它放大 4-10 倍)。
//
// 修法:前 scanWarnSampleLimit 筆照樣 Warn(含 row+原始 error),之後 suppress;
// scan 結束統一 emit 一筆 summary Warn 帶總計 skippedRows、columnCount 與
// suppressed 計數,讓 operator 既知道有問題又不被 log 噪音淹沒。
const scanWarnSampleLimit = 10

// LargeFileHandler 處理大文件的結構.
type LargeFileHandler struct {
	config        *config.AppConfig
	pathValidator *security.PathValidator
	csvValidator  *csvvalidator.Validator
	logger        *logging.Logger

	// 大文件處理配置
	bufferSize  int   // 讀取緩衝區大小
	maxFileSize int64 // 最大文件大小 (bytes)
}

// NewLargeFileHandler 創建大文件處理器.
func NewLargeFileHandler(config *config.AppConfig) *LargeFileHandler {
	allowedPaths := []string{
		config.InputDir,
		config.OutputDir,
		config.OperateDir,
	}

	h := &LargeFileHandler{
		config:        config,
		pathValidator: security.NewPathValidator(allowedPaths),
		csvValidator:  csvvalidator.NewValidator(),
		logger:        logging.GetLogger("large_file_handler"),

		// 預設配置
		bufferSize:  defaultBufferSizeKB * kilobyte,     // 64KB 緩衝區
		maxFileSize: 2 * kilobyte * kilobyte * kilobyte, // 2GB 最大文件大小
	}

	return h
}

// FileInfo 文件信息.
type FileInfo struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	LineCount   int64  `json:"line_count"`
	ColumnCount int    `json:"column_count"`
	IsLarge     bool   `json:"is_large"`
}

// GetFileInfo 執行基本安全檢查（路徑遍歷攻擊防護），支援任意路徑的檔案.
func (h *LargeFileHandler) GetFileInfo(filename string) (*FileInfo, error) {
	h.logger.Debug("開始獲取文件信息", map[string]any{
		"filename": filename,
	})

	// 清理路徑 — 後 SanitizePath 改回 (string, error),原 silent rewrite 取消。
	sanitizedPath, sanitizeErr := h.pathValidator.SanitizePath(filename)
	if sanitizeErr != nil {
		return nil, errors.WrapError(sanitizeErr, errors.ErrCodePathValidation, "路徑淨化失敗")
	}

	// 檢查路徑遍歷攻擊：用 element-based 比對而非 substring，與 PathValidator
	// 一致 — 含字面雙點的合法檔名（report..v2.csv）不應被誤拒
	// （codex Wave 6 second-pass P2）。
	if security.HasTraversalElement(sanitizedPath) {
		return nil, errors.NewAppErrorWithDetails(
			errors.ErrCodePathValidation,
			"路徑包含遍歷字符",
			fmt.Sprintf("路徑 '%s' 包含不安全的遍歷模式", filename),
		)
	}

	// 獲取絕對路徑
	absPath, err := filepath.Abs(sanitizedPath)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodePathValidation, "無法解析路徑")
	}

	// 獲取文件統計信息
	fileInfo, err := os.Stat(absPath)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeFileNotFound, "無法獲取文件信息")
	}

	info := &FileInfo{
		Path: absPath,
		Size: fileInfo.Size(),
		// 從 maxFileSize/10 (= 200MB) 降到 isLargeFileThreshold (100MB)。
		// ReadAll path 在 100MB 已是 GUI process OOM 邊界 (memory peak 4-10x source),
		// 200MB ReadAll 對典型 8-16GB RAM Mac 仍有 crash 風險。100MB+ 的真實 dataset
		// 目前一律拒絕(streaming 路徑已刪)。
		IsLarge: fileInfo.Size() > isLargeFileThreshold,
	}

	// 檢查是否為超大文件
	if fileInfo.Size() > h.maxFileSize {
		return nil, errors.NewAppErrorWithDetails(
			errors.ErrCodeFileTooLarge,
			"文件過大",
			fmt.Sprintf("文件大小 %d bytes 超過限制 %d bytes", fileInfo.Size(), h.maxFileSize),
		)
	}

	// 快速掃描獲取行數和列數
	lineCount, skippedRows, columnCount, err := h.scanFileStructure(absPath)
	if err != nil {
		return nil, err
	}

	info.LineCount = lineCount
	info.ColumnCount = columnCount

	// scanFileStructure 內部對 malformed row (field count 不符) 會
	// 用 continue 跳過，先前 caller 拿不到任何訊號。改為由 scanFileStructure
	// 回傳 skippedRows，這裡若 > 0 就 log warning，避免 silently dropped row
	// 在下游分析結果中造成 missing data 卻無人察覺。
	if skippedRows > 0 {
		h.logger.Warn("掃描檔案時跳過 malformed 行", map[string]any{
			"file_size":     info.Size,
			"line_count":    lineCount,
			"skipped_rows":  skippedRows,
			"column_count":  columnCount,
			"original_path": filename,
		})
	}

	h.logger.Info("文件信息獲取完成", map[string]any{
		"file_size":    info.Size,
		"line_count":   info.LineCount,
		"column_count": info.ColumnCount,
		"skipped_rows": skippedRows,
		"is_large":     info.IsLarge,
	})

	return info, nil
}

// scanFileStructure 快速掃描文件結構.
//
// 回傳 (validLineCount, skippedRows, columnCount, error)：
//   - validLineCount: header + 通過 csv.Reader 解析的 data row 數
//   - skippedRows: field count 與 header 不符或其他 parser error 被 continue 跳過的 row 數
//
// 加 skippedRows 是 先前只回 (lineCount, columnCount, error)，malformed
// row 被 silently 丟棄，operator 無從察覺下游 missing data。Caller 應在
// skippedRows > 0 時 log warning（GetFileInfo 已實作）。
func (h *LargeFileHandler) scanFileStructure(filename string) (int64, int64, int, error) {
	file, err := os.OpenFile(filename, fsperm.ReadFlags, 0) //nolint:gosec // filename sanitized and validated; fsperm.ReadFlags adds O_NOFOLLOW (symmetric with WriteFlags)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("無法開啟文件 %s: %w", filename, err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉文件時發生錯誤", map[string]any{
				"file":  filename,
				"error": closeErr.Error(),
			})
		}
	}()

	// BOM 處理: Excel 匯出的 UTF-8 CSV 帶 0xEF 0xBB 0xBF 前綴,若不剝除 firstRow[0]
	// 會帶 U+FEFF,造成欄位/標題比對失敗。與 internal/io/csv_handler.go:230 對稱:
	// bufio + PeekBOM + csv.NewReader 三段式。
	bufReader := bufio.NewReaderSize(file, h.bufferSize)
	if _, err := csvutil.PeekBOM(bufReader); err != nil {
		return 0, 0, 0, fmt.Errorf("BOM 偵測失敗 %s: %w", filename, err)
	}
	reader := csv.NewReader(bufReader)

	// strict defaults — header 後 enforce 同欄位數（FieldsPerRecord=0 由 reader
	// 從第一筆自動鎖定），未配對引號 fail-fast（LazyQuotes=false），row-by-row 走流式且
	// 不跨 read 共享 record slice，故啟用 ReuseRecord=true 省 alloc。
	//
	// 注意 ReuseRecord=true 的安全契約：本 func 完全忽略每筆 record 的內容（只計數），
	// 不在跨 Read 邊界儲存 slice，所以 backing array 被覆寫無影響。若未來改動本 func
	// 要 cross-read 保留 record（例如收集 sample），必須 deep copy 或關 ReuseRecord。
	reader.FieldsPerRecord = 0
	reader.LazyQuotes = false
	reader.ReuseRecord = true

	// 讀取第一行獲取列數
	firstRow, err := reader.Read()
	if err != nil {
		if stderrors.Is(err, stdio.EOF) {
			return 0, 0, 0, nil
		}

		return 0, 0, 0, fmt.Errorf("讀取文件標題行失敗: %w", err)
	}

	columnCount := len(firstRow)
	lineCount := int64(1)
	skippedRows := int64(0)

	// 計算剩餘行數
	//
	// 首 N 筆 malformed 仍 emit 個別 Warn(operator 可以拿到原始 csv error
	// 用來定位 culprit row),超過 N 筆改 suppress,避免 log 爆炸。Scan 結束統一
	// emit summary Warn 帶 skippedRows / columnCount / suppressed 計數。
	for {
		_, err := reader.Read()
		if stderrors.Is(err, stdio.EOF) {
			break
		}

		if err != nil {
			skippedRows++
			if skippedRows <= scanWarnSampleLimit {
				h.logger.Warn("掃描文件時遇到錯誤，繼續處理", map[string]any{
					"error":         err.Error(),
					"line":          lineCount + skippedRows,
					"skipped_total": skippedRows,
				})
			}

			continue
		}

		lineCount++
	}

	if skippedRows > scanWarnSampleLimit {
		h.logger.Warn("malformed 行 sample 已達上限,後續逐筆 warn 已 suppress", map[string]any{
			"file":               filename,
			"skipped_total":      skippedRows,
			"warn_sample_limit":  scanWarnSampleLimit,
			"suppressed_after_n": skippedRows - scanWarnSampleLimit,
		})
	}

	return lineCount, skippedRows, columnCount, nil
}
