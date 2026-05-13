package chart

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"

	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// Sentinel errors for dataset validation.
var (
	ErrDatasetNil             = errors.New("數據集為空")
	ErrDatasetNoHeaders       = errors.New("數據集缺少標題")
	ErrDatasetNoData          = errors.New("數據集沒有數據")
	ErrDatasetChannelMismatch = errors.New("數據通道數不匹配")
)

// Chart configuration constants.
const (
	defaultLineWidth    = 1.5  // 預設線條寬度
	scientificLowBound  = 1e-3 // 科學記號下界
	scientificHighBound = 1e6  // 科學記號上界
	dataZoomStart       = 0    // 數據縮放起始位置
	dataZoomEnd         = 100  // 數據縮放結束位置
	dpiDivisor          = 100  // DPI除數用於計算pixelRatio
)

// EChartsGenerator 使用 go-echarts 生成互動式圖表.
type EChartsGenerator struct {
	logger *logging.Logger
}

// NewEChartsGenerator 創建新的 ECharts 圖表生成器.
func NewEChartsGenerator() *EChartsGenerator {
	return &EChartsGenerator{
		logger: logging.GetLogger("echarts_generator"),
	}
}

// InteractiveChartConfig 互動式圖表配置.
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

// getDefaultChartColors 返回預設圖表顏色.
func getDefaultChartColors() []string {
	return []string{
		"#FF0000", "#00FF00", "#0000FF", "#FFA500",
		"#800080", "#FFC0CB", "#A52A2A", "#808080",
		"#FFD700", "#4B0082", "#00CED1", "#FF1493",
	}
}

// GenerateInteractiveChart 生成互動式圖表.
func (e *EChartsGenerator) GenerateInteractiveChart(
	dataset *models.EMGDataset,
	config *InteractiveChartConfig,
	outputPath string,
) error {
	e.logger.Info("開始生成互動式圖表", map[string]interface{}{
		"title":            config.Title,
		"selected_columns": config.SelectedColumns,
		"output_path":      outputPath,
		"data_points":      len(dataset.Data),
	})

	// 創建並配置圖表
	line := charts.NewLine()
	e.setInteractiveChartOptions(line, config, len(dataset.Data))

	// 設置數據
	line.SetXAxis(extractTimeData(dataset))
	columnsToShow := resolveColumnsToShow(config, dataset)
	e.addInteractiveChartSeries(line, dataset, config, columnsToShow)

	// 添加自定義JavaScript功能
	addCustomJavaScript(line)

	// 保存圖表
	if err := saveChartToFile(line, outputPath); err != nil {
		return err
	}

	e.logger.Info("互動式圖表生成完成", map[string]interface{}{
		"output_path": outputPath,
		"columns":     len(columnsToShow),
	})

	return nil
}

// setInteractiveChartOptions 設置互動式圖表選項.
func (*EChartsGenerator) setInteractiveChartOptions(
	line *charts.Line,
	config *InteractiveChartConfig,
	dataPoints int,
) {
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Theme:  types.ThemeWesteros,
			Width:  config.Width,
			Height: config.Height,
		}),
		charts.WithTitleOpts(opts.Title{
			Title:    config.Title,
			Subtitle: fmt.Sprintf("數據點數: %d", dataPoints),
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
		charts.WithToolboxOpts(createToolboxOpts()),
	)
}

// createToolboxOpts 創建工具箱選項.
func createToolboxOpts() opts.Toolbox {
	return opts.Toolbox{
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
	}
}

// resolveColumnsToShow 解析要顯示的欄位.
func resolveColumnsToShow(config *InteractiveChartConfig, dataset *models.EMGDataset) []int {
	if !config.ShowAllColumns && len(config.SelectedColumns) > 0 {
		return config.SelectedColumns
	}

	// 顯示所有欄位（除了時間欄位）
	columns := make([]int, len(dataset.Headers)-1)
	for i := 1; i < len(dataset.Headers); i++ {
		columns[i-1] = i
	}

	return columns
}

