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
// ctx.Err() guard at the top of execute. The previous race-y select had a 1/N
// chance of letting one worker job slip through on a fast machine before
// ctx.Done() was observed; this test asserts deterministic short-circuit by
// pinning the returned error to context.Canceled.
//
// Pre-ADR-0006 version of this test additionally asserted
// backpressureController.ActiveJobs() == 0 to prove no worker started, but the
// counter was self-referential — only this test read it. After collapsing the
// controller (ADR-0006) the user-facing contract is fully expressed by the
// errors.Is(context.Canceled) check; the internal counter probe was removed
// per ADR-0006 process note item 5.
func TestCalculate_PreCancelledContext_DoesNotStartWorkers(t *testing.T) {
	calc := NewMaxMeanCalculator(10)
	dataset := buildLargeDataset(10000, 8)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := calc.Calculate(ctx, dataset, 100)
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled),
		"expected context.Canceled, got %v", err)
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

// TestMaxMean_TimeRangeOverflow_Saturated 釘住 scaled 域時間範圍在極端 input
// (±Inf) 下的行為。改用 scaled 域 float64 直比後,±Inf range 不再需要 int64
// clamp:+Inf 下界使任何 finite row 都不命中 → ErrInvalidTimeRange;+Inf 上界
// 使所有 finite row 都通過。配合下游 resolveDataRange 對 startIdx == -1 /
// endIdx == -1 / 區間不足 windowSize 的 ErrInvalidTimeRange,自然 fail-fast。
//
// 三條保護:
//   - SaturatedStartIsRejected:StartRange=+Inf → scaledStartRange=+Inf,
//     任何 finite row.Time < +Inf,startIdx = -1 → ErrInvalidTimeRange。
//   - SaturatedEndIsAccepted:EndRange=+Inf → scaledEndRange=+Inf,
//     所有 finite row.Time <= +Inf 都通過,endIdx == 最後一個 row。startRange 在
//     合法範圍內仍應算出正確 MaxMean。
//   - NormalizeTimeContract:直接呼 normalizeTime 驗 ±Inf 穿透 / NaN→0 /
//     大 finite 值穿透 (不再 ×1e6 溢位) 的契約。
func TestMaxMean_TimeRangeOverflow_Saturated(t *testing.T) {
	t.Run("SaturatedStartIsRejected", func(t *testing.T) {
		// scalingFactor = 0,StartRange = +Inf → scaledStartRange = +Inf,
		// 所有 row.Time finite 都 < +Inf → 找不到合法 startIdx → ErrInvalidTimeRange。
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

	t.Run("NormalizeTimeContract", func(t *testing.T) {
		// 直接驗 normalizeTime 對極端 input 的 contract,不依賴下游 resolveDataRange
		// 的副作用。改用 scaled 域 float64 直比後,只有 NaN 需正規化 (→0),±Inf 與
		// 大 finite 值原封穿透 (float64 比較天然正確,不再 ×1e6 轉 int64 sentinel)。
		cases := []struct {
			name string
			in   float64
			want float64
		}{
			{"PlusInf passes through", math.Inf(1), math.Inf(1)},
			{"MinusInf passes through", math.Inf(-1), math.Inf(-1)},
			{"NaN normalizes to 0", math.NaN(), 0},
			{"Large finite passes through (no 1e6 overflow)", 1e18, 1e18},
			{"Negative large finite passes through", -1e18, -1e18},
			{"Normal value passes through", 1.5, 1.5},
			{"Zero passes through", 0, 0},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				got := normalizeTime(tc.in)
				assert.Equal(t, tc.want, got,
					"normalizeTime(%v) = %v, want %v", tc.in, got, tc.want)
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

// TestNormalizeTime_TimePath 守護 resolveDataRange 對 data.Time 的比較必須與
// startRange / endRange 走同一個 normalizeTime path,在 scaled 域 float64 直比。
//
// data.Time = +Inf 時,float64 比較天然滿足「>= 合法 finite 下界」,不再需要
// ×1e6 轉 int64 (該乘法把溢位門檻拉到 ~922 秒,且 +Inf 裸 cast 為 int64 行為
// implementation-defined)。NaN 經 normalizeTime → 0。
//
// 兩條互補:
//   - Branch1 (HasEndRange=false):row.Time 含 +Inf 時,+Inf >= scaledStartRange
//     (合法 finite) → 命中為有效 startIdx。windowSize=3 + finite startRange (=0)
//     → 應算出整段 max,+Inf row 影響 startIdx 但不污染 sliding window 結果
//     (channel 值仍 finite)。
//   - Branch2 (HasEndRange=true):類似策略,endRange=+Inf → scaledEndRange=+Inf,
//     所有 row 都 <= → endIdx = 最後 row。row.Time 含 +Inf 也 <= +Inf,符合預期。
func TestNormalizeTime_TimePath(t *testing.T) {
	t.Run("Branch1_HasEndRangeFalse_PlusInfTimeRow_IsHandled", func(t *testing.T) {
		// HasEndRange=false 走 maxmean.go 僅 startRange filter 區塊。
		// 構造:dataset 含 5 個 finite row + 末尾 1 個 +Inf time row;
		// startRange=0 → scaledStartRange=0,所有 finite row.Time >=0 命中 startIdx=0。
		// +Inf row 經 normalizeTime 穿透,+Inf >= 0 為 true,不會走入未定義 cast。
		//
		// channel 值全 finite,sliding window 結果不受 +Inf row 影響 (channel 0 行為定義)。
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
		require.NoError(t, err, "Time path +Inf row 經 normalizeTime 不應 crash 或誤入錯誤 branch")
		require.Len(t, results, 1)
		// startIdx=0 (finite row Time=0 >= startRange=0),endIdx=5 (最後 row +Inf)。
		// idx 0..5 共 6 row,sliding window=3,最後窗 3+4+5 = 12/3 = 4.0
		assert.InDelta(t, 4.0, results[0].MaxMean, 1e-9,
			"Time path 對 +Inf 走 float64 直比 (+Inf >= 0),結果應為 4 (idx 3..5)")
	})

	t.Run("Branch2_HasEndRangeTrue_PlusInfTimeRow_IsHandled", func(t *testing.T) {
		// HasEndRange=true 走 maxmean.go start + end filter 區塊。
		// 構造同上,但 endRange=+Inf → scaledEndRange=+Inf,所有 row <= +Inf → endIdx=5。
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
		require.NoError(t, err, "Branch2 對 Time +Inf 與 endRange +Inf 必須 float64 直比對稱")
		require.Len(t, results, 1)
		assert.InDelta(t, 4.0, results[0].MaxMean, 1e-9,
			"Branch2 (HasEndRange=true) 結果同 Branch1,Time path float64 直比對稱")
	})

	t.Run("DirectNormalizeContract_TimePath_Inf", func(t *testing.T) {
		// 對齊 NormalizeTimeContract,釘住 Time path caller 用同一 helper 的單元
		// 保證 — 加 explicit Test 句點避免未來 refactor 不小心改回裸 cast / ×1e6。
		// ±Inf 經 normalizeTime 穿透 (NaN 才正規化),靠 float64 比較天然定序。
		assert.Equal(t, math.Inf(1), normalizeTime(math.Inf(1)),
			"Time path +Inf row 經 normalizeTime 穿透為 +Inf")
		assert.Equal(t, math.Inf(-1), normalizeTime(math.Inf(-1)),
			"Time path -Inf row 經 normalizeTime 穿透為 -Inf")
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

// TestMaxMean_ScaledTimeOverflow_WindowMiscompute 釘住 P2-1:過去 resolveDataRange
// 把 scaled-time 再乘 MicrosecondsPerSecond (×1e6) 才轉 int64 比較,額外的 ×1e6
// 把 int64 溢位門檻從 scaled 域的 ~9.22e18 拉低到 ~9.22e12;在預設 scalingFactor=10
// 下,任何 > ~922 秒的 row.Time × 1e6 全部 clamp 成 MaxInt64,使同一 range 內
// > 922 秒的不同 row 互相無法區分 → startIdx / endIdx 邊界靜默算錯 (非 panic,
// 是錯誤窗格)。改在 scaled 域直接 float64 比較後,溢位與 clamp 一併消失。
//
// 既有 overflow 測試都用 NewMaxMeanCalculator(0),SF=0 下 ×1e6 永遠觸不到 bug;
// 本 test 必須用 SF=10 + 兩個 > 922 秒的 row 落在同一 range 才能重現。
//
// 構造 (SF=10,Str2Number 已把秒乘 1e10 故此處直接放 scaled 值):
//   - 6 個 row,秒 = 1000..1005 (全 > 922.34s),scaled = 1e13 .. 1.005e13。
//     舊 ×1e6 後 (1e13 × 1e6 = 1e19 > MaxInt64) 全 clamp 成 MaxInt64,無法區分。
//   - channels = [1,1,1,100,100,100];range = [1000s, 1002s],windowSize=3。
//   - 正確:in-range = idx 0..2 (三個 1),唯一窗 = (1+1+1)/3 = 1.0。
//   - 舊 bug:endRangeUs 也 clamp MaxInt64 → `dataTimeUs <= endRangeUs` 對所有
//     row 恆 true → endIdx = 5,in-range 變 idx 0..5,max 窗 = (100+100+100)/3 = 100.0。
//
// 故修正前 MaxMean=100 (錯窗),修正後 MaxMean=1 (正確窗)。
func TestMaxMean_ScaledTimeOverflow_WindowMiscompute(t *testing.T) {
	const sf = 10
	const scale = 1e10 // 10^sf,Str2Number 對 row[0] 的乘數
	calc := NewMaxMeanCalculator(sf)

	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data:    make([]models.EMGData, 6),
	}
	chans := []float64{1, 1, 1, 100, 100, 100}
	for i := range dataset.Data {
		dataset.Data[i] = models.EMGData{
			Time:     (1000.0 + float64(i)) * scale, // 1000s..1005s,均 > 922.34s
			Channels: []float64{chans[i]},
		}
	}

	// CalculateWithRange 收的是「秒」,內部自乘 10^sf,與 dataset.Time 同域。
	results, err := calc.CalculateWithRange(context.Background(), dataset, 3, 1000.0, 1002.0)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 1.0, results[0].MaxMean, 1e-9,
		"SF=10 下 >922s 的 range 必須 float64 直比鎖住 idx 0..2 (MaxMean=1);"+
			"舊 ×1e6 溢位會誤納 idx 3..5 算成 100 (錯窗)")

	// 對稱補強:把高值放在 range 前緣外,證明 startIdx 同樣不被溢位 clamp 污染。
	dataset2 := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data:    make([]models.EMGData, 6),
	}
	chans2 := []float64{100, 100, 100, 1, 1, 1}
	for i := range dataset2.Data {
		dataset2.Data[i] = models.EMGData{
			Time:     (1000.0 + float64(i)) * scale,
			Channels: []float64{chans2[i]},
		}
	}
	// range = [1003s, 1005s] → 正確 startIdx=3,in-range idx 3..5,唯一窗 = 1.0。
	// 舊 bug:startRangeUs clamp MaxInt64,start 迴圈第一個 dataTimeUs(MaxInt64)
	// >= startRangeUs(MaxInt64) 即 i=0 命中 → startIdx=0,誤納前緣高值 → 100。
	results2, err := calc.CalculateWithRange(context.Background(), dataset2, 3, 1003.0, 1005.0)
	require.NoError(t, err)
	require.Len(t, results2, 1)
	assert.InDelta(t, 1.0, results2[0].MaxMean, 1e-9,
		"SF=10 下 startIdx 必須 float64 直比鎖在 idx 3 (MaxMean=1);"+
			"舊 ×1e6 溢位會把 startIdx clamp 到 0 誤納前緣 100 (錯窗)")
}

// TestMaxMean_FractionalScaledTimeBoundary 驗證移除 int64 取整後,scaled-time
// 比較不會因取整誤納/漏納次單位邊界。過去 saturateMicroseconds 走
// int64(math.Round(scaled × 1e6)),在 ×1e6 粒度 round;float64 直比則精確到
// scaled 值本身。
//
// 構造 (SF=0 讓 Time = 秒原值,聚焦次單位邊界):row.Time = 0.0, 0.5, 1.0, 1.5, 2.0。
// endRange = 1.4999 (略小於 1.5)。windowSize=2。
//   - 精確比較:idx 3 (Time=1.5) 不應 <= 1.4999 → endIdx=2 (Time=1.0)。
//     in-range idx 0..2,channels = [10,10,1,...] → 窗 (10+10)/2 = 10。
//   - 若有 round-to-1e6 誤差把 1.5 與 1.4999 視為相等 (× 1e6 = 1_500_000 vs
//     1_499_900,其實仍可分辨;此 case 主要鎖「精確 float64 邊界判定」回歸)。
func TestMaxMean_FractionalScaledTimeBoundary(t *testing.T) {
	calc := NewMaxMeanCalculator(0)
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data:    make([]models.EMGData, 5),
	}
	times := []float64{0.0, 0.5, 1.0, 1.5, 2.0}
	chans := []float64{10, 10, 1, 100, 100}
	for i := range dataset.Data {
		dataset.Data[i] = models.EMGData{Time: times[i], Channels: []float64{chans[i]}}
	}

	// endRange 略小於 1.5 → idx 3 在界外,endIdx=2;in-range idx 0..2 唯一窗 = 10。
	results, err := calc.CalculateWithRange(context.Background(), dataset, 2, 0.0, 1.4999)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.InDelta(t, 10.0, results[0].MaxMean, 1e-9,
		"endRange=1.4999 必須精確排除 Time=1.5 的 idx 3,只算 idx 0..2 → MaxMean=10")

	// endRange 恰等於 1.5 → idx 3 在界內 (<=),endIdx=3;in-range idx 0..3,
	// max 窗 = (1+100)/2 = 50.5 (idx 2..3),驗證邊界值「等於上界」被納入。
	results2, err := calc.CalculateWithRange(context.Background(), dataset, 2, 0.0, 1.5)
	require.NoError(t, err)
	require.Len(t, results2, 1)
	assert.InDelta(t, 50.5, results2[0].MaxMean, 1e-9,
		"endRange=1.5 恰等於 Time=1.5 必須納入 idx 3 (<=),max 窗 (1+100)/2=50.5")
}

// TestMaxMean_DirectDataset_NonFiniteTime 驗證 direct-dataset caller
// (CalculateWithRange) 繞過上游 Str2Number 的非有限攔截時 (validateChannelValues
// 只掃 channel 不掃 Time),非有限 row.Time 走 float64 比較不 panic 且語意正確:
// NaN→0、+Inf→極大、−Inf→極小 (normalizeTime 契約)。
func TestMaxMean_DirectDataset_NonFiniteTime(t *testing.T) {
	t.Run("NaNTime_TreatedAsZero", func(t *testing.T) {
		// row.Time = NaN 的 row 經 normalizeTime → 0;startRange=0 → 該 row >= 0
		// 命中。構造 finite tail 確保有合法窗,僅驗 NaN row 不 panic、不亂跳 branch。
		calc := NewMaxMeanCalculator(0)
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: math.NaN(), Channels: []float64{5}}, // → 視為 0
				{Time: 0.1, Channels: []float64{5}},
				{Time: 0.2, Channels: []float64{5}},
			},
		}
		results, err := calc.CalculateWithRange(context.Background(), dataset, 3, 0.0, 0.2)
		require.NoError(t, err, "NaN time row 經 normalizeTime→0 不應 panic")
		require.Len(t, results, 1)
		// NaN→0 命中 startIdx=0,endIdx=2,唯一窗 (5+5+5)/3 = 5。
		assert.InDelta(t, 5.0, results[0].MaxMean, 1e-9,
			"NaN time 視為 0,idx 0 為合法下界,MaxMean=5")
	})

	t.Run("PlusInfTime_TreatedAsVeryLarge", func(t *testing.T) {
		// +Inf row 在 float64 比較下必 > 任何 finite 上界,不會被 endRange 納入。
		calc := NewMaxMeanCalculator(0)
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{10}},
				{Time: 0.1, Channels: []float64{10}},
				{Time: 0.2, Channels: []float64{10}},
				{Time: math.Inf(1), Channels: []float64{100}}, // 界外 (> endRange)
			},
		}
		results, err := calc.CalculateWithRange(context.Background(), dataset, 3, 0.0, 0.2)
		require.NoError(t, err, "+Inf time row 不應 panic")
		require.Len(t, results, 1)
		// +Inf > 0.2 → 不納入,endIdx=2,唯一窗 (10+10+10)/3 = 10 (不含 +Inf 的 100)。
		assert.InDelta(t, 10.0, results[0].MaxMean, 1e-9,
			"+Inf time 視為極大,> endRange=0.2 不納入,MaxMean=10")
	})

	t.Run("MinusInfTime_TreatedAsVerySmall", func(t *testing.T) {
		// −Inf row 在 float64 比較下必 < 任何 finite 下界,不會被 startRange 納入。
		calc := NewMaxMeanCalculator(0)
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: math.Inf(-1), Channels: []float64{100}}, // 界外 (< startRange)
				{Time: 1.0, Channels: []float64{10}},
				{Time: 1.1, Channels: []float64{10}},
				{Time: 1.2, Channels: []float64{10}},
			},
		}
		results, err := calc.CalculateWithRange(context.Background(), dataset, 3, 1.0, 1.2)
		require.NoError(t, err, "−Inf time row 不應 panic")
		require.Len(t, results, 1)
		// −Inf < 1.0 → 不納入 startIdx;startIdx=1,endIdx=3,唯一窗 (10+10+10)/3=10。
		assert.InDelta(t, 10.0, results[0].MaxMean, 1e-9,
			"−Inf time 視為極小,< startRange=1.0 不納入,MaxMean=10")
	})
}

