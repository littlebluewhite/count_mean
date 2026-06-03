package cci

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CCI chart 的 customJS 透過 postMessage 把 restore / legendselect 事件回
// 傳給 parent;origin allowlist (wailsParentOrigins) 現定義於
// assets.IframeCommsJS,由 addCCICustomJS 拼接注入 customJS。ADR-0003 後
// envelope 從 raw string 改為 {type: '<event>'} object,讓 iframeBridge
// 在 parent 端可以用 type 比對 dispatch。
//
// 本測試確認：
//  1. 產生的 HTML 不再含有 `postMessage("cci-chart-restored", "*")` 字面
//     (regression guard — 直接走原始字串 + "*" targetOrigin 是 over-permissive)
//  2. 3 個 Wails 平台 origin(darwin/linux/windows)都被嵌入 allowlist
//  3. allowlist 是「字串陣列」形式,且 postToParent 走新 envelope 格式
func TestAddCCICustomJS_PostMessageOriginAllowlisted(t *testing.T) {
	result := &CCIAnalysisResult{
		Subject:    "origin-test",
		TimeValues: []float64{0, 1, 2},
		PairResults: []CCIResult{
			{PairName: "RA/ES", Values: []float64{0.1, 0.2, 0.3}},
		},
		GaitStartTime: 0,
		GaitEndTime:   2.0,
	}

	var buf bytes.Buffer

	err := GenerateCCIInteractiveChart(context.Background(), result, &buf)
	require.NoError(t, err)

	html := buf.String()

	// 1. 必須不再有「caller 直接 postMessage("cci-chart-restored", "*")」字面 —
	// 這是早期 over-permissive 寫法。outbound '*' 退路只能藏在 postToParent
	// helper 內(memory feedback_wails_sandbox_iframe_crossframe 要求 dev fallback,
	// 詳見 ADR-0003 outbound policy)。
	postMessageStarRestored := regexp.MustCompile(
		`postMessage\(\s*['"]cci-chart-restored['"]\s*,\s*['"]\*['"]`)
	postMessageStarLegend := regexp.MustCompile(
		`postMessage\(\s*['"]cci-chart-legend-changed['"]\s*,\s*['"]\*['"]`)
	assert.False(t, postMessageStarRestored.MatchString(html),
		"caller 不應直接 postMessage('cci-chart-restored', '*') — 走 postToParent")
	assert.False(t, postMessageStarLegend.MatchString(html),
		"caller 不應直接 postMessage('cci-chart-legend-changed', '*') — 走 postToParent")

	// 2. 3 個 Wails origin 都要在 HTML 內找得到
	// （他們嵌在 assets.IframeCommsJS 的 wailsParentOrigins 字串陣列,拼接注入 customJS）。
	assert.Contains(t, html, "wails://wails",
		"darwin/linux Wails origin 應在 allowlist")
	assert.Contains(t, html, "http://wails.localhost",
		"windows Wails origin 應在 allowlist")
	assert.Contains(t, html, "https://wails.localhost",
		"windows future-https-scheme Wails origin 應在 allowlist（前向相容）")

	// 3. allowlist 是 JS 字串陣列形式,且 postToParent helper 在 chart restore /
	//    legendselectchanged 事件中走 ADR-0003 envelope 形式呼叫。
	assert.Contains(t, html, "wailsParentOrigins",
		"allowlist 應以 wailsParentOrigins 變數命名（方便日後 grep 與 audit）")
	assert.Contains(t, html, `postToParent({type: 'cci-chart-restored'})`,
		"restore 事件 envelope 必須是 {type: 'cci-chart-restored'} — bridge 用 type 比對 dispatch")
	assert.Contains(t, html, `postToParent({type: 'cci-chart-legend-changed'})`,
		"legendselectchanged envelope 必須是 {type: 'cci-chart-legend-changed'}")
}

// TestAddCCICustomJS_ChartCommsInjected mirrors PhaseMarkersInjected: the
// shared assets.IframeCommsJS IIFE must be concatenated into the CCI customJS
// so the inline body can call window.__chartComms.* (ADR-0003 family).
func TestAddCCICustomJS_ChartCommsInjected(t *testing.T) {
	html := renderCCIToString(t)

	assert.Contains(t, html, `window.__chartComms`,
		"IframeCommsJS IIFE 必須 attach helpers 至 window.__chartComms")
	assert.Contains(t, html, `function postToParent`,
		"IframeCommsJS 必須含 postToParent declaration")
	assert.Contains(t, html, `function isFromParent`,
		"IframeCommsJS 必須含 isFromParent declaration")
	assert.Contains(t, html, `function handlePngRequest`,
		"IframeCommsJS 必須含 handlePngRequest declaration")
	// Pin the migration symmetrically: CCI's old inline negated guard must be
	// GONE too (same partial-refactor false-pass risk as composer). (codex R2.)
	assert.NotContains(t, html, `e.source !== window.parent`,
		"CCI 不可保留舊 inline negated guard;origin 驗證必須走 window.__chartComms.isFromParent")
}

// renderCCIToString 共用 fixture — 給後續 ADR-0003 listener / syntax assertion
// 共用,避免每個 test 重複 result struct + buf 樣板。
func renderCCIToString(t *testing.T) string {
	t.Helper()
	result := &CCIAnalysisResult{
		Subject:    "bridge-test",
		TimeValues: []float64{0, 1, 2},
		PairResults: []CCIResult{
			{PairName: "RA/ES", Values: []float64{0.1, 0.2, 0.3}},
		},
		GaitStartTime: 0,
		GaitEndTime:   2.0,
	}
	var buf bytes.Buffer
	err := GenerateCCIInteractiveChart(context.Background(), result, &buf)
	require.NoError(t, err)
	return buf.String()
}

