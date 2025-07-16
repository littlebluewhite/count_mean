package chart

import (
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
)

// EChartsGenerator 使用 go-echarts 生成互動式圖表
type EChartsGenerator struct {
	logger *logging.Logger
}

// NewEChartsGenerator 創建新的 ECharts 圖表生成器
func NewEChartsGenerator() *EChartsGenerator {
	return &EChartsGenerator{
		logger: logging.GetLogger("echarts_generator"),
	}
}

// InteractiveChartConfig 互動式圖表配置
type InteractiveChartConfig struct {
	Title           string
	XAxisLabel      string
	YAxisLabel      string
	SelectedColumns []int    // 要顯示的欄位索引
	ColumnNames     []string // 對應的欄位名稱
	ShowAllColumns  bool     // 是否顯示所有欄位
	Width           string   // 圖表寬度
	Height          string   // 圖表高度
}

// GenerateInteractiveChart 生成互動式圖表
func (e *EChartsGenerator) GenerateInteractiveChart(dataset *models.EMGDataset, config InteractiveChartConfig, outputPath string) error {
	e.logger.Info("開始生成互動式圖表", map[string]interface{}{
		"title":            config.Title,
		"selected_columns": config.SelectedColumns,
		"output_path":      outputPath,
		"data_points":      len(dataset.Data),
	})

	// 創建新的折線圖
	line := charts.NewLine()

	// 設置全局選項
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Theme:  types.ThemeWesteros,
			Width:  config.Width,
			Height: config.Height,
		}),
		charts.WithTitleOpts(opts.Title{
			Title:    config.Title,
			Subtitle: fmt.Sprintf("數據點數: %d", len(dataset.Data)),
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:    opts.Bool(true),
			Trigger: "axis",
			AxisPointer: &opts.AxisPointer{
				Type: "cross",
				Label: &opts.Label{
					Show:            opts.Bool(true),
					BackgroundColor: "#6a7985",
				},
			},
		}),
		charts.WithLegendOpts(opts.Legend{
			Show:   opts.Bool(true),
			Left:   "center",
			Top:    "30px",
			Orient: "horizontal",
			Type:   "scroll",
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:       "inside",
			Start:      0,
			End:        100,
			XAxisIndex: []int{0},
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:       "slider",
			Start:      0,
			End:        100,
			XAxisIndex: []int{0},
		}),
		charts.WithXAxisOpts(opts.XAxis{
			Type: "value",
			Name: config.XAxisLabel,
			AxisLabel: &opts.AxisLabel{
				Formatter: "{value} s",
			},
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Type: "value",
			Name: config.YAxisLabel,
			AxisLabel: &opts.AxisLabel{
				Formatter: opts.FuncOpts("function (value) { return value.toExponential(2); }"),
			},
		}),
		charts.WithToolboxOpts(opts.Toolbox{
			Show: opts.Bool(true),
			Feature: &opts.ToolBoxFeature{
				SaveAsImage: &opts.ToolBoxFeatureSaveAsImage{
					Show:  opts.Bool(true),
					Title: "下載圖片",
					Type:  "png",
				},
				DataZoom: &opts.ToolBoxFeatureDataZoom{
					Show: opts.Bool(true),
					Title: map[string]string{
						"zoom": "區域縮放",
						"back": "縮放還原",
					},
				},
				Restore: &opts.ToolBoxFeatureRestore{
					Show:  opts.Bool(true),
					Title: "還原",
				},
				DataView: &opts.ToolBoxFeatureDataView{
					Show:  opts.Bool(true),
					Title: "數據視圖",
					Lang:  []string{"數據視圖", "關閉", "刷新"},
				},
			},
		}),
	)

	// 準備X軸數據（時間）
	xAxisData := make([]float64, len(dataset.Data))
	for i, data := range dataset.Data {
		xAxisData[i] = data.Time
	}

	// 設置X軸
	line.SetXAxis(xAxisData)

	// 決定要顯示的欄位
	columnsToShow := config.SelectedColumns
	if config.ShowAllColumns || len(columnsToShow) == 0 {
		// 顯示所有欄位（除了時間欄位）
		columnsToShow = make([]int, len(dataset.Headers)-1)
		for i := 1; i < len(dataset.Headers); i++ {
			columnsToShow[i-1] = i
		}
	}

	// 為每個選中的欄位添加數據系列
	colors := []string{
		"#FF0000", "#00FF00", "#0000FF", "#FFA500",
		"#800080", "#FFC0CB", "#A52A2A", "#808080",
		"#FFD700", "#4B0082", "#00CED1", "#FF1493",
	}

	for idx, colIndex := range columnsToShow {
		if colIndex >= len(dataset.Headers) || colIndex == 0 {
			continue
		}

		// 準備數據
		lineData := make([]opts.LineData, len(dataset.Data))
		for i, data := range dataset.Data {
			value := 0.0
			if colIndex-1 < len(data.Channels) {
				value = data.Channels[colIndex-1]
			}
			lineData[i] = opts.LineData{Value: value}
		}

		// 獲取列名
		columnName := dataset.Headers[colIndex]
		if len(config.ColumnNames) > idx && config.ColumnNames[idx] != "" {
			columnName = config.ColumnNames[idx]
		}

		// 添加數據系列
		line.AddSeries(columnName, lineData).
			SetSeriesOptions(
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:     opts.Bool(false),
					ShowSymbol: opts.Bool(false),
				}),
				charts.WithLineStyleOpts(opts.LineStyle{
					Width: 1.5,
					Color: colors[idx%len(colors)],
				}),
			)
	}

	// 添加自定義JavaScript功能
	e.addCustomJavaScript(line)

	// 確保輸出目錄存在
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("創建輸出目錄失敗: %w", err)
	}

	// 創建HTML文件
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("創建輸出文件失敗: %w", err)
	}
	defer file.Close()

	// 渲染圖表
	if err := line.Render(file); err != nil {
		return fmt.Errorf("渲染圖表失敗: %w", err)
	}

	e.logger.Info("互動式圖表生成完成", map[string]interface{}{
		"output_path": outputPath,
		"columns":     len(columnsToShow),
	})

	return nil
}

