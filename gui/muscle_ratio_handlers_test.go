package gui

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
	"count_mean/internal/muscle_ratio"
)

// setupMuscleRatioTestApp 構造僅含 AnalyzeMuscleRatio 所需依賴的最小 App：
// logger、config（OutputDir）、muscleRatioAnalyzer。其他 field 留 nil，被觸及
// 時會立刻 panic 暴露問題。對稱 setupNormalizedPhaseSyncTestApp 的 pattern。
func setupMuscleRatioTestApp(t *testing.T) *App {
	t.Helper()

	cfg := &config.AppConfig{OutputDir: t.TempDir()}

	app := &App{
		logger:              logging.GetLogger("muscle_ratio_test"),
		muscleRatioAnalyzer: muscle_ratio.NewAnalyzer(),
	}
	app.state.Store(&appState{config: cfg})

	return app
}

// TestAnalyzeMuscleRatio_LocaleSwitchAffectsMessage 驗證 Phase 2 i18n 遷移：
// handler L72 fail-path message 經 i18n.T(KeyErrorMuscleRatioHandlerAnalysisFailed)
// 查表 — 不是 hard-code zh-TW。
//
// Phase 5 翻譯落地後升級：每個 locale 期望各自的 catalog 內容（distinct-output
// assertion），這也順帶守門「locale 切換確實產生不同 message」— 防 catalog
// 某 locale entry 不慎被改回 copy zh-TW 的 regression。
func TestAnalyzeMuscleRatio_LocaleSwitchAffectsMessage(t *testing.T) {
	require.NoError(t, i18n.InitI18n("./nonexistent"))

	prevLocale := i18n.GetLocale()
	t.Cleanup(func() { i18n.SetLocale(prevLocale) })

	app := setupMuscleRatioTestApp(t)

	// Fail path: 不存在的 manifest → muscle_ratio.Analyze return err
	// → handler L72 走 i18n.T(KeyErrorMuscleRatioHandlerAnalysisFailed, err)
	params := MuscleRatioParams{
		ManifestFile: filepath.Join(t.TempDir(), "nonexistent.csv"),
		DataFolder:   t.TempDir(),
	}

	cases := []struct {
		locale         i18n.Locale
		expectContains string
	}{
		{i18n.LocaleZhTW, "分析失敗"},
		{i18n.LocaleZhCN, "分析失败"},
		{i18n.LocaleEnUS, "Analysis failed"},
		{i18n.LocaleJaJP, "解析に失敗"},
	}

	for _, tc := range cases {
		t.Run(string(tc.locale), func(t *testing.T) {
			i18n.SetLocale(tc.locale)

			result, err := app.AnalyzeMuscleRatio(params)
			require.NoError(t, err)
			require.False(t, result.Success)

			// i18n lookup path 命中：message 不是 key 本身（fallback 行為）
			assert.NotEqual(t, i18n.KeyErrorMuscleRatioHandlerAnalysisFailed, result.Message,
				"locale %s: message 應從 catalog 查表，不應 fallback 到 key 本身", tc.locale)

			// Phase 5 catalog 內容應為各 locale 翻譯
			assert.Contains(t, result.Message, tc.expectContains,
				"locale %s: message 應含該 locale 的 catalog 翻譯 %q", tc.locale, tc.expectContains)
		})
	}
}
