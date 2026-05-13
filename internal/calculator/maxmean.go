package calculator

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sync"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// MaxWorkerCount is the maximum number of worker goroutines for parallel calculation.
const MaxWorkerCount = 16

// MicrosecondsPerSecond is the number of microseconds in one second.
const MicrosecondsPerSecond = 1_000_000

// CalculationOptions 計算選項.
type CalculationOptions struct {
	StartRange float64 // 起始時間範圍（0 表示從頭開始）
	EndRange   float64 // 結束時間範圍（0 表示到結尾）
}

// MaxMeanCalculator 處理最大平均值計算.
type MaxMeanCalculator struct {
	scalingFactor          int
	logger                 *logging.Logger
	workerCount            int
	progressCallback       models.ProgressCallback
	backpressureController *models.BackpressureController
	dataParser             *parsers.DataParser
	slidingWindow          *SlidingWindowCalculator
}

// calculationJob 表示一個通道計算任務.
type calculationJob struct {
	channelIdx int
	provider   *EMGDatasetProvider
	windowSize int
	startIdx   int
	endIdx     int
}

// channelResult 表示通道計算結果.
type channelResult struct {
	channelIdx int
	result     models.MaxMeanResult
	err        error
}

// orchestrator 處理計算流程的內部協調器.
type orchestrator struct {
	calc            *MaxMeanCalculator
	dataset         *models.EMGDataset
	provider        *EMGDatasetProvider
	windowSize      int
	startIdx        int
	endIdx          int
	channelCount    int
	progressTracker *models.ProgressTracker
	isRanged        bool
}

// NewMaxMeanCalculator 創建新的最大平均值計算器.
func NewMaxMeanCalculator(scalingFactor int) *MaxMeanCalculator {
	workerCount := runtime.NumCPU()
	if workerCount > MaxWorkerCount {
		workerCount = MaxWorkerCount
	}

	backpressureConfig := models.DefaultBackpressureConfig()
	backpressureConfig.MaxWorkers = workerCount

	logger := logging.GetLogger("max_mean_calculator")

	return &MaxMeanCalculator{
		scalingFactor:          scalingFactor,
		logger:                 logger,
		workerCount:            workerCount,
		backpressureController: models.NewBackpressureController(backpressureConfig),
		dataParser:             parsers.NewDataParserWithLogger(scalingFactor, logger),
		slidingWindow:          NewSlidingWindowCalculator(),
	}
}

// ScalingFactor exposes the calculator's configured scaling factor. Used by gui
// snapshot-consistency tests to assert that a captured *appState's config and
// maxMeanCalc agree (i.e., no torn snapshot across atomic.Pointer reads).
func (c *MaxMeanCalculator) ScalingFactor() int {
	return c.scalingFactor
}

// SetProgressCallback 設置進度回調函數.
func (c *MaxMeanCalculator) SetProgressCallback(callback models.ProgressCallback) {
	if c == nil {
		return
	}

	c.progressCallback = callback
}

// GetBackpressureStats 獲取背壓統計信息.
func (c *MaxMeanCalculator) GetBackpressureStats() models.BackpressureStats {
	if c.backpressureController != nil {
		return c.backpressureController.GetStats()
	}

	return models.BackpressureStats{}
}

// getMemoryUsageInfo 獲取記憶體使用信息（委託給 BackpressureController）.
func (c *MaxMeanCalculator) getMemoryUsageInfo() map[string]interface{} {
	if c.backpressureController != nil {
		return c.backpressureController.GetMemoryUsageInfo()
	}
	// 如果沒有背壓控制器，返回基本記憶體信息
	var memStats runtime.MemStats

	runtime.ReadMemStats(&memStats)

	return map[string]interface{}{
		"alloc_mb":       memStats.Alloc / 1024 / 1024,
		"total_alloc_mb": memStats.TotalAlloc / 1024 / 1024,
		"sys_mb":         memStats.Sys / 1024 / 1024,
		"num_gc":         memStats.NumGC,
	}
}

