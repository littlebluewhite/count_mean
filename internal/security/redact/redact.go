// Package redact 提供 process-wide path-redaction helper:
//
// 把 caller-facing 文字裡的 absolute path 替換成 `<redacted-path>/`,保留檔名與
// 行號(對 debug 與 audit log 仍精確識別 source location)。
//
// # 為什麼存在這個 package
//
// 過去 gui/recover.go 內定義了 redactPathsInStack + pathRedactPattern,專供 panic
// recovery 路徑 redact debug.Stack() 用。需要把同樣的 redact 行為套用到
// handler 層的 error.Error() message(避免 patient 看到 `/Volumes/patient_xx/...`
// 直接洩漏路徑),又需要把同樣的 path pattern 套到 logger.sanitizeMessage。
//
// 與其在三個地方各 copy 一份正則(每次更新 root prefix 都得三邊改),不如抽到
// internal/security/redact 作為 single source of truth:
//
//   - gui/recover.go::redactPathsInStack → 改成 thin wrapper 呼叫 redact.Paths()
//   - handler 錯誤訊息塞給前端前在 gui 的 Webview envelope(ADR-0036)出口 redact:
//     Message 通道 gui/envelope.go(failMessage / inputMessage → RedactForMessage)、
//     err 通道 gui/recover.go(recoverHandlerPanic → Paths)
//   - internal/logging/logger.go::sanitizeMessage 直接調用 Paths()
//
// # 為什麼不直接 export pathRedactPattern
//
// 暴露 *regexp.Regexp 給 caller 雖然方便,但 caller 直接拿 Regexp 自己處理會繞過
// L2 line-fallback path(gui/recover.go 原本對未被 regex 抓到的「以 / 開頭的 line」
// 走 trim + basename + <redacted-path> 拼接的 fallback)。把 Paths() 包成單一進入點
// 才能保證 caller 都吃到完整 redact pipeline,未來新增 fallback rule 也只改一處。
package redact

import (
	"path/filepath"
	"regexp"
	"strings"
)

// pathRedactPattern 匹配字串中**任意 ≥1 目錄段的路徑**(POSIX `/a/b/`、Windows
// drive-letter、UNC),把目錄部分換成 `<redacted-path>/`、保留 basename。
//
// # 為什麼不做前導邊界錨定(no-boundary 設計)
//
// 早期版本曾加前導邊界(僅當路徑前接分隔符才匹配)以避免過度脫敏相對路徑;但 codex
// review 連續 5 輪揭示邊界錨定對 PHI 是錯誤偏置 — 它為減少 false-positive(過度脫敏=
// 可讀性損失)而引入 false-negative(跳脫換行 / 冒號標籤 / file:// URL / 帶 host 的
// file URL 等「路徑前綴不是分隔符」情形下漏脫敏=PHI 洩漏)。PHI redaction 應反過來:
// 寧可過度脫敏,絕不洩漏。故移除邊界,直接匹配任意 `/dir/.../` 序列。
//
// 取捨(刻意接受):多段相對路徑(`a/b/c.go`→`a<redacted-path>/c.go`)、URL path、
// `file://host/path` 的 host 都會被一併脫敏(P3、安全方向、僅 log 可讀性損失)。單段相對
// 參照(`internal/x.go:12` — 無 trailing-slash 目錄段)不受影響。basename 不被消費而保留。
//
// # 目錄段文法
//
// 目錄段 = 以 1+ 個半形空白分隔的「詞」(posixWord / winWord),不能以空白開頭或結尾。
//   - POSIX 詞:不含空白、`/`、`"`;`\` 是一般字元;`'` 與 `:` 只能在詞中間(O'Neil、
//     macOS Finder 名稱的 2026:05:18)。頭尾限制讓路徑後的 `: no such file`、閉引號
//     (`'/a/b.csv' and '/c/d.csv'`)不被吃掉;stack 的 `recover.go:42 +0x1a` 後面沒有
//     `/`,整段不成目錄段。
//   - drive-letter / UNC 詞:不含空白、`\`、`/`、`:`、`"`(後兩者在 Windows 名稱不合法),
//     `'` 可在任何位置。分隔字元是 `\`、`/` 或 %q 跳脫後成對的 `\\`。
//
// 換行是空白:Paths 要吃原始文字,且同一段文字不可在跳脫後再吃一次。換行已跳脫成
// 字面 `\n` / `\t` 的文字再呼叫的話,多行 stack 會被黏成一個「目錄段」而只剩最後一個
// frame —— logger.sanitizeMessage 因此先 Paths 再跳脫控制字元,writeText 組 `k=v` 時
// 也不對已 sanitize 的 value 重跑 Paths。
//
// 不符文法的目錄段會中斷匹配、該段原文留存;其後的目錄段要看分隔字元(ADR-0036
// Decision 5):`/` 會讓 POSIX 分支重新起始,只有該段留存;單一 `\` 不會重新起始任何
// 分支,該段之後到末段前的所有目錄段都留存;%q 的 `\\` 可讓 UNC 分支重新起始(需其後
// 至少兩個目錄段)。放寬文法(例如允許 `: `)會把 `c.csv: input/` 當成目錄段而吃掉
// basename 與錯誤文字。
//
//nolint:gochecknoglobals // immutable regex shared across redact callers
var pathRedactPattern = regexp.MustCompile(
	// POSIX:`/` 後 1+ 個「目錄段/」(涵蓋 /Volumes/pCloud Drive/、/Users/x/O'Neil/)。
	`/(?:` + posixSegment + `/)+` +
		// Windows drive-letter(`C:\...` 或 `C:/...`);`\b` 要求盤符在詞邊界,避免把
		// `file:/path` 的 `e:/`(`e` 前接詞字符 `l`)誤當盤符而吃掉 label 末字母。
		`|\b[A-Za-z]:` + winSep + `(?:` + winSegment + winSep + `)+` +
		// UNC(`\\server\share\...`;%q 跳脫後開頭是 4 個反斜線)
		`|\\\\(?:\\\\)?` + winSegment + `(?:` + winSep + winSegment + `)+` + winSep,
)

