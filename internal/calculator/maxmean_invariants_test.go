package calculator

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/models"
)

// TestCalculate_PreCancelledContext_DoesNotStartWorkers exercises the explicit
// ctx.Err() guard added at the top of execute. The previous race-y select had
// 1/N chance of letting one worker job slip through on a fast machine before
// ctx.Done() was observed; this test asserts deterministic short-circuit:
// 收到 already-cancelled ctx 時，所有 backpressure 計數應維持 0
// （等同 worker pool 從未啟動）。
func TestCalculate_PreCancelledContext_DoesNotStartWorkers(t *testing.T) {
	calc := NewMaxMeanCalculator(10)
	dataset := buildLargeDataset(10000, 8)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := calc.Calculate(ctx, dataset, 100)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled),
		"expected context.Canceled, got %v", err)

	// 顯式契約：pre-cancelled ctx 進入 execute 時就回傳，
	// worker pool 從未啟動，所以 backpressure 從未 RecordJobStart。
	assert.Equal(t, 0, calc.backpressureController.ActiveJobs(),
		"no jobs should have started under pre-cancelled context")
}

// TestProcessJob_PanicResetsActiveJobs verifies that even when the sliding
// window calculation panics mid-flight, the deferred RecordJobComplete still
// fires and activeJobs returns to zero.
//
// 注入 panicking provider 的手法：EMGDatasetProvider.GetValue 在 dataset == nil
// 時會 nil-pointer panic（len(p.dataset.Data) 解 nil pointer）。利用這點驅動
// SlidingWindowCalculator 在 RecordJobStart 之後 panic，期望 defer 守住
// activeJobs 計數對稱性。
func TestProcessJob_PanicResetsActiveJobs(t *testing.T) {
	calc := NewMaxMeanCalculator(10)
	calc.backpressureController.Reset()

	// 真實 dataset 用來建 orchestrator（execute 走不到，但 newOrchestrator 需要
	// dataset.Data[0].Channels 取 channelCount）。
	realDataset := buildLargeDataset(10, 1)

	// 直接組 orchestrator，繞過 Calculate 的合法性驗證；
	// 此測試專注 processJob 的 panic-safe 計數對稱性。
	orch := calc.newOrchestrator(realDataset, 5, 0, 9, false)

	// 注入會 panic 的 provider（dataset == nil → GetValue 解 nil dataset.Data）。
	panickyJob := calculationJob{
		channelIdx: 0,
		provider:   &EMGDatasetProvider{dataset: nil},
		windowSize: 5,
		startIdx:   0,
		endIdx:     9,
	}

	require.Panics(t, func() {
		_ = orch.processJob(context.Background(), panickyJob)
	}, "panicky provider should panic processJob mid-flight")

	// 關鍵斷言：即使 panic，defer RecordJobComplete 仍須執行，
	// activeJobs 應回到 0；若回 0 之前的實作（非 defer）則此處會卡 1。
	assert.Equal(t, 0, calc.backpressureController.ActiveJobs(),
		"deferred RecordJobComplete must restore activeJobs to zero after panic")
}

