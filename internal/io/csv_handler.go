// Package io provides file input/output operations for the EMG data analysis
// application, including CSV reading and writing.
package io

import (
	"bufio"
	"context"
	"encoding/csv"
	stderrors "errors"
	"fmt"
	stdio "io" // alias to avoid name shadow with package io
	"io/fs"
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
	csvvalidator "count_mean/internal/validation/csv"
	"count_mean/internal/validation/filename"
)

// csvReaderBufSize 是 bufio.Reader 的初始 buffer 大小（64 KiB）。
// 與 internal/parsers 套件的同名常數對齊；保留 unexported 在各套件各自定義，
// 避免讓 io 套件直接 reach into parsers 內部。
const csvReaderBufSize = 64 * 1024

// CSVHandler 處理 CSV 檔案讀寫.
type CSVHandler struct {
	config            *config.AppConfig
	pathValidator     *security.PathValidator
	csvValidator      *csvvalidator.Validator
	filenameValidator *filename.Validator
	logger            *logging.Logger
	converter         *csvConverter
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

	return &CSVHandler{
		config:            cfg,
		pathValidator:     pathValidator,
		csvValidator:      csvvalidator.NewValidator(),
		filenameValidator: filename.NewValidator(),
		logger:            logging.GetLogger("csv_handler"),
		converter:         newCSVConverter(calculator.NewUnitScale(cfg.ScalingFactor), cfg.Precision),
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
	fileName = ensureCSVExtension(fileName)
	fullPath := filepath.Join(h.config.InputDir, dirName, fileName)

	return h.ReadCSV(fullPath)
}

// parseCSV 解析已開啟的 CSV 檔案(單次 ReadAll)。path 只用於 log。
//
// 讀取以 LimitReader 封頂在 maxReadCSVBytes+1:fstat 之後檔案若又長大,
// 多讀到的那 1 byte 會被視為超過上限,不會無界吃進記憶體。
func (h *CSVHandler) parseCSV(file *os.File, path string) ([][]string, error) {
	limited := &stdio.LimitedReader{R: file, N: maxReadCSVBytes + 1}
	// 用 bufio 包 *os.File 避免 csv.Reader 每次 Read 都觸發 syscall（大檔差異明顯）。
	// BOM 處理：Excel 匯出的 UTF-8 CSV 常帶 0xEF 0xBB 0xBF 前綴。若不剝除，
	// records[0][0] 會帶 U+FEFF，污染後續以 header 字串比對 channel 名稱的路徑，
	// header 寫進輸出 CSV 時也會帶出 user-visible 怪字元。
	// 與 internal/parsers/csv_reader.go 對稱：先 bufio.NewReaderSize 再 PeekBOM
	// 再 csv.NewReader（PeekBOM 在 < 3 bytes 輸入時視為「無 BOM」回 nil，
	// 不會把空檔的 EOF 提前丟出）。
	bufReader := bufio.NewReaderSize(limited, csvReaderBufSize)
	if _, err := csvutil.PeekBOM(bufReader); err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeDataParsing, "BOM 偵測失敗")
		h.logger.Error("BOM 偵測失敗", appErr, map[string]any{"path": path})

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
	if limited.N == 0 {
		return nil, h.tooLargeError(path, maxReadCSVBytes+1)
	}

	if err != nil {
		appErr := errors.WrapError(err, errors.ErrCodeDataParsing, "無法讀取 CSV 資料")
		h.logger.Error("CSV 資料讀取失敗", appErr, map[string]any{"path": path})

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

	if err := h.csvValidator.ValidateCSVData(records, cleanPath); err != nil {
		h.logger.Error("CSV 資料結構驗證失敗", err, map[string]any{"path": cleanPath})

		return fmt.Errorf("CSV 資料驗證失敗: %w", err)
	}

	return nil
}

// maxReadCSVBytes 是 ReadCSV 的檔案大小上限(100 MB)。
//
// csv.Reader.ReadAll 把整檔 materialize 成 [][]string,實測 memory 約為 source bytes 的
// 4-10x;200MB source → 800MB-2GB heap 對 GUI process 是 OOM 風險。典型 EMG CSV 1-50MB,
// 100MB 已是極端 outlier;超過一律拒絕(streaming 路徑已刪,見 ADR-0033)。
const maxReadCSVBytes = 100 * 1024 * 1024

// ReadCSV 是使用者選取 CSV 的唯一讀取入口。
//
// 流程:base filename 驗證(ValidateFilename:檔名規則 + .csv 副檔名,ADR-0054)→
// ValidateExternalPath(任意絕對路徑皆可,仍擋 traversal 與系統敏感位置)→
// OpenFile(fsperm.ReadFlags,O_NOFOLLOW)→ fstat(必須是 regular file 且 ≤ 100MB)→
// 單次解析 → validateCSVRecords。
//
// 路徑不做 URL-decode,`%`、`+` 皆為字面檔名字元。
func (h *CSVHandler) ReadCSV(path string) ([][]string, error) {
	h.logger.Debug("開始讀取 CSV 檔案", map[string]any{"filename": path})

	if err := h.filenameValidator.ValidateFilename(filepath.Base(path)); err != nil {
		h.logger.Error("檔案名稱驗證失敗", err, map[string]any{"filename": path})

		return nil, fmt.Errorf("檔案名稱驗證失敗: %w", err)
	}

	if err := h.pathValidator.ValidateExternalPath(path); err != nil {
		h.logger.Error("路徑驗證失敗", err, map[string]any{"filename": path})

		return nil, fmt.Errorf("路徑驗證失敗: %w", err)
	}

	// open 前先 stat 擋非 regular(FIFO 等會讓 OpenFile 阻塞)。stat 失敗不在此報錯,
	// 交給 OpenFile 產生既有的錯誤訊息;open 後的 file.Stat() 才是權威檢查。
	if pre, statErr := os.Stat(path); statErr == nil && !pre.Mode().IsRegular() {
		return nil, h.notRegularError(path)
	}

	file, err := os.OpenFile(path, fsperm.ReadFlags, 0) //nolint:gosec // path 已過 ValidateExternalPath;fsperm.ReadFlags 加 O_NOFOLLOW(與 WriteFlags 對稱)
	if err != nil {
		appErr := openFailure(err)
		h.logger.Error("檔案開啟失敗", appErr, map[string]any{"path": path})

		return nil, appErr
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉檔案時發生錯誤", map[string]any{
				"file": file.Name(), "error": closeErr.Error(),
			})
		}
	}()

	// fstat 作用在已開啟的 fd 上,檢查與讀取是同一個 inode(無 stat-then-open TOCTOU)。
	info, err := file.Stat()
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeFileNotFound, "無法獲取文件信息")
	}

	if !info.Mode().IsRegular() {
		return nil, h.notRegularError(path)
	}

	if info.Size() > maxReadCSVBytes {
		return nil, h.tooLargeError(path, info.Size())
	}

	records, err := h.parseCSV(file, path)
	if err != nil {
		return nil, err
	}

	if err := h.validateCSVRecords(records, path); err != nil {
		return nil, err
	}

	h.logger.Info("CSV 檔案讀取成功", map[string]any{
		"path": path, "record_count": len(records), "column_count": len(records[0]),
	})

	return records, nil
}

