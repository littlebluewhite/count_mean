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
//   - gui/cci_handlers.go / muscle_ratio_handlers.go / normalized_phase_sync_handlers.go
//     錯誤訊息塞給前端前先過 redact.RedactForMessage(err)
//   - internal/logging/logger.go::sanitizeMessage 加入 redact.PathPattern() / 直接調用 Paths()
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

// pathRedactPattern 匹配任意絕對路徑格式。
//
// POSIX 分支採「任意絕對路徑 + 前導邊界錨定」設計:
//   - 前導邊界(group 1,回吐):行首 ^ 或 空白/引號/(/ = 之一。
//     Go RE2 無 lookbehind,用捕獲組 + "${1}<redacted-path>/" 回吐邊界字符。
//     相對路徑(internal/x.go、./x.go)的 `/` 前接詞字符或 `.`,不在邊界集合 → 天然豁免。
//   - POSIX 絕對路徑:後接 1+ 個「元素/」;元素內可含單空白分隔子詞(涵蓋 pCloud Drive)。
//   - 元素字符排除 \s/:"' — 避免吃掉相鄰 token、閉引號或 recover.go:42 的行號。
//   - Windows drive-letter 與 UNC 亦包含在 group 1 邊界保護內。
//
//nolint:gochecknoglobals // immutable regex shared across redact callers
var pathRedactPattern = regexp.MustCompile(
	// group 1:前導邊界(行首 / 空白 / 引號 / ( / = / 跳脫換行 \n \r),回吐以確保只
	// 匹配絕對路徑(相對路徑的 `/` 前接詞字符或 `.`,不在邊界集合 → 天然豁免;RE2 無
	// lookbehind)。`\\[nr]` 涵蓋 logger.sanitizeMessage 先把 raw \n/\r escape 成字面
	// `\n`/`\r` 後、絕對路徑緊接其後的情形(否則多行 error 第二行的路徑會漏脫敏)。
	`(^|[\s"'(=]|\\[nr])` +
		`(?:` +
		// POSIX 絕對路徑目錄段;元素排除 \s/:"'(避免吃掉相鄰 token、閉引號、
		// recover.go:42 的行號),basename 不被消費而保留
		`/(?:[^\s/:"']+(?: [^\s/:"']+)*/)+` +
		// Windows drive-letter(`C:\...` 或 `C:/...`)— case-insensitive
		`|[A-Za-z]:[\\/](?:[^\s:"'\\/]+[\\/])+` +
		// UNC(`\\server\share\...`)
		`|\\\\[^\s\\]+(?:\\[^\s\\]+)+\\` +
		`)`,
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
	redacted := pathRedactPattern.ReplaceAllString(s, "${1}<redacted-path>/")

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

// RedactForMessage 對 error 文字做 path-redact 處理後回傳 string,專供 handler
// 把 err 訊息塞給前端前用。nil error 回空字串,讓 caller 可直接:
//
//	result.Message = redact.RedactForMessage(err)
//
// 不必先 nil-check。
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