// Calculate 計算指定窗口大小的最大平均值.
// ctx 用於取消長時間執行的計算；GUI 與 CLI 應傳入可取消的 context，
// 不確定來源時使用 context.Background()。
func (c *MaxMeanCalculator) Calculate(
	ctx context.Context,
	dataset *models.EMGDataset,
	windowSize int,
) ([]models.MaxMeanResult, error) {
	return c.calculateWithOptions(ctx, dataset, windowSize, CalculationOptions{})
}

// CalculateWithRange 計算指定時間範圍內的最大平均值.
func (c *MaxMeanCalculator) CalculateWithRange(
	ctx context.Context,
	dataset *models.EMGDataset,
	windowSize int,
	startRange, endRange float64,
) ([]models.MaxMeanResult, error) {
	return c.calculateWithOptions(ctx, dataset, windowSize, CalculationOptions{
		StartRange: startRange,
		EndRange:   endRange,
	})
}

// calculateWithOptions 統一的計算入口點.
func (c *MaxMeanCalculator) calculateWithOptions(
	ctx context.Context,
	dataset *models.EMGDataset,
	windowSize int,
	opts CalculationOptions,
) ([]models.MaxMeanResult, error) {
	if err := c.validateInput(dataset, windowSize); err != nil {
		return nil, err
	}

	isRanged := opts.StartRange != 0 || opts.EndRange != 0

	c.logCalculationStart(dataset, windowSize, opts, isRanged)

	startIdx, endIdx, err := c.resolveDataRange(dataset, windowSize, opts, isRanged)
	if err != nil {
		return nil, err
	}

	orch := c.newOrchestrator(dataset, windowSize, startIdx, endIdx, isRanged)

	return orch.execute(ctx)
}

// validateInput 驗證輸入參數.
func (c *MaxMeanCalculator) validateInput(dataset *models.EMGDataset, windowSize int) error {
	if dataset == nil || len(dataset.Data) == 0 {
		err := calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "數據集為空")

		dataLength := 0
		if dataset != nil {
			dataLength = len(dataset.Data)
		}

		c.logger.Error("計算參數驗證失敗", err, map[string]interface{}{
			"dataset_nil": dataset == nil,
			"data_length": dataLength,
		})

		return err
	}

	if windowSize < 1 {
		err := calcerrors.NewCalculatorError(calcerrors.ErrInvalidWindowSize, "窗口大小必須大於 0")
		c.logger.Error("窗口大小驗證失敗", err, map[string]interface{}{
			"window_size": windowSize,
		})

		return err
	}

	if len(dataset.Data) < windowSize {
		err := calcerrors.NewCalculatorError(calcerrors.ErrWindowTooLarge, "數據集無效或窗口大小過大")
		c.logger.Error("窗口大小驗證失敗", err, map[string]interface{}{
			"data_length": len(dataset.Data),
			"window_size": windowSize,
		})

		return err
	}

	return nil
}

// logCalculationStart 記錄計算開始日誌.
//
//nolint:revive // flag-parameter: isRanged indicates range-based logging format
func (c *MaxMeanCalculator) logCalculationStart(
	dataset *models.EMGDataset,
	windowSize int,
	opts CalculationOptions,
	isRanged bool,
) {
	logCtx := map[string]interface{}{
		"window_size":   windowSize,
		"data_points":   len(dataset.Data),
		"channel_count": len(dataset.Data[0].Channels),
	}
	if isRanged {
		logCtx["start_range"] = opts.StartRange
		logCtx["end_range"] = opts.EndRange
		c.logger.Info("開始指定範圍內的最大平均值計算", logCtx)
	} else {
		c.logger.Info("開始最大平均值計算", logCtx)
	}
}

