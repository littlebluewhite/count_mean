package chart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"

	"count_mean/internal/models"
)

// composerDownsampleThreshold Chart Composer 對 EMG / muscle_ratio 的 LTTB 目標點數。
// 與 cci.cciChartDownsampleThreshold 對齊(5000):視覺上 zoom-in 仍夠細,前端渲染壓力降一個量級。
const composerDownsampleThreshold = 5000

// composerCtxCheckInterval 控制 hot loop 每幾筆檢查一次 ctx.Done(),
// 對齊 cci.cciChartCtxCheckInterval = 1024 — chart 點數規模小,密 cadence 提高 cancel-latency 敏感度。
const composerCtxCheckInterval = 1024

// ErrComposerEMGRequired 表示 RenderComposer 收到 nil EMGDataset。
// EMG 是 composer 必要輸入(整張圖至少含一個 EMG grid),nil 直接 fail-fast。
var ErrComposerEMGRequired = errors.New("composer: EMGDataset 不可為 nil")

// MotionData composer 自有 motion 資料載體;結構刻意脫離 models.MotionData(index-based)
// 改為時間軸 + 多 series,避免 chart package 反向依賴 motion-domain semantics。
//
//   - Time: motion 時間序列(秒);長度與每個 series 一致
//   - Series: channel 名稱 → 值序列
//   - Order: 渲染順序,讓 series 出現順序穩定可預測(map iteration 不保證順序)
type MotionData struct {
	Time   []float64
	Series map[string][]float64
	Order  []string
}

// MuscleRatioData 與 MotionData 同形,專屬 muscle ratio grid 使用。
// 拆兩個 type 而非共用 alias:語意上是不同 domain,且未來 muscle_ratio 可能加上
// per-pair 額外欄位(如 reference channel 名稱),先拆能避免後續 backward-compat 衝突。
type MuscleRatioData struct {
	Time   []float64
	Series map[string][]float64
	Order  []string
}

// ComposerInput 是 RenderComposer 的全部輸入。所有 user-controlled 字串(Subject、
// SelectedChannels、MotionData.Order 等)會在內部走 SanitizeChartString 防 XSS。
//
// MuscleRatioData == nil → 2 grid 配置(EMG + motion)
// MuscleRatioData != nil → 3 grid 配置(EMG + muscle_ratio + motion)
//
// EMGMotionOffset 為 motion-time 對 EMG-time 的位移(以 motion frame 計);
// 目前 composer 不對 motion-time 做位移轉換(motion data 已是時間序列),保留欄位
// 為 caller 上層(Wails handler)在準備 MotionData 時換算之用。
type ComposerInput struct {
	Subject          string
	EMGDataset       *models.EMGDataset
	SelectedChannels []string
	MuscleRatioData  *MuscleRatioData // nil → 2-grid (EMG + motion)
	MotionData       *MotionData
	PhasePoints      models.PhasePoints
	EMGMotionOffset  int
}

