package util

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStr2int(t *testing.T) {
	t.Run("test 1", func(t *testing.T) {
		r, err := Str2Number[int, int]("3.70188E-05", 10)
		require.NoError(t, err)
		require.Equal(t, 370188, r)
	})
	t.Run("test 2", func(t *testing.T) {
		r, err := Str2Number[int, int]("0.001356", 6)
		require.NoError(t, err)
		require.Equal(t, 1356, r)
	})
	t.Run("test error", func(t *testing.T) {
		_, err := Str2Number[int, int]("invalid", 6)
		require.Error(t, err)
	})
}
