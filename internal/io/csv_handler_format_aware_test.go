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
// stats.Subject 交給 calculator.GenerateOutputFileName -> SanitizeFileName,
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
// Row layout (與 calculator.EMGStatisticsCalculator.ExportToCSV 對齊,
// 確保 migrate 後輸出檔對等):
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