// RenderComposer 把 Subject 的 EMG + (可選) muscle_ratio + motion 渲染成
// 單一 ECharts instance + N stacked grids 的 HTML。
//
// 行為合約:
//   - MuscleRatioData == nil → 2 grid;非 nil → 3 grid
//   - EMG / muscle_ratio dataset > 5000 點時跑 LTTB(threshold 5000)
//   - motion 不 downsample(對齊 ADR-0002)
//   - PhasePoints 翻成秒值後 attach 為 dashed grey markLine(沿用 CCI 樣式)
//   - dataZoom slider + inside 聯動全部 grid
//   - tooltip 走 ECharts native axisPointer 跨 grid
//
// ctx-aware:入口先 check ctx;hot loop 每 composerCtxCheckInterval 點 select 檢查。
// cancel 路徑不寫出半成品 HTML。
func RenderComposer(ctx context.Context, in ComposerInput, w io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if in.EMGDataset == nil {
		return ErrComposerEMGRequired
	}

	line, err := buildComposerLine(ctx, in)
	if err != nil {
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := line.Render(w); err != nil {
		return fmt.Errorf("composer render: %w", err)
	}

	return nil
}

// gridSpec 描述單一 stacked grid 的 layout(top% / height%)。
type gridSpec struct {
	top    string
	height string
}

// computeGridLayout 把 N 個 grid 在垂直方向均分,首 grid top 從 90px(留 title + legend),
// 末 grid bottom 留 80px(dataZoom slider)。height = (100% - 200px) / N。
//
// 為何用 px + % 混合而非純 %:title / legend / slider 高度固定,純 % 在容器高度變化下
// 比例失準(legend 會撞到第一 grid)。混合表達法是 echarts 官方推薦(option.grid 範例)。
func computeGridLayout(n int) []gridSpec {
	if n <= 0 {
		return nil
	}
	specs := make([]gridSpec, n)
	// 預留 90px 給 title+legend(top)、80px 給 dataZoom slider(bottom);
	// 並在 grid 之間留 40px gap 讓 top-axis label 可見
	const reservedTop = 90
	const reservedBottom = 80
	const gapPx = 40
	totalGap := gapPx * (n - 1)
	// 每 grid 高 = (容器 px 高 - reserved - gaps) / N;用百分比表達需仰賴容器 height,
	// 改用 calc 形式:height = (100% - X px) / N
	for i := 0; i < n; i++ {
		topPx := reservedTop + i*(gapPx)
		specs[i] = gridSpec{
			top:    fmt.Sprintf("calc((100%% - %dpx) * %d / %d + %dpx)", reservedTop+reservedBottom+totalGap, i, n, topPx),
			height: fmt.Sprintf("calc((100%% - %dpx) / %d)", reservedTop+reservedBottom+totalGap, n),
		}
	}
	return specs
}

// buildComposerLine 配置整張 multi-grid chart。
// 步驟:
//  1. 算出 N(2 or 3)、grid layout
//  2. 設 global options(title / tooltip / legend / dataZoom / toolbox / N 個 grid)
//  3. 為每個 grid 設 bottom-time + top-percent xAxis(共 2N 個)、Y axis(N 個)
//  4. 為每個 grid 加 series(EMG / muscle_ratio downsample;motion 原樣)
//  5. 第一條 series attach phase markLine
//  6. 加 custom JS(resize + 鍵盤 R reset zoom)
func buildComposerLine(ctx context.Context, in ComposerInput) (*charts.Line, error) {
	// 計算 grid 配置
	n := 2
	hasMuscle := in.MuscleRatioData != nil
	if hasMuscle {
		n = 3
	}
	grids := computeGridLayout(n)

	// EMG / muscle_ratio downsample
	emgTime, emgSeries, err := buildEMGSeries(ctx, in.EMGDataset, in.SelectedChannels)
	if err != nil {
		return nil, err
	}

	var muscleTime []float64
	var muscleSeries map[string][]float64
	if hasMuscle {
		muscleTime, muscleSeries = downsampleSeriesMap(in.MuscleRatioData.Time, in.MuscleRatioData.Series, composerDownsampleThreshold)
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	line := charts.NewLine()
	setComposerGlobalOptions(line, in.Subject, grids, n)

	// X axes:每 grid 一 bottom-time(category) + 一 top-percent(category)
	// XAxisList[0] 是 echarts 預設;後續 ExtendXAxis 追加
	// 對齊原則:bottom xAxis index = i(0..n-1),top xAxis index = n+i(n..2n-1)
	if err := configureComposerAxes(ctx, line, n, emgTime, muscleTime, in.MotionData, &in.PhasePoints); err != nil {
		return nil, err
	}

	// Series:依序加 EMG(grid 0)、muscle_ratio(grid 1, if any)、motion(最後一個 grid)
	if err := addComposerSeries(ctx, line, n, emgTime, emgSeries, muscleTime, muscleSeries, in.MotionData, in.PhasePoints, hasMuscle); err != nil {
		return nil, err
	}

	addComposerCustomJS(line)

	return line, nil
}

// buildEMGSeries 從 EMGDataset 取出 selected channels,跑 LTTB downsample。
//
// channelIdx-based 對映:Headers[0] 是 time,channels 由 EMGDataset.Data[i].Channels 索引。
// SelectedChannels 用 channel 名稱對 Headers[1:] 比對;不存在的名稱 silently skip。
// SelectedChannels 為空時 fallback 顯示全部 channel(對齊 echarts_generator
// ShowAllColumns 行為)。
//
// 回傳:downsampled time slice + map(channel name → downsampled values)。
func buildEMGSeries(
	ctx context.Context,
	dataset *models.EMGDataset,
	selectedChannels []string,
) ([]float64, map[string][]float64, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	headers := dataset.Headers
	channelCount := len(headers) - 1 // headers[0] = "time"

	// channelIdx 表(name → row.Channels 索引)
	indexByName := make(map[string]int, channelCount)
	for c := 0; c < channelCount; c++ {
		indexByName[headers[c+1]] = c
	}

	// 解析要 render 的 channel 列表:caller 指定 → 取交集;空 → 全部
	useChannels := selectedChannels
	if len(useChannels) == 0 {
		useChannels = make([]string, channelCount)
		for c := 0; c < channelCount; c++ {
			useChannels[c] = headers[c+1]
		}
	}

	// 為合法 channel 配置 columnar slice(name → values)
	n := len(dataset.Data)
	rawMap := make(map[string][]float64, len(useChannels))
	channelIdxList := make([]int, 0, len(useChannels))
	nameList := make([]string, 0, len(useChannels))
	for _, name := range useChannels {
		idx, ok := indexByName[name]
		if !ok {
			continue
		}
		rawMap[name] = make([]float64, n)
		channelIdxList = append(channelIdxList, idx)
		nameList = append(nameList, name)
	}

	// 單一 pass:填 time + 各 channel values。ctx-aware loop。
	timeRaw := make([]float64, n)
	for i, row := range dataset.Data {
		if i > 0 && i%composerCtxCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			default:
			}
		}
		timeRaw[i] = row.Time
		for k, channelIdx := range channelIdxList {
			if channelIdx < len(row.Channels) {
				rawMap[nameList[k]][i] = row.Channels[channelIdx]
			} else {
				// ragged row:該 channel 在本 row 缺值 → NaN(下游 LineData.Value = nil)
				rawMap[nameList[k]][i] = math.NaN()
			}
		}
	}

	downTime, downMap := downsampleSeriesMap(timeRaw, rawMap, composerDownsampleThreshold)
	return downTime, downMap, nil
}