// createLineChart 創建單個折線圖
func (e *EChartsGenerator) createLineChart(dataset *models.EMGDataset, config InteractiveChartConfig) *charts.Line {
	line := charts.NewLine()

	// 設置全局選項（同上）
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Theme:  types.ThemeWesteros,
			Width:  config.Width,
			Height: config.Height,
		}),
		charts.WithTitleOpts(opts.Title{
			Title: config.Title,
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:    opts.Bool(true),
			Trigger: "axis",
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:  "inside",
			Start: 0,
			End:   100,
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:  "slider",
			Start: 0,
			End:   100,
		}),
	)

	// 準備X軸數據
	xAxisData := make([]float64, len(dataset.Data))
	for i, data := range dataset.Data {
		xAxisData[i] = data.Time
	}
	line.SetXAxis(xAxisData)

	// 添加數據系列
	columnsToShow := config.SelectedColumns
	if config.ShowAllColumns || len(columnsToShow) == 0 {
		columnsToShow = make([]int, len(dataset.Headers)-1)
		for i := 1; i < len(dataset.Headers); i++ {
			columnsToShow[i-1] = i
		}
	}

	for _, colIndex := range columnsToShow {
		if colIndex >= len(dataset.Headers) || colIndex == 0 {
			continue
		}

		lineData := make([]opts.LineData, len(dataset.Data))
		for i, data := range dataset.Data {
			value := 0.0
			if colIndex-1 < len(data.Channels) {
				value = data.Channels[colIndex-1]
			}
			lineData[i] = opts.LineData{Value: value}
		}

		columnName := dataset.Headers[colIndex]
		line.AddSeries(columnName, lineData).
			SetSeriesOptions(
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:     opts.Bool(false),
					ShowSymbol: opts.Bool(false),
				}),
			)
	}

	return line
}

// addCustomJavaScript 添加自定義JavaScript功能
func (e *EChartsGenerator) addCustomJavaScript(chart *charts.Line) {
	// 添加滾輪縮放功能
	wheelZoomJS := `
		// 滾輪縮放功能
		let myChart = %MY_ECHARTS%;
		if (myChart) {
			myChart.on('datazoom', function(params) {
				// 同步更新所有DataZoom組件
				if (params.batch) {
					params.batch.forEach(function(item) {
						if (item.dataZoomId !== params.dataZoomId) {
							myChart.dispatchAction({
								type: 'dataZoom',
								dataZoomIndex: item.dataZoomIndex,
								start: item.start,
								end: item.end
							});
						}
					});
				}
			});

			// 支持鍵盤快捷鍵
			document.addEventListener('keydown', function(e) {
				if (e.key === 'r' || e.key === 'R') {
					// R鍵重置縮放
					myChart.dispatchAction({
						type: 'dataZoom',
						start: 0,
						end: 100
					});
				}
			});

			// 自適應視窗大小
			window.addEventListener('resize', function() {
				myChart.resize();
			});
		}
	`

	chart.AddJSFuncStrs(opts.FuncOpts(wheelZoomJS))
}

