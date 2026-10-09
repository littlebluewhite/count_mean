package synchronizer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// TestPhaseTimeline 用一張表釘住 [[Phase timeline]] 的三條規則:canonical 順序、
// 出現與否(parsers.GetPhaseValue)、換算公式。
//   - 力板時間:emg = force − (offset−1)/250
//   - motion-index(D/O):emg = (idx − offset)/250
func TestPhaseTimeline(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		points models.PhasePoints
		want   PhaseTimeline
	}{
		{
			name:   "全部 10 個分期點依 canonical 順序換算",
			offset: 26, // 力板 −0.100、motion-index −0.104
			points: models.PhasePoints{
				P0: models.MakeOpt(0.312), P1: models.MakeOpt(0.347), P2: models.MakeOpt(0.405),
				S: models.MakeOpt(0.523), C: models.MakeOpt(0.618), D: 177,
				T0: models.MakeOpt(0.781), T: models.MakeOpt(0.902), O: 277, L: models.MakeOpt(1.236),
			},
			want: PhaseTimeline{
				{models.PhaseP0, 0.212}, {models.PhaseP1, 0.247}, {models.PhaseP2, 0.305},
				{models.PhaseS, 0.423}, {models.PhaseC, 0.518}, {models.PhaseD, 0.604},
				{models.PhaseT0, 0.681}, {models.PhaseT, 0.802}, {models.PhaseO, 1.004},
				{models.PhaseL, 1.136},
			},
		},
		{
			name:   "未提供的分期點不在 timeline:OptFloat Set=false 與 motion-index 0 sentinel",
			offset: 26,
			points: models.PhasePoints{S: models.MakeOpt(0.523), L: models.MakeOpt(1.236)},
			want:   PhaseTimeline{{models.PhaseS, 0.423}, {models.PhaseL, 1.136}},
		},
		{
			name:   "力板 t=0 是已提供的值;換算可為負",
			offset: 100, // 力板 −0.396
			points: models.PhasePoints{
				P0: models.MakeOpt(0.0), P1: models.MakeOpt(0.4), P2: models.MakeOpt(1.0),
			},
			want: PhaseTimeline{{models.PhaseP0, -0.396}, {models.PhaseP1, 0.004}, {models.PhaseP2, 0.604}},
		},
		{
			name:   "motion-index 早於 offset 換算為負 EMG 秒數",
			offset: 100,
			points: models.PhasePoints{D: 50, O: 350},
			want:   PhaseTimeline{{models.PhaseD, -0.2}, {models.PhaseO, 1.0}},
		},
		{
			name:   "offset 0",
			offset: 0, // 力板 +0.004、motion-index −0
			points: models.PhasePoints{P0: models.MakeOpt(1.0), D: 250},
			want:   PhaseTimeline{{models.PhaseP0, 1.004}, {models.PhaseD, 1.0}},
		},
		{
			name:   "負 motion-index 視為未提供",
			offset: 26,
			points: models.PhasePoints{D: -1, O: 277},
			want:   PhaseTimeline{{models.PhaseO, 1.004}},
		},
		{
			name:   "motion-index 不設上限 guard(parser 已在 parse 階段擋 >1e9)",
			offset: 0,
			points: models.PhasePoints{D: parsers.MaxReasonableMotionIndex + 1},
			want:   PhaseTimeline{{models.PhaseD, 4_000_000.004}},
		},
		{
			name:   "沒有任何分期點",
			offset: 26,
			want:   PhaseTimeline{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &models.PhaseManifest{EMGMotionOffset: tt.offset, PhasePoints: tt.points}

			got := NewPhaseTimeline(m)

			require.Len(t, got, len(tt.want))
			for i := range tt.want {
				assert.Equal(t, tt.want[i].Phase, got[i].Phase, "index %d", i)
				assert.InDelta(t, tt.want[i].EMGTime, got[i].EMGTime, 1e-9, "%s", tt.want[i].Phase)
			}

			// At 與 timeline 內容一致:有就回秒數,沒有就 (0, false)。
			wantAt := make(map[models.PhasePoint]float64, len(tt.want))
			for _, pt := range tt.want {
				wantAt[pt.Phase] = pt.EMGTime
			}
			for _, p := range models.AllPhases() {
				sec, ok := got.At(p)
				wantSec, wantOK := wantAt[p]
				assert.Equal(t, wantOK, ok, "At(%s) presence", p)
				assert.InDelta(t, wantSec, sec, 1e-9, "At(%s)", p)
			}
		})
	}
}

// TestPhaseTimeline_AtUnknownPhase:不在 10 個合法值內的 PhasePoint 一律 (0, false)。
func TestPhaseTimeline_AtUnknownPhase(t *testing.T) {
	tl := PhaseTimeline{{models.PhaseS, 0.4}}

	sec, ok := tl.At(models.PhasePoint("X"))

	assert.False(t, ok)
	assert.Zero(t, sec)
}
