package io

import (
	"context"
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/calculator"
	"count_mean/internal/cci"
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

	outputPath, err := handler.WriteMaxMean(
		WriteRequest{Filename: "max_mean.csv"},
		headers, results, 0.5, 3.0,
	)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tempDir, "max_mean.csv"), outputPath)

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

	_, err := handler.WriteMaxMean(
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

	outputPath, err := handler.WriteNormalized(
		WriteRequest{Filename: "norm.csv"}, dataset,
	)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tempDir, "norm.csv"), outputPath)

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

	outputPath, err := handler.WritePhaseAnalysis(
		WriteRequest{Filename: "phase_single.csv"}, headers, result,
	)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tempDir, "phase_single.csv"), outputPath)

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

	_, err := handler.WritePhaseAnalysis(
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

	_, err := handler.WritePhaseAnalysis(
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

	_, err := handler.WritePhaseAnalysis(
		WriteRequest{Filename: "empty.csv"},
		[]string{"Time", "Ch1"},
		&calculator.AnalyzeResult{},
	)
	require.ErrorIs(t, err, errEmptyPhaseAnalysis)
}

// TestWriteNormalizedPhaseSyncResult_NilStats 驗證 nil stats 回 errEmptyPhaseSyncResult。
func TestWriteNormalizedPhaseSyncResult_NilStats(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	_, err := handler.WriteNormalizedPhaseSyncResult(WriteRequest{}, nil,
		models.PhaseP0, models.PhaseL)
	require.ErrorIs(t, err, errEmptyPhaseSyncResult)
}

// TestWriteNormalizedPhaseSyncResult_FilenameTemplate 驗證 filename 包含 norm/stats 兩窗口資訊。
func TestWriteNormalizedPhaseSyncResult_FilenameTemplate(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	stats := &models.EMGStatistics{
		Subject:      "testSubject",
		StartPhase:   models.PhaseP1,
		EndPhase:     models.PhaseC,
		StartTime:    0.0,
		EndTime:      2.0,
		ChannelNames: []string{"Ch1"},
		ChannelMeans: map[string]float64{"Ch1": 1.0},
		ChannelMaxes: map[string]float64{"Ch1": 2.0},
	}

	outputPath, err := handler.WriteNormalizedPhaseSyncResult(WriteRequest{}, stats,
		models.PhaseP0, models.PhaseL)
	require.NoError(t, err)

	base := filepath.Base(outputPath)
	require.Contains(t, base, "_normalized_norm-P0-L_stats-P1-C.csv",
		"filename must embed norm and stats windows")
	require.True(t, strings.HasPrefix(outputPath, tempDir),
		"outputPath must stay inside tempDir")
}

// TestWriteNormalizedPhaseSyncResult_RoundTrip 驗證 8-row layout 與 ConvertPhaseSyncResult 完全對應。
func TestWriteNormalizedPhaseSyncResult_RoundTrip(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	stats := &models.EMGStatistics{
		Subject:      "subject_norm",
		StartPhase:   models.PhaseS,
		StartTime:    1.0,
		EndPhase:     models.PhaseT,
		EndTime:      3.0,
		ChannelNames: []string{"RA", "ES"},
		ChannelMeans: map[string]float64{"RA": 0.11, "ES": 0.22},
		ChannelMaxes: map[string]float64{"RA": 1.1, "ES": 2.2},
	}

	outputPath, err := handler.WriteNormalizedPhaseSyncResult(WriteRequest{}, stats,
		models.PhaseP0, models.PhaseL)
	require.NoError(t, err)

	rows := readCSVRows(t, outputPath)
	expected := newCSVConverter(1.0, 4).ConvertPhaseSyncResult(stats)
	require.Len(t, rows, len(expected), "row count must match ConvertPhaseSyncResult")
	for i, row := range rows {
		require.Equal(t, expected[i], row, "row %d mismatch", i)
	}
}

// TestWriteNormalizedPhaseSyncResult_SubDir 驗證 req.SubDir 自動 mkdir 並寫到子目錄。
func TestWriteNormalizedPhaseSyncResult_SubDir(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	stats := &models.EMGStatistics{
		Subject:      "subj_norm",
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseL,
		StartTime:    0.0,
		EndTime:      1.0,
		ChannelNames: []string{"Ch1"},
		ChannelMeans: map[string]float64{"Ch1": 1.0},
		ChannelMaxes: map[string]float64{"Ch1": 2.0},
	}

	outputPath, err := handler.WriteNormalizedPhaseSyncResult(
		WriteRequest{SubDir: "run_norm"}, stats, models.PhaseP0, models.PhaseL)
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(outputPath, filepath.Join(tempDir, "run_norm")),
		"outputPath should be inside SubDir")
	_, err = os.Stat(outputPath)
	require.NoError(t, err, "SubDir path should exist (auto mkdir)")
}

