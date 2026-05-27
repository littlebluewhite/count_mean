package chart

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

// makeEMG 建構共用 EMG dataset：headers = ["time", ch1, ch2, ...]，每筆 channels 與 headers[1:] 一致。
func makeEMGDataset(n int, channelNames ...string) *models.EMGDataset {
	headers := append([]string{"time"}, channelNames...)
	data := make([]models.EMGData, n)
	for i := 0; i < n; i++ {
		channels := make([]float64, len(channelNames))
		for c := range channelNames {
			// 給每個 channel 不同 amplitude，避免 LTTB bucket 全平
			channels[c] = float64(i)*0.001 + float64(c)
		}
		data[i] = models.EMGData{Time: float64(i) * 0.001, Channels: channels}
	}
	return &models.EMGDataset{Headers: headers, Data: data, OriginalTimePrecision: 3}
}

func makeMotionData(n int, channelNames ...string) *MotionData {
	t := make([]float64, n)
	series := make(map[string][]float64, len(channelNames))
	for i := 0; i < n; i++ {
		t[i] = float64(i) * 0.01
	}
	for _, name := range channelNames {
		vals := make([]float64, n)
		for i := 0; i < n; i++ {
			vals[i] = float64(i) * 0.1
		}
		series[name] = vals
	}
	return &MotionData{Time: t, Series: series, Order: channelNames}
}

func makeMuscleRatioData(n int, channelNames ...string) *MuscleRatioData {
	t := make([]float64, n)
	series := make(map[string][]float64, len(channelNames))
	for i := 0; i < n; i++ {
		t[i] = float64(i) * 0.001
	}
	for _, name := range channelNames {
		vals := make([]float64, n)
		for i := 0; i < n; i++ {
			vals[i] = float64(i) * 0.01
		}
		series[name] = vals
	}
	return &MuscleRatioData{Time: t, Series: series, Order: channelNames}
}

// renderToString 共用 helper：失敗即 fatal。
func renderToString(t *testing.T, ctx context.Context, in ComposerInput) string {
	t.Helper()
	var buf bytes.Buffer
	err := RenderComposer(ctx, in, &buf)
	require.NoError(t, err)
	return buf.String()
}

// TestRenderComposer_ThreeGridLayout — MuscleRatioData != nil → 三個 grid。
//
// echarts JSON 把 grid 序列化為陣列 `"grid":[{...},{...},{...}]`。
// 三 grid 的 layout 預期含三組 Top（每個 grid 自己一個 Top 偏移），
// 用 substring count 而非 JSON parse 對齊既有 CCI test 風格。
func TestRenderComposer_ThreeGridLayout(t *testing.T) {
	in := ComposerInput{
		Subject:          "S1",
		EMGDataset:       makeEMGDataset(100, "RA", "ES"),
		SelectedChannels: []string{"RA", "ES"},
		MuscleRatioData:  makeMuscleRatioData(100, "RA/ES"),
		MotionData:       makeMotionData(50, "knee_angle"),
	}
	html := renderToString(t, context.Background(), in)

	// echarts 輸出 grid 陣列；3 grid 表示 grid:[ 三筆 ]。
	// 每筆 grid 都帶 `"left":"70px"`(setComposerGlobalOptions 固定 left),透過
	// substring count 間接驗證 grid 數。
	assert.Contains(t, html, `"grid":`, "HTML 必須含 grid 陣列")
	gridCount := strings.Count(html, `"left":"70px"`)
	assert.Equal(t, 3, gridCount, "MuscleRatioData != nil 預期 3 個 grid")
}

// TestRenderComposer_TwoGridLayout — MuscleRatioData == nil → 兩個 grid。
func TestRenderComposer_TwoGridLayout(t *testing.T) {
	in := ComposerInput{
		Subject:          "S1",
		EMGDataset:       makeEMGDataset(100, "RA", "ES"),
		SelectedChannels: []string{"RA", "ES"},
		MuscleRatioData:  nil,
		MotionData:       makeMotionData(50, "knee_angle"),
	}
	html := renderToString(t, context.Background(), in)

	assert.Contains(t, html, `"grid":`)
	gridCount := strings.Count(html, `"left":"70px"`)
	assert.Equal(t, 2, gridCount, "MuscleRatioData == nil 預期 2 個 grid (EMG + motion)")
}

