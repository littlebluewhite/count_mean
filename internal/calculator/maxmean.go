package calculator

import (
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/util"
	"fmt"
	"math"
	"runtime"
	"sync"
	"time"
)

// MaxMeanCalculator 處理最大平均值計算
type MaxMeanCalculator struct {
	scalingFactor          int
	logger                 *logging.Logger
	workerCount            int
	progressCallback       models.ProgressCallback
	startTime              time.Time
	backpressureController *models.BackpressureController
}

// channelJob 表示一個通道計算任務
type channelJob struct {
	channelIdx int
	dataset    *models.EMGDataset
	windowSize int
}

// channelResult 表示通道計算結果
type channelResult struct {
	channelIdx int
	result     models.MaxMeanResult
	err        error
}

// NewMaxMeanCalculator 創建新的最大平均值計算器
func NewMaxMeanCalculator(scalingFactor int) *MaxMeanCalculator {
	// 默認使用 CPU 核心數作為工作協程數量，但不超過16個
	workerCount := runtime.NumCPU()
	if workerCount > 16 {
		workerCount = 16
	}

	// 創建背壓控制器配置
	backpressureConfig := models.DefaultBackpressureConfig()
	backpressureConfig.MaxWorkers = workerCount

	return &MaxMeanCalculator{
		scalingFactor:          scalingFactor,
		logger:                 logging.GetLogger("max_mean_calculator"),
		workerCount:            workerCount,
		backpressureController: models.NewBackpressureController(backpressureConfig),
	}
}

// SetProgressCallback 設置進度回調函數
func (c *MaxMeanCalculator) SetProgressCallback(callback models.ProgressCallback) {
	c.progressCallback = callback
}

// SetBackpressureConfig 設置背壓控制配置
func (c *MaxMeanCalculator) SetBackpressureConfig(config *models.BackpressureConfig) {
	if config != nil {
		c.backpressureController = models.NewBackpressureController(config)
	}
}

// GetBackpressureStats 獲取背壓統計信息
func (c *MaxMeanCalculator) GetBackpressureStats() models.BackpressureStats {
	if c.backpressureController != nil {
		return c.backpressureController.GetStats()
	}
	return models.BackpressureStats{}
}

// getMemoryUsageInfo 獲取記憶體使用信息
func (c *MaxMeanCalculator) getMemoryUsageInfo() map[string]interface{} {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	info := map[string]interface{}{
		"alloc_mb":       memStats.Alloc / 1024 / 1024,
		"total_alloc_mb": memStats.TotalAlloc / 1024 / 1024,
		"sys_mb":         memStats.Sys / 1024 / 1024,
		"num_gc":         memStats.NumGC,
	}

	if c.backpressureController != nil {
		info["usage_ratio"] = c.backpressureController.GetMemoryUsageRatio()
		info["is_throttled"] = c.backpressureController.IsThrottled()
	}

	return info
}

// reportProgress 報告進度
func (c *MaxMeanCalculator) reportProgress(currentStep, totalSteps int, status string, channelIndex int, channelName string) {
	if c.progressCallback == nil {
		return
	}

	percentage := float64(currentStep) / float64(totalSteps) * 100
	if percentage > 100 {
		percentage = 100
	}

	elapsed := time.Since(c.startTime)
	var estimated string
	if currentStep > 0 && currentStep < totalSteps {
		avgTimePerStep := elapsed / time.Duration(currentStep)
		remainingSteps := totalSteps - currentStep
		estimatedRemaining := avgTimePerStep * time.Duration(remainingSteps)
		estimated = estimatedRemaining.Round(time.Second).String()
	} else {
		estimated = "計算中..."
	}

	info := models.ProgressInfo{
		CurrentStep:   currentStep,
		TotalSteps:    totalSteps,
		Percentage:    percentage,
		Status:        status,
		ChannelIndex:  channelIndex,
		ChannelName:   channelName,
		ElapsedTime:   elapsed.Round(time.Second).String(),
		EstimatedTime: estimated,
	}

	c.progressCallback(info)
}