// TestCalculate_HasStartRange_ZeroIsExplicit 守護 
// StartRange=0 + HasStartRange=true 必須視為「顯式下界 = 0」而非「未設」。
// 過去用 `StartRange != 0` sentinel,StartRange=0 會被誤判,結果區間從資料起點開始,
// 對 time-axis 從負值開始的資料 (例:預備動作 t=-1s ~ t=2s) 會把 -1 ~ 0 區間包進來。
//
// 構造策略:在 t<0 處放最大值,t>=0 處放小值;HasStartRange=true 不應該選到 t<0
// 的 max,但 HasStartRange=false (從資料起點) 應該選到。
func TestCalculate_HasStartRange_ZeroIsExplicit(t *testing.T) {
	calc := NewMaxMeanCalculator(0) // scalingFactor=0,時間軸直接對應秒

	// 構造 time 從 -0.5 到 0.5 (11 點,每 0.1s 一點) 的 dataset,
	// channel 0 的最大值在 t<0 (idx 0~4),t>=0 是低值。
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data:    make([]models.EMGData, 11),
	}
	for i := range dataset.Data {
		dataset.Data[i].Time = float64(i)*0.1 - 0.5
		// 前 5 點放大值 100,後 6 點放小值 1
		if i < 5 {
			dataset.Data[i].Channels = []float64{100.0}
		} else {
			dataset.Data[i].Channels = []float64{1.0}
		}
	}

	// HasStartRange=true + StartRange=0 → 應從 t>=0 開始,只看到小值,MaxMean = 1
	results, err := calc.calculateWithOptions(context.Background(), dataset, 3, CalculationOptions{
		StartRange:    0.0,
		EndRange:      0.5,
		HasStartRange: true,
		HasEndRange:   true,
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 1.0, results[0].MaxMean, 1e-9,
		"HasStartRange=true,StartRange=0 應從 t=0 起算,MaxMean=1 (低值區);實際=%v", results[0].MaxMean)

	// 對比:不設 HasStartRange + StartRange=0 → 從資料起點 (t=-0.5) 開始,
	// sliding window 會涵蓋 t<0 的高值區,MaxMean 應為 100。
	resultsBypass, err := calc.calculateWithOptions(context.Background(), dataset, 3, CalculationOptions{
		EndRange:    0.5,
		HasEndRange: true,
	})
	require.NoError(t, err)
	require.Len(t, resultsBypass, 1)
	assert.InDelta(t, 100.0, resultsBypass[0].MaxMean, 1e-9,
		"HasStartRange=false 應從資料起點開始,MaxMean=100 (高值區);實際=%v", resultsBypass[0].MaxMean)
}

// TestProcessJob_PreCancelledCtx_NoCountLeak 驗證 WaitForCapacity 因 ctx 取消
// 提早回傳時，因尚未 RecordJobStart，activeJobs 應始終為 0。
// 這條 invariant 保護「RecordJobStart 必須與 RecordJobComplete 配對」的契約。
func TestProcessJob_PreCancelledCtx_NoCountLeak(t *testing.T) {
	calc := NewMaxMeanCalculator(10)
	calc.backpressureController.Reset()

	dataset := buildLargeDataset(100, 2)
	orch := calc.newOrchestrator(dataset, 5, 0, 99, false)

	// WaitForCapacity 在記憶體壓力不大時會立即回傳 nil，所以這個測試其實
	// 走的是「正常完成 → defer 後 activeJobs 歸零」路徑，搭配上面 panic
	// 測試構成完整不變式（symmetric RecordJobStart/Complete）。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := calculationJob{
		channelIdx: 0,
		provider:   NewEMGDatasetProvider(dataset),
		windowSize: 5,
		startIdx:   0,
		endIdx:     99,
	}

	result := orch.processJob(ctx, job)
	require.NoError(t, result.err)

	assert.Equal(t, 0, calc.backpressureController.ActiveJobs(),
		"after normal processJob, activeJobs must be zero")
}