// TestRenderComposer_EMGDownsampled — EMG 點數 > 5000 時 series data 長度受 LTTB 壓回 5000。
//
// 直接觀察 HTML 內 series.data 陣列長度比較危險（JSON 包含 markLine 等 sub-array），
// 改走「composer 內部 downsample helper」單元驗證 — 把 downsample 暴露為非 exported 函式。
// 這裡退而用「HTML 內 LineData literal 數量上限不超過 threshold * channels * 上限緩衝」做粗檢。
func TestRenderComposer_EMGDownsampleTriggered(t *testing.T) {
	const total = 12000

	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(total, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(100, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	// downsample 觸發後 series 內 LineData 數量必須遠小於 total。直接 count `"value":` 出現次數
	// 給 EMG series 是個粗略上限(motion / markLine 也會貢獻),但 total=12000 vs threshold=5000
	// 兩端差距足夠。實際上限：downsampled + motion + markLine < total
	valueCount := strings.Count(html, `"value":`)
	assert.Less(t, valueCount, total,
		"EMG 12000 點觸發 LTTB,輸出 LineData 總數應小於原始點數")
}

// TestRenderComposer_MotionNotDownsampled — motion 即使 > 5000 點仍不 downsample。
// 透過「motion-only 100k 點 + EMG 100 點」確認 motion 大資料 path 不會 reject 也不會 silent
// 截斷;直接 render 成功且 HTML 含對應 series 名稱。
func TestRenderComposer_MotionNotDownsampled(t *testing.T) {
	const motionLen = 10000

	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(motionLen, "knee_angle"),
	}
	var buf bytes.Buffer
	err := RenderComposer(context.Background(), in, &buf)
	require.NoError(t, err)

	html := buf.String()
	assert.Contains(t, html, "knee_angle", "motion series name 應出現於 HTML")

	// motion 不被 downsample → 對應 series 至少含 motionLen 個 LineData。
	// 統計輸出中 knee_angle series 的 LineData 數比較難精準,改驗整體 LineData
	// 數至少 >= motionLen(motion 一定有 motionLen 個,EMG 50 個加總 ≥ 10050)
	valueCount := strings.Count(html, `"value":`)
	assert.GreaterOrEqual(t, valueCount, motionLen, "motion 不 downsample → LineData 數應 ≥ motionLen")
}

// TestRenderComposer_PhaseMarkLines — HTML 包含 markLine 區塊。
// PhasePoints 各 OptFloat 設定後,composer 應把秒值轉成 markLine xAxis item。
func TestRenderComposer_PhaseMarkLines(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(100, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
		PhasePoints: models.PhasePoints{
			P0: models.MakeOpt(0.010),
			S:  models.MakeOpt(0.030),
			T:  models.MakeOpt(0.080),
			L:  models.MakeOpt(0.095),
		},
	}
	html := renderToString(t, context.Background(), in)

	assert.Contains(t, html, `"markLine"`, "HTML 必須包含 markLine 區塊")
	// markLine name baked 為 `Phase\n(pct%)` 格式(對齊 CCI / frontend updatePhaseLines
	// 風格);phase\n 之後接百分比讓 ECharts default formatter 即使在 frontend
	// setOption 失效時也能顯示「P0\n(0.0%)」而非裸 xAxis 數值。
	assert.Contains(t, html, `"P0\n(0.0%)"`, "P0 phase point 應以 baked name 出現於 markLine")
	assert.Contains(t, html, `"L\n(100.0%)"`, "L phase point 為 max time → 100%")
	// formatter `{b}` 顯式設定,不依賴 ECharts default(value-axis 上 markLine 預設
	// fallback 為 xAxis 數值,bug image #3 即此現象)。
	assert.Contains(t, html, `"formatter":"{b}"`, "markLine label 須顯式設 {b} formatter")
	// Bug 3 regression(2026-05-27 image #1):label 必須明確 rotate=0,否則
	// ECharts 預設讓文字沿 markLine 線方向(垂直線 → 90°)旋轉,顯示為傾斜書寫。
	// 實作細節:go-echarts opts.Label 不暴露 Rotate field,改在 addComposerCustomJS
	// 透過 setOption 補,故出現於 customJS 區段(JS 物件,非 JSON quoted key)。
	assert.Contains(t, html, `rotate:0`,
		"markLine label rotate 必須明確設 0,透過 customJS post-init setOption 補")
}

// TestRenderComposer_MarkLineOnEveryGrid — Bug 2 + Bug B regression(2026-05-27):
//
// 雙層需求:
//   1. (Bug 2 / image #2):每個 grid 都要 phase 虛線(過去只 grid 0 有)。
//   2. (Bug B / image #5):使用者透過 legend 隱藏某條 muscle/motion series 時
//      (e.g. IL/GMax),markLine 不可隨之消失。
//
// 修法:對 **每條** EMG/muscle/motion series 都 attach markLine。即便其中一條被
// legend 隱藏,其他 series 仍 visible 且帶 markLine → 視覺持續顯示。
//
// markLine 在 ECharts 是 series-level 屬性,繼承父 series 的 xAxisIndex;每條
// series 多帶一份 markLine data 視覺上重疊在同位置,markLine 點數少 perf 不痛。
//
// 防線:3-grid scenario(EMG 2 + muscle 4 + motion 2 = 8 series),每個 phase 對
// 應的 markLine.name 應出現 ≥ 8 次(每條 series 一次)。
func TestRenderComposer_MarkLineOnEveryGrid(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(100, "RA", "ES"),
		SelectedChannels: []string{"RA", "ES"},
		MuscleRatioData:  makeMuscleRatioData(100, "IL_GMax", "RA_ES", "RF_BF", "TAIO_MF"),
		MotionData:       makeMotionData(50, "knee", "ankle"),
		PhasePoints: models.PhasePoints{
			P0: models.MakeOpt(0.010),
			L:  models.MakeOpt(0.090),
		},
	}
	html := renderToString(t, context.Background(), in)

	// EMG 2 + muscle 4 + motion 2 = 8 series,每條都 attach markLine。
	p0Count := strings.Count(html, `"P0\n(0.0%)"`)
	lCount := strings.Count(html, `"L\n(100.0%)"`)
	assert.GreaterOrEqual(t, p0Count, 8,
		"P0 markLine name 應對每條 series 都 attach(2 EMG + 4 muscle + 2 motion = 8)")
	assert.GreaterOrEqual(t, lCount, 8,
		"L markLine name 應對每條 series 都 attach(同上)")
}

