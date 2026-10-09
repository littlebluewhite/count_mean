// Package synchronizer provides time synchronization utilities for EMG, motion,
// and force plate data analysis. It owns the phase timeline (manifest-row phase
// points → EMG seconds), the EMG time-index seam, and phase-order validation.
package synchronizer

import (
	"errors"
	"fmt"

	"count_mean/internal/models"
)

// Phase validation errors.
var (
	// ErrUnknownPhase indicates an unknown phase point was specified.
	ErrUnknownPhase = errors.New("unknown phase point")
	// ErrPhaseOrderInvalid indicates the start phase is not before the end phase.
	ErrPhaseOrderInvalid = errors.New("start phase must be before end phase")
)

// PhaseCalculator 分期點計算器.
type PhaseCalculator struct{}

// NewPhaseCalculator 創建新的分期點計算器.
func NewPhaseCalculator() *PhaseCalculator {
	return &PhaseCalculator{}
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
