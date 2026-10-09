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

// TestCalculateMaxMean_EmptyBatchPath_ErrorsExplicitly 釘住 validation facade
// collapse(刪 validator.go)後保留的「唯一」行為:批次模式空 InputPath 必須顯式
// 報錯,而非靜默把整個 InputDir root 當批次目錄處理(calculateMaxMeanBatch 的
// footgun — 空字串 → ListCSVFilesInDirectory("") → InputDir root)。
//
// inline empty-guard(gui/maxmean_batch_adapter.go)取代了已刪 facade 的
// ValidateDirectoryPath;舊 check 的 NUL/length 部分刻意下放給下游
// per-file security.PathValidator(internal GetSafePath / external
// ValidateExternalPath),故此處只釘 empty 這個有行為意義的 case。
//
// 斷言特定訊息「目錄路徑不能為空」而非僅 err!=nil:guard 被移除時下游 list/read
// 會回別的錯誤(或 0 檔靜默),特定訊息斷言才能鑑別 guard 存活。
func TestCalculateMaxMean_EmptyBatchPath_ErrorsExplicitly(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.InputDir = t.TempDir()
	cfg.OutputDir = t.TempDir()
	app := NewApp(cfg, "test")

	_, err := app.CalculateMaxMean(MaxMeanParams{
		InputPath:  "",
		WindowSize: 5,
		IsBatch:    true,
	})
	require.Error(t, err, "空 InputPath 批次必須顯式報錯,不可靜默處理 InputDir root")
	assert.ErrorContains(t, err, "目錄路徑不能為空",
		"必須是 empty-guard 的訊息(byte-parity 保留自舊 ValidateDirectoryPath),"+
			"而非下游 list/read 的其他錯誤 — 否則 guard 被移除仍會誤綠")
}

// TestCalculateMaxMean_BatchAllFailed_SuccessFalse 釘住批次「全部失敗」不得回報成功:
// 每個檔案都計算失敗時 Success 必須為 false,Message 帶成功/失敗計數,前端據此走失敗路徑。
func TestCalculateMaxMean_BatchAllFailed_SuccessFalse(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.InputDir = t.TempDir()
	cfg.OutputDir = t.TempDir()
	app := NewApp(cfg, "test")

	batchDir := t.TempDir()
	// 只有標題列、沒有資料列 → 每個檔案計算失敗
	for _, name := range []string{"a.csv", "b.csv"} {
		require.NoError(t, os.WriteFile(filepath.Join(batchDir, name), []byte("Time,Ch1,Ch2\n"), 0o644))
	}

	result, err := app.CalculateMaxMean(MaxMeanParams{InputPath: batchDir, WindowSize: 5, IsBatch: true})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Success, "全部檔案失敗不得回報 Success=true")
	assert.Contains(t, result.Message, "成功 0 個檔案，失敗 2 個檔案")
}

// TestCalculateMaxMean_SuccessMessage_NoAbsolutePath 釘住成功 Message 不含絕對路徑
// (絕對路徑只走 OutputPath 欄位,Message 若指名輸出只用 base name)。
func TestCalculateMaxMean_SuccessMessage_NoAbsolutePath(t *testing.T) {
	inDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = inDir
	cfg.OutputDir = t.TempDir()
	app := NewApp(cfg, "test")

	csvPath := filepath.Join(inDir, "sample.csv")
	writeEMGCSVForMaxMean(t, csvPath, 50)

	result, err := app.CalculateMaxMean(MaxMeanParams{InputPath: csvPath, WindowSize: 5})
	require.NoError(t, err)
	require.True(t, result.Success)

	assert.NotContains(t, result.Message, cfg.OutputDir, "Message 不得含輸出目錄絕對路徑")
	assert.Contains(t, result.Message, filepath.Base(result.OutputPath))
}

// readCSVRowsForTest 讀回輸出 CSV 全部列。
func readCSVRowsForTest(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path) //nolint:gosec // test-controlled temp path
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	rows, err := csv.NewReader(f).ReadAll()
	require.NoError(t, err)

	return rows
}

func parseCSVFloat(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	require.NoError(t, err, "CSV cell 應為數值: %q", s)

	return v
}

// TestCalculateMaxMean_BTSPercentFile 釘住:BTS 匯出檔名含字面 `%`(`SF_8_BTS%_6.10.csv`)
// 放在 InputDir 內時,讀取與寫入都必須成功 — 路徑一律不 URL-decode,輸出檔名保留字面 `%`。
func TestCalculateMaxMean_BTSPercentFile(t *testing.T) {
	inDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = inDir
	cfg.OutputDir = t.TempDir()
	app := NewApp(cfg, "test")

	csvPath := filepath.Join(inDir, "SF_8_BTS%_6.10.csv")
	writeEMGCSVForMaxMean(t, csvPath, 50)

	result, err := app.CalculateMaxMean(MaxMeanParams{InputPath: csvPath, WindowSize: 5})
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, filepath.Base(result.OutputPath), "SF_8_BTS%_6.10", "輸出檔名須保留字面 `%`")
	assert.FileExists(t, result.OutputPath)
}
