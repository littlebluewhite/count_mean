package phase_sync //nolint:revive // underscore in package name matches directory structure

// computePhaseSync_test.go — compute core 的 file-free 測試(ADR-0047,仿 ADR-0024 的
// computeCCI_test.go):EMG 與 manifest row 都在記憶體內組出,不碰 disk。
//
// EMGMotionOffset=26:力板 emg = force − (26−1)/250 = force − 0.1;motion-index
// emg = (idx − 26)/250。offset=1 會讓兩者相等,斷言就分辨不出換算有沒有接上。

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/models"
	"count_mean/internal/synchronizer"
)

// buildComputePhaseSyncFixture 組出 101 筆、100 Hz 的 EMG(t = i × 0.01,i = 0..100)與
// manifest row:
//   - "RA" = i(ramp),"ES" = 2(常數)
//   - S = 0.5、L = 0.9(力板)→ EMG 0.4 / 0.8
//   - D = 126、O = 226(motion-index)→ EMG 0.4 / 0.8,與 S / L 同一個視窗
//
// 視窗 [0.4, 0.8] 切到 i = 40..80(41 筆):RA mean 60、max 80;ES mean 2、max 2。
func buildComputePhaseSyncFixture() (*models.PhaseSyncEMGData, *models.PhaseManifest) {
	const n = 101
	times := make([]float64, n)
	ra := make([]float64, n)
	es := make([]float64, n)
	for i := range times {
		times[i] = float64(i) * 0.01
		ra[i] = float64(i)
		es[i] = 2
	}

	emgData := &models.PhaseSyncEMGData{
		Time:     times,
		Headers:  []string{"RA", "ES"},
		Channels: map[string][]float64{"RA": ra, "ES": es},
	}
	m := &models.PhaseManifest{
		Subject:         "TST",
		EMGMotionOffset: 26,
		PhasePoints: models.PhasePoints{
			S: models.MakeOpt(0.5),
			L: models.MakeOpt(0.9),
			D: 126,
			O: 226,
		},
	}

	return emgData, m
}

// TestComputePhaseSync_StatsOverPhaseWindow:分期點經 [[Phase timeline]] 換成 EMG 秒數 →
// 切片 → 統計;力板(S, L)與 motion-index(D, O)兩域落在同一視窗,結果相同。
func TestComputePhaseSync_StatsOverPhaseWindow(t *testing.T) {
	for _, pair := range [][2]models.PhasePoint{
		{models.PhaseS, models.PhaseL},
		{models.PhaseD, models.PhaseO},
	} {
		t.Run(string(pair[0])+"-"+string(pair[1]), func(t *testing.T) {
			emgData, m := buildComputePhaseSyncFixture()

			got, err := NewPhaseSyncAnalyzer().computePhaseSync(context.Background(), emgData, m, pair[0], pair[1])
			require.NoError(t, err)

			assert.Equal(t, "TST", got.Subject)
			assert.Equal(t, pair[0], got.StartPhase)
			assert.Equal(t, pair[1], got.EndPhase)
			assert.InDelta(t, 0.4, got.StartTime, 1e-9, "切片第一筆 sample 時間")
			assert.InDelta(t, 0.8, got.EndTime, 1e-9, "切片最後一筆 sample 時間")
			assert.Equal(t, []string{"RA", "ES"}, got.ChannelNames)
			assert.InDelta(t, 60.0, got.ChannelMeans["RA"], 1e-9)
			assert.InDelta(t, 80.0, got.ChannelMaxes["RA"], 1e-9)
			assert.InDelta(t, 2.0, got.ChannelMeans["ES"], 1e-9)
			assert.InDelta(t, 2.0, got.ChannelMaxes["ES"], 1e-9)
		})
	}
}

// TestComputePhaseSync_PreCancelledContext:取消的 ctx 在任何計算之前 bail,錯誤原樣是
// ctx.Err()(不包成 *AnalysisError),caller 據此辨識取消。取代原本以 test hook 卡住
// EMG parser 的 in-flight 取消測試。
func TestComputePhaseSync_PreCancelledContext(t *testing.T) {
	emgData, m := buildComputePhaseSyncFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := NewPhaseSyncAnalyzer().computePhaseSync(ctx, emgData, m, models.PhaseS, models.PhaseL)

	assert.Nil(t, got)
	require.ErrorIs(t, err, context.Canceled)
	var stageErr *AnalysisError
	assert.NotErrorAs(t, err, &stageErr, "ctx 取消不帶 Stage")
}

// TestComputePhaseSync_FailureStage:三個步驟的失敗各帶自己的 Stage,Error() 與 cause
// 逐字相同(不加前綴)—— NPS 的 gui 依 Stage 選訊息前綴,AnalyzePhaseSync 自己補前綴。
func TestComputePhaseSync_FailureStage(t *testing.T) {
	cases := []struct {
		name      string
		mutate    func(emgData *models.PhaseSyncEMGData, m *models.PhaseManifest)
		wantStage Stage
		wantErr   error
	}{
		{
			name:      "區間解析:結束分期點未提供",
			mutate:    func(_ *models.PhaseSyncEMGData, m *models.PhaseManifest) { m.PhasePoints.L = models.OptFloat{} },
			wantStage: StageStatsRange,
			wantErr:   ErrPhaseValueZero,
		},
		{
			name: "切片:視窗落在 EMG 內但兩筆 sample 之間",
			mutate: func(emgData *models.PhaseSyncEMGData, m *models.PhaseManifest) {
				emgData.Time = []float64{0.0, 0.5, 1.0}
				emgData.Channels = map[string][]float64{"RA": {1, 2, 3}, "ES": {1, 2, 3}}
				m.PhasePoints.S = models.MakeOpt(0.21) // EMG 0.11
				m.PhasePoints.L = models.MakeOpt(0.31) // EMG 0.21
			},
			wantStage: StageStatsSlice,
			wantErr:   synchronizer.ErrTimeRangeNotFound,
		},
		{
			name: "統計:視窗內有 NaN",
			mutate: func(emgData *models.PhaseSyncEMGData, _ *models.PhaseManifest) {
				emgData.Channels["RA"][60] = math.NaN()
			},
			wantStage: StageStatistics,
			wantErr:   calcerrors.ErrNaNInChannel,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emgData, m := buildComputePhaseSyncFixture()
			tc.mutate(emgData, m)

			got, err := NewPhaseSyncAnalyzer().computePhaseSync(context.Background(), emgData, m, models.PhaseS, models.PhaseL)

			assert.Nil(t, got)
			var stageErr *AnalysisError
			require.ErrorAs(t, err, &stageErr)
			assert.Equal(t, tc.wantStage, stageErr.Stage)
			assert.Equal(t, stageErr.Err.Error(), err.Error(), "Error() 與 cause 逐字相同")
			assert.ErrorIs(t, err, tc.wantErr)
		})
	}
}
