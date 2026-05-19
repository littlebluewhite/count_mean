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
//
// 在 V7 PoC 之前這四個 sentinel 已宣告但無 caller 使用 — declared-but-dead。
// 修補後由 validateDataset 統一在 chart-generator 入口回傳 ErrDatasetNil /
// ErrDatasetNoHeaders / ErrDatasetNoData,上層 (gui/chart_helpers.go) 透過 fmt.Errorf
// %w 包裝後 caller 可用 errors.Is 對應。ErrDatasetChannelMismatch 保留供未來
// per-row channel 長度校驗使用 (尚未啟用)。
var (
	ErrDatasetNil             = errors.New("數據集為空")
	ErrDatasetNoHeaders       = errors.New("數據集缺少標題")
	ErrDatasetNoData          = errors.New("數據集沒有數據")
	ErrDatasetChannelMismatch = errors.New("數據通道數不匹配")
)

// validateDataset 統一檢查 dataset 是否可用於繪圖。
//
// 三種失敗情境:
//   - dataset == nil → ErrDatasetNil (nil-deref 防護)
//   - len(Headers) == 0 → ErrDatasetNoHeaders (空 headers → make cap underflow 防護)
//   - len(Data) == 0 → ErrDatasetNoData (沒有數據點可繪圖)
//
// 注意:單一時間欄 (len(Headers) == 1, 無 channel 欄) 不在這裡擋下 — caller 期待
// 圖表空白 (有 X 軸但沒線),不是 error。
func validateDataset(dataset *models.EMGDataset) error {
	if dataset == nil {
		return ErrDatasetNil
	}
	if len(dataset.Headers) == 0 {
		return ErrDatasetNoHeaders
	}
	if len(dataset.Data) == 0 {
		return ErrDatasetNoData
	}
	return nil
}

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
//
// 入口先過 validateDataset → 回傳 ErrDatasetNil / NoHeaders / NoData,
// 防止 extractTimeData / extractColumnLineData 在後續路徑 nil-deref。
func (e *EChartsGenerator) GenerateInteractiveChart(
	dataset *models.EMGDataset,
	config *InteractiveChartConfig,
	outputPath string,
) error {
	if err := validateDataset(dataset); err != nil {
		return fmt.Errorf("生成互動式圖表失敗: %w", err)
	}

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
			Title:    sanitizeChartString(config.Title),
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
			Name: sanitizeChartString(config.XAxisLabel),
			AxisLabel: &opts.AxisLabel{
				Formatter: "{value} s",
			},
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Type: "value",
			Name: sanitizeChartString(config.YAxisLabel),
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
//
// 顯式設 ConnectNulls(false) — ragged row 的 nil-Value 在 echarts 渲染
// 為斷線 gap,不被 echarts version 預設值影響。
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
					Smooth:       opts.Bool(false),
					ShowSymbol:   opts.Bool(false),
					ConnectNulls: opts.Bool(false),
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
		return sanitizeChartString(config.ColumnNames[idx])
	}

	return sanitizeChartString(dataset.Headers[colIndex])
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
//
// 原本 silently drop config.XAxisLabel / YAxisLabel — caller config 帶了
// axis label 但圖表 HTML 不會渲染。改為傳給 echarts 的 XAxis.Name / YAxis.Name,
// 與 setInteractiveChartOptions 行為對齊。axis label 走 sanitizeChartString 防 XSS。
func applySimpleChartOptions(line *charts.Line, config *InteractiveChartConfig) {
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Theme:  types.ThemeWesteros,
			Width:  config.Width,
			Height: config.Height,
		}),
		charts.WithTitleOpts(opts.Title{
			Title: sanitizeChartString(config.Title),
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
		charts.WithXAxisOpts(opts.XAxis{
			Name: sanitizeChartString(config.XAxisLabel),
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Name: sanitizeChartString(config.YAxisLabel),
		}),
	)
}

