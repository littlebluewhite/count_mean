package calculator

import (
	"encoding/csv"
	"fmt"
	"os"
	"strings"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// EMGStatisticsCalculator EMG 統計計算器
type EMGStatisticsCalculator struct {
	precision int // 小數位數精度
}

// NewEMGStatisticsCalculator 創建新的 EMG 統計計算器
func NewEMGStatisticsCalculator(precision int) *EMGStatisticsCalculator {
	if precision <= 0 {
		precision = 6
	}
	return &EMGStatisticsCalculator{
		precision: precision,
	}
}

// CalculateStatistics 計算 EMG 數據的統計信息
func (calc *EMGStatisticsCalculator) CalculateStatistics(
	emgData *models.PhaseSyncEMGData,
	startPhase string,
	startTime float64,
	endPhase string,
	endTime float64,
	subject string,
) (*models.EMGStatistics, error) {

	// 驗證輸入數據
	if err := parsers.ValidateEMGData(emgData); err != nil {
		return nil, fmt.Errorf("EMG 數據驗證失敗: %w", err)
	}

	// 計算平均值和最大值
	means, maxes := parsers.CalculateEMGStatistics(emgData)

	// 創建統計結果
	stats := &models.EMGStatistics{
		Subject:      subject,
		StartPhase:   startPhase,
		StartTime:    startTime,
		EndPhase:     endPhase,
		EndTime:      endTime,
		ChannelNames: emgData.Headers,
		ChannelMeans: means,
		ChannelMaxes: maxes,
	}

	return stats, nil
}

// ExportToCSV 將統計結果導出為 CSV 檔案
func (calc *EMGStatisticsCalculator) ExportToCSV(
	stats *models.EMGStatistics,
	outputPath string,
) error {

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("無法創建輸出檔案 %s: %w", outputPath, err)
	}
	defer file.Close()

	// 寫入 UTF-8 BOM 以確保 Excel 正確顯示
	bomBytes := []byte{0xEF, 0xBB, 0xBF}
	if _, err := file.Write(bomBytes); err != nil {
		return fmt.Errorf("無法寫入 BOM: %w", err)
	}

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 準備標題行
	headers := []string{""}
	headers = append(headers, stats.ChannelNames...)
	if err := writer.Write(headers); err != nil {
		return fmt.Errorf("寫入標題行失敗: %w", err)
	}

	// 寫入開始分期點
	row := []string{"開始分期點"}
	for range stats.ChannelNames {
		row = append(row, stats.StartPhase)
	}
	if err := writer.Write(row); err != nil {
		return fmt.Errorf("寫入開始分期點失敗: %w", err)
	}

	// 寫入開始時間
	row = []string{"開始時間"}
	startTimeStr := calc.formatFloat(stats.StartTime)
	for range stats.ChannelNames {
		row = append(row, startTimeStr)
	}
	if err := writer.Write(row); err != nil {
		return fmt.Errorf("寫入開始時間失敗: %w", err)
	}

	// 寫入結束分期點
	row = []string{"結束分期點"}
	for range stats.ChannelNames {
		row = append(row, stats.EndPhase)
	}
	if err := writer.Write(row); err != nil {
		return fmt.Errorf("寫入結束分期點失敗: %w", err)
	}

	// 寫入結束時間
	row = []string{"結束時間"}
	endTimeStr := calc.formatFloat(stats.EndTime)
	for range stats.ChannelNames {
		row = append(row, endTimeStr)
	}
	if err := writer.Write(row); err != nil {
		return fmt.Errorf("寫入結束時間失敗: %w", err)
	}

	// 寫入平均值
	row = []string{"平均值"}
	for _, channelName := range stats.ChannelNames {
		meanValue := stats.ChannelMeans[channelName]
		row = append(row, calc.formatFloat(meanValue))
	}
	if err := writer.Write(row); err != nil {
		return fmt.Errorf("寫入平均值失敗: %w", err)
	}

	// 寫入最大值
	row = []string{"最大值"}
	for _, channelName := range stats.ChannelNames {
		maxValue := stats.ChannelMaxes[channelName]
		row = append(row, calc.formatFloat(maxValue))
	}
	if err := writer.Write(row); err != nil {
		return fmt.Errorf("寫入最大值失敗: %w", err)
	}

	return nil
}

// formatFloat 格式化浮點數
func (calc *EMGStatisticsCalculator) formatFloat(value float64) string {
	format := fmt.Sprintf("%%.%df", calc.precision)
	return fmt.Sprintf(format, value)
}

// GenerateOutputFileName 生成輸出檔案名
func GenerateOutputFileName(subject string, startPhase string, endPhase string) string {
	// 移除特殊字符
	safeSubject := sanitizeFileName(subject)

	// 生成檔案名: Subject_StartPhase-EndPhase_statistics.csv
	return fmt.Sprintf("%s_%s-%s_statistics.csv", safeSubject, startPhase, endPhase)
}

// sanitizeFileName 清理檔案名中的特殊字符
func sanitizeFileName(name string) string {
	// 替換不安全的字符
	replacements := map[rune]rune{
		'/':  '_',
		'\\': '_',
		':':  '_',
		'*':  '_',
		'?':  '_',
		'"':  '_',
		'<':  '_',
		'>':  '_',
		'|':  '_',
		' ':  '_',
	}

	result := make([]rune, 0, len(name))
	for _, ch := range name {
		if replacement, ok := replacements[ch]; ok {
			result = append(result, replacement)
		} else {
			result = append(result, ch)
		}
	}

	return string(result)
}

// ValidateStatisticsParams 驗證統計參數
func ValidateStatisticsParams(params *StatisticsParams) error {
	if params.StartTime < 0 {
		return fmt.Errorf("開始時間不能為負數: %.3f", params.StartTime)
	}

	if params.EndTime < 0 {
		return fmt.Errorf("結束時間不能為負數: %.3f", params.EndTime)
	}

	if params.StartTime >= params.EndTime {
		return fmt.Errorf("開始時間 (%.3f) 必須小於結束時間 (%.3f)",
			params.StartTime, params.EndTime)
	}

	if params.Subject == "" {
		return fmt.Errorf("主題名稱不能為空")
	}

	return nil
}

// StatisticsParams 統計參數
type StatisticsParams struct {
	Subject    string
	StartPhase string
	StartTime  float64
	EndPhase   string
	EndTime    float64
}

// FormatStatisticsReport 格式化統計報告
func FormatStatisticsReport(stats *models.EMGStatistics) string {
	report := fmt.Sprintf("EMG 統計分析報告\n")
	report += fmt.Sprintf("================\n")
	report += fmt.Sprintf("主題: %s\n", stats.Subject)
	report += fmt.Sprintf("分析區間: %s (%.3fs) → %s (%.3fs)\n",
		stats.StartPhase, stats.StartTime,
		stats.EndPhase, stats.EndTime)
	report += fmt.Sprintf("持續時間: %.3f 秒\n", stats.EndTime-stats.StartTime)
	report += fmt.Sprintf("通道數量: %d\n\n", len(stats.ChannelNames))

	report += fmt.Sprintf("各通道統計結果:\n")
	report += fmt.Sprintf("%-20s %15s %15s\n", "通道名稱", "平均值", "最大值")
	report += fmt.Sprintf("%s\n", strings.Repeat("-", 52))

	for _, channelName := range stats.ChannelNames {
		mean := stats.ChannelMeans[channelName]
		max := stats.ChannelMaxes[channelName]
		report += fmt.Sprintf("%-20s %15.6f %15.6f\n", channelName, mean, max)
	}

	return report
}
