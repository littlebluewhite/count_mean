package synchronizer

import (
	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// PhaseTime 是單一分期點在 EMG 時間軸上的位置(秒)。
type PhaseTime struct {
	Phase   models.PhasePoint
	EMGTime float64
}

// PhaseTimeline 是一筆 manifest row 的 [[Phase timeline]]:已提供的分期點依
// models.AllPhases() 的 canonical 順序換算成 EMG 秒數,未提供的分期點不在其中。
//
// 它是「manifest row 分期點 → EMG 秒數」的唯一 owner(ADR-0042)。caller 只保留
// 各自的 policy:CCI 排除 P0–P2 並要求 S/L、muscle_ratio 檢查 in-range 後排序加中點、
// Chart Composer 全部渲染、phase_sync 取一對分期點。
type PhaseTimeline []PhaseTime

// NewPhaseTimeline 依 canonical 順序走過 10 個分期點:
//   - 出現與否:parsers.GetPhaseValue(力板時間 OptFloat Set=false、motion-index ≤ 0 皆為未提供)
//   - 換算:力板時間走 ForceTimeToEMGTime,motion-index(D/O)走 MotionIndexToEMGTime
//
// 不設 motion-index 上限 guard:D/O 是 int,parser 已在 parse 階段以
// parsers.MaxReasonableMotionIndex 擋下過大值。
func NewPhaseTimeline(m *models.PhaseManifest) PhaseTimeline {
	ts := NewTimeSynchronizer()
	phases := models.AllPhases()
	tl := make(PhaseTimeline, 0, len(phases))

	for _, p := range phases {
		// AllPhases 只含 10 個合法分期點,GetPhaseValue 的 error path 不可達。
		opt, _, _ := parsers.GetPhaseValue(&m.PhasePoints, p) //nolint:errcheck // unreachable error path
		v, ok := opt.Get()
		if !ok {
			continue
		}

		var emgTime float64
		if p.IsMotionIndex() {
			emgTime = ts.MotionIndexToEMGTime(int(v), m.EMGMotionOffset)
		} else {
			emgTime = ts.ForceTimeToEMGTime(v, m.EMGMotionOffset)
		}
		tl = append(tl, PhaseTime{Phase: p, EMGTime: emgTime})
	}

	return tl
}

// At 回傳分期點 p 的 EMG 秒數;p 未提供(或不是合法分期點)時回 (0, false)。
func (tl PhaseTimeline) At(p models.PhasePoint) (float64, bool) {
	for _, pt := range tl {
		if pt.Phase == p {
			return pt.EMGTime, true
		}
	}

	return 0, false
}
