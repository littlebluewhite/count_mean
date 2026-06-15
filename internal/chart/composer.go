package chart

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/opts"

	"count_mean/internal/chart/assets"
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
//
// PhaseTimesEMG 是 phase 名 → EMG 秒數 map,已由 caller 統一換算成 EMG 時間 domain
// (力板時間欄位走 ForceTimeToEMGTime;motion-index 欄位 D/O 走 MotionIndexToEMGTime)。
// chart 套件不持有 conversion 知識,只把秒值 anchor 成 markLine —— 與 handler 回給
// 前端的 phaseTimes 為同一份 map,確保前端 checkbox 與後端 markLine 不分歧。
type ComposerInput struct {
	Subject          string
	EMGDataset       *models.EMGDataset
	SelectedChannels []string
	MuscleRatioData  *MuscleRatioData // nil → 2-grid (EMG + motion)
	MotionData       *MotionData
	PhaseTimesEMG    map[string]float64
	EMGMotionOffset  int
}

// RenderComposer 把 Subject 的 EMG + (可選) muscle_ratio + motion 渲染成
// 單一 ECharts instance + N stacked grids 的 HTML。
//
// 行為合約:
//   - MuscleRatioData == nil → 2 grid;非 nil → 3 grid
//   - EMG / muscle_ratio dataset > 5000 點時跑 LTTB(threshold 5000)
//   - motion 不 downsample(對齊 ADR-0002)
//   - PhaseTimesEMG(已是 EMG 秒值)attach 為 dashed grey markLine(沿用 CCI 樣式)
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
// 必須與 setComposerGlobalOptions 中的 Height 字串同步;若任一改變,grid 排版
// 比例會錯。
//
// 演進記錄:
//   - 初始 900,grid gap 40 → image #1/#2 layout 太擠
//   - 1100 + gap 80 + reservedTop 110 → 2026-05-27 修一輪;但 user 報 zoom bar
//     仍被切(image #5/#7),所以再 push 到 1200 + reservedBottom 100。
//
// iframe 高度同步在 frontend main.js#generateComposerChart 設成 1200px,
// 移除 iframe 內 scrollbar 並讓 dataZoom slider 完整顯示。
const composerContainerHeight = 1200

