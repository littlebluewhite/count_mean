package io

import (
	"bufio"
	"encoding/csv"
	stderrors "errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"time"

	"count_mean/internal/config"
	"count_mean/internal/csvutil"
	"count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/security"
	"count_mean/internal/security/fsperm"
	"count_mean/internal/validation"
	"count_mean/util"
)

// Buffer size constants.
const (
	kilobyte            = 1024
	defaultBufferSizeKB = 64
	fullProgress        = 100.0
)

// Static errors for err113 compliance.
var errDataRowTooShort = stderrors.New("數據行長度不足")

// ProgressCallback 進度回調函數類型.
type ProgressCallback func(processed, total int64, percentage float64)

// LargeFileHandler 處理大文件的結構.
type LargeFileHandler struct {
	config        *config.AppConfig
	pathValidator *security.PathValidator
	validator     *validation.InputValidator
	logger        *logging.Logger

	// 大文件處理配置
	chunkSize   int   // 每次處理的行數
	memoryLimit int64 // 記憶體限制 (bytes)
	bufferSize  int   // 讀取緩衝區大小
	maxFileSize int64 // 最大文件大小 (bytes)

	// 緩衝區池
	bufferPool *BufferPool
}

// NewLargeFileHandler 創建大文件處理器.
func NewLargeFileHandler(config *config.AppConfig) *LargeFileHandler {
	allowedPaths := []string{
		config.InputDir,
		config.OutputDir,
		config.OperateDir,
	}

	return &LargeFileHandler{
		config:        config,
		pathValidator: security.NewPathValidator(allowedPaths),
		validator:     validation.NewInputValidator(),
		logger:        logging.GetLogger("large_file_handler"),

		// 預設配置
		chunkSize:   1000,                               // 每次處理1000行
		memoryLimit: 512 * kilobyte * kilobyte,          // 512MB 記憶體限制
		bufferSize:  defaultBufferSizeKB * kilobyte,     // 64KB 緩衝區
		maxFileSize: 2 * kilobyte * kilobyte * kilobyte, // 2GB 最大文件大小

		// 緩衝區池
		bufferPool: NewBufferPool(),
	}
}

// FileInfo 文件信息.
type FileInfo struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	LineCount   int64  `json:"line_count"`
	ColumnCount int    `json:"column_count"`
	IsLarge     bool   `json:"is_large"`
}

// StreamingResult 流式處理結果.
type StreamingResult struct {
	ProcessedLines int64                  `json:"processed_lines"`
	TotalLines     int64                  `json:"total_lines"`
	Results        []models.MaxMeanResult `json:"results"`
	Headers        []string               `json:"headers"`
	StartTime      time.Time              `json:"start_time"`
	EndTime        time.Time              `json:"end_time"`
	Duration       time.Duration          `json:"duration"`
	MemoryUsed     int64                  `json:"memory_used"`
}

// GetFileInfo 執行基本安全檢查（路徑遍歷攻擊防護），支援任意路徑的檔案.
func (h *LargeFileHandler) GetFileInfo(filename string) (*FileInfo, error) {
	h.logger.Debug("開始獲取文件信息", map[string]interface{}{
		"filename": filename,
	})

	// 清理路徑
	sanitizedPath := h.pathValidator.SanitizePath(filename)

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
		Path:    absPath,
		Size:    fileInfo.Size(),
		IsLarge: fileInfo.Size() > h.maxFileSize/10, // 超過200MB視為大文件
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
	lineCount, columnCount, err := h.scanFileStructure(absPath)
	if err != nil {
		return nil, err
	}

	info.LineCount = lineCount
	info.ColumnCount = columnCount

	h.logger.Info("文件信息獲取完成", map[string]interface{}{
		"file_size":    info.Size,
		"line_count":   info.LineCount,
		"column_count": info.ColumnCount,
		"is_large":     info.IsLarge,
	})

	return info, nil
}

