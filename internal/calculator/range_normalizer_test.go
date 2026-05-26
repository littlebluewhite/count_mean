package calculator

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// makeSampleEMG 建立一份 sample EMG，時間 0.0..0.9（10 個取樣點），
// 預先安排兩條肌肉的最大值落在不同位置以驗證每條肌肉獨立計算最大值。
func makeSampleEMG() *models.PhaseSyncEMGData {
	time := []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9}
	muscleA := []float64{1.0, 2.0, 4.0, 8.0, 10.0, 5.0, 3.0, 1.5, 0.5, 0.2}
	muscleB := []float64{0.5, 0.5, 1.0, 2.0, 4.0, 8.0, 6.0, 3.0, 1.0, 0.5}

	return &models.PhaseSyncEMGData{
		Time: time,
		Channels: map[string][]float64{
			"MuscleA": muscleA,
			"MuscleB": muscleB,
		},
		Headers: []string{"MuscleA", "MuscleB"},
	}
}

func TestRangeNormalizer_NormalizeByRangeMax_Success(t *testing.T) {
	normalizer := NewRangeNormalizer()
	data := makeSampleEMG()

	// 區間 [0.2, 0.6]：MuscleA 在 0.4 達 10.0；MuscleB 在 0.5 達 8.0
	normalized, channelMaxes, err := normalizer.NormalizeByRangeMax(data, 0.2, 0.6)
	require.NoError(t, err)
	require.NotNil(t, normalized)

	// channelMaxes 應記錄標準化前的最大值
	require.InDelta(t, 10.0, channelMaxes["MuscleA"], 1e-9)
	require.InDelta(t, 8.0, channelMaxes["MuscleB"], 1e-9)

	// 標準化後在 [0.2, 0.6] 內的最大值應為 1.0
	var maxA, maxB float64

	for i, tv := range normalized.Time {
		if tv < 0.2 || tv > 0.6 {
			continue
		}

		if normalized.Channels["MuscleA"][i] > maxA {
			maxA = normalized.Channels["MuscleA"][i]
		}

		if normalized.Channels["MuscleB"][i] > maxB {
			maxB = normalized.Channels["MuscleB"][i]
		}
	}

	require.InDelta(t, 1.0, maxA, 1e-9, "MuscleA max in range should be 1.0 after normalization")
	require.InDelta(t, 1.0, maxB, 1e-9, "MuscleB max in range should be 1.0 after normalization")

	// 區間外資料也應被同樣的 max 除過：MuscleA 第 0 個是 1.0/10.0 = 0.1
	require.InDelta(t, 0.1, normalized.Channels["MuscleA"][0], 1e-9)
}

func TestRangeNormalizer_NormalizeByRangeMax_OutsideRangeMayExceedOne(t *testing.T) {
	// 若標準化的 max 出現在區間外，則區間內的值可能 > 1。
	normalizer := NewRangeNormalizer()
	data := makeSampleEMG()

	// 區間 [0.5, 0.9]：MuscleA 在這區間內 max = 5.0（位置 0.5）
	// 但全資料 MuscleA 最大值 10.0 在 0.4，落在區間外
	normalized, channelMaxes, err := normalizer.NormalizeByRangeMax(data, 0.5, 0.9)
	require.NoError(t, err)
	require.InDelta(t, 5.0, channelMaxes["MuscleA"], 1e-9)

	// 區間外 0.4 對應原值 10.0，標準化後 = 10.0/5.0 = 2.0 > 1
	require.InDelta(t, 2.0, normalized.Channels["MuscleA"][4], 1e-9)
}

func TestRangeNormalizer_NormalizeByRangeMax_DoesNotMutateInput(t *testing.T) {
	normalizer := NewRangeNormalizer()
	data := makeSampleEMG()

	originalA := append([]float64(nil), data.Channels["MuscleA"]...)
	originalB := append([]float64(nil), data.Channels["MuscleB"]...)
	originalTime := append([]float64(nil), data.Time...)

	_, _, err := normalizer.NormalizeByRangeMax(data, 0.2, 0.6)
	require.NoError(t, err)

	require.Equal(t, originalA, data.Channels["MuscleA"], "input MuscleA should not be mutated")
	require.Equal(t, originalB, data.Channels["MuscleB"], "input MuscleB should not be mutated")
	require.Equal(t, originalTime, data.Time, "input Time should not be mutated")
}

