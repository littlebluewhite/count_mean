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
// 傳給 parent,targetOrigin 從 "*" 改成 Wails 平台 origin allowlist。
// "*" 在純 Wails WebView 場景下是 over-permissive,即使現在沒有 third-party
// embedder,也應該明確列舉合法 parent origin 以防回頭引入安全 surface。
//
// 本測試確認：
//  1. 產生的 HTML 不再含有 postMessage("*") 字面（regression guard）
//  2. 3 個 Wails 平台 origin（darwin/linux/windows）都被嵌入 allowlist
//  3. allowlist 是「字串陣列」形式，符合 JS 的 indexOf / for-loop pattern
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

	// 1. 必須不再含有 "*" targetOrigin(regression guard)。
	//
	// 用 regex 取代 plain-string 比對:plain string `postMessage('cci-chart-restored', '*')`
	// 對 quote (single vs double) / 空白變化太脆弱;若 go-echarts 內部產生器版本
	// 變動把 quote 改為 double-quote 或空白排版改變,regression guard 會 silently
	// 失效。改成 regex 容忍 quote / 空白變化,只要 targetOrigin 仍是字面 "*" 都會被擋下。
	postMessageStarRestored := regexp.MustCompile(
		`postMessage\(\s*['"]cci-chart-restored['"]\s*,\s*['"]\*['"]`)
	postMessageStarLegend := regexp.MustCompile(
		`postMessage\(\s*['"]cci-chart-legend-changed['"]\s*,\s*['"]\*['"]`)
	assert.False(t, postMessageStarRestored.MatchString(html),
		"postMessage targetOrigin 不應再是 '*'")
	assert.False(t, postMessageStarLegend.MatchString(html),
		"postMessage targetOrigin 不應再是 '*'")

	// 2. 3 個 Wails origin 都要在 HTML 內找得到
	// （他們嵌在 customJS 的 wailsParentOrigins 字串陣列）。
	assert.Contains(t, html, "wails://wails",
		"darwin/linux Wails origin 應在 allowlist")
	assert.Contains(t, html, "http://wails.localhost",
		"windows Wails origin 應在 allowlist")
	assert.Contains(t, html, "https://wails.localhost",
		"windows future-https-scheme Wails origin 應在 allowlist（前向相容）")

	// 3. allowlist 是 JS 字串陣列形式，且 postToParent helper 在 chart restore /
	//    legendselectchanged 事件中都被呼叫。
	assert.Contains(t, html, "wailsParentOrigins",
		"allowlist 應以 wailsParentOrigins 變數命名（方便日後 grep 與 audit）")
	assert.Contains(t, html, "postToParent('cci-chart-restored')",
		"restore 事件應透過 postToParent helper 呼叫，而非直接 postMessage")
	assert.Contains(t, html, "postToParent('cci-chart-legend-changed')",
		"legendselectchanged 事件應透過 postToParent helper 呼叫")
}

// TestWailsParentOrigins_IsValidJSONArray 校驗 wailsParentOrigins 常數本身
// 是合法的 JSON / JS literal — 任何破壞 array literal 結構的修改（譬如忘了
// 加 quote、引入 newline）都會被擋下。
func TestWailsParentOrigins_IsValidJSONArray(t *testing.T) {
	// 必須以 [ 開頭 ] 結尾
	trimmed := strings.TrimSpace(wailsParentOrigins)
	require.True(t, strings.HasPrefix(trimmed, "["), "wailsParentOrigins 必須以 [ 開頭")
	require.True(t, strings.HasSuffix(trimmed, "]"), "wailsParentOrigins 必須以 ] 結尾")

	// 每個 element 必須用雙引號包起
	expectedSchemes := []string{
		`"wails://wails"`,
		`"http://wails.localhost"`,
		`"https://wails.localhost"`,
	}
	for _, scheme := range expectedSchemes {
		assert.Contains(t, wailsParentOrigins, scheme,
			"origin %s 必須以 quoted string 形式存在於 allowlist", scheme)
	}
}
