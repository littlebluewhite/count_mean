package io

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMuscleRatioOutputAll_RoundTrip 驗證 writer → reader 還原 Times / PairLabels / Ratios,
// NaN/Inf(writer 寫空 cell)讀回為 NaN。
func TestMuscleRatioOutputAll_RoundTrip(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)
	in := MuscleRatioOutputAllPayload{
		Subject:    "s1",
		PairLabels: []string{"RA/ES", "=evil"},
		Times:      []float64{0.0, 0.0123, 1.0},
		Ratios: [][]float64{
			{0.1, math.NaN(), 0.3},
			{0.4, 0.5, math.Inf(1)},
		},
	}
	path, err := handler.WriteMuscleRatioOutputAll(WriteRequest{}, in)
	require.NoError(t, err)

	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close() //nolint:errcheck // test read-only fd

	got, err := ReadMuscleRatioOutputAll(f)
	require.NoError(t, err)
	assert.Equal(t, in.PairLabels, got.PairLabels)
	assert.Equal(t, in.Times, got.Times)
	require.Len(t, got.Ratios, 2)
	assert.InDelta(t, 0.1, got.Ratios[0][0], 1e-9)
	assert.True(t, math.IsNaN(got.Ratios[0][1]))
	assert.InDelta(t, 0.3, got.Ratios[0][2], 1e-9)
	assert.InDelta(t, 0.5, got.Ratios[1][1], 1e-9)
	assert.True(t, math.IsNaN(got.Ratios[1][2]), "Inf 被 writer 寫成空 cell → NaN")
}

func TestReadMuscleRatioOutputAll_Malformed(t *testing.T) {
	t.Parallel()

	_, err := ReadMuscleRatioOutputAll(strings.NewReader("Time (s),A\n"))
	require.ErrorIs(t, err, ErrMuscleRatioCSVEmpty)

	_, err = ReadMuscleRatioOutputAll(strings.NewReader("Time (s)\n0.0\n"))
	require.ErrorIs(t, err, ErrMuscleRatioCSVNoHeader)

	got, err := ReadMuscleRatioOutputAll(strings.NewReader("Time (s),A\n0.0,1\nbad,2\n0.5\n1.0,3\n"))
	require.NoError(t, err)
	assert.Equal(t, []float64{0.0, 1.0}, got.Times, "不可解析 Time / jagged row 被 skip")
}