// computeGridLayout 把 N 個 grid 在垂直方向均分(以絕對 pixel 計算):
//   - 首 grid top 從 110px(留 title + legend + top-axis label header) —
//     legend ~30 + phase markLine label ~30 + 上下緩衝 → ≥ 110 否則 grid 0 上方
//     phase label 與 legend 重疊(image #1);
//   - 末 grid bottom 留 100px(dataZoom slider);
//   - 每 grid 之間留 80px gap(從 40 加倍) — 讓 grid 0 bottom xAxis name
//     ("Time (s)")、grid 1 top xAxis name + phase label 同時可見不重疊;
//   - gridHeight = (containerHeight - reservedTop - reservedBottom - gap*(N-1)) / N。
//
// emit 純 pixel 字串("110" 而非 "110px");ECharts 對純數字 string 視為 pixel。
// 避免使用 CSS calc(),ECharts 不會解析。
//
// Bug 2 第二輪註記(image #16,2026-05-27):chart-internal 推 slider 往上的 buffer
// (reservedBottom 100 → 150)無效 — user 實測 chart 仍被 iframe 切。真實修法在
// frontend main.js#generateComposerChart 的 iframe.style.height,需大於
// composerContainerHeight 至少 ~150-200px 給 ECharts 額外渲染元件(slider handles、
// dataBackground、邊距)+ webview 對 srcdoc iframe inline height 的默認 reservation
// 留空間。
func computeGridLayout(n int) []gridSpec {
	if n <= 0 {
		return nil
	}
	const reservedTop = 110
	const reservedBottom = 100
	const gapPx = 80
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
//     bottom xAxes 共用 union time min/max(Bug C 軸對齊)
//  4. 為每個 grid 加 series(EMG / muscle_ratio downsample;motion 原樣);
//     每條 series 都 attach phase markLine(Bug B legend 防護)
//  5. 加 custom JS(resize + 鍵盤 R reset zoom + PNG postMessage)
func buildComposerLine(ctx context.Context, in ComposerInput) (*charts.Line, error) {
	// 計算 grid 配置
	n := 2
	hasMuscle := in.MuscleRatioData != nil
	if hasMuscle {
		n = 3
	}
	grids := computeGridLayout(n)

	// EMG / muscle_ratio downsample
	// emgNames 是 EMG 欄位序(對齊 Decision 2),thread 進 addComposerSeries 取代字母序。
	emgTime, emgSeries, emgNames, err := buildEMGSeries(ctx, in.EMGDataset, in.SelectedChannels)
	if err != nil {
		return nil, err
	}

	var muscleTime []float64
	var muscleSeries map[string][]float64
	var muscleNames []string
	if hasMuscle {
		muscleTime, muscleSeries = downsampleSeriesMap(in.MuscleRatioData.Time, in.MuscleRatioData.Series, composerDownsampleThreshold)
		// muscle 欄位序 = MuscleRatioData.Order(對齊 Decision 2),thread 進 addComposerSeries。
		muscleNames = in.MuscleRatioData.Order
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 計算 union time range — 讓三 grid 的 bottom xAxis 共用同一 Min/Max,
	// 解決 motion 收集起始時間早於 EMG 造成的軸不對齊(Bug C 2026-05-27 image #7)。
	// 各 series time 都已換到 EMG-time domain(loadComposerMotion 已透過
	// MotionIndexToEMGTime 轉,muscle 也是 EMG 軸 input),只是 visible range
	// 不同;union 確保 X 刻度在三 grid 對齊。
	minTime, maxTime := computeUnionTimeRange(emgTime, muscleTime, in.MotionData)

	line := charts.NewLine()
	setComposerGlobalOptions(line, in.Subject, grids, n)

	// X axes:每 grid 一 bottom-time(value 軸,秒)+ 一 top-percent(value 軸,0-100)
	// XAxisList[0] 是 echarts 預設;後續 ExtendXAxis 追加
	// 對齊原則:bottom xAxis index = i(0..n-1),top xAxis index = n+i(n..2n-1)
	if err := configureComposerAxes(ctx, line, n, minTime, maxTime); err != nil {
		return nil, err
	}

	// Series:依序加 EMG(grid 0)、muscle_ratio(grid 1, if any)、motion(最後一個 grid)
	// emgNames / muscleNames 提供欄位序(對齊 Decision 2),取代過去的 sortedKeys 字母序。
	if err := addComposerSeries(ctx, line, n, emgTime, emgSeries, emgNames, muscleTime, muscleSeries, muscleNames, in.MotionData, in.PhaseTimesEMG, hasMuscle); err != nil {
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
// 回傳:downsampled time slice + map(channel name → downsampled values)+ 欄位序
// channel 名 slice(nameList)。nameList 是渲染順序的權威來源(對齊 Decision 2 —
// 字母序 → 欄位序),caller 把它 thread 進 addComposerSeries 取代 sortedKeys。
func buildEMGSeries(
	ctx context.Context,
	dataset *models.EMGDataset,
	selectedChannels []string,
) ([]float64, map[string][]float64, []string, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
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
				return nil, nil, nil, ctx.Err()
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
	return downTime, downMap, nameList, nil
}

// downsampleSeriesMap 對共享同一條 time series 的多個 channel 一起做 LTTB:
//   - union/sort/cap 數學委派給 UnionLTTBIndices kernel
//   - 對齊「all series share one X-axis」的 invariant
//   - threshold <= 0 or 長度 <= threshold 直接 passthrough
//
// Composer 的 mismatch 策略是 graceful skip（與 CCI fail-fast 相反）：
// 長度不符的 channel 在 pre-filter 階段靜默過濾，不進入 kernel，也不出現在結果 map。
// 若所有 channel 都不符（matrix 為空），kernel 回傳 nil，函式原封不動回傳原始 time + series。
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

	// 預先過濾長度符合的 channel，graceful skip 不符的（Composer 策略）
	matrix := make([][]float64, 0, len(series))
	for _, vals := range series {
		if len(vals) == len(time) {
			matrix = append(matrix, vals)
		}
	}

	indices := UnionLTTBIndices(time, matrix, threshold)
	if len(indices) == 0 {
		// matrix 為空（全部 channel 不符）→ kernel 回 nil → passthrough 原始輸入
		return time, series
	}

	newTime := make([]float64, len(indices))
	for i, idx := range indices {
		newTime[i] = time[idx]
	}
	newSeries := make(map[string][]float64, len(series))
	for name, vals := range series {
		if len(vals) != len(time) {
			// 長度不符的 channel 不被 downsample,X 軸對齊不上 — 跳過此 series
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
			Top:  g.top,
			Left: "70px",
			// Bug 6 regression(2026-05-27 image #3):60px 不夠寬,bottom xAxis name
			// "Muscle Ratio (s)" / "Motion (s)" 預設 position='end',會擠在 grid 右下被
			// 切掉(image #3 顯示 "Muscle R" 殘斷)。120px 給 axis name 足夠空間 +
			// 一點緩衝,即便 future name 加長也安全。
			Right:  "120px",
			Height: g.height,
		})
	}

	line.SetGlobalOptions(
		charts.WithInitializationOpts(opts.Initialization{
			Width: "100%",
			// 與 composerContainerHeight 同步;frontend iframe.style.height 也用此值
			// 避免 iframe 內部產生 scrollbar 並讓 dataZoom slider 完整顯示(Bug D)。
			Height: "1200px",
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
					// YAxisIndex:false → 區域縮放只控制 x 軸,與 inside/slider(皆 x-only,
					// XAxisIndex 涵蓋 0..2n-1)一致。預設 toolbox dataZoom 控制「全部 y 軸」,
					// 會讓 echarts 為每個 y 軸偷偷建立隱藏的 select 型 dataZoom(不出現在
					// getOption().dataZoom)。標準化視圖的無索引 dispatchAction({type:'dataZoom',
					// start,end}) 會打中那些隱藏 y-model → 三個 y 軸一起塌縮、線消失。設 false
					// 從源頭不生成 y-model,根治此 bug,且時序圖本就只該縮時間軸(x)。
					YAxisIndex: false,
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
	minTime float64,
	maxTime float64,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// XAxis[0] 預設存在 → 用 SetGlobalOptions 設 bottom time axis (grid 0)。
	// 不 SetXAxis(...) — 那只對 category 軸有意義(填 .Data);value 軸由 series data 推。
	//
	// Bug C(2026-05-27 image #7)— 明確設 Min/Max 而非讓 ECharts 自動從 series
	// 推導:三條 bottom xAxis 共用同一範圍才能視覺對齊。空 range fallback 為 nil
	// (省略 → ECharts 自動推)避免 minTime=maxTime=0 把所有 grid 軸壓縮到 0。
	bottomAxisOpts := func(name string, gridIdx int) opts.XAxis {
		ax := opts.XAxis{
			Type:      "value",
			Name:      name,
			GridIndex: gridIdx,
		}
		if minTime < maxTime {
			ax.Min = minTime
			ax.Max = maxTime
		}
		return ax
	}

	line.SetGlobalOptions(
		charts.WithXAxisOpts(bottomAxisOpts("Time (s)", 0), 0),
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
		line.ExtendXAxis(bottomAxisOpts(name, i))
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
	emgNames []string,
	muscleTime []float64,
	muscleSeries map[string][]float64,
	muscleNames []string,
	motion *MotionData,
	phaseTimes map[string]float64,
	hasMuscle bool,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// markLine 對 **每條** series 都 attach — Bug B regression(2026-05-27 image #5):
	//
	// 過去 markLine 只放 grid 首條 series,user 用 legend 隱藏該 series(e.g.
	// IL/GMax)時,markLine 也跟著消失。每條 series 各自帶一份 markLine 視覺上
	// 重疊在同位置,grid 0/1/2 對應 EMG/muscle/motion 各自的 series — 任一條沒被
	// legend 關掉,markLine 仍顯示。phase point 數量少 perf 不痛。
	//
	// 注意:必須用 AddSeries(name, data, opts...) 的第三 variadic 參數傳 seriesOpts,
	// 不可改用 line.AddSeries(...).SetSeriesOptions(opts...) — 後者會把 opts 套用到
	// **MultiSeries 內所有 series**(go-echarts 官方 charts/series.go:722-735 註解明示)
	// 違反此 contract → 全部 series 的 XAxisIndex/YAxisIndex 會被最後一個 series 覆寫。
	markLineOpts := composerPhaseMarkLineOpts(phaseTimes)

	// EMG series → grid 0
	// 依 emgNames(欄位序,對齊 Decision 2)渲染,取代過去的 sortedKeys 字母序。
	// colIdx = series 在欄位序中的位置(0-based) → composerEMGPalette[colIdx % len]。
	for colIdx, name := range emgNames {
		vals := emgSeries[name]
		if err := ctx.Err(); err != nil {
			return err
		}
		lineData, err := buildComposerLineData(ctx, emgTime, vals)
		if err != nil {
			return err
		}
		hex := composerEMGPalette[colIdx%len(composerEMGPalette)]
		seriesOpts := []charts.SeriesOpts{
			charts.WithLineChartOpts(opts.LineChart{
				Smooth:     opts.Bool(false),
				ShowSymbol: opts.Bool(false),
				XAxisIndex: 0,
				YAxisIndex: 0,
			}),
			charts.WithLineStyleOpts(opts.LineStyle{Color: hex, Width: 1.5}),
			// item style 是為 legend marker 上色:go-echarts v2 的 legend 小方塊不會自動
			// 跟 line color 走,需另設 itemStyle.color,line + item 同 hex 才一致。
			charts.WithItemStyleOpts(opts.ItemStyle{Color: hex}),
		}
		seriesOpts = append(seriesOpts, markLineOpts...)
		line.AddSeries(SanitizeChartString(name), lineData, seriesOpts...)
	}

	// muscle_ratio series → grid 1(僅 3-grid)
	// 依 muscleNames(= MuscleRatioData.Order,欄位序,對齊 Decision 2)渲染,取代字母序。
	// 與 motion 一致:Order 內但已被 downsample graceful-skip 的 channel(不在 series map)
	// 用 ok 跳過,但 colIdx 仍取欄位序位置,確保配色不因跳過而位移。
	if hasMuscle {
		for colIdx, name := range muscleNames {
			vals, ok := muscleSeries[name]
			if !ok {
				continue
			}
			lineData, err := buildComposerLineData(ctx, muscleTime, vals)
			if err != nil {
				return err
			}
			hex := composerMuscleRatioPalette[colIdx%len(composerMuscleRatioPalette)]
			seriesOpts := []charts.SeriesOpts{
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:     opts.Bool(false),
					ShowSymbol: opts.Bool(false),
					XAxisIndex: 1,
					YAxisIndex: 1,
				}),
				charts.WithLineStyleOpts(opts.LineStyle{Color: hex, Width: 1.5}),
				charts.WithItemStyleOpts(opts.ItemStyle{Color: hex}),
			}
			seriesOpts = append(seriesOpts, markLineOpts...)
			line.AddSeries(SanitizeChartString(name), lineData, seriesOpts...)
		}
	}

	// motion series → grid n-1
	if motion != nil {
		motionGridIdx := n - 1
		// 依 Order 渲染,確保順序穩定(已是欄位序,Decision 2 不動此處排序)。
		// colIdx = 欄位序位置 → composerMotionPalette[colIdx % len](motion 專用色票,
		// 與 muscle 僅第 4 色不同)。
		for colIdx, name := range motion.Order {
			vals, ok := motion.Series[name]
			if !ok {
				continue
			}
			lineData, err := buildComposerLineData(ctx, motion.Time, vals)
			if err != nil {
				return err
			}
			hex := composerMotionPalette[colIdx%len(composerMotionPalette)]
			seriesOpts := []charts.SeriesOpts{
				charts.WithLineChartOpts(opts.LineChart{
					Smooth:     opts.Bool(false),
					ShowSymbol: opts.Bool(false),
					XAxisIndex: motionGridIdx,
					YAxisIndex: motionGridIdx,
				}),
				charts.WithLineStyleOpts(opts.LineStyle{Color: hex, Width: 1.5}),
				charts.WithItemStyleOpts(opts.ItemStyle{Color: hex}),
			}
			seriesOpts = append(seriesOpts, markLineOpts...)
			line.AddSeries(SanitizeChartString(name), lineData, seriesOpts...)
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

// composerEMGPalette 是 EMG grid(grid 0)的固定色票,index 對應 series 在**欄位序**
// 中的位置(0-based)。明確指定每條 series 顏色(而非依賴 go-echarts 全域 palette)—
// 三 grid 色數不同、series 跨 grid 連續加入,全域 palette 會位移、顏色不穩定。
//
// 色序語意(對齊 Decision 3 spec,EMG 最多 8 channel):
//
//	0 #E8000B 紅   1 #FF6FB5 粉   2 #1B7837 深綠 3 #66BD63 淺綠
//	4 #1F4EAA 深藍 5 #6BAED6 淺藍 6 #FF7F0E 橘   7 #F2C500 黃
var composerEMGPalette = []string{
	"#E8000B", // 0 紅
	"#FF6FB5", // 1 粉
	"#1B7837", // 2 深綠
	"#66BD63", // 3 淺綠
	"#1F4EAA", // 4 深藍
	"#6BAED6", // 5 淺藍
	"#FF7F0E", // 6 橘
	"#F2C500", // 7 黃
}

// composerMuscleRatioPalette 是 muscle ratio grid(grid 1)的固定色票,index 對應
// series 在**欄位序**中的位置(0-based)。
//
// 色序語意(muscle 最多 4 channel):
//
//	0 #E8000B 紅 1 #2CA02C 綠 2 #1F77B4 藍 3 #F2C500 黃
//
// 與 composerMotionPalette 僅第 4 色不同(muscle 黃 / motion 紫)— 兩 grid 不再
// 共用色票,讓 muscle 第 4 條與 motion 第 4 條可視覺區分(user 指定)。
var composerMuscleRatioPalette = []string{
	"#E8000B", // 0 紅
	"#2CA02C", // 1 綠
	"#1F77B4", // 2 藍
	"#F2C500", // 3 黃
}

// composerMotionPalette 是 motion grid(grid n-1)的固定色票,index 對應 series 在
// **欄位序**中的位置(0-based)。
//
// 色序語意(motion 最多 4 channel):
//
//	0 #E8000B 紅 1 #2CA02C 綠 2 #1F77B4 藍 3 #9467BD 紫
var composerMotionPalette = []string{
	"#E8000B", // 0 紅
	"#2CA02C", // 1 綠
	"#1F77B4", // 2 藍
	"#9467BD", // 3 紫
}

// composerPhaseOrder 是 markLine 的 canonical phase 順序,對齊 frontend
// manifestPanel.mjs 的 phaseOrder whitelist。含 motion-index 衍生的 D(下蹲結束)、
// O(展體):caller 已用 MotionIndexToEMGTime 換算成 EMG 秒數放進 map。
var composerPhaseOrder = []string{"P0", "P1", "P2", "S", "C", "D", "T0", "T", "O", "L"}

// composerPhaseMarkLineOpts 把 phase 名 → EMG 秒數 map 轉成 markLine XAxis items。
//
// 輸入 phaseTimes 已是 EMG 時間 domain 秒值,由 caller(Wails handler)統一換算:
// 力板時間欄位(P0/P1/P2/S/C/T0/T/L)走 ForceTimeToEMGTime,motion-index 欄位
// (D/O)走 MotionIndexToEMGTime。chart 套件不持有 conversion 知識(對齊 ComposerInput
// doc),只負責把秒值 anchor 成 markLine —— 前端 checkbox(phaseTimes RPC return)與
// 後端預設 markLine 因此共用同一份 seconds 來源,不會分歧。
//
// 依 composerPhaseOrder 挑出 map 內存在的 phase,不存在的 key skip。
//
// 對齊 CCI markLine 風格:dashed grey、symbol none。
func composerPhaseMarkLineOpts(phaseTimes map[string]float64) []charts.SeriesOpts {
	type pt struct {
		name string
		sec  float64
	}
	pairs := make([]pt, 0, len(composerPhaseOrder))
	for _, name := range composerPhaseOrder {
		if sec, ok := phaseTimes[name]; ok {
			pairs = append(pairs, pt{name: name, sec: sec})
		}
	}

	if len(pairs) == 0 {
		return nil
	}

	// 計算每個 phase 相對於可用 phase 範圍的百分比(min=0%, max=100%),baked 進 Name。
	// 對齊 frontend/src/charts/phaseLines.mjs line 215-220 的 markData.name 格式
	// `P0\n(0.0%)` — 即使 frontend `updatePhaseLines` setOption 失效(e.g. iframe
	// sandbox 跨 frame access broken),backend 預設 markLine 也能顯示與 CCI 一致的
	// `phase\n(pct%)` label,避免 fallback 顯示 ECharts 預設(value 軸時)的 xAxis 數值。
	minSec, maxSec := pairs[0].sec, pairs[0].sec
	for _, p := range pairs {
		if p.sec < minSec {
			minSec = p.sec
		}
		if p.sec > maxSec {
			maxSec = p.sec
		}
	}
	duration := maxSec - minSec

	items := make([]opts.MarkLineNameXAxisItem, 0, len(pairs))
	for _, p := range pairs {
		// xAxis Type 為 value(見 configureComposerAxes doc) → markLine.XAxis 必須是
		// 數值(float64),echarts 才能 anchor 到絕對時間。category 軸時代用 string
		// 表示「Data 陣列的某個 label」,改 value 軸後 string 會被視為非數值丟棄。
		var displayName string
		if duration > 0 {
			pct := (p.sec - minSec) / duration * 100
			displayName = fmt.Sprintf("%s\n(%.1f%%)", p.name, pct)
		} else {
			// 單一 phase 場景(duration=0)— 不顯示百分比,只顯示 name。
			displayName = p.name
		}
		items = append(items, opts.MarkLineNameXAxisItem{
			Name:  displayName,
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
			// Formatter `{b}` 強制顯示 markLine.data[i].name(已 bake 含百分比)。
			// 沒有 Formatter 時 ECharts 在 value-type xAxis 上的 markLine 預設 fallback
			// 為 xAxis 數值(顯示「12.92」而非「P0\n(0.0%)」) — 此為 ECharts 版本相依
			// quirk,明確設 `{b}` 不依賴 default。
			//
			// NOTE — Bug 3(rotate=0):go-echarts opts.Label 不暴露 Rotate field
			// (見 series.go Label struct,僅含 Show/Color/.../Formatter),所以無法
			// 在這裡直接設 rotate:0。改在 addComposerCustomJS post-init 透過 setOption
			// patch 所有 markLine.label.rotate=0。`{b}` formatter + 水平 rotate=0
			// 是兩條獨立 invariant。
			Label: &opts.Label{
				Show:      opts.Bool(true),
				Position:  "insideEndTop",
				Formatter: "{b}",
			},
		}),
		charts.WithMarkLineNameXAxisItemOpts(items...),
	}
}

// addComposerCustomJS 加入 resize handler、鍵盤 R reset zoom,以及 parent→iframe
// postMessage 雙向通訊用於 PNG 下載。
//
// 為何需要 postMessage 而不是直接從 parent 跨 frame 讀 `iframe.contentWindow.echarts`:
// frontend/src/main.js (showCCIResult 區段) 註解明示 iframe sandbox=allow-scripts
// 無 allow-same-origin → opaque origin,parent 跨 frame 存取會 silent fail。P1-12
// codex review 確立此安全策略,Composer 鏡像同樣 sandbox + 用 postMessage 達成原
// 跨 frame 操作目的(取當下 zoom/legend 的 PNG dataURL)。
//
// postToParent / isFromParent / handlePngRequest 三個 comms primitives 現在由
// 共用 assets.IframeCommsJS IIFE 提供(ADR-0003 family / iframe-comms-preamble ADR),
// 掛在 window.__chartComms 上。customJS 直接呼叫 window.__chartComms.isFromParent(e)
// 做 origin 驗證,window.__chartComms.handlePngRequest(myChart, e, 'composer-png-result')
// 取 PNG — 不再在此 inline 重複這些邏輯。
//
// CRITICAL — JS 模板**不可含 `//` line comments**(只能用 `/* ... */` block comments):
// go-echarts `opts/js.go` 的 `newlineTabPat` 在 AddJSFuncStrs 時把 raw string 內所
// 有 `\n`/`\t` strip 成空。如果 JS 模板含 `//` line comments,strip 後 `//` 後面
// 整段(原本下一行的 code、closing braces)會被 single-line comment 吃掉到下個
// `\n`(若無,吃到 `</script>`)— 結果 catch / function 永不閉合 → SyntaxError
// → 整個 inline script 失敗 → ECharts setOption 不執行 → iframe 整片空白
// (image #5 災難)。
//
// `FuncStripCommentsOpts` 看似能解但**不能用**:其 regex `(//.*)\n` 不認識 string
// literal,把 assets.IframeCommsJS 內 `"wails://wails"` 的 `//` 也吃掉 → origin URL 被切斷。
// `/* ... */` block comment 在 newline-strip 後仍正確閉合於 `*/`,是唯一安全選項。
// 任何 future maintainer 想加註解必須用 block comment 形式。
func addComposerCustomJS(line *charts.Line) {
	customJS := assets.PhaseMarkersJS + assets.IframeCommsJS + `
		let myChart = %MY_ECHARTS%;
		if (myChart) {
			/* Bug 2 fix(2026-05-27 image #14):瀏覽器預設 body 8px margin 把 1200px chart 推到
			   viewport y=8..1208,bottom 8px(含 dataZoom slider 一部分)被 iframe 1200 viewport 切掉。
			   重置 body/html margin & padding,確保 chart 完全占滿 iframe viewport。 */
			document.documentElement.style.margin = '0';
			document.documentElement.style.padding = '0';
			document.body.style.margin = '0';
			document.body.style.padding = '0';
			/* Bug 3 fix(2026-05-27 image #1):強制 markLine.label.rotate=0。
			   go-echarts opts.Label 不暴露 Rotate field,只能在 post-init 透過 setOption
			   patch 所有原本 attach markLine 的 series — 每個 grid 都會被覆寫到 rotate:0。
			   一行 setOption,寫法刻意 minify(無 newline)避免 newlineTabPat 風險。 */
			myChart.setOption({series:(myChart.getOption().series||[]).map(function(s){return (s.markLine&&s.markLine.label)?{markLine:{label:{rotate:0}}}:{};})});
			/* Bug F fix(2026-05-27 image #8):dataZoom slider 顯式 position + height。
			   go-echarts opts.DataZoom struct 不暴露 Bottom/Height/Left/Right field,
			   ECharts default auto-position 在 1200px 容器內把 slider 擠成細條且被切。
			   bottom:30 與 reservedBottom=100 對齊(grid 2 bottom 之下留 100px buffer,
			   slider 自 bottom=30 起算、height=30,占用 30-60px 區間,剩 40px 給 axis
			   labels)。dataZoom index 1 對應 slider(index 0 是 inside),patch index-based。
			   注:Bug 2 第二輪試過 bottom=80 推 slider 往上,user 實測無效;真實修法在
			   frontend iframe.style.height 加大(>=1400px 給 ECharts 額外元件 + webview
			   reservation 留空間)。 */
			myChart.setOption({dataZoom:[{},{bottom:30,height:30,left:70,right:120}]});
			document.addEventListener('keydown', function(e) {
				if (e.key === 'r' || e.key === 'R') {
					myChart.dispatchAction({type: 'dataZoom', start: 0, end: 100});
				}
			});
			window.addEventListener('resize', function() {
				myChart.resize();
			});
			/* Parent ↔ iframe postMessage hub:
			   - 'composer-request-png' → 回 'composer-png-result' 帶 dataURL
			   - 'composer-update-phase-markers' → 內部 setOption 更新 markLine.data,
			     不回 ack(fire-and-forget;parent 不等待)
			   origin 驗證:e.source === window.parent 認 ANY origin(sandbox iframe 只能由
			   embedding parent 寄到 — 沒有第三方注入面);allowlist 退路保留 production 路徑。
			   2026-05-27 Bug 1/3 修補:wails dev 用 http://localhost:34115,不在 allowlist;
			   parent 端跨 frame 直接 setOption 在 sandbox=allow-scripts 下被 silent 阻擋 →
			   phase 勾選改用同一條 postMessage 路。 */
			window.addEventListener('message', function(e) {
				if (!window.__chartComms.isFromParent(e)) {
					return;
				}
				if (!e.data || typeof e.data !== 'object') {
					return;
				}
				if (e.data.type === 'composer-request-png') {
					window.__chartComms.handlePngRequest(myChart, e, 'composer-png-result');
					return;
				}
				if (e.data.type === 'composer-standardize-zoom') {
					/* ADR-0013 D4/D5 標準化視圖:parent 寄 {payload: {startSec, endSec}}
					   (勾選分期 min/max 秒 ± 5% buffer)。把秒值換算成 dataZoom 百分比。
					   ⚠️ 必用百分比 start/end(0-100)而非 startValue/endValue:dataZoom 的
					   XAxisIndex 涵蓋全部 2n 軸(bottom 秒軸 + top 百分比軸),絕對秒值會被
					   套到 0-100 百分比軸 → 錯位。百分比則每軸各自依自身範圍解讀,跨軸一致。
					   axisMin/axisMax 取 bottom xAxis[0](configureComposerAxes 已設 union 秒範圍)。 */
					try {
						const payload = e.data.payload || {};
						const startSec = payload.startSec;
						const endSec = payload.endSec;
						const xAxes = myChart.getOption().xAxis || [];
						if (xAxes.length > 0 && typeof xAxes[0].min === 'number' && typeof xAxes[0].max === 'number') {
							const axisMin = xAxes[0].min;
							const axisMax = xAxes[0].max;
							const span = axisMax - axisMin;
							if (span > 0) {
								let startPct = (startSec - axisMin) / span * 100;
								let endPct = (endSec - axisMin) / span * 100;
								startPct = Math.max(0, Math.min(100, startPct));
								endPct = Math.max(0, Math.min(100, endPct));
								myChart.dispatchAction({type: 'dataZoom', start: startPct, end: endPct});
							}
						}
					} catch (err) {
						try { console.error('composer-standardize-zoom 失敗:', err); } catch (e2) {}
					}
					return;
				}
				if (e.data.type === 'composer-update-phase-markers') {
					/* ADR-0003 §5 symmetric payload:parent 寄
					   {payload: {checkedPhases: [{name, time, pct}]}}。
					   iframe 自己組 markData(value-axis 直接用 numeric time)
					   並逐 series patch markLine.data — 對每條原本帶 markLine 的
					   series 套用同一份 markData(對齊 Bug B multi-attach 設計:
					   legend 隱藏某條 series 時其他 series 仍顯示 markLine)。
					   PhaseMarkersJS IIFE 已在前段載入,但 Composer 端不需要
					   recalcPercents(parent 已算好 pct)。 */
					try {
						const payload = e.data.payload || {};
						const checkedPhases = Array.isArray(payload.checkedPhases) ? payload.checkedPhases : [];
						const markData = checkedPhases.map(function(p) {
							const pct = (typeof p.pct === 'number') ? p.pct : 0;
							const pctRounded = Math.round(pct * 10) / 10;
							return { xAxis: p.time, name: p.name + '\n(' + pctRounded + '%)' };
						});
						const currentSeries = myChart.getOption().series || [];
						const seriesPatch = currentSeries.map(function(s) {
							if (s && s.markLine) {
								return { markLine: { silent: true, symbol: ['none','none'], data: markData } };
							}
							return {};
						});
						myChart.setOption({series: seriesPatch});
					} catch (err) {
						/* silent fail — phase 更新失敗不該擋 user 操作,debug 走 console */
						try { console.error('composer-update-phase-markers 失敗:', err); } catch (e2) {}
					}
				}
			});
		}
	`
	line.AddJSFuncStrs(opts.FuncOpts(customJS))
}

// computeUnionTimeRange 取 EMG / muscle / motion 三組 time slice 的 union min/max。
//
// 為何要 union 而非各軸獨立:三個 bottom xAxis 都是 value 軸,預設從各自 series
// data 推 min/max — 不同 dataset 起始時間或 sample 範圍不同會造成軸範圍不一致,
// 視覺上 phase markLine 雖然 numeric x 對齊,grid 與 grid 之間時間刻度錯位
// (Bug C 2026-05-27 image #7)。
//
// 任一 series time 為空 → 該源跳過(degraded mode 仍可回 union)。三組全空時
// 回 (0, 0) — caller(configureComposerAxes)需檢查 min<max 才套用,避免把所有
// 軸壓縮到單一 point。
func computeUnionTimeRange(emgTime, muscleTime []float64, motion *MotionData) (float64, float64) {
	minTime := math.Inf(1)
	maxTime := math.Inf(-1)
	consider := func(times []float64) {
		for _, t := range times {
			if math.IsNaN(t) || math.IsInf(t, 0) {
				continue
			}
			if t < minTime {
				minTime = t
			}
			if t > maxTime {
				maxTime = t
			}
		}
	}
	consider(emgTime)
	consider(muscleTime)
	if motion != nil {
		consider(motion.Time)
	}
	if math.IsInf(minTime, 0) || math.IsInf(maxTime, 0) {
		return 0, 0
	}
	return minTime, maxTime
}