// downsampleSeriesMap 對共享同一條 time series 的多個 channel 一起做 LTTB:
//   - 每 channel 各自跑 LTTB → union 索引 → 重組 time + 各 channel slice
//   - 對齊「all series share one X-axis」的 invariant
//   - threshold <= 0 or 長度 <= threshold 直接 passthrough
func downsampleSeriesMap(
	time []float64,
	series map[string][]float64,
	threshold int,
) ([]float64, map[string][]float64) {
	if len(time) <= threshold || threshold <= 0 || len(series) == 0 {
		// passthrough — return copies 還是 share?CCI 對 <=threshold path 直接 return 原 ref;
		// 這裡同策略,caller(composer)不會 mutate 中間結果。
		return time, series
	}

	// per-channel LTTB → union 索引
	seen := make(map[int]struct{}, threshold)
	for _, vals := range series {
		if len(vals) != len(time) {
			// 長度不符 → skip(不該污染 union;但也不 hard reject,保守 graceful 處理)
			continue
		}
		idx := LTTBDownsample(time, vals, threshold)
		for _, i := range idx {
			seen[i] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return time, series
	}

	indices := make([]int, 0, len(seen))
	for i := range seen {
		indices = append(indices, i)
	}
	sort.Ints(indices)

	// stride cap — 對齊 CCI cciChartMaxRenderPoints(2x threshold)
	indices = capComposerUnionIndices(indices, threshold*2)

	newTime := make([]float64, len(indices))
	for i, idx := range indices {
		newTime[i] = time[idx]
	}
	newSeries := make(map[string][]float64, len(series))
	for name, vals := range series {
		if len(vals) != len(time) {
			// 長度不符的 channel 不被 downsample,但 X 軸對齊不上 — 跳過此 series
			continue
		}
		out := make([]float64, len(indices))
		for i, idx := range indices {
			out[i] = vals[idx]
		}
		newSeries[name] = out
	}
	return newTime, newSeries
}

// capComposerUnionIndices stride decimation 對齊 CCI capUnionIndices ceiling-division 邏輯。
// 保留首末索引維持 zoom 範圍。
func capComposerUnionIndices(indices []int, limit int) []int {
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
	if last := indices[len(indices)-1]; len(capped) == 0 || capped[len(capped)-1] != last {
		capped = append(capped, last)
	}
	return capped
}

// setComposerGlobalOptions 設置 chart-level 全域選項:
//   - title:Composer / Subject
//   - tooltip:axis trigger + cross pointer(原生跨 grid hover)
//   - legend:頂部 scroll
//   - dataZoom:slider + inside,xAxisIndex 涵蓋全部 bottom+top(0..2n-1)
//   - toolbox:zoom / restore / dataView
//   - grid:N 個 stacked
func setComposerGlobalOptions(line *charts.Line, subject string, grids []gridSpec, n int) {
	safeSubject := SanitizeChartString(subject)

	// dataZoom 聯動的 xAxisIndex list:[0, 1, ..., 2n-1]
	zoomIndices := make([]int, 0, 2*n)
	for i := 0; i < 2*n; i++ {
		zoomIndices = append(zoomIndices, i)
	}

	gridOpts := make([]opts.Grid, 0, n)
	for _, g := range grids {
		gridOpts = append(gridOpts, opts.Grid{
			Top:    g.top,
			Left:   "60px",
			Right:  "60px",
			Height: g.height,
		})
	}

	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Width:  "100%",
			Height: "900px",
		}),
		charts.WithTitleOpts(opts.Title{
			Title:    SanitizeChartString("Chart Composer"),
			Subtitle: fmt.Sprintf("Subject: %s", safeSubject),
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
			Top:    "50px",
			Orient: "horizontal",
			Type:   "scroll",
		}),
		charts.WithDataZoomOpts(
			opts.DataZoom{
				Type:       "inside",
				Start:      0,
				End:        100,
				XAxisIndex: zoomIndices,
			},
			opts.DataZoom{
				Type:       "slider",
				Start:      0,
				End:        100,
				XAxisIndex: zoomIndices,
			},
		),
		charts.WithToolboxOpts(opts.Toolbox{
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
		}),
		charts.WithGridOpts(gridOpts...),
	)
}

