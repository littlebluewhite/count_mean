package util_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/util"
)

func TestMean(t *testing.T) {
	t.Run("test 1", func(t *testing.T) {
		r := util.ArrayMean[float64]([]float64{1, 2, 3, 4, 5})
		require.Equal(t, float64(3), r)
	})
	t.Run("test 2", func(t *testing.T) {
		r := util.ArrayMean[float64]([]float64{1, 2, 3, 8, 5})
		require.Equal(t, 3.8, r)
	})
}

// TestArrayMean_EmptyPanics 是 cross-compare review 的 footgun regression test。
// 修前：len(data)==0 會 sum/T(0) → +Inf/NaN（float）或 panic divide-by-zero（int），
// 視 type 而定，行為不明確；修後 panic with clear message 讓 caller 違反契約時立刻爆。
func TestArrayMean_EmptyPanics(t *testing.T) {
	require.PanicsWithValue(
		t,
		"util.ArrayMean: empty slice — caller must guard len(data) > 0",
		func() { util.ArrayMean[float64](nil) },
	)
	require.PanicsWithValue(
		t,
		"util.ArrayMean: empty slice — caller must guard len(data) > 0",
		func() { util.ArrayMean[float64]([]float64{}) },
	)
}
