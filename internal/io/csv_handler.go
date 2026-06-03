// Package io provides file input/output operations for the EMG data analysis
// application, including CSV reading, writing, and streaming support for large files.
package io

import (
	"bufio"
	"context"
	"encoding/csv"
	stderrors "errors"
	"fmt"
	stdio "io" // alias to avoid name shadow with package io
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"count_mean/internal/calculator"
	"count_mean/internal/cci"
	"count_mean/internal/config"
	"count_mean/internal/csvutil"
	"count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/security"
	"count_mean/internal/security/fsperm"
	"count_mean/internal/validation"
	"count_mean/internal/validation/filename"
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
	converter        *csvConverter
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
		converter:        newCSVConverter(scalingMultiplier, cfg.Precision),
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
		h.logger.Error(opts.logPrefix+"檔案路徑驗證失敗", err, map[string]any{"filename": filename})

		return "", err
	}

	if fileInfo.IsLarge {
		h.logger.Info("檢測到大文件，使用流式讀取", map[string]any{
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
		h.logger.Error("檔案格式驗證失敗", err, map[string]any{"path": cleanPath})

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
		h.logger.Error("檔案開啟失敗", appErr, map[string]any{"path": cleanPath})

		return nil, appErr
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉檔案時發生錯誤", map[string]any{
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
		h.logger.Error("BOM 偵測失敗", appErr, map[string]any{"path": cleanPath})

		return nil, appErr
	}

	// 明確套用 strict defaults，把 csv.NewReader 的 DoS / quote-injection
	// 守門收斂在一個地方，避免日後有人 silently 翻 LazyQuotes/FieldsPerRecord：
	//   - FieldsPerRecord = 0：保留 Go 預設行為，header 之後 enforce 同欄位數。
	//     ReadAll 路徑要求整檔欄位一致（與既存 validateCSVRecords/ValidateCSVData 對齊）。
	//   - LazyQuotes = false：reject 未配對引號（即 MalformedCSV test pins 的契約）。
	//     attacker-controlled CSV 不應靠不規範引號吃進 reader 的 cap 配置。
	//   - ReuseRecord = false：ReadAll 內部把每筆 record 完整 materialize 在回傳的
	//     [][]string，shared backing array 不適用；顯式 false 避免未來 refactor 切到
	//     row-by-row 時誤承載 reuse semantics。
	reader := csv.NewReader(bufReader)
	reader.FieldsPerRecord = 0
	reader.LazyQuotes = false
	reader.ReuseRecord = false

	records, err := reader.ReadAll()
	if err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeDataParsing, "無法讀取 CSV 資料")
		h.logger.Error("CSV 資料讀取失敗", appErr, map[string]any{"path": cleanPath})

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
		h.logger.Error("CSV 資料驗證失敗", err, map[string]any{
			"path": cleanPath, "record_count": len(records),
		})

		return err
	}

	if err := h.validator.ValidateCSVData(records, cleanPath); err != nil {
		h.logger.Error("CSV 資料結構驗證失敗", err, map[string]any{"path": cleanPath})

		return fmt.Errorf("CSV 資料驗證失敗: %w", err)
	}

	return nil
}

