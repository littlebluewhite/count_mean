// Package synchronizer provides time synchronization utilities for EMG, motion,
// and force plate data analysis. It handles phase calculation, time conversion
// between different sampling frequencies, and phase validation.
package synchronizer

import (
	"errors"
	"fmt"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// PhaseTypeMotion represents the motion phase type.
const PhaseTypeMotion = "motion"

// PhaseTypeForce represents the force phase type.
const PhaseTypeForce = "force"

// Phase validation errors.
var (
	// ErrPhaseValueZero indicates a phase point value is zero or not set.
	ErrPhaseValueZero = errors.New("phase value is zero or not set")
	// ErrUnknownPhase indicates an unknown phase point was specified.
	ErrUnknownPhase = errors.New("unknown phase point")
	// ErrPhaseOrderInvalid indicates the start phase is not before the end phase.
	ErrPhaseOrderInvalid = errors.New("start phase must be before end phase")
)

// PhaseCalculator 分期點計算器.
type PhaseCalculator struct {
	timeSynchronizer *TimeSynchronizer
}

// NewPhaseCalculator 創建新的分期點計算器.
func NewPhaseCalculator() *PhaseCalculator {
	return &PhaseCalculator{
		timeSynchronizer: NewTimeSynchronizer(),
	}
}

// GetPhaseTimeRange 根據開始和結束分期點，計算時間範圍.
//
//nolint:gocritic // hugeParam: phasePoints passed by value for API compatibility
func (pc *PhaseCalculator) GetPhaseTimeRange(
	phasePoints models.PhasePoints,
	startPhase models.PhasePoint,
	endPhase models.PhasePoint,
	emgMotionOffset int,
) (*models.PhaseTimeRange, error) {
	// 獲取開始分期點的值和類型
	startValue, startIsMotionIndex, err := parsers.GetPhaseValue(&phasePoints, startPhase)
	if err != nil {
		return nil, fmt.Errorf("獲取開始分期點 %s 失敗: %w", startPhase, err)
	}

	// 獲取結束分期點的值和類型
	endValue, endIsMotionIndex, err := parsers.GetPhaseValue(&phasePoints, endPhase)
	if err != nil {
		return nil, fmt.Errorf("獲取結束分期點 %s 失敗: %w", endPhase, err)
	}

	// 檢查值的有效性
	if startValue == 0 {
		return nil, fmt.Errorf("開始分期點 %s: %w", startPhase, ErrPhaseValueZero)
	}

	if endValue == 0 {
		return nil, fmt.Errorf("結束分期點 %s: %w", endPhase, ErrPhaseValueZero)
	}

	// 計算同步的時間範圍
	syncedRange, err := pc.timeSynchronizer.GetSyncedTimeRange(
		startValue, startIsMotionIndex,
		endValue, endIsMotionIndex,
		emgMotionOffset,
	)
	if err != nil {
		return nil, fmt.Errorf("計算同步時間範圍失敗: %w", err)
	}

	// 創建結果
	startType := PhaseTypeForce
	if startIsMotionIndex {
		startType = PhaseTypeMotion
	}

	endType := PhaseTypeForce
	if endIsMotionIndex {
		endType = PhaseTypeMotion
	}

	result := &models.PhaseTimeRange{
		StartTime: syncedRange.StartEMGTime,
		EndTime:   syncedRange.EndEMGTime,
		StartType: startType,
		EndType:   endType,
	}

	return result, nil
}

// ValidatePhaseOrder 驗證分期點的順序.
func (*PhaseCalculator) ValidatePhaseOrder(startPhase, endPhase models.PhasePoint) error {
	if !startPhase.IsValid() {
		return fmt.Errorf("開始分期點 %s: %w", startPhase, ErrUnknownPhase)
	}

	if !endPhase.IsValid() {
		return fmt.Errorf("結束分期點 %s: %w", endPhase, ErrUnknownPhase)
	}

	if startPhase.Order() >= endPhase.Order() {
		return fmt.Errorf("開始分期點 %s 與結束分期點 %s: %w", startPhase, endPhase, ErrPhaseOrderInvalid)
	}

	return nil
}

// GetAvailableStartPhases 獲取可用的開始分期點.
func GetAvailableStartPhases() []models.PhasePoint {
	return models.AllPhases()
}

// GetAvailableEndPhases 獲取可用的結束分期點.
// 結束分期點不能是第一個 (P0)，因為必須在開始分期點之後。
func GetAvailableEndPhases() []models.PhasePoint {
	all := models.AllPhases()
	return all[1:]
}
