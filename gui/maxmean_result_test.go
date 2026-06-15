package gui

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// writeEMGCSVForMaxMean 建立最小 EMG CSV(Time,Ch1,Ch2;1ms 取樣),足以讓
// max-mean 跑通。值略有變化以免 max==mean 退化。
func writeEMGCSVForMaxMean(t *testing.T, path string, rows int) {
	t.Helper()
	var b strings.Builder
	b.WriteString("Time,Ch1,Ch2\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&b, "%.6f,%d.5,%d.3\n", float64(i)/1000.0, 100+i%7, 200+i%5)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
}

// TestCalculateMaxMean_SingleFile_ReportsSuccessAndMessage 釘住 whole-project
// review P1:單檔 max-mean 成功時 calculateMaxMeanSingle 先前回傳的 MaxMeanResult
// 漏設 Success/Message → bool 零值 false。前端依 result.success 判定成敗,使用者
// 在成功計算後仍看到「失敗」。對照批次 RunBatch / NormalizeData 都有設。
func TestCalculateMaxMean_SingleFile_ReportsSuccessAndMessage(t *testing.T) {
	inDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = inDir
	cfg.OutputDir = t.TempDir()
	app := NewApp(cfg, "test")

	csvPath := filepath.Join(inDir, "sample.csv")
	writeEMGCSVForMaxMean(t, csvPath, 50)

	result, err := app.CalculateMaxMean(MaxMeanParams{
		InputPath:  csvPath,
		WindowSize: 5,
		IsBatch:    false,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.True(t, result.Success,
		"單檔 max-mean 成功必須回報 Success=true(先前漏設 → 預設 false → 前端誤判失敗)")
	assert.NotEmpty(t, result.Message, "成功時應有 Message")
	assert.NotEmpty(t, result.OutputPath)
	assert.FileExists(t, result.OutputPath)
}

// TestCalculateMaxMean_ExternalBatch_OutputPathUnderOutputDir 釘住 whole-project
// review P1:批次 OutputPath 漂移。external(直接)批次先前回傳的 OutputPath 是
// outputDirName 裸名(Base(inputPath),無 OutputDir 前綴),與檔案實際寫入位置
// OutputDir/<batchName>/ 不一致 → 前端顯示「已保存到」的路徑錯誤、無法定位輸出。
func TestCalculateMaxMean_ExternalBatch_OutputPathUnderOutputDir(t *testing.T) {
	outDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = t.TempDir() // 批次目錄刻意放在 InputDir 之外 → 走 external/direct 路徑
	cfg.OutputDir = outDir
	app := NewApp(cfg, "test")

	batchDir := t.TempDir()
	writeEMGCSVForMaxMean(t, filepath.Join(batchDir, "a.csv"), 50)
	writeEMGCSVForMaxMean(t, filepath.Join(batchDir, "b.csv"), 50)

	result, err := app.CalculateMaxMean(MaxMeanParams{
		InputPath:  batchDir,
		WindowSize: 5,
		IsBatch:    true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "Message: %s", result.Message)

	wantDir := filepath.Join(outDir, filepath.Base(batchDir))
	assert.Equal(t, wantDir, result.OutputPath,
		"batch OutputPath 應為實際寫入目錄 OutputDir/<batchName>,而非裸名或漂移路徑")
	assert.DirExists(t, result.OutputPath)
}

// TestCalculateMaxMean_ArrayValuesReverseScaledLikeCSV 釘住 P3-12:GUI 回傳的
// Results 陣列先前未做反縮放,值停在「縮放域」(被 10^ScalingFactor 放大),與寫出
// 的 CSV (csvConverter.scaleValue 已除 10^SF) 單位分歧。此測試對同一次計算同時讀
// GUI 陣列與輸出 CSV 的「最大平均值 / 開始計算秒數 / 結束計算秒數」三列,斷言兩者
// 數值一致 — 修復前陣列大 10^SF 倍故 red,修復後一致故 green。
func TestCalculateMaxMean_ArrayValuesReverseScaledLikeCSV(t *testing.T) {
	inDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = inDir
	cfg.OutputDir = t.TempDir()
	cfg.ScalingFactor = 2 // 10^2=100,可辨識的非 1 縮放;避開 default 10 造成的 1e-? 噪音
	app := NewApp(cfg, "test")

	csvPath := filepath.Join(inDir, "sample.csv")
	writeEMGCSVForMaxMean(t, csvPath, 50)

	result, err := app.CalculateMaxMean(MaxMeanParams{
		InputPath:  csvPath,
		WindowSize: 5,
		IsBatch:    false,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "Message: %s", result.Message)
	require.NotEmpty(t, result.Results, "應至少有一個通道結果")

	// 讀回輸出 CSV:row 順序 = header / 開始範圍 / 結束範圍 / 開始計算 / 結束計算 / 最大平均值。
	// 各 data column 對應一個通道,順序與 result.Results 相同。
	maxMeanRow, startRow, endRow := readMaxMeanOutputRows(t, result.OutputPath)
	require.Len(t, result.Results, len(maxMeanRow)-1,
		"GUI 陣列通道數應與 CSV data column 數一致")

	const mult = 100.0 // 10^ScalingFactor(SF=2)

	for ch, arr := range result.Results {
		require.Len(t, arr, 3, "每列為 [MaxMean, StartTime, EndTime]")
		col := ch + 1 // column 0 是 row label
		csvMaxMean := parseCSVFloat(t, maxMeanRow[col])
		csvStart := parseCSVFloat(t, startRow[col])
		csvEnd := parseCSVFloat(t, endRow[col])

		// 核心 P3-12 斷言:GUI 陣列須與 CSV 反縮放後同單位。修復前陣列停在縮放域
		// (= CSV 值 × 100)→ 此處差 100 倍故 red;修復後一致故 green。
		// 時間欄可能為 0(window 起點 t=0),用 InDelta 而非 InEpsilon(後者拒零期望值)。
		assert.InDelta(t, csvMaxMean, arr[0], 1e-9,
			"通道 %d MaxMean:GUI 陣列值須與 CSV 反縮放後一致(修復前大 10^SF 倍)", ch+1)
		assert.InDelta(t, csvStart, arr[1], 1e-9, "通道 %d StartTime 須與 CSV 一致", ch+1)
		assert.InDelta(t, csvEnd, arr[2], 1e-9, "通道 %d EndTime 須與 CSV 一致", ch+1)
		// 防「兩條路徑都忘了反縮放」的偽綠:陣列值須嚴格小於縮放域值(= 已被除過 100)。
		// MaxMean 來自 ~100-200 量級的合成資料,反縮放後 ~1-2,遠小於 scaled 的數百。
		assert.Less(t, arr[0], csvMaxMean*mult,
			"通道 %d MaxMean 反縮放須真的發生(陣列值應 < 縮放域值)", ch+1)
	}
}

// readMaxMeanOutputRows 讀 WriteMaxMean 輸出的 CSV,回傳 (最大平均值, 開始計算秒數,
// 結束計算秒數) 三列。layout 由 csvConverter.ConvertMaxMeanResults 固定。
func readMaxMeanOutputRows(t *testing.T, path string) (maxMeanRow, startRow, endRow []string) {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	rows, err := csv.NewReader(f).ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(rows), maxMeanResultRowCountForTest, "輸出 CSV 應有 header + 5 列")

	return rows[5], rows[3], rows[4]
}

// maxMeanResultRowCountForTest 對齊 csvConverter 的 6 列輸出(header + 5 data rows)。
const maxMeanResultRowCountForTest = 6

func parseCSVFloat(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	require.NoError(t, err, "CSV cell 應為數值: %q", s)

	return v
}