// calculateChannelMaxMean 計算單個通道的最大平均值 (使用增量滑動窗口優化)
func (c *MaxMeanCalculator) calculateChannelMaxMean(dataset *models.EMGDataset, channelIdx, windowSize int) (models.MaxMeanResult, error) {
	maxMean := 0.0
	bestStartIdx := 0

	// 計算第一個窗口的初始和
	windowSum := 0.0
	for i := 0; i < windowSize; i++ {
		if channelIdx < len(dataset.Data[i].Channels) {
			windowSum += dataset.Data[i].Channels[channelIdx]
		}
	}

	// 計算第一個窗口的平均值
	currentMean := windowSum / float64(windowSize)
	maxMean = currentMean

	// 使用增量滑動窗口計算後續窗口
	for startIdx := 1; startIdx <= len(dataset.Data)-windowSize; startIdx++ {
		// 移除窗口左側的值
		if channelIdx < len(dataset.Data[startIdx-1].Channels) {
			windowSum -= dataset.Data[startIdx-1].Channels[channelIdx]
		}

		// 添加窗口右側的新值
		rightIdx := startIdx + windowSize - 1
		if rightIdx < len(dataset.Data) && channelIdx < len(dataset.Data[rightIdx].Channels) {
			windowSum += dataset.Data[rightIdx].Channels[channelIdx]
		}

		// 計算當前窗口的平均值
		currentMean = windowSum / float64(windowSize)
		if currentMean > maxMean {
			maxMean = currentMean
			bestStartIdx = startIdx
		}
	}

	result := models.MaxMeanResult{
		ColumnIndex: channelIdx + 1, // +1 因為第一列是時間
		StartTime:   dataset.Data[bestStartIdx].Time,
		EndTime:     dataset.Data[bestStartIdx+windowSize-1].Time,
		MaxMean:     maxMean,
	}

	return result, nil
}

// calculateChannelMaxMeanWithRange 計算單個通道在指定範圍內的最大平均值 (使用增量滑動窗口優化)
func (c *MaxMeanCalculator) calculateChannelMaxMeanWithRange(dataset *models.EMGDataset, channelIdx, windowSize, startIdx, endIdx int) (models.MaxMeanResult, error) {
	maxMean := 0.0
	bestStartIdx := startIdx

	// 計算第一個窗口的初始和
	windowSum := 0.0
	for i := startIdx; i < startIdx+windowSize; i++ {
		if channelIdx < len(dataset.Data[i].Channels) {
			windowSum += dataset.Data[i].Channels[channelIdx]
		}
	}

	// 計算第一個窗口的平均值
	currentMean := windowSum / float64(windowSize)
	maxMean = currentMean

	// 使用增量滑動窗口計算後續窗口
	for winStartIdx := startIdx + 1; winStartIdx <= endIdx-windowSize+1; winStartIdx++ {
		// 移除窗口左側的值
		if channelIdx < len(dataset.Data[winStartIdx-1].Channels) {
			windowSum -= dataset.Data[winStartIdx-1].Channels[channelIdx]
		}

		// 添加窗口右側的新值
		rightIdx := winStartIdx + windowSize - 1
		if rightIdx < len(dataset.Data) && channelIdx < len(dataset.Data[rightIdx].Channels) {
			windowSum += dataset.Data[rightIdx].Channels[channelIdx]
		}

		// 計算當前窗口的平均值
		currentMean = windowSum / float64(windowSize)
		if currentMean > maxMean {
			maxMean = currentMean
			bestStartIdx = winStartIdx
		}
	}

	result := models.MaxMeanResult{
		ColumnIndex: channelIdx + 1, // +1 因為第一列是時間
		StartTime:   dataset.Data[bestStartIdx].Time,
		EndTime:     dataset.Data[bestStartIdx+windowSize-1].Time,
		MaxMean:     maxMean,
	}

	return result, nil
}

// channelRangeJob 表示一個通道範圍計算任務
type channelRangeJob struct {
	channelIdx int
	dataset    *models.EMGDataset
	windowSize int
	startIdx   int
	endIdx     int
}