// scanFileStructure 快速掃描文件結構.
func (h *LargeFileHandler) scanFileStructure(filename string) (int64, int, error) {
	file, err := os.OpenFile(filename, fsperm.ReadFlags, 0) //nolint:gosec // filename sanitized and validated; fsperm.ReadFlags adds O_NOFOLLOW (symmetric with WriteFlags)
	if err != nil {
		return 0, 0, fmt.Errorf("無法開啟文件 %s: %w", filename, err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			h.logger.Warn("關閉文件時發生錯誤", map[string]interface{}{
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
		return 0, 0, fmt.Errorf("BOM 偵測失敗 %s: %w", filename, err)
	}
	reader := csv.NewReader(bufReader)

	// 讀取第一行獲取列數
	firstRow, err := reader.Read()
	if err != nil {
		if stderrors.Is(err, io.EOF) {
			return 0, 0, nil
		}

		return 0, 0, fmt.Errorf("讀取文件標題行失敗: %w", err)
	}

	columnCount := len(firstRow)
	lineCount := int64(1)

	// 計算剩餘行數
	for {
		_, err := reader.Read()
		if stderrors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			h.logger.Warn("掃描文件時遇到錯誤，繼續處理", map[string]interface{}{
				"error": err.Error(),
				"line":  lineCount,
			})

			continue
		}

		lineCount++
	}

	return lineCount, columnCount, nil
}

// streamingContext 流式處理上下文.
type streamingContext struct {
	reader           *csv.Reader
	headers          []string
	fileInfo         *FileInfo
	processedLines   int64
	progressInterval int64
	lastProgress     int64
	callback         ProgressCallback
}

// recordProcessor 記錄處理器函數類型.
type recordProcessor func(ctx *streamingContext, record []string) error

// processStreamingFile 通用流式文件處理.
func (h *LargeFileHandler) processStreamingFile(
	filename string,
	callback ProgressCallback,
	processor recordProcessor,
	resultBuilder func(*streamingContext) []models.MaxMeanResult,
) (*StreamingResult, error) {
	startTime := time.Now()

	// 獲取文件信息
	fileInfo, err := h.GetFileInfo(filename)
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(fileInfo.Path, fsperm.ReadFlags, 0) //nolint:gosec // fileInfo.Path resolved from validated input; fsperm.ReadFlags adds O_NOFOLLOW
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeFileNotFound, "無法開啟文件")
	}

	defer h.closeFileWithLog(file, fileInfo.Path)

	// BOM 處理: 對稱於 scanFileStructure。Excel UTF-8 CSV 的 0xEF 0xBB 0xBF
	// 前綴若不剝除會污染 ctx.headers[0] 進而干擾 channel 名稱對映。
	bufReader := bufio.NewReaderSize(file, h.bufferSize)
	if _, err := csvutil.PeekBOM(bufReader); err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeDataParsing, "BOM 偵測失敗")
	}
	reader := csv.NewReader(bufReader)

	// 讀取標題行
	headers, err := reader.Read()
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeDataParsing, "無法讀取標題行")
	}

	ctx := &streamingContext{
		reader:           reader,
		headers:          headers,
		fileInfo:         fileInfo,
		processedLines:   1, // 已處理標題行
		progressInterval: int64(h.chunkSize),
		lastProgress:     0,
		callback:         callback,
	}

	// 執行流式處理
	if err := h.executeStreamingLoop(ctx, processor); err != nil {
		return nil, err
	}

	// 最終進度回調
	h.reportFinalProgress(ctx)

	// 構建結果
	result := &StreamingResult{
		Headers:        headers,
		StartTime:      startTime,
		TotalLines:     fileInfo.LineCount,
		ProcessedLines: ctx.processedLines,
		EndTime:        time.Now(),
		MemoryUsed:     h.getMemoryUsage(),
	}
	result.Duration = result.EndTime.Sub(startTime)

	if resultBuilder != nil {
		result.Results = resultBuilder(ctx)
	} else {
		result.Results = make([]models.MaxMeanResult, 0)
	}

	h.logger.Info("流式處理完成", map[string]interface{}{
		"processed_lines": ctx.processedLines,
		"duration_ms":     result.Duration.Milliseconds(),
		"memory_used_mb":  result.MemoryUsed / 1024 / 1024,
	})

	return result, nil
}

