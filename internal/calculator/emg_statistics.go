package calculator

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strings"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// Report formatting constants.
const reportSeparatorWidth = 52

// Statistics validation errors.
var (
	// ErrNegativeStartTime indicates a negative start time.
	ErrNegativeStartTime = errors.New("start time cannot be negative")
	// ErrNegativeEndTime indicates a negative end time.
	ErrNegativeEndTime = errors.New("end time cannot be negative")
	// ErrStartTimeNotBeforeEnd indicates start time is not before end time.
	ErrStartTimeNotBeforeEnd = errors.New("start time must be less than end time")
	// ErrEmptySubject indicates an empty subject name.
	ErrEmptySubject = errors.New("subject name cannot be empty")
)

// EMGStatisticsCalculator EMG 統計計算器.
type EMGStatisticsCalculator struct {
	precision int // 小數位數精度
}

// NewEMGStatisticsCalculator 創建新的 EMG 統計計算器.
func NewEMGStatisticsCalculator(precision int) *EMGStatisticsCalculator {
	if precision <= 0 {
		precision = 6
	}

	return &EMGStatisticsCalculator{
		precision: precision,
	}
}

// CalculateStatistics 計算 EMG 數據的統計信息.
func (*EMGStatisticsCalculator) CalculateStatistics(
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

// buildUniformRow creates a row with the same value repeated for all channels.
func buildUniformRow(label, value string, channelCount int) []string {
	row := make([]string, 0, channelCount+1)
	row = append(row, label)

	for i := 0; i < channelCount; i++ {
		row = append(row, value)
	}

	return row
}

// buildChannelValueRow creates a row with values from a channel map.
func buildChannelValueRow(
	label string,
	channelNames []string,
	values map[string]float64,
	calc *EMGStatisticsCalculator,
) []string {
	row := make([]string, 0, len(channelNames)+1)
	row = append(row, label)

	for _, name := range channelNames {
		row = append(row, calc.formatFloat(values[name]))
	}

	return row
}

// writeCSVWithBOM creates a file with UTF-8 BOM and returns a CSV writer.
func writeCSVWithBOM(outputPath string) (*os.File, *csv.Writer, error) {
	file, err := os.Create(outputPath) //nolint:gosec // G304: outputPath is validated by caller
	if err != nil {
		return nil, nil, fmt.Errorf("無法創建輸出檔案 %s: %w", outputPath, err)
	}

	bomBytes := []byte{0xEF, 0xBB, 0xBF}
	if _, err := file.Write(bomBytes); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}

		return nil, nil, fmt.Errorf("無法寫入 BOM: %w", err)
	}

	return file, csv.NewWriter(file), nil
}

// ExportToCSV 將統計結果導出為 CSV 檔案.
func (calc *EMGStatisticsCalculator) ExportToCSV(
	stats *models.EMGStatistics,
	outputPath string,
) error {
	file, writer, err := writeCSVWithBOM(outputPath)
	if err != nil {
		return err
	}

	defer func() {
		writer.Flush()

		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	channelCount := len(stats.ChannelNames)

	// Define all rows to write
	rows := []struct {
		row      []string
		errorMsg string
	}{
		{append([]string{""}, stats.ChannelNames...), "寫入標題行失敗"},
		{buildUniformRow("開始分期點", stats.StartPhase, channelCount), "寫入開始分期點失敗"},
		{buildUniformRow("開始時間", calc.formatFloat(stats.StartTime), channelCount), "寫入開始時間失敗"},
		{buildUniformRow("結束分期點", stats.EndPhase, channelCount), "寫入結束分期點失敗"},
		{buildUniformRow("結束時間", calc.formatFloat(stats.EndTime), channelCount), "寫入結束時間失敗"},
		{buildUniformRow("時間差值", calc.formatFloat(stats.EndTime-stats.StartTime), channelCount), "寫入時間差值失敗"},
		{buildChannelValueRow("平均值", stats.ChannelNames, stats.ChannelMeans, calc), "寫入平均值失敗"},
		{buildChannelValueRow("最大值", stats.ChannelNames, stats.ChannelMaxes, calc), "寫入最大值失敗"},
	}

	for _, r := range rows {
		if err := writer.Write(r.row); err != nil {
			return fmt.Errorf("%s: %w", r.errorMsg, err)
		}
	}

	return nil
}

// formatFloat 格式化浮點數.
func (calc *EMGStatisticsCalculator) formatFloat(value float64) string {
	format := fmt.Sprintf("%%.%df", calc.precision)
	return fmt.Sprintf(format, value)
}

// GenerateOutputFileName 生成輸出檔案名.
func GenerateOutputFileName(subject, startPhase, endPhase string) string {
	// 移除特殊字符
	safeSubject := sanitizeFileName(subject)

	// 生成檔案名: Subject_StartPhase-EndPhase_statistics.csv
	return fmt.Sprintf("%s_%s-%s_statistics.csv", safeSubject, startPhase, endPhase)
}

// sanitizeFileName 清理檔案名中的特殊字符.
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

// ValidateStatisticsParams 驗證統計參數.
func ValidateStatisticsParams(params *StatisticsParams) error {
	if params.StartTime < 0 {
		return fmt.Errorf("開始時間不能為負數 (%.3f): %w", params.StartTime, ErrNegativeStartTime)
	}

	if params.EndTime < 0 {
		return fmt.Errorf("結束時間不能為負數 (%.3f): %w", params.EndTime, ErrNegativeEndTime)
	}

	if params.StartTime >= params.EndTime {
		return fmt.Errorf("開始時間 (%.3f) 必須小於結束時間 (%.3f): %w",
			params.StartTime, params.EndTime, ErrStartTimeNotBeforeEnd)
	}

	if params.Subject == "" {
		return ErrEmptySubject
	}

	return nil
}

// StatisticsParams 統計參數.
type StatisticsParams struct {
	Subject    string
	StartPhase string
	StartTime  float64
	EndPhase   string
	EndTime    float64
}

// FormatStatisticsReport 格式化統計報告.
func FormatStatisticsReport(stats *models.EMGStatistics) string {
	report := "EMG 統計分析報告\n"
	report += "================\n"
	report += fmt.Sprintf("主題: %s\n", stats.Subject)
	report += fmt.Sprintf("分析區間: %s (%.3fs) → %s (%.3fs)\n",
		stats.StartPhase, stats.StartTime,
		stats.EndPhase, stats.EndTime)
	report += fmt.Sprintf("持續時間: %.3f 秒\n", stats.EndTime-stats.StartTime)
	report += fmt.Sprintf("通道數量: %d\n\n", len(stats.ChannelNames))

	report += "各通道統計結果:\n"
	report += fmt.Sprintf("%-20s %15s %15s\n", "通道名稱", "平均值", "最大值")
	report += fmt.Sprintf("%s\n", strings.Repeat("-", reportSeparatorWidth))

	for _, channelName := range stats.ChannelNames {
		mean := stats.ChannelMeans[channelName]
		maxVal := stats.ChannelMaxes[channelName]
		report += fmt.Sprintf("%-20s %15.6f %15.6f\n", channelName, mean, maxVal)
	}

	return report
}