// workerWithRange 處理通道範圍計算任務的工作協程
func (c *MaxMeanCalculator) workerWithRange(jobs <-chan channelRangeJob, results chan<- channelResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		// 背壓控制：等待足夠的容量
		if c.backpressureController != nil {
			c.backpressureController.WaitForCapacity()
			c.backpressureController.RecordJobStart()
		}

		c.logger.Debug("工作協程開始處理通道範圍計算", map[string]interface{}{
			"channel_index": job.channelIdx + 1,
			"start_idx":     job.startIdx,
			"end_idx":       job.endIdx,
			"memory_usage":  c.getMemoryUsageInfo(),
		})

		result, err := c.calculateChannelMaxMeanWithRange(job.dataset, job.channelIdx, job.windowSize, job.startIdx, job.endIdx)

		results <- channelResult{
			channelIdx: job.channelIdx,
			result:     result,
			err:        err,
		}

		// 記錄任務完成
		if c.backpressureController != nil {
			c.backpressureController.RecordJobComplete()
		}
	}
}

// worker 處理通道計算任務的工作協程
func (c *MaxMeanCalculator) worker(jobs <-chan channelJob, results chan<- channelResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		// 背壓控制：等待足夠的容量
		if c.backpressureController != nil {
			c.backpressureController.WaitForCapacity()
			c.backpressureController.RecordJobStart()
		}

		c.logger.Debug("工作協程開始處理通道", map[string]interface{}{
			"channel_index": job.channelIdx + 1,
			"memory_usage":  c.getMemoryUsageInfo(),
		})

		result, err := c.calculateChannelMaxMean(job.dataset, job.channelIdx, job.windowSize)

		results <- channelResult{
			channelIdx: job.channelIdx,
			result:     result,
			err:        err,
		}

		// 記錄任務完成
		if c.backpressureController != nil {
			c.backpressureController.RecordJobComplete()
		}
	}
}

// Calculate 計算指定窗口大小的最大平均值
func (c *MaxMeanCalculator) Calculate(dataset *models.EMGDataset, windowSize int) ([]models.MaxMeanResult, error) {
	c.startTime = time.Now()

	if dataset == nil || len(dataset.Data) == 0 {
		err := fmt.Errorf("數據集為空")
		dataLength := 0
		if dataset != nil {
			dataLength = len(dataset.Data)
		}
		c.logger.Error("計算參數驗證失敗", err, map[string]interface{}{
			"dataset_nil": dataset == nil,
			"data_length": dataLength,
		})
		return nil, err
	}

	c.logger.Info("開始最大平均值計算", map[string]interface{}{
		"window_size":   windowSize,
		"data_points":   len(dataset.Data),
		"channel_count": len(dataset.Data[0].Channels),
	})

	if len(dataset.Data) < windowSize {
		err := fmt.Errorf("數據集無效或窗口大小過大")
		c.logger.Error("窗口大小驗證失敗", err, map[string]interface{}{
			"data_length": len(dataset.Data),
			"window_size": windowSize,
		})
		return nil, err
	}

	if windowSize < 1 {
		err := fmt.Errorf("窗口大小必須大於 0")
		c.logger.Error("窗口大小驗證失敗", err, map[string]interface{}{
			"window_size": windowSize,
		})
		return nil, err
	}

	channelCount := len(dataset.Data[0].Channels)
	results := make([]models.MaxMeanResult, channelCount)

	// 根據背壓控制調整工作協程數
	actualWorkerCount := c.workerCount
	if c.backpressureController != nil {
		c.backpressureController.Reset()
		actualWorkerCount = c.backpressureController.GetOptimalWorkerCount()
	}

	c.logger.Info("開始並行處理通道計算", map[string]interface{}{
		"worker_count":        c.workerCount,
		"actual_worker_count": actualWorkerCount,
		"channel_count":       channelCount,
		"memory_usage":        c.getMemoryUsageInfo(),
	})

	// 初始化進度報告
	c.reportProgress(0, channelCount, "初始化並行計算", 0, "")

	// 創建任務和結果通道
	jobs := make(chan channelJob, channelCount)
	resultsChan := make(chan channelResult, channelCount)

	// 啟動工作協程池
	var wg sync.WaitGroup
	for w := 0; w < actualWorkerCount; w++ {
		wg.Add(1)
		go c.worker(jobs, resultsChan, &wg)
	}

	// 發送任務到工作協程
	go func() {
		defer close(jobs)
		for channelIdx := 0; channelIdx < channelCount; channelIdx++ {
			jobs <- channelJob{
				channelIdx: channelIdx,
				dataset:    dataset,
				windowSize: windowSize,
			}
		}
	}()

	// 等待所有工作協程完成並關閉結果通道
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// 收集結果
	processedCount := 0
	for result := range resultsChan {
		if result.err != nil {
			c.logger.Error("通道計算失敗", result.err, map[string]interface{}{
				"channel_index": result.channelIdx + 1,
			})
			return nil, fmt.Errorf("通道 %d 計算失敗: %w", result.channelIdx+1, result.err)
		}

		results[result.channelIdx] = result.result
		processedCount++

		// 報告進度
		channelName := fmt.Sprintf("Ch%d", result.channelIdx+1)
		if len(dataset.Headers) > result.channelIdx+1 {
			channelName = dataset.Headers[result.channelIdx+1]
		}

		status := fmt.Sprintf("通道 %s 計算完成", channelName)
		c.reportProgress(processedCount, channelCount, status, result.channelIdx+1, channelName)

		c.logger.Debug("通道計算完成", map[string]interface{}{
			"channel_index": result.channelIdx + 1,
			"progress":      fmt.Sprintf("%d/%d", processedCount, channelCount),
		})
	}

	duration := time.Since(c.startTime)
	c.logger.Info("最大平均值計算完成", map[string]interface{}{
		"duration_ms":   duration.Milliseconds(),
		"channel_count": len(results),
		"window_size":   windowSize,
	})

	// 報告完成狀態
	c.reportProgress(channelCount, channelCount, "計算完成", 0, "")

	// 記錄背壓控制統計
	if c.backpressureController != nil {
		stats := c.backpressureController.GetStats()
		c.logger.Info("背壓控制統計", map[string]interface{}{
			"peak_memory_mb":      stats.PeakMemoryUsage / 1024 / 1024,
			"average_workers":     stats.AverageWorkers,
			"throttle_events":     stats.ThrottleEvents,
			"processing_time_ms":  stats.TotalProcessingTime.Milliseconds(),
			"throughput_jobs_sec": stats.ThroughputJobsPerSec,
		})
	}

	// 主動觸發垃圾回收以釋放計算過程中產生的臨時記憶體
	runtime.GC()
	c.logger.Debug("計算完成後觸發垃圾回收")

	return results, nil
}