// GetAvailableColumns 獲取可用的欄位列表
func (e *EChartsGenerator) GetAvailableColumns(dataset *models.EMGDataset) []ColumnInfo {
	columns := make([]ColumnInfo, 0, len(dataset.Headers)-1)

	for i := 1; i < len(dataset.Headers); i++ {
		// 計算該欄位的基本統計信息
		var min, max, sum float64
		count := 0

		for _, data := range dataset.Data {
			if i-1 < len(data.Channels) {
				value := data.Channels[i-1]
				if count == 0 {
					min = value
					max = value
				} else {
					if value < min {
						min = value
					}
					if value > max {
						max = value
					}
				}
				sum += value
				count++
			}
		}

		mean := sum / float64(count)

		columns = append(columns, ColumnInfo{
			Index:      i,
			Name:       dataset.Headers[i],
			DataPoints: count,
			Min:        min,
			Max:        max,
			Mean:       mean,
		})
	}

	return columns
}

// ColumnInfo 欄位信息
type ColumnInfo struct {
	Index      int     `json:"index"`
	Name       string  `json:"name"`
	DataPoints int     `json:"dataPoints"`
	Min        float64 `json:"min"`
	Max        float64 `json:"max"`
	Mean       float64 `json:"mean"`
}

// RenderChartToWriter 將圖表渲染到Writer
func (e *EChartsGenerator) RenderChartToWriter(dataset *models.EMGDataset, config InteractiveChartConfig, w io.Writer) error {
	line := e.createLineChart(dataset, config)
	e.addCustomJavaScript(line)
	return line.Render(w)
}

// DataPoint 數據點結構
type DataPoint struct {
	Time  float64   `json:"time"`
	Value []float64 `json:"value"`
}

// ConvertToJSON 將數據轉換為JSON格式
func (e *EChartsGenerator) ConvertToJSON(dataset *models.EMGDataset, selectedColumns []int) []DataPoint {
	points := make([]DataPoint, len(dataset.Data))

	for i, data := range dataset.Data {
		values := make([]float64, len(selectedColumns))
		for j, colIdx := range selectedColumns {
			if colIdx > 0 && colIdx-1 < len(data.Channels) {
				values[j] = data.Channels[colIdx-1]
			}
		}
		points[i] = DataPoint{
			Time:  data.Time,
			Value: values,
		}
	}

	return points
}

// CalculateOptimalSampling 計算最佳採樣率以優化性能
func (e *EChartsGenerator) CalculateOptimalSampling(totalPoints int, maxPoints int) int {
	if totalPoints <= maxPoints {
		return 1
	}
	return int(math.Ceil(float64(totalPoints) / float64(maxPoints)))
}

// SampleData 對數據進行採樣
func (e *EChartsGenerator) SampleData(dataset *models.EMGDataset, samplingRate int) *models.EMGDataset {
	if samplingRate <= 1 {
		return dataset
	}

	sampledData := &models.EMGDataset{
		Headers: dataset.Headers,
		Data:    make([]models.EMGData, 0, len(dataset.Data)/samplingRate+1),
	}

	for i := 0; i < len(dataset.Data); i += samplingRate {
		sampledData.Data = append(sampledData.Data, dataset.Data[i])
	}

	// 確保最後一個數據點被包含
	if len(dataset.Data) > 0 && len(sampledData.Data) > 0 {
		lastIdx := len(dataset.Data) - 1
		if sampledData.Data[len(sampledData.Data)-1].Time != dataset.Data[lastIdx].Time {
			sampledData.Data = append(sampledData.Data, dataset.Data[lastIdx])
		}
	}

	return sampledData
}

// ExportConfig 導出配置
type ExportConfig struct {
	Format   string // "png", "svg", "pdf"
	Width    int
	Height   int
	DPI      int
	FileName string
}

