package calculator

import (
	"fmt"
	"strings"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// Report formatting constants.
const reportSeparatorWidth = 52

// EMGStatisticsCalculator EMG 統計計算器.
type EMGStatisticsCalculator struct{}

// NewEMGStatisticsCalculator 創建新的 EMG 統計計算器.
func NewEMGStatisticsCalculator() *EMGStatisticsCalculator {
	return &EMGStatisticsCalculator{}
}

// CalculateStatistics 計算 EMG 數據的統計信息.
func (*EMGStatisticsCalculator) CalculateStatistics(
	emgData *models.PhaseSyncEMGData,
	params StatisticsParams,
) (*models.EMGStatistics, error) {
	// 驗證輸入數據
	if err := parsers.ValidateEMGData(emgData); err != nil {
		return nil, fmt.Errorf("EMG 數據驗證失敗: %w", err)
	}

	// 計算平均值和最大值
	means, maxes := parsers.CalculateEMGStatistics(emgData)

	// 創建統計結果
	stats := &models.EMGStatistics{
		Subject:      params.Subject,
		StartPhase:   params.StartPhase,
		StartTime:    params.StartTime,
		EndPhase:     params.EndPhase,
		EndTime:      params.EndTime,
		ChannelNames: emgData.Headers,
		ChannelMeans: means,
		ChannelMaxes: maxes,
	}

	return stats, nil
}

// StatisticsParams 統計參數.
type StatisticsParams struct {
	Subject    string
	StartPhase models.PhasePoint
	StartTime  float64
	EndPhase   models.PhasePoint
	EndTime    float64
}

// FormatStatisticsReport 格式化統計報告.
// 使用 strings.Builder 取代 `report += fmt.Sprintf(...)`，避免在迴圈內每次 reallocate。
func FormatStatisticsReport(stats *models.EMGStatistics) string {
	var sb strings.Builder
	sb.Grow(256 + len(stats.ChannelNames)*64)

	sb.WriteString("EMG 統計分析報告\n")
	sb.WriteString("================\n")
	fmt.Fprintf(&sb, "主題: %s\n", stats.Subject)
	fmt.Fprintf(&sb, "分析區間: %s (%.3fs) → %s (%.3fs)\n",
		stats.StartPhase, stats.StartTime,
		stats.EndPhase, stats.EndTime)
	fmt.Fprintf(&sb, "持續時間: %.3f 秒\n", stats.EndTime-stats.StartTime)
	fmt.Fprintf(&sb, "通道數量: %d\n\n", len(stats.ChannelNames))

	sb.WriteString("各通道統計結果:\n")
	fmt.Fprintf(&sb, "%-20s %15s %15s\n", "通道名稱", "平均值", "最大值")
	sb.WriteString(strings.Repeat("-", reportSeparatorWidth))
	sb.WriteByte('\n')

	for _, channelName := range stats.ChannelNames {
		mean := stats.ChannelMeans[channelName]
		maxVal := stats.ChannelMaxes[channelName]
		fmt.Fprintf(&sb, "%-20s %15.6f %15.6f\n", channelName, mean, maxVal)
	}

	return sb.String()
}