// CalculateFromRawData 從原始字符串數據計算
func (c *MaxMeanCalculator) CalculateFromRawData(records [][]string, windowSize int) ([]models.MaxMeanResult, error) {
	c.logger.Info("開始從原始數據計算最大平均值", map[string]interface{}{
		"record_count": len(records),
		"window_size":  windowSize,
	})

	dataset, err := c.parseRawData(records)
	if err != nil {
		c.logger.Error("原始數據解析失敗", err)
		return nil, fmt.Errorf("解析數據失敗: %w", err)
	}

	return c.Calculate(dataset, windowSize)
}

// CalculateFromRawDataWithRange 從原始字符串數據計算指定時間範圍內的最大平均值
func (c *MaxMeanCalculator) CalculateFromRawDataWithRange(records [][]string, windowSize int, startRange, endRange float64) ([]models.MaxMeanResult, error) {
	c.logger.Info("開始從原始數據計算指定範圍內的最大平均值", map[string]interface{}{
		"record_count": len(records),
		"window_size":  windowSize,
		"start_range":  startRange,
		"end_range":    endRange,
	})

	dataset, err := c.parseRawData(records)
	if err != nil {
		c.logger.Error("原始數據解析失敗", err)
		return nil, fmt.Errorf("解析數據失敗: %w", err)
	}

	return c.CalculateWithRange(dataset, windowSize, startRange, endRange)
}

