package chart

import (
	"encoding/json"
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
//  3. 把 U+2028 / U+2029（JS line terminators）替換成 ` ` / ` `
//     literal 字串，阻擋 raw line terminator 在舊 JS engine 中破出字串。
//     `unicode.IsControl` 不認這兩個 codepoint（它們是 Zl / Zp category），
//     必須在 replacer 顯式處理，不能仰賴下面的 builder loop。
//  4. 拒絕 control character — 進一步把 stdin smuggling 阻擋掉。
//  5. 長度上限 1024 — title 不該那麼長；reject 異常輸入。
//
// 在 escape table 加入 (2)(3) 兩條,並補上 FuzzSanitizeChartString 守門。
//
// Exported（H3）：cci package 也要把 user-controlled subject 嵌進
// chart title/subtitle，需要跨 package 重用同一份 sanitizer 確保策略一致；
// 既有 chart 套件內 caller 走 sanitizeChartString alias。
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
// 為何需要四條 escape：
//  1. "</"  → "<\\/" : 阻擋 `</script>` 跳出 script context（OWASP）。
//  2. "<!"  → "<\\!" : 阻擋 `<!--` HTML comment injection；echarts 把 JSON
//     嵌進 <script>，瀏覽器 HTML parser 看到 `<!--` 會以為 script body 進入
//     HTML comment 狀態，後續直到 `-->` 都被忽略，攻擊者可挾帶任意 payload。
//  3. " " / " " → 對應 escape : JavaScript 把 U+2028
//     (LINE SEPARATOR) 與 U+2029 (PARAGRAPH SEPARATOR) 視為硬換行符
//     （ES5 之後仍保留，ES2019 才把它們納入 string literal 合法字元）。在
//     `<script>` 內的 JSON 字串中出現 raw U+2028 會被 V8 / Safari 等 engine
//     當作 statement terminator，可破出字串並執行後續任意 token。
//     `unicode.IsControl` 不認 U+2028 / U+2029，因此 builder loop 不會剔除，
//     必須在這裡顯式 escape 成 ` ` / ` ` literal。
//
//nolint:gochecknoglobals // immutable replace-table; documented above.
var chartStringEscaper = strings.NewReplacer(
	"</", "<\\/",
	"<!", "<\\!",
	" ", "\\u2028",
	" ", "\\u2029",
)

// sanitizeChartString 是 SanitizeChartString 的套件內 alias，保留既有 caller
// 命名一致性（chart 套件內所有舊用法不必同步改名）。
func sanitizeChartString(s string) string {
	return SanitizeChartString(s)
}

// sanitizeForJSString 用於把 user-controlled 字串嵌入 JS 字面字串時：用
// json.Marshal 取得 JSON-escaped 形式（含外層引號），讓嵌入 JS 後絕無
// 逸出字串邊界的可能。caller 不應該再外包引號。
//
// 防 XSS 三道契約 (明示):
//
//  1. 先過 sanitizeChartString:把 `</` → `<\/`、`<!` → `<\!`、U+2028/2029
//     →  / ,並剔除 control char + cap 長度。
//  2. json.Marshal:預設行為對 `<` / `>` / `&` 會 escape 為 `<` / `>`
//     / `&`(encoding/json 預設 HTMLEscape=true)。所以 sanitize 之後再過
//     json.Marshal,所有 HTML 特殊字元都會變成 `\uXXXX` 序列,絕對不會留下
//     可逸出 JS 字串邊界的 `"` 或 `</`。
//  3. 第二道 strings.ReplaceAll(s, "</", "<\\/"):
//     **與 (2) 重複,目前為 no-op (defensive)**。
//     原因:(2) 已把 `<` escape 成 `<`,輸出不會含字面 `</`,所以
//     ReplaceAll 不會 match 任何 substring。保留此行作為「即使未來 encoding/json
//     預設 HTMLEscape 行為改變、或 caller 改走 Encoder.SetEscapeHTML(false)」時
//     仍能擋下 `</script>` 跳出 — 屬於 defense-in-depth 守門。
//
// 如果未來 caller 改走 SetEscapeHTML(false) (例如為了輸出 emoji / 中文不被
// escape 成 \uXXXX),(2) 的 HTML escape 就失效,此時 (3) 的 ReplaceAll 變成
// 真正的 active defense。保留此 helper 不會降低安全強度,刪除則會 silently
// 退化 — 寧可保留 redundancy。
func sanitizeForJSString(s string) string {
	b, err := json.Marshal(sanitizeChartString(s))
	if err != nil {
		return `""`
	}
	// 第二道 defense-in-depth:目前 no-op (json.Marshal 預設 HTMLEscape=true 已
	// 把 `<` escape 成 <),保留以防 caller 未來改走 SetEscapeHTML(false)。
	out := strings.ReplaceAll(string(b), "</", "<\\/")
	return out
}