// addInteractiveChartSeries 添加互動式圖表數據系列.
func (*EChartsGenerator) addInteractiveChartSeries(
	line *charts.Line,
	dataset *models.EMGDataset,
	config *InteractiveChartConfig,
	columnsToShow []int,
) {
	colors := getDefaultChartColors()

	for idx, colIndex := range columnsToShow {
		// colIndex 0 表示時間欄；負值會讓 extractColumnLineData 走 Channels[colIdx-1] panic。
		if colIndex <= 0 || colIndex >= len(dataset.Headers) {
			continue
		}

		lineData := extractColumnLineData(dataset, colIndex)
		columnName := resolveColumnName(dataset, config, colIndex, idx)

		line.AddSeries(columnName, lineData).
			SetSeriesOptions(
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:     opts.Bool(false),
					ShowSymbol: opts.Bool(false),
				}),
				charts.WithLineStyleOpts(opts.LineStyle{
					Width: defaultLineWidth,
					Color: colors[idx%len(colors)],
				}),
			)
	}
}

// resolveColumnName 解析欄位名稱.
func resolveColumnName(
	dataset *models.EMGDataset,
	config *InteractiveChartConfig,
	colIndex, idx int,
) string {
	if len(config.ColumnNames) > idx && config.ColumnNames[idx] != "" {
		return config.ColumnNames[idx]
	}

	return dataset.Headers[colIndex]
}

// saveChartToFile 保存圖表到文件.
func saveChartToFile(line *charts.Line, outputPath string) error {
	// 確保輸出目錄存在
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, fsperm.DirPerm); err != nil {
		return fmt.Errorf("創建輸出目錄失敗: %w", err)
	}

	// 創建HTML文件
	// G304: outputPath comes from trusted application code
	// 使用 fsperm.WriteFlags 以在 Unix 上自動帶 O_NOFOLLOW，阻擋 symlink-swap TOCTOU。
	file, err := os.OpenFile(filepath.Clean(outputPath), fsperm.WriteFlags, fsperm.FilePerm)
	if err != nil {
		return fmt.Errorf("創建輸出文件失敗: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	// 渲染圖表
	if err := line.Render(file); err != nil {
		return fmt.Errorf("渲染圖表失敗: %w", err)
	}

	return nil
}

// createLineChart 創建單個折線圖（簡化版，複用現有方法）.
func (*EChartsGenerator) createLineChart(
	dataset *models.EMGDataset,
	config *InteractiveChartConfig,
) *charts.Line {
	line := charts.NewLine()

	// 使用簡化的圖表選項
	applySimpleChartOptions(line, config)

	// 設置X軸數據
	line.SetXAxis(extractTimeData(dataset))

	// 解析並添加數據系列
	columnsToShow := resolveColumnsToShow(config, dataset)
	addSimpleChartSeries(line, dataset, columnsToShow)

	return line
}

// applySimpleChartOptions 應用簡化的圖表選項（用於 RenderChartToWriter）.
func applySimpleChartOptions(line *charts.Line, config *InteractiveChartConfig) {
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
}

// addSimpleChartSeries 添加簡化的圖表數據系列（無顏色配置）.
func addSimpleChartSeries(line *charts.Line, dataset *models.EMGDataset, columnsToShow []int) {
	for _, colIndex := range columnsToShow {
		// colIndex 0 表示時間欄；負值會讓 extractColumnLineData 走 Channels[colIdx-1] panic。
		if colIndex <= 0 || colIndex >= len(dataset.Headers) {
			continue
		}

		lineData := extractColumnLineData(dataset, colIndex)
		columnName := dataset.Headers[colIndex]

		line.AddSeries(columnName, lineData).
			SetSeriesOptions(
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:     opts.Bool(false),
					ShowSymbol: opts.Bool(false),
				}),
			)
	}
}

