package io

import (
	"bufio"
	"count_mean/internal/config"
	"count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/security"
	"count_mean/internal/validation"
	"count_mean/util"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"
)

// ProgressCallback 進度回調函數類型
type ProgressCallback func(processed int64, total int64, percentage float64)

// LargeFileHandler 處理大文件的結構
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

// NewLargeFileHandler 創建大文件處理器
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
		chunkSize:   1000,                   // 每次處理1000行
		memoryLimit: 512 * 1024 * 1024,      // 512MB 記憶體限制
		bufferSize:  64 * 1024,              // 64KB 緩衝區
		maxFileSize: 2 * 1024 * 1024 * 1024, // 2GB 最大文件大小

		// 緩衝區池
		bufferPool: NewBufferPool(),
	}
}

// FileInfo 文件信息
type FileInfo struct {
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	LineCount   int64  `json:"line_count"`
	ColumnCount int    `json:"column_count"`
	IsLarge     bool   `json:"is_large"`
}

// StreamingResult 流式處理結果
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

// GetFileInfo 獲取文件基本信息
func (h *LargeFileHandler) GetFileInfo(filename string) (*FileInfo, error) {
	h.logger.Debug("開始獲取文件信息", map[string]interface{}{
		"filename": filename,
	})

	// 驗證路徑
	sanitizedPath := h.pathValidator.SanitizePath(filename)
	if err := h.pathValidator.ValidateFilePath(sanitizedPath); err != nil {
		return nil, errors.WrapError(err, errors.ErrCodePathValidation, "路徑驗證失敗")
	}

	// 獲取文件統計信息
	fileInfo, err := os.Stat(sanitizedPath)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeFileNotFound, "無法獲取文件信息")
	}

	info := &FileInfo{
		Path:    sanitizedPath,
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
	lineCount, columnCount, err := h.scanFileStructure(sanitizedPath)
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

// scanFileStructure 快速掃描文件結構
func (h *LargeFileHandler) scanFileStructure(filename string) (int64, int, error) {
	file, err := os.Open(filename)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReaderSize(file, h.bufferSize))

	lineCount := int64(0)
	columnCount := 0

	// 讀取第一行獲取列數
	firstRow, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	columnCount = len(firstRow)
	lineCount = 1

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

// ReadCSVStreaming 流式讀取大 CSV 文件
func (h *LargeFileHandler) ReadCSVStreaming(filename string, callback ProgressCallback) (*StreamingResult, error) {
	startTime := time.Now()
	h.logger.Info("開始流式讀取 CSV 文件", map[string]interface{}{
		"filename":   filename,
		"chunk_size": h.chunkSize,
	})

	// 獲取文件信息
	fileInfo, err := h.GetFileInfo(filename)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(fileInfo.Path)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeFileNotFound, "無法開啟文件")
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReaderSize(file, h.bufferSize))

	// 讀取標題行
	headers, err := reader.Read()
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeDataParsing, "無法讀取標題行")
	}

	result := &StreamingResult{
		Headers:    headers,
		StartTime:  startTime,
		TotalLines: fileInfo.LineCount,
		Results:    make([]models.MaxMeanResult, 0),
	}

	processedLines := int64(1) // 已處理標題行
	chunk := h.bufferPool.GetStringArray()
	defer h.bufferPool.PutStringArray(chunk)
	chunk = append(chunk, headers) // 添加標題行

	lastProgressReport := int64(0)
	progressInterval := int64(h.chunkSize)

	// 流式處理
	for {
		// 檢查記憶體使用
		if err := h.checkMemoryUsage(); err != nil {
			h.logger.Warn("記憶體使用過高，觸發垃圾回收", map[string]interface{}{
				"processed_lines": processedLines,
				"error":           err.Error(),
			})
			runtime.GC()

			// 如果記憶體仍然過高，減少塊大小
			if err := h.checkMemoryUsage(); err != nil {
				progressInterval = progressInterval / 2
				if progressInterval < 100 {
					progressInterval = 100
				}
				h.logger.Warn("減少進度報告間隔", map[string]interface{}{
					"new_interval": progressInterval,
				})
			}
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			h.logger.Warn("讀取行時發生錯誤，跳過", map[string]interface{}{
				"error": err.Error(),
				"line":  processedLines + 1,
			})
			processedLines++ // 仍然計數，確保進度追蹤準確
			continue
		}

		// 驗證數據行
		if len(record) != len(headers) {
			h.logger.Warn("行列數不匹配，跳過", map[string]interface{}{
				"expected_columns": len(headers),
				"actual_columns":   len(record),
				"line":             processedLines + 1,
			})
			processedLines++ // 仍然計數，確保進度追蹤準確
			continue
		}

		// 深拷貝記錄以避免引用問題
		recordCopy := make([]string, len(record))
		copy(recordCopy, record)
		chunk = append(chunk, recordCopy)
		processedLines++

		// 當處理的行數達到進度報告間隔時，進行進度回調
		if processedLines-lastProgressReport >= progressInterval {
			if callback != nil {
				percentage := float64(processedLines) / float64(fileInfo.LineCount) * 100
				if percentage > 100 {
					percentage = 100
				}
				callback(processedLines, fileInfo.LineCount, percentage)
			}
			lastProgressReport = processedLines

			// 記錄緩衝區池統計
			poolStats := h.bufferPool.GetStats()
			h.logger.Debug("緩衝區池統計", map[string]interface{}{
				"reuse_ratio":       poolStats.ReuseRatio,
				"string_array_gets": poolStats.StringArrayGets,
				"string_array_puts": poolStats.StringArrayPuts,
			})

			// 定期清理塊數據以釋放記憶體（保留標題行）
			if len(chunk) > h.chunkSize+1 { // +1 因為包含標題行
				// 保留標題行並重新開始
				headers := chunk[0]
				chunk = chunk[:1]
				chunk[0] = headers
			}
		}
	}

	// 最終進度回調
	if callback != nil {
		callback(processedLines, fileInfo.LineCount, 100.0)
	}

	endTime := time.Now()
	result.ProcessedLines = processedLines
	result.EndTime = endTime
	result.Duration = endTime.Sub(startTime)
	result.MemoryUsed = h.getMemoryUsage()

	h.logger.Info("流式讀取完成", map[string]interface{}{
		"processed_lines": processedLines,
		"duration_ms":     result.Duration.Milliseconds(),
		"memory_used_mb":  result.MemoryUsed / 1024 / 1024,
	})

	return result, nil
}

