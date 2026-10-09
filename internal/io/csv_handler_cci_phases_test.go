package io

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/cci"
)

// TestWriteCCIPhasesResult_ContentRoundTrip 驗證 WriteCCIPhasesResult 的 header
// layout、interval row (HasTime=false)、point row (HasTime=true)、NaN value → ""。
func TestWriteCCIPhasesResult_ContentRoundTrip(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	payload := &cci.CCIAnalysisResult{
		Subject:     "NSF1",
		PairResults: []cci.CCIResult{{PairName: "RA/ES"}, {PairName: "IL/GMax"}},
		PhaseStats: []cci.CCIPhaseStatRow{
			// interval row — no time
			{Item: "IC→LR", Metric: "mean", HasTime: false, Values: []float64{0.123456, 0.654321}},
			// point row — has time
			{Item: "P0", Metric: "midpoint", HasTime: true, Time: 10.633, Values: []float64{0.111111, 0.222222}},
			// NaN in second value
			{Item: "P1", Metric: "midpoint", HasTime: true, Time: 20.0, Values: []float64{0.333333, math.NaN()}},
		},
	}

	outputPath, err := handler.WriteCCIPhasesResult(context.Background(), WriteRequest{}, payload)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "NSF1_CCI_Rudolph_phases.csv"), outputPath)

	rows := readCSVRows(t, outputPath)
	require.Len(t, rows, 4, "1 header + 3 data rows")

	// header
	assert.Equal(t, []string{"項目", "指標", "Time (s)", "RA/ES", "IL/GMax"}, rows[0])

	// interval row: Time cell empty
	assert.Equal(t, "IC→LR", rows[1][0])
	assert.Equal(t, "mean", rows[1][1])
	assert.Equal(t, "", rows[1][2], "interval row Time cell must be empty")
	assert.Equal(t, fmt.Sprintf("%.6f", 0.123456), rows[1][3])
	assert.Equal(t, fmt.Sprintf("%.6f", 0.654321), rows[1][4])

	// point row: Time cell "10.6330"
	assert.Equal(t, "P0", rows[2][0])
	assert.Equal(t, "midpoint", rows[2][1])
	assert.Equal(t, "10.6330", rows[2][2], "point row Time cell must be %.4f")
	assert.Equal(t, fmt.Sprintf("%.6f", 0.111111), rows[2][3])
	assert.Equal(t, fmt.Sprintf("%.6f", 0.222222), rows[2][4])

	// NaN value → empty string
	assert.Equal(t, "P1", rows[3][0])
	assert.Equal(t, "20.0000", rows[3][2])
	assert.Equal(t, fmt.Sprintf("%.6f", 0.333333), rows[3][3])
	assert.Equal(t, "", rows[3][4], "NaN value cell must be empty")
}

// TestWriteCCIPhasesResult_EmptyRows 驗證 len(Rows)==0 → errEmptyCCIPhasesPayload。
func TestWriteCCIPhasesResult_EmptyRows(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	payload := &cci.CCIAnalysisResult{
		Subject:     "subj",
		PairResults: []cci.CCIResult{{PairName: "P1"}},
		PhaseStats:  nil,
	}

	_, err := handler.WriteCCIPhasesResult(context.Background(), WriteRequest{}, payload)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errEmptyCCIPhasesPayload),
		"expected errEmptyCCIPhasesPayload, got: %v", err)
}

// TestWriteCCIPhasesResult_NilResult 驗證 result == nil → errEmptyCCIPhasesPayload
// (A2 pointer 簽名新增的分支,ADR-0025)。
func TestWriteCCIPhasesResult_NilResult(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	_, err := handler.WriteCCIPhasesResult(context.Background(), WriteRequest{}, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errEmptyCCIPhasesPayload),
		"expected errEmptyCCIPhasesPayload for nil result, got: %v", err)
}
