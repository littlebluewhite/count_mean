package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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

// TestAnalyzePhaseSync_HappyPath 釘住 AnalyzePhaseSync 成功路徑的 envelope:
// (result, nil)、Success=true、Message="分析完成"、OutputPath 實際寫出、欄位取自
// analyzer 統計。fixture 沿用 normalized_phase_sync_handlers_test.go 的
// setupNormalizedPhaseSyncFixture(P0=0.1 / P2=0.3)。
func TestAnalyzePhaseSync_HappyPath(t *testing.T) {
	app := setupPhaseSyncTestApp(t)
	manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)

	result, err := app.AnalyzePhaseSync(PhaseSyncParams{
		ManifestFile: manifestPath,
		DataFolder:   dataFolder,
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseP2,
		SubjectIndex: 0,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "Message: %s", result.Message)
	assert.Equal(t, "分析完成", result.Message)
	assert.FileExists(t, result.OutputPath)
	assert.Equal(t, "TestSubject", result.Subject)
	assert.Equal(t, models.PhaseP0, result.StartPhase)
	assert.Equal(t, models.PhaseP2, result.EndPhase)
	assert.Equal(t, []string{"Ch1", "Ch2"}, result.ChannelNames)
	assert.Len(t, result.ChannelMeans, 2)
	assert.Len(t, result.ChannelMaxes, 2)
	assert.NotEmpty(t, result.Report)
}

// TestAnalyzePhaseSync_WriteFailure_FailedResultNilErr 釘住 AnalyzePhaseSync 的
// mixed 錯誤通道中「寫檔失敗」分支:analyzer 成功、CSV 寫出失敗 →
// (failedResult, nil),Message 以「導出失敗: 」開頭且不洩漏絕對路徑。
//
// 注入:OutputDir 指向一個一般檔案,atomic write 建目錄 / 落檔必失敗。
func TestAnalyzePhaseSync_WriteFailure_FailedResultNilErr(t *testing.T) {
	notADir := filepath.Join(t.TempDir(), "output_is_a_file")
	require.NoError(t, os.WriteFile(notADir, []byte("x"), 0o600))

	app := &App{
		logger:            logging.GetLogger("phase_sync_test"),
		phaseSyncAnalyzer: phase_sync.NewPhaseSyncAnalyzer(),
	}
	app.state.Store(buildAppState(&config.AppConfig{OutputDir: notADir}))

	manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)

	result, err := app.AnalyzePhaseSync(PhaseSyncParams{
		ManifestFile: manifestPath,
		DataFolder:   dataFolder,
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseP2,
		SubjectIndex: 0,
	})
	require.NoError(t, err, "寫檔失敗走 failed-result 通道,不走 Go err")
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.True(t, strings.HasPrefix(result.Message, "導出失敗: "),
		"寫檔失敗訊息前綴應為「導出失敗: 」,got %q", result.Message)
	assert.NotContains(t, result.Message, notADir, "Message 不可含絕對路徑")
	assert.Empty(t, result.OutputPath)
}