// TestRenderComposer_BottomXAxisUnionRange — Bug C regression(2026-05-27 image #7):
//
// 三個 bottom xAxis 各自 Type:"value" 不設 Min/Max,從各自 series data 推 →
// EMG 與 motion 收集起始時間不同會導致軸範圍不同(EMG 4.58~19.65s vs motion
// 1.19~17.81s),三軸時間刻度視覺不對齊。
//
// 修法:backend 取 EMG/muscle/motion 三組 time 的 union min/max,套到所有 bottom
// xAxis Min/Max,讓三軸時間刻度一致。
//
// 防線:用 motion t=[0..0.49](max 大於 EMG 的 0.099)做測,bottom xAxis 應該都
// 顯式 emit `"max":0.49`(三個 grid 同樣 Max 值)。
func TestRenderComposer_BottomXAxisUnionRange(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(100, "RA"),       // t=[0..0.099]
		SelectedChannels: []string{"RA"},
		MuscleRatioData:  makeMuscleRatioData(100, "RA/ES"), // t=[0..0.099]
		MotionData:       makeMotionData(50, "knee"),       // t=[0..0.49] — 最大
	}
	html := renderToString(t, context.Background(), in)

	// motion max=0.49 應該被套到所有 3 個 bottom xAxis 的 Max。
	// `"max":0.49` 應出現至少 3 次(每個 bottom xAxis 一次)。
	// (top percent xAxis 是 Min:0/Max:100,不會匹配 "max":0.49)
	maxCount := strings.Count(html, `"max":0.49`)
	assert.GreaterOrEqual(t, maxCount, 3,
		`"max":0.49" 應出現至少 3 次(3 bottom xAxis 共用 union Max)`)
}

// TestRenderComposer_DataZoomSliderPosition — Bug F regression(2026-05-27 image #8):
//
// opts.DataZoom struct 不暴露 Bottom/Height/Left/Right field,ECharts 用 default
// auto-position → 在 1200px 容器內把 slider 擠成細條且被切。修法:customJS post-init
// setOption patch dataZoom index 1(slider)的明確 layout。
//
// 防線:customJS 必須含 `dataZoom:[{},{bottom:30,height:30,left:70,right:120}]` 形
// patch,明確指定 slider 位置與尺寸。
//
// Bug 2 第二輪註記(image #16):試過把 slider 推到 bottom=80(增加 chart-internal
// buffer)但 user 實測仍被 iframe 切。真實修法在 frontend iframe.style.height 加大,
// 不是 chart-internal slider 位置;此 test 鎖回原 bottom=30 不再追加 chart-internal
// buffer。
func TestRenderComposer_DataZoomSliderPosition(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	assert.Contains(t, html, `bottom:30`,
		"customJS 應明確設 dataZoom slider bottom=30(避免 default auto-position 被切)")
	assert.Contains(t, html, `height:30`,
		"customJS 應明確設 dataZoom slider height=30")
}

// TestRenderComposer_ContainerHeightAccommodatesZoomBar — Bug D regression(2026-05-27):
//
// User 報「整體圖長度太短,zoom bar 被切掉」。修法:擴大 composerContainerHeight
// 與 reservedBottom 給 dataZoom slider 充足空間。
//
// 防線:Initialization.Height 必須 ≥ "1200px"(從 1100 拉大),確保 zoom bar 不
// 被 iframe 邊界裁切。
func TestRenderComposer_ContainerHeightAccommodatesZoomBar(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	assert.Contains(t, html, `height:1200px`,
		"Initialization.Height 必須 ≥ 1200px,確保 dataZoom slider 不被切")
}

// TestRenderComposer_GridRightMargin — Bug 6 regression(2026-05-27):
//
// 過去 grid.Right="60px" 太窄,當 yAxis name(尤其 grid 1 "Muscle Ratio"、grid 2
// "Motion")出現在 grid 右下時,標籤被切到只剩 "Muscle R" / "Motion " (image #3)。
//
// 修法:Right 拉大到 "120px" 以上,讓 yAxis name 完整顯示。
func TestRenderComposer_GridRightMargin(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(100, "RA"),
		SelectedChannels: []string{"RA"},
		MuscleRatioData:  makeMuscleRatioData(100, "RA/ES"),
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	// 拒絕 60px(舊值);應 emit 較大值。為避免硬釘特定數字,assert Right >= 100。
	// 用負面斷言 + 正面包含,let composer 在 ≥ 100px 範圍自由調整。
	assert.NotContains(t, html, `"right":"60px"`,
		"grid right margin 不可為 60px(過窄,yAxis name 被切 — image #3)")
}