func TestRangeNormalizer_NormalizeByRangeMax_ZeroMaxReturnsError(t *testing.T) {
	normalizer := NewRangeNormalizer()
	data := &models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.1, 0.2, 0.3, 0.4},
		Channels: map[string][]float64{
			"ZeroMuscle":   {0.0, 0.0, 0.0, 0.0, 0.0},
			"NormalMuscle": {1.0, 2.0, 3.0, 4.0, 5.0},
		},
		Headers: []string{"ZeroMuscle", "NormalMuscle"},
	}

	_, _, err := normalizer.NormalizeByRangeMax(data, 0.1, 0.3)
	require.Error(t, err)

	var zeroErr *ErrZeroChannelMax
	require.True(t, errors.As(err, &zeroErr), "error should be *ErrZeroChannelMax")
	require.Equal(t, "ZeroMuscle", zeroErr.Channel)
	require.Contains(t, err.Error(), "ZeroMuscle")
	require.Contains(t, err.Error(), "最大值為 0")
}

// TestRangeNormalizer_NormalizeByRangeMax_MissingChannelReturnsDistinctError 守護
// 「通道在 Headers 但不在 Channels map」必須回 ErrMissingChannelInRange,
// 不再與「通道存在但實測全為 0」(ErrZeroChannelMax) 共用同一個錯誤型別。
// 前端可區分「資料缺失需查 parser」vs「實測為 0 需檢查設備」兩種診斷流程。
func TestRangeNormalizer_NormalizeByRangeMax_MissingChannelReturnsDistinctError(t *testing.T) {
	normalizer := NewRangeNormalizer()
	data := &models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.1, 0.2, 0.3, 0.4},
		Channels: map[string][]float64{
			// "MissingMuscle" 出現在 Headers 但不在 Channels — 模擬 parser / mapping bug
			"NormalMuscle": {1.0, 2.0, 3.0, 4.0, 5.0},
		},
		Headers: []string{"MissingMuscle", "NormalMuscle"},
	}

	_, _, err := normalizer.NormalizeByRangeMax(data, 0.1, 0.3)
	require.Error(t, err)

	var missingErr *ErrMissingChannelInRange
	require.True(t, errors.As(err, &missingErr),
		"missing-channel 必須是 *ErrMissingChannelInRange,實際=%T (%v)", err, err)
	require.Equal(t, "MissingMuscle", missingErr.Channel)
	require.Contains(t, err.Error(), "MissingMuscle")
	require.Contains(t, err.Error(), "缺少資料")

	// 雙保險:絕不能誤判為 ErrZeroChannelMax
	var zeroErr *ErrZeroChannelMax
	require.False(t, errors.As(err, &zeroErr),
		"missing-channel 不應被誤判為 ErrZeroChannelMax — 兩個 error type 必須互斥")
}

// TestComputeChannelMaxes_EmptyChannelSliceIsMissing 守護:
// 通道在 Channels map 但對應的 slice 長度為 0 (區間切出空 slice) 也算 missing,
// 不算 zero — 因為「沒資料」與「實測為 0」的根因不同。
// 直接測 computeChannelMaxes — 因為 NormalizeByRangeMax 走 GetEMGDataInTimeRange
// 切片時對空 slice 會 panic,empty 案例只能透過內部 helper 觸發。
func TestComputeChannelMaxes_EmptyChannelSliceIsMissing(t *testing.T) {
	rangeResult := &parsers.EMGTimeRangeResult{
		Data: &models.PhaseSyncEMGData{
			Time: []float64{0.0, 0.1},
			Channels: map[string][]float64{
				"EmptyMuscle":  {}, // 長度 0
				"NormalMuscle": {1.0, 2.0},
			},
			Headers: []string{"EmptyMuscle", "NormalMuscle"},
		},
		ActualStartTime: 0.0,
		ActualEndTime:   0.1,
	}

	_, err := computeChannelMaxes(rangeResult, []string{"EmptyMuscle", "NormalMuscle"})
	require.Error(t, err)

	var missingErr *ErrMissingChannelInRange
	require.True(t, errors.As(err, &missingErr),
		"empty slice 應算 missing 而非 zero,實際=%T (%v)", err, err)
	require.Equal(t, "EmptyMuscle", missingErr.Channel)
}

func TestRangeNormalizer_NormalizeByRangeMax_NilData(t *testing.T) {
	normalizer := NewRangeNormalizer()
	_, _, err := normalizer.NormalizeByRangeMax(nil, 0.0, 1.0)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrEmptyEMGData)
}

func TestRangeNormalizer_NormalizeByRangeMax_InvalidRange(t *testing.T) {
	normalizer := NewRangeNormalizer()
	data := makeSampleEMG()

	// startTime > endTime → 應由底層 GetDataInTimeRange 拒絕
	_, _, err := normalizer.NormalizeByRangeMax(data, 0.6, 0.2)
	require.Error(t, err)
}

