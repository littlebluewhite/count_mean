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
