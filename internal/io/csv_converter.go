package io

import (
	"fmt"

	"count_mean/internal/models"
	"count_mean/internal/csvutil"
)

// CSV result row count constants.
const (
	maxMeanResultRowCount = 6 // Number of rows for max mean results (header + 5 data rows)
)

// CSVConverter handles conversion of data structures to CSV format.
type CSVConverter struct {
	scalingMultiplier float64
	precision         int
}

// NewCSVConverter creates a new CSVConverter instance.
func NewCSVConverter(scalingMultiplier float64, precision int) *CSVConverter {
	return &CSVConverter{
		scalingMultiplier: scalingMultiplier,
		precision:         precision,
	}
}

// ConvertMaxMeanResults converts MaxMeanResult slice to CSV format.
func (c *CSVConverter) ConvertMaxMeanResults(
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
func (c *CSVConverter) ConvertNormalizedData(dataset *models.EMGDataset) [][]string {
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
func (c *CSVConverter) ConvertPhaseAnalysis(
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
func (c *CSVConverter) formatPrecision(value float64) string {
	return fmt.Sprintf(fmt.Sprintf("%%.%df", c.precision), value)
}

// scaleValue scales a value by the scaling multiplier.
func (c *CSVConverter) scaleValue(value float64) float64 {
	return value / c.scalingMultiplier
}

// buildRow creates a new row with the given label and capacity.
func (*CSVConverter) buildRow(label string, capacity int) []string {
	row := make([]string, 1, capacity)
	row[0] = label

	return row
}