func TestRangeNormalizer_NormalizeByRangeMax_PreservesHeaderOrder(t *testing.T) {
	normalizer := NewRangeNormalizer()
	data := makeSampleEMG()

	normalized, _, err := normalizer.NormalizeByRangeMax(data, 0.2, 0.6)
	require.NoError(t, err)
	require.Equal(t, data.Headers, normalized.Headers)
}

func TestRangeNormalizer_NormalizeByRangeMax_HandlesNegativeMaxValue(t *testing.T) {
	// 如果區間內 max 為負（例如全部為負值），仍視為合法數學運算，
	// 標準化會把符號反轉，但不拋錯（max != 0）。
	normalizer := NewRangeNormalizer()
	data := &models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.1, 0.2, 0.3},
		Channels: map[string][]float64{
			"NegMuscle": {-5.0, -3.0, -1.0, -2.0},
		},
		Headers: []string{"NegMuscle"},
	}

	normalized, channelMaxes, err := normalizer.NormalizeByRangeMax(data, 0.0, 0.3)
	require.NoError(t, err)
	require.Equal(t, -1.0, channelMaxes["NegMuscle"])

	// 標準化後最大值（即原 max = -1）應等於 1.0
	got := normalized.Channels["NegMuscle"][2]
	require.True(t, math.Abs(got-1.0) < 1e-9, "got %v", got)
}

// TestBuildNormalizedDataset_NaNInfChannelMaxes 驗證 第一支：
// channelMaxes 含 NaN/Inf 必須回明確錯誤，避免下游全資料污染。
func TestBuildNormalizedDataset_NaNInfChannelMaxes(t *testing.T) {
	data := &models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.1, 0.2},
		Channels: map[string][]float64{
			"MuscleA": {1.0, 2.0, 3.0},
			"MuscleB": {0.5, 1.0, 1.5},
		},
		Headers: []string{"MuscleA", "MuscleB"},
	}

	t.Run("NaNMax", func(t *testing.T) {
		channelMaxes := map[string]float64{
			"MuscleA": math.NaN(),
			"MuscleB": 1.5,
		}
		got, err := buildNormalizedDataset(data, channelMaxes)
		require.Error(t, err)
		require.Contains(t, err.Error(), "MuscleA")
		require.Contains(t, err.Error(), "NaN")
		require.Nil(t, got)
	})

	t.Run("PositiveInfMax", func(t *testing.T) {
		channelMaxes := map[string]float64{
			"MuscleA": 3.0,
			"MuscleB": math.Inf(1),
		}
		got, err := buildNormalizedDataset(data, channelMaxes)
		require.Error(t, err)
		require.Contains(t, err.Error(), "MuscleB")
		require.Contains(t, err.Error(), "Inf")
		require.Nil(t, got)
	})

	t.Run("NegativeInfMax", func(t *testing.T) {
		channelMaxes := map[string]float64{
			"MuscleA": math.Inf(-1),
			"MuscleB": 1.5,
		}
		got, err := buildNormalizedDataset(data, channelMaxes)
		require.Error(t, err)
		require.Contains(t, err.Error(), "MuscleA")
		require.Contains(t, err.Error(), "Inf")
		require.Nil(t, got)
	})
}

// TestBuildNormalizedDataset_MissingChannelMax 驗證 第二支：
// headers 列舉的 channel 在 channelMaxes 中缺鍵時，需明確錯誤帶 channel 名，
// 而非無聲產出空 dst slice。
func TestBuildNormalizedDataset_MissingChannelMax(t *testing.T) {
	data := &models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.1, 0.2},
		Channels: map[string][]float64{
			"MuscleA": {1.0, 2.0, 3.0},
			"MuscleB": {0.5, 1.0, 1.5},
		},
		Headers: []string{"MuscleA", "MuscleB"},
	}

	// channelMaxes 缺 MuscleB
	channelMaxes := map[string]float64{
		"MuscleA": 3.0,
	}
	got, err := buildNormalizedDataset(data, channelMaxes)
	require.Error(t, err)
	require.Contains(t, err.Error(), "MuscleB")
	require.Nil(t, got)
}

// TestBuildNormalizedDataset_MissingChannelData 驗證 補強：
// headers 列舉的 channel 在 data.Channels 中缺鍵時也回明確錯誤，
// 避免靜默產生長度 0 的 normalized channel。
func TestBuildNormalizedDataset_MissingChannelData(t *testing.T) {
	data := &models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.1, 0.2},
		Channels: map[string][]float64{
			"MuscleA": {1.0, 2.0, 3.0},
			// MuscleB key 不存在
		},
		Headers: []string{"MuscleA", "MuscleB"},
	}
	channelMaxes := map[string]float64{
		"MuscleA": 3.0,
		"MuscleB": 1.5,
	}
	got, err := buildNormalizedDataset(data, channelMaxes)
	require.Error(t, err)
	require.Contains(t, err.Error(), "MuscleB")
	require.Nil(t, got)
}