// TestWriteNormalizedPhaseSyncResult_SubjectSanitization 驗證 Subject 中的危險字元不出現在 filename 中。
func TestWriteNormalizedPhaseSyncResult_SubjectSanitization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		subject string
		mustNot []rune
	}{
		{name: "rtl_override", subject: "evil\u202egsp.csv", mustNot: []rune{0x202E}},
		{name: "null_byte", subject: "Sub\x00ject", mustNot: []rune{0x00}},
		{name: "zero_width_space", subject: "Sub\u200bject", mustNot: []rune{0x200B}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler, tempDir := newFormatAwareTestHandler(t)

			stats := &models.EMGStatistics{
				Subject:      tc.subject,
				StartPhase:   models.PhaseP0,
				EndPhase:     models.PhaseL,
				StartTime:    0.0,
				EndTime:      1.0,
				ChannelNames: []string{"Ch1"},
				ChannelMeans: map[string]float64{"Ch1": 1.0},
				ChannelMaxes: map[string]float64{"Ch1": 2.0},
			}

			outputPath, err := handler.WriteNormalizedPhaseSyncResult(WriteRequest{}, stats,
				models.PhaseP0, models.PhaseL)
			require.NoError(t, err)

			base := filepath.Base(outputPath)
			for _, ch := range tc.mustNot {
				require.NotContainsf(t, base, string(ch),
					"output filename %q still contains forbidden rune U+%04X (subject=%q)",
					base, ch, tc.subject)
			}

			cleaned := filepath.Clean(outputPath)
			require.True(t, strings.HasPrefix(cleaned, tempDir),
				"sanitized output path %q must stay inside %q", cleaned, tempDir)

			_, statErr := os.Stat(outputPath)
			require.NoError(t, statErr)
		})
	}
}

// TestWriteNormalizedPhaseSyncResult_NoStrayTmpFiles 驗證 happy-path 不留任何 tmp orphan。
func TestWriteNormalizedPhaseSyncResult_NoStrayTmpFiles(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	stats := &models.EMGStatistics{
		Subject:      "clean_subj",
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseL,
		StartTime:    0.0,
		EndTime:      1.0,
		ChannelNames: []string{"Ch1"},
		ChannelMeans: map[string]float64{"Ch1": 1.0},
		ChannelMaxes: map[string]float64{"Ch1": 2.0},
	}

	_, err := handler.WriteNormalizedPhaseSyncResult(WriteRequest{}, stats,
		models.PhaseP0, models.PhaseL)
	require.NoError(t, err)

	entries, readErr := os.ReadDir(tempDir)
	require.NoError(t, readErr)

	for _, e := range entries {
		require.False(t, strings.Contains(e.Name(), ".tmp."),
			"no stray tmp file should remain in output dir, found: %s", e.Name())
	}
}

// TestWritePhaseAnalysis_NilResult 驗證 nil result 也走同樣 fail-fast.
func TestWritePhaseAnalysis_NilResult(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	_, err := handler.WritePhaseAnalysis(
		WriteRequest{Filename: "nil.csv"},
		[]string{"Time", "Ch1"},
		nil,
	)
	require.ErrorIs(t, err, errEmptyPhaseAnalysis)
}

