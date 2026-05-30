package io

import (
	"fmt"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
)

// CSV result row count constants.
const (
	maxMeanResultRowCount = 6 // Number of rows for max mean results (header + 5 data rows)

	// phaseSyncPrecision 是 PhaseSync 統計輸出的固定小數位數。
	// EMG 統計報告的數字格式視為跨 config 的物理單位常規,不從 config.Precision 拿;
	// 與舊路徑 phase_sync 統計輸出的小數位數 (6) 對齊以保證 migrate 前後輸出等效。
	phaseSyncPrecision = 6
)

// csvConverter handles conversion of data structures to CSV format.
type csvConverter struct {
	scalingMultiplier float64
	precision         int
}

// newCSVConverter creates a new csvConverter instance.
func newCSVConverter(scalingMultiplier float64, precision int) *csvConverter {
	return &csvConverter{
		scalingMultiplier: scalingMultiplier,
		precision:         precision,
	}
}

// ConvertMaxMeanResults converts MaxMeanResult slice to CSV format.
func (c *csvConverter) ConvertMaxMeanResults(
	headers []string,
	results []models.MaxMeanResult,
	startRange, endRange float64,
) [][]string {
	data := make([][]string, 0, maxMeanResultRowCount)

	// Add headers (sanitize against CSV formula injection on write)
	data = append(data, csvutil.SanitizeHeaderRow(headers))

	// Create result rows
	startRangeTimes := c.buildRow("開始範圍秒數", len(headers))
	endRangeTimes := c.buildRow("結束範圍秒數", len(headers))
	startTimes := c.buildRow("開始計算秒數", len(headers))
	endTimes := c.buildRow("結束計算秒數", len(headers))
	maxMeans := c.buildRow("最大平均值", len(headers))

	// Fill results
	for _, result := range results {
		startRangeTimes = append(startRangeTimes, c.formatPrecision(startRange))
		endRangeTimes = append(endRangeTimes, c.formatPrecision(endRange))
		startTimes = append(startTimes, c.formatPrecision(c.scaleValue(result.StartTime)))
		endTimes = append(endTimes, c.formatPrecision(c.scaleValue(result.EndTime)))
		maxMeans = append(maxMeans, c.formatPrecision(c.scaleValue(result.MaxMean)))
	}

	data = append(data, startRangeTimes, endRangeTimes, startTimes, endTimes, maxMeans)

	return data
}

// ConvertNormalizedData converts EMGDataset to CSV format.
func (c *csvConverter) ConvertNormalizedData(dataset *models.EMGDataset) [][]string {
	data := make([][]string, 0, len(dataset.Data)+1)

	// Add headers (sanitize against CSV formula injection on write)
	data = append(data, csvutil.SanitizeHeaderRow(dataset.Headers))

	// Time column uses original precision, data columns use config precision
	timePrecision := fmt.Sprintf("%%.%df", dataset.OriginalTimePrecision)
	dataPrecision := fmt.Sprintf("%%.%df", c.precision)

	for _, emgData := range dataset.Data {
		row := make([]string, 0, len(dataset.Headers))

		// Time column - use original file time precision
		row = append(row, fmt.Sprintf(timePrecision, c.scaleValue(emgData.Time)))

		// Data columns - use configured precision
		for _, val := range emgData.Channels {
			row = append(row, fmt.Sprintf(dataPrecision, val))
		}

		data = append(data, row)
	}

	return data
}