// readCSVCore is the internal method that handles CSV reading logic.
func (h *CSVHandler) readCSVCore(filename string, opts readOptions) ([][]string, error) {
	h.logger.Debug("開始讀取"+opts.logPrefix+" CSV 檔案", map[string]any{"filename": filename})

	// 路徑驗證：external 走 lenient，預設走嚴格白名單。
	// 之前 readCSVCore 完全不驗證路徑，導致 GetCSVHeaders 可透過 raw absolute
	// path 讀任意檔（multi-agent code review 確認的漏洞）。
	if opts.external {
		if err := h.pathValidator.ValidateExternalPath(filename); err != nil {
			h.logger.Error(opts.logPrefix+"外部路徑驗證失敗", err, map[string]any{"filename": filename})

			return nil, fmt.Errorf("路徑驗證失敗: %w", err)
		}
	} else {
		if err := h.pathValidator.ValidateFilePath(filename); err != nil {
			h.logger.Error(opts.logPrefix+"路徑驗證失敗", err, map[string]any{"filename": filename})

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

	h.logger.Info(opts.logPrefix+"CSV 檔案讀取成功", map[string]any{
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
//
// 採用 named return 以便在 defer 中捕獲 file.Sync() / file.Close() 錯誤：
//   - Sync (fsync syscall) 保證資料真的落 disk;否則 OS page cache 內的
//     bytes 在 power loss / kernel panic 後會失蹤,caller 卻收到 nil error。
//   - Close 失敗的常見原因是 buffered write flush 階段失敗(NFS 延遲寫入
//     是其中一種,但 local FS 同樣可能因 disk full / quota / I/O error 等
//     在 Close 浮現);僅 log 不傳播會讓 caller 誤以為寫入成功。
//
// 原本只 Close 不 Sync,且註解誤標「NFS 延遲寫入失敗只在 Close 時
// 浮現」— 對 local FS 是錯誤假設(local FS 的 fsync 與 close 是兩個獨立 syscall,
// close 不會自動 flush dirty page)。改為 defer 內 Sync → Close 兩段式收尾,
// 任一階段失敗都升為 named return err。
//
// Caller 替代方案:如需更強保證(crash-safe rename + tmp 清理),改走
// csvutil.WriteCSVAtomic — 它已內建 fsync + atomic rename。
func (h *CSVHandler) WriteCSV(filename string, data [][]string) (err error) {
	h.logger.Debug("開始寫入 CSV 檔案", map[string]any{
		"filename":    filename,
		"row_count":   len(data),
		"bom_enabled": h.config.BOMEnabled,
	})

	sanitizedPath, sanitizeErr := h.pathValidator.SanitizePath(filename)
	if sanitizeErr != nil {
		h.logger.Error("寫入路徑淨化失敗", sanitizeErr, map[string]any{
			"original_path": filename,
		})

		return fmt.Errorf("路徑驗證失敗: %w", sanitizeErr)
	}
	if err := h.pathValidator.ValidateFilePath(sanitizedPath); err != nil {
		h.logger.Error("寫入路徑驗證失敗", err, map[string]any{
			"original_path":  filename,
			"sanitized_path": sanitizedPath,
		})

		return fmt.Errorf("路徑驗證失敗: %w", err)
	}

	if !h.pathValidator.IsCSVFile(sanitizedPath) {
		err := fmt.Errorf("檔案 '%s': %w", sanitizedPath, errInvalidCSVFile)
		h.logger.Error("檔案格式驗證失敗", err, map[string]any{
			"path": sanitizedPath,
		})

		return err
	}

	// 空 data 的處理必須區分「target 是否已存在」:
	//   (a) target 不存在 → return nil(no-op 安全;不建檔案,caller 期望「空輸入
	//       產不出檔案」)。
	//   (b) target *已存在* → 必須 truncate stale 內容,否則 caller 以為這次寫入
	//       了空結果,但磁碟上仍是上次完整資料 — patient 載入時會誤以為是新分析。
	// truncate 策略:BOMEnabled 時保留 BOM(維持「這是 UTF-8 CSV」hint);
	// BOMEnabled=false 則 truncate 到 0 byte(完全空檔)。
	// path 驗證必須先過,空 data 也不該被當成 path-validation bypass 的後門。
	if len(data) == 0 {
		return h.handleEmptyDataWrite(sanitizedPath, filename)
	}

	// 原本 os.OpenFile(sanitizedPath, WriteFlags) 是 lexical-only + O_NOFOLLOW
	// 兩段式守門:
	//   - sanitizedPath 只是字串清理,沒 EvalSymlinks resolve,parent component 是
	//     symlink 時 lexical isPathWithinBase 通過,kernel 在 syscall 階段跟到底,
	//     檔案落在 OutputDir 外。
	//   - O_NOFOLLOW 只擋 leaf component 為 symlink 的 case,parent 為 symlink
	//     完全不擋。
	//
	// 改用 fsperm.OpenWriteValidated:內部會 EvalSymlinks resolve sanitizedPath 後
	// 比對 GetAllowedBasePaths(),resolved path 落在 base 外直接 reject;同時
	// Linux 用 openat2(RESOLVE_BENEATH)、Darwin 用 O_NOFOLLOW_ANY 取得 kernel-
	// level atomic 保證。詳見 internal/security/fsperm/validated_open.go 註解。
	//
	// GetAllowedBasePaths 透過 PathValidator 的 RWMutex 拿快照,確保與 SetAllowedBasePaths
	// 並發呼叫安全。
	file, err := fsperm.OpenWriteValidated(sanitizedPath, h.pathValidator.GetAllowedBasePaths())
	if err != nil {
		h.logger.Error("無法建立輸出檔案", err, map[string]any{
			"path": sanitizedPath,
		})

		return fmt.Errorf("無法建立檔案 %s: %w", sanitizedPath, err)
	}

	// 兩段式收尾 — 先 Sync 再 Close。
	//   - Sync 失敗(fsync syscall 拒絕,disk full / I/O error / EIO)代表 OS
	//     不能保證 page cache 的 bytes 已寫到 storage,必須回 err。
	//   - Sync 成功之後,Close 失敗多半是 fd 重複關閉之類,但仍記 err 以保險。
	// 兩階段都 mutate named return err,但只在原 err 為 nil 時覆寫(避免遮蔽
	// payload write 失敗的根因 err)。
	defer func() {
		if syncErr := file.Sync(); syncErr != nil {
			h.logger.Warn("fsync 輸出檔案時發生錯誤", map[string]any{
				"file":  file.Name(),
				"error": syncErr.Error(),
			})
			if err == nil {
				err = fmt.Errorf("fsync 輸出檔案 %s 失敗: %w", filename, syncErr)
			}
		}
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉輸出檔案時發生錯誤", map[string]any{
				"file":  file.Name(),
				"error": closeErr.Error(),
			})
			if err == nil {
				err = fmt.Errorf("關閉輸出檔案 %s 失敗: %w", filename, closeErr)
			}
		}
	}()

	if err := writeCSVPayload(file, data, h.config.BOMEnabled); err != nil {
		h.logger.Error("CSV 資料寫入失敗", err, map[string]any{
			"path":     sanitizedPath,
			"filename": filename,
		})

		return fmt.Errorf("無法寫入資料到 %s: %w", filename, err)
	}

	h.logger.Info("CSV 檔案寫入成功", map[string]any{
		"path":      sanitizedPath,
		"row_count": len(data),
		"bom_used":  h.config.BOMEnabled,
	})

	return nil
}

// handleEmptyDataWrite 處理 WriteCSV 收到 empty data 的兩個分支:
//   - target 不存在:return nil(no-op 安全)。
//   - target 已存在:用 fsperm.OpenWriteValidated 重開檔(O_TRUNC),寫入空內容
//     (BOMEnabled → BOM-only 維持 CSV 語意 hint;else → 0 byte)。
//
// caller (WriteCSV) 已完成 SanitizePath / ValidateFilePath / IsCSVFile 三段守門,
// 此 helper 不重覆驗證以避免 lexical/resolved 兩條路徑不一致;但仍走 fsperm
// safe-open(保留 symlink / parent-symlink 攻擊面的 kernel-level reject)。
func (h *CSVHandler) handleEmptyDataWrite(sanitizedPath, originalFilename string) (err error) {
	if _, statErr := os.Stat(sanitizedPath); statErr != nil {
		if os.IsNotExist(statErr) {
			h.logger.Warn("WriteCSV 收到空 data，目標不存在,跳過建檔", map[string]any{
				"filename": originalFilename,
			})
			return nil
		}
		// 其他 stat 錯誤(permission denied、I/O error 等)— 不該當成 not-exist
		// 處理(會 silently skip truncate),回傳錯誤讓 caller 知道。
		h.logger.Error("WriteCSV 空 data 路徑探測失敗", statErr, map[string]any{
			"path": sanitizedPath,
		})
		return fmt.Errorf("空 data 探測目標檔案失敗: %w", statErr)
	}

	// target 已存在 — 必須 truncate stale 內容,不能讓 caller 以為「寫了空結果」
	// 但磁碟仍是舊資料。fsperm.OpenWriteValidated 內含 O_TRUNC(WriteFlags),
	// 重新 open 等同 truncate。
	file, err := fsperm.OpenWriteValidated(sanitizedPath, h.pathValidator.GetAllowedBasePaths())
	if err != nil {
		h.logger.Error("無法 truncate stale 檔案", err, map[string]any{
			"path": sanitizedPath,
		})
		return fmt.Errorf("無法 truncate %s: %w", sanitizedPath, err)
	}

	// 兩段式收尾(同 WriteCSV main path):先 Sync 再 Close。
	defer func() {
		if syncErr := file.Sync(); syncErr != nil {
			h.logger.Warn("fsync truncated 檔案時發生錯誤", map[string]any{
				"file":  file.Name(),
				"error": syncErr.Error(),
			})
			if err == nil {
				err = fmt.Errorf("fsync truncated 檔案 %s 失敗: %w", originalFilename, syncErr)
			}
		}
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉 truncated 檔案時發生錯誤", map[string]any{
				"file":  file.Name(),
				"error": closeErr.Error(),
			})
			if err == nil {
				err = fmt.Errorf("關閉 truncated 檔案 %s 失敗: %w", originalFilename, closeErr)
			}
		}
	}()

	if h.config.BOMEnabled {
		if writeErr := csvutil.WriteBOM(file); writeErr != nil {
			return fmt.Errorf("寫入 BOM-only truncated 檔案失敗: %w", writeErr)
		}
	}
	// BOMEnabled=false 時不寫任何 byte,truncate 後檔案長度為 0。

	h.logger.Info("WriteCSV 空 data + 既有目標檔案: 已 truncate", map[string]any{
		"path":     sanitizedPath,
		"bom_used": h.config.BOMEnabled,
	})

	return nil
}