// TestMaxMean_NaNInf_Invariants 鎖定 的 fail-fast 契約:含 NaN / ±Inf 的
// channel 樣本必須在 validateInput 階段就被攔下並回傳 sentinel error,絕不可
// 進到 sliding window 計算。
//
// 為什麼非 fail-fast 不可:
//   - AllNaN 路徑下,sliding_window 初始 windowSum 即為 NaN,最終回 NaN —
//     看似 propagate,但 caller 拿到一個合法 float64 值,難以與「真實 0」區分。
//   - OneMiddleNaN 是最危險的 silent-fail:增量加減的特性讓 windowSum 一旦
//     被 NaN 污染就永久 stuck 在 NaN,而 `NaN > maxMean` 恆為 false,
//     初始有效窗的 mean (例: 1.0) 被當成「最大值」回傳。caller 拿到合法
//     finite 數字,完全不知道 80% 的窗口其實是 NaN —— 這是真的算錯,不是
//     propagate。
//   - PlusInf propagate 成 +Inf 看似 OK,但對下游 (CSV writer / chart) 仍是
//     未定義行為,且 caller 沒理由相信 +Inf 是有意義的「最大平均值」。
//   - MinusInf 走 OneMiddleNaN 同樣的污染 stuck 路徑 (windowSum + finite = -Inf,
//     `-Inf > 1.0` false),初始窗的 1.0 被當成最大值 —— 又是 silent miscompute。
//
// 對齊 normalizer (P0-3+4) 對 NaN/Inf 參考值的 ErrNaNReference / ErrInfReference
// fail-fast 處理,使「資料品質問題」與「計算配置問題」都以 errors.Is 可辨識的
// sentinel 表達,而不是讓 caller 在 result 裡靠 math.IsNaN 反推。
//
//nolint:funlen // 4 subtest covering all 4 corner cases; splitting hides intent.
func TestMaxMean_NaNInf_Invariants(t *testing.T) {
	const windowSize = 3
	const rows = 10

	// makeDataset 建構一個 finite baseline,呼叫者可選擇性地注入 NaN/Inf。
	makeDataset := func(injector func(data []models.EMGData)) *models.EMGDataset {
		data := make([]models.EMGData, rows)
		for i := range data {
			data[i] = models.EMGData{
				Time:     float64(i) * 0.001,
				Channels: []float64{1.0},
			}
		}
		if injector != nil {
			injector(data)
		}
		return &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    data,
		}
	}

	t.Run("AllNaN", func(t *testing.T) {
		calc := NewMaxMeanCalculator(0)
		dataset := makeDataset(func(data []models.EMGData) {
			for i := range data {
				data[i].Channels[0] = math.NaN()
			}
		})

		results, err := calc.Calculate(context.Background(), dataset, windowSize)
		require.Error(t, err, "all-NaN input must not silently compute a maxmean")
		require.True(t, errors.Is(err, calcerrors.ErrNaNInChannel),
			"expected ErrNaNInChannel sentinel via errors.Is, got %v", err)
		assert.Nil(t, results, "fail-fast: no partial result on NaN input")
	})

	t.Run("MiddleNaN", func(t *testing.T) {
		// 這是最會 silent-miscompute 的案例:第一個 window [idx 0..2] 全部 = 1.0
		// (mean = 1.0),NaN 落在 idx 5;舊行為下 windowSum 被 NaN 永久污染,
		// 但 `NaN > 1.0` 為 false,maxMean 鎖死在 1.0 —— caller 完全看不出資料壞。
		calc := NewMaxMeanCalculator(0)
		dataset := makeDataset(func(data []models.EMGData) {
			data[5].Channels[0] = math.NaN()
		})

		results, err := calc.Calculate(context.Background(), dataset, windowSize)
		require.Error(t, err, "mid-stream NaN must not be masked by initial-window mean")
		require.True(t, errors.Is(err, calcerrors.ErrNaNInChannel),
			"expected ErrNaNInChannel sentinel via errors.Is, got %v", err)
		assert.Nil(t, results, "fail-fast: no partial result on mid-stream NaN")
	})

	t.Run("PlusInf", func(t *testing.T) {
		calc := NewMaxMeanCalculator(0)
		dataset := makeDataset(func(data []models.EMGData) {
			data[3].Channels[0] = math.Inf(1)
		})

		results, err := calc.Calculate(context.Background(), dataset, windowSize)
		require.Error(t, err, "+Inf input must not propagate silently into result")
		require.True(t, errors.Is(err, calcerrors.ErrInfInChannel),
			"expected ErrInfInChannel sentinel via errors.Is, got %v", err)
		assert.Nil(t, results, "fail-fast: no partial result on +Inf input")
	})

	t.Run("MinusInf", func(t *testing.T) {
		// 對稱於 MiddleNaN: -Inf 污染 windowSum 後 `-Inf > 1.0` 為 false,
		// 舊行為下也是初始窗的 1.0 被當成「最大值」回傳,典型 silent miscompute。
		calc := NewMaxMeanCalculator(0)
		dataset := makeDataset(func(data []models.EMGData) {
			data[3].Channels[0] = math.Inf(-1)
		})

		results, err := calc.Calculate(context.Background(), dataset, windowSize)
		require.Error(t, err, "-Inf input must not be masked by initial-window mean")
		require.True(t, errors.Is(err, calcerrors.ErrInfInChannel),
			"expected ErrInfInChannel sentinel via errors.Is, got %v", err)
		assert.Nil(t, results, "fail-fast: no partial result on -Inf input")
	})
}

