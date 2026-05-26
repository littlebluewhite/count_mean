package io

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/models"
)

// newFormatAwareTestHandler 建立指向 tempDir 的 CSVHandler. tempDir 同時是 OutputDir,
// 讓 WriteRequest{Filename}/WriteRequest{Filename, SubDir} 都能 resolve 到該 dir.
func newFormatAwareTestHandler(t *testing.T) (*CSVHandler, string) {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = tempDir
	cfg.OutputDir = tempDir
	cfg.OperateDir = tempDir

	return NewCSVHandler(cfg), tempDir
}

// readRows 讀檔並 split lines (BOM 視 cfg.BOMEnabled, default config 是 true).
func readRows(t *testing.T, path string) []string {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	// 剝掉 UTF-8 BOM 0xEF 0xBB 0xBF (default config BOMEnabled=true)
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}

	return strings.Split(strings.TrimSpace(string(content)), "\n")
}

// TestWriteMaxMean_RoundTrip 驗證 WriteMaxMean 寫出的檔可被讀回, 6-row layout 對齊.
//
// row layout 由 implementation 持有 — caller 不再呼叫 Convert*. 此 test 從 caller 視角
// 直接驗 round-trip: 給 results -> WriteMaxMean -> ReadFile -> 比對行數與標籤.
func TestWriteMaxMean_RoundTrip(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	headers := []string{"Time", "Ch1", "Ch2"}
	results := []models.MaxMeanResult{
		{ColumnIndex: 1, StartTime: 1.0, EndTime: 2.0, MaxMean: 100.0},
		{ColumnIndex: 2, StartTime: 1.5, EndTime: 2.5, MaxMean: 200.0},
	}

	err := handler.WriteMaxMean(
		WriteRequest{Filename: "max_mean.csv"},
		headers, results, 0.5, 3.0,
	)
	require.NoError(t, err)

	lines := readRows(t, filepath.Join(tempDir, "max_mean.csv"))
	require.Len(t, lines, 6, "MaxMean 應產生 6 row (header + 5 data rows)")
	require.Equal(t, "Time,Ch1,Ch2", lines[0])
	require.Contains(t, lines[1], "開始範圍秒數")
	require.Contains(t, lines[2], "結束範圍秒數")
	require.Contains(t, lines[3], "開始計算秒數")
	require.Contains(t, lines[4], "結束計算秒數")
	require.Contains(t, lines[5], "最大平均值")
}

// TestWriteMaxMean_SubDir 驗證 WriteRequest.SubDir 自動 mkdir 並寫到子目錄.
func TestWriteMaxMean_SubDir(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	headers := []string{"Time", "Ch1"}
	results := []models.MaxMeanResult{
		{ColumnIndex: 1, StartTime: 0.1, EndTime: 0.5, MaxMean: 42.0},
	}

	err := handler.WriteMaxMean(
		WriteRequest{Filename: "batch.csv", SubDir: "run_001"},
		headers, results, 0.0, 1.0,
	)
	require.NoError(t, err)

	subPath := filepath.Join(tempDir, "run_001", "batch.csv")
	_, err = os.Stat(subPath)
	require.NoError(t, err, "SubDir 路徑應該存在")
}

// TestWriteNormalized_RoundTrip 驗證 WriteNormalized 對 EMGDataset 寫出 1+N row.
func TestWriteNormalized_RoundTrip(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	dataset := &models.EMGDataset{
		Headers:               []string{"Time", "Ch1", "Ch2"},
		OriginalTimePrecision: 2,
		Data: []models.EMGData{
			{Time: 0.1, Channels: []float64{1.0, 2.0}},
			{Time: 0.2, Channels: []float64{1.5, 2.5}},
		},
	}

	err := handler.WriteNormalized(
		WriteRequest{Filename: "norm.csv"}, dataset,
	)
	require.NoError(t, err)

	lines := readRows(t, filepath.Join(tempDir, "norm.csv"))
	require.Len(t, lines, 3, "Normalize 應產生 header + 2 data row")
	require.Equal(t, "Time,Ch1,Ch2", lines[0])
}

// TestWritePhaseAnalysis_SinglePhase 驗證單 phase 4-row layout (header + max + mean + time).
func TestWritePhaseAnalysis_SinglePhase(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	headers := []string{"Time", "Ch1", "Ch2"}
	result := &calculator.AnalyzeResult{
		PhaseResults: []models.PhaseAnalysisResult{
			{
				PhaseName:  "Stance",
				MaxValues:  map[int]float64{0: 100.0, 1: 50.0},
				MeanValues: map[int]float64{0: 80.0, 1: 40.0},
			},
		},
		MaxTimeIndex: map[int]float64{0: 1.5, 1: 1.8},
	}

	err := handler.WritePhaseAnalysis(
		WriteRequest{Filename: "phase_single.csv"}, headers, result,
	)
	require.NoError(t, err)

	lines := readRows(t, filepath.Join(tempDir, "phase_single.csv"))
	require.Len(t, lines, 4, "single phase 含 MaxTimeIndex 應產生 4 row")
	require.Contains(t, lines[1], "Stance 最大值")
	require.Contains(t, lines[2], "Stance 平均值")
	require.Contains(t, lines[3], "整個階段最大值出現在_秒")
}