// TestWritePhaseSyncResult_NilStats 驗證 nil stats 走 fail-fast,
// 與 WritePhaseAnalysis_NilResult 的對稱契約。
func TestWritePhaseSyncResult_NilStats(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	outputPath, err := handler.WritePhaseSyncResult(WriteRequest{}, nil)
	require.ErrorIs(t, err, errEmptyPhaseSyncResult)
	require.Empty(t, outputPath, "失敗時 outputPath 必為空")
}

// TestWritePhaseSyncResult_SubjectSanitization 釘住:WritePhaseSyncResult 把
// stats.Subject 交給 calculator.GenerateOutputFileName -> filename.Sanitize,
// Unicode 控制 / 雙向書寫覆寫 (U+202E) / NUL / ZWSP / 各 bidi isolation marker /
// CRLF 等不會落到實際 filename。原 TestExportResults_SubjectWithRTLAndControl_FilenameSanitized
// 在 phase_sync 套件以 ExportResults 路徑釘同一份契約; ADR-0001 把寫檔職責搬到 csvHandler 後
// 同份契約改在 io 套件以 WritePhaseSyncResult 路徑釘住。
//
// Unicode literals 一律走 \uXXXX 表記 (避免 editor 真的渲染 RTL override 干擾 review)。
func TestWritePhaseSyncResult_SubjectSanitization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		subject string
		mustNot []rune
	}{
		{name: "rtl_override", subject: "evil\u202egsp.csv", mustNot: []rune{0x202E}},
		{name: "null_byte", subject: "Sub\x00ject", mustNot: []rune{0x00}},
		{name: "ascii_bell", subject: "Sub\x07ject", mustNot: []rune{0x07}},
		{name: "zero_width_space", subject: "Sub\u200bject", mustNot: []rune{0x200B}},
		{name: "bidi_iso_pop", subject: "Sub\u2068x\u2069ject", mustNot: []rune{0x2068, 0x2069}},
		{name: "crlf_in_subject", subject: "Sub\r\nject", mustNot: []rune{0x0D, 0x0A}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler, tempDir := newFormatAwareTestHandler(t)

			stats := &models.EMGStatistics{
				Subject:      tc.subject,
				StartPhase:   models.PhaseP0,
				EndPhase:     models.PhaseP2,
				StartTime:    0.0,
				EndTime:      2.0,
				ChannelNames: []string{"Ch1"},
				ChannelMeans: map[string]float64{"Ch1": 1.0},
				ChannelMaxes: map[string]float64{"Ch1": 2.0},
			}

			outputPath, err := handler.WritePhaseSyncResult(WriteRequest{}, stats)
			require.NoError(t, err)

			base := filepath.Base(outputPath)
			for _, ch := range tc.mustNot {
				require.NotContainsf(t, base, string(ch),
					"output filename %q still contains forbidden rune U+%04X (subject=%q)",
					base, ch, tc.subject)
			}

			// sanitize 後組合路徑不會逸出 tempDir。
			cleaned := filepath.Clean(outputPath)
			require.True(t, strings.HasPrefix(cleaned, tempDir),
				"sanitized output path %q must stay inside %q", cleaned, tempDir)

			// 檔案真的被寫到清理後的路徑。
			_, statErr := os.Stat(outputPath)
			require.NoError(t, statErr)
		})
	}
}

// TestWritePhaseSyncResult_SubDir 驗證 req.SubDir 自動 mkdir 並寫到子目錄,
// 回傳的 outputPath 反映 OutputDir/SubDir/<auto-filename>。
func TestWritePhaseSyncResult_SubDir(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	stats := &models.EMGStatistics{
		Subject:      "subj_subdir",
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseP2,
		StartTime:    0.0,
		EndTime:      1.0,
		ChannelNames: []string{"Ch1"},
		ChannelMeans: map[string]float64{"Ch1": 1.0},
		ChannelMaxes: map[string]float64{"Ch1": 2.0},
	}

	outputPath, err := handler.WritePhaseSyncResult(WriteRequest{SubDir: "run_42"}, stats)
	require.NoError(t, err)

	expectedFilename := calculator.GenerateOutputFileName(
		stats.Subject, stats.StartPhase, stats.EndPhase,
	)
	require.Equal(t, filepath.Join(tempDir, "run_42", expectedFilename), outputPath,
		"outputPath 應包含 SubDir segment")

	_, err = os.Stat(outputPath)
	require.NoError(t, err, "SubDir 路徑應該存在 (自動 mkdir)")
}