// TestMaxMean_TimeRangeOverflow_Saturated 釘住 scaledStartRange *
// MicrosecondsPerSecond 在極端 input 下會 int64 overflow,過去用裸 int64 cast,
// 多數平台會 wrap-around 成負值,startRangeUs / endRangeUs 比對全錯且不會回任何
// error — silent miscompute。
//
// 修法後 saturateMicroseconds 對 ±Inf / 超出 int64 範圍的乘積 clamp 到
// ±math.MaxInt64,行為可預期。配合下游 resolveDataRange 對 startIdx == -1 /
// endIdx == -1 / 區間不足 windowSize 的 ErrInvalidTimeRange,saturated 路徑
// 自然 fail-fast。
//
// 三條保護:
//   - SaturatedStartIsRejected:極大 StartRange 使 startRangeUs == MaxInt64,
//     任何資料 row 的 dataTimeUs <= MaxInt64,startIdx 永遠對應「>= MaxInt64」的
//     row 卻沒有 — startIdx = -1 → ErrInvalidTimeRange。
//   - SaturatedEndIsAccepted:極大 EndRange 使 endRangeUs == MaxInt64,
//     所有 row.Time <= MaxInt64 都通過,endIdx == 最後一個 row。startRange 在
//     合法範圍內仍應算出正確 MaxMean。
//   - SaturationFunctionContract:直接呼 saturateMicroseconds 驗 ±Inf / NaN /
//     超大值的 clamp 行為。
func TestMaxMean_TimeRangeOverflow_Saturated(t *testing.T) {
	t.Run("SaturatedStartIsRejected", func(t *testing.T) {
		// scalingFactor = 0,StartRange = +Inf → scaled = +Inf,
		// saturateMicroseconds 回 MaxInt64,所有 row.Time finite 都 < MaxInt64 →
		// 找不到合法 startIdx → ErrInvalidTimeRange。
		calc := NewMaxMeanCalculator(0)
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    make([]models.EMGData, 10),
		}
		for i := range dataset.Data {
			dataset.Data[i] = models.EMGData{
				Time:     float64(i) * 0.1,
				Channels: []float64{float64(i)},
			}
		}

		_, err := calc.CalculateWithRange(context.Background(), dataset, 3, math.Inf(1), math.Inf(1))
		require.Error(t, err, "+Inf start range should not silently compute a result")
		require.True(t, errors.Is(err, calcerrors.ErrInvalidTimeRange),
			"saturated range with no matching rows should return ErrInvalidTimeRange, got %v", err)
	})

	t.Run("SaturatedEndIsAccepted", func(t *testing.T) {
		// StartRange = 0, EndRange = +Inf → endRangeUs saturates to MaxInt64,
		// 所有 row 都 <= MaxInt64,endIdx = 最後一個 row,start = 0,
		// MaxMean 應該算得出來且等於完整 range 的計算。
		calc := NewMaxMeanCalculator(0)
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    make([]models.EMGData, 10),
		}
		for i := range dataset.Data {
			dataset.Data[i] = models.EMGData{
				Time:     float64(i) * 0.1,
				Channels: []float64{float64(i)}, // 0..9
			}
		}

		results, err := calc.CalculateWithRange(context.Background(), dataset, 3, 0, math.Inf(1))
		require.NoError(t, err)
		require.Len(t, results, 1)
		// 最大平均窗口應為 idx 7..9: (7+8+9)/3 = 8.0
		assert.InDelta(t, 8.0, results[0].MaxMean, 1e-9)
	})

	t.Run("SaturationFunctionContract", func(t *testing.T) {
		// 直接驗 saturateMicroseconds 對極端 input 的 contract,
		// 不依賴下游 resolveDataRange 的副作用。
		cases := []struct {
			name string
			in   float64
			want int64
		}{
			{"PlusInf saturates to MaxInt64", math.Inf(1), math.MaxInt64},
			{"MinusInf saturates to MinInt64", math.Inf(-1), math.MinInt64},
			{"NaN clamps to 0", math.NaN(), 0},
			{"Above MaxInt64/1e6 saturates", 1e18, math.MaxInt64},
			{"Below MinInt64/1e6 saturates", -1e18, math.MinInt64},
			{"Normal value passes through", 1.5, int64(1.5 * MicrosecondsPerSecond)},
			{"Zero passes through", 0, 0},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := saturateMicroseconds(tc.in)
				assert.Equal(t, tc.want, got,
					"saturateMicroseconds(%v) = %d, want %d", tc.in, got, tc.want)
			})
		}
	})
}

