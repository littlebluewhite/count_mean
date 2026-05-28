package gui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/cci"
	"count_mean/internal/config"
	"count_mean/internal/logging"
	"count_mean/internal/muscle_ratio"
	"count_mean/internal/phase_sync"
)

// setupDownloadCCIChartTestApp 構造 minimal App,讓 DownloadCCIChart 測試
// 可以聚焦在 boundary path validation 而不被 nil-deref 卡住。
//
// 不 reuse setupHandlerTestApp 是因為 download chart 需要 OutputDir 可控
// (對齊 traversal 攻擊 simulation)。
func setupDownloadCCIChartTestApp(t *testing.T, outputDir string) *App {
	t.Helper()

	cfg := &config.AppConfig{OutputDir: outputDir}

	app := &App{
		logger:              logging.GetLogger("cci_download_test"),
		phaseSyncAnalyzer:   phase_sync.NewPhaseSyncAnalyzer(),
		cciAnalyzer:         cci.NewCCIAnalyzer(),
		muscleRatioAnalyzer: muscle_ratio.NewAnalyzer(),
	}
	app.state.Store(&appState{config: cfg})

	return app
}

// validPNGBase64DataURL 回傳一個最小但合法的 PNG dataURL,供 DownloadCCIChart
// 不被 DecodeAndValidatePNG 攔下,讓 test 可以走到後續的 boundary validation。
func validPNGBase64DataURL() string {
	// 直接 reuse png_validation_test.go 的 validPNGBytes(同 package 可見)。
	png := validPNGBytes(1, 1)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// TestDownloadCCIChart_RejectsTraversalOutputDir 釘住 boundary 對齊:
// DownloadCCIChart 雖然 OutputDir 來自 config 不來自 params,但若 OutputDir
// 本身被汙染(惡意 config / 開發者誤設)含 traversal segment,boundary
// validation 應該擋下,而不是讓 traversal path 走進 fsperm.WriteFileNoFollow。
//
// defense-in-depth 精神:即使 fsperm 已有 nofollow,boundary 提早 reject 對
// audit log 與錯誤訊息都更友善。
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
			assert.True(t,
				strings.Contains(err.Error(), "路徑驗證失敗") ||
					strings.Contains(err.Error(), "輸出路徑"),
				"err 應含 boundary path validation 標誌: got %v", err)
		})
	}
}

// TestDownloadCCIChart_AcceptsLegitimatePath sanity-check:用合法 t.TempDir()
// 當 OutputDir,DownloadCCIChart 應該成功寫出 PNG。確保新增的 boundary
// validation 不誤殺正常流程。
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

	// 確認檔案真的被寫出來
	_, statErr := os.Stat(expectedPath)
	require.NoError(t, statErr, "PNG 必須真的被寫入 disk")
}

// 註:bad-prefix→ErrInvalidImageFormat（含 short-circuit 順序）與 sensitive-path
// rejection + no-double-label 這兩個原本在此的 case，已隨 ADR-0009 Phase 2 把
// 共用 PNG 管線抽到 downloadValidatedPNG，改由 helper 的 seam test
// （png_download_test.go: TestDownloadValidatedPNG_RejectsBadPrefix /
// TestDownloadValidatedPNG_RejectsSensitivePath_NoDoubleLabel）擁有，避免重複覆蓋。
// 此檔僅保留 CCI adapter 行為:OutputDir + sanitize(Subject) 推導檔名 + boundary
// 對 derived path 生效。