// TestWritePhaseSyncResult_RoundTrip 釘住 ADR-0001 invariant:
// PhaseSync 的 8-row layout 跟自動 filename 由 CSVHandler 持有,
// caller 只給 stats 跟 WriteRequest (SubDir 可選); 回傳 outputPath。
//
// Row layout (8-row PhaseSync layout:
// header + 開始分期點/時間 + 結束分期點/時間 + 時間差值 + 平均值 + 最大值):
//
//	row 0: header (空格 + channel names)
//	row 1: 開始分期點
//	row 2: 開始時間
//	row 3: 結束分期點
//	row 4: 結束時間
//	row 5: 時間差值
//	row 6: 平均值
//	row 7: 最大值
//
// Filename = calculator.GenerateOutputFileName(stats.Subject, StartPhase, EndPhase).
func TestWritePhaseSyncResult_RoundTrip(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	stats := &models.EMGStatistics{
		Subject:      "subject_01",
		StartPhase:   models.PhaseP0,
		StartTime:    1.0,
		EndPhase:     models.PhaseL,
		EndTime:      2.5,
		ChannelNames: []string{"RA", "ES"},
		ChannelMeans: map[string]float64{"RA": 0.123456, "ES": 0.234567},
		ChannelMaxes: map[string]float64{"RA": 1.5, "ES": 2.0},
	}

	outputPath, err := handler.WritePhaseSyncResult(WriteRequest{}, stats)
	require.NoError(t, err)

	// outputPath 應反映自動生成 filename (含 subject + phase range + _statistics.csv)。
	expectedFilename := calculator.GenerateOutputFileName(
		stats.Subject, stats.StartPhase, stats.EndPhase,
	)
	require.Equal(t, filepath.Join(tempDir, expectedFilename), outputPath,
		"outputPath 應為 OutputDir/<auto-generated filename>")

	lines := readRows(t, outputPath)
	require.Len(t, lines, 8, "PhaseSync 8-row layout (header + 5 metadata + 平均值 + 最大值)")
	require.Contains(t, lines[0], "RA", "header 應含 channel name")
	require.Contains(t, lines[0], "ES")
	require.Contains(t, lines[1], "開始分期點")
	require.Contains(t, lines[1], "P0", "開始分期點 row 應含 StartPhase 值")
	require.Contains(t, lines[2], "開始時間")
	require.Contains(t, lines[3], "結束分期點")
	require.Contains(t, lines[3], "L", "結束分期點 row 應含 EndPhase 值")
	require.Contains(t, lines[4], "結束時間")
	require.Contains(t, lines[5], "時間差值")
	require.Contains(t, lines[6], "平均值")
	require.Contains(t, lines[7], "最大值")
}

// readCSVRows 讀檔、剝 BOM，並用 encoding/csv 解析為 [][]string,供 CCI round-trip 精確比對。
func readCSVRows(t *testing.T, path string) [][]string {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	// 剝掉 UTF-8 BOM 0xEF 0xBB 0xBF
	if len(content) >= 3 && content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF {
		content = content[3:]
	}

	r := csv.NewReader(strings.NewReader(string(content)))
	rows, err := r.ReadAll()
	require.NoError(t, err)

	return rows
}

// TestWriteCCIResult_RoundTrip 驗證 WriteCCIResult 寫出的檔可被讀回,
// row layout (Time / Gait Cycle / N pair columns) 對齊。
func TestWriteCCIResult_RoundTrip(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	result := &cci.CCIAnalysisResult{
		Subject:       "subj_A",
		GaitStartTime: 0.0,
		GaitEndTime:   1.0,
		TimeValues:    []float64{0.0, 0.5, 1.0},
		PairResults: []cci.CCIResult{
			{PairName: "P1_P2", Values: []float64{0.1, 0.2, 0.3}},
			{PairName: "P3_P4", Values: []float64{0.4, 0.5, 0.6}},
		},
	}

	outputPath, err := handler.WriteCCIResult(context.Background(), WriteRequest{}, result)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "subj_A_CCI_Rudolph.csv"), outputPath)

	rows := readCSVRows(t, outputPath)
	require.Len(t, rows, 4) // 1 header + 3 data rows
	assert.Equal(t, []string{"Time (s)", "Gait Cycle (%)", "P1_P2", "P3_P4"}, rows[0])
	assert.Equal(t, []string{"0.0000", "0.00", "0.100000", "0.400000"}, rows[1])
}