// TestSlidingWindow_RaggedRow_FailsFast 釘住 任一 row 通道數不等於
// 第一列即 fail-fast,不可再走過去 EMGDatasetProvider.GetValue 靜默補 0 的
// silent-miscompute 路徑。對齊 normalizer 的 ErrChannelMismatch 模型。
//
// 兩條子測試:
//   - validateInput 直接回 ErrChannelMismatch error (正常路徑,所有 caller 都走
//     MaxMean.Calculate 入口)
//   - NewEMGDatasetProvider 構造階段同步 panic with row_index / row_channels /
//     expected_channels context (defense-in-depth,擋住跳過 validateInput 的測試
//     jig / 直接 struct literal 使用者;搭配 t.Run subtest 不會傳染給 t.Parallel
//     的 sibling)
func TestSlidingWindow_RaggedRow_FailsFast(t *testing.T) {
	makeRagged := func() *models.EMGDataset {
		// row 0: 3 channels,row 1: 3 channels,row 2: 2 channels (ragged) →
		// expectedChannels = 3,row 2 應觸發 fail-fast。10 rows 才能滿足 windowSize=3
		// 的 ErrWindowTooLarge guard。
		data := make([]models.EMGData, 10)
		for i := range data {
			data[i] = models.EMGData{
				Time:     float64(i) * 0.001,
				Channels: []float64{1.0, 2.0, 3.0},
			}
		}
		data[2].Channels = []float64{1.0, 2.0} // ragged: 缺一個 channel
		return &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2", "Ch3"},
			Data:    data,
		}
	}

	t.Run("ValidateInput_ReturnsErrChannelMismatch", func(t *testing.T) {
		calc := NewMaxMeanCalculator(0)
		dataset := makeRagged()

		results, err := calc.Calculate(context.Background(), dataset, 3)
		require.Error(t, err, "ragged row must fail-fast before sliding window")
		require.True(t, errors.Is(err, calcerrors.ErrChannelMismatch),
			"expected ErrChannelMismatch sentinel, got %v", err)
		assert.Nil(t, results, "fail-fast: no partial result on ragged row")
	})

	t.Run("NewEMGDatasetProvider_PanicsWithContext", func(t *testing.T) {
		// 直接呼 NewEMGDatasetProvider — 跳過 MaxMean.validateInput 的 jig 路徑,
		// expectedChannels 不一致應在構造階段 panic with 清楚 context。
		dataset := makeRagged()

		// 用 deferred recover 捕捉 panic message + 驗證含
		// row_index / row_channels / expected_channels token。
		defer func() {
			r := recover()
			require.NotNil(t, r, "NewEMGDatasetProvider on ragged dataset must panic")
			msg, ok := r.(string)
			require.True(t, ok, "panic value should be string, got %T", r)
			assert.Contains(t, msg, "row_index=3",
				"panic message should pinpoint the offending row (1-based, ragged row is idx 2 → row_index=3)")
			assert.Contains(t, msg, "row_channels=2", "panic message should report actual row width")
			assert.Contains(t, msg, "expected_channels=3", "panic message should report expected width")
			assert.Contains(t, msg, "EMGDatasetProvider", "panic message should identify the source component")
		}()

		_ = NewEMGDatasetProvider(dataset)
	})

	t.Run("GetValue_PanicsWithContext_OnPostConstructionMutation", func(t *testing.T) {
		// 模擬「provider 構造後 dataset 被外部修改成 ragged」的場景 —
		// expectedChannels snapshot 過,GetValue 偵測到 row.Channels 長度與
		// expectedChannels 不一致時 panic with row_index + channel_index + expected。
		uniformData := make([]models.EMGData, 5)
		for i := range uniformData {
			uniformData[i] = models.EMGData{
				Time:     float64(i) * 0.001,
				Channels: []float64{1.0, 2.0, 3.0},
			}
		}
		ds := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2", "Ch3"},
			Data:    uniformData,
		}

		provider := NewEMGDatasetProvider(ds)
		// 構造後外部把第 2 行截掉一個 channel — 過去 GetValue 會靜默回 0;
		// 現在 expectedChannels 已 snapshot = 3,GetValue 看到 len = 2 → panic。
		ds.Data[1].Channels = []float64{1.0, 2.0}

		defer func() {
			r := recover()
			require.NotNil(t, r, "GetValue on ragged row must panic")
			msg, ok := r.(string)
			require.True(t, ok, "panic value should be string, got %T", r)
			assert.Contains(t, msg, "row_index=2", "row 2 (1-based) is ragged")
			assert.Contains(t, msg, "row_channels=2", "actual row width")
			assert.Contains(t, msg, "expected_channels=3", "snapshot expected width")
			assert.Contains(t, msg, "channel_index=2", "the queried channel index")
		}()

		// 嘗試讀 channel 2 of row 1 (0-based) → row 通道數 2 != expected 3 → panic
		_ = provider.GetValue(1, 2)
	})
}