// notRegularError 建立「不是一般檔案」錯誤(目錄、FIFO、device 等)。
func (h *CSVHandler) notRegularError(path string) *errors.AppError {
	appErr := errors.NewAppErrorWithDetails(
		errors.ErrCodeFileFormat, "檔案格式無效",
		fmt.Sprintf("'%s' 不是一般檔案", path),
	)
	h.logger.Error("檔案類型驗證失敗", appErr, map[string]any{"path": path})

	return appErr
}

// tooLargeError 建立超過 100MB 的錯誤。MB 數無條件進位,超過上限時一律顯示 >= 101,
// 避免「100 MB，上限 100 MB」的矛盾訊息。
func (h *CSVHandler) tooLargeError(path string, size int64) *errors.AppError {
	const mb = 1024 * 1024

	h.logger.Info("檢測到大文件，拒絕讀取", map[string]any{"filename": path, "file_size": size})

	return errors.NewAppErrorWithDetails(
		errors.ErrCodeFileTooLarge,
		fmt.Sprintf("檔案過大（%d MB，上限 100 MB），請分割檔案後再試", (size+mb-1)/mb),
		fmt.Sprintf("文件 %s 過大 (至少 %d bytes)，超過 100 MB 上限", path, size),
	)
}

// openFailure 把 OpenFile 錯誤轉成 AppError。不存在沿用既有的「無法獲取文件信息」訊息。
func openFailure(err error) *errors.AppError {
	if stderrors.Is(err, os.ErrNotExist) {
		return errors.WrapError(err, errors.ErrCodeFileNotFound, "無法獲取文件信息")
	}

	return errors.WrapError(err, errors.ErrCodeFileNotFound, "無法開啟檔案")
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

// WriteRequest 是 File-based write(WriteMaxMean / WriteNormalized / WritePhaseAnalysis /
// WriteFileCSV)的目的地請求。Subject-based writer 不收它:檔名由 subject 推導,一律落在
// OutputDir 根。
//
// Filename 是 CSV 檔名;SubDir 為空時直接寫到 OutputDir 根,非空時(MaxMean 批次)
// MkdirAll(OutputDir/SubDir) 後寫到該子目錄。File-based writer 一律經 WriteFileCSV
// 落檔並回傳實際寫入的路徑。
type WriteRequest struct {
	Filename string
	SubDir   string
}

// WriteMaxMean 把 MaxMean 計算結果寫成 6-row CSV (header / startRange / endRange /
// startTime / endTime / maxMean)。
//
// row layout、scaling (config.ScalingFactor 推導的 calculator.UnitScale)、precision
// (config.Precision) 由 implementation 持有;caller 不再呼叫 Convert* 後組裝
// [][]string。落檔經 WriteFileCSV(BOM、formula-injection sanitize、atomic 寫入)。
func (h *CSVHandler) WriteMaxMean(
	req WriteRequest,
	headers []string,
	results []models.MaxMeanResult,
	startRange, endRange float64,
) (string, error) {
	data := h.converter.ConvertMaxMeanResults(headers, results, startRange, endRange)

	return h.WriteFileCSV(req, data)
}

// WriteNormalized 把標準化後的 EMGDataset 寫成 CSV (1 header + N data rows)。
//
// Time 欄套用 dataset.OriginalTimePrecision,data 欄套用 config.Precision —
// 這條「time vs data 不同 precision」的內部規則 caller 不會看到。
func (h *CSVHandler) WriteNormalized(req WriteRequest, dataset *models.EMGDataset) (string, error) {
	data := h.converter.ConvertNormalizedData(dataset)

	return h.WriteFileCSV(req, data)
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

	return h.WriteFileCSV(req, data)
}

// errEmptyFileCSV 標示 WriteFileCSV 收到 0 列:至少要有 data[0] 當標題列。
var errEmptyFileCSV = stderrors.New("WriteFileCSV: 資料不可為空(至少要有標題列)")

// WriteFileCSV 是 File-based write 的單一寫門:caller 給檔名(req.Filename)與 rows
// (data[0] 為標題列,data[1:] 逐列輸出),經 placeOutput 落檔,回傳「實際寫入的路徑」,
// caller 不得自行重組。WriteMaxMean / WriteNormalized / WritePhaseAnalysis 都經過這裡。
//
// req.Filename 可帶子路徑(Normalize 的自由輸入,如 "sub/out.csv" 寫到 OutputDir/sub/out.csv);
// 檔名規則只驗 join 後的 base,逸出 OutputDir 的名稱被拒。
// 路徑不做 URL-decode:`%`、`+` 皆為字面檔名字元([[Format-aware write]])。
func (h *CSVHandler) WriteFileCSV(req WriteRequest, data [][]string) (string, error) {
	if len(data) == 0 {
		return "", errEmptyFileCSV
	}

	return h.placeOutput(req.SubDir, req.Filename, h.csvPayload(data[0], emitRows(data[1:])))
}

// placeOutput 是所有輸出落檔的單一步驟([[Subject output placement]],File-based 與
// Subject-based 共用),依序:
//  1. 命名:caller 給 name —— File-based 是 req.Filename;Subject-based 是
//     filename.SubjectOutputName(subject, suffix)+".csv"(見 placeSubjectOutput)。
//  2. safeJoinOutput(OutputDir/subDir/name,containment)。join 後以
//     ValidateFilename(Base(path)) 驗檔名;Subject 檔名恆通過(ADR-0054 property 測試)。
//  3. ValidateExternalPath(敏感位置,含 symlink resolve)。
//  4. MkdirAll —— 先驗證、後建目錄,被拒的路徑不留下目錄。
//  5. fsperm.AtomicWriteFile(path, GetAllowedBasePaths(), payload):tmp → fsync → rename,
//     parent 以 dirfd 錨定在 allow-list 之下。leaf 若是 symlink,rename 取代連結本身,
//     不跟到目標;parent 是逸出 base 的 symlink 則拒寫。
//
// 回傳「實際寫入的路徑」。錯誤文字為「輸出路徑無效」「檔案名稱驗證失敗」「輸出目錄建立失敗」,
// 以 %w 包底層 error。各分支的 PHI 性質:
//   - containment 失敗(errOutputPathEscapesOutputDir):固定哨兵,不帶 SubDir / 目錄路徑。
//   - 檔名驗證失敗:validator error 帶檔名(base),不帶目錄。
//   - MkdirAll 失敗:只包底層 errno(去掉 *fs.PathError 的路徑),不帶目錄路徑。
//   - ValidateExternalPath 失敗:以 %w 包 validator error,可帶輸出「檔案」路徑
//     (webview sink 只保留檔名,見 ADR-0036 D5)。
//   - AtomicWriteFile 失敗:原樣回傳底層 error,不另加前綴。
func (h *CSVHandler) placeOutput(subDir, name string, payload func(stdio.Writer) error) (string, error) {
	outputPath, err := h.safeJoinOutput(subDir, name)
	if err != nil {
		return "", fmt.Errorf("輸出路徑無效: %w", err)
	}

	if err := h.filenameValidator.ValidateFilename(filepath.Base(outputPath)); err != nil {
		return "", fmt.Errorf("檔案名稱驗證失敗: %w", err)
	}

	if err := h.pathValidator.ValidateExternalPath(outputPath); err != nil {
		return "", fmt.Errorf("輸出路徑無效: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), fsperm.DirPerm); err != nil {
		// *fs.PathError 的文字帶目錄路徑(末段可能是病患資料夾名),只包底層 errno,
		// errors.Is(err, fs.ErrPermission) 等比對仍成立。
		var pe *fs.PathError
		if stderrors.As(err, &pe) {
			err = pe.Err
		}
		return "", fmt.Errorf("輸出目錄建立失敗: %w", err)
	}

	if err := fsperm.AtomicWriteFile(outputPath, h.pathValidator.GetAllowedBasePaths(), payload); err != nil {
		return "", err
	}

	return outputPath, nil
}

// csvPayload 是 CSV 的 payload adapter:BOM 照 config.BOMEnabled(io 是唯一讀這個設定的地方),
// 標題列與資料列的 formula-injection sanitize 由 csvutil.CSVPayload 持有。
func (h *CSVHandler) csvPayload(header []string, emit csvutil.RowEmitter) func(stdio.Writer) error {
	return csvutil.CSVPayload(csvutil.SafeWriteOptions{
		Header:  header,
		Emit:    emit,
		OmitBOM: !h.config.BOMEnabled,
	})
}

// emitRows 把現成的 rows 逐列交給 emit。
func emitRows(rows [][]string) csvutil.RowEmitter {
	return func(emit func([]string) error) error {
		for _, row := range rows {
			if err := emit(row); err != nil {
				return err
			}
		}
		return nil
	}
}

// placeSubjectOutput 是 Subject-based write 的命名:輸出檔名 =
// filename.SubjectOutputName(subject, suffix) + ".csv"(subject 內部強制 Sanitize),
// 其餘交給 placeOutput。
//
// ADR-0016 invariant「Subject-based write ⟹ atomic 寫入 + BasePaths」(ADR-0040)在此結構化:
// 7 個 Subject-based writer 只持有 row layout,沒有任何一個能繞過 placement。
func (h *CSVHandler) placeSubjectOutput(
	subject, suffix string,
	header []string,
	emit csvutil.RowEmitter,
) (string, error) {
	return h.placeOutput("", filename.SubjectOutputName(subject, suffix)+".csv", h.csvPayload(header, emit))
}

// placeSubjectRows 把 data[][]string(data[0] 為 header)交給 placeSubjectOutput 串流寫出。
// ConvertPhaseSyncResult 恆回固定 8-row layout,故 data 至少 1 row,data[0] 不會 panic。
func (h *CSVHandler) placeSubjectRows(subject, suffix string, data [][]string) (string, error) {
	return h.placeSubjectOutput(subject, suffix, data[0], emitRows(data[1:]))
}

// WritePhaseSyncResult 把 PhaseSync 分析結果 (EMGStatistics) 寫成 CSV。
//
// Filename 由 filename.SubjectOutputName(Subject, "{StartPhase}-{EndPhase}_statistics")
// 自動生成,落在 OutputDir 根。回傳實際 outputPath (形如 OutputDir/<filename>) 與錯誤。
//
// row layout (8-row: header / 開始分期點 / 開始時間 / 結束分期點 / 結束時間 /
// 時間差值 / 平均值 / 最大值)、precision (phaseSyncPrecision=6) 由 implementation 持有。
// 路徑由 placeSubjectOutput 守門 + tmp+rename atomic 寫入 —
// ADR-0016 invariant: Subject-based write ⟹ atomic 寫入。
func (h *CSVHandler) WritePhaseSyncResult(stats *models.EMGStatistics) (string, error) {
	if stats == nil {
		return "", errEmptyPhaseSyncResult
	}

	suffix := fmt.Sprintf("%s-%s_statistics", stats.StartPhase, stats.EndPhase)

	return h.placeSubjectRows(stats.Subject, suffix, h.converter.ConvertPhaseSyncResult(stats))
}

// WriteNormalizedPhaseSyncResult 把 normalized PhaseSync 分析結果寫成 CSV。
//
// Filename 由 stats.Subject 經 filename.Sanitize 後與 normStart/normEnd/stats 分期
// 自動推導,落在 OutputDir 根。
// Filename template: {safeSubject}_normalized_norm-{normStart}-{normEnd}_stats-{StartPhase}-{EndPhase}.csv
// (對齊 GUI 現行 template)。
//
// row layout 與 WritePhaseSyncResult 相同 (8-row,由 ConvertPhaseSyncResult 持有)。
// 路徑由 placeSubjectOutput 守門 + tmp+rename atomic 寫入 —
// ADR-0016 invariant: Subject-based write ⟹ atomic 寫入。
func (h *CSVHandler) WriteNormalizedPhaseSyncResult(
	stats *models.EMGStatistics,
	normStart, normEnd models.PhasePoint,
) (string, error) {
	if stats == nil {
		return "", errEmptyPhaseSyncResult
	}

	suffix := fmt.Sprintf("normalized_norm-%s-%s_stats-%s-%s",
		normStart, normEnd, stats.StartPhase, stats.EndPhase)

	return h.placeSubjectRows(stats.Subject, suffix, h.converter.ConvertPhaseSyncResult(stats))
}

// WriteCCIResult 把 CCI 分析結果寫成 CSV。
//
// Filename 由 result.Subject 經 filename.Sanitize 後 + "_CCI_Rudolph.csv" suffix
// 推導,落在 OutputDir 根。回傳實際 outputPath 與錯誤。
//
// row layout: 1 header row ["Time (s)", "Gait Cycle (%)", PairName...]
//
//   - N data rows,每 row 含 [time, gait_pct, pair_value...]
//
// NaN/Inf cell → 空字串;整 row 所有 pair 都 NaN/Inf → skip 整 row(計入 droppedRowCount)。
//
// ctx 為第一個參數,sample loop 中每 cciStreamCtxCheckInterval 點檢查一次 ctx.Done()
// — caller cancel 後立即停寫並回 ctx.Err。placement 的 atomic 寫入對 emit 回 error 走
// tmp file abort 路徑,不留下半成品。
//
// ADR-0016 invariant: Subject-based write ⟹ atomic 寫入 + BasePaths,經 placeSubjectOutput
// 落檔(ADR-0040);pathValidator 守門覆蓋原 cci.ExportToCSV 缺少的 defense-in-depth
// (原路徑走 security.NewPathValidator(nil) 未整合 CSVHandler 既有 allowedPaths;
// 由本 method 統一補上)。
func (h *CSVHandler) WriteCCIResult(ctx context.Context, result *cci.CCIAnalysisResult) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// ADR-0053:不變式(非 nil / gait 區間有限且 duration > 0 / pair 長度對齊)由結果自己持有。
	if err := result.Validate(); err != nil {
		return "", err
	}

	header := []string{"Time (s)", "Gait Cycle (%)"}
	for _, pr := range result.PairResults {
		header = append(header, pr.PairName)
	}

	var droppedRowCount int
	numPoints := len(result.TimeValues)

	outputPath, err := h.placeSubjectOutput(result.Subject, "CCI_Rudolph", header,
		func(emit func([]string) error) error {
			for i := 0; i < numPoints; i++ {
				if i > 0 && i%cciStreamCtxCheckInterval == 0 {
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}
				}

				t := result.TimeValues[i]
				pct := result.GaitPercent(t)

				pairCells := make([]string, 0, len(result.PairResults))
				allNonFinite := true

				for _, pr := range result.PairResults {
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
// Filename 由 Subject 經 filename.Sanitize 後 + "_muscle_ratio.csv" 推導,落在 OutputDir 根。
//
// row layout: 1 header ["Time (s)", PairLabels...] + N data rows。
// NaN/Inf cell → 空字串。
func (h *CSVHandler) WriteMuscleRatioOutputAll(p MuscleRatioOutputAllPayload) (string, error) {
	if len(p.Times) == 0 {
		return "", errEmptyMuscleRatioPayload
	}

	header := make([]string, 0, 1+len(p.PairLabels))
	header = append(header, "Time (s)")
	header = append(header, p.PairLabels...)

	return h.placeSubjectOutput(p.Subject, "muscle_ratio", header,
		func(emit func([]string) error) error {
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
		})
}

// WriteMuscleRatioOutputPhases 寫 per-subject Output 2 — phase+midpoint slice CSV。
//
// Filename 由 Subject 推導 + "_muscle_ratio_phases_avg11.csv",落在 OutputDir 根。
// Row 數量由 caller (muscle_ratio.Analyzer) 預先決定的 Points 切片長度決定。
// Analyzer 已算好 window mean 值,handler 純 layout emit。(ADR-0014)
func (h *CSVHandler) WriteMuscleRatioOutputPhases(p MuscleRatioOutputPhasesPayload) (string, error) {
	if len(p.Points) == 0 {
		return "", errEmptyMuscleRatioPayload
	}

	header := make([]string, 0, 2+len(p.PairLabels))
	header = append(header, "Phase", "Time (s)")
	header = append(header, p.PairLabels...)

	return h.placeSubjectOutput(p.Subject, "muscle_ratio_phases_avg11", header,
		func(emit func([]string) error) error {
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
		})
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

// safeJoinOutput 把 subDir + filename 安全 join 在 OutputDir 之下,拒絕逸出 OutputDir
// 的 SubDir(如 traversal "../evil")。絕對 SubDir(如 "/etc")經 filepath.Join 被當成相對片段,
// 結果落在 OutputDir/etc 之內,並非逸出;其敏感位置由 ValidateExternalPath 把關。
//
// ADR-0016 / ADR-0040:placeOutput(File-based 與 Subject-based 共用)經本 helper
// 守住 OutputDir 邊界,確保 SubDir / 檔名 traversal 不會把 *.csv 寫到 OutputDir 外面。
func (h *CSVHandler) safeJoinOutput(subDir, filename string) (string, error) {
	joined := filepath.Join(h.config.OutputDir, subDir, filename)
	if !fsperm.IsWithin(h.config.OutputDir, joined) {
		return "", errOutputPathEscapesOutputDir
	}
	return joined, nil
}

var errEmptyMuscleRatioPayload = stderrors.New("WriteMuscleRatio*: payload 缺 Times/Points")

var errEmptyCCIPhasesPayload = stderrors.New("WriteCCIPhasesResult: payload has no rows")

// errOutputPathEscapesOutputDir 標示 SubDir 含 traversal 導致 join
// 後的路徑逸出 OutputDir;供 safeJoinOutput 回傳、placeOutput wrap。
var errOutputPathEscapesOutputDir = stderrors.New("輸出路徑逸出 OutputDir")

// WriteCCIPhasesResult 寫 per-subject Output 2 — CCI 分期視窗統計 CSV (ADR-0018)。
//
// 直接吃 *cci.CCIAnalysisResult(與 WriteCCIResult 對稱):pair 欄位 header 由
// result.PairResults 推導、資料列由 result.PhaseStats 推導(ADR-0025)。
// Filename 由 result.Subject 推導 + "_CCI_Rudolph_phases.csv",落在 OutputDir 根。
// Row 數量 = len(result.PhaseStats);Time cell 在 row.HasTime
// 為 false 時為空字串;NaN/Inf value cell → 空字串。
func (h *CSVHandler) WriteCCIPhasesResult(ctx context.Context, result *cci.CCIAnalysisResult) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if result == nil || len(result.PhaseStats) == 0 {
		return "", errEmptyCCIPhasesPayload
	}

	header := []string{"項目", "指標", "Time (s)"}
	for _, pr := range result.PairResults {
		header = append(header, pr.PairName)
	}

	return h.placeSubjectOutput(result.Subject, "CCI_Rudolph_phases", header,
		func(emit func([]string) error) error {
			for _, row := range result.PhaseStats {
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
		})
}

// WriteNormalizedPhaseSyncEMG 把 normalized phase-sync EMG 時序資料寫成 CSV
// (Output 1 — 時序型 EMG,非統計摘要)。
//
// Filename: {sanitize(subject)}_normalized.csv,落在 OutputDir 根。
// 回傳實際 outputPath 與錯誤。
//
// row layout: header row (Time + data.Headers 順序) + 每個時間點一列。
// NaN/Inf → 空 cell (Output 1 missing-data 慣例;勿與 Output 2 ConvertPhaseSyncResult
// 的 fmt.Sprintf("%.6f") 合併 — 後者寫出 "NaN" 字面)。
// precision = phaseSyncPrecision = 6 (常數,無 precision 參數 — 與 Output 2 共享常數,不共享 formatter)。
//
// 路徑由 placeSubjectOutput 守門 + tmp+rename atomic 寫入 —
// ADR-0016 invariant: Subject-based write ⟹ atomic 寫入。
func (h *CSVHandler) WriteNormalizedPhaseSyncEMG(data *models.PhaseSyncEMGData, subject string) (string, error) {
	if data == nil {
		return "", errEmptyPhaseSyncEMGData
	}

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

	return h.placeSubjectOutput(subject, "normalized", buildEMGCSVHeader(data.Headers), emit)
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