// TestMaxMean_StartBypass_Permutations 釘住 startBypass := !opts.HasStartRange
// (P2 startBypass latent bug)。舊 `!opts.HasStartRange && opts.StartRange == 0`
// 會在 HasStartRange=false 但殘留非零 StartRange 時讓該下界漏進過濾;HasStartRange
// 必須是唯一 source of truth (對齊 struct doc)。
//
// 唯一行為分歧發生於 isRanged=true 才走得到 startBypass 行,即
// HasStartRange=false && HasEndRange=true。構造 dataset:idx 0..2 高值 (Time<下界),
// idx 3.. 低值 → 看 start 是否被殘留 StartRange bypass。
func TestMaxMean_StartBypass_Permutations(t *testing.T) {
	newDataset := func() *models.EMGDataset {
		ds := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    make([]models.EMGData, 6),
		}
		for i := range ds.Data {
			ds.Data[i].Time = float64(i) * 0.1 // 0.0, 0.1, ... 0.5
			if i < 3 {
				ds.Data[i].Channels = []float64{100.0}
			} else {
				ds.Data[i].Channels = []float64{1.0}
			}
		}
		return ds
	}
	calc := NewMaxMeanCalculator(0)

	t.Run("HasStartRangeFalse_StrayStartRange_BypassesLowerBound", func(t *testing.T) {
		// HasStartRange=false 但 StartRange=0.3 (殘留);HasEndRange=true → isRanged。
		// 正解:startBypass=true → startIdx=0,前緣高值 idx 0..2 入窗 → MaxMean=100。
		// 舊 bug (`&& StartRange==0`):StartRange=0.3≠0 → startBypass=false →
		// 以 0.3 為下界,startIdx=3,只剩低值 → MaxMean=1 (錯)。
		ds := newDataset()
		results, err := calc.calculateWithOptions(context.Background(), ds, 3, CalculationOptions{
			StartRange:    0.3,
			EndRange:      0.5,
			HasStartRange: false,
			HasEndRange:   true,
		})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.InDelta(t, 100.0, results[0].MaxMean, 1e-9,
			"HasStartRange=false 必須 bypass 殘留 StartRange=0.3,從 idx 0 起算 → MaxMean=100")
	})

	t.Run("HasStartRangeTrue_StartRangeHonored", func(t *testing.T) {
		// HasStartRange=true + StartRange=0.3 → 顯式下界,startIdx=3,只看低值 → 1。
		ds := newDataset()
		results, err := calc.calculateWithOptions(context.Background(), ds, 3, CalculationOptions{
			StartRange:    0.3,
			EndRange:      0.5,
			HasStartRange: true,
			HasEndRange:   true,
		})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.InDelta(t, 1.0, results[0].MaxMean, 1e-9,
			"HasStartRange=true 須以 StartRange=0.3 為顯式下界,startIdx=3 → MaxMean=1")
	})

	t.Run("HasStartRangeFalse_ZeroStartRange_BypassesUnchanged", func(t *testing.T) {
		// HasStartRange=false + StartRange=0:新舊邏輯一致 (startBypass=true)。
		// 守住改動沒回歸既有 zero-start bypass 行為。
		ds := newDataset()
		results, err := calc.calculateWithOptions(context.Background(), ds, 3, CalculationOptions{
			StartRange:    0.0,
			EndRange:      0.5,
			HasStartRange: false,
			HasEndRange:   true,
		})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.InDelta(t, 100.0, results[0].MaxMean, 1e-9,
			"HasStartRange=false + StartRange=0 仍 bypass,從 idx 0 起算 → MaxMean=100")
	})
}
