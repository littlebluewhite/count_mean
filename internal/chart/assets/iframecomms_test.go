package assets

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIframeCommsJS_WailsParentOriginsIsValidJSONArray migrates the former
// internal/cci TestWailsParentOrigins_IsValidJSONArray. The wailsParentOrigins
// allowlist moved from a CCI Go const into this shared .mjs; assert it against
// the embedded string so the guard tracks what actually ships in the iframe.
func TestIframeCommsJS_WailsParentOriginsIsValidJSONArray(t *testing.T) {
	expectedSchemes := []string{
		`"wails://wails"`,
		`"http://wails.localhost"`,
		`"https://wails.localhost"`,
	}
	for _, scheme := range expectedSchemes {
		assert.Contains(t, IframeCommsJS, scheme,
			"origin %s 必須以 quoted string 形式存在於 .mjs allowlist", scheme)
	}
	assert.Contains(t, IframeCommsJS,
		`var wailsParentOrigins = ["wails://wails", "http://wails.localhost", "https://wails.localhost"]`,
		"wailsParentOrigins 必須是 well-formed JS array literal")
}

// TestIframeCommsJS_ExposesHelpers asserts the three primitives land on the
// shared window namespace.
func TestIframeCommsJS_ExposesHelpers(t *testing.T) {
	for _, frag := range []string{
		`window.__chartComms`,
		`function postToParent`,
		`function isFromParent`,
		`function handlePngRequest`,
	} {
		assert.Contains(t, IframeCommsJS, frag, "IframeCommsJS 必須含 %s", frag)
	}
}

// TestIframeCommsJS_BlockCommentOnly is the real block-comment-only tripwire.
// go-echarts AddJSFuncStrs strips newlines/tabs from the concatenated customJS;
// a // line comment then eats to the next newline (or to </script>) and blanks
// the chart silently (image #5). The brace-balance syntax tests count braces
// lexically and would NOT catch this. Strip /* */ block comments + neutralize
// URL schemes (://), then assert no // line comment remains.
func TestIframeCommsJS_BlockCommentOnly(t *testing.T) {
	src := IframeCommsJS
	for {
		start := strings.Index(src, "/*")
		if start == -1 {
			break
		}
		rel := strings.Index(src[start:], "*/")
		require.NotEqual(t, -1, rel,
			"iframecomms.mjs 有未閉合的 /* */ block comment(無效 JS,會使圖表空白)")
		src = src[:start] + src[start+rel+2:]
	}
	src = strings.ReplaceAll(src, "://", ":@@") // wails:// http:// https:// are legit
	assert.NotContains(t, src, "//",
		"iframecomms.mjs 只能用 /* */ block comments;// line comment 會被 newlineTabPat 吃掉並使圖表空白")
}