// ProcessLargeFileInChunks 分塊處理大文件
func (h *LargeFileHandler) ProcessLargeFileInChunks(filename string, windowSize int, callback ProgressCallback) (*StreamingResult, error) {
	startTime := time.Now()
	h.logger.Info("開始分塊處理大文件", map[string]interface{}{
		"filename":    filename,
		"window_size": windowSize,
		"chunk_size":  h.chunkSize,
	})

	// 獲取文件信息
	fileInfo, err := h.GetFileInfo(filename)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(fileInfo.Path)
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeFileNotFound, "無法開啟文件")
	}
	defer file.Close()

	reader := csv.NewReader(bufio.NewReaderSize(file, h.bufferSize))

	// 讀取標題行
	headers, err := reader.Read()
	if err != nil {
		return nil, errors.WrapError(err, errors.ErrCodeDataParsing, "無法讀取標題行")
	}

	result := &StreamingResult{
		Headers:    headers,
		StartTime:  startTime,
		TotalLines: fileInfo.LineCount,
		Results:    make([]models.MaxMeanResult, 0),
	}

	processedLines := int64(1) // 已處理標題行
	chunk := h.bufferPool.GetStringArray()
	defer h.bufferPool.PutStringArray(chunk)
	chunk = append(chunk, headers) // 添加標題行

	// 累積數據用於計算（使用滑動視窗）
	dataBuffer := h.bufferPool.GetEMGDataSlice()
	defer h.bufferPool.PutEMGDataSlice(dataBuffer)
	scalingFactor := h.config.ScalingFactor

	channelMaxMeans := make([]float64, len(headers)-1)     // 存儲每個通道的最大平均值
	channelBestTimes := make([][2]float64, len(headers)-1) // 存儲最優時間範圍

	// 流式處理每一行
	for {
		// 檢查記憶體使用
		if err := h.checkMemoryUsage(); err != nil {
			h.logger.Warn("記憶體使用過高，觸發垃圾回收")
			runtime.GC()
		}

		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			h.logger.Debug("讀取行時發生錯誤，跳過", map[string]interface{}{
				"error": err.Error(),
				"line":  processedLines + 1,
			})
			continue
		}

		// 驗證數據行
		if len(record) != len(headers) {
			continue
		}

		// 解析數據行
		emgData, err := h.parseDataRow(record, scalingFactor)
		if err != nil {
			h.logger.Debug("解析數據行失敗，跳過", map[string]interface{}{
				"error": err.Error(),
				"line":  processedLines + 1,
			})
			continue
		}

		// 添加到數據緩衝區
		dataBuffer = append(dataBuffer, *emgData)

		// 當緩衝區達到滑動視窗大小時，開始計算
		if len(dataBuffer) >= windowSize {
			h.calculateSlidingWindow(dataBuffer, windowSize, channelMaxMeans, channelBestTimes)

			// 保持緩衝區大小，移除最舊的數據
			// 改進：更安全的緩衝區管理，防止數據丟失
			bufferLimit := windowSize * 3 // 增加緩衝區限制以提供更多重疊
			if len(dataBuffer) >= bufferLimit {
				keepCount := windowSize * 2 // 保留更多數據以確保連續性
				if keepCount < len(dataBuffer) {
					// 使用安全的切片操作，避免數據丟失
					copy(dataBuffer, dataBuffer[len(dataBuffer)-keepCount:])
					dataBuffer = dataBuffer[:keepCount]

					h.logger.Debug("緩衝區清理", map[string]interface{}{
						"new_size":     len(dataBuffer),
						"keep_count":   keepCount,
						"buffer_limit": bufferLimit,
					})
				}
			}
		}

		processedLines++

		// 進度回調
		if processedLines%int64(h.chunkSize) == 0 && callback != nil {
			percentage := float64(processedLines) / float64(fileInfo.LineCount) * 100
			callback(processedLines, fileInfo.LineCount, percentage)
		}
	}

	// 生成最終結果
	for i := 0; i < len(channelMaxMeans); i++ {
		result.Results = append(result.Results, models.MaxMeanResult{
			ColumnIndex: i + 1,
			StartTime:   channelBestTimes[i][0],
			EndTime:     channelBestTimes[i][1],
			MaxMean:     channelMaxMeans[i],
		})
	}

	// 最終進度回調
	if callback != nil {
		callback(processedLines, fileInfo.LineCount, 100.0)
	}

	endTime := time.Now()
	result.ProcessedLines = processedLines
	result.EndTime = endTime
	result.Duration = endTime.Sub(startTime)
	result.MemoryUsed = h.getMemoryUsage()

	// 獲取最終統計信息
	finalPoolStats := h.bufferPool.GetStats()
	finalMemStats := h.getDetailedMemoryStats()

	h.logger.Info("分塊處理完成", map[string]interface{}{
		"processed_lines":          processedLines,
		"duration_ms":              result.Duration.Milliseconds(),
		"memory_used_mb":           result.MemoryUsed / 1024 / 1024,
		"results_count":            len(result.Results),
		"buffer_reuse_ratio":       finalPoolStats.ReuseRatio,
		"buffer_gets":              finalPoolStats.StringArrayGets + finalPoolStats.EMGDataGets + finalPoolStats.Float64Gets,
		"buffer_puts":              finalPoolStats.StringArrayPuts + finalPoolStats.EMGDataPuts + finalPoolStats.Float64Puts,
		"final_memory_usage_ratio": finalMemStats.UsageRatio,
		"gc_count":                 finalMemStats.NumGC,
	})

	return result, nil
}