// ConvertPhaseAnalysis converts PhaseAnalysisResult to CSV format.
func (c *csvConverter) ConvertPhaseAnalysis(
	headers []string,
	result *models.PhaseAnalysisResult,
	maxTimeIndex map[int]float64,
) [][]string {
	data := make([][]string, 0, len(result.MaxValues)+len(result.MeanValues)+2)

	// Add headers (sanitize against CSV formula injection on write)
	data = append(data, csvutil.SanitizeHeaderRow(headers))

	// Max values row
	maxRow := c.buildRow(result.PhaseName+" 最大值", len(headers))

	for j := 1; j < len(headers); j++ {
		channelIdx := j - 1
		if maxVal, exists := result.MaxValues[channelIdx]; exists {
			maxRow = append(maxRow, c.formatPrecision(c.scaleValue(maxVal)))
		} else {
			maxRow = append(maxRow, "N/A")
		}
	}

	data = append(data, maxRow)

	// Mean values row
	meanRow := c.buildRow(result.PhaseName+" 平均值", len(headers))

	for j := 1; j < len(headers); j++ {
		channelIdx := j - 1
		if meanVal, exists := result.MeanValues[channelIdx]; exists {
			meanRow = append(meanRow, c.formatPrecision(c.scaleValue(meanVal)))
		} else {
			meanRow = append(meanRow, "N/A")
		}
	}

	data = append(data, meanRow)

	// Max time index row
	if len(maxTimeIndex) > 0 {
		timeRow := c.buildRow("整個階段最大值出現在_秒", len(headers))

		for j := 1; j < len(headers); j++ {
			channelIdx := j - 1
			if timeVal, exists := maxTimeIndex[channelIdx]; exists {
				timeRow = append(timeRow, c.formatPrecision(c.scaleValue(timeVal)))
			} else {
				timeRow = append(timeRow, "N/A")
			}
		}

		data = append(data, timeRow)
	}

	return data
}

// formatPrecision formats a float value with the configured precision.
func (c *csvConverter) formatPrecision(value float64) string {
	return fmt.Sprintf(fmt.Sprintf("%%.%df", c.precision), value)
}

// scaleValue scales a value by the scaling multiplier.
func (c *csvConverter) scaleValue(value float64) float64 {
	return value / c.scalingMultiplier
}

// buildRow creates a new row with the given label and capacity.
func (*csvConverter) buildRow(label string, capacity int) []string {
	row := make([]string, 1, capacity)
	row[0] = label

	return row
}

// ConvertPhaseSyncResult converts EMGStatistics to the 8-row PhaseSync layout
// (header + 5 metadata rows + 平均值 + 最大值). Precision is fixed to
// phaseSyncPrecision; values are written as-is (no scaling).
func (*csvConverter) ConvertPhaseSyncResult(stats *models.EMGStatistics) [][]string {
	channelCount := len(stats.ChannelNames)
	headerRow := make([]string, 0, channelCount+1)
	headerRow = append(headerRow, "")
	headerRow = append(headerRow, stats.ChannelNames...)

	format := fmt.Sprintf("%%.%df", phaseSyncPrecision)

	return [][]string{
		csvutil.SanitizeHeaderRow(headerRow),
		buildUniformPhaseSyncRow("開始分期點", string(stats.StartPhase), channelCount),
		buildUniformPhaseSyncRow("開始時間", fmt.Sprintf(format, stats.StartTime), channelCount),
		buildUniformPhaseSyncRow("結束分期點", string(stats.EndPhase), channelCount),
		buildUniformPhaseSyncRow("結束時間", fmt.Sprintf(format, stats.EndTime), channelCount),
		buildUniformPhaseSyncRow("時間差值",
			fmt.Sprintf(format, stats.EndTime-stats.StartTime), channelCount),
		buildChannelPhaseSyncRow("平均值", stats.ChannelNames, stats.ChannelMeans, format),
		buildChannelPhaseSyncRow("最大值", stats.ChannelNames, stats.ChannelMaxes, format),
	}
}

// buildUniformPhaseSyncRow creates a row with the same value repeated for each channel.
func buildUniformPhaseSyncRow(label, value string, channelCount int) []string {
	row := make([]string, 0, channelCount+1)
	row = append(row, label)

	for range channelCount {
		row = append(row, value)
	}

	return row
}

// buildChannelPhaseSyncRow creates a row with per-channel values pulled from a map
// in the order given by channelNames.
func buildChannelPhaseSyncRow(
	label string, channelNames []string, values map[string]float64, format string,
) []string {
	row := make([]string, 0, len(channelNames)+1)
	row = append(row, label)

	for _, name := range channelNames {
		row = append(row, fmt.Sprintf(format, values[name]))
	}

	return row
}
