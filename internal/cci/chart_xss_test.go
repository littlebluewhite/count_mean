package cci

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateCCIInteractiveChart_SubjectXSSEscaped 是 regression test：
// CCI chart subtitle 直接把 result.Subject 嵌入 echarts option，而 go-echarts 用
// SetEscapeHTML(false) 序列化 JSON，含 `</script>` 序列的 user-controlled subject
// 會破出 <script> context（Wails WebView 等同 RPC 任意執行）。
//
// 修法：subtitle / title 應走 chart.SanitizeChartString，把 `</` rewrite 為 `<\/`。
// 即使最終嵌入 HTML <script>，瀏覽器 parser 也找不到 `</script>` close tag。
//
// 本測試確認：
//  1. user-controlled Subject 含完整 `</script><script>` payload，輸出 HTML 不含
//     原樣 `</script><script>` 序列（regression guard）
//  2. 安全 Subject（包含 `&` / 中文 / 普通文字）仍正確顯示
//  3. control char (e.g. NUL / ESC) 不會 smuggle 進輸出
func TestGenerateCCIInteractiveChart_SubjectXSSEscaped(t *testing.T) {
	t.Run("malicious_subject_blocks_script_close", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:    `</script><script>alert(1)</script>`,
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

		// regression guard：原樣 `</script><script>` 序列不該出現
		assert.NotContains(t, html, "</script><script>",
			"subtitle should not allow script-close XSS")

		// 任何形式的 `</script><` 都意味著可能逸出 script context（chart 自身結尾
		// 標籤是 `</script>\n` 後不會緊跟 `<`）
		assert.NotContains(t, html, "</script><",
			"any </script>< suggests breakout from script context")

		// sanitize 後 `</` rewrite 為 `<\/`，echarts 再用 json.Marshal 序列化
		// 整個 option，JSON 對 `\` 自身會再 escape 一層為 `\\`，因此最終 HTML 中
		// 應該看到 raw byte 序列 `<\\/script`（5 bytes：`<`、`\`、`\`、`/`、`s`）。
		// double-escape 是 OK 的：HTML parser 看 script context 內的 JSON，
		// 找不到 `</script>` close tag，XSS 路徑被阻斷。
		assert.Contains(t, html, `<\\/script`,
			`after sanitize+JSON-marshal, </script should appear as <\\/script in HTML`)
	})

	t.Run("safe_subject_passthrough", func(t *testing.T) {
		result := &CCIAnalysisResult{
			Subject:    "SF8-受試者甲 & SF9",
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
		assert.Contains(t, html, "SF8-受試者甲", "normal CJK subject should appear verbatim")
		assert.Contains(t, html, "SF9", "text after & should appear verbatim")
	})

	t.Run("control_char_subject_filtered", func(t *testing.T) {
		// 含 NUL / ESC 等 control char 的 subject — 應該被 sanitize strip 掉
		result := &CCIAnalysisResult{
			Subject:    "Subject\x00Smuggle\x1bABC",
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
		htmlLower := strings.ToLower(html)

		// raw NUL / ESC 不該在輸出
		assert.NotContains(t, html, "\x00", "raw NUL should not appear in output")
		assert.NotContains(t, html, "\x1b", "raw ESC should not appear in output")

		// JSON unicode escape 形式（六字 ASCII：反斜線+u+0000、反斜線+u+001b）
		// 也不該出現 — sanitize 是 strip 而非 escape。用 string concat 避免
		// raw control byte 進入 source。
		escapedNUL := "\\" + "u0000"
		escapedESC := "\\" + "u001b"
		assert.NotContains(t, htmlLower, escapedNUL,
			"NUL should be stripped, not escaped as \\u0000")
		assert.NotContains(t, htmlLower, escapedESC,
			"ESC should be stripped, not escaped as \\u001b")

		// subject 的 printable 部分仍應保留（strip 後三段 printable 緊鄰）
		assert.Contains(t, html, "Subject", "subtitle prefix should remain")
		assert.Contains(t, html, "SubjectSmuggleABC", "printable runs should be concatenated after strip")
	})
}
