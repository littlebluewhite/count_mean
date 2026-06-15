package cci

import (
	"context"
	"fmt"
	"io"
	"math"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"

	"count_mean/internal/chart"
	"count_mean/internal/chart/assets"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
)

// cciChartDownsampleThreshold CCI 圖表降採樣目標點數。
// 30 萬點原始資料壓到 5000 點，視覺上 zoom-in 仍夠細，前端渲染壓力降一個量級。
const cciChartDownsampleThreshold = 5000

// cciChartCtxCheckInterval 控制 chart pipeline 在 hot loop 每幾筆檢查一次 ctx.Done()。
// 1024 對齊 calculator.sliding_window 的 4096 cadence (chart 點數規模小,更密的 cadence
// 提高 cancel-latency 敏感度);hot-loop overhead 約 0.1% (千次 +1 select)。
const cciChartCtxCheckInterval = 1024

// GenerateCCIInteractiveChart renders an interactive CCI chart as HTML.
//
// 入口先 fail-fast 校驗：
//   - duration > 0 (對齊 analyzer.go:464-468 CSV 出口；違反 → ErrInvalidGaitCycle)
//   - 每個 pair.Values 長度 == TimeValues 長度（CCI invariant；違反 →
//     ErrPairLengthMismatch）
//
// 兩個 invariant 都是 caller 應該保證的；違反時不寫出半成品 HTML，避免下游
// 覆蓋既有正常檔。
//
// ctx 為第一個參數,讓 caller (例 GUI Wails lifecycle ctx) 可在大 dataset
// render 中斷流程。hot loop (downsample / label 建構) 每 cciChartCtxCheckInterval
// 點檢查一次 ctx.Done(),pre-cancel 直接 return ctx.Err。
func GenerateCCIInteractiveChart(ctx context.Context, result *CCIAnalysisResult, w io.Writer) error {
	logger := logging.GetLogger("cci_chart")
	logger.Info("開始生成 CCI 互動式圖表", nil)

	// Fast pre-cancel:已 cancel 的 ctx 直接拒絕 render,避免動到 w 寫入半成品 HTML。
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := validateCCIResultForChart(result); err != nil {
		return err
	}

	line, err := buildCCILine(ctx, result)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := line.Render(w); err != nil {
		return fmt.Errorf("%s: %w", i18n.T(i18n.KeyErrorCCIRenderChartFailed), err)
	}

	logger.Info("CCI 互動式圖表生成完成", nil)

	return nil
}

// validateCCIResultForChart 校驗 chart pipeline 共用的兩個 invariant：
// duration > 0、所有 pair.Values 長度與 TimeValues 對齊。
//
// 對齊 analyzer.go writeCSVFile 的 duration guard，使用同一個 ErrInvalidGaitCycle
// sentinel；pair 長度不符回傳 ErrPairLengthMismatch（新 sentinel，detail
// 在 analyzer.go doc）。
func validateCCIResultForChart(result *CCIAnalysisResult) error {
	if result == nil {
		return ErrNilResult
	}

	// GaitStartTime/GaitEndTime 為 NaN/±Inf 時,下面的 `d <= 0` 對 NaN 與 +Inf 恆為
	// false 而 silently 通過,產出含 "NaN%"/"+Inf%" 軸標的圖。先擋非有限值,共用
	// ErrInvalidGaitCycle sentinel(與 zero/negative duration 同語意:gait 區間不合法)。
	if math.IsNaN(result.GaitStartTime) || math.IsInf(result.GaitStartTime, 0) ||
		math.IsNaN(result.GaitEndTime) || math.IsInf(result.GaitEndTime, 0) {
		return fmt.Errorf("%w: start=%v end=%v(含非有限值)",
			ErrInvalidGaitCycle, result.GaitStartTime, result.GaitEndTime)
	}

	if d := result.GaitEndTime - result.GaitStartTime; d <= 0 {
		return fmt.Errorf("%w: start=%v end=%v",
			ErrInvalidGaitCycle, result.GaitStartTime, result.GaitEndTime)
	}

	timeLen := len(result.TimeValues)
	for i, pr := range result.PairResults {
		if len(pr.Values) != timeLen {
			return fmt.Errorf("%w: pair[%d] %q values=%d, time=%d",
				ErrPairLengthMismatch, i, pr.PairName, len(pr.Values), timeLen)
		}
	}

	return nil
}