// writeCSVPayload 把 [data] 透過 csv.Writer 寫到 w，必要時先寫 BOM。
//
// 先前 inline 在 WriteCSV 內，僅靠 csv.Writer.WriteAll 的回傳值判斷
// 寫入是否成功。WriteAll 本身已包含 Flush 並回 flush error，但若未來 csv 套件
// 修改契約（或 caller 改成手動 Write loop），缺少 writer.Error() 顯式檢查會
// 讓 bufio 累積的延遲錯誤被靜默吞掉。改成獨立 helper 並補上顯式 Error() check
// 同時支援注入 fake io.Writer 進行 flush-failure 測試。
//
// Single chokepoint sanitize: csv_converter sanitizes headers; SanitizeAllRows
// catches body-row labels (e.g. result.PhaseName from config.json) that bypass
// the converter-level guard. SanitizeCellForWrite is idempotent so doubling up
// is harmless. Closes the formula-injection vector noted by review wave 7
// security/QA agents (PhaseName="=cmd|/c calc!A1" landing in row[0]).
func writeCSVPayload(w stdio.Writer, data [][]string, bomEnabled bool) error {
	if bomEnabled {
		if err := csvutil.WriteBOM(w); err != nil {
			return fmt.Errorf("無法寫入 BOM: %w", err)
		}
	}

	writer := csv.NewWriter(w)
	if err := writer.WriteAll(csvutil.SanitizeAllRows(data)); err != nil {
		return fmt.Errorf("WriteAll 失敗: %w", err)
	}

	// 顯式 writer.Error() check：即使 WriteAll 回 nil，仍可能有先前 Write 累積
	// 在 bufio 內的延遲錯誤未被回報（csv.Writer 文件明文要求呼叫 Error() 確認）。
	// 防止 named-return + defer 結構在未來 refactor 中被誤刪而 silently lose error。
	if err := writer.Error(); err != nil {
		return fmt.Errorf("csv.Writer flush 失敗: %w", err)
	}

	return nil
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
	h.logger.Info("開始處理大文件", map[string]any{
		"filename":    filename,
		"window_size": windowSize,
	})

	return h.largeFileHandler.ProcessLargeFileInChunks(filename, windowSize, callback)
}

// errEmptyPhaseAnalysis 標示 WritePhaseAnalysis 收到沒有 phase 結果可寫的請求。
// caller 該在進 WritePhaseAnalysis 前就確保 PhaseResults 非空, 但對外仍給明確 error
// 以便 GUI 顯示「無分析結果可匯出」而非吞 nil。
var errEmptyPhaseAnalysis = stderrors.New("WritePhaseAnalysis: 沒有 phase 結果可寫")

// errEmptyPhaseSyncResult 標示 WritePhaseSyncResult / WriteNormalizedPhaseSyncResult
// 收到 nil stats。對稱於 errEmptyPhaseAnalysis: caller 應確保 stats 非 nil,但對外仍
// 給明確 error 避免 nil-deref panic 上拋到 GUI。
var errEmptyPhaseSyncResult = stderrors.New("PhaseSync stats 不可為 nil")

// errEmptyPhaseSyncEMGData 標示 WriteNormalizedPhaseSyncEMG 收到 nil data。
// 對稱於 errEmptyPhaseSyncResult:caller 應確保 data 非 nil,但對外仍給明確 error
// 供 GUI 顯示「EMG 數據為空」而非吞 nil。i18n 契約:中文訊息直接顯示給使用者。
var errEmptyPhaseSyncEMGData = stderrors.New("EMG 數據為空")