// GetBufferPoolStats 獲取緩衝區池統計信息
func (h *LargeFileHandler) GetBufferPoolStats() BufferPoolStats {
	return h.bufferPool.GetStats()
}

// GetMemoryStats 獲取記憶體統計信息
func (h *LargeFileHandler) GetMemoryStats() *MemoryStats {
	return h.getDetailedMemoryStats()
}

// ResetBufferPool 重置緩衝區池
func (h *LargeFileHandler) ResetBufferPool() {
	h.bufferPool = NewBufferPool()
	h.logger.Info("緩衝區池已重置")
}

// parseDataRow 解析數據行
func (h *LargeFileHandler) parseDataRow(record []string, scalingFactor int) (*models.EMGData, error) {
	if len(record) < 2 {
		return nil, fmt.Errorf("數據行長度不足")
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

// calculateSlidingWindow 計算滑動視窗
func (h *LargeFileHandler) calculateSlidingWindow(data []models.EMGData, windowSize int, maxMeans []float64, bestTimes [][2]float64) {
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

// MemoryStats 記憶體統計信息
type MemoryStats struct {
	Alloc         uint64  `json:"alloc"`           // 當前分配的記憶體
	TotalAlloc    uint64  `json:"total_alloc"`     // 總分配記憶體
	Sys           uint64  `json:"sys"`             // 系統記憶體
	Lookups       uint64  `json:"lookups"`         // 查找次數
	Mallocs       uint64  `json:"mallocs"`         // 分配次數
	Frees         uint64  `json:"frees"`           // 釋放次數
	HeapAlloc     uint64  `json:"heap_alloc"`      // 堆分配
	HeapSys       uint64  `json:"heap_sys"`        // 堆系統記憶體
	HeapIdle      uint64  `json:"heap_idle"`       // 堆空閒記憶體
	HeapInuse     uint64  `json:"heap_inuse"`      // 堆使用記憶體
	HeapReleased  uint64  `json:"heap_released"`   // 堆釋放記憶體
	HeapObjects   uint64  `json:"heap_objects"`    // 堆對象數
	StackInuse    uint64  `json:"stack_inuse"`     // 棧使用記憶體
	StackSys      uint64  `json:"stack_sys"`       // 棧系統記憶體
	MSpanInuse    uint64  `json:"mspan_inuse"`     // MSpan使用記憶體
	MSpanSys      uint64  `json:"mspan_sys"`       // MSpan系統記憶體
	MCacheInuse   uint64  `json:"mcache_inuse"`    // MCache使用記憶體
	MCacheSys     uint64  `json:"mcache_sys"`      // MCache系統記憶體
	BuckHashSys   uint64  `json:"buckhash_sys"`    // BuckHash系統記憶體
	GCSys         uint64  `json:"gc_sys"`          // GC系統記憶體
	OtherSys      uint64  `json:"other_sys"`       // 其他系統記憶體
	NextGC        uint64  `json:"next_gc"`         // 下次GC閾值
	LastGC        uint64  `json:"last_gc"`         // 上次GC時間
	PauseTotalNs  uint64  `json:"pause_total"`     // GC暫停總時間
	PauseNs       uint64  `json:"pause_ns"`        // 最近GC暫停時間
	NumGC         uint32  `json:"num_gc"`          // GC次數
	NumForcedGC   uint32  `json:"num_forced_gc"`   // 強制GC次數
	GCCPUFraction float64 `json:"gc_cpu_fraction"` // GC CPU比例
	UsageRatio    float64 `json:"usage_ratio"`     // 使用率
	IsOverLimit   bool    `json:"is_over_limit"`   // 是否超過限制
}

// BufferPool 緩衝區池管理
type BufferPool struct {
	stringArrayPool *sync.Pool // 字符串陣列池
	emgDataPool     *sync.Pool // EMG數據池
	float64Pool     *sync.Pool // float64 切片池
	mutex           sync.RWMutex
	stats           BufferPoolStats
}

// BufferPoolStats 緩衝區池統計
type BufferPoolStats struct {
	StringArrayGets int64   `json:"string_array_gets"`
	StringArrayPuts int64   `json:"string_array_puts"`
	EMGDataGets     int64   `json:"emg_data_gets"`
	EMGDataPuts     int64   `json:"emg_data_puts"`
	Float64Gets     int64   `json:"float64_gets"`
	Float64Puts     int64   `json:"float64_puts"`
	ReuseRatio      float64 `json:"reuse_ratio"`
}

// NewBufferPool 創建緩衝區池
func NewBufferPool() *BufferPool {
	return &BufferPool{
		stringArrayPool: &sync.Pool{
			New: func() interface{} {
				return make([][]string, 0, 1000) // 預分配1000個元素的容量
			},
		},
		emgDataPool: &sync.Pool{
			New: func() interface{} {
				return make([]models.EMGData, 0, 2000) // 預分配2000個元素的容量
			},
		},
		float64Pool: &sync.Pool{
			New: func() interface{} {
				return make([]float64, 0, 100) // 預分配100個元素的容量
			},
		},
	}
}

// GetStringArray 獲取字符串陣列
func (bp *BufferPool) GetStringArray() [][]string {
	bp.mutex.Lock()
	bp.stats.StringArrayGets++
	bp.mutex.Unlock()

	arr := bp.stringArrayPool.Get().([][]string)
	return arr[:0] // 重置長度但保留容量
}

// PutStringArray 歸還字符串陣列
func (bp *BufferPool) PutStringArray(arr [][]string) {
	if cap(arr) > 0 {
		bp.mutex.Lock()
		bp.stats.StringArrayPuts++
		bp.mutex.Unlock()

		// 清空引用以避免記憶體洩漏
		for i := range arr {
			arr[i] = nil
		}
		bp.stringArrayPool.Put(arr[:0])
	}
}

// GetEMGDataSlice 獲取EMG數據切片
func (bp *BufferPool) GetEMGDataSlice() []models.EMGData {
	bp.mutex.Lock()
	bp.stats.EMGDataGets++
	bp.mutex.Unlock()

	slice := bp.emgDataPool.Get().([]models.EMGData)
	return slice[:0] // 重置長度但保留容量
}

// PutEMGDataSlice 歸還EMG數據切片
func (bp *BufferPool) PutEMGDataSlice(slice []models.EMGData) {
	if cap(slice) > 0 {
		bp.mutex.Lock()
		bp.stats.EMGDataPuts++
		bp.mutex.Unlock()

		// 清空數據以避免記憶體洩漏
		for i := range slice {
			slice[i] = models.EMGData{}
		}
		bp.emgDataPool.Put(slice[:0])
	}
}

// GetFloat64Slice 獲取float64切片
func (bp *BufferPool) GetFloat64Slice() []float64 {
	bp.mutex.Lock()
	bp.stats.Float64Gets++
	bp.mutex.Unlock()

	slice := bp.float64Pool.Get().([]float64)
	return slice[:0] // 重置長度但保留容量
}

// PutFloat64Slice 歸還float64切片
func (bp *BufferPool) PutFloat64Slice(slice []float64) {
	if cap(slice) > 0 {
		bp.mutex.Lock()
		bp.stats.Float64Puts++
		bp.mutex.Unlock()

		bp.float64Pool.Put(slice[:0])
	}
}

// GetStats 獲取緩衝區池統計
func (bp *BufferPool) GetStats() BufferPoolStats {
	bp.mutex.RLock()
	defer bp.mutex.RUnlock()

	stats := bp.stats

	// 計算重用率
	totalGets := stats.StringArrayGets + stats.EMGDataGets + stats.Float64Gets
	totalPuts := stats.StringArrayPuts + stats.EMGDataPuts + stats.Float64Puts

	if totalGets > 0 {
		stats.ReuseRatio = float64(totalPuts) / float64(totalGets)
	}

	return stats
}

// getDetailedMemoryStats 獲取詳細記憶體統計信息
func (h *LargeFileHandler) getDetailedMemoryStats() *MemoryStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	stats := &MemoryStats{
		Alloc:         m.Alloc,
		TotalAlloc:    m.TotalAlloc,
		Sys:           m.Sys,
		Lookups:       m.Lookups,
		Mallocs:       m.Mallocs,
		Frees:         m.Frees,
		HeapAlloc:     m.HeapAlloc,
		HeapSys:       m.HeapSys,
		HeapIdle:      m.HeapIdle,
		HeapInuse:     m.HeapInuse,
		HeapReleased:  m.HeapReleased,
		HeapObjects:   m.HeapObjects,
		StackInuse:    m.StackInuse,
		StackSys:      m.StackSys,
		MSpanInuse:    m.MSpanInuse,
		MSpanSys:      m.MSpanSys,
		MCacheInuse:   m.MCacheInuse,
		MCacheSys:     m.MCacheSys,
		BuckHashSys:   m.BuckHashSys,
		GCSys:         m.GCSys,
		OtherSys:      m.OtherSys,
		NextGC:        m.NextGC,
		LastGC:        m.LastGC,
		PauseTotalNs:  m.PauseTotalNs,
		NumGC:         m.NumGC,
		NumForcedGC:   m.NumForcedGC,
		GCCPUFraction: m.GCCPUFraction,
		UsageRatio:    float64(m.Alloc) / float64(h.memoryLimit),
		IsOverLimit:   int64(m.Alloc) > h.memoryLimit,
	}

	// 計算最近的GC暫停時間
	if m.NumGC > 0 {
		stats.PauseNs = m.PauseNs[(m.NumGC+255)%256]
	}

	return stats
}

// checkMemoryUsage 檢查記憶體使用
func (h *LargeFileHandler) checkMemoryUsage() error {
	stats := h.getDetailedMemoryStats()

	// 記錄詳細的記憶體信息
	h.logger.Debug("記憶體使用統計", map[string]interface{}{
		"alloc_mb":        stats.Alloc / 1024 / 1024,
		"total_alloc_mb":  stats.TotalAlloc / 1024 / 1024,
		"sys_mb":          stats.Sys / 1024 / 1024,
		"heap_alloc_mb":   stats.HeapAlloc / 1024 / 1024,
		"heap_sys_mb":     stats.HeapSys / 1024 / 1024,
		"heap_idle_mb":    stats.HeapIdle / 1024 / 1024,
		"heap_inuse_mb":   stats.HeapInuse / 1024 / 1024,
		"heap_objects":    stats.HeapObjects,
		"stack_inuse_mb":  stats.StackInuse / 1024 / 1024,
		"usage_ratio":     stats.UsageRatio,
		"num_gc":          stats.NumGC,
		"num_forced_gc":   stats.NumForcedGC,
		"gc_cpu_fraction": stats.GCCPUFraction,
		"next_gc_mb":      stats.NextGC / 1024 / 1024,
		"limit_mb":        h.memoryLimit / 1024 / 1024,
	})

	// 多級記憶體檢查
	if stats.IsOverLimit {
		h.logger.Warn("記憶體使用超過限制", map[string]interface{}{
			"current_mb":  stats.Alloc / 1024 / 1024,
			"limit_mb":    h.memoryLimit / 1024 / 1024,
			"usage_ratio": stats.UsageRatio,
		})
		return fmt.Errorf("記憶體使用超過限制: %d MB > %d MB (%.2f%%)",
			stats.Alloc/1024/1024, h.memoryLimit/1024/1024, stats.UsageRatio*100)
	}

	// 檢查是否接近限制 (90%)
	if stats.UsageRatio > 0.9 {
		h.logger.Warn("記憶體使用接近限制", map[string]interface{}{
			"current_mb":  stats.Alloc / 1024 / 1024,
			"limit_mb":    h.memoryLimit / 1024 / 1024,
			"usage_ratio": stats.UsageRatio,
		})

		// 建議執行GC
		if stats.UsageRatio > 0.95 {
			h.logger.Info("記憶體使用率超過95%，建議執行GC")
			return fmt.Errorf("記憶體使用率過高: %.2f%%", stats.UsageRatio*100)
		}
	}

	// 檢查堆碎片化
	if stats.HeapSys > 0 {
		heapEfficiency := float64(stats.HeapInuse) / float64(stats.HeapSys)
		if heapEfficiency < 0.6 {
			h.logger.Warn("堆記憶體效率低", map[string]interface{}{
				"heap_efficiency": heapEfficiency,
				"heap_inuse_mb":   stats.HeapInuse / 1024 / 1024,
				"heap_sys_mb":     stats.HeapSys / 1024 / 1024,
				"heap_idle_mb":    stats.HeapIdle / 1024 / 1024,
			})
		}
	}

	return nil
}

// getMemoryUsage 獲取當前記憶體使用
func (h *LargeFileHandler) getMemoryUsage() int64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return int64(m.Alloc)
}

// WriteCSVStreaming 流式寫入 CSV 文件
func (h *LargeFileHandler) WriteCSVStreaming(filename string, data [][]string, callback ProgressCallback) error {
	h.logger.Info("開始流式寫入 CSV 文件", map[string]interface{}{
		"filename":  filename,
		"row_count": len(data),
	})

	// 驗證路徑
	sanitizedPath := h.pathValidator.SanitizePath(filename)
	if err := h.pathValidator.ValidateFilePath(sanitizedPath); err != nil {
		return errors.WrapError(err, errors.ErrCodePathValidation, "路徑驗證失敗")
	}

	file, err := os.Create(sanitizedPath)
	if err != nil {
		return errors.WrapError(err, errors.ErrCodeFileNotFound, "無法創建文件")
	}
	defer file.Close()

	// 使用緩衝寫入
	bufferedWriter := bufio.NewWriterSize(file, h.bufferSize)
	defer bufferedWriter.Flush()

	// 寫入 BOM（如果啟用）
	if h.config.BOMEnabled {
		if _, err := bufferedWriter.Write(BOMBytes); err != nil {
			return fmt.Errorf("無法寫入 BOM: %w", err)
		}
	}

	writer := csv.NewWriter(bufferedWriter)

	totalRows := len(data)

	// 分批寫入
	for i, row := range data {
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("寫入第 %d 行失敗: %w", i+1, err)
		}

		// 定期刷新和進度回調
		if i%h.chunkSize == 0 {
			writer.Flush()
			if err := writer.Error(); err != nil {
				return fmt.Errorf("刷新寫入緩衝區失敗: %w", err)
			}

			if callback != nil {
				percentage := float64(i+1) / float64(totalRows) * 100
				callback(int64(i+1), int64(totalRows), percentage)
			}
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("最終刷新失敗: %w", err)
	}

	// 最終進度回調
	if callback != nil {
		callback(int64(totalRows), int64(totalRows), 100.0)
	}

	h.logger.Info("流式寫入完成", map[string]interface{}{
		"filename":  sanitizedPath,
		"row_count": totalRows,
	})

	return nil
}
