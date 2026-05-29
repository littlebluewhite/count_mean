package gui

import (
	"fmt"
	"os"
	"path/filepath"
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
// 在成功計算後仍看到「失敗」。對照批次 executeBatchLoop / NormalizeData 都有設。
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