// TestSaturateMicroseconds_TimePath 守護 resolveDataRange 對 data.Time 的
// 微秒換算必須與 startRange / endRange 走同一個 saturateMicroseconds path。
//
// 過去 maxmean.go:475 與 :484 用 `int64(math.Round(data.Time * MicrosecondsPerSecond))`
// 裸 cast;data.Time = +Inf / -Inf / 1e18 等極端值會 wrap-around 成不可預期的 int64,
// 與 startRangeUs / endRangeUs (已 saturate) 比對全錯,startIdx / endIdx 搜尋結果
// 隨機 — silent miscompute。修正後兩條 path 都改走 saturateMicroseconds clamp。
//
// 兩條互補:
//   - Branch1 (HasEndRange=false, maxmean.go 470 區塊):row.Time 含 +Inf 時,
//     saturate 後 dataTimeUs == MaxInt64,>= startRangeUs (合法 finite) → 命中為
//     有效 startIdx。windowSize=2 + finite startRange (=0) → 應算出整段 max,
//     +Inf row 影響 startIdx 但不污染 sliding window 結果 (channel 值仍 finite)。
//   - Branch2 (HasEndRange=true, maxmean.go 482 區塊):類似策略,endRange=+Inf
//     saturate 後 endRangeUs == MaxInt64,所有 row 都 <= → endIdx = 最後 row。
//     row.Time 含 +Inf 在 Time path 也應 saturate 後 <= MaxInt64,符合預期。
//
// Time path 的真實 fail 模式是「裸 cast +Inf 為 int64 各平台行為 implementation-
// defined」— wrap 後可能為負值,startIdx 搜尋路徑全錯。saturate 後行為可預期。
func TestSaturateMicroseconds_TimePath(t *testing.T) {
	t.Run("Branch1_HasEndRangeFalse_PlusInfTimeRow_IsClampedToMaxInt64", func(t *testing.T) {
		// HasEndRange=false 走 maxmean.go 第 470 區塊 (僅 startRange filter)。
		// 構造:dataset 含 5 個 finite row + 末尾 1 個 +Inf time row;
		// startRange=0 saturate 後 = 0,所有 finite row.Time >=0 命中 startIdx=0。
		// +Inf row 經 saturate 為 MaxInt64,符合「>= 0」契約,不會走入未定義 cast。
		//
		// channel 值全 finite,sliding window 結果不受 +Inf row 影響 (channel 0 行為定義),
		// 但 startIdx 搜尋若 +Inf cast wrap 成負值,行為錯亂。
		calc := NewMaxMeanCalculator(0)
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    make([]models.EMGData, 6),
		}
		for i := 0; i < 5; i++ {
			dataset.Data[i] = models.EMGData{
				Time:     float64(i) * 0.1,
				Channels: []float64{float64(i)},
			}
		}
		// 末尾 row.Time = +Inf,channel 值仍 finite 避免 validateInput 攔下
		dataset.Data[5] = models.EMGData{
			Time:     math.Inf(1),
			Channels: []float64{5.0},
		}

		results, err := calc.calculateWithOptions(context.Background(), dataset, 3, CalculationOptions{
			StartRange:    0.0,
			HasStartRange: true,
			// HasEndRange=false → 走 maxmean.go:470 區塊
		})
		require.NoError(t, err, "Time path saturate 後 +Inf row 不應 crash 或誤入錯誤 branch")
		require.Len(t, results, 1)
		// startIdx=0 (finite row Time=0 >= startRange=0),endIdx=5 (最後 row +Inf)。
		// idx 0..5 共 6 row,sliding window=3,最後窗 3+4+5 = 12/3 = 4.0
		assert.InDelta(t, 4.0, results[0].MaxMean, 1e-9,
			"Time path 對 +Inf 必須 saturate 到 MaxInt64,不可裸 cast wrap;結果應為 4 (idx 3..5)")
	})

	t.Run("Branch2_HasEndRangeTrue_PlusInfTimeRow_IsClampedToMaxInt64", func(t *testing.T) {
		// HasEndRange=true 走 maxmean.go 第 482 else 區塊 (start + end filter)。
		// 構造同上,但 endRange=+Inf saturate 為 MaxInt64,所有 row <= → endIdx=5。
		calc := NewMaxMeanCalculator(0)
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    make([]models.EMGData, 6),
		}
		for i := 0; i < 5; i++ {
			dataset.Data[i] = models.EMGData{
				Time:     float64(i) * 0.1,
				Channels: []float64{float64(i)},
			}
		}
		dataset.Data[5] = models.EMGData{
			Time:     math.Inf(1),
			Channels: []float64{5.0},
		}

		results, err := calc.calculateWithOptions(context.Background(), dataset, 3, CalculationOptions{
			StartRange:    0.0,
			EndRange:      math.Inf(1),
			HasStartRange: true,
			HasEndRange:   true,
		})
		require.NoError(t, err, "Branch2 對 Time +Inf 與 endRange +Inf 必須 saturate symmetric")
		require.Len(t, results, 1)
		assert.InDelta(t, 4.0, results[0].MaxMean, 1e-9,
			"Branch2 (HasEndRange=true) 結果同 Branch1,Time path saturate 對稱")
	})

	t.Run("DirectSaturationContract_TimePath_Inf", func(t *testing.T) {
		// 對齊 SaturationFunctionContract,釘住 Time path caller 用同一 helper
		// (而非直接 cast) 的單元保證。重複驗 saturateMicroseconds 為 Time path 的
		// 共用契約 — 加 explicit Test 句點避免未來 refactor 不小心改回裸 cast。
		assert.Equal(t, int64(math.MaxInt64), saturateMicroseconds(math.Inf(1)),
			"Time path +Inf row 用 saturate 後 == MaxInt64")
		assert.Equal(t, int64(math.MinInt64), saturateMicroseconds(math.Inf(-1)),
			"Time path -Inf row 用 saturate 後 == MinInt64")
	})
}