// TestAddCCICustomJS_HasUpdatePhaseMarkersListener — ADR-0003 latent bomb #1 拆除:
//
// 修補根因:wails dev 預設 webview opaque-origin sandbox 嚴格,parent 跨 frame
// `iframe.contentWindow.echarts.setOption` 在 CCI category mode 是 silent block,
// CCI phase 切換在 wails dev 整片失效(memory feedback_wails_sandbox_iframe_crossframe)。
//
// 修法:phaseLines.mjs category mode setOption 路徑全部廢除,parent 改 bridge.send
// `cci-update-phase-markers` + payload.checkedPhases;iframe customJS 收到後在
// iframe 內 setOption,完全繞開 cross-frame 限制。
//
// 防線:customJS 必須含 `cci-update-phase-markers` listener、`checkedPhases` 解構、
// `findNearestLabel` 呼叫(category axis 必須對映到 string label),且仍走
// `setOption({series, xAxis})` 完整重畫。
func TestAddCCICustomJS_HasUpdatePhaseMarkersListener(t *testing.T) {
	html := renderCCIToString(t)

	assert.Contains(t, html, `cci-update-phase-markers`,
		"customJS 必須含 phase markers postMessage listener(ADR-0003 latent bomb #1 拆除)")
	assert.Contains(t, html, `checkedPhases`,
		"ADR-0003 §5 symmetric payload:iframe handler 必須解構 payload.checkedPhases")
	assert.Contains(t, html, `findNearestLabel`,
		"category-axis CCI 必須走 findNearestLabel 把 numeric phaseTime 對映回 string xAxis label")
	assert.Contains(t, html, `markLine`,
		"iframe 端 setOption 仍走 series.markLine 路徑")
}

// TestAddCCICustomJS_HasRequestPngListener — ADR-0003 latent bomb #2 拆除:
//
// 修補根因:downloadCCIChart 既有 `iframe.contentWindow.echarts.getInstanceByDom`
// 跨 frame 讀 chart instance 在 wails dev sandbox 下 silent fail。
//
// 修法:走 postMessage round-trip — parent bridge.requestReply('cci-request-png'),
// iframe 端 myChart.getDataURL → post 回 'cci-png-result' 帶 ADR-0003 §4 reply
// envelope {requestId, payload: {dataURL}} 或 {requestId, error}。
//
// 防線:customJS 必須同時含三條 token — request 識別字、result 識別字、getDataURL 呼叫。
func TestAddCCICustomJS_HasRequestPngListener(t *testing.T) {
	html := renderCCIToString(t)

	assert.Contains(t, html, `cci-request-png`,
		"customJS 必須含 PNG request 識別字(ADR-0003 latent bomb #2 拆除)")
	assert.Contains(t, html, `cci-png-result`,
		"customJS 必須含 PNG result 識別字(reply envelope type)")
	assert.Contains(t, html, `getDataURL`,
		"PNG 取值呼叫(myChart.getDataURL)必須在 iframe 端發生,不再 cross-frame")
}

// TestAddCCICustomJS_PhaseMarkersInjected — ADR-0003 §8:
//
// PhaseMarkersJS canonical(internal/chart/assets/phasemarkers.mjs)透過 //go:embed
// 注入 CCI iframe customJS 前段,把 helpers attach 到 window.__phaseMarkers。
// 後續 listener body 才能呼叫 findNearestLabel(無需 cross-frame import)。
//
// 防線:customJS 必須含 function declaration 識別字,確保 IIFE 真的 inline 進去
// 而非被 build / strip 流程吃掉。
func TestAddCCICustomJS_PhaseMarkersInjected(t *testing.T) {
	html := renderCCIToString(t)

	assert.Contains(t, html, `function recalcPercents`,
		"PhaseMarkersJS IIFE 必須含 recalcPercents declaration")
	assert.Contains(t, html, `function findNearestLabel`,
		"PhaseMarkersJS IIFE 必須含 findNearestLabel declaration")
	assert.Contains(t, html, `window.__phaseMarkers`,
		"PhaseMarkersJS 必須 attach helpers 至 window.__phaseMarkers")
}

// TestAddCCICustomJS_SyntaxValid — 釘住 go-echarts `newlineTabPat` 陷阱:
// 若 customJS 內含 `//` line comment,strip 後吃掉 closing braces → SyntaxError。
// Plan §Risk 4 要求 CCI 也加 brace balance 等價斷言(對齊 Composer 既有
// TestRenderComposer_CustomJSSyntaxValid)。
func TestAddCCICustomJS_SyntaxValid(t *testing.T) {
	html := renderCCIToString(t)

	// wailsParentOrigins URLs 完整(driver: `//` 在 string literal 內不被 strip)
	assert.Contains(t, html, `"wails://wails"`, "wails:// URL 完整")
	assert.Contains(t, html, `"http://wails.localhost"`, "http:// URL 完整")
	assert.Contains(t, html, `"https://wails.localhost"`, "https:// URL 完整")

	scriptStart := strings.Index(html, `<script type="text/javascript">`)
	require.GreaterOrEqual(t, scriptStart, 0, "找不到 <script> 區段")
	scriptStart += len(`<script type="text/javascript">`)
	scriptEnd := strings.Index(html[scriptStart:], `</script>`)
	require.GreaterOrEqual(t, scriptEnd, 0, "找不到 </script>")
	js := html[scriptStart : scriptStart+scriptEnd]

	openBraces := strings.Count(js, `{`)
	closeBraces := strings.Count(js, `}`)
	assert.Equal(t, openBraces, closeBraces,
		"CCI <script> 區段 { 與 } 數量必相等 — 不平衡代表 newline-strip 吃掉 closing braces")
}
