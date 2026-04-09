package cci

import (
	"fmt"
	"io"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"

	"count_mean/internal/logging"
)

// GenerateCCIInteractiveChart renders an interactive CCI chart as HTML.
func GenerateCCIInteractiveChart(result *CCIAnalysisResult, w io.Writer) error {
	logger := logging.GetLogger("cci_chart")
	logger.Info("開始生成 CCI 互動式圖表", nil)

	line := buildCCILine(result)

	if err := line.Render(w); err != nil {
		return fmt.Errorf("渲染 CCI 圖表失敗: %w", err)
	}

	logger.Info("CCI 互動式圖表生成完成", nil)

	return nil
}

// buildCCILine creates and fully configures the go-echarts Line chart.
func buildCCILine(result *CCIAnalysisResult) *charts.Line {
	line := charts.NewLine()

	setCCIGlobalOptions(line, result)

	// Set primary X-axis data (actual time labels)
	timeLabels := buildTimeAxisLabels(result.TimeValues)
	line.SetXAxis(toInterfaceSlice(timeLabels))

	addCCIMeanSeries(line, result)
	addCCISDBandSeries(line, result)
	addCCICustomJS(line)

	return line
}

// setCCIGlobalOptions configures all global chart options.
func setCCIGlobalOptions(line *charts.Line, result *CCIAnalysisResult) {
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Width:  "100%",
			Height: "550px",
		}),
		charts.WithTitleOpts(opts.Title{
			Title:    "CCI: Rudolph",
			Subtitle: fmt.Sprintf("Subject: %s", result.Subject),
		}),
		createCCITooltipOpt(),
		createCCILegendOpt(),
		createCCIDataZoomOpts(),
		charts.WithXAxisOpts(opts.XAxis{
			Type:        "category",
			Name:        "Time (s)",
			BoundaryGap: opts.Bool(false),
		}),
		charts.WithYAxisOpts(opts.YAxis{
			Type:    "value",
			Name:    "CCI",
			NameGap: 30,
		}),
		charts.WithToolboxOpts(createCCIToolboxOpts()),
		charts.WithGridOpts(opts.Grid{
			Top:    "120px",
			Left:   "60px",
			Right:  "60px",
			Bottom: "80px",
		}),
	)

	// Add secondary X-axis (percentage) at top
	pctLabels := buildPercentAxisLabels(result)
	line.ExtendXAxis(opts.XAxis{
		Position:    "top",
		Type:        "category",
		Data:        toInterfaceSlice(pctLabels),
		Name:        "Time (%)",
		BoundaryGap: opts.Bool(false),
		AxisLabel: &opts.AxisLabel{
			Show:     opts.Bool(true),
			Interval: "20",
		},
		AxisTick:  &opts.AxisTick{Show: opts.Bool(false)},
		SplitLine: &opts.SplitLine{Show: opts.Bool(false)},
	})
}

func createCCITooltipOpt() charts.GlobalOpts {
	return charts.WithTooltipOpts(opts.Tooltip{
		Show:    opts.Bool(true),
		Trigger: "axis",
		AxisPointer: &opts.AxisPointer{
			Type: "cross",
			Label: &opts.Label{
				Show:            opts.Bool(true),
				BackgroundColor: "#6a7985",
			},
		},
	})
}

func createCCILegendOpt() charts.GlobalOpts {
	return charts.WithLegendOpts(opts.Legend{
		Show:   opts.Bool(true),
		Left:   "center",
		Top:    "50px",
		Orient: "horizontal",
		Type:   "scroll",
	})
}

func createCCIDataZoomOpts() charts.GlobalOpts {
	return charts.WithDataZoomOpts(
		opts.DataZoom{
			Type:       "inside",
			Start:      0,
			End:        100,
			XAxisIndex: []int{0, 1},
		},
		opts.DataZoom{
			Type:       "slider",
			Start:      0,
			End:        100,
			XAxisIndex: []int{0, 1},
		},
	)
}

// buildTimeAxisLabels creates time labels from actual data time values.
func buildTimeAxisLabels(timeValues []float64) []string {
	labels := make([]string, len(timeValues))
	for i, t := range timeValues {
		labels[i] = fmt.Sprintf("%.3f", t)
	}

	return labels
}

// buildPercentAxisLabels creates percentage labels from actual time values.
func buildPercentAxisLabels(result *CCIAnalysisResult) []string {
	duration := result.GaitEndTime - result.GaitStartTime
	labels := make([]string, len(result.TimeValues))

	for i, t := range result.TimeValues {
		pct := (t - result.GaitStartTime) / duration * 100
		labels[i] = fmt.Sprintf("%.1f%%", pct)
	}

	return labels
}

