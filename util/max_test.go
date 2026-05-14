package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArrayMax_Basic(t *testing.T) {
	t.Run("ascending", func(t *testing.T) {
		val, idx := ArrayMax[float64]([]float64{1, 2, 3, 4, 5})
		require.Equal(t, float64(5), val)
		require.Equal(t, 4, idx)
	})
	t.Run("with_peak_middle", func(t *testing.T) {
		val, idx := ArrayMax[float64]([]float64{1, 8, 3, 2, 5})
		require.Equal(t, float64(8), val)
		require.Equal(t, 1, idx)
	})
	t.Run("all_negative", func(t *testing.T) {
		// max in all-negative slice is the least-negative
		val, idx := ArrayMax[float64]([]float64{-10, -3, -7, -1, -5})
		require.Equal(t, float64(-1), val)
		require.Equal(t, 3, idx)
	})
	t.Run("first_index_on_tie", func(t *testing.T) {
		val, idx := ArrayMax[float64]([]float64{5, 5, 3, 5})
		require.Equal(t, float64(5), val)
		require.Equal(t, 0, idx, "ties resolve to earliest index (legacy semantics)")
	})
}

// TestArrayMax_EmptyPanics 是 cross-compare review 的 footgun regression test。
// 修前 data[0] 會 panic with opaque "index out of range"；修後 panic with clear message。
func TestArrayMax_EmptyPanics(t *testing.T) {
	require.PanicsWithValue(
		t,
		"util.ArrayMax: empty slice — caller must guard len(data) > 0",
		func() { ArrayMax[float64](nil) },
	)
	require.PanicsWithValue(
		t,
		"util.ArrayMax: empty slice — caller must guard len(data) > 0",
		func() { ArrayMax[float64]([]float64{}) },
	)
}
