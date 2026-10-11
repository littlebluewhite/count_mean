package gui

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
)

// newPNGTestState 以 buildAppState 建完整 state(含 csvHandler),OutputDir 由 caller 指定;
// InputDir / OperateDir 各用一個 t.TempDir(),避免 allow-list 落到 CWD。
func newPNGTestState(t *testing.T, outputDir string) *appState {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.InputDir = t.TempDir()
	cfg.OperateDir = t.TempDir()
	cfg.OutputDir = outputDir

	return buildAppState(cfg)
}

// newPNGTestApp 構造只含 logger 與 state 的最小 App,供 PNG 下載管線測試。
func newPNGTestApp(t *testing.T, outputDir string) *App {
	t.Helper()

	app := &App{logger: logging.GetLogger("png_download_test")}
	app.state.Store(newPNGTestState(t, outputDir))

	return app
}

// TestDownloadValidatedPNG_RejectsBadPrefix 釘住第一道守門:imageData 缺
// "data:image/png;base64," 前綴時直接回 ErrInvalidImageFormat(不 wrap),且不落檔。
func TestDownloadValidatedPNG_RejectsBadPrefix(t *testing.T) {
	outputDir := t.TempDir()
	app := newPNGTestApp(t, outputDir)

	result, err := app.downloadValidatedPNG("not-a-data-url", "S1", "CCI_Rudolph")

	require.Error(t, err, "缺 PNG dataURL 前綴必須 reject")
	assert.Nil(t, result, "格式錯誤時不應回 result")
	assert.True(t, errors.Is(err, ErrInvalidImageFormat),
		"reject reason 應為 ErrInvalidImageFormat（不 wrap），got %v", err)
	assertDirEmpty(t, outputDir)
}

// TestDownloadValidatedPNG_RejectsInvalidPNGPayload 釘住第二道守門:前綴正確但 payload
// 不是合法 base64 / PNG 時,DecodeAndValidatePNG 的錯誤 wrap 成「PNG 驗證失敗」,
// 且在落檔之前就擋下(OutputDir 不留任何東西)。
func TestDownloadValidatedPNG_RejectsInvalidPNGPayload(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		wantIs  error
	}{
		{name: "NotBase64", payload: "!!! not base64 !!!"},
		{
			name:    "NotPNG",
			payload: base64.StdEncoding.EncodeToString([]byte("GIF89a not a png at all, padding")),
			wantIs:  ErrPNGSignatureInvalid,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outputDir := t.TempDir()
			app := newPNGTestApp(t, outputDir)

			result, err := app.downloadValidatedPNG(pngDataURLPrefix+tc.payload, "S1", "CCI_Rudolph")

			require.Error(t, err, "前綴正確但 payload 非法 PNG 必須 reject")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "PNG 驗證失敗")
			if tc.wantIs != nil {
				assert.ErrorIs(t, err, tc.wantIs)
			}
			assertDirEmpty(t, outputDir)
		})
	}
}

// TestDownloadCCIChart_LeafSymlinkIsReplaced 釘住 leaf symlink 語意(ADR-0055 起與 CSV 相同):
// 輸出檔位置已是指向 OutputDir 外的 symlink 時,atomic rename 取代連結本身 —— 下載成功、
// 連結變成內容為新 PNG 的一般檔,連結目標(victim)不被改寫。
func TestDownloadCCIChart_LeafSymlinkIsReplaced(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires Developer Mode / admin; skipping leaf-symlink test")
	}

	outputDir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.png")
	require.NoError(t, os.WriteFile(victim, []byte("untouched"), 0o600), "setup: 寫 victim 檔失敗")

	link := filepath.Join(outputDir, "S1_CCI_Rudolph.png")
	require.NoError(t, os.Symlink(victim, link), "setup: 建 symlink 失敗")

	app := newPNGTestApp(t, outputDir)
	result, err := app.DownloadCCIChart(CCIDownloadParams{ImageData: validPNGBase64DataURL(), Subject: "S1"})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, link, result.OutputPath)

	got, readErr := os.ReadFile(victim)
	require.NoError(t, readErr)
	assert.Equal(t, "untouched", string(got), "symlink 目標不可被改寫")

	info, lstatErr := os.Lstat(link)
	require.NoError(t, lstatErr)
	assert.True(t, info.Mode().IsRegular(), "連結應被取代成一般檔,mode=%v", info.Mode())

	written, readErr := os.ReadFile(link)
	require.NoError(t, readErr)
	assert.Equal(t, validPNGBytes(1, 1), written)
}

// TestDownloadPNG_SaveConfigToMissingOutputDir 釘住 bug:SaveConfig 把 OutputDir 換到尚不存在
// 的資料夾後,PNG 下載必須直接成功(建立目錄),不必重啟 App(只有 Startup 的
// EnsureDirectories 會建目錄)。CSV 經 Output placement 一向會 MkdirAll,PNG 原本沒有。
func TestDownloadPNG_SaveConfigToMissingOutputDir(t *testing.T) {
	// SaveConfig 會 i18n.SetLocale(cfg.Language) 改全域 locale。
	prevLocale := i18n.GetLocale()
	t.Cleanup(func() { i18n.SetLocale(prevLocale) })

	downloads := []struct {
		name     string
		wantName string
		download func(app *App) (*ChartResult, error)
	}{
		{
			name: "DownloadCCIChart", wantName: "S1_CCI_Rudolph.png",
			download: func(app *App) (*ChartResult, error) {
				return app.DownloadCCIChart(CCIDownloadParams{ImageData: validPNGBase64DataURL(), Subject: "S1"})
			},
		},
		{
			name: "DownloadChartComposerImage", wantName: "S1_chart_composer.png",
			download: func(app *App) (*ChartResult, error) {
				return app.DownloadChartComposerImage(&DownloadChartComposerImageParams{
					Base64Data: validPNGBase64DataURL(), Subject: "S1",
				})
			},
		},
	}

	for _, tc := range downloads {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			newConfig := func(outputDir string) *config.AppConfig {
				cfg := config.DefaultConfig()
				cfg.InputDir = root
				cfg.OperateDir = root
				cfg.OutputDir = outputDir
				return cfg
			}
			app := NewAppWithConfigPath(newConfig(root), "test", filepath.Join(root, "config.json"))

			missing := filepath.Join(root, "new_output")
			require.NoError(t, app.SaveConfig(newConfig(missing)))
			require.NoDirExists(t, missing, "前提:SaveConfig 不建立 OutputDir")

			result, err := tc.download(app)

			require.NoError(t, err, "SaveConfig 換到新資料夾後 PNG 下載必須成功")
			require.NotNil(t, result)
			assert.True(t, result.Success)
			assert.Equal(t, filepath.Join(missing, tc.wantName), result.OutputPath)
			written, readErr := os.ReadFile(result.OutputPath)
			require.NoError(t, readErr)
			assert.Equal(t, validPNGBytes(1, 1), written)
		})
	}
}

// assertDirEmpty 斷言 dir 內沒有任何檔案(被拒的下載不落檔、不留 tmp)。
func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "被拒的下載不得在 OutputDir 留下任何檔案")
}
