package chart

import (
	"math"

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
//
// guard:
//   - dataset == nil → 回傳空 slice (avoid nil-deref panic)
//   - len(Headers) == 0 → 回傳空 slice (avoid `make(.., 0, -1)` panic: cap out of range)
//   - len(Headers) == 1 → 只有時間欄,沒可顯示欄位 → 回傳空 slice
//
// 公開 API 保持 no-error 簽章 (caller 無 production 使用者,僅 test); 上游真正需要
// sentinel 的 path (RenderChartToWriter / GenerateInteractiveChart / GenerateComparisonChart)
// 透過 validateDataset 入口回傳 ErrDatasetNil / ErrDatasetNoHeaders。
func GetAvailableColumns(dataset *models.EMGDataset) []ColumnInfo {
	if dataset == nil || len(dataset.Headers) == 0 {
		return []ColumnInfo{}
	}

	// Headers 至少 1 個 → cap 至少 0,不會 underflow 為 -1。
	columns := make([]ColumnInfo, 0, len(dataset.Headers)-1)

	for i := 1; i < len(dataset.Headers); i++ {
		stats := calculateChannelStatistics(dataset, i-1)
		mean := 0.0

		if stats.count > 0 {
			mean = stats.sum / float64(stats.count)
		}

		// Headers 來自 caller-controlled CSV 第一行,可能含 `</script>` /
		// `<!--` 等 XSS payload。即使目前 ColumnInfo 走 JSON 序列化(html/template
		// 在 JS context 會自動 escape),defense-in-depth 仍應在資料離開 chart
		// package 前過一次 sanitizeChartString — 與 chart series name / title /
		// label 流程對稱,避免未來 caller 改用 raw HTML 模板時留下注入破口。
		columns = append(columns, ColumnInfo{
			Index:      i,
			Name:       sanitizeChartString(dataset.Headers[i]),
			DataPoints: stats.count,
			Min:        stats.minVal,
			Max:        stats.maxVal,
			Mean:       mean,
		})
	}

	return columns
}

// GetChartStatistics 獲取圖表統計信息.
//
// 本函式總是從完整 dataset 計算統計（min/max/mean/range/sampling_rate），
// 即使 caller 上層使用 UniformSubsample 後的 downsampled 資料來繪圖。為避免 UI
// 顯示「stats 看起來不像目前圖表上的資料」的誤導，回傳 map 帶入兩個 metadata key：
//   - statistics_source: "full_dataset"，明示統計來源
//   - statistics_points: 完整資料點數（== total_points）；上層可比對目前 chart
//     渲染的點數，組合出「Statistics from full data, chart shows N/M points」
//     之類的 disclaimer。
//
// 若未來改為從降採樣後資料計算統計，請同步把 statistics_source 改為 "subsampled"
// 並保留 statistics_points 的語意（== 實際參與統計的點數）。
func GetChartStatistics(
	dataset *models.EMGDataset,
	selectedColumns []int,
) map[string]any {
	// guard: nil dataset → 回傳 canonical zero map (避免 dereference panic)。
	// 公開 API 保持 no-error 簽章; production caller path 走 chart-generator method
	// 並由 validateDataset 回傳 ErrDatasetNil。
	if dataset == nil {
		return map[string]any{
			"total_points":      0,
			"selected_columns":  len(selectedColumns),
			"statistics_source": "full_dataset",
			"statistics_points": 0,
		}
	}

	stats := make(map[string]any)

	stats["total_points"] = len(dataset.Data)
	stats["selected_columns"] = len(selectedColumns)
	stats["statistics_source"] = "full_dataset"
	stats["statistics_points"] = len(dataset.Data)

	populateTimeStatistics(stats, dataset)
	populateColumnStatistics(stats, dataset, selectedColumns)

	return stats
}

// populateTimeStatistics 填充時間相關統計信息.
func populateTimeStatistics(stats map[string]any, dataset *models.EMGDataset) {
	if len(dataset.Data) == 0 {
		return
	}

	stats["start_time"] = dataset.Data[0].Time
	stats["end_time"] = dataset.Data[len(dataset.Data)-1].Time
	stats["duration"] = dataset.Data[len(dataset.Data)-1].Time - dataset.Data[0].Time

	// 計算採樣率。
	//
	// guard:
	//   - avgInterval > 0:防止全相同時間戳 (avgInterval == 0) 或倒退時間
	//     (avgInterval < 0) 導致 sampling_rate = ±Inf
	//   - !math.IsInf:防止 1/極小正值 仍 overflow 至 +Inf
	//   - !math.IsNaN:防止 0/0 退化
	//
	// 任一條件未滿足都不寫入 sampling_rate key — JSON marshal 的 +Inf / NaN 會
	// 讓整段 stats RPC fail (encoding/json: unsupported value)。
	if len(dataset.Data) > 1 {
		avgInterval := (dataset.Data[len(dataset.Data)-1].Time - dataset.Data[0].Time) / float64(len(dataset.Data)-1)
		if avgInterval > 0 {
			rate := 1.0 / avgInterval
			if !math.IsInf(rate, 0) && !math.IsNaN(rate) {
				stats["sampling_rate"] = rate
			}
		}
	}
}

// populateColumnStatistics 填充欄位統計信息.
func populateColumnStatistics(
	stats map[string]any,
	dataset *models.EMGDataset,
	selectedColumns []int,
) {
	columnStats := make([]map[string]any, 0, len(selectedColumns))

	for _, colIdx := range selectedColumns {
		if colIdx <= 0 || colIdx >= len(dataset.Headers) {
			continue
		}

		// 使用共享的統計計算函數
		chanStats := calculateChannelStatistics(dataset, colIdx-1)

		// 同 ColumnInfo 流程 — Headers 來自 caller-controlled CSV,
		// XSS payload 必須在離開 chart package 前過 sanitizeChartString。
		colStat := map[string]any{
			"name":  sanitizeChartString(dataset.Headers[colIdx]),
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