// WriteRequest 是 format-aware write 共用的目的地請求。
//
// Filename 是 CSV 檔名;SubDir 為空時直接寫到 OutputDir 根,非空時自動
// MkdirAll(OutputDir/SubDir) 後寫到該子目錄。
//
// 此 struct 取代 caller 端在「WriteCSVToOutput vs WriteCSVToOutputDirectory」
// 與「Convert* + WriteCSV*」兩條選擇上的 ad-hoc 拼接 — caller 一次描述「寫哪裡」即可。
type WriteRequest struct {
	Filename string
	SubDir   string
}

// WriteMaxMean 把 MaxMean 計算結果寫成 6-row CSV (header / startRange / endRange /
// startTime / endTime / maxMean)。
//
// row layout、scaling (config.ScalingFactor 推導的 scalingMultiplier)、precision
// (config.Precision) 由 implementation 持有;caller 不再呼叫 Convert* 後組裝
// [][]string。BOM / formula-injection sanitize / fsperm symlink reject / fsync
// 兩段式收尾沿用 WriteCSV 既有路徑。
func (h *CSVHandler) WriteMaxMean(
	req WriteRequest,
	headers []string,
	results []models.MaxMeanResult,
	startRange, endRange float64,
) (string, error) {
	data := h.converter.ConvertMaxMeanResults(headers, results, startRange, endRange)

	if err := h.writeToTarget(req, data); err != nil {
		return "", err
	}

	return filepath.Join(h.config.OutputDir, req.SubDir, req.Filename), nil
}

// WriteNormalized 把標準化後的 EMGDataset 寫成 CSV (1 header + N data rows)。
//
// Time 欄套用 dataset.OriginalTimePrecision,data 欄套用 config.Precision —
// 這條「time vs data 不同 precision」的內部規則 caller 不會看到。
func (h *CSVHandler) WriteNormalized(req WriteRequest, dataset *models.EMGDataset) (string, error) {
	data := h.converter.ConvertNormalizedData(dataset)

	if err := h.writeToTarget(req, data); err != nil {
		return "", err
	}

	return filepath.Join(h.config.OutputDir, req.SubDir, req.Filename), nil
}

// WritePhaseAnalysis 把 phase 分析結果寫成 CSV;支援單 phase 與多 phase merge。
//
// Multi-phase row layout:
//
//	row 0:        header
//	row 1..2N:    p1.max, p1.mean, p2.max, p2.mean, ..., pN.max, pN.mean
//	row 2N+1:     全域 time-index row (僅當 MaxTimeIndex 非空)
//
// Single phase 退化為 [header, max, mean, time-index?]。
//
// 取代 caller 端「迴圈 Convert + phaseRows[1:] skip header + fullRows[3:] dedupe
// time row」這串對 ConvertPhaseAnalysis 內部 4-row 結構的 leakage:
// 由於 MaxTimeIndex 是全域資料 (跨 phase 共用),把它帶給「最後一個 phase」呼叫
// converter 是合法 invariant — 結果與「先全部 nil 再單獨補 time row」等價,但消除
// row index 切片的 magic number。
func (h *CSVHandler) WritePhaseAnalysis(
	req WriteRequest,
	headers []string,
	result *calculator.AnalyzeResult,
) (string, error) {
	if result == nil || len(result.PhaseResults) == 0 {
		return "", errEmptyPhaseAnalysis
	}

	var data [][]string

	lastIdx := len(result.PhaseResults) - 1
	for i := range result.PhaseResults {
		// 只在最後一個 phase 把 MaxTimeIndex 傳給 converter,讓 time-index row
		// 自然 land 在所有 phase 之後;前面的 phase 一律傳 nil 不加 time row。
		var timeIdx map[int]float64
		if i == lastIdx {
			timeIdx = result.MaxTimeIndex
		}

		phaseRows := h.converter.ConvertPhaseAnalysis(headers, &result.PhaseResults[i], timeIdx)
		if i == 0 {
			data = phaseRows
			continue
		}
		// 後續 phase 跳過 header (phaseRows[0]),只附加 max/mean (與最後一個的 time-index)。
		if len(phaseRows) > 1 {
			data = append(data, phaseRows[1:]...)
		}
	}

	if err := h.writeToTarget(req, data); err != nil {
		return "", err
	}

	return filepath.Join(h.config.OutputDir, req.SubDir, req.Filename), nil
}

// writeToTarget 是 format-aware write 的內部 dispatch 點:依 req.SubDir 走 WriteCSVToOutput
// 或 WriteCSVToOutputDirectory,確保 path validation / BOM / sanitize / fsync 路徑沿用
// WriteCSV 既有守門。
func (h *CSVHandler) writeToTarget(req WriteRequest, data [][]string) error {
	if req.SubDir != "" {
		return h.WriteCSVToOutputDirectory(req.SubDir, req.Filename, data)
	}

	return h.WriteCSVToOutput(req.Filename, data)
}

// phaseSyncAtomicWrite 是所有 PhaseSync / normalized-EMG 原子寫檔的共用 seam:
// path join → validate → mkdir → WriteCSVAtomic{Header, BasePaths, Emit}。
// error-wrap 字串「PhaseSync 輸出路徑無效/輸出目錄建立失敗」逐字保留供呼叫端識別。
func (h *CSVHandler) phaseSyncAtomicWrite(
	subDir, filename string,
	header []string,
	emit csvutil.RowEmitter,
) (string, error) {
	outputPath, joinErr := h.safeJoinOutput(subDir, filename)
	if joinErr != nil {
		return "", fmt.Errorf("PhaseSync 輸出路徑無效: %w", joinErr)
	}

	if err := h.pathValidator.ValidateExternalPath(outputPath); err != nil {
		return "", fmt.Errorf("PhaseSync 輸出路徑無效: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), fsperm.DirPerm); err != nil {
		return "", fmt.Errorf("PhaseSync 輸出目錄建立失敗: %w", err)
	}

	err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
		Header:    header,
		BasePaths: h.pathValidator.GetAllowedBasePaths(),
		Emit:      emit,
	})
	if err != nil {
		return "", err
	}

	return outputPath, nil
}

