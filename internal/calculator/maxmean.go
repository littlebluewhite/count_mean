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
//
// HasEndRange 是顯式 sentinel，取代過去「EndRange == 0 = no end」的
// overloaded 約定。EndRange == 0 在時間軸從 0 開始的情境是合法上界（例如
// CalculateWithRange(start=-1, end=0)），舊版會被靜默改寫為「到資料末端」。
// 新版只有 HasEndRange == false 才略過 end 比對。
//
// HasStartRange 對 StartRange 提供同樣的「顯式 sentinel」契約。
// 舊版用 `StartRange != 0` 作為 implicit「是否提供起始」判斷,但 StartRange == 0
// 在時間軸從負值開始的場景 (例如校準前的 -1s ~ 5s 區間) 是合法下界 —
// `StartRange != 0` 會把它誤判為「沒設」並 fall back 到 0,等同把使用者指定的
// 0s 上推到「不限制下界」,結果區間從整個資料起點開始,而非預期的 [0, end]。
//
// HasStartRange == false 視為「不限制下界」,從資料最早 sample 開始。
// HasStartRange == true 即使 StartRange == 0 也視為顯式下界。
//
// 既有 CalculateWithRange 同時設 StartRange + EndRange,此 API 自動 set 兩個
// Has* 為 true,既有 caller 行為不變。
type CalculationOptions struct {
	StartRange    float64 // 起始時間範圍
	EndRange      float64 // 結束時間範圍（語意由 HasEndRange 決定）
	HasStartRange bool    // true：StartRange 為顯式下界；false：從資料起點
	HasEndRange   bool    // true：EndRange 為顯式上界；false：到資料末端
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
// 此 API 同時設定 StartRange / EndRange，故 HasStartRange = HasEndRange = true。
func (c *MaxMeanCalculator) CalculateWithRange(
	ctx context.Context,
	dataset *models.EMGDataset,
	windowSize int,
	startRange, endRange float64,
) ([]models.MaxMeanResult, error) {
	return c.calculateWithOptions(ctx, dataset, windowSize, CalculationOptions{
		StartRange:    startRange,
		EndRange:      endRange,
		HasStartRange: true,
		HasEndRange:   true,
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

	// / isRanged 完全由顯式 Has* 旗標決定,脫離舊 sentinel 約定。
	// HasStartRange / HasEndRange 任一為 true 即視為 ranged。
	// 過去保留的 `StartRange != 0` OR 子句違反 契約 (HasStartRange 必須是
	// 唯一 source of truth) — 任何 caller 帶非零 StartRange 但忘了設 HasStartRange
	// 都會被誤判為 ranged。caller 已全部遷移到顯式旗標,移除 OR 子句鎖死契約。
	isRanged := opts.HasStartRange || opts.HasEndRange

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

	// 0-channel input fail-fast。原本 windowSize check 過後直接進 Calculate
	// 迴圈，channel count 0 會回空 result map（看起來「正常」）但 user 期望會出錯。
	if len(dataset.Data[0].Channels) == 0 {
		err := calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "數據集沒有任何通道")
		c.logger.Error("通道數量驗證失敗", err, map[string]interface{}{
			"data_length": len(dataset.Data),
		})

		return err
	}

	// ragged row fail-fast。EMGDatasetProvider.GetValue 過去對
	// channelIdx >= len(p.dataset.Data[dataIdx].Channels) 靜默回 0,
	// 把 ragged dataset (任一 row channel 數 != Data[0]) 偽裝成「全部對齊到第一列
	// 通道數的合法資料」並繼續算 sliding window — caller 拿到的 mean 含 phantom
	// 0 樣本,典型 silent miscompute。
	//
	// 對齊 normalizer 的 ErrChannelMismatch 模型,以 row_index /
	// row_channels / expected_channels context 回 fail-fast error;Provider
	// 構造階段 (NewEMGDatasetProvider) 也 store expectedChannels 作為 defense-in-depth,
	// 若 ragged 仍透過直接 struct literal 路徑 (例:跳過 MaxMean.validateInput
	// 的測試 jigs) 進入,GetValue 會 panic with context 而非靜默補 0。
	expectedChannels := len(dataset.Data[0].Channels)
	for rowIdx, row := range dataset.Data {
		if len(row.Channels) != expectedChannels {
			err := calcerrors.NewCalculatorErrorWithContext(
				calcerrors.ErrChannelMismatch,
				fmt.Sprintf(
					"第 %d 列通道數 %d 與首列通道數 %d 不一致",
					rowIdx+1, len(row.Channels), expectedChannels,
				),
				map[string]interface{}{
					"row_index":         rowIdx + 1,
					"row_channels":      len(row.Channels),
					"expected_channels": expectedChannels,
				},
			)
			c.logger.Error("資料列通道數不一致", err, map[string]interface{}{
				"row_index":         rowIdx + 1,
				"row_channels":      len(row.Channels),
				"expected_channels": expectedChannels,
			})

			return err
		}
	}

	// fail-fast on NaN / ±Inf channel samples. Sliding-window 增量加減後,
	// NaN 會把 windowSum 永久污染成 NaN,但因 `NaN > maxMean` 恆為 false,
	// 第一個有效 window 的 mean 會被當成「最大值」回傳 — caller 看到合法 finite
	// 數字,完全不知道資料有問題。-Inf 走同樣路徑 (一旦 windowSum = -Inf,
	// 後續 sum + finite 仍 = -Inf,所有 mean 被 maxMean 蓋掉)。+Inf 雖然會
	// propagate 成 +Inf result,但 caller 也應該明確得到錯誤而非靜默 +Inf。
	// 對齊 normalizer 的 ErrNaNReference / ErrInfReference fail-fast 模型。
	if err := c.validateChannelValues(dataset); err != nil {
		return err
	}

	return nil
}

// validateChannelValues scans the dataset for NaN / ±Inf channel samples and
// returns a sentinel error on the first occurrence. Linear O(N×channels) scan
// up-front is acceptable: sliding-window already touches every sample so the
// added cost is one extra pass; the wrong-but-finite result silently produced
// today is a real correctness bug worth paying for.
func (c *MaxMeanCalculator) validateChannelValues(dataset *models.EMGDataset) error {
	for rowIdx, row := range dataset.Data {
		for chIdx, v := range row.Channels {
			if math.IsNaN(v) {
				err := calcerrors.NewCalculatorErrorWithContext(
					calcerrors.ErrNaNInChannel,
					fmt.Sprintf("通道 %d 在第 %d 列含 NaN,無法計算最大平均值", chIdx+1, rowIdx+1),
					map[string]interface{}{
						"row_index":     rowIdx,
						"channel_index": chIdx + 1,
					},
				)
				c.logger.Error("通道資料含 NaN", err, map[string]interface{}{
					"row_index":     rowIdx,
					"channel_index": chIdx + 1,
				})

				return err
			}

			if math.IsInf(v, 0) {
				err := calcerrors.NewCalculatorErrorWithContext(
					calcerrors.ErrInfInChannel,
					fmt.Sprintf("通道 %d 在第 %d 列含 Inf,無法計算最大平均值", chIdx+1, rowIdx+1),
					map[string]interface{}{
						"row_index":     rowIdx,
						"channel_index": chIdx + 1,
						"sign":          infSign(v),
					},
				)
				c.logger.Error("通道資料含 Inf", err, map[string]interface{}{
					"row_index":     rowIdx,
					"channel_index": chIdx + 1,
					"sign":          infSign(v),
				})

				return err
			}
		}
	}

	return nil
}

// infSign returns "+Inf" or "-Inf" for diagnostics (no-op for finite values).
func infSign(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}

	if math.IsInf(v, -1) {
		return "-Inf"
	}

	return ""
}

// saturateMicroseconds 將 scaledTime (已 Pow10 處理過的秒值) 乘上 MicrosecondsPerSecond
// 並 clamp 到 int64 範圍。
//
// 過去用 `int64(math.Round(scaled * MicrosecondsPerSecond))` 直接 cast,
// 當 scaled > math.MaxInt64 / MicrosecondsPerSecond (約 9.22e12) 時 cast 行為
// 在 Go 標準是 implementation-defined — 多數平台會 wrap-around 成負值,
// 導致 startRangeUs / endRangeUs 比對全錯且不會回任何 error。
//
// 雖然 EMG 時間域上限 (秒級) + 合理 scalingFactor (典型 0~10) 組合下永遠
// reachable 不到溢位閾值 (這也是 FP1 的論據),但加 saturation 是 0 成本的
// 防禦:overflow 改為 clamp 到 ±MaxInt64,行為可預期且不影響合法 input。
//
// 邊界 case:
//   - NaN scaled 走 math.Round 仍為 NaN → cast 為 0 (Go spec),回 0。
//     NaN scalingFactor / StartRange 應該在更上游被攔下,這裡 0 是保守選擇。
//   - +Inf → 回 math.MaxInt64
//   - -Inf → 回 math.MinInt64
func saturateMicroseconds(scaled float64) int64 {
	scaledUs := scaled * MicrosecondsPerSecond

	if math.IsNaN(scaledUs) {
		return 0
	}

	// 用 float64 直接比 MaxInt64 會因 precision 漂移 (1<<63 不可被 float64 精確表達)
	// 過早觸發 saturation;改用 math.MaxInt64 的 float64 representation
	// (= 9223372036854775808.0,實際是 1<<63) 與 math.MinInt64 對稱比較。
	const maxFloat = float64(math.MaxInt64) // 9.223372036854776e+18
	const minFloat = float64(math.MinInt64) // -9.223372036854776e+18

	if scaledUs >= maxFloat {
		return math.MaxInt64
	}

	if scaledUs <= minFloat {
		return math.MinInt64
	}

	return int64(math.Round(scaledUs))
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

	// 用 saturateMicroseconds 把 scaled-time → int64-微秒的乘法 clamp 進
	// [MinInt64, MaxInt64],避免極端 scalingFactor / StartRange 組合下 int64
	// overflow 產生 wrap-around 比較。實務 EMG 時間域 (秒級) 不會 hit,但加 sat
	// 保險,fail-fast 的成本是 0 而 wrap-around 的 silent miscompute 才致命。
	startRangeUs := saturateMicroseconds(scaledStartRange)
	endRangeUs := saturateMicroseconds(scaledEndRange)

	startIdx = -1
	endIdx = -1

	// HasStartRange == false 視為「從資料起點」,startIdx 直接設為 0。
	// 過去仰賴 `StartRange != 0` 隱式判斷,StartRange=0 被誤判為「未指定起點」,
	// 但對 time-axis 從負值開始的資料 (校準資料),StartRange=0 應該是合法下界。
	startBypass := !opts.HasStartRange && opts.StartRange == 0
	if startBypass {
		startIdx = 0
	}

	// HasEndRange == false 才略過 end 比對（資料末端為止）。
	// 過去用 EndRange == 0 sentinel，遮蔽了「end=0 為合法上界」的場景。
	//nolint:nestif // nested blocks necessary for different end range handling
	if !opts.HasEndRange {
		endIdx = len(dataset.Data) - 1

		if !startBypass {
			for i, data := range dataset.Data {
				// Time path 與 startRange / endRange path 同走 saturateMicroseconds,
				// 對稱 clamp。data.Time = ±Inf / NaN / 過大值的裸 int64 cast 在 Go spec 中
				// 是 implementation-defined,雖然 Go 1.21+ runtime 行為趨於 saturate,
				// 改走 helper 鎖死契約並讓行為跨平台/跨版本一致。
				dataTimeUs := saturateMicroseconds(data.Time)
				if dataTimeUs >= startRangeUs {
					startIdx = i
					break
				}
			}
		}
	} else {
		for i, data := range dataset.Data {
			// 同 470 區塊,Time path 走 saturate symmetric。
			dataTimeUs := saturateMicroseconds(data.Time)
			if !startBypass && startIdx == -1 && dataTimeUs >= startRangeUs {
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
	// 顯式檢查 pre-cancelled ctx，避免依賴 worker pool 與 ctx.Done()
	// 之間的 race-y select 來中斷。已取消的 ctx 直接以 ctx.Err() 回傳，
	// worker 從未啟動 → backpressure 計數維持 0，行為對使用者契約明確。
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
//
// results <- ... 也包進 select(ctx.Done) — ctx 取消後 collectResults
// 已 return (見 collectResults 的 `<-ctx.Done()` case),不再 drain resultsChan。
// 雖然 resultsChan 容量為 channelCount (理論上 send 不會 block),但若有 worker
// 在 cancel 時剛好遇到 buffer 滿邊界 (例: 多 worker 並發 send 超過實際 channel
// 數,雖然當前架構不會,但防禦性編程),裸 send 會 deadlock 並 leak goroutine。
// 改成 select 形式,確保 worker 退出路徑與 ctx 完全綁定。
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

			res := o.processJob(ctx, job)
			select {
			case <-ctx.Done():
				// ctx 已取消 — 不要 blocking-send 到一個沒人 drain 的 channel,
				// 直接退出讓 wg.Wait 完成 → close(resultsChan) → 任何殘留 reader 解放。
				return
			case results <- res:
			}
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
		// 用 defer 保護 RecordJobComplete，確保 panic / early return
		// 路徑下 activeJobs 對稱遞減。過去裸呼叫位於函式底部，slidingWindow
		// panic 會繞過它，導致 activeJobs 計數永久偏高，背壓判斷失準。
		defer o.calc.backpressureController.RecordJobComplete()
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

	// 執行滑動窗口計算；ctx 傳入後 mid-flight cancel 也能中斷。
	swResult, err := o.calc.slidingWindow.CalculateMaxMean(
		ctx, job.provider, job.channelIdx, job.windowSize, job.startIdx, job.endIdx,
	)
	if err != nil {
		return channelResult{channelIdx: job.channelIdx, err: err}
	}

	result := models.MaxMeanResult{
		ColumnIndex: job.channelIdx + 1,
		StartTime:   job.provider.GetTime(swResult.BestStartIdx),
		EndTime:     job.provider.GetTime(swResult.BestStartIdx + job.windowSize - 1),
		MaxMean:     swResult.MaxMean,
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
