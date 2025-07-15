package synchronizer

import (
	"fmt"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// PhaseCalculator 分期點計算器
type PhaseCalculator struct {
	timeSynchronizer *TimeSynchronizer
}

// NewPhaseCalculator 創建新的分期點計算器
func NewPhaseCalculator() *PhaseCalculator {
	return &PhaseCalculator{
		timeSynchronizer: NewTimeSynchronizer(),
	}
}

// GetPhaseTimeRange 根據開始和結束分期點，計算時間範圍
func (pc *PhaseCalculator) GetPhaseTimeRange(
	phasePoints models.PhasePoints,
	startPhase string,
	endPhase string,
	emgMotionOffset int,
) (*models.PhaseTimeRange, error) {

	// 獲取開始分期點的值和類型
	startValue, startIsMotionIndex, err := parsers.GetPhaseValue(phasePoints, startPhase)
	if err != nil {
		return nil, fmt.Errorf("獲取開始分期點 %s 失敗: %w", startPhase, err)
	}

	// 獲取結束分期點的值和類型
	endValue, endIsMotionIndex, err := parsers.GetPhaseValue(phasePoints, endPhase)
	if err != nil {
		return nil, fmt.Errorf("獲取結束分期點 %s 失敗: %w", endPhase, err)
	}

	// 檢查值的有效性
	if startValue == 0 {
		return nil, fmt.Errorf("開始分期點 %s 的值為 0 或未設置", startPhase)
	}
	if endValue == 0 {
		return nil, fmt.Errorf("結束分期點 %s 的值為 0 或未設置", endPhase)
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
	result := &models.PhaseTimeRange{
		StartTime: syncedRange.StartEMGTime,
		EndTime:   syncedRange.EndEMGTime,
		StartType: pc.getPhaseType(startIsMotionIndex),
		EndType:   pc.getPhaseType(endIsMotionIndex),
	}

	return result, nil
}

// getPhaseType 根據是否為 Motion index 返回類型字符串
func (pc *PhaseCalculator) getPhaseType(isMotionIndex bool) string {
	if isMotionIndex {
		return "motion"
	}
	return "force"
}

// ValidatePhaseOrder 驗證分期點的順序
func (pc *PhaseCalculator) ValidatePhaseOrder(startPhase, endPhase string) error {
	// 定義分期點的順序
	phaseOrder := map[string]int{
		"P0": 1,
		"P1": 2,
		"P2": 3,
		"S":  4,
		"C":  5,
		"D":  6,
		"T0": 7,
		"T":  8,
		"O":  9,
		"L":  10,
	}

	startOrder, startExists := phaseOrder[startPhase]
	endOrder, endExists := phaseOrder[endPhase]

	if !startExists {
		return fmt.Errorf("未知的開始分期點: %s", startPhase)
	}
	if !endExists {
		return fmt.Errorf("未知的結束分期點: %s", endPhase)
	}

	if startOrder >= endOrder {
		return fmt.Errorf("開始分期點 %s 必須在結束分期點 %s 之前", startPhase, endPhase)
	}

	return nil
}

// GetAvailableStartPhases 獲取可用的開始分期點
func GetAvailableStartPhases() []string {
	return []string{"P0", "P1", "P2", "S", "C", "D", "T0", "T", "O", "L"}
}

// GetAvailableEndPhases 獲取可用的結束分期點
func GetAvailableEndPhases() []string {
	return []string{"P1", "P2", "S", "C", "D", "T0", "T", "O", "L"}
}

// PhaseInfo 分期點信息
type PhaseInfo struct {
	Name        string
	Description string
	Type        string // "force" or "motion"
}

// GetPhaseInfo 獲取分期點的詳細信息
func GetPhaseInfo() []PhaseInfo {
	return []PhaseInfo{
		{Name: "P0", Description: "準備期開始", Type: "force"},
		{Name: "P1", Description: "準備期第一階段", Type: "force"},
		{Name: "P2", Description: "準備期第二階段", Type: "force"},
		{Name: "S", Description: "啟動瞬間", Type: "force"},
		{Name: "C", Description: "下蹲加速減速轉換瞬間", Type: "force"},
		{Name: "D", Description: "下蹲結束時間", Type: "motion"},
		{Name: "T0", Description: "正沖涼結束時間", Type: "force"},
		{Name: "T", Description: "起跳瞬間", Type: "force"},
		{Name: "O", Description: "展體轉間", Type: "motion"},
		{Name: "L", Description: "著地瞬間", Type: "force"},
	}
}

// FormatPhaseTime 格式化分期點時間顯示
func FormatPhaseTime(value float64, isMotionIndex bool) string {
	if isMotionIndex {
		return fmt.Sprintf("Index: %d", int(value))
	}
	return fmt.Sprintf("%.3f 秒", value)
}