// resolveDataRange 解析數據範圍索引.
//
//nolint:gocognit,revive,nonamedreturns // complex logic; flag-parameter; named returns for clarity
func (c *MaxMeanCalculator) resolveDataRange(
	dataset *models.EMGDataset,
	windowSize int,
	opts CalculationOptions,
	isRanged bool,
) (startIdx, endIdx int, err error) {
	if !isRanged {
		return 0, len(dataset.Data) - 1, nil
	}

	// 轉換時間範圍為縮放後的值
	scaledStartRange := opts.StartRange * math.Pow10(c.scalingFactor)
	scaledEndRange := opts.EndRange * math.Pow10(c.scalingFactor)

	// 將時間轉換為整數微秒進行比較，避免浮點數精度問題
	startRangeUs := int64(math.Round(scaledStartRange * MicrosecondsPerSecond))
	endRangeUs := int64(math.Round(scaledEndRange * MicrosecondsPerSecond))

	startIdx = -1
	endIdx = -1

	// 如果 endRange 為 0，表示使用所有數據直到最後
	//nolint:nestif // nested blocks necessary for different end range handling
	if opts.EndRange == 0 {
		endIdx = len(dataset.Data) - 1

		for i, data := range dataset.Data {
			dataTimeUs := int64(math.Round(data.Time * MicrosecondsPerSecond))
			if startIdx == -1 && dataTimeUs >= startRangeUs {
				startIdx = i
				break
			}
		}
	} else {
		for i, data := range dataset.Data {
			dataTimeUs := int64(math.Round(data.Time * MicrosecondsPerSecond))
			if startIdx == -1 && dataTimeUs >= startRangeUs {
				startIdx = i
			}

			if dataTimeUs <= endRangeUs {
				endIdx = i
			}
		}
	}

	if startIdx == -1 || endIdx == -1 || endIdx-startIdx+1 < windowSize {
		err := calcerrors.NewCalculatorError(calcerrors.ErrInvalidTimeRange, "指定時間範圍內的數據不足以進行窗口分析")
		c.logger.Error("時間範圍內數據不足", err, map[string]interface{}{
			"start_idx":        startIdx,
			"end_idx":          endIdx,
			"available_points": endIdx - startIdx + 1,
			"required_points":  windowSize,
			"start_range":      opts.StartRange,
			"end_range":        opts.EndRange,
		})

		return 0, 0, err
	}

	c.logger.Debug("時間範圍分析完成", map[string]interface{}{
		"start_idx":        startIdx,
		"end_idx":          endIdx,
		"available_points": endIdx - startIdx + 1,
		"scaled_start":     scaledStartRange,
		"scaled_end":       scaledEndRange,
	})

	return startIdx, endIdx, nil
}

// newOrchestrator 創建計算協調器.
func (c *MaxMeanCalculator) newOrchestrator(
	dataset *models.EMGDataset,
	windowSize, startIdx, endIdx int,
	isRanged bool,
) *orchestrator {
	channelCount := len(dataset.Data[0].Channels)

	var tracker *models.ProgressTracker

	if c.progressCallback != nil {
		tracker = models.NewProgressTracker(channelCount, c.progressCallback)
	}

	return &orchestrator{
		calc:            c,
		dataset:         dataset,
		provider:        NewEMGDatasetProvider(dataset),
		windowSize:      windowSize,
		startIdx:        startIdx,
		endIdx:          endIdx,
		channelCount:    channelCount,
		progressTracker: tracker,
		isRanged:        isRanged,
	}
}

// execute 執行計算流程.
func (o *orchestrator) execute(ctx context.Context) ([]models.MaxMeanResult, error) {
	results := make([]models.MaxMeanResult, o.channelCount)

	// 調整工作協程數
	actualWorkerCount := o.calc.workerCount

	if o.calc.backpressureController != nil {
		o.calc.backpressureController.Reset()
		actualWorkerCount = o.calc.backpressureController.GetOptimalWorkerCount()
	}

	initStatus := "初始化並行計算"
	if o.isRanged {
		initStatus = "初始化範圍並行計算"
	}

	o.calc.logger.Info("開始並行處理通道計算", map[string]interface{}{
		"worker_count":        o.calc.workerCount,
		"actual_worker_count": actualWorkerCount,
		"channel_count":       o.channelCount,
		"start_idx":           o.startIdx,
		"end_idx":             o.endIdx,
		"memory_usage":        o.calc.getMemoryUsageInfo(),
	})

	// 初始化進度報告
	if o.progressTracker != nil {
		o.progressTracker.Start(initStatus)
	}

	// 創建任務和結果通道
	jobs := make(chan calculationJob, o.channelCount)
	resultsChan := make(chan channelResult, o.channelCount)

	// 啟動工作協程池
	var wg sync.WaitGroup
	for w := 0; w < actualWorkerCount; w++ {
		wg.Add(1)

		go o.worker(ctx, jobs, resultsChan, &wg)
	}

	// 發送任務；若 ctx 取消，停止發送並關閉 jobs，worker 會自然退出
	go func() {
		defer close(jobs)

		for channelIdx := 0; channelIdx < o.channelCount; channelIdx++ {
			select {
			case <-ctx.Done():
				return
			case jobs <- calculationJob{
				channelIdx: channelIdx,
				provider:   o.provider,
				windowSize: o.windowSize,
				startIdx:   o.startIdx,
				endIdx:     o.endIdx,
			}:
			}
		}
	}()

	// 等待所有工作協程完成
	go func() {
		wg.Wait()
		close(resultsChan)
	}()

	// 收集結果
	if err := o.collectResults(ctx, results, resultsChan); err != nil {
		return nil, err
	}

	// 完成處理
	o.finalize(results)

	return results, nil
}

