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

// TestDownloadCCIChart_RejectsInvalidPNGBeforeBoundary 守 short-circuit 順序:
// PNG 驗證錯誤應在 boundary validation 之前 reject(因為 OutputDir 即使合法,
// 也不該寫非 PNG 內容)。此 test 確認 PNG check 仍在最前面,沒被
// boundary validation 重排到 PNG check 之前。
func TestDownloadCCIChart_RejectsInvalidPNGBeforeBoundary(t *testing.T) {
	app := setupDownloadCCIChartTestApp(t, t.TempDir())

	result, err := app.DownloadCCIChart(CCIDownloadParams{
		ImageData: "not-a-data-url",
		Subject:   "S1",
	})

	require.Error(t, err, "非 data URL 必須 reject")
	assert.Nil(t, result)
	// 應該是 ErrInvalidImageFormat,不是 boundary error
	assert.Contains(t, err.Error(), "圖片",
		"reject reason 應為 PNG format 相關: got %v", err)
}

// TestDownloadCCIChart_ErrorMessage_NoDuplicateLabel 釘住 過去的 wrap 鏈
// 會在邊界路徑驗證失敗時產生「PNG 輸出 輸出路徑 路徑驗證失敗: ...」這種
// 重複 label 的字串(來源:validateExternalPathInputs 已加 "輸出路徑 路徑驗證
// 失敗: ..." 前綴,DownloadCCIChart 又在外層 fmt.Errorf("PNG 輸出 %w", ...) 再
// 包一次,於是 "PNG 輸出" 與 "輸出路徑" 同時出現)。
//
// 修法:label 改在 validateExternalPathInputs 一次帶 "PNG 輸出路徑",caller
// 不再外層 wrap。本測試在錯誤訊息中強制要求:
//   - 「輸出路徑 路徑驗證失敗」字串只能出現一次
//   - 不再出現「PNG 輸出 輸出路徑」這種重複 label 拼接
//   - 仍含「PNG」與「路徑驗證失敗」兩個語義 token,讓 audit log 可定位
func TestDownloadCCIChart_ErrorMessage_NoDuplicateLabel(t *testing.T) {
	// 用 "/etc" 當 outputDir 觸發 sensitive path → 一定走到
	// validateExternalPathInputs failure 分支
	app := setupDownloadCCIChartTestApp(t, "/etc")

	result, err := app.DownloadCCIChart(CCIDownloadParams{
		ImageData: validPNGBase64DataURL(),
		Subject:   "S1",
	})

	require.Error(t, err, "/etc outputDir 必須被 boundary 擋下")
	assert.Nil(t, result)

	msg := err.Error()
	t.Logf("DownloadCCIChart 邊界錯誤訊息: %q", msg)

	// 1. 不可再有重複 label「PNG 輸出 輸出路徑」(空格分隔的舊版 wrap)
	assert.NotContains(t, msg, "PNG 輸出 輸出路徑",
		"重複 label 已修(P2-8):不應再出現「PNG 輸出 輸出路徑」拼接")

	// 2. 「路徑驗證失敗」應該只出現一次(過去 nested wrap 會出現多次)
	occurrences := strings.Count(msg, "路徑驗證失敗")
	assert.Equal(t, 1, occurrences,
		"「路徑驗證失敗」label 在 wrap chain 中應只出現一次,got %d 次: %q",
		occurrences, msg)

	// 3. 仍包含關鍵 context — PNG / 路徑驗證 兩語義 token 都還在
	assert.Contains(t, msg, "PNG", "audit 仍需指出失敗於 PNG 流程")
	assert.Contains(t, msg, "路徑驗證失敗", "audit 仍需指出失敗類別")
}
