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
	"runtime"
	"strings"
	"time"

	"count_mean/internal/config"
	"count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/security"
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

	// 檢查路徑遍歷攻擊
	if strings.Contains(sanitizedPath, "..") {
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
	file, err := os.Open(filename) //nolint:gosec // filename is sanitized and validated
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

	reader := csv.NewReader(bufio.NewReaderSize(file, h.bufferSize))

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
		if err == io.EOF {
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

// ReadCSVStreaming 流式讀取大 CSV 文件.
func (h *LargeFileHandler) ReadCSVStreaming(filename string, callback ProgressCallback) (*StreamingResult, error) {
	h.logger.Info("開始流式讀取 CSV 文件", map[string]interface{}{
		"filename":   filename,
		"chunk_size": h.chunkSize,
	})

	chunk := h.bufferPool.GetStringArray()
	defer h.bufferPool.PutStringArray(chunk)

	processor := func(_ *streamingContext, record []string) error {
		// 深拷貝記錄以避免引用問題
		recordCopy := make([]string, len(record))
		copy(recordCopy, record)
		chunk = append(chunk, recordCopy)

		// 定期清理塊數據以釋放記憶體（保留標題行）
		if len(chunk) > h.chunkSize+1 {
			headerRow := chunk[0]
			chunk = chunk[:1]
			chunk[0] = headerRow
		}

		return nil
	}

	return h.processStreamingFile(filename, callback, processor, nil)
}

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

	file, err := os.Open(fileInfo.Path)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeFileNotFound, "無法開啟文件")
	}

	defer h.closeFileWithLog(file, fileInfo.Path)

	reader := csv.NewReader(bufio.NewReaderSize(file, h.bufferSize))

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
func (h *LargeFileHandler) executeStreamingLoop(ctx *streamingContext, processor recordProcessor) error {
	for {
		// 檢查並處理記憶體壓力
		ctx.progressInterval = h.handleMemoryPressure(ctx.processedLines, ctx.progressInterval)

		record, err := ctx.reader.Read()
		if err == io.EOF {
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

		// 報告進度
		h.reportProgressIfNeeded(ctx)
	}

	return nil
}

// handleMemoryPressure 處理記憶體壓力.
func (h *LargeFileHandler) handleMemoryPressure(processedLines, progressInterval int64) int64 {
	if err := h.checkMemoryUsage(); err != nil {
		h.logger.Warn("記憶體使用過高，觸發垃圾回收", map[string]interface{}{
			"processed_lines": processedLines,
			"error":           err.Error(),
		})
		runtime.GC()

		// 如果記憶體仍然過高，減少塊大小
		if err := h.checkMemoryUsage(); err != nil {
			newInterval := progressInterval / 2
			if newInterval < 100 {
				newInterval = 100
			}

			h.logger.Warn("減少進度報告間隔", map[string]interface{}{
				"new_interval": newInterval,
			})

			return newInterval
		}
	}

	return progressInterval
}

// reportProgressIfNeeded 在需要時報告進度.
func (h *LargeFileHandler) reportProgressIfNeeded(ctx *streamingContext) {
	if ctx.processedLines-ctx.lastProgress >= ctx.progressInterval {
		h.reportProgress(ctx)
		ctx.lastProgress = ctx.processedLines

		// 記錄緩衝區池統計
		poolStats := h.bufferPool.GetStats()
		h.logger.Debug("緩衝區池統計", map[string]interface{}{
			"reuse_ratio":       poolStats.ReuseRatio,
			"string_array_gets": poolStats.StringArrayGets,
			"string_array_puts": poolStats.StringArrayPuts,
		})
	}
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

// slidingWindowState 滑動視窗計算狀態.
type slidingWindowState struct {
	dataBuffer       []models.EMGData
	channelMaxMeans  []float64
	channelBestTimes [][2]float64
	windowSize       int
	scalingFactor    int
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

	// 初始化滑動視窗狀態
	state := h.initSlidingWindowState(windowSize)
	defer h.bufferPool.PutEMGDataSlice(state.dataBuffer)

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

	// 記錄最終統計信息
	h.logFinalStats(result)

	return result, nil
}

// initSlidingWindowState 初始化滑動視窗狀態.
func (h *LargeFileHandler) initSlidingWindowState(windowSize int) *slidingWindowState {
	return &slidingWindowState{
		dataBuffer:    h.bufferPool.GetEMGDataSlice(),
		windowSize:    windowSize,
		scalingFactor: h.config.ScalingFactor,
	}
}

// initChannelArrays 初始化通道陣列（在第一次處理記錄時調用）.
func (state *slidingWindowState) initChannelArrays(channelCount int) {
	if state.channelMaxMeans != nil {
		return
	}

	state.channelMaxMeans = make([]float64, channelCount)
	state.channelBestTimes = make([][2]float64, channelCount)

	for i := range state.channelMaxMeans {
		state.channelMaxMeans[i] = math.Inf(-1) // 支援全負值資料集
	}
}

// processSlidingWindowRecord 處理滑動視窗記錄.
func (h *LargeFileHandler) processSlidingWindowRecord(
	ctx *streamingContext,
	record []string,
	state *slidingWindowState,
) error {
	// 解析數據行
	emgData, err := h.parseDataRow(record, state.scalingFactor)
	if err != nil {
		h.logger.Debug("解析數據行失敗，跳過", map[string]interface{}{
			"error": err.Error(),
			"line":  ctx.processedLines + 1,
		})
		// 故意返回 nil 以跳過錯誤行，繼續處理剩餘數據
		// 這是數據處理的常見模式，允許容錯處理
		return nil //nolint:nilerr // Intentionally skip invalid rows and continue processing
	}

	// 延遲初始化通道陣列
	state.initChannelArrays(len(emgData.Channels))

	// 添加到數據緩衝區
	state.dataBuffer = append(state.dataBuffer, *emgData)

	// 當緩衝區達到滑動視窗大小時，開始計算
	if len(state.dataBuffer) >= state.windowSize {
		h.calculateSlidingWindow(state.dataBuffer, state.windowSize, state.channelMaxMeans, state.channelBestTimes)
		state.dataBuffer = h.manageDataBuffer(state.dataBuffer, state.windowSize)
	}

	return nil
}

// manageDataBuffer 管理數據緩衝區大小.
func (h *LargeFileHandler) manageDataBuffer(dataBuffer []models.EMGData, windowSize int) []models.EMGData {
	bufferLimit := windowSize * 3

	if len(dataBuffer) < bufferLimit {
		return dataBuffer
	}

	keepCount := windowSize * 2
	if keepCount >= len(dataBuffer) {
		return dataBuffer
	}

	// 使用安全的切片操作，避免數據丟失
	copy(dataBuffer, dataBuffer[len(dataBuffer)-keepCount:])
	dataBuffer = dataBuffer[:keepCount]

	h.logger.Debug("緩衝區清理", map[string]interface{}{
		"new_size":     len(dataBuffer),
		"keep_count":   keepCount,
		"buffer_limit": bufferLimit,
	})

	return dataBuffer
}

// buildSlidingWindowResults 構建滑動視窗結果.
func (*LargeFileHandler) buildSlidingWindowResults(state *slidingWindowState) []models.MaxMeanResult {
	if state.channelMaxMeans == nil {
		return make([]models.MaxMeanResult, 0)
	}

	results := make([]models.MaxMeanResult, len(state.channelMaxMeans))
	for i := 0; i < len(state.channelMaxMeans); i++ {
		results[i] = models.MaxMeanResult{
			ColumnIndex: i + 1,
			StartTime:   state.channelBestTimes[i][0],
			EndTime:     state.channelBestTimes[i][1],
			MaxMean:     state.channelMaxMeans[i],
		}
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

// calculateSlidingWindow 計算滑動視窗.
func (*LargeFileHandler) calculateSlidingWindow(
	data []models.EMGData,
	windowSize int,
	maxMeans []float64,
	bestTimes [][2]float64,
) {
	if len(data) < windowSize {
		return
	}

	channelCount := len(data[0].Channels)

	// 對每個通道計算滑動視窗
	for channelIdx := 0; channelIdx < channelCount; channelIdx++ {
		for startIdx := 0; startIdx <= len(data)-windowSize; startIdx++ {
			// 計算這個視窗的平均值
			sum := 0.0

			for i := startIdx; i < startIdx+windowSize; i++ {
				if channelIdx < len(data[i].Channels) {
					sum += data[i].Channels[channelIdx]
				}
			}

			mean := sum / float64(windowSize)

			// 更新最大平均值
			if mean > maxMeans[channelIdx] {
				maxMeans[channelIdx] = mean
				bestTimes[channelIdx][0] = data[startIdx].Time
				bestTimes[channelIdx][1] = data[startIdx+windowSize-1].Time
			}
		}
	}
}

// WriteCSVStreaming 流式寫入 CSV 文件.
func (h *LargeFileHandler) WriteCSVStreaming(filename string, data [][]string, callback ProgressCallback) error {
	h.logger.Info("開始流式寫入 CSV 文件", map[string]interface{}{
		"filename":  filename,
		"row_count": len(data),
	})

	// 驗證路徑
	sanitizedPath, err := h.validateWritePath(filename)
	if err != nil {
		return err
	}

	// 創建文件和緩衝寫入器
	file, bufferedWriter, err := h.createBufferedWriter(sanitizedPath)
	if err != nil {
		return err
	}

	defer h.closeFileWithLog(file, sanitizedPath)
	defer h.flushBufferWithLog(bufferedWriter, sanitizedPath)

	// 寫入 BOM（如果啟用）
	if err := h.writeBOMIfEnabled(bufferedWriter); err != nil {
		return err
	}

	// 執行寫入
	if err := h.executeCSVWrite(bufferedWriter, data, callback); err != nil {
		return err
	}

	h.logger.Info("流式寫入完成", map[string]interface{}{
		"filename":  sanitizedPath,
		"row_count": len(data),
	})

	return nil
}

// validateWritePath 驗證寫入路徑.
func (h *LargeFileHandler) validateWritePath(filename string) (string, error) {
	sanitizedPath := h.pathValidator.SanitizePath(filename)
	if err := h.pathValidator.ValidateFilePath(sanitizedPath); err != nil {
		return "", errors.WrapError(err, errors.ErrCodePathValidation, "路徑驗證失敗")
	}

	return sanitizedPath, nil
}

// File permission constants.
const filePermission = 0o600 // File permission mode for created files.

// createBufferedWriter 創建緩衝寫入器.
func (h *LargeFileHandler) createBufferedWriter(path string) (*os.File, *bufio.Writer, error) {
	//nolint:gosec // path is validated
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePermission)
	if err != nil {
		return nil, nil, errors.WrapError(err, errors.ErrCodeFileNotFound, "無法創建文件")
	}

	bufferedWriter := bufio.NewWriterSize(file, h.bufferSize)

	return file, bufferedWriter, nil
}

// flushBufferWithLog 刷新緩衝區並記錄錯誤.
func (h *LargeFileHandler) flushBufferWithLog(writer *bufio.Writer, path string) {
	if flushErr := writer.Flush(); flushErr != nil {
		h.logger.Warn("刷新緩衝區時發生錯誤", map[string]interface{}{
			"file":  path,
			"error": flushErr.Error(),
		})
	}
}

// writeBOMIfEnabled 如果啟用則寫入 BOM.
func (h *LargeFileHandler) writeBOMIfEnabled(writer *bufio.Writer) error {
	if !h.config.BOMEnabled {
		return nil
	}

	if _, err := writer.Write(BOMBytes); err != nil {
		return fmt.Errorf("無法寫入 BOM: %w", err)
	}

	return nil
}

// executeCSVWrite 執行 CSV 寫入.
func (h *LargeFileHandler) executeCSVWrite(
	bufferedWriter *bufio.Writer,
	data [][]string,
	callback ProgressCallback,
) error {
	writer := csv.NewWriter(bufferedWriter)
	totalRows := len(data)

	for i, row := range data {
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("寫入第 %d 行失敗: %w", i+1, err)
		}

		if err := h.flushAndReportProgress(writer, i, totalRows, callback); err != nil {
			return err
		}
	}

	writer.Flush()

	if err := writer.Error(); err != nil {
		return fmt.Errorf("最終刷新失敗: %w", err)
	}

	// 最終進度回調
	if callback != nil {
		callback(int64(totalRows), int64(totalRows), fullProgress)
	}

	return nil
}

// flushAndReportProgress 定期刷新並報告進度.
func (h *LargeFileHandler) flushAndReportProgress(
	writer *csv.Writer,
	currentRow, totalRows int,
	callback ProgressCallback,
) error {
	if currentRow%h.chunkSize != 0 {
		return nil
	}

	writer.Flush()

	if err := writer.Error(); err != nil {
		return fmt.Errorf("刷新寫入緩衝區失敗: %w", err)
	}

	if callback != nil {
		percentage := float64(currentRow+1) / float64(totalRows) * 100
		callback(int64(currentRow+1), int64(totalRows), percentage)
	}

	return nil
}
