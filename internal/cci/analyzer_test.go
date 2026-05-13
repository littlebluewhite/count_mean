package cci

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestCCIAnalyzer_AnalyzeCCI_AcceptsLiteralPercentInEMGFilename 釘住 codex review
// post-impl P1：BTS 匯出的 EMG 檔名常含字面 "%"（例 "SF_8_BTS%_*.csv"），原本 CCI loadEMGData
// 用 PathValidator.GetSafePath 會誤拒，整個 CCI 對標準資料不可用。改走 security.ResolveLenientPath
// 後，這條 path 必須能順利通過驗證階段。
//
// 本 test 是 contract 守門：不關心 AnalyzeCCI 是否最後成功（可能因其他原因失敗），只要
// 「EMG 檔案路徑驗證失敗」訊息不出現即視為 fix 持續有效。
func TestCCIAnalyzer_AnalyzeCCI_AcceptsLiteralPercentInEMGFilename(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	emgFile := "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv"

	// 最小 8-channel R.* EMG (200 samples × 0.001s = 0~0.199s)
	var emgBuf strings.Builder
	emgBuf.WriteString(`X [],R.RA: EMG 1,R.ES: EMG 2,R.IL: EMG 3,R.GMax: EMG 4,` +
		`R.RF: EMG 5,R.BF: EMG 6,R.TA&IO: EMG 7,R.MF: EMG 8` + "\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&emgBuf, "%.4f,1.0,1.0,1.0,1.0,1.0,1.0,1.0,1.0\n", float64(i)*0.001)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, emgFile), []byte(emgBuf.String()), 0o600))

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	manifestContent := "Subject,motion file,Force Plate file,EMG file," +
		"EMG第一筆時間對應Motion的時間index值,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		fmt.Sprintf("SF8,m.csv,f.anc,%s,1,0.01,0.02,0.03,0.04,0.05,20,0.07,0.08,30,0.09\n", emgFile)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o600))

	a := NewCCIAnalyzer()
	_, err := a.AnalyzeCCI(&CCIParams{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		SubjectIndex: 0,
	})

	// 強化主張：path-validation 不該因 '%' 失敗（fix 退化的徵兆），同時必須真的穿越到
	// 下游階段——若 err==nil 表示完整跑完，亦合格；若 err!=nil 但訊息不是 path-validation
	// 字串，代表 path 解析確實放行了 '%' 檔名（後續可能因 manifest 時間範圍等其他原因失敗）。
	if err != nil {
		assert.NotContains(t, err.Error(), "EMG 檔案路徑驗證失敗",
			"path validation 不該因 '%%' 失敗，err=%v", err)
		assert.NotContains(t, err.Error(), "URL-encoded 殘留",
			"不應觸發 PathValidator 的 URL-decode 拒絕邏輯，err=%v", err)
		assert.NotContains(t, err.Error(), "EMG 檔案不存在",
			"含 '%%' 的檔案應該被正確開啟，err=%v", err)
	}
}

// TestCCIAnalyzer_ConcurrentCallsNoRace 釘住與 muscle_ratio 對稱的並發守門：
// CCIAnalyzer 也持有 shared `*EMGParser`，並行 Wails RPC 呼叫 AnalyzeCCI 會在
// `parsers.EMGParser.ParseFile` 寫 `frequency` 欄位時觸發 write/write race。
// 修法：loadEMGData 內 per-call new。
//
// 本 test 必須在 `go test -race` 下跑才會觸發 race detector。
func TestCCIAnalyzer_ConcurrentCallsNoRace(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	emgFile := "concurrent.csv"

	var emgBuf strings.Builder
	emgBuf.WriteString(`X [],R.RA: EMG 1,R.ES: EMG 2,R.IL: EMG 3,R.GMax: EMG 4,` +
		`R.RF: EMG 5,R.BF: EMG 6,R.TA&IO: EMG 7,R.MF: EMG 8` + "\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&emgBuf, "%.4f,1.0,1.0,1.0,1.0,1.0,1.0,1.0,1.0\n", float64(i)*0.001)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, emgFile), []byte(emgBuf.String()), 0o600))

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	manifestContent := "Subject,motion file,Force Plate file,EMG file," +
		"EMG第一筆時間對應Motion的時間index值,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		fmt.Sprintf("S,m.csv,f.anc,%s,1,0.01,0.02,0.03,0.04,0.05,20,0.07,0.08,30,0.09\n", emgFile)
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o600))

	a := NewCCIAnalyzer()

	// 並行 8 個 AnalyzeCCI 呼叫共用同個 Analyzer instance — 模擬 Wails 並行 RPC
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.AnalyzeCCI(&CCIParams{
				ManifestFile: manifestPath,
				DataFolder:   dataDir,
				SubjectIndex: 0,
			})
			// 不 assert err — race detector 才是這個 test 的守門
			_ = err
		}()
	}
	wg.Wait()
}
