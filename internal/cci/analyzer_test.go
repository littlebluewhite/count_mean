package cci

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCCIAnalyzer_ExportToCSV_MaliciousSubject_NoTraversal 防止路徑穿越：
// 原本 ExportToCSV 直接把 result.Subject 拼進 fileName，含 "../" 或 "/" 的 Subject
// 會讓 filepath.Join 把檔案寫到 outputDir 之外。修正後套用 calculator.SanitizeFileName。
func TestCCIAnalyzer_ExportToCSV_MaliciousSubject_NoTraversal(t *testing.T) {
	tempDir := t.TempDir()

	maliciousSubjects := []string{
		"../etc/passwd",
		"..\\windows\\sensitive",
		"foo/bar",
		"with space and:colon",
	}

	a := NewCCIAnalyzer()

	for _, subject := range maliciousSubjects {
		t.Run(subject, func(t *testing.T) {
			result := &CCIAnalysisResult{
				Subject:       subject,
				PairResults:   nil,
				TimeValues:    []float64{0, 1, 2},
				PhasePercents: map[string]float64{},
				PhaseTimes:    map[string]float64{},
				MeanCurves:    map[string][]float64{},
				GaitStartTime: 0,
				GaitEndTime:   2,
			}

			outputPath, err := a.ExportToCSV(result, tempDir)
			require.NoError(t, err)

			absOutput, err := filepath.Abs(outputPath)
			require.NoError(t, err)
			absTempDir, err := filepath.Abs(tempDir)
			require.NoError(t, err)

			// outputPath 必須在 tempDir 之下，不能穿越上層
			assert.True(t,
				strings.HasPrefix(absOutput, absTempDir+string(filepath.Separator)),
				"output path %s escaped tempDir %s", absOutput, absTempDir,
			)

			// 確認檔案真的有建立在 tempDir 內
			info, err := os.Stat(outputPath)
			require.NoError(t, err)
			assert.False(t, info.IsDir())
		})
	}
}

// TestCCIAnalyzer_ExportToCSV_InvalidGaitDuration 防止無聲輸出 NaN/Inf：
// 當 GaitEndTime == GaitStartTime（或 End < Start）時，原本會除以零產生
// +Inf / NaN 寫入 CSV 的 "Gait Cycle (%)" 欄位。修正後在迴圈前 fail-fast。
func TestCCIAnalyzer_ExportToCSV_InvalidGaitDuration(t *testing.T) {
	tempDir := t.TempDir()
	a := NewCCIAnalyzer()

	cases := []struct {
		name      string
		startTime float64
		endTime   float64
	}{
		{"equal_times", 1.5, 1.5},
		{"negative_duration", 2.0, 1.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := &CCIAnalysisResult{
				Subject:       "test_subject",
				PairResults:   nil,
				TimeValues:    []float64{tc.startTime, (tc.startTime + tc.endTime) / 2, tc.endTime},
				PhasePercents: map[string]float64{},
				PhaseTimes:    map[string]float64{},
				MeanCurves:    map[string][]float64{},
				GaitStartTime: tc.startTime,
				GaitEndTime:   tc.endTime,
			}

			_, err := a.ExportToCSV(result, tempDir)
			require.Error(t, err, "expected error for invalid gait duration")
			assert.Contains(t, err.Error(), "步態週期")
		})
	}
}

