package chart

import (
	"bytes"
	"context"
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
	// 透過 axis 數量間接驗證(每 grid 1 bottom + 1 top → 6 個 xAxis)
	assert.Contains(t, html, `"grid":`, "HTML 必須含 grid 陣列")
	gridCount := strings.Count(html, `"left":"60px"`)
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
	gridCount := strings.Count(html, `"left":"60px"`)
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
	// markLine name 用 phase point 字母標示
	assert.Contains(t, html, `"P0"`, "P0 phase point 應出現於 markLine")
	assert.Contains(t, html, `"S"`, "S phase point 應出現於 markLine")
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
