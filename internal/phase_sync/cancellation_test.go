package phase_sync //nolint:revive // underscore in package name matches directory structure

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

// TestAnalyzePhaseSync_PreCancelledContextReturnsErr 鎖定 Wave 7 review
// api-designer P2 的修補：phaseSyncAnalyzer.AnalyzePhaseSync 收 ctx 後，預先
// cancel 的 ctx 必須在 file load 之前就 bail，避免長運算被使用者 cancel 後
// 還要等 IO 完成。
//
// compute core 入口的取消由其 pre-cancel 測試釘住(computePhaseSync_test.go,
// ADR-0047):原本以 test hook 注入阻塞 parser 的 in-flight 測試隨 hook 一併刪除。
func TestAnalyzePhaseSync_PreCancelledContextReturnsErr(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()
	require.NotNil(t, analyzer)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 故意給一個不存在的 manifest path — 如果 ctx 檢查沒生效，會先 fail 在 file
	// load 上（不同錯誤訊息）。ctx.Err() 檢查若正常會比 file load 更早 return
	// context.Canceled。
	params := &models.AnalysisParams{
		ManifestFile: "/nonexistent/manifest.csv",
		DataFolder:   "/nonexistent",
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseP1,
		SubjectIndex: 0,
	}

	_, err := analyzer.AnalyzePhaseSync(ctx, params)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "pre-cancelled ctx 應在 file load 前 bail")
}

// TestAnalyzeNormalizedPhaseSync_PreCancelledContextReturnsErr:NPS 入口同樣在任何
// I/O 之前檢查 ctx;取消錯誤原樣回傳(不包成 *AnalysisError),gui 據此選「分析已取消」。
func TestAnalyzeNormalizedPhaseSync_PreCancelledContextReturnsErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewPhaseSyncAnalyzer().AnalyzeNormalizedPhaseSync(ctx, &NormalizedParams{
		ManifestFile:    "/nonexistent/manifest.csv",
		DataFolder:      "/nonexistent",
		NormStartPhase:  models.PhaseP0,
		NormEndPhase:    models.PhaseP1,
		StatsStartPhase: models.PhaseP0,
		StatsEndPhase:   models.PhaseP1,
	})

	require.ErrorIs(t, err, context.Canceled)
	var stageErr *AnalysisError
	assert.NotErrorAs(t, err, &stageErr, "ctx 取消不帶 Stage")
}