// TestWriteCCIResult_NaNRowSkip 驗證全 NaN/Inf 的 row 被跳過。
func TestWriteCCIResult_NaNRowSkip(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	nan := math.NaN()
	result := &cci.CCIAnalysisResult{
		Subject:       "subj_B",
		GaitStartTime: 0.0,
		GaitEndTime:   1.0,
		TimeValues:    []float64{0.0, 0.5, 1.0},
		PairResults: []cci.CCIResult{
			{PairName: "P1", Values: []float64{0.1, nan, 0.3}},
			{PairName: "P2", Values: []float64{0.2, nan, 0.4}},
		},
	}

	outputPath, err := handler.WriteCCIResult(context.Background(), WriteRequest{}, result)
	require.NoError(t, err)

	rows := readCSVRows(t, outputPath)
	require.Len(t, rows, 3) // header + 2 surviving rows (row i=1 全 NaN 被跳過)
}

// TestWriteCCIResult_CtxCancel 驗證 pre-cancel 不寫檔。
func TestWriteCCIResult_CtxCancel(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := &cci.CCIAnalysisResult{
		Subject:       "subj_C",
		GaitStartTime: 0.0,
		GaitEndTime:   1.0,
		TimeValues:    []float64{0.0},
		PairResults:   []cci.CCIResult{{PairName: "P1", Values: []float64{0.1}}},
	}

	_, err := handler.WriteCCIResult(ctx, WriteRequest{}, result)
	require.ErrorIs(t, err, context.Canceled)

	_, statErr := os.Stat(filepath.Join(tempDir, "subj_C_CCI_Rudolph.csv"))
	require.True(t, os.IsNotExist(statErr), "pre-cancel 不應留下檔案")
}

// TestWriteMuscleRatioOutputAll_RoundTrip 驗證 Output 1 (per-subject full
// time-series ratio) 的 round-trip + filename derivation.
func TestWriteMuscleRatioOutputAll_RoundTrip(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	payload := MuscleRatioOutputAllPayload{
		Subject:    "s1",
		PairLabels: []string{"R1", "R2"},
		Times:      []float64{0.0, 0.5, 1.0},
		Ratios:     [][]float64{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}},
	}

	outputPath, err := handler.WriteMuscleRatioOutputAll(WriteRequest{}, payload)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "s1_muscle_ratio.csv"), outputPath)

	rows := readCSVRows(t, outputPath)
	require.Len(t, rows, 4)
	assert.Equal(t, []string{"Time (s)", "R1", "R2"}, rows[0])
	assert.Equal(t, []string{"0.0000", "0.100000", "0.400000"}, rows[1])
}

// TestWriteMuscleRatioOutputPhases_RoundTrip 驗證 Output 2 round-trip.
// handler 純 layout emit — PairLabels 無後綴(後綴由 Analyzer 負責),
// Values 直接 emit(不 reach-in Times/Ratios)。(ADR-0014)
func TestWriteMuscleRatioOutputPhases_RoundTrip(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	payload := MuscleRatioOutputPhasesPayload{
		Subject:    "s1",
		PairLabels: []string{"R1"},
		Points: []MuscleRatioPhasePoint{
			{Name: "P1", Time: 0.5, Values: []float64{0.2}},
		},
	}

	outputPath, err := handler.WriteMuscleRatioOutputPhases(WriteRequest{}, payload)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "s1_muscle_ratio_phases_avg11.csv"), outputPath)

	rows := readCSVRows(t, outputPath)
	require.Len(t, rows, 2)
	assert.Equal(t, []string{"Phase", "Time (s)", "R1"}, rows[0])
	assert.Equal(t, []string{"P1", "0.5000", "0.200000"}, rows[1])
}