// TestCCIAnalyzer_ExportToCSV_FiltersNaNAndInfRows 是 codex Wave 6 C-4 的
// regression：CalculateCCIRudolph 對 invalid input（如負值除以零、NaN/Inf 輸入）
// 回傳 math.NaN()，但 writeCSVFile 之前直接 fmt.Sprintf("%.6f", v) 把字面 "NaN"
// 寫進 CSV — Excel / pandas / R 都會誤讀為字串非數值。修法：partial NaN cell
// 寫空字串、全 NaN row 整列 skip 並計入 droppedRowCount + log warning。
func TestCCIAnalyzer_ExportToCSV_FiltersNaNAndInfRows(t *testing.T) {
	tempDir := t.TempDir()
	a := NewCCIAnalyzer()

	// 構造 3 個 time point，1 對 pair：
	//   t=0.0: finite 值
	//   t=0.5: NaN（整 row 唯一 pair 也是 NaN → row 全 NaN → 應被 skip）
	//   t=1.0: +Inf（row 全 Inf → 應被 skip）
	result := &CCIAnalysisResult{
		Subject:       "nan_filter_test",
		TimeValues:    []float64{0.0, 0.5, 1.0},
		PhasePercents: map[string]float64{},
		PhaseTimes:    map[string]float64{},
		MeanCurves:    map[string][]float64{},
		GaitStartTime: 0,
		GaitEndTime:   1.0,
		PairResults: []CCIResult{
			{
				PairName: "RA/ES",
				Values:   []float64{0.42, math.NaN(), math.Inf(1)},
			},
		},
	}

	outputPath, err := a.ExportToCSV(result, tempDir)
	require.NoError(t, err)

	csvBytes, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	csvContent := string(csvBytes)

	// 1. CSV 不能含字面 "NaN" 或 "Inf" 字串 — 這正是修法要避免的下游 Excel / pandas 誤讀
	assert.NotContains(t, csvContent, "NaN", "CSV 不該含字面 NaN 字串")
	assert.NotContains(t, csvContent, "Inf", "CSV 不該含字面 Inf 字串")

	// 2. finite 行（t=0.0, value=0.42）應該存在
	assert.Contains(t, csvContent, "0.0000")
	assert.Contains(t, csvContent, "0.420000")

	// 3. 全 NaN/Inf 的 row（t=0.5 / 1.0）應該被 skip — 不會在輸出 CSV 中出現
	lines := strings.Split(strings.TrimSpace(csvContent), "\n")
	// header(1) + finite row(1) = 2 lines；NaN/Inf 兩個 row 已 skip
	require.Equal(t, 2, len(lines), "expected only header + 1 finite row, got %d lines: %v", len(lines), lines)
}

// TestCCIAnalyzer_ExportToCSV_PartialNaNRowKeepsOtherPairs 確認當 row 中只有
// 部分 pair 為 NaN 時，整 row 不會被 skip — NaN cell 寫空字串、其他 pair 保留。
// CSV 慣例：空字串代表 missing data，下游 Excel / pandas 會正確識別為 NULL。
func TestCCIAnalyzer_ExportToCSV_PartialNaNRowKeepsOtherPairs(t *testing.T) {
	tempDir := t.TempDir()
	a := NewCCIAnalyzer()

	// 2 對 pair；t=0.5 一個 NaN 一個 finite → 整 row 應保留，NaN cell 寫空字串
	result := &CCIAnalysisResult{
		Subject:       "partial_nan_test",
		TimeValues:    []float64{0.0, 0.5},
		PhasePercents: map[string]float64{},
		PhaseTimes:    map[string]float64{},
		MeanCurves:    map[string][]float64{},
		GaitStartTime: 0,
		GaitEndTime:   1.0,
		PairResults: []CCIResult{
			{PairName: "RA/ES", Values: []float64{0.10, math.NaN()}},
			{PairName: "IL/GMax", Values: []float64{0.20, 0.30}},
		},
	}

	outputPath, err := a.ExportToCSV(result, tempDir)
	require.NoError(t, err)

	csvBytes, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	csvContent := string(csvBytes)

	assert.NotContains(t, csvContent, "NaN")

	// finite 值都應該存在
	assert.Contains(t, csvContent, "0.100000")
	assert.Contains(t, csvContent, "0.200000")
	assert.Contains(t, csvContent, "0.300000")

	// header + 兩個 data row = 3 lines（partial NaN row 不 skip）
	lines := strings.Split(strings.TrimSpace(csvContent), "\n")
	require.Equal(t, 3, len(lines), "partial NaN row should not be skipped; expected 3 lines, got: %v", lines)

	// t=0.5 那 row 的 RA/ES cell 應該是空字串（在兩個逗號之間或末尾）
	// row 形式：t,pct,RA/ES_val,IL/GMax_val → 0.5000,50.00,,0.300000
	lastRow := lines[2]
	assert.Contains(t, lastRow, ",,", "partial-NaN cell 應為空字串（連續兩個逗號），actual row: %s", lastRow)
}