// writePhaseSyncAtomic 是 WritePhaseSyncResult / WriteNormalizedPhaseSyncResult
// 的薄包裝:委派給 phaseSyncAtomicWrite,並把 data[][]string 轉換為 streaming emit。
// data[0] 是 header,data[1:] 是 body rows(由 converter.ConvertPhaseSyncResult 產生)。
// ConvertPhaseSyncResult 恆回固定 8-row layout,故 data 至少 1 row,data[0] 不會 panic。
func (h *CSVHandler) writePhaseSyncAtomic(subDir, filename string, data [][]string) (string, error) {
	return h.phaseSyncAtomicWrite(subDir, filename, data[0], func(emit func([]string) error) error {
		for _, row := range data[1:] {
			if err := emit(row); err != nil {
				return err
			}
		}
		return nil
	})
}

// WritePhaseSyncResult 把 PhaseSync 分析結果 (EMGStatistics) 寫成 CSV。
//
// Filename 由 calculator.GenerateOutputFileName(Subject, StartPhase, EndPhase)
// 自動生成 — req.Filename 被忽略, 僅 req.SubDir 生效 (空字串 → OutputDir 根)。
// 回傳實際 outputPath (絕對路徑, 形如 OutputDir/[SubDir/]<filename>) 與錯誤。
//
// row layout (8-row: header / 開始分期點 / 開始時間 / 結束分期點 / 結束時間 /
// 時間差值 / 平均值 / 最大值)、precision (phaseSyncPrecision=6) 由 implementation 持有。
// 路徑由 writePhaseSyncAtomic 守門 + WriteCSVAtomic tmp+rename atomic 寫入 —
// ADR-0001 invariant: Subject-based write ⟹ WriteCSVAtomic。
func (h *CSVHandler) WritePhaseSyncResult(
	req WriteRequest,
	stats *models.EMGStatistics,
) (string, error) {
	if stats == nil {
		return "", errEmptyPhaseSyncResult
	}

	filename := calculator.GenerateOutputFileName(
		stats.Subject, stats.StartPhase, stats.EndPhase,
	)

	data := h.converter.ConvertPhaseSyncResult(stats)

	return h.writePhaseSyncAtomic(req.SubDir, filename, data)
}

// WriteNormalizedPhaseSyncResult 把 normalized PhaseSync 分析結果寫成 CSV。
//
// Filename 由 stats.Subject 經 filename.Sanitize 後與 normStart/normEnd/stats 分期
// 自動推導 — req.Filename 被忽略,僅 req.SubDir 生效。
// Filename template: {safeSubject}_normalized_norm-{normStart}-{normEnd}_stats-{StartPhase}-{EndPhase}.csv
// (對齊 GUI 現行 template)。
//
// row layout 與 WritePhaseSyncResult 相同 (8-row,由 ConvertPhaseSyncResult 持有)。
// 路徑由 writePhaseSyncAtomic 守門 + WriteCSVAtomic tmp+rename atomic 寫入 —
// ADR-0001 invariant: Subject-based write ⟹ WriteCSVAtomic。
func (h *CSVHandler) WriteNormalizedPhaseSyncResult(
	req WriteRequest,
	stats *models.EMGStatistics,
	normStart, normEnd models.PhasePoint,
) (string, error) {
	if stats == nil {
		return "", errEmptyPhaseSyncResult
	}

	suffix := fmt.Sprintf("normalized_norm-%s-%s_stats-%s-%s",
		normStart, normEnd, stats.StartPhase, stats.EndPhase)
	fname := filename.SubjectOutputName(stats.Subject, suffix) + ".csv"

	data := h.converter.ConvertPhaseSyncResult(stats)

	return h.writePhaseSyncAtomic(req.SubDir, fname, data)
}