// CalculateWithRange 計算指定時間範圍內的最大平均值
func (c *MaxMeanCalculator) CalculateWithRange(dataset *models.EMGDataset, windowSize int, startRange, endRange float64) ([]models.MaxMeanResult, error) {
	c.startTime = time.Now()

	if dataset == nil || len(dataset.Data) < windowSize {
		err := fmt.Errorf("數據集無效或窗口大小過大")
		dataLength := 0
		if dataset != nil {
			dataLength = len(dataset.Data)
		}
		c.logger.Error("範圍計算參數驗證失敗", err, map[string]interface{}{
			"dataset_nil": dataset == nil,
			"data_length": dataLength,
			"window_size": windowSize,
		})
		return nil, err
	}

	c.logger.Info("開始指定範圍內的最大平均值計算", map[string]interface{}{
		"window_size":   windowSize,
		"start_range":   startRange,
		"end_range":     endRange,
		"data_points":   len(dataset.Data),
		"channel_count": len(dataset.Data[0].Channels),
	})

	if windowSize < 1 {
		err := fmt.Errorf("窗口大小必須大於 0")
		c.logger.Error("窗口大小驗證失敗", err, map[string]interface{}{
			"window_size": windowSize,
		})
		return nil, err
	}

	// 轉換時間範圍為縮放後的值
	scaledStartRange := startRange * math.Pow10(c.scalingFactor)
	scaledEndRange := endRange * math.Pow10(c.scalingFactor)

	// 找到時間範圍內的數據索引
	startIdx := -1
	endIdx := -1

	// 如果 endRange 為 0，表示使用所有數據直到最後
	if endRange == 0 {
		endIdx = len(dataset.Data) - 1
		for i, data := range dataset.Data {
			if startIdx == -1 && data.Time >= scaledStartRange {
				startIdx = i
				break
			}
		}
	} else {
		for i, data := range dataset.Data {
			if startIdx == -1 && data.Time >= scaledStartRange {
				startIdx = i
			}
			if data.Time <= scaledEndRange {
				endIdx = i
			}
		}
	}

	if startIdx == -1 || endIdx == -1 || endIdx-startIdx+1 < windowSize {
		err := fmt.Errorf("指定時間範圍內的數據不足以進行窗口分析")
		c.logger.Error("時間範圍內數據不足", err, map[string]interface{}{
			"start_idx":        startIdx,
			"end_idx":          endIdx,
			"available_points": endIdx - startIdx + 1,
			"required_points":  windowSize,
			"start_range":      startRange,
			"end_range":        endRange,
		})
		return nil, err
	}

	c.logger.Debug("時間範圍分析完成", map[string]interface{}{
		"start_idx":        startIdx,
		"end_idx":          endIdx,
		"available_points": endIdx - startIdx + 1,
		"scaled_start":     scaledStartRange,
		"scaled_end":       scaledEndRange,
	})

	channelCount := len(dataset.Data[0].Channels)
	results := make([]models.MaxMeanResult, channelCount)

	// 根據背壓控制調整工作協程數
	actualWorkerCount := c.workerCount
	if c.backpressureController != nil {
		c.backpressureController.Reset()
		actualWorkerCount = c.backpressureController.GetOptimalWorkerCount()
	}

	c.logger.Info("開始並行處理通道範圍計算", map[string]interface{}{
		"worker_count":        c.workerCount,
		"actual_worker_count": actualWorkerCount,
		"channel_count":       channelCount,
		"start_idx":           startIdx,
		"end_idx":             endIdx,
		"memory_usage":        c.getMemoryUsageInfo(),
	})

	// 初始化進度報告
	c.reportProgress(0, channelCount, "初始化範圍並行計算", 0, "")

	// 創建任務和結果通道
	jobs := make(chan channelRangeJob, channelCount)
	resultsChan := make(chan channelResult, channelCount)

	// 啟動工作協程池
	var wg sync.WaitGroup
	for w := 0; w < actualWorkerCount; w++ {
		wg.Add(1)
		go c.workerWithRange(jobs, resultsChan, &wg)
	}

	// 發送任務到工作協程
	go func() {
		defer close(jobs)
		for channelIdx := 0; channelIdx < channelCount; channelIdx++ {
			jobs <- channelRangeJob{
				channelIdx: channelIdx,
				dataset:    dataset,
				windowSize: windowSize,
				startIdx:   startIdx,
				endIdx:     endIdx,
			}
		}
	}()

	// 等待所有工作協程完成並關閉結果通道
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// 收集結果
	processedCount := 0
	for result := range resultsChan {
		if result.err != nil {
			c.logger.Error("通道範圍計算失敗", result.err, map[string]interface{}{
				"channel_index": result.channelIdx + 1,
				"start_idx":     startIdx,
				"end_idx":       endIdx,
			})
			return nil, fmt.Errorf("通道 %d 範圍計算失敗: %w", result.channelIdx+1, result.err)
		}

		results[result.channelIdx] = result.result
		processedCount++

		// 報告進度
		channelName := fmt.Sprintf("Ch%d", result.channelIdx+1)
		if len(dataset.Headers) > result.channelIdx+1 {
			channelName = dataset.Headers[result.channelIdx+1]
		}

		status := fmt.Sprintf("範圍計算: 通道 %s 完成", channelName)
		c.reportProgress(processedCount, channelCount, status, result.channelIdx+1, channelName)

		c.logger.Debug("通道範圍計算完成", map[string]interface{}{
			"channel_index": result.channelIdx + 1,
			"progress":      fmt.Sprintf("%d/%d", processedCount, channelCount),
		})
	}

	duration := time.Since(c.startTime)
	c.logger.Info("指定範圍內最大平均值計算完成", map[string]interface{}{
		"duration_ms":      duration.Milliseconds(),
		"channel_count":    len(results),
		"window_size":      windowSize,
		"start_range":      startRange,
		"end_range":        endRange,
		"processed_points": endIdx - startIdx + 1,
	})

	// 報告完成狀態
	c.reportProgress(channelCount, channelCount, "範圍計算完成", 0, "")

	// 記錄背壓控制統計
	if c.backpressureController != nil {
		stats := c.backpressureController.GetStats()
		c.logger.Info("範圍計算背壓控制統計", map[string]interface{}{
			"peak_memory_mb":      stats.PeakMemoryUsage / 1024 / 1024,
			"average_workers":     stats.AverageWorkers,
			"throttle_events":     stats.ThrottleEvents,
			"processing_time_ms":  stats.TotalProcessingTime.Milliseconds(),
			"throughput_jobs_sec": stats.ThroughputJobsPerSec,
		})
	}

	// 主動觸發垃圾回收以釋放計算過程中產生的臨時記憶體
	runtime.GC()
	c.logger.Debug("範圍計算完成後觸發垃圾回收")

	return results, nil
}