// configureComposerAxes 設置 N 個 bottom xAxis(time)+ N 個 top xAxis(percent)+ N 個 yAxis。
//
// echarts 預設 XAxisList / YAxisList 各 1 個(initXYAxis);ExtendXAxis / ExtendYAxis 追加。
//
// 為何 N=2 時 bottom_idx=0,1 / top_idx=2,3;N=3 時 bottom_idx=0,1,2 / top_idx=3,4,5:
// dataZoom XAxisIndex 才能用單一 [0..2n-1] 表達聯動,讓 inside-zoom 自動同步所有 axis。
//
// percent axis 計算規則:
//   - EMG / muscle_ratio:以該 grid 的 time series duration 為 100%
//   - motion:同樣對映自己的 time series
//
// 為簡化 caller 端,percent 直接 derive 自 time series(time[0] 為 0%,time[len-1] 為 100%)。
func configureComposerAxes(
	ctx context.Context,
	line *charts.Line,
	n int,
	emgTime []float64,
	muscleTime []float64,
	motion *MotionData,
	_ *models.PhasePoints,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Bottom time axes — 每 grid 對應一條 time series。
	// grid 順序:0 = EMG, 1 = muscle(if 3-grid), last = motion
	gridTimes := make([][]float64, 0, n)
	gridTimes = append(gridTimes, emgTime)
	if n == 3 {
		gridTimes = append(gridTimes, muscleTime)
	}
	if motion != nil {
		gridTimes = append(gridTimes, motion.Time)
	} else {
		// motion 缺席:用 EMG time 充當(罕見路徑,不該到此 — caller 應提供 motion)
		gridTimes = append(gridTimes, emgTime)
	}

	// XAxisList[0] 預設存在 → 直接 SetXAxis 為 emg time labels(LineData.SetXAxis 只填第 0 軸)
	emgTimeLabels, err := buildComposerTimeLabels(ctx, emgTime)
	if err != nil {
		return err
	}
	line.SetXAxis(toComposerInterfaceSlice(emgTimeLabels))

	// 設 axis[0] 的 name 為 "Time (s)"(透過 WithXAxisOpts index=0)
	line.SetGlobalOptions(
		charts.WithXAxisOpts(opts.XAxis{
			Type:        "category",
			Name:        "Time (s)",
			Data:        toComposerInterfaceSlice(emgTimeLabels),
			BoundaryGap: opts.Bool(false),
			GridIndex:   0,
		}, 0),
		charts.WithYAxisOpts(opts.YAxis{
			Type:      "value",
			Name:      "EMG",
			NameGap:   30,
			GridIndex: 0,
		}, 0),
	)

	// Extend 其餘 N-1 個 bottom time xAxis(對應 grid 1..n-1)
	for i := 1; i < n; i++ {
		labels, err := buildComposerTimeLabels(ctx, gridTimes[i])
		if err != nil {
			return err
		}
		var name string
		switch {
		case n == 3 && i == 1:
			name = "Muscle Ratio (s)"
		default:
			name = "Motion (s)"
		}
		line.ExtendXAxis(opts.XAxis{
			Type:        "category",
			Name:        name,
			Data:        toComposerInterfaceSlice(labels),
			BoundaryGap: opts.Bool(false),
			GridIndex:   i,
		})
		// 對應 Y axis(grid i)
		var yName string
		switch {
		case n == 3 && i == 1:
			yName = "Ratio"
		default:
			yName = "Motion"
		}
		line.ExtendYAxis(opts.YAxis{
			Type:      "value",
			Name:      yName,
			NameGap:   30,
			GridIndex: i,
		})
	}

	// Top percent axes — N 個,grid 0..n-1 各一個 position=top
	for i := 0; i < n; i++ {
		pctLabels := buildComposerPercentLabels(gridTimes[i])
		line.ExtendXAxis(opts.XAxis{
			Type:        "category",
			Position:    "top",
			Name:        "Time (%)",
			Data:        toComposerInterfaceSlice(pctLabels),
			BoundaryGap: opts.Bool(false),
			GridIndex:   i,
			AxisLabel: &opts.AxisLabel{
				Show:     opts.Bool(true),
				Interval: "20",
			},
			AxisTick:  &opts.AxisTick{Show: opts.Bool(false)},
			SplitLine: &opts.SplitLine{Show: opts.Bool(false)},
		})
	}

	return nil
}