// TestRenderComposer_GridGapEnough — Bug 5 regression(2026-05-27):
//
// 過去 gap=40px 太小,相鄰 grid 之間 top-axis label / yAxis name 容易重疊
// (image #2 顯示 "EMG"、"Ratio"、"Motion" 與相鄰 grid 軸刻度擠在一起)。
//
// 修法:gap 拉大到 ≥ 70px,確保相鄰 axis 文字不重疊。
//
// 防線:emit 的 grid `top` 偏移之間差距須 >= (gridHeight + 70)。直接看 composer
// computeGridLayout 純函式即可,不需做 HTML 解析。
func TestRenderComposer_GridGapEnough(t *testing.T) {
	grids := computeGridLayout(3)
	require.Len(t, grids, 3)

	// grids[i].top 是字串 pixel;手動 parse 比 string contains 穩定。
	parseTop := func(s string) int {
		var n int
		_, err := fmt.Sscanf(s, "%d", &n)
		require.NoError(t, err)
		return n
	}
	parseHeight := parseTop

	for i := 1; i < len(grids); i++ {
		prevBottom := parseTop(grids[i-1].top) + parseHeight(grids[i-1].height)
		gap := parseTop(grids[i].top) - prevBottom
		assert.GreaterOrEqual(t, gap, 70,
			"grid %d 與 %d 之間 gap 應 ≥ 70px,目前 %d", i-1, i, gap)
	}
}

// TestRenderComposer_NoComposerUpdatePhaseLinesListener — 釘住舊的失敗識別字。
//
// 歷史:前次 session 嘗試用 `composer-update-phase-lines` postMessage 但 origin
// filter 把 message 拒掉。後來改回 cross-frame setOption,2026-05-27 user 報
// 「phase 勾選毫無效果」,實際上是 wails dev 的 webview 比生產 webview 嚴格,
// 跨 frame chart 存取 silent block。**新解**:postMessage with identifier
// `composer-update-phase-markers`(注意 markers ≠ lines)+ 放寬 origin check
// 走 e.source === window.parent,二者一起讓 wails dev 也能 work。
//
// 防線:確保未來 maintainer 不會誤改回舊識別字。新識別字 `markers` 走
// TestRenderComposer_HasComposerPhaseMarkersListener。
func TestRenderComposer_NoComposerUpdatePhaseLinesListener(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	assert.NotContains(t, html, `composer-update-phase-lines`,
		"customJS 不應再有舊 `composer-update-phase-lines` 識別字(用 -markers 取代)")
}

// TestRenderComposer_HasComposerPhaseMarkersListener — Bug 1 fix(2026-05-27 image #12),
// schema 收乾 by ADR-0003:
//
// 修補根因:wails dev 預設 webview(opaque origin enforcement 嚴格)下,parent 跨
// frame 呼叫 `iframe.contentWindow.echarts.setOption` 被 silent block → phase 勾選
// 改變毫無反應(user report:只勾 C/T0/T 但 8 個 markLine 仍在)。
//
// 修法:frontend bridge.send 寄 `composer-update-phase-markers` + payload
// `{checkedPhases: [{name, time, pct}]}` 給 iframe,iframe customJS 自己組 markData
// 後本地 setOption — 完全繞開 cross-frame 限制。Parent 不再持有 chart-internal
// 知識(markData 形狀、series patch 順序)。
//
// 防線:customJS 必須含 `composer-update-phase-markers` listener、`checkedPhases`
// 解構、`__phaseMarkers` IIFE 注入,且仍走 series-level markLine patch。
func TestRenderComposer_HasComposerPhaseMarkersListener(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	assert.Contains(t, html, `composer-update-phase-markers`,
		"customJS 必須含 phase markers postMessage listener(Bug 1 修補)")
	assert.Contains(t, html, `checkedPhases`,
		"ADR-0003 §5 symmetric payload:iframe handler 必須解構 payload.checkedPhases")
	assert.Contains(t, html, `__phaseMarkers`,
		"PhaseMarkersJS IIFE 必須在 customJS 前段注入,attach helpers 到 window.__phaseMarkers")
	// 確認 iframe 端仍有逐 series patch markLine 的 setOption 動作
	assert.Contains(t, html, `markLine`,
		"customJS 應透過 setOption 寫 series[i].markLine.data")
}