// TestMaxMean_IsRanged_RespectsZeroExplicitFlag 守護 isRanged 不能再依賴
// 舊的 `StartRange != 0` 隱式 sentinel,必須完全由 HasStartRange / HasEndRange
// 顯式旗標決定。
//
// 之前的 OR 子句 (`opts.StartRange != 0`) 違反 契約 — 任何沒設 HasStartRange
// 但 StartRange 偶然非 0 的 caller 會被誤判為「ranged」走 resolveDataRange 而非
// 整段資料。修正後:HasStartRange=false + StartRange=0 必須走「整段資料」路徑;
// HasStartRange=true + StartRange=0 必須走「ranged from 0」路徑。
//
// 構造策略:dataset Time 從 -0.5 開始,t<0 放高值 (100),t>=0 放低值 (1)。
//   - HasStartRange=true + StartRange=0:應只看到 t>=0 區間,MaxMean=1
//   - HasStartRange=false + StartRange=0:應從整段起點 t=-0.5 起算,MaxMean=100
//     (windowSize=3 涵蓋前 3 個 100 → mean=100)
//
// 注意:這個 test 與 TestCalculate_HasStartRange_ZeroIsExplicit 互補 — 前者鎖死
// HasStartRange=true 的語意,本 test 守住「HasStartRange=false + StartRange=0
// 不能被舊 sentinel 路徑誤判為 ranged」的對稱契約。
func TestMaxMean_IsRanged_RespectsZeroExplicitFlag(t *testing.T) {
	calc := NewMaxMeanCalculator(0)

	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data:    make([]models.EMGData, 11),
	}
	for i := range dataset.Data {
		dataset.Data[i].Time = float64(i)*0.1 - 0.5
		if i < 5 {
			dataset.Data[i].Channels = []float64{100.0}
		} else {
			dataset.Data[i].Channels = []float64{1.0}
		}
	}

	t.Run("HasStartRangeTrue_StartZero_IsRanged", func(t *testing.T) {
		// HasStartRange=true + StartRange=0 → ranged 從 t=0 起算,只看到低值 → MaxMean=1
		results, err := calc.calculateWithOptions(context.Background(), dataset, 3, CalculationOptions{
			StartRange:    0.0,
			EndRange:      0.5,
			HasStartRange: true,
			HasEndRange:   true,
		})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.InDelta(t, 1.0, results[0].MaxMean, 1e-9,
			"HasStartRange=true 即使 StartRange=0 也須視為顯式下界,MaxMean=1 (低值區)")
	})

	t.Run("HasStartRangeFalse_StartZero_NotRanged", func(t *testing.T) {
		// HasStartRange=false + HasEndRange=false + StartRange=0 + EndRange=0 →
		// isRanged 應為 false,走「整段資料」路徑,MaxMean=100 (前 3 個高值點)。
		// 舊 sentinel 路徑會因 `StartRange == 0` 落入 `EndRange != 0` else 分支
		// 之後 endIdx 仍為 -1 → 但更糟的是即使有 OR `StartRange != 0` 子句也
		// 會把 zero-zero 視為「未設 ranged」走整段。這個 case 主要確保 zero-zero
		// 不會誤入 ranged 分支。
		results, err := calc.calculateWithOptions(context.Background(), dataset, 3, CalculationOptions{
			StartRange: 0.0,
			EndRange:   0.0,
		})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.InDelta(t, 100.0, results[0].MaxMean, 1e-9,
			"HasStartRange=false + HasEndRange=false 必須走整段資料,MaxMean=100 (高值區)")
	})

	t.Run("HasStartRangeFalse_StartNonZero_LegacyOrClauseRemoved", func(t *testing.T) {
		// 這是 的核心 regression:HasStartRange=false 但 StartRange != 0
		// (例如 caller 沒遷移到顯式旗標,但 struct literal 帶非零值) 必須走
		// 整段資料路徑,不能因為舊 `StartRange != 0` OR 子句被誤分類為 ranged。
		//
		// 期望:整段 sliding window 算出 MaxMean=100 (前 3 個高值點)。
		// 若舊 OR 子句殘留:會誤入 ranged 分支,以 StartRange=0.2 為下界,
		// 從 idx 7 (Time=0.2) 之後只剩低值 → MaxMean=1,並非整段最大值。
		results, err := calc.calculateWithOptions(context.Background(), dataset, 3, CalculationOptions{
			StartRange: 0.2,
		})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.InDelta(t, 100.0, results[0].MaxMean, 1e-9,
			"HasStartRange=false + StartRange!=0 不可再走 ranged 分支 (legacy OR 子句移除)")
	})
}