// addSimpleChartSeries 添加簡化的圖表數據系列（無顏色配置）.
//
// ConnectNulls(false) 與 addInteractiveChartSeries 對齊。
func addSimpleChartSeries(line *charts.Line, dataset *models.EMGDataset, columnsToShow []int) {
	for _, colIndex := range columnsToShow {
		// colIndex 0 表示時間欄；負值會讓 extractColumnLineData 走 Channels[colIdx-1] panic。
		if colIndex <= 0 || colIndex >= len(dataset.Headers) {
			continue
		}

		lineData := extractColumnLineData(dataset, colIndex)
		columnName := sanitizeChartString(dataset.Headers[colIndex])

		line.AddSeries(columnName, lineData).
			SetSeriesOptions(
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:       opts.Bool(false),
					ShowSymbol:   opts.Bool(false),
					ConnectNulls: opts.Bool(false),
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
//
// 入口先過 validateDataset → 回傳 ErrDatasetNil / NoHeaders / NoData。
// 由於 gui/chart_helpers.go GenerateInteractiveChart 透過 fmt.Errorf %w 包裝
// 此 error 後送回前端,caller 可用 errors.Is(err, chart.ErrDatasetNil) 等 sentinel 比對。
func (e *EChartsGenerator) RenderChartToWriter(
	dataset *models.EMGDataset,
	config InteractiveChartConfig, //nolint:gocritic // hugeParam: API compatibility
	w io.Writer,
) error {
	if err := validateDataset(dataset); err != nil {
		return fmt.Errorf("渲染圖表到Writer失敗: %w", err)
	}

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
//
// nil dataset → 回傳空 slice (避免 dereference panic)。
func ConvertToJSON(dataset *models.EMGDataset, selectedColumns []int) []DataPoint {
	if dataset == nil {
		return []DataPoint{}
	}

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

// uniformSubsampleMaxOutputPoints 是 UniformSubsample 輸出的硬上限。
//
// 防止 caller 對 100M-row 資料 (或 rate <= 1 pass-through path) 噴出
// 超大輸出耗盡記憶體;與 calculator 的 backpressure 設計對齊 — UI 渲染、JSON
// 序列化、echarts 互動效能都需要 hard cap。
//
// 200_000 點:
//   - 對人眼視覺已足夠細 (1080p 寬約 1920px,1 點 / pixel 仍綽綽有餘)
//   - 對 ECharts 互動延遲低於 100ms (典型瀏覽器)
//   - 序列化 JSON 約 5-10MB,Wails RPC / 檔案輸出仍可接受
//
// 後續若有「保留視覺峰谷」需求,改用 LTTBDownsample (internal/chart/downsampling.go)。
const uniformSubsampleMaxOutputPoints = 200_000

// UniformSubsample 以等間距步長對數據集做降採樣，保留首末資料點。
//
// 原函式名暗示「視覺最佳化降採樣」（如 LTTB / Largest-Triangle-
// Three-Buckets），但本函式僅執行 uniform stride 取樣，會丟失視覺重要的峰谷點。
// Rename 後語意明確；caller 若需保留視覺特徵應另實作 LTTB。
//
// 行為：
//   - samplingRate <= 1:走 fast-path,但 dataset 過大時觸發 cap 進入 capped path。
//   - samplingRate > 1：每 samplingRate 個點保留 1 個，最後確保末筆被保留。
//   - 輸出長度超過 uniformSubsampleMaxOutputPoints 時,再做一次 stride 壓回 cap。
//   - Headers 直接共享指標（caller 不可在背後 mutate）。
func UniformSubsample(dataset *models.EMGDataset, samplingRate int) *models.EMGDataset {
	// nil dataset → 回傳空 dataset (避免 dereference panic)。
	if dataset == nil {
		return &models.EMGDataset{}
	}

	if samplingRate <= 1 {
		// rate ≤ 1 fast-path 也要過 cap — 若 dataset 已 ≤ cap 直接 return 原 ref。
		if len(dataset.Data) <= uniformSubsampleMaxOutputPoints {
			return dataset
		}
		// 超過 cap → 走 capped path,等同 rate = ceil(n / cap)
		samplingRate = (len(dataset.Data) + uniformSubsampleMaxOutputPoints - 1) /
			uniformSubsampleMaxOutputPoints
		if samplingRate < 2 {
			samplingRate = 2
		}
	} else {
		// samplingRate >= 2 branch 過去缺 cap — caller 可能傳 rate=2 對 1M
		// 點 dataset 噴出 500K 輸出,超過 200K cap、與 rate <= 1 branch 行為不對稱
		// (rate <= 1 branch 永遠遵守 cap)。修法:估算 rate 下的輸出長度,若超出
		// cap 則放大 rate 以壓回 cap。對齊 rate <= 1 branch 的計算方式(ceil
		// division),保證所有 path 共享同一 hard cap 上限。
		//
		// 為何不直接 reject:caller (echarts_generator hot path) 對「subsample
		// 結果太大」沒有後路,reject 等於整段圖表渲染失敗。silently 放大 rate
		// 是 graceful degrade — UI 仍能渲染,只是密度降低。
		estimatedOutput := (len(dataset.Data) + samplingRate - 1) / samplingRate
		if estimatedOutput > uniformSubsampleMaxOutputPoints {
			samplingRate = (len(dataset.Data) + uniformSubsampleMaxOutputPoints - 1) /
				uniformSubsampleMaxOutputPoints
			if samplingRate < 2 {
				samplingRate = 2
			}
		}
	}

	sampledData := &models.EMGDataset{
		Headers: dataset.Headers,
		Data:    make([]models.EMGData, 0, len(dataset.Data)/samplingRate+1),
	}

	for i := 0; i < len(dataset.Data); i += samplingRate {
		sampledData.Data = append(sampledData.Data, dataset.Data[i])
	}

	// 末筆保留 — 改用 index 比較取代 Time 浮點比較。
	// 原本 `if last.Time != dataset.Data[lastIdx].Time` 在 stride 命中 lastIdx 但 Time
	// 有 ULP 飄移時會誤判「不同」而 append 末筆 → 末筆重複。改用 index 比較確定性化:
	// stride 是否命中 lastIdx 完全由 lastIdx % samplingRate 決定,跟 Time 浮點無關。
	if len(dataset.Data) > 0 {
		lastIdx := len(dataset.Data) - 1
		if lastIdx%samplingRate != 0 {
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
// 安全契約：
//   - Format 限定白名單 png/svg/pdf，未在 list 內 fallback "png"。
//   - FileName 走 sanitizeForJSString（json.Marshal + </ 轉義），確保
//     即使含 `';alert(1);//` 也無法逸出 JS 字串。caller 不應再外加引號。
//   - DPI 用整數計算，數值型本身不可注入。
func GenerateExportScript(config ExportConfig) string {
	format := normalizeExportFormat(config.Format)
	fileNameJSLiteral := sanitizeForJSString(config.FileName)
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
			link.download = %s;
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
	`, format, config.DPI/dpiDivisor, fileNameJSLiteral)
}

// normalizeExportFormat 把 ExportConfig.Format 收斂到白名單值，避免
// user-controlled 字串直接內嵌 JS。
func normalizeExportFormat(format string) string {
	switch format {
	case "png", "svg", "pdf", "jpeg":
		return format
	default:
		return "png"
	}
}

// GenerateComparisonChart 生成比較圖表（多個數據集）.
//
// 對每個 dataset 過 validateDataset — 任何一個 nil / 空 headers / 空 data
// 都會回傳對應 sentinel,index 附在 wrap error 內 caller 可定位來源。
func (*EChartsGenerator) GenerateComparisonChart(
	datasets []*models.EMGDataset,
	labels []string,
	config *InteractiveChartConfig,
	outputPath string,
) error {
	if len(datasets) == 0 {
		return fmt.Errorf("生成比較圖表失敗: %w", ErrDatasetNoData)
	}
	for i, ds := range datasets {
		if err := validateDataset(ds); err != nil {
			return fmt.Errorf("生成比較圖表失敗 (dataset[%d]): %w", i, err)
		}
	}

	line := charts.NewLine()

	// 設置全局選項
	setComparisonChartOptions(line, config)

	// 為每個數據集添加系列
	addComparisonSeries(line, datasets, labels, config)

	// 渲染並保存圖表
	return saveChartToFile(line, outputPath)
}

// setComparisonChartOptions 設置比較圖表的全局選項.
//
// 原本 silently drop config.XAxisLabel / YAxisLabel — caller config 帶了
// axis label 但圖表 HTML 不會渲染。改為傳給 echarts 的 XAxis.Name / YAxis.Name,
// 與 setInteractiveChartOptions 行為對齊。axis label 走 sanitizeChartString 防 XSS。
func setComparisonChartOptions(line *charts.Line, config *InteractiveChartConfig) {
	line.SetGlobalOptions(
		charts.WithTitleOpts(opts.Title{
			Title:    sanitizeChartString(config.Title),
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
		charts.WithXAxisOpts(opts.XAxis{
			Name: sanitizeChartString(config.XAxisLabel),
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Name: sanitizeChartString(config.YAxisLabel),
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
//
// ConnectNulls(false) 與 addInteractiveChartSeries 對齊。
func addDatasetSeries(line *charts.Line, dataset *models.EMGDataset, label string, selectedColumns []int) {
	for _, colIdx := range selectedColumns {
		// colIdx 0 表示時間欄；負值會讓 extractColumnLineData 走 Channels[colIdx-1] panic。
		if colIdx <= 0 || colIdx >= len(dataset.Headers) {
			continue
		}

		lineData := extractColumnLineData(dataset, colIdx)
		seriesName := sanitizeChartString(fmt.Sprintf("%s - %s", label, dataset.Headers[colIdx]))

		line.AddSeries(seriesName, lineData).
			SetSeriesOptions(
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:       opts.Bool(false),
					ShowSymbol:   opts.Bool(false),
					ConnectNulls: opts.Bool(false),
				}),
			)
	}
}

// extractTimeData 提取時間數據.
//
// nil dataset → 回傳空 slice。call sites 都先過 validateDataset,此處
// 為 defense-in-depth (若未來 caller 漏接會 noop 而非 panic)。
func extractTimeData(dataset *models.EMGDataset) []float64 {
	if dataset == nil {
		return []float64{}
	}

	xAxisData := make([]float64, len(dataset.Data))
	for i, data := range dataset.Data {
		xAxisData[i] = data.Time
	}

	return xAxisData
}

// extractColumnLineData 提取欄位的折線數據.
//
// 防 panic / 索引越界三道保險（P1-A13-4：caller 不慎傳入越界 colIdx 時必須 no-op
// 而非 panic / 讀錯記憶體）：
//  1. colIdx <= 0：時間欄或負數，視為缺欄並回傳全 nil-Value（caller 已過濾，
//     此處仍 defensive return）。
//  2. colIdx >= len(dataset.Headers)：colIdx 超過欄位定義範圍，直接 noop。
//  3. 每 row 內 colIdx-1 >= len(row.Channels)：ragged dataset 場景（部分 row 通道
//     不足）— 顯式 emit Value: nil,讓 echarts 走 null gap 而非依賴 zero-value
//     LineData{} 的 omitempty 行為。
//
// ragged row 從「zero-value LineData{}」改為「顯式 LineData{Value: nil}」。
// 配合 series-level ConnectNulls: false 設定 (見 addInteractiveChartSeries),確保:
//   - echarts JSON 顯式輸出 {"value":null} 而非 {} (omitempty 行為依版本)
//   - ConnectNulls: false → null gap 處線條斷開,不誤導使用者「資料連續」
//
// 同時保留 lineData 長度 == len(dataset.Data)，echarts X 軸 align 不會錯位。
func extractColumnLineData(dataset *models.EMGDataset, colIdx int) []opts.LineData {
	// nil dataset → 回傳空 slice。call sites 都先過 validateDataset,此處
	// 為 defense-in-depth (若未來 caller 漏接會 noop 而非 panic)。
	if dataset == nil {
		return []opts.LineData{}
	}

	lineData := make([]opts.LineData, len(dataset.Data))

	if colIdx <= 0 || colIdx >= len(dataset.Headers) {
		// 全 row 通道缺失:顯式 Value: nil 而非保留 zero-value
		for i := range lineData {
			lineData[i] = opts.LineData{Value: nil}
		}
		return lineData
	}

	channelIdx := colIdx - 1

	for i, data := range dataset.Data {
		// Double bounds check：colIdx 已驗證落在 Headers 範圍；此處再驗 row 自身的
		// Channels 長度。Ragged dataset (row.Channels < expected) 顯式 emit nil-Value。
		if channelIdx >= len(data.Channels) {
			lineData[i] = opts.LineData{Value: nil}
			continue
		}

		lineData[i] = opts.LineData{Value: data.Channels[channelIdx]}
	}

	return lineData
}
