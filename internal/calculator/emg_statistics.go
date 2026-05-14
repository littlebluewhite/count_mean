package calculator

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strings"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security/fsperm"
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
//
// fsperm.WriteFlags 含 O_NOFOLLOW (unix) 拒絕 symlink；攻擊者無法在 OutputDir
// 植入 symlink 把寫入導向 /etc/passwd 等敏感檔。
func writeCSVWithBOM(outputPath string) (*os.File, *csv.Writer, error) {
	//nolint:gosec // G304: outputPath is validated by caller
	file, err := os.OpenFile(outputPath, fsperm.WriteFlags, fsperm.FilePerm)
	if err != nil {
		return nil, nil, fmt.Errorf("無法創建輸出檔案 %s: %w", outputPath, err)
	}

	if err := csvutil.WriteBOM(file); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}

		return nil, nil, fmt.Errorf("無法寫入 BOM: %w", err)
	}

	return file, csv.NewWriter(file), nil
}

// ExportToCSV 將統計結果導出為 CSV 檔案.
// Flush 與 Close 的錯誤都會被回傳：磁碟滿等狀況下 csv.Writer 把錯誤延後到 Flush 才丟，
// 若僅在 defer 中忽略，caller 會看到 nil 但檔案內容不完整。
func (calc *EMGStatisticsCalculator) ExportToCSV(
	stats *models.EMGStatistics,
	outputPath string,
) (err error) {
	file, writer, err := writeCSVWithBOM(outputPath)
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("關閉檔案失敗: %w", closeErr)
		}
	}()

	channelCount := len(stats.ChannelNames)

	rows := []struct {
		row      []string
		errorMsg string
	}{
		{csvutil.SanitizeHeaderRow(append([]string{""}, stats.ChannelNames...)), "寫入標題行失敗"},
		{buildUniformRow("開始分期點", string(stats.StartPhase), channelCount), "寫入開始分期點失敗"},
		{buildUniformRow("開始時間", calc.formatFloat(stats.StartTime), channelCount), "寫入開始時間失敗"},
		{buildUniformRow("結束分期點", string(stats.EndPhase), channelCount), "寫入結束分期點失敗"},
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

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("CSV flush 失敗: %w", err)
	}

	return nil
}

// formatFloat 格式化浮點數.
func (calc *EMGStatisticsCalculator) formatFloat(value float64) string {
	format := fmt.Sprintf("%%.%df", calc.precision)
	return fmt.Sprintf(format, value)
}

// GenerateOutputFileName 生成輸出檔案名.
func GenerateOutputFileName(subject string, startPhase, endPhase models.PhasePoint) string {
	safeSubject := SanitizeFileName(subject)

	return fmt.Sprintf("%s_%s-%s_statistics.csv", safeSubject, startPhase, endPhase)
}

// SanitizeFileName 把可能造成路徑穿越或檔名衝突的字元替換為底線。
// 防範手段：把 /、\、:、*、?、"、<、>、|、空白都替換為 _，使 caller 可以安全地
// 將外部輸入（如 manifest.Subject 或前端傳入的字串）直接組進 filepath.Join 的尾段。
func SanitizeFileName(name string) string {
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
