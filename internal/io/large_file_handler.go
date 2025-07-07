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
	chunk := make([][]string, 0, h.chunkSize)
	chunk = append(chunk, headers) // 添加標題行

	// 流式處理
	for {
		// 檢查記憶體使用
		if err := h.checkMemoryUsage(); err != nil {
			h.logger.Warn("記憶體使用過高，觸發垃圾回收", map[string]interface{}{
				"processed_lines": processedLines,
			})
			runtime.GC()
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
			continue
		}

		// 驗證數據行
		if len(record) != len(headers) {
			h.logger.Warn("行列數不匹配，跳過", map[string]interface{}{
				"expected_columns": len(headers),
				"actual_columns":   len(record),
				"line":             processedLines + 1,
			})
			continue
		}

		chunk = append(chunk, record)
		processedLines++

		// 當塊達到指定大小時，進行進度回調
		if len(chunk)-1 >= h.chunkSize { // -1 因為包含標題行
			if callback != nil {
				percentage := float64(processedLines) / float64(fileInfo.LineCount) * 100
				callback(processedLines, fileInfo.LineCount, percentage)
			}

			// 清理塊數據以釋放記憶體
			chunk = chunk[:1] // 保留標題行
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
	chunk := make([][]string, 0, h.chunkSize)
	chunk = append(chunk, headers) // 添加標題行

	// 累積數據用於計算（使用滑動視窗）
	dataBuffer := make([]models.EMGData, 0, windowSize*2)
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
			if len(dataBuffer) > windowSize*2 {
				removeCount := len(dataBuffer) - windowSize
				if removeCount > 0 && removeCount < len(dataBuffer) {
					copy(dataBuffer, dataBuffer[removeCount:])
					dataBuffer = dataBuffer[:len(dataBuffer)-removeCount]
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

	h.logger.Info("分塊處理完成", map[string]interface{}{
		"processed_lines": processedLines,
		"duration_ms":     result.Duration.Milliseconds(),
		"memory_used_mb":  result.MemoryUsed / 1024 / 1024,
		"results_count":   len(result.Results),
	})

	return result, nil
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
	channels := make([]float64, 0, len(record)-1)
	for i := 1; i < len(record); i++ {
		val, err := util.Str2Number[float64, int](record[i], scalingFactor)
		if err != nil {
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

// checkMemoryUsage 檢查記憶體使用
func (h *LargeFileHandler) checkMemoryUsage() error {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	if int64(m.Alloc) > h.memoryLimit {
		return fmt.Errorf("記憶體使用超過限制: %d > %d", m.Alloc, h.memoryLimit)
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