// executeStreamingLoop 執行流式處理循環.
//
// 記憶體壓力檢查改在 reportProgressIfNeeded 內進行（每 progressInterval 筆一次）
// 而非每筆 row 都跑：checkMemoryUsage 內部呼叫 runtime.ReadMemStats，是 STW-ish
// 操作，30M-row 大檔每筆都跑會把 O(n×channels) 演算法的成本完全抵銷
// （Wave 6 review P1 — code-debugger 抓到，benchmark 證實）。
// 入口先做一次 cold-start check，避免起始時就已超出 memoryLimit 仍進入 loop。
func (h *LargeFileHandler) executeStreamingLoop(ctx *streamingContext, processor recordProcessor) error {
	if err := h.checkMemoryUsage(); err != nil {
		return fmt.Errorf("streaming 過程記憶體不足: %w", err)
	}

	for {
		record, err := ctx.reader.Read()
		if stderrors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			h.logger.Warn("讀取行時發生錯誤，跳過", map[string]interface{}{
				"error": err.Error(),
				"line":  ctx.processedLines + 1,
			})

			ctx.processedLines++

			continue
		}

		// 驗證數據行
		if len(record) != len(ctx.headers) {
			h.logger.Warn("行列數不匹配，跳過", map[string]interface{}{
				"expected_columns": len(ctx.headers),
				"actual_columns":   len(record),
				"line":             ctx.processedLines + 1,
			})

			ctx.processedLines++

			continue
		}

		// 執行處理器
		if err := processor(ctx, record); err != nil {
			return err
		}

		ctx.processedLines++

		// 報告進度與檢查記憶體壓力（同頻率）
		if err := h.reportProgressIfNeeded(ctx); err != nil {
			return err
		}
	}

	return nil
}

// reportProgressIfNeeded 在需要時報告進度並做記憶體壓力檢查。
//
// 兩件事共用 progressInterval 觸發條件：runtime.ReadMemStats 是 STW-ish
// 操作，每筆 row 跑會把 sliding window 的 O(n×channels) 優化全部抵銷；
// 每 progressInterval 跑一次足夠 fail-fast 也避免 hot-path overhead。
func (h *LargeFileHandler) reportProgressIfNeeded(ctx *streamingContext) error {
	if ctx.processedLines-ctx.lastProgress < ctx.progressInterval {
		return nil
	}

	h.reportProgress(ctx)
	ctx.lastProgress = ctx.processedLines

	// 記錄緩衝區池統計
	poolStats := h.bufferPool.GetStats()
	h.logger.Debug("緩衝區池統計", map[string]interface{}{
		"reuse_ratio":       poolStats.ReuseRatio,
		"string_array_gets": poolStats.StringArrayGets,
		"string_array_puts": poolStats.StringArrayPuts,
	})

	if err := h.checkMemoryUsage(); err != nil {
		return fmt.Errorf("streaming 過程記憶體不足: %w", err)
	}

	return nil
}

// reportProgress 報告當前進度.
func (*LargeFileHandler) reportProgress(ctx *streamingContext) {
	if ctx.callback == nil {
		return
	}

	percentage := float64(ctx.processedLines) / float64(ctx.fileInfo.LineCount) * fullProgress
	if percentage > fullProgress {
		percentage = fullProgress
	}

	ctx.callback(ctx.processedLines, ctx.fileInfo.LineCount, percentage)
}

// reportFinalProgress 報告最終進度 (100%).
func (*LargeFileHandler) reportFinalProgress(ctx *streamingContext) {
	if ctx.callback == nil {
		return
	}

	ctx.callback(ctx.processedLines, ctx.fileInfo.LineCount, fullProgress)
}

// closeFileWithLog 關閉文件並記錄錯誤.
func (h *LargeFileHandler) closeFileWithLog(file *os.File, path string) {
	if closeErr := file.Close(); closeErr != nil {
		h.logger.Warn("關閉文件時發生錯誤", map[string]interface{}{
			"file":  path,
			"error": closeErr.Error(),
		})
	}
}