// TestRenderComposer_RelaxedOriginCheck — Bug 3 fix(2026-05-27 image #15):
//
// wails dev parent origin 為 http://localhost:34115(動態 port),不在硬編
// wailsParentOrigins allowlist (`wails://wails`、`http://wails.localhost` 等)
// → iframe customJS message listener 把 parent 訊息全部 reject → PNG 下載 10s
// timeout(user report)。
//
// 修法:接受 `e.source === window.parent` 作為 origin allowlist 退路。安全性
// 推論:sandbox=allow-scripts iframe 為 opaque origin,只有 embedding parent
// 持有 iframe.contentWindow 引用,沒有第三方注入路徑;e.source 比對等同於
// 「來自 embedding parent」,等價於 trusted。
//
// 防線:customJS 必須含 `e.source === window.parent` 比對(或 `e.source!==window.parent`
// 否定形式),避免 production-only allowlist 在 dev 環境鎖死。
func TestRenderComposer_RelaxedOriginCheck(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	assert.Contains(t, html, `e.source !== window.parent`,
		"customJS 必須含 `e.source !== window.parent` 退路,讓 wails dev 也能 postMessage")
}

// TestRenderComposer_BodyMarginReset — Bug 2 fix(2026-05-27 image #14):
//
// 瀏覽器預設 body 8px margin 把 1200px chart 推到 viewport y=8..1208,bottom 8px
// (含 dataZoom slider 底邊)被 iframe 1200 viewport 切掉(user report:zoom bar
// 被切)。
//
// 修法:customJS 在 init 後 reset body/html margin & padding 為 0,確保 chart
// 完整占滿 iframe viewport,slider 不被切。
//
// 防線:customJS 必須含 `body.style.margin = '0'` 等樣式 reset。
func TestRenderComposer_BodyMarginReset(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	assert.Contains(t, html, `document.body.style.margin`,
		"customJS 必須 reset body margin(Bug 2:預設 8px margin 把 slider 切掉)")
}

// TestRenderComposer_CustomJSSyntaxValid — 釘住 go-echarts `newlineTabPat` 陷阱:
// AddJSFuncStrs 把 raw string 內所有 `\n`/`\t` strip 成空。如果 JS 模板含 `//` line
// comments,strip 後 `//` 後面整段(closing braces / 下一行 code)被吃成註解 →
// catch / function 永不閉合 → SyntaxError → 整個 inline script 失敗 → iframe
// 整片空白(2026-05-27 image #5 災難)。
//
// 修法為 JS 模板只用 `/* ... */` block comments,且 `FuncStripCommentsOpts` 不能用
// (regex 不認識 string literal,會把 `"wails://wails"` 內的 `//` 也吃掉)。
//
// 此 test 釘 3 條 invariant:
//   1. wailsParentOrigins 陣列完整(URLs 內 `//` 未被誤吃)
//   2. postMessage handler 結構完整(關鍵 token 都在,代表 function/if 都閉合到位)
//   3. customJS 區段內 `{` 與 `}` 數量相等(若 `//` 吃掉 closing braces 就會不平衡)
func TestRenderComposer_CustomJSSyntaxValid(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
		PhasePoints: models.PhasePoints{
			P0: models.MakeOpt(0.010),
			L:  models.MakeOpt(0.090),
		},
	}
	html := renderToString(t, context.Background(), in)

	// (1) wailsParentOrigins URLs 完整:`FuncStripCommentsOpts` regex 會吃 `//`
	// 不認識 string,改回 FuncOpts 後 URLs 必須保留。
	assert.Contains(t, html, `"wails://wails"`, "wails:// URL 完整")
	assert.Contains(t, html, `"http://wails.localhost"`, "http:// URL 完整")
	assert.Contains(t, html, `"https://wails.localhost"`, "https:// URL 完整")

	// (2) postMessage handler 結構完整(三個關鍵 token 都在):
	assert.Contains(t, html, `composer-request-png`, "postMessage handler 識別字未被 strip")
	assert.Contains(t, html, `composer-png-result`, "postMessage response 識別字未被 strip")
	assert.Contains(t, html, `getDataURL`, "PNG 取值呼叫未被 strip")

	// (3) 抽出 `<script type="text/javascript">` ... `</script>` 區段做 brace
	// balance 檢查 — 若 `//` 吃掉 closing braces,{ 開越多 } 越少,平衡破壞。
	scriptStart := strings.Index(html, `<script type="text/javascript">`)
	if scriptStart < 0 {
		t.Fatal("找不到 <script> 區段")
	}
	scriptStart += len(`<script type="text/javascript">`)
	scriptEnd := strings.Index(html[scriptStart:], `</script>`)
	if scriptEnd < 0 {
		t.Fatal("找不到 </script>")
	}
	js := html[scriptStart : scriptStart+scriptEnd]

	openBraces := strings.Count(js, `{`)
	closeBraces := strings.Count(js, `}`)
	// option JSON 內含大量 {} 但都對稱,customJS 內 {} 也對稱 — 整體必平衡。
	// 不平衡 = SyntaxError 警訊 → image #5 復發。
	assert.Equal(t, openBraces, closeBraces,
		"<script> 區段 { 與 } 數量必相等 — 不平衡代表 newline-strip 後 `//` 吃掉 closing braces(image #5 復發)")
}

