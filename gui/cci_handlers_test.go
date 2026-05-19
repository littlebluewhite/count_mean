package gui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/cci"
	"count_mean/internal/config"
	"count_mean/internal/logging"
)

// setupCCITestApp 構造僅含 AnalyzeCCI 所需依賴的最小 App,與 setupMuscleRatioTestApp
// 對稱 — 不啟動真實 phase_sync analyzer / Wails ctx,只專注於 handler 邊界行為。
func setupCCITestApp(t *testing.T) *App {
	t.Helper()

	cfg := &config.AppConfig{OutputDir: t.TempDir()}

	app := &App{
		logger:      logging.GetLogger("cci_test"),
		cciAnalyzer: cci.NewCCIAnalyzer(),
	}
	app.state.Store(&appState{config: cfg})

	return app
}

// TestCCIHandler_ErrorMessage_NoAbsolutePath 是 主守:CCI handler 在
// AnalyzeCCI fail path (analyzer 內部讀檔失敗、CSV export 失敗等) 把錯誤訊息
// 塞進 result.Message 時必須先過 redact.RedactForMessage — patient 不該在
// 前端看到 `/Volumes/xxx/patient_yyy/...` 之類絕對路徑。
//
// PoC:傳一個指向真實絕對路徑的 manifest 路徑(在 t.TempDir 之下,但路徑包含
// "/Users/..." 或 "/var/folders/..."),analyzer 內部讀檔失敗會把該路徑 wrap 進
// err.Error()。修法後 result.Message 不應含 system-root prefix。
//
// 注意:這條 test 不直接驗 redact pattern (那由 redact package test 覆蓋),
// 只驗 "handler 走過 redact path" 的 contract。
func TestCCIHandler_ErrorMessage_NoAbsolutePath(t *testing.T) {
	app := setupCCITestApp(t)

	// 構造會讓 analyzer 失敗的 params — manifest 是一個正當絕對路徑但檔不存在,
	// analyzer 內部 OpenFile 會回 fs.PathError,errStr 內含完整 absolute path。
	// 用 t.TempDir() 確保此路徑是真正含 /Users/ 或 /var/folders/ 等 system-root prefix。
	tempDir := t.TempDir()
	bogusManifest := tempDir + "/nonexistent_manifest.csv"
	bogusDataFolder := tempDir + "/nonexistent_data_folder"

	params := CCIParams{
		ManifestFile: bogusManifest,
		DataFolder:   bogusDataFolder,
		SubjectIndex: 0,
	}

	result, err := app.AnalyzeCCI(params)
	require.NoError(t, err, "AnalyzeCCI 應遵守 Go err 通道契約 — non-panic 失敗不該 propagate err")
	require.NotNil(t, result, "result 應永遠 non-nil(雙通道契約)")
	require.False(t, result.Success, "不存在 manifest 應觸發 failure path")

	// 核心斷言:result.Message 內不該含 system-root prefix。
	// tempDir 在 macOS 下通常是 /var/folders/...,Linux CI 是 /tmp/...,
	// Windows 是 C:\Users\...\Temp\... — 任一 prefix 出現都算 leak。
	leakyPrefixes := []string{
		"/Users/",
		"/home/",
		"/var/folders/",
		"/private/",
		"/Volumes/",
		"/mnt/",
		"/tmp/",
		`C:\Users\`,
		`C:\`,
	}
	for _, prefix := range leakyPrefixes {
		if strings.Contains(result.Message, prefix) {
			t.Errorf("result.Message leaks absolute path prefix %q:\n%s",
				prefix, result.Message)
		}
	}

	// 同時應保留非路徑語意 — 「分析失敗」前綴應該還在,否則 patient 完全
	// 不知道發生什麼。注意 message 內含「<redacted-path>」標誌或不含都可以
	// (redact 可能因 path 不在 known root prefix 而走 line-fallback),這裡
	// 主要驗 leak 不發生,而非驗 redact 標誌存在。
	if result.Message == "" {
		t.Error("result.Message 不該空 — 應保留 user-friendly 失敗原因")
	}
}
