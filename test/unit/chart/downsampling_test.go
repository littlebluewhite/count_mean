package chart_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/chart"
)

func TestLTTBDownsample_BelowOrAtThreshold_ReturnsAllIndices(t *testing.T) {
	xs := []float64{0, 1, 2, 3, 4}
	ys := []float64{0, 1, 4, 9, 16}

	indices := chart.LTTBDownsample(xs, ys, 10)

	assert.Equal(t, []int{0, 1, 2, 3, 4}, indices,
		"threshold > len: 全索引回傳")
}

func TestLTTBDownsample_EqualThreshold_ReturnsAllIndices(t *testing.T) {
	xs := []float64{0, 1, 2, 3, 4}
	ys := []float64{0, 1, 4, 9, 16}

	indices := chart.LTTBDownsample(xs, ys, 5)

	assert.Equal(t, []int{0, 1, 2, 3, 4}, indices,
		"threshold == len: 全索引回傳")
}

func TestLTTBDownsample_ThresholdTwoOrLess_ReturnsAllIndices(t *testing.T) {
	xs := []float64{0, 1, 2, 3, 4}
	ys := []float64{0, 1, 4, 9, 16}

	indices := chart.LTTBDownsample(xs, ys, 2)

	assert.Equal(t, []int{0, 1, 2, 3, 4}, indices,
		"threshold <= 2: LTTB 無意義，退化為全索引")
}

func TestLTTBDownsample_PreservesEndpoints(t *testing.T) {
	n := 1000
	xs := make([]float64, n)
	ys := make([]float64, n)

	for i := range xs {
		xs[i] = float64(i)
		ys[i] = math.Sin(float64(i) * 0.05)
	}

	indices := chart.LTTBDownsample(xs, ys, 50)

	require.Len(t, indices, 50)
	assert.Equal(t, 0, indices[0], "第一個索引必為 0")
	assert.Equal(t, n-1, indices[len(indices)-1], "最後一個索引必為 n-1")
}

func TestLTTBDownsample_IndicesAreMonotonic(t *testing.T) {
	n := 10_000
	xs := make([]float64, n)
	ys := make([]float64, n)

	for i := range xs {
		xs[i] = float64(i) * 0.001
		ys[i] = math.Sin(xs[i]*10) + 0.3*math.Sin(xs[i]*47)
	}

	indices := chart.LTTBDownsample(xs, ys, 200)

	require.Len(t, indices, 200)

	for i := 1; i < len(indices); i++ {
		assert.Greater(t, indices[i], indices[i-1],
			"索引應嚴格遞增 (i=%d, prev=%d, curr=%d)",
			i, indices[i-1], indices[i])
	}
}

func TestLTTBDownsample_IndicesInBounds(t *testing.T) {
	n := 5_000
	xs := make([]float64, n)
	ys := make([]float64, n)

	for i := range xs {
		xs[i] = float64(i)
		ys[i] = float64(i % 100)
	}

	indices := chart.LTTBDownsample(xs, ys, 100)

	for _, idx := range indices {
		assert.GreaterOrEqual(t, idx, 0)
		assert.Less(t, idx, n)
	}
}

func TestLTTBDownsample_MismatchedLength_Panics(t *testing.T) {
	xs := []float64{0, 1, 2, 3}
	ys := []float64{0, 1}

	assert.Panics(t, func() {
		chart.LTTBDownsample(xs, ys, 3)
	}, "xs/ys 長度不一致應 panic")
}

func TestLTTBDownsample_CapturesAmplitudeInHighVarianceRegions(t *testing.T) {
	// 1000 點曲線：前 500 點 y=0，後 500 點正弦震盪 (振幅 ~1.0)。
	// LTTB bucket size 固定，前後各約 25 個取樣點；驗證的不是取樣分佈
	// 而是「後半段取到的 Y 值覆蓋原訊號振幅」— 即 LTTB 在每個 bucket
	// 內成功挑出局部極值，而非平庸地選 bucketStart。
	n := 1000
	xs := make([]float64, n)
	ys := make([]float64, n)

	for i := range xs {
		xs[i] = float64(i)
		if i < n/2 {
			ys[i] = 0
		} else {
			ys[i] = math.Sin(float64(i) * 0.5)
		}
	}

	indices := chart.LTTBDownsample(xs, ys, 50)

	var minY, maxY float64

	for _, idx := range indices {
		if idx >= n/2 {
			if ys[idx] < minY {
				minY = ys[idx]
			}

			if ys[idx] > maxY {
				maxY = ys[idx]
			}
		}
	}

	assert.Greater(t, maxY-minY, 1.5,
		"LTTB 應挑到正弦的峰與谷 (Y 範圍 %.3f..%.3f)，覆蓋振幅至少 1.5", minY, maxY)
}