func createCCIToolboxOpts() opts.Toolbox {
	return opts.Toolbox{
		Show: opts.Bool(true),
		Feature: &opts.ToolBoxFeature{
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

// addCCIMeanSeries adds the 12 CCI mean curve series.
// Colors are managed by ECharts default palette to match legend/tooltip dots.
func addCCIMeanSeries(
	line *charts.Line, result *CCIAnalysisResult,
) {
	for _, pr := range result.PairResults {
		lineData := make([]opts.LineData, len(pr.Values))
		for j, v := range pr.Values {
			lineData[j] = opts.LineData{Value: v}
		}

		line.AddSeries(pr.PairName, lineData).
			SetSeriesOptions(
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:     opts.Bool(false),
					ShowSymbol: opts.Bool(false),
					XAxisIndex: 0,
				}),
				charts.WithLineStyleOpts(opts.LineStyle{
					Width: 2,
				}),
			)
	}
}

// addCCISDBandSeries adds SD band series for pairs with non-zero SD.
func addCCISDBandSeries(
	line *charts.Line, result *CCIAnalysisResult,
) {
	for _, pr := range result.PairResults {
		sdData, ok := result.SDCurves[pr.PairName]
		if !ok || !hasNonZeroSD(sdData) {
			continue
		}

		meanData := result.MeanCurves[pr.PairName]
		stackName := pr.PairName + "_sd"

		addSDBandPair(line, pr.PairName, meanData, sdData, "#aaa", stackName)
	}
}

// hasNonZeroSD checks if any SD value is non-zero.
func hasNonZeroSD(sd []float64) bool {
	for _, v := range sd {
		if v > 0 {
			return true
		}
	}

	return false
}

// addSDBandPair adds lower boundary + band width series for one pair.
func addSDBandPair(
	line *charts.Line,
	pairName string,
	mean, sd []float64,
	color, stackName string,
) {
	// Lower boundary (mean - SD): invisible base line
	lowerData := make([]opts.LineData, len(mean))
	for i := range mean {
		lowerData[i] = opts.LineData{Value: mean[i] - sd[i]}
	}

	line.AddSeries(pairName+" -SD", lowerData).
		SetSeriesOptions(
			charts.WithLineChartOpts(opts.LineChart{
				Smooth: opts.Bool(false), ShowSymbol: opts.Bool(false),
				Stack: stackName, XAxisIndex: 0,
			}),
			charts.WithLineStyleOpts(opts.LineStyle{
				Opacity: opts.Float(0), Width: 0,
			}),
			charts.WithItemStyleOpts(opts.ItemStyle{
				Opacity: opts.Float(0),
			}),
		)

	// Band width (2*SD): stacked on lower, with area fill
	bandData := make([]opts.LineData, len(mean))
	for i := range mean {
		bandData[i] = opts.LineData{Value: 2 * sd[i]}
	}

	line.AddSeries(pairName+" +SD", bandData).
		SetSeriesOptions(
			charts.WithLineChartOpts(opts.LineChart{
				Smooth: opts.Bool(false), ShowSymbol: opts.Bool(false),
				Stack: stackName, XAxisIndex: 0,
			}),
			charts.WithLineStyleOpts(opts.LineStyle{
				Opacity: opts.Float(0), Width: 0,
			}),
			charts.WithAreaStyleOpts(opts.AreaStyle{
				Opacity: opts.Float(0.15),
				Color:   color,
			}),
			charts.WithItemStyleOpts(opts.ItemStyle{
				Opacity: opts.Float(0),
			}),
		)
}

// addCCICustomJS adds resize handler, keyboard shortcuts, and restore listener.
func addCCICustomJS(line *charts.Line) {
	customJS := `
		let myChart = %MY_ECHARTS%;
		if (myChart) {
			document.addEventListener('keydown', function(e) {
				if (e.key === 'r' || e.key === 'R') {
					myChart.dispatchAction({type: 'dataZoom', start: 0, end: 100});
				}
			});
			window.addEventListener('resize', function() {
				myChart.resize();
			});
			myChart.on('restore', function() {
				window.parent.postMessage('cci-chart-restored', '*');
			});
			myChart.on('legendselectchanged', function() {
				window.parent.postMessage('cci-chart-legend-changed', '*');
			});
		}
	`

	line.AddJSFuncStrs(opts.FuncOpts(customJS))
}

// toInterfaceSlice converts []string to []interface{} for go-echarts.
func toInterfaceSlice(ss []string) []interface{} {
	result := make([]interface{}, len(ss))
	for i, s := range ss {
		result[i] = s
	}

	return result
}