// buildCCILine creates and fully configures the go-echarts Line chart.
// LTTB 降採樣在 setCCIGlobalOptions 之後、X 軸建立之前套用：
// 12 配對共享同一組索引，TimeValues / Values / PhaseTimes 同步壓縮，
// 維持所有 series 對齊 shared category X-axis。
//
// 回傳 error 用於 propagate downsampleCCIResult 的對齊校驗失敗。
// GenerateCCIInteractiveChart 入口已先做一次同樣校驗，正常路徑下
// downsample 不會再 reject — 這層 error 是 defense-in-depth，避免未來
// 有其他 caller 直接 build line 時繞過 entry guard。
func buildCCILine(ctx context.Context, result *CCIAnalysisResult) (*charts.Line, error) {
	downsampled, err := downsampleCCIResult(result, cciChartDownsampleThreshold)
	if err != nil {
		return nil, err
	}

	// downsample 後檢查 ctx,避免後續 series 建構在 cancel 後仍跑完。
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	line := charts.NewLine()

	setCCIGlobalOptions(line, downsampled)

	// Set primary X-axis data (actual time labels)
	timeLabels, err := buildTimeAxisLabels(ctx, downsampled.TimeValues)
	if err != nil {
		return nil, err
	}
	line.SetXAxis(toInterfaceSlice(timeLabels))

	if err := addCCIMeanSeries(ctx, line, downsampled); err != nil {
		return nil, err
	}
	addCCICustomJS(line)

	return line, nil
}

// downsampleCCIResult 對每個 pair 獨立跑 LTTB，再 union 各組保留索引、排序後
// apply 到 TimeValues 與所有 PairResults[i].Values，保證所有曲線共享同一個 X 軸。
// union index 運算（per-pair LTTB + union + threshold*2 cap）委由 chart.UnionLTTBIndices 實施。
//
// 為什麼 CCI 在這裡 fail-fast 校驗長度（而非靠 kernel）：
//   - CCI 的 PairResults[i].Values 與 TimeValues 必須 1:1 對齊，違反是 caller bug。
//   - chart.UnionLTTBIndices 的 caller invariant 是「長度一致由 adapter 保證」，
//     mismatch 會在 LTTBDownsample 端 panic。CCI adapter 選擇在 panic 發生前就
//     fail-fast 回 ErrPairLengthMismatch，避免 half-state HTML 寫出。
//   - silent passthrough 會讓部分 pair 不被壓縮、X 軸 drift；fail-fast 是 spec 的一部分。
//
// MeanCurves / PhasePercents / PhaseTimes / GaitStartTime / GaitEndTime 不動：
// 那些是 phase-domain 元資料（已 normalize 到 0-100% 或單純時間值），不是時序樣本。
func downsampleCCIResult(result *CCIAnalysisResult, threshold int) (*CCIAnalysisResult, error) {
	if result == nil {
		return result, nil
	}

	timeLen := len(result.TimeValues)
	for i, pr := range result.PairResults {
		if len(pr.Values) != timeLen {
			return nil, fmt.Errorf("%w: pair[%d] %q values=%d, time=%d",
				ErrPairLengthMismatch, i, pr.PairName, len(pr.Values), timeLen)
		}
	}

	matrix := make([][]float64, len(result.PairResults))
	for i, pr := range result.PairResults {
		matrix[i] = pr.Values
	}

	indices := chart.UnionLTTBIndices(result.TimeValues, matrix, threshold)
	if len(indices) == 0 {
		return result, nil
	}

	downsampledTime := make([]float64, len(indices))
	for i, idx := range indices {
		downsampledTime[i] = result.TimeValues[idx]
	}

	downsampledPairs := make([]CCIResult, len(result.PairResults))

	for i, pr := range result.PairResults {
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
	}, nil
}