// TestWriteMuscleRatioOutputAll_NaNInfCell 驗證 NaN/Inf cell → 空字串.
func TestWriteMuscleRatioOutputAll_NaNInfCell(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	payload := MuscleRatioOutputAllPayload{
		Subject:    "s2",
		PairLabels: []string{"R1"},
		Times:      []float64{0.0, 1.0},
		Ratios:     [][]float64{{math.NaN(), math.Inf(1)}},
	}

	outputPath, err := handler.WriteMuscleRatioOutputAll(WriteRequest{}, payload)
	require.NoError(t, err)

	rows := readCSVRows(t, outputPath)
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"0.0000", ""}, rows[1])
	assert.Equal(t, []string{"1.0000", ""}, rows[2])
}

// TestWriteMuscleRatioOutputPhases_EmptyPointsRejected 驗證 Points 為空時提早回
// errEmptyMuscleRatioPayload(ADR-0014:Times/Ratios 已移除,guard 改為 Points 空檢)。
func TestWriteMuscleRatioOutputPhases_EmptyPointsRejected(t *testing.T) {
	handler, _ := newFormatAwareTestHandler(t)

	_, err := handler.WriteMuscleRatioOutputPhases(WriteRequest{}, MuscleRatioOutputPhasesPayload{
		Subject:    "empty_points",
		PairLabels: []string{"R1"},
		Points:     []MuscleRatioPhasePoint{}, // 故意空
	})
	require.ErrorIs(t, err, errEmptyMuscleRatioPayload)
}

// TestSafeJoinOutput_RejectsTraversal 驗證 codex review Run 2 抓的 P2 — req.SubDir
// 含 traversal ("../evil") 或絕對路徑 ("/etc") 時,3 個 direct atomic writer
// (WriteCCIResult / WriteMuscleRatioOutputAll / WriteMuscleRatioOutputPhases)
// 都要 reject,不能讓 *.csv 寫到 OutputDir 外面。
func TestSafeJoinOutput_RejectsTraversal(t *testing.T) {
	traversalCases := []struct {
		name   string
		subDir string
	}{
		{"relative_traversal", "../evil"},
		{"absolute_path", "/etc"},
		{"deep_traversal", "../../etc"},
	}

	for _, tc := range traversalCases {
		t.Run(tc.name+"/WriteCCIResult", func(t *testing.T) {
			handler, _ := newFormatAwareTestHandler(t)
			_, err := handler.WriteCCIResult(context.Background(),
				WriteRequest{SubDir: tc.subDir},
				&cci.CCIAnalysisResult{
					Subject:       "s",
					GaitStartTime: 0.0,
					GaitEndTime:   1.0,
					TimeValues:    []float64{0.0},
					PairResults:   []cci.CCIResult{{PairName: "P1", Values: []float64{0.1}}},
				})
			require.Error(t, err)
			require.Contains(t, err.Error(), "輸出路徑")
		})

		t.Run(tc.name+"/WriteMuscleRatioOutputAll", func(t *testing.T) {
			handler, _ := newFormatAwareTestHandler(t)
			_, err := handler.WriteMuscleRatioOutputAll(
				WriteRequest{SubDir: tc.subDir},
				MuscleRatioOutputAllPayload{
					Subject:    "s",
					PairLabels: []string{"R1"},
					Times:      []float64{0.0, 1.0},
					Ratios:     [][]float64{{0.1, 0.2}},
				})
			require.Error(t, err)
			require.Contains(t, err.Error(), "輸出路徑")
		})

		t.Run(tc.name+"/WriteMuscleRatioOutputPhases", func(t *testing.T) {
			handler, _ := newFormatAwareTestHandler(t)
			_, err := handler.WriteMuscleRatioOutputPhases(
				WriteRequest{SubDir: tc.subDir},
				MuscleRatioOutputPhasesPayload{
					Subject:    "s",
					PairLabels: []string{"R1"},
					Points:     []MuscleRatioPhasePoint{{Name: "P1", Time: 0.5, Values: []float64{0.1}}},
				})
			require.Error(t, err)
			require.Contains(t, err.Error(), "輸出路徑")
		})
	}
}
