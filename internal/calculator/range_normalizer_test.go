package calculator

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
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