// WriteCCIResult 把 CCI 分析結果寫成 CSV。
//
// Filename 由 result.Subject 經 filename.Sanitize 後 + "_CCI_Rudolph.csv" suffix
// 推導 — req.Filename 被忽略,僅 req.SubDir 生效(空字串 → OutputDir 根)。
// 回傳實際 outputPath 與錯誤。
//
// row layout: 1 header row ["Time (s)", "Gait Cycle (%)", PairName...]
//
//   - N data rows,每 row 含 [time, gait_pct, pair_value...]
//
// NaN/Inf cell → 空字串;整 row 所有 pair 都 NaN/Inf → skip 整 row(計入 droppedRowCount)。
//
// ctx 為第一個參數,sample loop 中每 cciStreamCtxCheckInterval 點檢查一次 ctx.Done()
// — caller cancel 後立即停寫並回 ctx.Err。csvutil.WriteCSVAtomic 對 emit 回 error 走
// tmp file abort 路徑,不留下半成品。
//
// ADR-0001 invariant 延伸到 CCI: pathValidator 守門覆蓋原 cci.ExportToCSV 缺少的
// defense-in-depth(原路徑走 security.NewPathValidator(nil) 未整合 CSVHandler 既有
// allowedPaths;由本 method 統一補上)。
func (h *CSVHandler) WriteCCIResult(
	ctx context.Context, req WriteRequest, result *cci.CCIAnalysisResult,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if result == nil {
		return "", errEmptyCCIResult
	}

	duration := result.GaitEndTime - result.GaitStartTime
	if duration <= 0 {
		return "", fmt.Errorf("%w: start=%v end=%v",
			cci.ErrInvalidGaitCycle, result.GaitStartTime, result.GaitEndTime)
	}

	fname := filename.SubjectOutputName(result.Subject, "CCI_Rudolph") + ".csv"
	outputPath, joinErr := h.safeJoinOutput(req.SubDir, fname)
	if joinErr != nil {
		return "", fmt.Errorf("CCI 輸出路徑無效: %w", joinErr)
	}

	// Defense-in-depth: system-dir prefix check via pathValidator(對齊 ADR-0001 守門)。
	if err := h.pathValidator.ValidateExternalPath(outputPath); err != nil {
		return "", fmt.Errorf("CCI 輸出路徑無效: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), fsperm.DirPerm); err != nil {
		return "", fmt.Errorf("CCI 輸出目錄建立失敗: %w", err)
	}

	header := []string{"Time (s)", "Gait Cycle (%)"}
	for _, pr := range result.PairResults {
		header = append(header, pr.PairName)
	}

	var droppedRowCount int
	numPoints := len(result.TimeValues)

	err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
		Header:    header,
		BasePaths: h.pathValidator.GetAllowedBasePaths(),
		Emit: func(emit func([]string) error) error {
			for i := 0; i < numPoints; i++ {
				if i > 0 && i%cciStreamCtxCheckInterval == 0 {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
				}

				t := result.TimeValues[i]
				pct := (t - result.GaitStartTime) / duration * 100

				pairCells := make([]string, 0, len(result.PairResults))
				allNonFinite := true

				for _, pr := range result.PairResults {
					if i >= len(pr.Values) {
						pairCells = append(pairCells, "")
						continue
					}
					v := pr.Values[i]
					if math.IsNaN(v) || math.IsInf(v, 0) {
						pairCells = append(pairCells, "")
						continue
					}
					pairCells = append(pairCells, fmt.Sprintf("%.6f", v))
					allNonFinite = false
				}

				if len(result.PairResults) > 0 && allNonFinite {
					droppedRowCount++
					continue
				}

				row := []string{
					fmt.Sprintf("%.4f", t),
					fmt.Sprintf("%.2f", pct),
				}
				row = append(row, pairCells...)

				if err := emit(row); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err != nil {
		return "", err
	}

	if droppedRowCount > 0 && h.logger != nil {
		h.logger.Warn("CCI 匯出 CSV 時跳過全 NaN/Inf 的 row", map[string]any{
			"dropped_rows": droppedRowCount,
			"total_rows":   numPoints,
			"output_path":  outputPath,
		})
	}

	return outputPath, nil
}

// cciStreamCtxCheckInterval 是 CCI 寫檔 emit loop 內 ctx 取消檢查間隔,
// 跟原 cci 套件常數 (cciChartCtxCheckInterval) 對齊,避免每點 select 過熱。
const cciStreamCtxCheckInterval = 64

// errEmptyCCIResult 標示 WriteCCIResult 收到 nil result。
var errEmptyCCIResult = stderrors.New("WriteCCIResult: result is nil")

// MuscleRatioOutputAllPayload 是 WriteMuscleRatioOutputAll 的輸入承載結構。
//
// 把 muscle_ratio.Analyzer.analyzeSubject 內部計算出的 ratios 與時間軸打包傳給
// CSVHandler;Subject 給 filename derivation 用,PairLabels 是 ratio pair 的
// header 名稱 (對齊 muscle_ratio.DefaultRatios() Name 欄位)。
type MuscleRatioOutputAllPayload struct {
	Subject    string
	PairLabels []string
	Times      []float64
	Ratios     [][]float64 // 每個 pair 一個 inner slice,長度與 Times 對齊
}

// CCIPhaseStatRowPayload 是 Output-2 的一行:Item 為區間標籤或相位點名稱,
// Metric 為視窗說明文字,Time 為 EMG 時間(只在 HasTime 為 true 時輸出),
// Values 為各 pair 的統計值(與 PairLabels 同序)。對應 MuscleRatioPhasePoint。(ADR-0018)
type CCIPhaseStatRowPayload struct {
	Item    string
	Metric  string
	Time    float64
	HasTime bool
	Values  []float64
}

// CCIOutputPhasesPayload — handler 純 layout:verbatim emit Rows;PairLabels 由 caller
// 設定(GUI 從 cci.CCIAnalysisResult.PhaseStats 加上 12 個 pair 名稱)。(ADR-0018)
type CCIOutputPhasesPayload struct {
	Subject    string
	PairLabels []string
	Rows       []CCIPhaseStatRowPayload
}

// MuscleRatioPhasePoint 是 Analyzer 在 collectPhasePoints + WindowMean 階段算好的
// Output 2 條目:Name 顯示名稱,Time 為對齊到最近 EMG sample 的中心時間,
// Values 為各 pair 已算好的 11 點 window mean(與 PairLabels 同序)。(ADR-0014)
type MuscleRatioPhasePoint struct {
	Name   string
	Time   float64
	Values []float64
}

// MuscleRatioOutputPhasesPayload — handler 純 layout:只 emit Points 內算好的值。
// 不再帶 Times/Ratios(ADR-0014:math 上移 Analyzer,handler 不 reach-in)。
type MuscleRatioOutputPhasesPayload struct {
	Subject    string
	PairLabels []string // 已由 Analyzer 加上 "(11pt avg)" 後綴,handler verbatim emit
	Points     []MuscleRatioPhasePoint
}

// WriteMuscleRatioOutputAll 寫 per-subject Output 1 — full time-series ratio CSV。
//
// Filename 由 Subject 經 filename.Sanitize 後 + "_muscle_ratio.csv" 推導;
// req.Filename 被忽略,僅 req.SubDir 生效。
//
// row layout: 1 header ["Time (s)", PairLabels...] + N data rows。
// NaN/Inf cell → 空字串。
func (h *CSVHandler) WriteMuscleRatioOutputAll(
	req WriteRequest, p MuscleRatioOutputAllPayload,
) (string, error) {
	if len(p.Times) == 0 {
		return "", errEmptyMuscleRatioPayload
	}

	fname := filename.SubjectOutputName(p.Subject, "muscle_ratio") + ".csv"
	outputPath, joinErr := h.safeJoinOutput(req.SubDir, fname)
	if joinErr != nil {
		return "", fmt.Errorf("muscle_ratio 輸出路徑無效: %w", joinErr)
	}

	if err := h.validateMuscleRatioOutputDir(req.SubDir); err != nil {
		return "", err
	}

	header := make([]string, 0, 1+len(p.PairLabels))
	header = append(header, "Time (s)")
	header = append(header, p.PairLabels...)

	err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
		Header:    header,
		BasePaths: h.pathValidator.GetAllowedBasePaths(),
		Emit: func(emit func([]string) error) error {
			for i, t := range p.Times {
				row := make([]string, 0, 1+len(p.Ratios))
				row = append(row, fmt.Sprintf("%.4f", t))
				for k := range p.Ratios {
					row = append(row, formatMuscleRatioCell(p.Ratios[k], i))
				}
				if err := emit(row); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err != nil {
		return "", err
	}
	return outputPath, nil
}

// WriteMuscleRatioOutputPhases 寫 per-subject Output 2 — phase+midpoint slice CSV。
//
// Filename 由 Subject 推導 + "_muscle_ratio_phases_avg11.csv";req.Filename 被忽略。
// Row 數量由 caller (muscle_ratio.Analyzer) 預先決定的 Points 切片長度決定。
// Analyzer 已算好 window mean 值,handler 純 layout emit。(ADR-0014)
func (h *CSVHandler) WriteMuscleRatioOutputPhases(
	req WriteRequest, p MuscleRatioOutputPhasesPayload,
) (string, error) {
	if len(p.Points) == 0 {
		return "", errEmptyMuscleRatioPayload
	}

	fname := filename.SubjectOutputName(p.Subject, "muscle_ratio_phases_avg11") + ".csv"
	outputPath, joinErr := h.safeJoinOutput(req.SubDir, fname)
	if joinErr != nil {
		return "", fmt.Errorf("muscle_ratio 輸出路徑無效: %w", joinErr)
	}

	if err := h.validateMuscleRatioOutputDir(req.SubDir); err != nil {
		return "", err
	}

	header := make([]string, 0, 2+len(p.PairLabels))
	header = append(header, "Phase", "Time (s)")
	header = append(header, p.PairLabels...)

	err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
		Header:    header,
		BasePaths: h.pathValidator.GetAllowedBasePaths(),
		Emit: func(emit func([]string) error) error {
			for _, point := range p.Points {
				row := make([]string, 0, 2+len(point.Values))
				row = append(row, point.Name, fmt.Sprintf("%.4f", point.Time))
				for _, v := range point.Values {
					row = append(row, formatRatioValue(v))
				}
				if err := emit(row); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err != nil {
		return "", err
	}
	return outputPath, nil
}

// formatRatioValue formats a single ratio float (NaN/Inf → 空字串,否則 %.6f)。
// Output-2 handler 直接呼叫;Output-1 透過 formatMuscleRatioCell 委派。
func formatRatioValue(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}
	return fmt.Sprintf("%.6f", v)
}

// formatMuscleRatioCell formats one ratio value (與 cci.ExportToCSV row-format
// 同款規則,NaN/Inf → 空字串、否則 %.6f)。Output-1 專用(含 bounds-check)。
func formatMuscleRatioCell(values []float64, idx int) string {
	if idx < 0 || idx >= len(values) {
		return ""
	}
	return formatRatioValue(values[idx])
}

// validateMuscleRatioOutputDir 是 muscle_ratio write path 共用的 defense-in-depth
// 守門 — 對齊既有 muscle_ratio.Analyzer 內 ValidateExternalPath 的位置。
// SubDir 的 traversal 檢查已由 safeJoinOutput 在 caller 端完成;本 helper 只負責
// system-dir prefix 檢查(/etc 等) + 確保目錄存在。
func (h *CSVHandler) validateMuscleRatioOutputDir(subDir string) error {
	checkPath, joinErr := h.safeJoinOutput(subDir, "_validation_marker")
	if joinErr != nil {
		return fmt.Errorf("muscle_ratio 輸出路徑無效: %w", joinErr)
	}
	if err := h.pathValidator.ValidateExternalPath(checkPath); err != nil {
		return fmt.Errorf("muscle_ratio 輸出路徑無效: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(checkPath), fsperm.DirPerm); err != nil {
		return fmt.Errorf("muscle_ratio 輸出目錄建立失敗: %w", err)
	}
	return nil
}

// safeJoinOutput 把 subDir + filename 安全 join 在 OutputDir 之下,拒絕逸出 OutputDir
// 的 SubDir(含 traversal 如 "../evil" 或絕對路徑如 "/etc")。
//
// ADR-0001 invariant 補課:既有 writeToTarget → WriteCSVToOutputDirectory → WriteCSV
// 路徑透過 WriteCSV 內部 path containment 守住 OutputDir 邊界;新加的 direct
// csvutil.WriteCSVAtomic writer(WriteCCIResult / WriteMuscleRatioOutput*)沒走
// WriteCSV,本 helper 把同款邊界檢查補回來,確保 codex review 抓到的 SubDir traversal
// 不會把 *.csv 寫到 OutputDir 外面。
func (h *CSVHandler) safeJoinOutput(subDir, filename string) (string, error) {
	joined := filepath.Join(h.config.OutputDir, subDir, filename)
	rel, err := filepath.Rel(h.config.OutputDir, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: SubDir=%q filename=%q (resolved=%q)",
			errOutputPathEscapesOutputDir, subDir, filename, joined)
	}
	return joined, nil
}

var errEmptyMuscleRatioPayload = stderrors.New("WriteMuscleRatio*: payload 缺 Times/Points")

var errEmptyCCIPhasesPayload = stderrors.New("WriteCCIPhasesResult: payload has no rows")

// errOutputPathEscapesOutputDir 標示 SubDir 含 traversal 或絕對路徑導致 join
// 後的路徑逸出 OutputDir;供 safeJoinOutput / WriteCCIResult / WriteMuscleRatio* wrap。
var errOutputPathEscapesOutputDir = stderrors.New("輸出路徑逸出 OutputDir")

// WriteCCIPhasesResult 寫 per-subject Output 2 — CCI 分期視窗統計 CSV (ADR-0018)。
//
// Filename 由 Subject 推導 + "_CCI_Rudolph_phases.csv";req.Filename 被忽略,
// 僅 req.SubDir 生效。Row 數量 = len(p.Rows);Time cell 在 row.HasTime 為 false
// 時為空字串;NaN/Inf value cell → 空字串。
func (h *CSVHandler) WriteCCIPhasesResult(
	ctx context.Context, req WriteRequest, p CCIOutputPhasesPayload,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if len(p.Rows) == 0 {
		return "", errEmptyCCIPhasesPayload
	}

	fname := filename.SubjectOutputName(p.Subject, "CCI_Rudolph_phases") + ".csv"
	outputPath, joinErr := h.safeJoinOutput(req.SubDir, fname)
	if joinErr != nil {
		return "", fmt.Errorf("CCI 輸出路徑無效: %w", joinErr)
	}

	if err := h.pathValidator.ValidateExternalPath(outputPath); err != nil {
		return "", fmt.Errorf("CCI 輸出路徑無效: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), fsperm.DirPerm); err != nil {
		return "", fmt.Errorf("CCI 輸出目錄建立失敗: %w", err)
	}

	header := []string{"項目", "指標", "Time (s)"}
	header = append(header, p.PairLabels...)

	err := csvutil.WriteCSVAtomic(outputPath, csvutil.SafeWriteOptions{
		Header:    header,
		BasePaths: h.pathValidator.GetAllowedBasePaths(),
		Emit: func(emit func([]string) error) error {
			for _, row := range p.Rows {
				timeCell := ""
				if row.HasTime {
					timeCell = fmt.Sprintf("%.4f", row.Time)
				}
				cells := make([]string, 0, 3+len(row.Values))
				cells = append(cells, row.Item, row.Metric, timeCell)
				for _, v := range row.Values {
					cells = append(cells, formatRatioValue(v))
				}
				if err := emit(cells); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err != nil {
		return "", err
	}
	return outputPath, nil
}

// WriteNormalizedPhaseSyncEMG 把 normalized phase-sync EMG 時序資料寫成 CSV
// (Output 1 — 時序型 EMG,非統計摘要)。
//
// Filename: {sanitize(subject)}_normalized.csv (req.Filename 被忽略,僅 req.SubDir 生效)。
// 回傳實際 outputPath (絕對路徑) 與錯誤。
//
// row layout: header row (Time + data.Headers 順序) + 每個時間點一列。
// NaN/Inf → 空 cell (Output 1 missing-data 慣例;勿與 Output 2 ConvertPhaseSyncResult
// 的 fmt.Sprintf("%.6f") 合併 — 後者寫出 "NaN" 字面)。
// precision = phaseSyncPrecision = 6 (常數,無 precision 參數 — 與 Output 2 共享常數,不共享 formatter)。
//
// 路徑由 phaseSyncAtomicWrite 守門 + WriteCSVAtomic tmp+rename atomic 寫入 —
// ADR-0001 invariant: Subject-based write ⟹ WriteCSVAtomic。
func (h *CSVHandler) WriteNormalizedPhaseSyncEMG(
	req WriteRequest,
	data *models.PhaseSyncEMGData,
	subject string,
) (string, error) {
	if data == nil {
		return "", errEmptyPhaseSyncEMGData
	}

	fname := fmt.Sprintf("%s_normalized.csv", filename.Sanitize(subject))

	emit := func(write func(row []string) error) error {
		for i := range data.Time {
			row := make([]string, 0, len(data.Headers)+1)
			row = append(row, formatNormalizedEMGCell(data.Time[i]))

			for _, name := range data.Headers {
				channel := data.Channels[name]

				if i >= len(channel) {
					row = append(row, "")
					continue
				}

				row = append(row, formatNormalizedEMGCell(channel[i]))
			}

			if err := write(row); err != nil {
				return fmt.Errorf("寫入第 %d 列失敗: %w", i+1, err)
			}
		}
		return nil
	}

	return h.phaseSyncAtomicWrite(req.SubDir, fname, buildEMGCSVHeader(data.Headers), emit)
}

// buildEMGCSVHeader 組合 EMG CSV 標頭：`Time` + 各肌肉名稱。
// 從 parsers.emg_writer.go 搬入 (cross-package 同名不衝突);package io 側使用。
func buildEMGCSVHeader(channelHeaders []string) []string {
	header := make([]string, 0, len(channelHeaders)+1)
	header = append(header, "Time")
	header = append(header, channelHeaders...)

	return header
}

// formatNormalizedEMGCell 將浮點數格式化為 EMG 時序 CSV cell。
// NaN/Inf → 空字串（Output 1 missing-data 慣例）。
// 精度固定為 phaseSyncPrecision（=6），來源為 csv_converter.go 常數，
// 與 Output 2 共享常數而非共享 formatter（Output 2 走 fmt.Sprintf 會寫 "NaN" 字面）。
func formatNormalizedEMGCell(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return ""
	}

	return strconv.FormatFloat(v, 'f', phaseSyncPrecision, 64)
}
