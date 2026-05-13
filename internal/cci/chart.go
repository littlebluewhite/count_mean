package cci

import (
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"

	"count_mean/internal/chart"
	"count_mean/internal/logging"
)

// cciChartDownsampleThreshold CCI 圖表降採樣目標點數。
// 30 萬點原始資料壓到 5000 點，視覺上 zoom-in 仍夠細，前端渲染壓力降一個量級。
const cciChartDownsampleThreshold = 5000

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
// LTTB 降採樣在 setCCIGlobalOptions 之後、X 軸建立之前套用：
// 12 配對共享同一組索引，TimeValues / Values / PhaseTimes 同步壓縮，
// 維持所有 series 對齊 shared category X-axis。
func buildCCILine(result *CCIAnalysisResult) *charts.Line {
	result = downsampleCCIResult(result, cciChartDownsampleThreshold)

	line := charts.NewLine()

	setCCIGlobalOptions(line, result)

	// Set primary X-axis data (actual time labels)
	timeLabels := buildTimeAxisLabels(result.TimeValues)
	line.SetXAxis(toInterfaceSlice(timeLabels))

	addCCIMeanSeries(line, result)
	addCCICustomJS(line)

	return line
}

// downsampleCCIResult 對每個 pair 獨立跑 LTTB，再 union 各組保留索引、排序後
// apply 到 TimeValues 與所有 PairResults[i].Values，保證所有曲線共享同一個 X 軸。
//
// 為什麼不用「第一個 pair 當 representative」的單組 LTTB：
//   - 若 representative 在某 bucket 平緩、其他 pair 在同 bucket 有窄峰，那個峰會被遺失。
//   - per-pair LTTB + union 保留每個 series 自身的高 variance 點，
//     代價是 union 後總點數可能稍超 threshold（受 pair 數 × threshold 上限約束，
//     實務 12 對相關曲線 union ≈ 1.2-2x threshold）。
//
// MeanCurves / PhasePercents / PhaseTimes / GaitStartTime / GaitEndTime 不動：
// 那些是 phase-domain 元資料（已 normalize 到 0-100% 或單純時間值），不是時序樣本。
func downsampleCCIResult(result *CCIAnalysisResult, threshold int) *CCIAnalysisResult {
	if result == nil || len(result.TimeValues) <= threshold || len(result.PairResults) == 0 {
		return result
	}

	indices := unionLTTBIndices(result, threshold)
	if len(indices) == 0 {
		return result
	}

	downsampledTime := make([]float64, len(indices))
	for i, idx := range indices {
		downsampledTime[i] = result.TimeValues[idx]
	}

	downsampledPairs := make([]CCIResult, len(result.PairResults))

	for i, pr := range result.PairResults {
		if len(pr.Values) != len(result.TimeValues) {
			downsampledPairs[i] = pr

			continue
		}

		newVals := make([]float64, len(indices))
		for j, idx := range indices {
			newVals[j] = pr.Values[idx]
		}

		downsampledPairs[i] = CCIResult{PairName: pr.PairName, Values: newVals}
	}

	return &CCIAnalysisResult{
		Subject:       result.Subject,
		PairResults:   downsampledPairs,
		TimeValues:    downsampledTime,
		PhasePercents: result.PhasePercents,
		PhaseTimes:    result.PhaseTimes,
		MeanCurves:    result.MeanCurves,
		GaitStartTime: result.GaitStartTime,
		GaitEndTime:   result.GaitEndTime,
	}
}

// cciChartMaxRenderPoints 是 union 後的 hard cap，避免 12 對 union 最壞情況灌出
// pairCount × threshold ≈ 60k 點壓垮 ECharts 互動效能。2× threshold 保留 LTTB
// 變異敏感點同時讓首尾 zoom-in 視覺仍夠細。
const cciChartMaxRenderPoints = cciChartDownsampleThreshold * 2

// unionLTTBIndices 對每個 PairResult 獨立跑 LTTB，回傳所有保留索引的 union（排序後）。
// 各 pair 用相同 threshold；map 去重後排序，保證輸出單調遞增。
//
// cross-compare review:
//   - codex P2 抓到「LTTB 非均勻索引在 category x-axis 上會視覺擠壓/拉伸」。category
//     軸每個點等寬，但時間真實間隔不等。
//   - Claude P2 抓到 union 可超 threshold 達 pairCount 倍，理論 worst 60k 點。
//
// 修法：union 排序後若超過 cciChartMaxRenderPoints，用 stride 平均抽樣再壓回，並保留
// 首尾索引維持 zoom 範圍正確。stride decimation 雖然無法完全消除 category 軸 jitter，
// 但限制最大點數 + 平均化間距能緩解視覺失真；徹底解需切到 value axis 的 [time, value]
// 資料形式，列入 backlog（變更面比較大、含次要 percent 軸對齊）。
func unionLTTBIndices(result *CCIAnalysisResult, threshold int) []int {
	seen := make(map[int]struct{}, threshold)

	for _, pr := range result.PairResults {
		if len(pr.Values) != len(result.TimeValues) {
			continue
		}

		idx := chart.LTTBDownsample(result.TimeValues, pr.Values, threshold)
		for _, i := range idx {
			seen[i] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	indices := make([]int, 0, len(seen))
	for i := range seen {
		indices = append(indices, i)
	}

	sort.Ints(indices)

	return capUnionIndices(indices, cciChartMaxRenderPoints)
}

// capUnionIndices 在 indices 超出 limit 時用 stride decimation 壓回 limit 上限，
// 保留首尾索引以維持 zoom 範圍。indices 必須已遞增排序。
// 參數命名為 limit 避免與內建 cap() 混淆（cross-compare review 補強）。
//
// 注意：stride 必須用 ceiling division — `len / limit` 的 floor 結果在
// `len % limit != 0` 時會給出太小的 stride。例：len=29999, limit=10000 用 floor
// 算出 stride=2，輸出 ≈ 15k 點仍超過 limit（codex re-review P2）。ceiling
// `(len + limit - 1) / limit` 保證 `len / stride <= limit`，最後 append 末筆讓
// 輸出至多 limit+1。
func capUnionIndices(indices []int, limit int) []int {
	if limit < 1 || len(indices) <= limit {
		return indices
	}

	stride := (len(indices) + limit - 1) / limit
	if stride < 2 {
		stride = 2
	}

	capped := make([]int, 0, limit+1)
	for i := 0; i < len(indices); i += stride {
		capped = append(capped, indices[i])
	}

	// 確保最後一筆保留，否則 zoom 末端會被截掉
	if last := indices[len(indices)-1]; len(capped) == 0 || capped[len(capped)-1] != last {
		capped = append(capped, last)
	}

	return capped
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
	// NaN / ±Inf → echarts null (line gap), aligned with CSV exporter's
	// NaN-row dropping. CalculateCCIRudolph returns math.NaN() for invalid
	// inputs (negative / NaN / Inf EMG samples); without this filter
	// go-echarts emits an empty option block on NaN-bearing series, producing
	// a blank chart that silently looks "successful". Codex Wave 7 finding.
	for _, pr := range result.PairResults {
		lineData := make([]opts.LineData, len(pr.Values))
		for j, v := range pr.Values {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				lineData[j] = opts.LineData{Value: nil}
			} else {
				lineData[j] = opts.LineData{Value: v}
			}
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