// worker 工作協程.
// 同時聽 ctx.Done() 與 jobs channel：ctx 取消時立即停工，避免長計算無法中斷。
func (o *orchestrator) worker(
	ctx context.Context,
	jobs <-chan calculationJob,
	results chan<- channelResult,
	wg *sync.WaitGroup,
) {
	defer wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}
			results <- o.processJob(ctx, job)
		}
	}
}

// processJob 處理單個計算任務.
func (o *orchestrator) processJob(ctx context.Context, job calculationJob) channelResult {
	// 背壓控制；ctx 取消時 WaitForCapacity 立即回傳，processJob 也帶錯誤退出。
	if o.calc.backpressureController != nil {
		if err := o.calc.backpressureController.WaitForCapacity(ctx); err != nil {
			return channelResult{channelIdx: job.channelIdx, err: err}
		}
		o.calc.backpressureController.RecordJobStart()
	}

	// 日誌記錄
	logContext := map[string]interface{}{
		"channel_index": job.channelIdx + 1,
		"memory_usage":  o.calc.getMemoryUsageInfo(),
	}
	if o.isRanged {
		logContext["start_idx"] = job.startIdx
		logContext["end_idx"] = job.endIdx
		o.calc.logger.Debug("工作協程開始處理通道範圍計算", logContext)
	} else {
		o.calc.logger.Debug("工作協程開始處理通道", logContext)
	}

	// 執行滑動窗口計算
	swResult := o.calc.slidingWindow.CalculateMaxMean(
		job.provider, job.channelIdx, job.windowSize, job.startIdx, job.endIdx,
	)

	result := models.MaxMeanResult{
		ColumnIndex: job.channelIdx + 1,
		StartTime:   job.provider.GetTime(swResult.BestStartIdx),
		EndTime:     job.provider.GetTime(swResult.BestStartIdx + job.windowSize - 1),
		MaxMean:     swResult.MaxMean,
	}

	// 記錄任務完成
	if o.calc.backpressureController != nil {
		o.calc.backpressureController.RecordJobComplete()
	}

	return channelResult{
		channelIdx: job.channelIdx,
		result:     result,
		err:        nil,
	}
}

// collectResults 收集計算結果.
// 在 worker 投遞結果與 ctx 取消之間 select，避免主執行緒在 ctx 已取消後仍等候慢 worker。
func (o *orchestrator) collectResults(
	ctx context.Context,
	results []models.MaxMeanResult,
	resultsChan <-chan channelResult,
) error {
	processedCount := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-resultsChan:
			if !ok {
				return nil
			}

			if result.err != nil {
				errMsg := "通道計算失敗"
				if o.isRanged {
					errMsg = "通道範圍計算失敗"
				}

				o.calc.logger.Error(errMsg, result.err, map[string]interface{}{
					"channel_index": result.channelIdx + 1,
					"start_idx":     o.startIdx,
					"end_idx":       o.endIdx,
				})

				return fmt.Errorf("通道 %d 計算失敗: %w", result.channelIdx+1, result.err)
			}

			results[result.channelIdx] = result.result
			processedCount++

			// 報告進度
			channelName := fmt.Sprintf("Ch%d", result.channelIdx+1)
			if len(o.dataset.Headers) > result.channelIdx+1 {
				channelName = o.dataset.Headers[result.channelIdx+1]
			}

			status := fmt.Sprintf("通道 %s 計算完成", channelName)
			if o.isRanged {
				status = fmt.Sprintf("範圍計算: 通道 %s 完成", channelName)
			}

			if o.progressTracker != nil {
				o.progressTracker.UpdateProgress(processedCount, status, result.channelIdx+1, channelName)
			}

			o.calc.logger.Debug("通道計算完成", map[string]interface{}{
				"channel_index": result.channelIdx + 1,
				"progress":      fmt.Sprintf("%d/%d", processedCount, o.channelCount),
			})
		}
	}
}