// GenerateExportScript 生成導出腳本
func (e *EChartsGenerator) GenerateExportScript(config ExportConfig) string {
	return fmt.Sprintf(`
		function exportChart() {
			const myChart = %%MY_ECHARTS%%;
			if (!myChart) return;
			
			// 獲取當前圖表配置
			const option = myChart.getOption();
			const dataURL = myChart.getDataURL({
				type: '%s',
				pixelRatio: %d,
				backgroundColor: '#fff'
			});
			
			// 創建下載鏈接
			const link = document.createElement('a');
			link.download = '%s';
			link.href = dataURL;
			link.click();
		}
		
		// 綁定導出按鈕
		document.addEventListener('DOMContentLoaded', function() {
			const exportBtn = document.getElementById('export-btn');
			if (exportBtn) {
				exportBtn.addEventListener('click', exportChart);
			}
		});
	`, config.Format, config.DPI/100, config.FileName)
}

// GenerateComparisonChart 生成比較圖表（多個數據集）
func (e *EChartsGenerator) GenerateComparisonChart(datasets []*models.EMGDataset, labels []string, config InteractiveChartConfig, outputPath string) error {
	line := charts.NewLine()

	// 設置全局選項
	line.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{
			Title:    config.Title,
			Subtitle: "數據比較圖",
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:    opts.Bool(true),
			Trigger: "axis",
		}),
		charts.WithLegendOpts(opts.Legend{
			Show: opts.Bool(true),
			Type: "scroll",
		}),
		charts.WithDataZoomOpts(opts.DataZoom{
			Type:  "inside",
			Start: 0,
			End:   100,
		}),
	)

	// 找出所有數據集中的最大時間範圍
	var maxTime float64
	for _, dataset := range datasets {
		if len(dataset.Data) > 0 {
			lastTime := dataset.Data[len(dataset.Data)-1].Time
			if lastTime > maxTime {
				maxTime = lastTime
			}
		}
	}

	// 為每個數據集添加系列
	for datasetIdx, dataset := range datasets {
		if datasetIdx >= len(labels) {
			continue
		}

		xAxisData := make([]float64, len(dataset.Data))
		for i, data := range dataset.Data {
			xAxisData[i] = data.Time
		}

		if datasetIdx == 0 {
			line.SetXAxis(xAxisData)
		}

		// 為每個選定的列添加數據
		for _, colIdx := range config.SelectedColumns {
			if colIdx >= len(dataset.Headers) || colIdx == 0 {
				continue
			}

			lineData := make([]opts.LineData, len(dataset.Data))
			for i, data := range dataset.Data {
				value := 0.0
				if colIdx-1 < len(data.Channels) {
					value = data.Channels[colIdx-1]
				}
				lineData[i] = opts.LineData{Value: value}
			}

			seriesName := fmt.Sprintf("%s - %s", labels[datasetIdx], dataset.Headers[colIdx])
			line.AddSeries(seriesName, lineData).
				SetSeriesOptions(
					charts.WithLineChartOpts(opts.LineChart{
						Smooth:     opts.Bool(false),
						ShowSymbol: opts.Bool(false),
					}),
				)
		}
	}

	// 創建輸出文件
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("創建輸出文件失敗: %w", err)
	}
	defer file.Close()

	return line.Render(file)
}

// BatchExportCharts 批量導出圖表
func (e *EChartsGenerator) BatchExportCharts(dataset *models.EMGDataset, columnGroups [][]int, baseConfig InteractiveChartConfig, outputDir string) error {
	for i, columns := range columnGroups {
		config := baseConfig
		config.SelectedColumns = columns
		config.Title = fmt.Sprintf("%s - Group %d", baseConfig.Title, i+1)

		outputPath := filepath.Join(outputDir, fmt.Sprintf("chart_group_%d.html", i+1))
		if err := e.GenerateInteractiveChart(dataset, config, outputPath); err != nil {
			e.logger.Error("批量導出失敗", err, map[string]interface{}{
				"group": i + 1,
			})
			return err
		}
	}

	return nil
}

// ValidateDataset 驗證數據集
func (e *EChartsGenerator) ValidateDataset(dataset *models.EMGDataset) error {
	if dataset == nil {
		return fmt.Errorf("數據集為空")
	}

	if len(dataset.Headers) == 0 {
		return fmt.Errorf("數據集缺少標題")
	}

	if len(dataset.Data) == 0 {
		return fmt.Errorf("數據集沒有數據")
	}

	// 檢查數據一致性
	expectedChannels := len(dataset.Headers) - 1
	for i, data := range dataset.Data {
		if len(data.Channels) != expectedChannels {
			return fmt.Errorf("第 %d 行數據通道數不匹配: 期望 %d, 實際 %d",
				i+1, expectedChannels, len(data.Channels))
		}
	}

	return nil
}