// slidingWindowState 滑動視窗計算狀態（true streaming, O(n × k)）。
//
// 演算法核心：固定大小 ring buffer 記錄當前 window 的每筆 channel 值，
// 配合 per-channel rolling sum (windowSums)。每筆記錄只做 O(channels)
// 加減；window 滿了之後每筆記錄做一次 max 比較。取代舊版「每筆都重新
// 遍歷整個 dataBuffer 計算所有 window」的 O(windowSize² × channels) 災難。
//
// 數值穩定性：rolling sum 長期累積會有 ULP 級漂移（float64 加減非結合）。
// 每 recalibInterval 筆從 ring 重新累加一次校準，攤平到 < 1% overhead。
//
// **並行契約：non-thread-safe — 必須由單一 goroutine 連續呼叫 feed()**。
// 目前唯一 caller 是 ProcessLargeFileInChunks 的 serial reader loop；如果未來把
// chunk reading 並行化（例如分檔讀 + worker pool），必須為每個 worker 各別建一個
// slidingWindowState，或在 caller 那層加 mutex；不可共享同一實例（cross-compare review）。
type slidingWindowState struct {
	windowSize      int
	scalingFactor   int
	recalibInterval int // 動態：max(10*windowSize, 10_000)

	// Ring buffer：[windowSize][channels] 與時間軸（用於 best start time）
	ringValues  [][]float64
	ringTimes   []float64
	ringIdx     int
	recordsSeen int

	// Per-channel rolling state
	windowSums       []float64
	channelMaxMeans  []float64
	channelBestTimes [][2]float64

	// 觀測性：channel-count 不一致而被靜默丟棄的 row 數。streaming 結束時
	// 由 caller 報告，避免 operator 在資料毀損時毫無察覺。
	droppedRowCount uint64
}

// ProcessLargeFileInChunks 分塊處理大文件.
func (h *LargeFileHandler) ProcessLargeFileInChunks(
	filename string,
	windowSize int,
	callback ProgressCallback,
) (*StreamingResult, error) {
	h.logger.Info("開始分塊處理大文件", map[string]interface{}{
		"filename":    filename,
		"window_size": windowSize,
		"chunk_size":  h.chunkSize,
	})

	// 初始化滑動視窗狀態（ring buffer 自有，無需 pool defer cleanup）
	state := h.initSlidingWindowState(windowSize)

	// 創建記錄處理器
	processor := func(ctx *streamingContext, record []string) error {
		return h.processSlidingWindowRecord(ctx, record, state)
	}

	// 創建結果構建器
	resultBuilder := func(_ *streamingContext) []models.MaxMeanResult {
		return h.buildSlidingWindowResults(state)
	}

	result, err := h.processStreamingFile(filename, callback, processor, resultBuilder)
	if err != nil {
		return nil, err
	}

	// 若有 channel-count 不一致而被丟棄的 row，提醒 operator 資料可能受損。
	if state.droppedRowCount > 0 {
		h.logger.Warn("streaming 過程中丟棄通道數不符的 row", map[string]interface{}{
			"filename":          filename,
			"dropped_row_count": state.droppedRowCount,
			"expected_channels": len(state.windowSums),
		})
	}

	// 記錄最終統計信息
	h.logFinalStats(result)

	return result, nil
}

// minRecalibInterval 保證即使 windowSize 很小也不要太頻繁校準。
const minRecalibInterval = 10_000

// chooseRecalibInterval 計算數值穩定性校準週期：
//
//	max(10 × windowSize, 10_000)
//
// rolling sum 在每筆記錄做加減，IEEE 754 不結合性會讓 sum 長期偏離真值
// 約 1e-13 量級。每 N 筆完整重算一次將漂移歸零；N 取 10×windowSize 讓
// 校準成本 O(windowSize × channels) 攤平到 < 1% overhead，floor 10_000
// 保護小 windowSize 情境。
func chooseRecalibInterval(windowSize int) int {
	if windowSize <= 0 {
		return minRecalibInterval
	}
	interval := 10 * windowSize
	if interval < minRecalibInterval {
		return minRecalibInterval
	}
	return interval
}

// initSlidingWindowState 初始化滑動視窗狀態.
func (h *LargeFileHandler) initSlidingWindowState(windowSize int) *slidingWindowState {
	return &slidingWindowState{
		windowSize:      windowSize,
		scalingFactor:   h.config.ScalingFactor,
		recalibInterval: chooseRecalibInterval(windowSize),
	}
}