const (
	// posixWord:POSIX 目錄段的一個詞(`\` 是一般字元)。詞中間可夾 `'`(O'Neil)與
	// `:` —— macOS Finder 名稱裡的 `/`(例「2026/05/18」)在 POSIX 層是 `:`。
	posixWord    = `[^\s/:"']+(?:[':][^\s/:"']+)*`
	posixSegment = posixWord + `(?: +` + posixWord + `)*`

	// winWord:drive-letter / UNC 目錄段的一個詞。`\` 是分隔字元、`:` 與 `"` 在
	// Windows 名稱不合法,三者排除;`'` 可在任何位置('Jane'、O''Neil)。
	winWord    = `[^\s:"\\/]+`
	winSegment = winWord + `(?: +` + winWord + `)*`

	// winSep:`\`、`/`,或 %q 格式化後成對的 `\\`(`resolved="C:\\Users\\..."`)。
	winSep = `(?:\\\\?|/)`
)

// lineFallbackPathPrefix 是 line-loop fallback 的 trigger — 任何 trim 後以 absolute
// path opener(POSIX `/`、UNC `\\`、Windows drive-letter `[A-Za-z]:[\\/]`)開頭的
// line 都會被整段 trim 成 `<redacted-path>/<basename>`。覆蓋 custom `$GOPATH`
// 或非標準 mount 等不被 pathRedactPattern 抓到的長尾情境。
//
//nolint:gochecknoglobals // immutable regex shared across redact callers
var lineFallbackPathPrefix = regexp.MustCompile(`^(?:/|\\\\|[A-Za-z]:[\\/])`)

// Paths 把 stack trace / error message 內的絕對路徑換成 `<redacted-path>/`,
// 保留檔名與行號(對 debug 而言已足夠精確識別 source location)。
//
// 範例:
//
//	in:  goroutine 1 [running]:
//	     /Users/wilson/IdeaProjects/count_mean/gui/recover.go:42 +0x1a
//	out: goroutine 1 [running]:
//	     <redacted-path>/recover.go:42 +0x1a
//
// 此 redact 不可逆,但 debug 用 UUID 對應 internal log 已足夠(若真需要原始
// stack,把 logger level 開到 Trace 不啟用此 redact 是另一條 follow-up)。
//
// 與 gui/recover.go 原版的 redactPathsInStack 行為等價(搬家)。
func Paths(s string) string {
	redacted := pathRedactPattern.ReplaceAllString(s, "<redacted-path>/")

	// 處理少數沒被 regex 抓到但仍含 path-like 字串的邊角(例:custom $GOPATH,
	// 不在標準 system root 下)。保守再過一輪:任何 trim 後以 absolute path
	// opener 開頭的 line(POSIX `/`、UNC `\\`、Windows drive-letter `C:\/`)
	// 都改寫成 `<redacted-path>/<basename>`。
	lines := strings.Split(redacted, "\n")
	for i, ln := range lines {
		trimmed := strings.TrimSpace(ln)
		if lineFallbackPathPrefix.MatchString(trimmed) {
			lines[i] = "<redacted-path>/" + filepath.Base(trimmed)
		}
	}

	return strings.Join(lines, "\n")
}

// RedactForMessage 對 error 文字做 path-redact 處理後回傳 string。nil error 回空
// 字串,caller 不必先 nil-check。
//
// gui 的 caller 只有 gui/envelope.go 的 failMessage / inputMessage([[Webview envelope]],
// ADR-0036):handler 不得自己呼叫它拼 Message,一律經那兩個 helper(AST 守門:gui 內
// 只有 envelope.go 與 recover.go 可 import 本套件)。
//
// 主目標:happy-path error message 不能塞 absolute path PII 給 webview。
// patient 看到的錯誤訊息走 RedactForMessage 後,絕對 path 都會被換成 `<redacted-path>`,
// 但「permission denied」、「i/o error」、「file not found」等非路徑語意保留。
func RedactForMessage(err error) string {
	if err == nil {
		return ""
	}
	return Paths(err.Error())
}