// GetChartStatistics 獲取圖表統計信息
func (e *EChartsGenerator) GetChartStatistics(dataset *models.EMGDataset, selectedColumns []int) map[string]interface{} {
	stats := make(map[string]interface{})

	stats["total_points"] = len(dataset.Data)
	stats["selected_columns"] = len(selectedColumns)

	if len(dataset.Data) > 0 {
		stats["start_time"] = dataset.Data[0].Time
		stats["end_time"] = dataset.Data[len(dataset.Data)-1].Time
		stats["duration"] = dataset.Data[len(dataset.Data)-1].Time - dataset.Data[0].Time

		// 計算採樣率
		if len(dataset.Data) > 1 {
			avgInterval := (dataset.Data[len(dataset.Data)-1].Time - dataset.Data[0].Time) / float64(len(dataset.Data)-1)
			stats["sampling_rate"] = 1.0 / avgInterval
		}
	}

	// 計算每個選定列的統計信息
	columnStats := make([]map[string]interface{}, 0)
	for _, colIdx := range selectedColumns {
		if colIdx > 0 && colIdx < len(dataset.Headers) {
			colStat := map[string]interface{}{
				"name":  dataset.Headers[colIdx],
				"index": colIdx,
			}

			var min, max, sum float64
			count := 0

			for _, data := range dataset.Data {
				if colIdx-1 < len(data.Channels) {
					value := data.Channels[colIdx-1]
					if count == 0 {
						min = value
						max = value
					} else {
						if value < min {
							min = value
						}
						if value > max {
							max = value
						}
					}
					sum += value
					count++
				}
			}

			if count > 0 {
				colStat["min"] = min
				colStat["max"] = max
				colStat["mean"] = sum / float64(count)
				colStat["range"] = max - min
			}

			columnStats = append(columnStats, colStat)
		}
	}

	stats["column_statistics"] = columnStats

	return stats
}

// OptimizeForLargeDataset 針對大數據集優化
func (e *EChartsGenerator) OptimizeForLargeDataset(dataset *models.EMGDataset, maxPoints int) *models.EMGDataset {
	if len(dataset.Data) <= maxPoints {
		return dataset
	}

	// 使用 LTTB (Largest Triangle Three Buckets) 算法進行降採樣
	return e.lttbDownsample(dataset, maxPoints)
}

// lttbDownsample LTTB 降採樣算法實現
func (e *EChartsGenerator) lttbDownsample(dataset *models.EMGDataset, threshold int) *models.EMGDataset {
	dataLength := len(dataset.Data)
	if threshold >= dataLength || threshold == 0 {
		return dataset
	}

	sampled := &models.EMGDataset{
		Headers: dataset.Headers,
		Data:    make([]models.EMGData, 0, threshold),
	}

	// 始終包含第一個和最後一個點
	sampled.Data = append(sampled.Data, dataset.Data[0])

	// 計算每個桶的大小
	every := float64(dataLength-2) / float64(threshold-2)

	for i := 0; i < threshold-2; i++ {
		// 計算當前桶的範圍
		avgRangeStart := int(math.Floor(float64(i)*every)) + 1
		avgRangeEnd := int(math.Floor(float64(i+1)*every)) + 1

		if avgRangeEnd >= dataLength {
			avgRangeEnd = dataLength
		}

		avgRangeLength := avgRangeEnd - avgRangeStart

		// 獲取下一個桶的平均點
		avgX := 0.0
		avgY := make([]float64, len(dataset.Data[0].Channels))

		for j := 0; j < avgRangeLength; j++ {
			idx := avgRangeStart + j
			avgX += dataset.Data[idx].Time
			for k := range avgY {
				if k < len(dataset.Data[idx].Channels) {
					avgY[k] += dataset.Data[idx].Channels[k]
				}
			}
		}

		avgX /= float64(avgRangeLength)
		for k := range avgY {
			avgY[k] /= float64(avgRangeLength)
		}

		// 尋找最大面積的點
		maxArea := -1.0
		maxAreaPoint := 0

		for j := 0; j < avgRangeLength; j++ {
			idx := avgRangeStart + j

			// 計算面積（使用第一個通道的值）
			area := 0.0
			if len(dataset.Data[idx].Channels) > 0 && len(sampled.Data) > 0 && len(sampled.Data[len(sampled.Data)-1].Channels) > 0 {
				area = math.Abs(
					(sampled.Data[len(sampled.Data)-1].Time-avgX)*
						(dataset.Data[idx].Channels[0]-sampled.Data[len(sampled.Data)-1].Channels[0])-
						(sampled.Data[len(sampled.Data)-1].Time-dataset.Data[idx].Time)*
							(avgY[0]-sampled.Data[len(sampled.Data)-1].Channels[0]),
				) * 0.5
			}

			if area > maxArea {
				maxArea = area
				maxAreaPoint = idx
			}
		}

		sampled.Data = append(sampled.Data, dataset.Data[maxAreaPoint])
	}

	// 添加最後一個點
	sampled.Data = append(sampled.Data, dataset.Data[dataLength-1])

	return sampled
}