// addCustomJavaScript 添加自定義JavaScript功能.
func addCustomJavaScript(chart *charts.Line) {
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

// RenderChartToWriter 將圖表渲染到Writer.
func (e *EChartsGenerator) RenderChartToWriter(
	dataset *models.EMGDataset,
	config InteractiveChartConfig, //nolint:gocritic // hugeParam: API compatibility
	w io.Writer,
) error {
	line := e.createLineChart(dataset, &config)
	addCustomJavaScript(line)

	if err := line.Render(w); err != nil {
		return fmt.Errorf("渲染圖表到Writer失敗: %w", err)
	}

	return nil
}

// DataPoint 數據點結構.
type DataPoint struct {
	Time  float64   `json:"time"`
	Value []float64 `json:"value"`
}

// ConvertToJSON 將數據轉換為JSON格式.
func ConvertToJSON(dataset *models.EMGDataset, selectedColumns []int) []DataPoint {
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

// CalculateOptimalSampling 計算最佳採樣率以優化性能.
func CalculateOptimalSampling(totalPoints, maxPoints int) int {
	if totalPoints <= maxPoints {
		return 1
	}

	return int(math.Ceil(float64(totalPoints) / float64(maxPoints)))
}

// SampleData 對數據進行採樣.
func SampleData(dataset *models.EMGDataset, samplingRate int) *models.EMGDataset {
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

// ExportConfig 導出配置.
type ExportConfig struct {
	Format   string // "png", "svg", "pdf"
	Width    int
	Height   int
	DPI      int
	FileName string
}

// GenerateExportScript 生成導出腳本.
func GenerateExportScript(config ExportConfig) string {
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
	`, config.Format, config.DPI/dpiDivisor, config.FileName)
}

// GenerateComparisonChart 生成比較圖表（多個數據集）.
func (*EChartsGenerator) GenerateComparisonChart(
	datasets []*models.EMGDataset,
	labels []string,
	config *InteractiveChartConfig,
	outputPath string,
) error {
	line := charts.NewLine()

	// 設置全局選項
	setComparisonChartOptions(line, config)

	// 為每個數據集添加系列
	addComparisonSeries(line, datasets, labels, config)

	// 渲染並保存圖表
	return saveChartToFile(line, outputPath)
}

// setComparisonChartOptions 設置比較圖表的全局選項.
func setComparisonChartOptions(line *charts.Line, config *InteractiveChartConfig) {
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
}

// addComparisonSeries 為比較圖表添加數據系列.
func addComparisonSeries(
	line *charts.Line,
	datasets []*models.EMGDataset,
	labels []string,
	config *InteractiveChartConfig,
) {
	for datasetIdx, dataset := range datasets {
		if datasetIdx >= len(labels) {
			continue
		}

		// 設置 X 軸（僅第一個數據集）
		if datasetIdx == 0 {
			xAxisData := extractTimeData(dataset)
			line.SetXAxis(xAxisData)
		}

		// 為每個選定的列添加數據
		addDatasetSeries(line, dataset, labels[datasetIdx], config.SelectedColumns)
	}
}

// addDatasetSeries 為單個數據集添加系列.
func addDatasetSeries(line *charts.Line, dataset *models.EMGDataset, label string, selectedColumns []int) {
	for _, colIdx := range selectedColumns {
		// colIdx 0 表示時間欄；負值會讓 extractColumnLineData 走 Channels[colIdx-1] panic。
		if colIdx <= 0 || colIdx >= len(dataset.Headers) {
			continue
		}

		lineData := extractColumnLineData(dataset, colIdx)
		seriesName := fmt.Sprintf("%s - %s", label, dataset.Headers[colIdx])

		line.AddSeries(seriesName, lineData).
			SetSeriesOptions(
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:     opts.Bool(false),
					ShowSymbol: opts.Bool(false),
				}),
			)
	}
}

// extractTimeData 提取時間數據.
func extractTimeData(dataset *models.EMGDataset) []float64 {
	xAxisData := make([]float64, len(dataset.Data))
	for i, data := range dataset.Data {
		xAxisData[i] = data.Time
	}

	return xAxisData
}

// extractColumnLineData 提取欄位的折線數據.
// colIdx <= 0 視為缺欄並填 0；caller 應在迴圈內已過濾，但 defensive check 防 panic。
func extractColumnLineData(dataset *models.EMGDataset, colIdx int) []opts.LineData {
	lineData := make([]opts.LineData, len(dataset.Data))

	if colIdx <= 0 {
		return lineData
	}

	for i, data := range dataset.Data {
		value := 0.0
		if colIdx-1 < len(data.Channels) {
			value = data.Channels[colIdx-1]
		}

		lineData[i] = opts.LineData{Value: value}
	}

	return lineData
}