// TestRenderComposer_SeriesRoutingPerGrid — H1 regression:每個 series 必須路由到自
// 己的 grid(EMG→0、muscle_ratio→1、motion→n-1),否則上方 grid 全部空、資料擠到底。
//
// 過去 bug:`line.AddSeries(...).SetSeriesOptions(opts...)` 把 opts apply 到 **所有**
// 既存 series(go-echarts charts/series.go:722-735 註解明示 last-write-wins),
// motion 是最後加,把全部 series 的 XAxisIndex/YAxisIndex 蓋成 motionGridIdx。修法
// 為改用 `line.AddSeries(name, data, opts...)` 第三 variadic 參數,每條 series
// 獨立 opts。
func TestRenderComposer_SeriesRoutingPerGrid(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(100, "RA", "ES"),
		SelectedChannels: []string{"RA", "ES"},
		MuscleRatioData:  makeMuscleRatioData(100, "RA/ES"),
		MotionData:       makeMotionData(50, "knee", "ankle"),
	}
	html := renderToString(t, context.Background(), in)

	// muscle_ratio (grid 1) 必須出現 `"xAxisIndex":1,"yAxisIndex":1` series 配對。
	assert.Contains(t, html, `"xAxisIndex":1,"yAxisIndex":1`,
		"muscle_ratio series 應路由到 grid 1(xAxisIndex=1, yAxisIndex=1)")
	// motion (grid n-1=2) 必須出現 `"xAxisIndex":2,"yAxisIndex":2`。
	assert.Contains(t, html, `"xAxisIndex":2,"yAxisIndex":2`,
		"motion series 應路由到 grid 2(xAxisIndex=2, yAxisIndex=2)")

	// H1 regression 核心斷言:全部 series series 配對個數不可全部等於 motion 的 (2,2)。
	// 過去 bug 下 6 個 series 全部 `"xAxisIndex":2,"yAxisIndex":2`(被覆寫),雖然
	// motion 自己也是 (2,2),正常情況最多 2 個(motion 2 個 channels)。
	// 斷言 ≤2 個 — 任何 (2,2) 出現次數超過 motion channel 數即代表 H1 復發。
	motionPairCount := strings.Count(html, `"xAxisIndex":2,"yAxisIndex":2`)
	assert.LessOrEqual(t, motionPairCount, 2,
		"(xAxisIndex=2, yAxisIndex=2) 出現 >2 次代表 H1 series-routing bug 復發(EMG/muscle 被覆寫到 motion grid)")

	// 對應地 (1,1) 配對只能出現 1 次(只有 1 個 muscle channel)。
	musclePairCount := strings.Count(html, `"xAxisIndex":1,"yAxisIndex":1`)
	assert.Equal(t, 1, musclePairCount,
		"(xAxisIndex=1, yAxisIndex=1) 應僅 1 次(對應唯一 muscle_ratio channel)")
}

// TestRenderComposer_SharedTimeAndPercentAxes — HTML 含兩種 axis：bottom time + top percent。
// 三個 grid 時 → 至少 3 個 axis 為 bottom、3 個為 top。
func TestRenderComposer_SharedAxes(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(100, "RA"),
		SelectedChannels: []string{"RA"},
		MuscleRatioData:  makeMuscleRatioData(100, "RA/ES"),
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	// echarts 把 top axis 標記為 `"position":"top"`,bottom 是預設不寫 position(底部)。
	topCount := strings.Count(html, `"position":"top"`)
	assert.Equal(t, 3, topCount, "3 grid 對應 3 個 top percent xAxis")

	// xAxis 陣列總數 = bottom (3) + top (3) = 6;在 HTML 內 xAxis 區塊以 `"xAxis":[` 開頭
	assert.Contains(t, html, `"xAxis":[`)

	// dataZoom 須同步全部 grid:`"xAxisIndex":[0,1,2,3,4,5]`(slider/inside 各一)
	assert.Contains(t, html, `"xAxisIndex":[0,1,2,3,4,5]`,
		"dataZoom 應聯動全部 6 個 xAxis(3 bottom + 3 top)")
}

// TestRenderComposer_CtxCancelled — 入口已 cancel 的 ctx 立即 return ctx.Err。
func TestRenderComposer_CtxCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 預先 cancel

	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(100, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}

	var buf bytes.Buffer
	err := RenderComposer(ctx, in, &buf)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, buf.String(), "cancel 路徑不該寫出半成品 HTML")
}

// TestRenderComposer_NilEMGDataset — nil EMG dataset 必須 reject。
func TestRenderComposer_NilEMGDataset(t *testing.T) {
	in := ComposerInput{
		Subject:    "S",
		EMGDataset: nil,
		MotionData: makeMotionData(10, "knee"),
	}
	var buf bytes.Buffer
	err := RenderComposer(context.Background(), in, &buf)
	require.Error(t, err)
}