// buildComposerTimeLabels 把 float64 時間軸格式化為「%.3f」字串 — 對齊 CCI 風格。
// hot loop ctx-aware:每 composerCtxCheckInterval 點 check 一次。
func buildComposerTimeLabels(ctx context.Context, timeValues []float64) ([]string, error) {
	labels := make([]string, len(timeValues))
	for i, t := range timeValues {
		if i > 0 && i%composerCtxCheckInterval == 0 {
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

// buildComposerPercentLabels duration 為 0 / 空 slice 時 fallback 全 "0.0%"。
// 對齊 CCI buildPercentAxisLabels 的責任分工:caller 保證 duration > 0,此處 defensive。
func buildComposerPercentLabels(timeValues []float64) []string {
	if len(timeValues) == 0 {
		return []string{}
	}
	start := timeValues[0]
	end := timeValues[len(timeValues)-1]
	duration := end - start
	labels := make([]string, len(timeValues))
	if duration <= 0 {
		for i := range labels {
			labels[i] = "0.0%"
		}
		return labels
	}
	for i, t := range timeValues {
		pct := (t - start) / duration * 100
		labels[i] = fmt.Sprintf("%.1f%%", pct)
	}
	return labels
}

// addComposerSeries 把所有 series attach 到對應 grid。Series 順序:
//   - EMG channels(grid 0)
//   - muscle_ratio channels(grid 1,若 3-grid)
//   - motion channels(grid n-1)
//
// 第一個 series(EMG 首條)會 carry phase markLine — markLine 透過 XAxisIndex 鎖在 grid 0,
// 但因為 echarts dataZoom 聯動,visual 上會在所有 grid 對齊。
//
// markLine 樣式對齊 CCI:dashed grey(#808080 / dashed)。
func addComposerSeries(
	ctx context.Context,
	line *charts.Line,
	n int,
	emgTime []float64,
	emgSeries map[string][]float64,
	muscleTime []float64,
	muscleSeries map[string][]float64,
	motion *MotionData,
	phase models.PhasePoints,
	hasMuscle bool,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// EMG series → grid 0
	firstSeries := true
	emgNames := sortedKeys(emgSeries)
	for _, name := range emgNames {
		vals := emgSeries[name]
		if err := ctx.Err(); err != nil {
			return err
		}
		lineData, err := buildComposerLineData(ctx, vals)
		if err != nil {
			return err
		}
		seriesOpts := []charts.SeriesOpts{
			charts.WithLineChartOpts(opts.LineChart{
				Smooth:     opts.Bool(false),
				ShowSymbol: opts.Bool(false),
				XAxisIndex: 0,
				YAxisIndex: 0,
			}),
			charts.WithLineStyleOpts(opts.LineStyle{Width: 1.5}),
		}
		if firstSeries {
			seriesOpts = append(seriesOpts, composerPhaseMarkLineOpts(phase)...)
			firstSeries = false
		}
		line.AddSeries(SanitizeChartString(name), lineData).SetSeriesOptions(seriesOpts...)
	}

	// muscle_ratio series → grid 1(僅 3-grid)
	if hasMuscle {
		muscleNames := sortedKeys(muscleSeries)
		_ = muscleTime
		for _, name := range muscleNames {
			vals := muscleSeries[name]
			lineData, err := buildComposerLineData(ctx, vals)
			if err != nil {
				return err
			}
			line.AddSeries(SanitizeChartString(name), lineData).
				SetSeriesOptions(
					charts.WithLineChartOpts(opts.LineChart{
						Smooth:     opts.Bool(false),
						ShowSymbol: opts.Bool(false),
						XAxisIndex: 1,
						YAxisIndex: 1,
					}),
					charts.WithLineStyleOpts(opts.LineStyle{Width: 1.5}),
				)
		}
	}

	// motion series → grid n-1
	if motion != nil {
		motionGridIdx := n - 1
		// 依 Order 渲染,確保順序穩定
		for _, name := range motion.Order {
			vals, ok := motion.Series[name]
			if !ok {
				continue
			}
			lineData, err := buildComposerLineData(ctx, vals)
			if err != nil {
				return err
			}
			line.AddSeries(SanitizeChartString(name), lineData).
				SetSeriesOptions(
					charts.WithLineChartOpts(opts.LineChart{
						Smooth:     opts.Bool(false),
						ShowSymbol: opts.Bool(false),
						XAxisIndex: motionGridIdx,
						YAxisIndex: motionGridIdx,
					}),
					charts.WithLineStyleOpts(opts.LineStyle{Width: 1.5}),
				)
		}
	}
	_ = emgTime
	return nil
}

// buildComposerLineData 把 float64 values 轉成 opts.LineData,NaN/Inf → nil(線斷開)。
// hot loop ctx-aware:每 composerCtxCheckInterval 點 check。
func buildComposerLineData(ctx context.Context, vals []float64) ([]opts.LineData, error) {
	out := make([]opts.LineData, len(vals))
	for i, v := range vals {
		if i > 0 && i%composerCtxCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			out[i] = opts.LineData{Value: nil}
		} else {
			out[i] = opts.LineData{Value: v}
		}
	}
	return out, nil
}

// composerPhaseMarkLineOpts 把 PhasePoints 已 Set 的 OptFloat 轉成 markLine XAxis items。
// motion-index 欄位(D/O)目前不轉換(屬於 motion-domain;chart composer 不負責 conversion);
// 由 caller 上層在準備 input 時透過 EMGMotionOffset 換算成秒。
//
// 對齊 CCI markLine 風格:dashed grey、symbol none。
func composerPhaseMarkLineOpts(phase models.PhasePoints) []charts.SeriesOpts {
	// 收集 (name, sec) pairs;Set=false 的 OptFloat skip
	type pt struct {
		name string
		sec  float64
	}
	pairs := make([]pt, 0, 10)
	add := func(name string, opt models.OptFloat) {
		if v, ok := opt.Get(); ok {
			pairs = append(pairs, pt{name: name, sec: v})
		}
	}
	add("P0", phase.P0)
	add("P1", phase.P1)
	add("P2", phase.P2)
	add("S", phase.S)
	add("C", phase.C)
	add("T0", phase.T0)
	add("T", phase.T)
	add("L", phase.L)
	// D / O 是 motion-index sentinel,不在 chart composer 處理;caller 換算後可
	// 透過 PhasePoints 別的 OptFloat 欄位(若未來引入)或自行 inject。

	if len(pairs) == 0 {
		return nil
	}

	items := make([]opts.MarkLineNameXAxisItem, 0, len(pairs))
	for _, p := range pairs {
		items = append(items, opts.MarkLineNameXAxisItem{
			Name:  p.name,
			XAxis: fmt.Sprintf("%.3f", p.sec),
		})
	}

	return []charts.SeriesOpts{
		charts.WithMarkLineStyleOpts(opts.MarkLineStyle{
			Symbol:     []string{"none", "none"},
			SymbolSize: 0,
			LineStyle: &opts.LineStyle{
				Color: "#808080",
				Type:  "dashed",
				Width: 1.0,
			},
			Label: &opts.Label{
				Show:     opts.Bool(true),
				Position: "insideEndTop",
			},
		}),
		charts.WithMarkLineNameXAxisItemOpts(items...),
	}
}

// addComposerCustomJS 加入 resize handler、鍵盤 R reset zoom。
// 與 CCI 風格一致但 composer 不需要 postMessage 給 parent(沒有 phase line 同步事件),
// 等 Slice C handler 視需求再注入。
func addComposerCustomJS(line *charts.Line) {
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
		}
	`
	line.AddJSFuncStrs(opts.FuncOpts(customJS))
}

// sortedKeys 對 map[string][]float64 取 keys 並排序;讓 series 順序可預測。
func sortedKeys(m map[string][]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// toComposerInterfaceSlice converts []string to []any for go-echarts X axis data.
func toComposerInterfaceSlice(ss []string) []any {
	result := make([]any, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}
