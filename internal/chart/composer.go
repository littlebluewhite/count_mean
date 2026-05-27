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

// gridSpec 描述單一 stacked grid 的 layout(pixel top / height)。
//
// 為何用純 pixel 而非 percent 或 CSS calc():
//   - ECharts 的 grid.top / grid.height 只解析數字(pixel)或簡單百分比字串(如 "30%"),
//     **不解析 CSS calc(...)** — calc 字串原樣序列化進 JSON,前端解析失敗 layout 破版。
//   - 純 percent 在容器高度變動時 reserved-top (title+legend) / reserved-bottom (dataZoom slider)
//     比例會失準(legend 撞第一 grid)。
//   - 既然 RenderComposer 透過 Initialization.Height 固定容器 900px,直接 emit 絕對 pixel
//     最穩;後續 Slice D 若改變容器高度,需同步更新 composerContainerHeight。
type gridSpec struct {
	top    string
	height string
}

// composerContainerHeight 是 Initialization.Height 對應的容器 pixel 高度。
// 必須與 setComposerGlobalOptions 中的 Height: "900px" 同步;若任一改變,grid 排版
// 比例會錯。Slice D frontend 應維持同樣高度的 iframe 容器。
const composerContainerHeight = 900

// computeGridLayout 把 N 個 grid 在垂直方向均分(以絕對 pixel 計算):
//   - 首 grid top 從 90px(留 title + legend);
//   - 末 grid bottom 留 80px(dataZoom slider);
//   - 每 grid 之間留 40px gap 讓相鄰 grid 的 top-axis label 可見;
//   - gridHeight = (containerHeight - reservedTop - reservedBottom - gap*(N-1)) / N。
//
// emit 純 pixel 字串("90" 而非 "90px");ECharts 對純數字 string 視為 pixel。
// 避免使用 CSS calc(),ECharts 不會解析。
func computeGridLayout(n int) []gridSpec {
	if n <= 0 {
		return nil
	}
	const reservedTop = 90
	const reservedBottom = 80
	const gapPx = 40
	totalGap := gapPx * (n - 1)
	available := composerContainerHeight - reservedTop - reservedBottom - totalGap
	gridHeight := available / n
	specs := make([]gridSpec, n)
	for i := 0; i < n; i++ {
		topPx := reservedTop + i*(gridHeight+gapPx)
		specs[i] = gridSpec{
			top:    fmt.Sprintf("%d", topPx),
			height: fmt.Sprintf("%d", gridHeight),
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

	// X axes:每 grid 一 bottom-time(value 軸,秒)+ 一 top-percent(value 軸,0-100)
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

// configureComposerAxes 設置 N 個 bottom xAxis(time, value 軸)+ N 個 top xAxis(percent, value 軸)+ N 個 yAxis。
//
// echarts 預設 XAxisList / YAxisList 各 1 個(initXYAxis);ExtendXAxis / ExtendYAxis 追加。
//
// 為何 N=2 時 bottom_idx=0,1 / top_idx=2,3;N=3 時 bottom_idx=0,1,2 / top_idx=3,4,5:
// dataZoom XAxisIndex 才能用單一 [0..2n-1] 表達聯動,讓 inside-zoom 自動同步所有 axis。
//
// 為何全部 xAxis 用 Type:"value" 而非 "category":
//   - category axis 把 series data 視為 ordinal index 對映而非絕對時間 — 當 EMG 與 motion
//     有不同 sampling rate / 起始時間時,「同一秒」的兩組樣本會落在不同 x 位置,phase
//     markLine / tooltip / dataZoom 全錯位。
//   - value axis 從 series data 的 [time, value] pair 推 min/max,時間維度被忠實保留;
//     不同 series 不同採樣率 / 不同起始時間都對得齊。
//   - 代價:caller 必須把 series data 從 []v 改為 [][t,v] pair 形式(addComposerSeries 處理)。
//
// Top percent axes 同樣用 Type:"value",並設 Min:0/Max:100 固定範圍,避免「無 series
// 對應」時 echarts 推不出 min/max 而 axis 不顯示。
func configureComposerAxes(
	ctx context.Context,
	line *charts.Line,
	n int,
	_ []float64, // emgTime — kept for signature stability; not needed on value axis
	_ []float64, // muscleTime
	_ *MotionData, // motion
	_ *models.PhasePoints,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// XAxis[0] 預設存在 → 用 SetGlobalOptions 設 bottom time axis (grid 0)。
	// 不 SetXAxis(...) — 那只對 category 軸有意義(填 .Data);value 軸由 series data 推。
	line.SetGlobalOptions(
		charts.WithXAxisOpts(opts.XAxis{
			Type:      "value",
			Name:      "Time (s)",
			GridIndex: 0,
		}, 0),
		charts.WithYAxisOpts(opts.YAxis{
			Type:      "value",
			Name:      "EMG",
			NameGap:   30,
			GridIndex: 0,
		}, 0),
	)

	// Extend 其餘 N-1 個 bottom time xAxis(對應 grid 1..n-1)+ 對應 Y axis。
	for i := 1; i < n; i++ {
		var name string
		switch {
		case n == 3 && i == 1:
			name = "Muscle Ratio (s)"
		default:
			name = "Motion (s)"
		}
		line.ExtendXAxis(opts.XAxis{
			Type:      "value",
			Name:      name,
			GridIndex: i,
		})
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

	// Top percent axes — N 個,grid 0..n-1 各一個 position=top。
	// 沒有 series 對應 top axes(series 只 attach 到 bottom xAxisIndex);Min:0 / Max:100
	// 確保 axis ticks 在 0-100% 範圍。caller 上層應對齊 bottom 軸 zoom range 對應 0-100%
	// (frontend 透過 dataZoom 同步,本層不負責)。
	for i := 0; i < n; i++ {
		minVal := 0.0
		maxVal := 100.0
		line.ExtendXAxis(opts.XAxis{
			Type:      "value",
			Position:  "top",
			Name:      "Time (%)",
			GridIndex: i,
			Min:       minVal,
			Max:       maxVal,
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

// addComposerSeries 把所有 series attach 到對應 grid。Series 順序:
//   - EMG channels(grid 0)
//   - muscle_ratio channels(grid 1,若 3-grid)
//   - motion channels(grid n-1)
//
// 第一個 series(EMG 首條)會 carry phase markLine — markLine 透過 XAxisIndex 鎖在 grid 0,
// 但因為 echarts dataZoom 聯動,visual 上會在所有 grid 對齊。
//
// Series data 格式:`[]opts.LineData{ {Value: [t1, v1]}, {Value: [t2, v2]}, ... }`。
// 因為 xAxis Type 為 "value"(見 configureComposerAxes doc),series data 必須以
// [time, value] pair 形式提供,echarts 才能正確 anchor 每筆樣本到絕對時間。
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
		lineData, err := buildComposerLineData(ctx, emgTime, vals)
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
		for _, name := range muscleNames {
			vals := muscleSeries[name]
			lineData, err := buildComposerLineData(ctx, muscleTime, vals)
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
			lineData, err := buildComposerLineData(ctx, motion.Time, vals)
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
	return nil
}

// buildComposerLineData 把 (time, values) pair 轉成 opts.LineData 序列。
//
// 因 xAxis Type=value (見 configureComposerAxes doc),每筆 LineData.Value 必須是
// [time, value] 二元 slice;echarts 從這個 pair 直接 anchor 樣本到絕對時間軸,
// 不同 sampling rate / 不同起始時間的 series 自動對齊。
//
// NaN/Inf value 或 NaN/Inf time → LineData.Value=nil(線斷開、不繪製)。time 應該
// 不會 NaN(來自 EMG.Time / motion.Time / muscle.Time,parser 已校驗);保留防禦
// 用以對齊 CCI 的 NaN-row dropping invariant。
//
// time / vals 長度不等 → 用 min(len(time), len(vals)) 截斷;這是 caller bug
// (composer 內部都同源),不該發生,defensively handle 避免 panic。
//
// hot loop ctx-aware:每 composerCtxCheckInterval 點 check。
func buildComposerLineData(ctx context.Context, time []float64, vals []float64) ([]opts.LineData, error) {
	n := len(vals)
	if len(time) < n {
		n = len(time)
	}
	out := make([]opts.LineData, n)
	for i := 0; i < n; i++ {
		if i > 0 && i%composerCtxCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}
		t := time[i]
		v := vals[i]
		if math.IsNaN(t) || math.IsInf(t, 0) || math.IsNaN(v) || math.IsInf(v, 0) {
			out[i] = opts.LineData{Value: nil}
		} else {
			out[i] = opts.LineData{Value: []any{t, v}}
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
		// xAxis Type 為 value(見 configureComposerAxes doc) → markLine.XAxis 必須是
		// 數值(float64),echarts 才能 anchor 到絕對時間。category 軸時代用 string
		// 表示「Data 陣列的某個 label」,改 value 軸後 string 會被視為非數值丟棄。
		items = append(items, opts.MarkLineNameXAxisItem{
			Name:  p.name,
			XAxis: p.sec,
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