// GenerateRealtimeChart 生成實時更新圖表
func (e *EChartsGenerator) GenerateRealtimeChart(config InteractiveChartConfig, outputPath string) error {
	line := charts.NewLine()

	// 設置全局選項
	line.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{
			Title: config.Title + " (實時)",
		}),
		charts.WithTooltipOpts(opts.Tooltip{
			Show:    opts.Bool(true),
			Trigger: "axis",
		}),
	)

	// 添加實時更新JavaScript
	realtimeJS := `
		let myChart = %MY_ECHARTS%;
		let data = [];
		let now = new Date();
		let value = Math.random() * 1000;

		// 初始化數據
		for (let i = 0; i < 100; i++) {
			data.push({
				name: now.toString(),
				value: [
					now.getTime(),
					Math.round(Math.random() * 1000)
				]
			});
			now = new Date(now.getTime() + 1000);
		}

		// 更新數據
		setInterval(function() {
			for (let i = 0; i < 5; i++) {
				data.shift();
				now = new Date(now.getTime() + 1000);
				data.push({
					name: now.toString(),
					value: [
						now.getTime(),
						Math.round(Math.random() * 1000)
					]
				});
			}

			myChart.setOption({
				series: [{
					data: data
				}]
			});
		}, 1000);
	`

	line.AddJSFuncStrs(opts.FuncOpts(realtimeJS))

	// 創建輸出文件
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("創建輸出文件失敗: %w", err)
	}
	defer file.Close()

	return line.Render(file)
}

// FormatValue 格式化數值顯示
func (e *EChartsGenerator) FormatValue(value float64, precision int) string {
	if math.Abs(value) < 1e-3 || math.Abs(value) > 1e6 {
		return strconv.FormatFloat(value, 'e', precision, 64)
	}
	return strconv.FormatFloat(value, 'f', precision, 64)
}

// GenerateCustomTheme 生成自定義主題
func (e *EChartsGenerator) GenerateCustomTheme() string {
	return `
		{
			"color": ["#2E86C1", "#28B463", "#F39C12", "#E74C3C", "#8E44AD", "#34495E"],
			"backgroundColor": "rgba(252, 252, 252, 1)",
			"textStyle": {},
			"title": {
				"textStyle": {
					"color": "#2C3E50"
				},
				"subtextStyle": {
					"color": "#7F8C8D"
				}
			},
			"line": {
				"itemStyle": {
					"borderWidth": 2
				},
				"lineStyle": {
					"width": 2
				},
				"symbolSize": 4,
				"symbol": "circle",
				"smooth": false
			},
			"categoryAxis": {
				"axisLine": {
					"show": true,
					"lineStyle": {
						"color": "#BDC3C7"
					}
				},
				"axisTick": {
					"show": true,
					"lineStyle": {
						"color": "#BDC3C7"
					}
				},
				"axisLabel": {
					"show": true,
					"textStyle": {
						"color": "#34495E"
					}
				},
				"splitLine": {
					"show": true,
					"lineStyle": {
						"color": ["#ECF0F1"]
					}
				},
				"splitArea": {
					"show": false,
					"areaStyle": {
						"color": ["rgba(250,250,250,0.3)", "rgba(200,200,200,0.3)"]
					}
				}
			},
			"valueAxis": {
				"axisLine": {
					"show": true,
					"lineStyle": {
						"color": "#BDC3C7"
					}
				},
				"axisTick": {
					"show": true,
					"lineStyle": {
						"color": "#BDC3C7"
					}
				},
				"axisLabel": {
					"show": true,
					"textStyle": {
						"color": "#34495E"
					}
				},
				"splitLine": {
					"show": true,
					"lineStyle": {
						"color": ["#ECF0F1"]
					}
				},
				"splitArea": {
					"show": false,
					"areaStyle": {
						"color": ["rgba(250,250,250,0.3)", "rgba(200,200,200,0.3)"]
					}
				}
			}
		}
	`
}