// TestWritePhaseAnalysis_MultiPhase 是這次 grilling 的核心 leverage 驗證 —
// multi-phase merge + time-index dedup 由 implementation 吸收, caller 不再 skip row.
//
// 期望 row layout: [header, p1.max, p1.mean, p2.max, p2.mean, p3.max, p3.mean, time-index]
// = 1 + 2*3 + 1 = 8 row. Header 只出現一次, time-index row 也只出現一次.
func TestWritePhaseAnalysis_MultiPhase(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	headers := []string{"Time", "Ch1", "Ch2"}
	result := &calculator.AnalyzeResult{
		PhaseResults: []models.PhaseAnalysisResult{
			{PhaseName: "Stance", MaxValues: map[int]float64{0: 100, 1: 50}, MeanValues: map[int]float64{0: 80, 1: 40}},
			{PhaseName: "Swing", MaxValues: map[int]float64{0: 200, 1: 100}, MeanValues: map[int]float64{0: 150, 1: 80}},
			{PhaseName: "Stand", MaxValues: map[int]float64{0: 50, 1: 25}, MeanValues: map[int]float64{0: 40, 1: 20}},
		},
		MaxTimeIndex: map[int]float64{0: 1.5, 1: 1.8},
	}

	err := handler.WritePhaseAnalysis(
		WriteRequest{Filename: "phase_multi.csv"}, headers, result,
	)
	require.NoError(t, err)

	lines := readRows(t, filepath.Join(tempDir, "phase_multi.csv"))
	require.Len(t, lines, 8, "3 phase + time-index 應該是 1 header + 2*3 + 1 = 8 row")
	require.Equal(t, "Time,Ch1,Ch2", lines[0])
	require.Contains(t, lines[1], "Stance 最大值")
	require.Contains(t, lines[2], "Stance 平均值")
	require.Contains(t, lines[3], "Swing 最大值")
	require.Contains(t, lines[4], "Swing 平均值")
	require.Contains(t, lines[5], "Stand 最大值")
	require.Contains(t, lines[6], "Stand 平均值")
	require.Contains(t, lines[7], "整個階段最大值出現在_秒")

	// header 不應在 phase 2/3 之間重複出現 — caller 端 phaseRows[1:] dedup 邏輯已內化
	headerCount := 0
	for _, line := range lines {
		if line == "Time,Ch1,Ch2" {
			headerCount++
		}
	}
	require.Equal(t, 1, headerCount, "header 只應出現 1 次")
}

// TestWritePhaseAnalysis_NoTimeIndex 驗證 MaxTimeIndex 為 nil/空時不附 time-index row.
func TestWritePhaseAnalysis_NoTimeIndex(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	headers := []string{"Time", "Ch1"}
	result := &calculator.AnalyzeResult{
		PhaseResults: []models.PhaseAnalysisResult{
			{PhaseName: "P1", MaxValues: map[int]float64{0: 100}, MeanValues: map[int]float64{0: 80}},
		},
		MaxTimeIndex: nil,
	}

	err := handler.WritePhaseAnalysis(
		WriteRequest{Filename: "phase_no_idx.csv"}, headers, result,
	)
	require.NoError(t, err)

	lines := readRows(t, filepath.Join(tempDir, "phase_no_idx.csv"))
	require.Len(t, lines, 3, "MaxTimeIndex 為 nil 時應只有 header + max + mean")
}

// TestWritePhaseAnalysis_Empty 驗證空 PhaseResults 回明確 error 而非吞 nil 寫空檔.
func TestWritePhaseAnalysis_Empty(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	err := handler.WritePhaseAnalysis(
		WriteRequest{Filename: "empty.csv"},
		[]string{"Time", "Ch1"},
		&calculator.AnalyzeResult{},
	)
	require.ErrorIs(t, err, errEmptyPhaseAnalysis)
}

// TestWritePhaseAnalysis_NilResult 驗證 nil result 也走同樣 fail-fast.
func TestWritePhaseAnalysis_NilResult(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	err := handler.WritePhaseAnalysis(
		WriteRequest{Filename: "nil.csv"},
		[]string{"Time", "Ch1"},
		nil,
	)
	require.ErrorIs(t, err, errEmptyPhaseAnalysis)
}