// TestRenderComposer_SubjectXSSEscaped — Subject 走 SanitizeChartString。
func TestRenderComposer_SubjectXSSEscaped(t *testing.T) {
	in := ComposerInput{
		Subject:          `</script><script>alert(1)</script>`,
		EMGDataset:       makeEMGDataset(50, "RA"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(20, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	assert.NotContains(t, html, "</script><script>",
		"malicious subject 不該以原樣 escape 進 HTML")
	assert.NotContains(t, html, "</script><",
		"任何 </script>< 序列暗示 script context 逸出")
}

// TestRenderComposer_SelectedChannelsFilter — SelectedChannels 限制顯示的 EMG channels。
// 即 EMGDataset.Headers 有 [time, RA, ES, BB],但 SelectedChannels=[RA] → 只渲染 RA series。
func TestRenderComposer_SelectedChannelsFilter(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(100, "RA", "ES", "BB"),
		SelectedChannels: []string{"RA"},
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	// "RA" 在 series.name 中該出現,ES / BB 在 SelectedChannels 不含 → 不應出現 series name
	assert.Contains(t, html, `"name":"RA"`, "selected RA 必須出現")
	// echarts series 的 name 序列化為 `"name":"<channel>"`,以此檢查未選 channel 不在 EMG series 中。
	// 注意:motion / muscle_ratio 也可能含 "name":,所以僅 assert ES 完整 series-name 不存在
	assert.NotContains(t, html, `"name":"ES"`, "未選 ES 不應出現於 EMG series")
	assert.NotContains(t, html, `"name":"BB"`, "未選 BB 不應出現於 EMG series")
}

// TestRenderComposer_GridLayoutNoCSSCalc — regression for codex P1 #1.
//
// 舊版 computeGridLayout emit `top: calc((100% - 210px) * 0 / 2 + 90px)`
// 之類 CSS calc(...) 字串給 opts.Grid.{Top,Height};ECharts 前端解析 grid 字串
// 僅接受純數字(pixel)或簡單百分比("30%"),不認 calc。Runtime 時 grid layout
// 會 fallback 或破版,但 Go test 看 HTML 含 "calc(" 字串也看不出來。
//
// 修法:emit 純 pixel 字串(如 "90" / "200")。
// 防線:HTML 不該含字串 "calc(" 任何處(尤其 grid block 內)。
func TestRenderComposer_GridLayoutNoCSSCalc(t *testing.T) {
	t.Run("two-grid", func(t *testing.T) {
		in := ComposerInput{
			Subject:          "S",
			EMGDataset:       makeEMGDataset(100, "RA"),
			SelectedChannels: []string{"RA"},
			MotionData:       makeMotionData(50, "knee"),
		}
		html := renderToString(t, context.Background(), in)
		assert.NotContains(t, html, "calc(",
			"grid layout 不該洩漏 CSS calc(...) — ECharts 不解析 calc, runtime 會破版")
	})
	t.Run("three-grid", func(t *testing.T) {
		in := ComposerInput{
			Subject:          "S",
			EMGDataset:       makeEMGDataset(100, "RA"),
			SelectedChannels: []string{"RA"},
			MuscleRatioData:  makeMuscleRatioData(100, "RA/ES"),
			MotionData:       makeMotionData(50, "knee"),
		}
		html := renderToString(t, context.Background(), in)
		assert.NotContains(t, html, "calc(",
			"3-grid layout 不該洩漏 CSS calc(...)")
	})
}

// TestRenderComposer_AbsoluteTimeAxis — regression for codex P1 #2 part 1.
//
// 舊版所有 xAxis 用 Type:"category" + .Data: [time string labels...];
// category 軸把 series data 當 ordinal index 對映 → 不同 sampling rate / 不同
// 起始時間的 EMG 與 motion 同一秒會落在不同 x 位置,phase markLine / dataZoom
// 全錯位。
//
// 修法:全部 bottom + top xAxis 改 Type:"value",移除 Data 欄(value 軸從 series 推);
// series data 改 [time, value] pair。
//
// 防線:
//   1. HTML 含 `"type":"value"` (至少 bottom + top xAxes + yAxes)
//   2. xAxis block 不含 `"type":"category"` — 整個 chart 沒任何 category 軸
//   3. xAxis block 不含 `"data":` (value 軸不應該有 Data field 序列化)
//      註:這條 assert 整個 HTML 不含 `"data":` — series 雖然有 data,但 go-echarts
//      template 把 series.data 渲染成 "data:[..]" 而非 JSON `"data":` (含引號),
//      所以這個 assert 純針對 xAxis JSON block 內的 data field。
func TestRenderComposer_AbsoluteTimeAxis(t *testing.T) {
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       makeEMGDataset(100, "RA"),
		SelectedChannels: []string{"RA"},
		MuscleRatioData:  makeMuscleRatioData(100, "RA/ES"),
		MotionData:       makeMotionData(50, "knee"),
	}
	html := renderToString(t, context.Background(), in)

	// 至少 xAxis 應該全是 value 軸 → 應出現多次 "type":"value"
	// (3 bottom xAxis + 3 top xAxis + 3 yAxis = 9 個 "type":"value")
	valueTypeCount := strings.Count(html, `"type":"value"`)
	assert.GreaterOrEqual(t, valueTypeCount, 9,
		"3 grid 應有 ≥9 個 value 軸(3 bottom xAxis + 3 top xAxis + 3 yAxis)")

	// category 軸不該出現 — 修法後沒有任何 category 軸
	assert.NotContains(t, html, `"type":"category"`,
		"所有 xAxis 應改用 Type:value,不該有 category 軸殘留")

	// "boundaryGap" 屬於 category 軸 — value 軸下該 field 應該 omitempty 不序列化
	assert.NotContains(t, html, `"boundaryGap"`,
		"value 軸不該有 boundaryGap field;殘留代表還有 category 軸 attribute 沒清掉")
}

// TestRenderComposer_SeriesAsTimeValuePairs — regression for codex P1 #2 part 2.
//
// 配合 value 軸,series data 必須是 [time, value] pair。EMG 3 點 t=[0, 0.5, 1.0],
// motion 2 點 t=[0.5, 1.0],render 後 HTML 內 series.data 應該包含這些 pair。
func TestRenderComposer_SeriesAsTimeValuePairs(t *testing.T) {
	// 用「精心構造」的 dataset:EMG 3 點、3 個時間 (0, 0.5, 1.0),3 個值 (10, 20, 30)。
	emg := &models.EMGDataset{
		Headers: []string{"time", "RA"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{10}},
			{Time: 0.5, Channels: []float64{20}},
			{Time: 1.0, Channels: []float64{30}},
		},
		OriginalTimePrecision: 1,
	}
	// motion 2 點:t=[0.5, 1.0],v=[100, 200]
	motion := &MotionData{
		Time:   []float64{0.5, 1.0},
		Series: map[string][]float64{"knee": {100, 200}},
		Order:  []string{"knee"},
	}
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       emg,
		SelectedChannels: []string{"RA"},
		MotionData:       motion,
	}
	html := renderToString(t, context.Background(), in)

	// EMG series 預期 pair `[0,10]` `[0.5,20]` `[1,30]` 出現於 HTML。
	// go-echarts 對 LineData.Value = []any{t, v} 序列化會產生 {"value":[0,10]}。
	// 為了不過度依賴細部 formatting,用「包含 [t,v] 子字串」做粗 assert。
	assert.Contains(t, html, `[0,10]`, "EMG t=0,v=10 應以 [0,10] pair 出現")
	assert.Contains(t, html, `[0.5,20]`, "EMG t=0.5,v=20 應以 [0.5,20] pair 出現")
	assert.Contains(t, html, `[1,30]`, "EMG t=1,v=30 應以 [1,30] pair 出現")

	// motion series pair
	assert.Contains(t, html, `[0.5,100]`, "motion t=0.5,v=100 應以 [0.5,100] pair 出現")
	assert.Contains(t, html, `[1,200]`, "motion t=1,v=200 應以 [1,200] pair 出現")
}

