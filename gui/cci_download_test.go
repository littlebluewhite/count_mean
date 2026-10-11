package gui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/cci"
	"count_mean/internal/logging"
	"count_mean/internal/muscle_ratio"
	"count_mean/internal/phase_sync"
)

// setupDownloadCCIChartTestApp 構造 minimal App,讓 DownloadCCIChart 測試
// 可以聚焦在 Output placement 的邊界行為而不被 nil-deref 卡住。
//
// 不 reuse setupHandlerTestApp 是因為 download chart 需要 OutputDir 可控
// (對齊 traversal 攻擊 simulation)。state 走 buildAppState(含 csvHandler)。
func setupDownloadCCIChartTestApp(t *testing.T, outputDir string) *App {
	t.Helper()

	app := &App{
		logger:              logging.GetLogger("cci_download_test"),
		phaseSyncAnalyzer:   phase_sync.NewPhaseSyncAnalyzer(),
		cciAnalyzer:         cci.NewCCIAnalyzer(),
		muscleRatioAnalyzer: muscle_ratio.NewAnalyzer(),
	}
	app.state.Store(newPNGTestState(t, outputDir))

	return app
}

// validPNGBase64DataURL 回傳一個最小但合法的 PNG dataURL,供 DownloadCCIChart
// 不被 DecodeAndValidatePNG 攔下,讓 test 可以走到後續的 Output placement。
func validPNGBase64DataURL() string {
	// 直接 reuse png_validation_test.go 的 validPNGBytes(同 package 可見)。
	png := validPNGBytes(1, 1)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// TestDownloadCCIChart_RejectsTraversalOutputDir 釘住 boundary 對齊:
// DownloadCCIChart 雖然 OutputDir 來自 config 不來自 params,但若 OutputDir
// 本身被汙染(惡意 config / 開發者誤設)含 traversal segment 或指向敏感目錄,
// Output placement 的 ValidateExternalPath 應該擋下(「輸出路徑無效」),不建目錄也不寫檔。
// 錯誤不再帶 ADR-0055 前的「PNG 輸出路徑」標籤。
func TestDownloadCCIChart_RejectsTraversalOutputDir(t *testing.T) {
	traversalDirs := []struct {
		name      string
		outputDir string
	}{
		{"relative_traversal", "../etc"},
		{"deep_relative_traversal", "../../../root"},
		{"absolute_etc", "/etc"},
		{"absolute_root", "/root/.ssh"},
	}

	for _, tc := range traversalDirs {
		t.Run(tc.name, func(t *testing.T) {
			app := setupDownloadCCIChartTestApp(t, tc.outputDir)

			result, err := app.DownloadCCIChart(CCIDownloadParams{
				ImageData: validPNGBase64DataURL(),
				Subject:   "S1",
			})

			require.Error(t, err,
				"惡意 OutputDir (%s) 應被 boundary 擋下", tc.outputDir)
			assert.Nil(t, result,
				"boundary 失敗時不應回 result")
			assert.Contains(t, err.Error(), "輸出路徑無效")
			assert.NotContains(t, err.Error(), "PNG 輸出路徑")
		})
	}
}

// TestDownloadCCIChart_AcceptsLegitimatePath sanity-check:用合法 t.TempDir()
// 當 OutputDir,DownloadCCIChart 寫出 {subject}_CCI_Rudolph.png,內容等於 decode 後的
// PNG bytes;Message 只帶檔名(進 webview,不帶輸出目錄的絕對路徑)。
func TestDownloadCCIChart_AcceptsLegitimatePath(t *testing.T) {
	outputDir := t.TempDir()
	app := setupDownloadCCIChartTestApp(t, outputDir)

	result, err := app.DownloadCCIChart(CCIDownloadParams{
		ImageData: validPNGBase64DataURL(),
		Subject:   "S1",
	})

	require.NoError(t, err, "合法 OutputDir 必須通過 boundary validation")
	require.NotNil(t, result)
	assert.True(t, result.Success)

	expectedPath := filepath.Join(outputDir, "S1_CCI_Rudolph.png")
	assert.Equal(t, expectedPath, result.OutputPath)
	assert.Equal(t, "圖表已下載至: S1_CCI_Rudolph.png", result.Message)
	assert.NotContains(t, result.Message, outputDir)

	written, readErr := os.ReadFile(expectedPath)
	require.NoError(t, readErr, "PNG 必須真的被寫入 disk")
	assert.Equal(t, validPNGBytes(1, 1), written, "寫入的 bytes 必須等於 decode 後的 PNG bytes")
}

// 註:bad-prefix→ErrInvalidImageFormat 與 decode/validate 失敗由 png_download_test.go 的
// downloadValidatedPNG 測試擁有;leaf symlink 與「SaveConfig 換到不存在的 OutputDir」也在
// 該檔(handler 層)。落檔本身(命名、containment、敏感位置、MkdirAll、atomic)由
// internal/io 的 Output placement 表(SubjectPNG 列)覆蓋。此檔僅保留 CCI adapter 行為:
// OutputDir + sanitize(Subject) 推導檔名、boundary 對 derived path 生效。