// finalize 完成計算後的處理.
func (o *orchestrator) finalize(results []models.MaxMeanResult) {
	completeStatus := "計算完成"
	if o.isRanged {
		completeStatus = "範圍計算完成"
	}

	// 報告完成狀態
	if o.progressTracker != nil {
		o.progressTracker.Complete(completeStatus)
	}

	// 記錄完成日誌
	logCtx := map[string]interface{}{
		"channel_count": len(results),
		"window_size":   o.windowSize,
	}
	if o.progressTracker != nil {
		logCtx["duration_ms"] = o.progressTracker.GetElapsedTime().Milliseconds()
	}

	if o.isRanged {
		logCtx["processed_points"] = o.endIdx - o.startIdx + 1
		o.calc.logger.Info("指定範圍內最大平均值計算完成", logCtx)
	} else {
		o.calc.logger.Info("最大平均值計算完成", logCtx)
	}

	// 記錄背壓控制統計。
	// 注意：peak_memory_mb / average_workers / throttle_events 在 391f347 Wave 4 PR8
	// 移除 backpressure monitor 後就沒有 writer 了，會永遠是 0 — cross-compare review
	// P2 抓到這個 misleading observability，這次清掉避免假象。
	if o.calc.backpressureController != nil {
		stats := o.calc.backpressureController.GetStats()

		statsLogCtx := map[string]interface{}{
			"processing_time_ms":  stats.TotalProcessingTime.Milliseconds(),
			"throughput_jobs_sec": stats.ThroughputJobsPerSec,
		}
		if o.isRanged {
			o.calc.logger.Info("範圍計算背壓控制統計", statsLogCtx)
		} else {
			o.calc.logger.Info("背壓控制統計", statsLogCtx)
		}
	}

}

// CalculateFromRawData 從原始字符串數據計算.
func (c *MaxMeanCalculator) CalculateFromRawData(
	ctx context.Context,
	records [][]string,
	windowSize int,
) ([]models.MaxMeanResult, error) {
	c.logger.Info("開始從原始數據計算最大平均值", map[string]interface{}{
		"record_count": len(records),
		"window_size":  windowSize,
	})

	dataset, err := c.dataParser.ParseRawData(records)
	if err != nil {
		c.logger.Error("原始數據解析失敗", err)
		return nil, fmt.Errorf("解析數據失敗: %w", err)
	}

	return c.Calculate(ctx, dataset, windowSize)
}

// CalculateFromRawDataWithRange 從原始字符串數據計算指定時間範圍內的最大平均值.
func (c *MaxMeanCalculator) CalculateFromRawDataWithRange(
	ctx context.Context,
	records [][]string,
	windowSize int,
	startRange, endRange float64,
) ([]models.MaxMeanResult, error) {
	c.logger.Info("開始從原始數據計算指定範圍內的最大平均值", map[string]interface{}{
		"record_count": len(records),
		"window_size":  windowSize,
		"start_range":  startRange,
		"end_range":    endRange,
	})

	dataset, err := c.dataParser.ParseRawData(records)
	if err != nil {
		c.logger.Error("原始數據解析失敗", err)
		return nil, fmt.Errorf("解析數據失敗: %w", err)
	}

	return c.CalculateWithRange(ctx, dataset, windowSize, startRange, endRange)
}
