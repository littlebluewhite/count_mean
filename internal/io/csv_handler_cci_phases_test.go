package io

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/cci"
	"count_mean/internal/csvutil"
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

// TestWriteCCIPhasesResult_BOMPresent 驗證寫出的檔案開頭有 UTF-8 BOM。
func TestWriteCCIPhasesResult_BOMPresent(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	payload := &cci.CCIAnalysisResult{
		Subject:     "subj",
		PairResults: []cci.CCIResult{{PairName: "P1"}},
		PhaseStats: []cci.CCIPhaseStatRow{
			{Item: "IC", Metric: "mean", HasTime: false, Values: []float64{1.0}},
		},
	}

	outputPath, err := handler.WriteCCIPhasesResult(context.Background(), WriteRequest{}, payload)
	require.NoError(t, err)

	content, readErr := os.ReadFile(outputPath)
	require.NoError(t, readErr)
	require.True(t, len(content) >= 3, "file too short to contain BOM")
	assert.Equal(t, csvutil.BOMBytes(), content[:3], "first 3 bytes must be UTF-8 BOM")
}

// TestWriteCCIPhasesResult_NoTmpResidue 驗證成功寫出後輸出目錄沒有殘留 .tmp. 檔。
func TestWriteCCIPhasesResult_NoTmpResidue(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	payload := &cci.CCIAnalysisResult{
		Subject:     "subj",
		PairResults: []cci.CCIResult{{PairName: "P1"}},
		PhaseStats: []cci.CCIPhaseStatRow{
			{Item: "IC", Metric: "mean", HasTime: false, Values: []float64{1.0}},
		},
	}

	_, err := handler.WriteCCIPhasesResult(context.Background(), WriteRequest{}, payload)
	require.NoError(t, err)

	entries, readErr := os.ReadDir(tempDir)
	require.NoError(t, readErr)
	for _, e := range entries {
		assert.False(t, strings.Contains(e.Name(), ".tmp."),
			"no stray tmp file should remain in output dir, found: %s", e.Name())
	}
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

// TestWriteCCIPhasesResult_PathTraversalRejected 驗證 SubDir 含 traversal 或絕對路徑時
// writer 拒絕並返回 error,不寫入任何檔案。
func TestWriteCCIPhasesResult_PathTraversalRejected(t *testing.T) {
	t.Parallel()

	traversalCases := []struct {
		name   string
		subDir string
	}{
		{"relative_traversal", "../evil"},
		{"absolute_path", "/etc"},
		{"deep_traversal", "../../etc"},
	}

	row := cci.CCIPhaseStatRow{
		Item: "IC", Metric: "mean", HasTime: false, Values: []float64{1.0},
	}
	payload := &cci.CCIAnalysisResult{
		Subject:     "subj",
		PairResults: []cci.CCIResult{{PairName: "P1"}},
		PhaseStats:  []cci.CCIPhaseStatRow{row},
	}

	for _, tc := range traversalCases {
		t.Run(tc.name, func(t *testing.T) {
			handler, _ := newFormatAwareTestHandler(t)
			_, err := handler.WriteCCIPhasesResult(
				context.Background(),
				WriteRequest{SubDir: tc.subDir},
				payload,
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "輸出路徑",
				"error should mention 輸出路徑 for subDir=%q", tc.subDir)
		})
	}
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
