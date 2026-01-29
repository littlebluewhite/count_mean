package chart

import (
	"count_mean/internal/models"
)

// ColumnInfo 欄位信息.
type ColumnInfo struct {
	Index      int     `json:"index"`
	Name       string  `json:"name"`
	DataPoints int     `json:"dataPoints"`
	Min        float64 `json:"min"`
	Max        float64 `json:"max"`
	Mean       float64 `json:"mean"`
}

// channelStatistics 通道統計信息（內部使用）.
type channelStatistics struct {
	minVal float64
	maxVal float64
	sum    float64
	count  int
}

// calculateChannelStatistics 計算指定通道的統計信息.
func calculateChannelStatistics(dataset *models.EMGDataset, channelIdx int) channelStatistics {
	stats := channelStatistics{}

	for _, data := range dataset.Data {
		if channelIdx >= len(data.Channels) {
			continue
		}

		value := data.Channels[channelIdx]
		stats.sum += value
		stats.count++

		if stats.count == 1 {
			stats.minVal = value
			stats.maxVal = value

			continue
		}

		if value < stats.minVal {
			stats.minVal = value
		}

		if value > stats.maxVal {
			stats.maxVal = value
		}
	}

	return stats
}

// GetAvailableColumns 獲取可用的欄位列表.
func GetAvailableColumns(dataset *models.EMGDataset) []ColumnInfo {
	columns := make([]ColumnInfo, 0, len(dataset.Headers)-1)

	for i := 1; i < len(dataset.Headers); i++ {
		stats := calculateChannelStatistics(dataset, i-1)
		mean := 0.0

		if stats.count > 0 {
			mean = stats.sum / float64(stats.count)
		}

		columns = append(columns, ColumnInfo{
			Index:      i,
			Name:       dataset.Headers[i],
			DataPoints: stats.count,
			Min:        stats.minVal,
			Max:        stats.maxVal,
			Mean:       mean,
		})
	}

	return columns
}

// GetChartStatistics 獲取圖表統計信息.
func GetChartStatistics(
	dataset *models.EMGDataset,
	selectedColumns []int,
) map[string]interface{} {
	stats := make(map[string]interface{})

	stats["total_points"] = len(dataset.Data)
	stats["selected_columns"] = len(selectedColumns)

	populateTimeStatistics(stats, dataset)
	populateColumnStatistics(stats, dataset, selectedColumns)

	return stats
}

// populateTimeStatistics 填充時間相關統計信息.
func populateTimeStatistics(stats map[string]interface{}, dataset *models.EMGDataset) {
	if len(dataset.Data) == 0 {
		return
	}

	stats["start_time"] = dataset.Data[0].Time
	stats["end_time"] = dataset.Data[len(dataset.Data)-1].Time
	stats["duration"] = dataset.Data[len(dataset.Data)-1].Time - dataset.Data[0].Time

	// 計算採樣率
	if len(dataset.Data) > 1 {
		avgInterval := (dataset.Data[len(dataset.Data)-1].Time - dataset.Data[0].Time) / float64(len(dataset.Data)-1)
		stats["sampling_rate"] = 1.0 / avgInterval
	}
}

// populateColumnStatistics 填充欄位統計信息.
func populateColumnStatistics(
	stats map[string]interface{},
	dataset *models.EMGDataset,
	selectedColumns []int,
) {
	columnStats := make([]map[string]interface{}, 0, len(selectedColumns))

	for _, colIdx := range selectedColumns {
		if colIdx <= 0 || colIdx >= len(dataset.Headers) {
			continue
		}

		// 使用共享的統計計算函數
		chanStats := calculateChannelStatistics(dataset, colIdx-1)

		colStat := map[string]interface{}{
			"name":  dataset.Headers[colIdx],
			"index": colIdx,
		}

		if chanStats.count > 0 {
			colStat["min"] = chanStats.minVal
			colStat["max"] = chanStats.maxVal
			colStat["mean"] = chanStats.sum / float64(chanStats.count)
			colStat["range"] = chanStats.maxVal - chanStats.minVal
		}

		columnStats = append(columnStats, colStat)
	}

	stats["column_statistics"] = columnStats
}
