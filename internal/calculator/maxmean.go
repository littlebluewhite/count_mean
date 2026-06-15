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
	scalingFactor    int
	logger           *logging.Logger
	workerCount      int
	progressCallback models.ProgressCallback
	dataParser       *parsers.DataParser
	slidingWindow    *SlidingWindowCalculator
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

	logger := logging.GetLogger("max_mean_calculator")

	return &MaxMeanCalculator{
		scalingFactor: scalingFactor,
		logger:        logger,
		workerCount:   workerCount,
		dataParser:    parsers.NewDataParserWithLogger(scalingFactor, logger),
		slidingWindow: NewSlidingWindowCalculator(),
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

// memoryUsageInfo 獲取記憶體使用信息(供 logging 使用)。
// 內聯自原本透過 BackpressureController 的 GetMemoryUsageInfo wrapper;collapse 後
// 直接 read runtime.MemStats 並補上 usage_ratio / is_throttled 兩個欄位以維持
// 原本 logging payload 形狀。
func (c *MaxMeanCalculator) memoryUsageInfo() map[string]any {
	var memStats runtime.MemStats

	runtime.ReadMemStats(&memStats)

	ratio := memoryUsageRatio(&memStats)

	return map[string]any{
		"alloc_mb":       memStats.Alloc / 1024 / 1024,
		"total_alloc_mb": memStats.TotalAlloc / 1024 / 1024,
		"sys_mb":         memStats.Sys / 1024 / 1024,
		"num_gc":         memStats.NumGC,
		"usage_ratio":    ratio,
		"is_throttled":   ratio >= defaultMemoryThreshold,
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

		c.logger.Error("計算參數驗證失敗", err, map[string]any{
			"dataset_nil": dataset == nil,
			"data_length": dataLength,
		})

		return err
	}

	if windowSize < 1 {
		err := calcerrors.NewCalculatorError(calcerrors.ErrInvalidWindowSize, "窗口大小必須大於 0")
		c.logger.Error("窗口大小驗證失敗", err, map[string]any{
			"window_size": windowSize,
		})

		return err
	}

	if len(dataset.Data) < windowSize {
		err := calcerrors.NewCalculatorError(calcerrors.ErrWindowTooLarge, "數據集無效或窗口大小過大")
		c.logger.Error("窗口大小驗證失敗", err, map[string]any{
			"data_length": len(dataset.Data),
			"window_size": windowSize,
		})

		return err
	}

	// 0-channel input fail-fast。原本 windowSize check 過後直接進 Calculate
	// 迴圈，channel count 0 會回空 result map（看起來「正常」）但 user 期望會出錯。
	if len(dataset.Data[0].Channels) == 0 {
		err := calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "數據集沒有任何通道")
		c.logger.Error("通道數量驗證失敗", err, map[string]any{
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
				map[string]any{
					"row_index":         rowIdx + 1,
					"row_channels":      len(row.Channels),
					"expected_channels": expectedChannels,
				},
			)
			c.logger.Error("資料列通道數不一致", err, map[string]any{
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
					map[string]any{
						"row_index":     rowIdx,
						"channel_index": chIdx + 1,
					},
				)
				c.logger.Error("通道資料含 NaN", err, map[string]any{
					"row_index":     rowIdx,
					"channel_index": chIdx + 1,
				})

				return err
			}

			if math.IsInf(v, 0) {
				err := calcerrors.NewCalculatorErrorWithContext(
					calcerrors.ErrInfInChannel,
					fmt.Sprintf("通道 %d 在第 %d 列含 Inf,無法計算最大平均值", chIdx+1, rowIdx+1),
					map[string]any{
						"row_index":     rowIdx,
						"channel_index": chIdx + 1,
						"sign":          infSign(v),
					},
				)
				c.logger.Error("通道資料含 Inf", err, map[string]any{
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

// normalizeTime 把 scaled-time 域 (已 Pow10 處理過的時間值) 的非有限值正規化,
// 讓 resolveDataRange 可以直接在 float64 scaled 域比較,不必再乘 1e6 轉 int64。
//
// 過去用 saturateMicroseconds 先乘 MicrosecondsPerSecond 再 round 成 int64,
// 額外的 ×1e6 把 int64 溢位門檻從 scaled 域的 ~9.22e18 拉低到 ~9.22e12;在預設
// scalingFactor=10 下,任何 >~922 秒的 time 一乘 1e6 就全部 clamp 成 MaxInt64,
// 使同一 range 內 >922 秒的 row 互相無法區分,startIdx / endIdx 邊界靜默算錯。
// 改在 scaled 域直接 float64 比較後,這層 ×1e6 與 int64 round 完全移除,既無
// 溢位也無次單位取整誤差。
//
// 非有限值語意 (保留 saturateMicroseconds 舊行為的等效結果):
//   - NaN → 0。NaN scalingFactor / range / Time 應在更上游攔下,這裡 0 是保守選擇,
//     等同舊版 NaN→0。
//   - +Inf / -Inf → 原值穿透。float64 直比下 +Inf 必 >= 任何 finite 下界、必不
//     <= 任何 finite 上界 (等同舊 MaxInt64);-Inf 對稱 (等同舊 MinInt64),
//     無須再 clamp 成 int64 sentinel。
func normalizeTime(scaled float64) float64 {
	if math.IsNaN(scaled) {
		return 0
	}

	return scaled
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
	logCtx := map[string]any{
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

	// 轉換時間範圍為縮放後的值。dataset.Time 經 Str2Number 已乘 10^scalingFactor,
	// 與下面 scaledStartRange / scaledEndRange 同屬 scaled 域,直接 float64 比較即可。
	// normalizeTime 只把 NaN 正規化為 0 (保留舊行為);±Inf 在 float64 比較下語意
	// 天然正確,無須轉 int64-微秒,故移除過去多餘的 ×1e6 與 int64 round (該乘法
	// 把溢位門檻拉到 ~922 秒,使 scalingFactor=10 下 >922 秒的 row 被靜默 clamp)。
	//
	// 邊界為「精確 float64 比較」,刻意不保留舊 ×1e6 的次微秒取整容差 (codex R2 dismiss):
	// 那容差是 int64-微秒機制的附帶產物、非刻意 spec,而 ×1e6 正是溢位 bug 根源。
	// 資料時間戳 (CSV→Str2Number→×Pow10) 與使用者邊界 (×Pow10) 共用縮放路徑,相等
	// 字串值產生 bit-identical float 必被納入;僅「真正相異的次微秒值」被排除 (本應排除)。
	scaledStartRange := normalizeTime(opts.StartRange * math.Pow10(c.scalingFactor))
	scaledEndRange := normalizeTime(opts.EndRange * math.Pow10(c.scalingFactor))

	startIdx = -1
	endIdx = -1

	// HasStartRange == false 視為「從資料起點」,startIdx 直接設為 0。
	// HasStartRange 是唯一 source of truth — 即使 caller 殘留非零 StartRange,
	// 只要未宣告 HasStartRange 就 bypass 下界 (對齊 struct doc;舊 `&& StartRange==0`
	// 子句會讓未宣告的 StartRange 漏進過濾,是 latent bug)。
	startBypass := !opts.HasStartRange
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
				// Time path 與 startRange / endRange path 同走 normalizeTime,
				// 在 scaled 域 float64 直比。data.Time = NaN 正規化為 0,±Inf 穿透
				// (+Inf 必 >= finite 下界、-Inf 必不 >=),行為跨平台一致。
				dataTime := normalizeTime(data.Time)
				if dataTime >= scaledStartRange {
					startIdx = i
					break
				}
			}
		}
	} else {
		for i, data := range dataset.Data {
			// 同上區塊,Time path 走 normalizeTime symmetric。
			dataTime := normalizeTime(data.Time)
			if !startBypass && startIdx == -1 && dataTime >= scaledStartRange {
				startIdx = i
			}

			if dataTime <= scaledEndRange {
				endIdx = i
			}
		}
	}

	if startIdx == -1 || endIdx == -1 || endIdx-startIdx+1 < windowSize {
		err := calcerrors.NewCalculatorError(calcerrors.ErrInvalidTimeRange, "指定時間範圍內的數據不足以進行窗口分析")
		c.logger.Error("時間範圍內數據不足", err, map[string]any{
			"start_idx":        startIdx,
			"end_idx":          endIdx,
			"available_points": endIdx - startIdx + 1,
			"required_points":  windowSize,
			"start_range":      opts.StartRange,
			"end_range":        opts.EndRange,
		})

		return 0, 0, err
	}

	c.logger.Debug("時間範圍分析完成", map[string]any{
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

	actualWorkerCount := o.calc.workerCount

	initStatus := "初始化並行計算"
	if o.isRanged {
		initStatus = "初始化範圍並行計算"
	}

	o.calc.logger.Info("開始並行處理通道計算", map[string]any{
		"worker_count":        o.calc.workerCount,
		"actual_worker_count": actualWorkerCount,
		"channel_count":       o.channelCount,
		"start_idx":           o.startIdx,
		"end_idx":             o.endIdx,
		"memory_usage":        o.calc.memoryUsageInfo(),
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
	// Admission gate:ctx 取消時 waitForMemoryCapacity 立即回傳 ctx.Err(),
	// processJob 也帶錯誤退出。collapse 後不再追蹤 activeJobs counter
	// (Wave 6 已移除 monitor + 對外 zero reader),純 memory ratio
	// 守 ThrottleThreshold 即可。
	if err := o.calc.waitForMemoryCapacity(ctx); err != nil {
		return channelResult{channelIdx: job.channelIdx, err: err}
	}

	// 日誌記錄
	logContext := map[string]any{
		"channel_index": job.channelIdx + 1,
		"memory_usage":  o.calc.memoryUsageInfo(),
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

				o.calc.logger.Error(errMsg, result.err, map[string]any{
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

			o.calc.logger.Debug("通道計算完成", map[string]any{
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
	logCtx := map[string]any{
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
}

// CalculateFromRawData 從原始字符串數據計算.
func (c *MaxMeanCalculator) CalculateFromRawData(
	ctx context.Context,
	records [][]string,
	windowSize int,
) ([]models.MaxMeanResult, error) {
	c.logger.Info("開始從原始數據計算最大平均值", map[string]any{
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
	c.logger.Info("開始從原始數據計算指定範圍內的最大平均值", map[string]any{
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