// parseRawData 解析原始字符串數據
func (c *MaxMeanCalculator) parseRawData(records [][]string) (*models.EMGDataset, error) {
	c.logger.Debug("開始解析原始數據", map[string]interface{}{
		"record_count":   len(records),
		"scaling_factor": c.scalingFactor,
	})

	if len(records) < 2 {
		err := fmt.Errorf("數據至少需要包含標題行和一行數據")
		c.logger.Error("原始數據結構驗證失敗", err, map[string]interface{}{
			"record_count": len(records),
		})
		return nil, err
	}

	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}

	// 複製標題
	copy(dataset.Headers, records[0])

	// 解析數據
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue // 跳過無效行
		}

		// 解析時間
		if row[0] == "" {
			c.logger.Debug("跳過空白時間行", map[string]interface{}{
				"row_number": i + 1,
			})
			continue // 跳過空白時間值的行
		}

		timeVal, err := util.Str2Number[float64, int](row[0], c.scalingFactor)
		if err != nil {
			c.logger.Warn("時間值解析失敗，跳過此行", map[string]interface{}{
				"row_number": i + 1,
				"time_value": row[0],
				"error":      err.Error(),
			})
			continue // 跳過無法解析的行
		}

		// 解析通道數據
		channels := make([]float64, 0, len(row)-1)
		for j := 1; j < len(row); j++ {
			val, err := util.Str2Number[float64, int](row[j], c.scalingFactor)
			if err != nil {
				c.logger.Error("通道數據解析失敗", err, map[string]interface{}{
					"row_number":    i + 1,
					"column_number": j + 1,
					"value":         row[j],
				})
				return nil, fmt.Errorf("解析數據失敗在第 %d 行第 %d 列: %w", i+1, j+1, err)
			}
			channels = append(channels, val)
		}

		data := models.EMGData{
			Time:     timeVal,
			Channels: channels,
		}

		dataset.Data = append(dataset.Data, data)
	}

	if len(dataset.Data) == 0 {
		err := fmt.Errorf("解析後數據集為空，所有行都被跳過")
		c.logger.Error("原始數據解析失敗", err, map[string]interface{}{
			"header_count": len(dataset.Headers),
		})
		return nil, err
	}

	c.logger.Info("原始數據解析完成", map[string]interface{}{
		"parsed_records": len(dataset.Data),
		"channel_count":  len(dataset.Data[0].Channels),
		"header_count":   len(dataset.Headers),
	})

	return dataset, nil
}