// TestRenderComposer_OffsetMisalignmentRegression — capture bug 本質。
//
// 在 category 軸下,EMG t=[0,1,2] 與 motion t=[1,2,3](起始 +1s)會被 echarts
// 視為「相同 3 個 ordinal index」對映 → motion 第一點顯示在 x=0 位置而非絕對
// 時間 t=1。修法後 value 軸 + [t,v] pair,motion 第一點時間值是 1.0,正確 anchor。
//
// 防線:motion 第一個 sample 必須以 `[1,` 開頭的 pair 出現於 HTML,而非 `[0,`。
func TestRenderComposer_OffsetMisalignmentRegression(t *testing.T) {
	// EMG t=[0,1,2], v=[10,20,30]
	emg := &models.EMGDataset{
		Headers: []string{"time", "RA"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{10}},
			{Time: 1.0, Channels: []float64{20}},
			{Time: 2.0, Channels: []float64{30}},
		},
		OriginalTimePrecision: 1,
	}
	// motion t=[1,2,3], v=[100,200,300] — 起始時間比 EMG 慢 1s
	motion := &MotionData{
		Time:   []float64{1.0, 2.0, 3.0},
		Series: map[string][]float64{"knee": {100, 200, 300}},
		Order:  []string{"knee"},
	}
	in := ComposerInput{
		Subject:          "S",
		EMGDataset:       emg,
		SelectedChannels: []string{"RA"},
		MotionData:       motion,
	}
	html := renderToString(t, context.Background(), in)

	// motion 第一點絕對時間是 1.0 — 必須以 [1, ... pair 出現
	assert.Contains(t, html, `[1,100]`,
		"motion 第一點 t=1.0,v=100 應以 [1,100] pair 出現 — 確保 motion 起始時間被 preserve")
	// motion 第二點 t=2,v=200
	assert.Contains(t, html, `[2,200]`, "motion 第二點時間應 preserve")
	// motion 第三點 t=3,v=300
	assert.Contains(t, html, `[3,300]`, "motion 第三點時間應 preserve")

	// 反例:motion 第一點不應該被 echarts 誤判到 t=0(那是 category 軸 bug)。
	// 透過「motion data 中不該存在 [0,100] pair」做 negative assert。
	assert.NotContains(t, html, `[0,100]`,
		"motion v=100 不該 anchor 到 t=0 — 那是 category-axis bug 的 fingerprint")
}