// TestComputeChannelMaxes_NaNInMiddle 驗證
// util.ArrayMax 用 `>` 比較對 NaN 不安全 — NaN 在中間 / 尾端時會被 silent skip，
// 最大值會落在 non-NaN 元素上，下游 buildNormalizedDataset 完全偵測不到 NaN，
// 結果 NaN sample 會以「除以 non-NaN max」流入 normalized output。
// 修法：computeChannelMaxes 讀完 channelData 後做 NaN/Inf scan，
// 任一位置出現 NaN/Inf 即 fail-fast 對齊 ErrChannelMaxNaN / ErrChannelMaxInf。
func TestComputeChannelMaxes_NaNInMiddle(t *testing.T) {
	normalizer := NewRangeNormalizer()

	t.Run("NaNAtMiddle", func(t *testing.T) {
		data := &models.PhaseSyncEMGData{
			Time: []float64{0.0, 0.1, 0.2},
			Channels: map[string][]float64{
				"MuscleA": {1.0, math.NaN(), 3.0},
			},
			Headers: []string{"MuscleA"},
		}
		_, _, err := normalizer.NormalizeByRangeMax(data, 0.0, 0.2)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrChannelMaxNaN)
		require.Contains(t, err.Error(), "MuscleA")
	})

	t.Run("NaNAtTail", func(t *testing.T) {
		data := &models.PhaseSyncEMGData{
			Time: []float64{0.0, 0.1, 0.2},
			Channels: map[string][]float64{
				"MuscleA": {1.0, 3.0, math.NaN()},
			},
			Headers: []string{"MuscleA"},
		}
		_, _, err := normalizer.NormalizeByRangeMax(data, 0.0, 0.2)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrChannelMaxNaN)
		require.Contains(t, err.Error(), "MuscleA")
	})

	t.Run("NaNAtHead", func(t *testing.T) {
		// NaN 在 head 位置原本就會被 ArrayMax 帶到 maxVal（因為 maxVal := data[0]），
		// downstream buildNormalizedDataset 才會抓到 — 但現在統一在 computeChannelMaxes
		// 階段就 fail-fast，error 也是同一條 ErrChannelMaxNaN。
		data := &models.PhaseSyncEMGData{
			Time: []float64{0.0, 0.1, 0.2},
			Channels: map[string][]float64{
				"MuscleA": {math.NaN(), 1.0, 3.0},
			},
			Headers: []string{"MuscleA"},
		}
		_, _, err := normalizer.NormalizeByRangeMax(data, 0.0, 0.2)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrChannelMaxNaN)
		require.Contains(t, err.Error(), "MuscleA")
	})

	t.Run("PositiveInfAtMiddle", func(t *testing.T) {
		// +Inf 會被 ArrayMax 比較規則接受為 max（Inf > 任何有限值），
		// 下游 buildNormalizedDataset 的 IsInf 檢查雖能擋下，但語意上
		// 同樣應該在 compute 階段就 fail-fast，避免 sentinel 落到不同層。
		data := &models.PhaseSyncEMGData{
			Time: []float64{0.0, 0.1, 0.2},
			Channels: map[string][]float64{
				"MuscleA": {1.0, math.Inf(1), 3.0},
			},
			Headers: []string{"MuscleA"},
		}
		_, _, err := normalizer.NormalizeByRangeMax(data, 0.0, 0.2)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrChannelMaxInf)
		require.Contains(t, err.Error(), "MuscleA")
	})

	t.Run("NegativeInfAtMiddle", func(t *testing.T) {
		// -Inf 不會被 ArrayMax 選為 max（被任何有限值蓋過），
		// 但採樣中存在 -Inf 同樣會在 normalize 階段污染整批，需 fail-fast。
		data := &models.PhaseSyncEMGData{
			Time: []float64{0.0, 0.1, 0.2},
			Channels: map[string][]float64{
				"MuscleA": {1.0, math.Inf(-1), 3.0},
			},
			Headers: []string{"MuscleA"},
		}
		_, _, err := normalizer.NormalizeByRangeMax(data, 0.0, 0.2)
		require.Error(t, err)
		require.ErrorIs(t, err, ErrChannelMaxInf)
		require.Contains(t, err.Error(), "MuscleA")
	})
}
