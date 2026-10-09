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
// 與 setupMuscleRatioTestApp 對稱。
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
