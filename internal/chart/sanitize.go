package chart

import (
	"strings"
	"unicode"
)

// SanitizeChartString 防 XSS：echarts 把 options 序列化為 JSON 嵌入 HTML
// <script> 標籤；JSON serialization 不會 escape `</script>` 序列。攻擊者
// 控制的 title / label / channel header 含 `</script><script>...</script>`
// 可跳出 script context。對於 Wails 本機 WebView 等同 RPC 任意執行。
//
// 對策：
//  1. 把所有 `</` 改寫為 `<\/`（JSON 字串中等價，但無法被 `<script>` parser
//     提早結束）。這是業界標準（OWASP 推薦）的「JS string in HTML」escape。
//  2. 把 `<!` 改寫為 `<\!` 阻擋 `<!--` HTML comment injection。
//     `<script>` 內若出現 `<!--` 會讓瀏覽器 HTML parser 把 script body 視為
//     進入 comment 狀態，吞掉所有 token 直到 `-->`，攻擊者可挾帶 payload。
//  3. 在 builder loop 直接剔除 U+2028 / U+2029（JS line terminators）。raw
//     line terminator 在舊 JS engine 的 `<script>` 字串中會破出字串。
//     `unicode.IsControl` 不認這兩個 codepoint（它們是 Zl / Zp category），
//     故在 builder loop 顯式比對剔除(與 control char 同 path)。早期版本曾改用
//     replacer escape 成 literal,但 go-echarts 後續 JSON marshal 會把反斜線
//     二次轉義成可見亂碼 `\u2028`;直接剔除 raw 字元語意等價且無 double-encode。
//  4. 拒絕 control character — 進一步把 stdin smuggling 阻擋掉。
//  5. 長度上限 1024 — title 不該那麼長；reject 異常輸入。
//
// 在 escape table 加入 (2);(3) 改在 builder loop 剔除;並補上 FuzzSanitizeChartString 守門。
//
// Exported（H3）：cci package 也要把 user-controlled subject 嵌進
// chart title/subtitle，需要跨 package 重用同一份 sanitizer 確保策略一致。
func SanitizeChartString(s string) string {
	if s == "" {
		return s
	}
	if len(s) > 1024 {
		// 1024-byte 硬切可能落在 multi-byte rune (e.g. CJK 3 bytes)
		// 中段,留下 incomplete sequence。Go 的 range loop 把 invalid byte 解
		// 成 U+FFFD ("�") 並寫入 builder,user 會在 chart title 結尾看到 mojibake。
		// 用 strings.ToValidUTF8 把尾端 incomplete sequence 整段拋掉 (replacement
		// 為 "" → 直接剔除而非替換成 U+FFFD),確保下游 builder 看到的是 valid UTF-8。
		s = strings.ToValidUTF8(s[:1024], "")
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == 0:
			continue
		case unicode.IsControl(r) && r != '\t':
			continue
		case r == ' ' || r == ' ':
			// JS line terminators(Zl / Zp category,unicode.IsControl 不認)。
			// 直接剔除 raw 字元 — XSS 防護來自「raw terminator 不存在」,且避免
			// 先前 escape 成 literal 後被 go-echarts JSON marshal 二次轉義成
			// 可見亂碼 `\\u2028`。
			continue
		default:
			b.WriteRune(r)
		}
	}
	return chartStringEscaper.Replace(b.String())
}

// chartStringEscaper 把 user-controlled 字串嵌入 echarts JSON / HTML <script>
// context 前所需的全部 escape pairs。集中在 package var 避免每次呼叫都重
// 建 replacer（NewReplacer 內部會把 patterns 編成 trie）。
//
// 為何需要這兩條 escape(U+2028 / U+2029 改在 SanitizeChartString 的 builder loop 剔除)：
//  1. "</"  → "<\\/" : 阻擋 `</script>` 跳出 script context（OWASP）。
//  2. "<!"  → "<\\!" : 阻擋 `<!--` HTML comment injection；echarts 把 JSON
//     嵌進 <script>，瀏覽器 HTML parser 看到 `<!--` 會以為 script body 進入
//     HTML comment 狀態，後續直到 `-->` 都被忽略，攻擊者可挾帶任意 payload。
//
// U+2028 / U+2029（JS line terminators,unicode.IsControl 不認的 Zl / Zp）改由
// SanitizeChartString builder loop 直接剔除 raw 字元,不再走 replacer escape —
// 避免 escape 後的反斜線被 go-echarts JSON marshal 二次轉義成可見亂碼。
//
//nolint:gochecknoglobals // immutable replace-table; documented above.
var chartStringEscaper = strings.NewReplacer(
	"</", "<\\/",
	"<!", "<\\!",
)