// setCCIGlobalOptions configures all global chart options.
//
// 安全契約（H3）：subtitle 內嵌 user-controlled `result.Subject`，
// go-echarts 以 SetEscapeHTML(false) 序列化 JSON 注入 <script> 區塊；若 Subject
// 含 `</script><script>...</script>` 序列會逸出 script context，在 Wails
// WebView 等同 RPC 任意執行。所有來源於 manifest CSV 的 user-controlled 字串
// （Subject、未來可能新增的 channel / label）都必須過 chart.SanitizeChartString，
// 把 `</` rewrite 為 `<\/`、剝除 control char、cap 長度 1024。
// Title 本身是常數但仍走 sanitize 以保持單一策略。
func setCCIGlobalOptions(line *charts.Line, result *CCIAnalysisResult) {
	// 先 sanitize user-controlled Subject 個別欄位，再用安全的格式拼進
	// subtitle。避免「先 format 後 sanitize」對固定前綴 "Subject: " 再做一次
	// `</` rewrite — 雖然冪等但語意混淆。Title 是 hard-coded 常數，也走
	// sanitize 維持單一策略（成本可忽略）。
	safeSubject := chart.SanitizeChartString(result.Subject)
	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Width:  "100%",
			Height: "550px",
		}),
		charts.WithTitleOpts(opts.Title{
			Title:    chart.SanitizeChartString("CCI: Rudolph"),
			Subtitle: fmt.Sprintf("Subject: %s", safeSubject),
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
//
// 每 cciChartCtxCheckInterval 點檢查一次 ctx.Done(),
// 避免大 timeValues 在 cancel 後仍跑完整個格式化迴圈。
func buildTimeAxisLabels(ctx context.Context, timeValues []float64) ([]string, error) {
	labels := make([]string, len(timeValues))
	for i, t := range timeValues {
		if i > 0 && i%cciChartCtxCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		labels[i] = fmt.Sprintf("%.3f", t)
	}

	return labels, nil
}

// buildPercentAxisLabels creates percentage labels from actual time values.
//
// Caller invariant: GenerateCCIInteractiveChart 在 entry 已校驗 duration > 0
// (fail-fast, 對齊 analyzer.go:464-468)，因此這裡可以放心除以 duration。
// 違反契約會在 caller 端就 ErrInvalidGaitCycle，不會走到這個 label builder。
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
//
// 每 pair 內每 cciChartCtxCheckInterval 點檢查一次 ctx.Done(),caller
// cancel 後立即回 ctx.Err。pair 之間也檢查一次 (pair 數通常 12,額外 overhead 可忽略)。
func addCCIMeanSeries(
	ctx context.Context, line *charts.Line, result *CCIAnalysisResult,
) error {
	// NaN / ±Inf → echarts null (line gap), aligned with CSV exporter's
	// NaN-row dropping. CalculateCCIRudolph returns math.NaN() for invalid
	// inputs (negative / NaN / Inf EMG samples); without this filter
	// go-echarts emits an empty option block on NaN-bearing series, producing
	// a blank chart that silently looks "successful". Codex Wave 7 finding.
	for _, pr := range result.PairResults {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lineData := make([]opts.LineData, len(pr.Values))
		for j, v := range pr.Values {
			if j > 0 && j%cciChartCtxCheckInterval == 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
			}
			if math.IsNaN(v) || math.IsInf(v, 0) {
				lineData[j] = opts.LineData{Value: nil}
			} else {
				lineData[j] = opts.LineData{Value: v}
			}
		}

		// PairName 為 user-controlled(來自 manifest CSV)— 嵌進 echarts series 名前
		// 必須過 SanitizeChartString,否則 `</script>` 序列逸出 <script> context(H3 契約)。
		line.AddSeries(chart.SanitizeChartString(pr.PairName), lineData).
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

	return nil
}

// addCCICustomJS adds resize handler, keyboard shortcuts, restore/legend
// notifications, and ADR-0003 inbound listeners (phase-markers update + PNG
// request) to the CCI chart iframe customJS.
//
// Origin validation and postToParent/isFromParent/handlePngRequest helpers now
// live in assets.IframeCommsJS (window.__chartComms.*); the per-platform Wails
// origin allowlist is defined there. Inbound listener uses
// window.__chartComms.isFromParent as the first guard.
//
// IMPORTANT: customJS body 只能用 /* ... */ block comments — Composer 那側
// 已踩過 go-echarts newlineTabPat 把 // 後內容吃光的雷(見 composer.go doc)。
func addCCICustomJS(line *charts.Line) {
	customJS := assets.PhaseMarkersJS + assets.IframeCommsJS + `
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
				window.__chartComms.postToParent({type: 'cci-chart-restored'});
			});
			myChart.on('legendselectchanged', function() {
				window.__chartComms.postToParent({type: 'cci-chart-legend-changed'});
			});
			/* ADR-0003 inbound hub:
			   - 'cci-update-phase-markers' → 解 payload.checkedPhases →
			     findNearestLabel against xAxis[0].data → 組 markData →
			     setOption({series, xAxis[1].data: newPctLabels})
			   - 'cci-request-png' → myChart.getDataURL → post 'cci-png-result'
			     reply envelope {requestId, payload?, error?} */
			window.addEventListener('message', function(e) {
				if (!window.__chartComms.isFromParent(e)) {
					return;
				}
				if (!e.data || typeof e.data !== 'object') {
					return;
				}
				if (e.data.type === 'cci-request-png') {
					window.__chartComms.handlePngRequest(myChart, e, 'cci-png-result');
					return;
				}
				if (e.data.type === 'cci-update-phase-markers') {
					try {
						const payload = e.data.payload || {};
						const checkedPhases = Array.isArray(payload.checkedPhases) ? payload.checkedPhases : [];
						const allChecked = payload.allChecked === true;
						const option = myChart.getOption();
						if (!option || !option.xAxis) return;
						const xLabels = (option.xAxis[0] && option.xAxis[0].data) || [];
						/* originalPctLabels cache:首次成功讀到 secondary xAxis label slice
						   後 cache 給後續切換 phase checkbox 想還原「全勾原始百分比」時用。 */
						if (window.__cciOriginalPctLabels === undefined && option.xAxis[1] && option.xAxis[1].data) {
							window.__cciOriginalPctLabels = option.xAxis[1].data.slice();
						}
						const cached = window.__cciOriginalPctLabels;
						let minT = 0, maxT = 0;
						if (checkedPhases.length > 0) {
							minT = checkedPhases[0].time;
							maxT = checkedPhases[0].time;
							for (let i = 1; i < checkedPhases.length; i++) {
								const t = checkedPhases[i].time;
								if (t < minT) minT = t;
								if (t > maxT) maxT = t;
							}
						}
						const duration = maxT - minT;
						/* secondary xAxis 百分比 label 重建:
						   - 全勾 + cache → 還原 cache(避免浮點累積誤差)
						   - >=2 個勾 + duration > 0 → 線性重算
						   - else fallback cache 或 0.0% */
						let newPctLabels;
						if (allChecked && cached) {
							newPctLabels = cached;
						} else if (checkedPhases.length >= 2 && duration > 0 && isFinite(duration)) {
							newPctLabels = xLabels.map(function(label) {
								const tVal = parseFloat(label);
								return ((tVal - minT) / duration * 100).toFixed(1) + '%';
							});
						} else if (cached) {
							newPctLabels = cached;
						} else {
							newPctLabels = xLabels.map(function() { return '0.0%'; });
						}
						/* markData via findNearestLabel(category axis 必須對映 string label) */
						const findNearestLabel = window.__phaseMarkers.findNearestLabel;
						const markData = [];
						for (let i = 0; i < checkedPhases.length; i++) {
							const p = checkedPhases[i];
							const pct = (typeof p.pct === 'number') ? p.pct : 0;
							const nearestLabel = findNearestLabel(p.time, xLabels);
							if (!nearestLabel) continue;
							markData.push({
								name: p.name + '\n(' + pct.toFixed(1) + '%)',
								xAxis: nearestLabel,
								lineStyle: { type: 'dashed', color: '#888', width: 1 },
								label: { show: true, formatter: '{b}', position: 'end' }
							});
						}
						/* 找 targetIdx(legendSelected 第一條 mean curve,跳過 ±SD pair) */
						if (option.series && option.series.length > 0) {
							const legendSelected = (option.legend && option.legend[0] && option.legend[0].selected) || {};
							let targetIdx = 0;
							for (let i = 0; i < option.series.length; i++) {
								const name = option.series[i].name || '';
								if (name.indexOf(' -SD') !== -1 || name.indexOf(' +SD') !== -1) continue;
								if (legendSelected[name] !== false) {
									targetIdx = i;
									break;
								}
							}
							const seriesUpdate = option.series.map(function(s, i) {
								if (i === targetIdx) {
									return { markLine: { silent: true, symbol: ['none','none'], data: markData } };
								}
								if (s.markLine && s.markLine.data && s.markLine.data.length > 0) {
									return { markLine: { data: [] } };
								}
								return {};
							});
							myChart.setOption({ series: seriesUpdate, xAxis: [{}, { data: newPctLabels }] });
						}
					} catch (err) {
						try { console.error('cci-update-phase-markers 失敗:', err); } catch (e2) {}
					}
				}
			});
		}
	`

	line.AddJSFuncStrs(opts.FuncOpts(customJS))
}

// toInterfaceSlice converts []string to []interface{} for go-echarts.
func toInterfaceSlice(ss []string) []any {
	result := make([]any, len(ss))
	for i, s := range ss {
		result[i] = s
	}

	return result
}
