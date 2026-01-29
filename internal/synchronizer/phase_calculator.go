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

// Phase order constants for validation.
const (
	phaseOrderP0 = 1
	phaseOrderP1 = 2
	phaseOrderP2 = 3
	phaseOrderS  = 4
	phaseOrderC  = 5
	phaseOrderD  = 6
	phaseOrderT0 = 7
	phaseOrderT  = 8
	phaseOrderO  = 9
	phaseOrderL  = 10
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
	startPhase string,
	endPhase string,
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
func (*PhaseCalculator) ValidatePhaseOrder(startPhase, endPhase string) error {
	// 定義分期點的順序
	phaseOrder := map[string]int{
		"P0": phaseOrderP0,
		"P1": phaseOrderP1,
		"P2": phaseOrderP2,
		"S":  phaseOrderS,
		"C":  phaseOrderC,
		"D":  phaseOrderD,
		"T0": phaseOrderT0,
		"T":  phaseOrderT,
		"O":  phaseOrderO,
		"L":  phaseOrderL,
	}

	startOrder, startExists := phaseOrder[startPhase]
	endOrder, endExists := phaseOrder[endPhase]

	if !startExists {
		return fmt.Errorf("開始分期點 %s: %w", startPhase, ErrUnknownPhase)
	}

	if !endExists {
		return fmt.Errorf("結束分期點 %s: %w", endPhase, ErrUnknownPhase)
	}

	if startOrder >= endOrder {
		return fmt.Errorf("開始分期點 %s 與結束分期點 %s: %w", startPhase, endPhase, ErrPhaseOrderInvalid)
	}

	return nil
}

// GetAvailableStartPhases 獲取可用的開始分期點.
func GetAvailableStartPhases() []string {
	return []string{"P0", "P1", "P2", "S", "C", "D", "T0", "T", "O", "L"}
}

// GetAvailableEndPhases 獲取可用的結束分期點.
func GetAvailableEndPhases() []string {
	return []string{"P1", "P2", "S", "C", "D", "T0", "T", "O", "L"}
}

// PhaseInfo 分期點信息.
type PhaseInfo struct {
	Name        string
	Description string
	Type        string // "force" or "motion"
}

// GetPhaseInfo 獲取分期點的詳細信息.
func GetPhaseInfo() []PhaseInfo {
	return []PhaseInfo{
		{Name: "P0", Description: "準備期開始", Type: PhaseTypeForce},
		{Name: "P1", Description: "準備期第一階段", Type: PhaseTypeForce},
		{Name: "P2", Description: "準備期第二階段", Type: PhaseTypeForce},
		{Name: "S", Description: "啟動瞬間", Type: PhaseTypeForce},
		{Name: "C", Description: "下蹲加速減速轉換瞬間", Type: PhaseTypeForce},
		{Name: "D", Description: "下蹲結束時間", Type: PhaseTypeMotion},
		{Name: "T0", Description: "正沖涼結束時間", Type: PhaseTypeForce},
		{Name: "T", Description: "起跳瞬間", Type: PhaseTypeForce},
		{Name: "O", Description: "展體轉間", Type: PhaseTypeMotion},
		{Name: "L", Description: "著地瞬間", Type: PhaseTypeForce},
	}
}

// FormatPhaseTime 格式化分期點時間顯示.
func FormatPhaseTime(value float64, phaseType string) string {
	if phaseType == PhaseTypeMotion {
		return fmt.Sprintf("Index: %d", int(value))
	}

	return fmt.Sprintf("%.3f 秒", value)
}
