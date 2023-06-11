package util

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestMean(t *testing.T) {
	t.Run("test 1", func(t *testing.T) {
		r := ArrayMean([]float64{1, 2, 3, 4, 5})
		require.Equal(t, float64(3), r)
	})
	t.Run("test 2", func(t *testing.T) {
		r := ArrayMean([]float64{1, 2, 3, 8, 5})
		require.Equal(t, 3.8, r)
	})
}
