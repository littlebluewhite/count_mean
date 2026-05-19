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

// setupSavePNGTestApp 構造 minimal App 給 savePNGFromBase64 / GenerateChart
// 測試使用,讓 boundary path validation case 可以聚焦 OutputDir 行為而不被
// nil-deref 卡住。參考 cci_download_test.go 的 setupDownloadCCIChartTestApp。
func setupSavePNGTestApp(t *testing.T, outputDir string) *App {
	t.Helper()

	cfg := &config.AppConfig{OutputDir: outputDir}

	app := &App{
		logger:              logging.GetLogger("chart_helpers_test"),
		phaseSyncAnalyzer:   phase_sync.NewPhaseSyncAnalyzer(),
		cciAnalyzer:         cci.NewCCIAnalyzer(),
		muscleRatioAnalyzer: muscle_ratio.NewAnalyzer(),
	}
	app.state.Store(&appState{config: cfg})

	return app
}

// validPNGBase64ForChart 回傳通過 DecodeAndValidatePNG 三層守門的最小 PNG
// dataURL。直接 reuse png_validation_test.go 的 validPNGBytes(同 package)。
func validPNGBase64ForChart() string {
	png := validPNGBytes(1, 1)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// TestSavePNGFromBase64_RejectsTraversal 釘住 修法:savePNGFromBase64
// 必須在 PNG 驗證後、寫檔前透過 validateExternalPathInputs 把合成的 outputPath
// 擋下;對齊 cci_handlers.go:158 DownloadCCIChart 模式。
//
// 攻擊面:OutputDir 雖來自 config 不來自前端,但若 config.json 被惡意修改
// (or app 啟動時被 supply chain 攻擊塞髒值)落到 "../etc" / "/etc" 等
// traversal / 系統敏感目錄,fsperm.WriteFileNoFollow 雖會擋 symlink,但
// boundary 提早 reject 對 audit log 與錯誤訊息更友善。
//
// 不可只測 outputDir 被汙染:params.Title 來自前端,SanitizeFileName 雖剝掉
// path separator 但仍可能拼出 traversal-like 字串(如 "..."),合成 outputPath
// 才是真正攻擊面。所以驗合成 path 而非分別驗 outputDir / fileName。
func TestSavePNGFromBase64_RejectsTraversal(t *testing.T) {
	traversalDirs := []struct {
		name      string
		outputDir string
	}{
		{"relative_traversal", "../etc"},
		{"deep_relative_traversal", "../../../root"},
		{"absolute_etc", "/etc"},
		{"absolute_root_ssh", "/root/.ssh"},
	}

	for _, tc := range traversalDirs {
		t.Run(tc.name, func(t *testing.T) {
			app := setupSavePNGTestApp(t, tc.outputDir)

			result, err := app.savePNGFromBase64(ChartParams{
				Title:     "test",
				FilePath:  "irrelevant.csv",
				ImageData: validPNGBase64ForChart(),
			})

			require.Error(t, err,
				"惡意 OutputDir (%s) 應被 boundary 擋下", tc.outputDir)
			assert.Nil(t, result,
				"boundary 失敗時不應回 result")
			assert.True(t,
				strings.Contains(err.Error(), "路徑驗證失敗") ||
					strings.Contains(err.Error(), "輸出路徑") ||
					strings.Contains(err.Error(), "PNG 輸出"),
				"err 應含 boundary path validation 標誌: got %v", err)
		})
	}
}

// TestSavePNGFromBase64_AcceptsLegitimatePath sanity-check:合法 t.TempDir()
// 當 OutputDir,savePNGFromBase64 應該成功寫出 PNG。確保新增的 boundary
// validation 不誤殺正常流程。
func TestSavePNGFromBase64_AcceptsLegitimatePath(t *testing.T) {
	outputDir := t.TempDir()
	app := setupSavePNGTestApp(t, outputDir)

	result, err := app.savePNGFromBase64(ChartParams{
		Title:     "myTitle",
		FilePath:  "/some/path/myData.csv",
		ImageData: validPNGBase64ForChart(),
	})

	require.NoError(t, err, "合法 OutputDir 必須通過 boundary validation")
	require.NotNil(t, result)
	assert.True(t, result.Success)

	// 預期檔名:<baseName>_<safeTitle>.png = "myData_myTitle.png"
	expectedPath := filepath.Join(outputDir, "myData_myTitle.png")
	assert.Equal(t, expectedPath, result.OutputPath)

	// 確認檔案真的被寫出來
	_, statErr := os.Stat(expectedPath)
	require.NoError(t, statErr, "PNG 必須真的被寫入 disk")
}

// TestSavePNGFromBase64_RejectsInvalidPNGBeforeBoundary 守 short-circuit 順序:
// PNG 驗證錯誤應在 boundary validation 之前 reject(因為 OutputDir 即使合法,
// 也不該寫非 PNG 內容)。此 test 確認 PNG check 仍在最前面,沒被 boundary
// validation 重排到 PNG check 之前(對齊 cci_download_test 同名 case)。
func TestSavePNGFromBase64_RejectsInvalidPNGBeforeBoundary(t *testing.T) {
	app := setupSavePNGTestApp(t, t.TempDir())

	result, err := app.savePNGFromBase64(ChartParams{
		Title:     "test",
		FilePath:  "irrelevant.csv",
		ImageData: "not-a-data-url",
	})

	require.Error(t, err, "非 data URL 必須 reject")
	assert.Nil(t, result)
	// 應該是 ErrInvalidImageFormat,不是 boundary error
	assert.Contains(t, err.Error(), "圖片",
		"reject reason 應為 PNG format 相關: got %v", err)
}

// TestSavePNGFromBase64_ErrorMessage_NoDuplicateLabel 釘住 對齊修法:
// savePNGFromBase64 與 DownloadCCIChart 共用同一個重複 label bug
// (validateExternalPathInputs 已加 label,caller 又外 wrap 一次造成
// 「PNG 輸出 輸出路徑 路徑驗證失敗: ...」)。本 test 確保兩 RPC 的錯誤
// 訊息格式保持一致 — label 只出現一次、語意關鍵字仍存在。
func TestSavePNGFromBase64_ErrorMessage_NoDuplicateLabel(t *testing.T) {
	// /etc 觸發 sensitive path 一定走到 boundary 失敗分支
	app := setupSavePNGTestApp(t, "/etc")

	result, err := app.savePNGFromBase64(ChartParams{
		Title:     "test",
		FilePath:  "irrelevant.csv",
		ImageData: validPNGBase64ForChart(),
	})

	require.Error(t, err)
	assert.Nil(t, result)

	msg := err.Error()
	t.Logf("savePNGFromBase64 邊界錯誤訊息: %q", msg)

	assert.NotContains(t, msg, "PNG 輸出 輸出路徑",
		"重複 label 已修(P2-8):不應再出現「PNG 輸出 輸出路徑」拼接")

	occurrences := strings.Count(msg, "路徑驗證失敗")
	assert.Equal(t, 1, occurrences,
		"「路徑驗證失敗」應只出現一次,got %d 次: %q", occurrences, msg)

	assert.Contains(t, msg, "PNG")
	assert.Contains(t, msg, "路徑驗證失敗")
}
