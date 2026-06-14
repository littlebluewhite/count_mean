package gui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/phase_sync"
)

// setupPhaseSyncTestApp 構造僅含 AnalyzePhaseSync 所需依賴的最小 App,
// 與 setupCCITestApp / setupMuscleRatioTestApp 對稱。
func setupPhaseSyncTestApp(t *testing.T) *App {
	t.Helper()

	cfg := &config.AppConfig{OutputDir: t.TempDir()}

	app := &App{
		logger:            logging.GetLogger("phase_sync_test"),
		phaseSyncAnalyzer: phase_sync.NewPhaseSyncAnalyzer(),
	}
	app.state.Store(buildAppState(cfg))
	return app
}

// TestPhaseSyncHandler_ErrorMessage_NoAbsolutePath 是 主守:AnalyzePhaseSync handler
// 在 Execute fail path(analyzer 讀檔失敗)把錯誤訊息塞進 result.Message 時必須先過
// redact.RedactForMessage — patient 不該在前端看到絕對路徑。
//
// 用 t.TempDir() 確保 manifest 路徑含 system-root prefix;manifest 不存在,
// analyzer 內部 OpenFile 失敗會 wrap 完整 absolute path 進 err.Error()。
// 修法後 result.Message 不應含 system-root prefix。
func TestPhaseSyncHandler_ErrorMessage_NoAbsolutePath(t *testing.T) {
	app := setupPhaseSyncTestApp(t)

	tempDir := t.TempDir()
	bogusManifest := tempDir + "/nonexistent_manifest.csv"
	bogusDataFolder := tempDir + "/nonexistent_data_folder"

	params := PhaseSyncParams{
		ManifestFile: bogusManifest,
		DataFolder:   bogusDataFolder,
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseL,
		SubjectIndex: 0,
	}

	result, err := app.AnalyzePhaseSync(params)
	require.NoError(t, err, "AnalyzePhaseSync 應遵守 Go err 通道契約 — non-panic 失敗不該 propagate err")
	require.NotNil(t, result, "result 應永遠 non-nil(雙通道契約)")
	require.False(t, result.Success, "不存在 manifest 應觸發 failure path")

	// 核心斷言:result.Message 內不該含 system-root prefix。
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

	if result.Message == "" {
		t.Error("result.Message 不該空 — 應保留 user-friendly 失敗原因")
	}
}