// initialRingCap 是 ring 動態 grow 的初始 cap，避免小檔大 windowSize 立刻 alloc
// 不必要的 (windowSize × channels) float storage。配合 ring 動態 grow，N < windowSize
// 的場景記憶體只長到 N。
const initialRingCap = 64

// initSumsAndMaxIfNeeded 只 alloc per-channel 大小（O(channels)）。ring 本身延後
// 在 appendToRing 動態 grow，避免小檔大 windowSize 時 over-allocate 整個 ring。
func (state *slidingWindowState) initSumsAndMaxIfNeeded(channelCount int) {
	if state.channelMaxMeans != nil || channelCount == 0 || state.windowSize <= 0 {
		return
	}

	state.windowSums = make([]float64, channelCount)
	state.channelMaxMeans = make([]float64, channelCount)
	state.channelBestTimes = make([][2]float64, channelCount)

	for i := range state.channelMaxMeans {
		state.channelMaxMeans[i] = math.Inf(-1) // 支援全負值資料集
	}
}

// isFiniteRow 檢查 channels 內所有值皆 finite。NaN/Inf 進 rolling sum 後會
// 永久汙染（NaN - NaN 仍 NaN），導致該 channel 後續所有 window 都被棄選。
func isFiniteRow(channels []float64) bool {
	for _, v := range channels {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// resetForNonFiniteRow 在遇到 NaN/Inf row 時清空 rolling 狀態，使下一個完整 window
// 必須由 NaN/Inf 之後的有效 row 重新填滿。等價於 legacy「window 跨越 NaN row 不入選 max」
// 的語意（legacy 把 NaN 加進 rolling sum → 該 window sum=NaN → mean=NaN → IEEE 754 下
// 永不會 > 現有 max）。channelMaxMeans/channelBestTimes 保留先前已建立的 max。
//
// 注意：isFiniteRow 是 row-level rejection — 即使只有單 channel NaN 整筆視為無效。
// 這比 legacy 的 per-channel NaN 略保守（NaN 出現在一個 channel 時，另一 channel 也
// 暫時失去當下 window），但避免 stitching 非相鄰 row 造成 over-reported max。
func (state *slidingWindowState) resetForNonFiniteRow() {
	state.recordsSeen = 0
	state.ringIdx = 0
	if state.ringValues != nil {
		state.ringValues = state.ringValues[:0]
		state.ringTimes = state.ringTimes[:0]
	}
	for c := range state.windowSums {
		state.windowSums[c] = 0
	}
}

// feed 把單筆 EMG 資料推入 streaming state，分兩階段：
//
//  1. recordsSeen < windowSize：appendToRing 動態 grow，直到 ring 第一次填滿
//  2. recordsSeen >= windowSize：rollRing wrap-around，rolling sum 扣舊加新
//
// 每筆 O(channels) — 取代舊版 O(windowSize² × channels)。
// NaN/Inf 記錄觸發 ring 重置，避免後續 window 跨越非相鄰 row（codex review P2：早期
// 只 return 不前進會把 NaN 前後的非相鄰 row 當成相鄰，造成 over-reported max）。
func (state *slidingWindowState) feed(emgData *models.EMGData) {
	state.initSumsAndMaxIfNeeded(len(emgData.Channels))
	if state.channelMaxMeans == nil {
		return
	}
	// Wave 5 PR3 belt-and-suspenders：執行 executeStreamingLoop 上游已透過
	// header check 擋住 channel-count mid-stream 變動，但若未來新 caller 繞過
	// header gate（或 emgData.Channels 被 buffer pool 借出時長度漂移），
	// 此處直接 return 避免 rollRing 撞到 ringValues[slot] 容量越界 panic。
	// 累計 droppedRowCount 讓 ProcessLargeFileInChunks 結束時可以 log warning，
	// 避免靜默丟 row 而 operator 無感。
	if len(emgData.Channels) != len(state.windowSums) {
		state.droppedRowCount++
		return
	}
	if !isFiniteRow(emgData.Channels) {
		state.resetForNonFiniteRow()
		return
	}

	if state.recordsSeen < state.windowSize {
		state.appendToRing(emgData)
		return
	}
	state.rollRing(emgData)
}

// appendToRing 在 ring 尚未填滿時動態 grow 並 append。
// recordsSeen 從 0 累計到 windowSize 的這段時間 ring 容量隨需求 2x 成長，
// 確保「N < windowSize」的小檔場景不會預先 alloc 整個 windowSize × channels。
func (state *slidingWindowState) appendToRing(emgData *models.EMGData) {
	channelCount := len(emgData.Channels)

	if state.ringValues == nil {
		capHint := initialRingCap
		if state.windowSize < capHint {
			capHint = state.windowSize
		}
		state.ringValues = make([][]float64, 0, capHint)
		state.ringTimes = make([]float64, 0, capHint)
	}

	row := make([]float64, channelCount)
	copy(row, emgData.Channels)
	state.ringValues = append(state.ringValues, row)
	state.ringTimes = append(state.ringTimes, emgData.Time)

	for c, v := range emgData.Channels {
		state.windowSums[c] += v
	}
	state.recordsSeen++

	if state.recordsSeen < state.windowSize {
		return
	}

	// 第一次填滿：ringIdx 指向下一筆要覆寫的位置（oldest slot = 0）
	state.ringIdx = 0
	state.compareMax(emgData.Time)
}

// rollRing 處理 ring 已填滿後的每筆記錄：扣舊加新、wrap-around、週期校準、比較 max。
func (state *slidingWindowState) rollRing(emgData *models.EMGData) {
	slot := state.ringIdx
	for c, v := range emgData.Channels {
		state.windowSums[c] -= state.ringValues[slot][c]
		state.windowSums[c] += v
		state.ringValues[slot][c] = v
	}
	state.ringTimes[slot] = emgData.Time
	state.ringIdx = (slot + 1) % state.windowSize
	state.recordsSeen++

	if state.recalibInterval > 0 && state.recordsSeen%state.recalibInterval == 0 {
		state.recalibrate()
	}

	state.compareMax(emgData.Time)
}

// compareMax 用當前 windowSums 對每個 channel 比較 max-mean。
// 增量後的 ringIdx 即「oldest slot」，剛好對應 window 起點時間。
func (state *slidingWindowState) compareMax(endTime float64) {
	startSlot := state.ringIdx
	startTime := state.ringTimes[startSlot]
	windowSizeF := float64(state.windowSize)

	for c, sum := range state.windowSums {
		mean := sum / windowSizeF
		if mean > state.channelMaxMeans[c] {
			state.channelMaxMeans[c] = mean
			state.channelBestTimes[c] = [2]float64{startTime, endTime}
		}
	}
}

// recalibrate 從 ring 直接重新累加 windowSums，消除 rolling 累積的 float drift。
// 代價 O(windowSize × channels)，每 recalibInterval 筆呼叫一次。
func (state *slidingWindowState) recalibrate() {
	for c := range state.windowSums {
		state.windowSums[c] = 0
	}
	for slot := 0; slot < state.windowSize; slot++ {
		row := state.ringValues[slot]
		for c, v := range row {
			state.windowSums[c] += v
		}
	}
}

// processSlidingWindowRecord 處理滑動視窗記錄.
func (h *LargeFileHandler) processSlidingWindowRecord(
	ctx *streamingContext,
	record []string,
	state *slidingWindowState,
) error {
	emgData, err := h.parseDataRow(record, state.scalingFactor)
	if err != nil {
		h.logger.Debug("解析數據行失敗，跳過", map[string]interface{}{
			"error": err.Error(),
			"line":  ctx.processedLines + 1,
		})
		// 故意返回 nil 以跳過錯誤行，繼續處理剩餘數據
		return nil //nolint:nilerr // Intentionally skip invalid rows and continue processing
	}

	state.feed(emgData)

	// ring buffer 已 copy in，parseDataRow 借出的 channels slice 立刻歸還 pool。
	// Put 後立即 nil 別名，避免未來新增 post-feed 邏輯誤讀已歸還 pool 的 slot
	// 造成 race against next GetFloat64Slice (Wave 5 PR3 defensive)。
	h.bufferPool.PutFloat64Slice(emgData.Channels)
	emgData.Channels = nil

	return nil
}

// buildSlidingWindowResults 構建滑動視窗結果.
//
// channelMaxMeans[i] 在 initSumsAndMaxIfNeeded 初始化為 -Inf（支援全負值資料集），
// 若 channel 從未產生完整 window（recordsSeen < windowSize 或所有 row 都是 NaN/Inf
// 被 resetForNonFiniteRow 攔下），值會停在 -Inf。-Inf 無法被 encoding/json marshal，
// 會讓 GUI 的 JSON 回應整支炸掉。改成跳過該 channel 並 log warning，避免毒化
// 下游序列化路徑（Wave 6 review P2 — code-debugger 抓到）。
func (h *LargeFileHandler) buildSlidingWindowResults(state *slidingWindowState) []models.MaxMeanResult {
	if state.channelMaxMeans == nil {
		return make([]models.MaxMeanResult, 0)
	}

	results := make([]models.MaxMeanResult, 0, len(state.channelMaxMeans))
	for i := range state.channelMaxMeans {
		if math.IsInf(state.channelMaxMeans[i], -1) {
			h.logger.Warn("通道未產生完整滑動視窗，跳過結果", map[string]interface{}{
				"channel_index": i + 1,
				"records_seen":  state.recordsSeen,
				"window_size":   state.windowSize,
			})

			continue
		}

		results = append(results, models.MaxMeanResult{
			ColumnIndex: i + 1,
			StartTime:   state.channelBestTimes[i][0],
			EndTime:     state.channelBestTimes[i][1],
			MaxMean:     state.channelMaxMeans[i],
		})
	}

	return results
}

// logFinalStats 記錄最終統計信息.
func (h *LargeFileHandler) logFinalStats(result *StreamingResult) {
	finalPoolStats := h.bufferPool.GetStats()
	finalMemStats := h.getDetailedMemoryStats()

	h.logger.Info("分塊處理統計", map[string]interface{}{
		"results_count":            len(result.Results),
		"buffer_reuse_ratio":       finalPoolStats.ReuseRatio,
		"buffer_gets":              finalPoolStats.StringArrayGets + finalPoolStats.EMGDataGets + finalPoolStats.Float64Gets,
		"buffer_puts":              finalPoolStats.StringArrayPuts + finalPoolStats.EMGDataPuts + finalPoolStats.Float64Puts,
		"final_memory_usage_ratio": finalMemStats.UsageRatio,
		"gc_count":                 finalMemStats.NumGC,
	})
}

// GetBufferPoolStats 獲取緩衝區池統計信息.
func (h *LargeFileHandler) GetBufferPoolStats() BufferPoolStats {
	return h.bufferPool.GetStats()
}

// GetMemoryStats 獲取記憶體統計信息.
func (h *LargeFileHandler) GetMemoryStats() *MemoryStats {
	return h.getDetailedMemoryStats()
}

// ResetBufferPool 重置緩衝區池.
func (h *LargeFileHandler) ResetBufferPool() {
	h.bufferPool = NewBufferPool()
	h.logger.Info("緩衝區池已重置")
}

// parseDataRow 解析數據行.
func (h *LargeFileHandler) parseDataRow(record []string, scalingFactor int) (*models.EMGData, error) {
	if len(record) < 2 {
		return nil, errDataRowTooShort
	}

	// 解析時間
	timeVal, err := util.Str2Number[float64, int](record[0], scalingFactor)
	if err != nil {
		return nil, fmt.Errorf("解析時間失敗: %w", err)
	}

	// 解析通道數據
	channels := h.bufferPool.GetFloat64Slice()
	// 注意：這裡不能使用defer，因為channels需要返回給調用者
	for i := 1; i < len(record); i++ {
		val, err := util.Str2Number[float64, int](record[i], scalingFactor)
		if err != nil {
			h.bufferPool.PutFloat64Slice(channels) // 出錯時歸還緩衝區
			return nil, fmt.Errorf("解析通道 %d 失敗: %w", i, err)
		}

		channels = append(channels, val)
	}

	return &models.EMGData{
		Time:     timeVal,
		Channels: channels,
	}, nil
}
