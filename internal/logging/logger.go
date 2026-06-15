// Package logging provides structured logging functionality with support for
// JSON and text output formats, sensitive data masking, and context-aware logging.
package logging

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"count_mean/internal/errors"
	"count_mean/internal/security/fsperm"
	"count_mean/internal/security/redact"
)

// LogLevel represents the severity level of a log entry.
type LogLevel int

// Log level constants define the severity levels for logging.
const (
	LevelDebug LogLevel = iota // LevelDebug represents debug-level logging
	LevelInfo                  // LevelInfo represents info-level logging
	LevelWarn                  // LevelWarn represents warning-level logging
	LevelError                 // LevelError represents error-level logging
	LevelFatal                 // LevelFatal represents fatal-level logging
)

// Masking thresholds.
const (
	minMaskLength    = 4 // Minimum length before masking
	mediumMaskLength = 8 // Threshold for medium-length masking
)

// String returns the string representation of the log level.
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// LogEntry represents a structured log entry.
type LogEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Module    string         `json:"module,omitempty"`
	Function  string         `json:"function,omitempty"`
	File      string         `json:"file,omitempty"`
	Line      int            `json:"line,omitempty"`
	Error     string         `json:"error,omitempty"`
	Context   map[string]any `json:"context,omitempty"`
}

// Logger provides structured logging functionality.
type Logger struct {
	level             LogLevel
	output            io.Writer
	jsonFormat        bool
	module            string
	contextData       map[string]any
	sensitivePatterns []*regexp.Regexp
}

// NewLogger creates a new logger instance.
func NewLogger(level LogLevel, output io.Writer, jsonFormat bool) *Logger {
	return &Logger{
		level:             level,
		output:            output,
		jsonFormat:        jsonFormat,
		contextData:       make(map[string]any),
		sensitivePatterns: initSensitivePatterns(),
	}
}

// NewFileLogger creates a logger that writes to a file with size-based rotation.
//
// 原版直接 os.OpenFile 並 append-only，長跑會塞爆磁碟。改用
// SizeRotatingWriter（預設 100MB rotate、保留 7 個 backup），與 logger 同生命週期。
// fsperm.AppendFlags 在 unix 加 O_NOFOLLOW：若 attacker 將 logDir/app.log 換為
// symlink 指向 cron 配置 / SSH authorized_keys，open 會以 ELOOP fail 而非靜默寫入。
//
// Logger.Close() 會關閉這個 SizeRotatingWriter — caller (main / gui shutdown)
// 透過 Logger.Close 釋放 file handle 而不必直接操作 writer。
func NewFileLogger(level LogLevel, logDir, filename string, jsonFormat bool) (*Logger, error) {
	if err := os.MkdirAll(logDir, fsperm.DirPerm); err != nil {
		return nil, fmt.Errorf("cannot create log directory: %w", err)
	}

	logPath := filepath.Join(logDir, filename)

	writer, err := NewSizeRotatingWriter(logPath, DefaultMaxLogSizeMB, DefaultMaxBackups)
	if err != nil {
		return nil, fmt.Errorf("cannot create rotating log writer: %w", err)
	}

	return NewLogger(level, writer, jsonFormat), nil
}

// Close 關閉 logger 底層的 writer (若為 io.Closer)。返回 nil 表示 writer 沒實作
// Closer 介面 (例如 bytes.Buffer / os.Stderr) — 那種情況 caller 也不需要 cleanup。
//
// Logger 過去無 Close,SizeRotatingWriter 持有的 *os.File 永遠不被關。
// 對長跑 process 風險低 (OS 退出時自動釋放),但對「啟動 / 關閉 logger 多次」
// 的情境 (例如 GUI 內 settings 改 logDir 重啟 logger,或 test 內 multiple init)
// 會造成 fd leak。暴露 Close 後 main / gui shutdown hook 可顯式釋放。
//
// **不負責關 InitLogger 用的 MultiWriter(stdout)**:InitLogger 把 fileLogger.output
// 與 os.Stdout 包成 MultiWriter,從這層只能取到 MultiWriter (非 Closer)。要關底層
// rotation file 必須走 Logger 自己持有的 output;但 default logger 是 MultiWriter
// 而非 fileLogger。因此 main 的 shutdown hook 應同時 hold 住 fileLogger reference
// 並對它呼叫 Close,而不只是對 default logger 呼叫 Close。
//
// 設計討論: 把 fileLogger 從 InitLogger 暴露出來,讓 caller 直接 close;或讓 Logger
// 持有 underlying file writer 的 reference 並提供「Close 鏈式委派」。目前選後者
// (簡單) — Logger.Close 對 io.Closer 委派,nil 友善。caller 確保把 fileLogger 而非
// MultiWriter logger 傳給 Close。
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}

	closer, ok := l.output.(io.Closer)
	if !ok {
		return nil
	}

	if err := closer.Close(); err != nil {
		return fmt.Errorf("logger close: %w", err)
	}
	return nil
}

// WithModule returns a logger with a specific module context.
//
// sensitivePatterns 採 copy-on-write — child logger 拿到的是 slice
// header 的 shallow copy（指向同樣的 *regexp.Regexp，這些 compiled regex
// 本身 immutable），因此若未來某個 child 透過 append 加入新的 pattern，不
// 會踩到「擴容導致 parent 與 child 共用 backing array、再 append 又分裂」
// 的 ABA race。當前實作所有 logger 都 share initSensitivePatterns 的同一份
// slice，但這道防護讓未來引入「per-logger custom pattern」時安全。
func (l *Logger) WithModule(module string) *Logger {
	newLogger := *l
	newLogger.module = module
	newLogger.contextData = make(map[string]any)

	for k, v := range l.contextData {
		newLogger.contextData[k] = v
	}

	newLogger.sensitivePatterns = copySensitivePatterns(l.sensitivePatterns)

	return &newLogger
}

// WithContext adds context data to the logger.
//
// 見 WithModule 的 COW 註解 — sensitivePatterns 同樣以 shallow copy
// 隔離 parent / child。
func (l *Logger) WithContext(key string, value any) *Logger {
	newLogger := *l
	newLogger.contextData = make(map[string]any)

	for k, v := range l.contextData {
		newLogger.contextData[k] = v
	}

	newLogger.contextData[key] = value
	newLogger.sensitivePatterns = copySensitivePatterns(l.sensitivePatterns)

	return &newLogger
}

// copySensitivePatterns returns a shallow copy of the slice header so child
// loggers do not share backing storage with their parent. The *regexp.Regexp
// elements themselves remain shared — they are immutable after compilation
// and safe for concurrent reuse.
func copySensitivePatterns(src []*regexp.Regexp) []*regexp.Regexp {
	if src == nil {
		return nil
	}

	out := make([]*regexp.Regexp, len(src))
	copy(out, src)

	return out
}

// initSensitivePatterns initializes patterns for sensitive data detection.
func initSensitivePatterns() []*regexp.Regexp {
	patterns := []string{
		`(?i)password\s*[=:]\s*[^\s]+`,
		`(?i)passwd\s*[=:]\s*[^\s]+`,
		`(?i)secret\s*[=:]\s*[^\s]+`,
		`(?i)token\s*[=:]\s*[^\s]+`,
		`(?i)key\s*[=:]\s*[^\s]+`,
		`(?i)auth\s*[=:]\s*[^\s]+`,
		`(?i)credential\s*[=:]\s*[^\s]+`,
		`(?i)api[-_]?key\s*[=:]\s*[^\s]+`,
		`(?i)access[-_]?token\s*[=:]\s*[^\s]+`,
		`(?i)refresh[-_]?token\s*[=:]\s*[^\s]+`,
		`(?i)bearer\s+[a-zA-Z0-9\-_\.]+`,
		`(?i)connection[-_]?string\s*[=:]\s*[^\s]+`,
		`(?i)database[-_]?url\s*[=:]\s*[^\s]+`,
		`(?i)db[-_]?password\s*[=:]\s*[^\s]+`,
		`\b\d{4}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}\b`,
		`\b\d{3}-\d{2}-\d{4}\b`,
		// 原本是 [A-Z|a-z]{2,} — 字元類內字面 | 字元造成
		// "foo@bar.|baz" 等帶字面 pipe 的非 email 被誤遮(false positive
		// 馬賽克使用者面向訊息)。改為 [a-zA-Z]{2,} 嚴格只接受 alpha
		// TLD。仍可能誤判 build@v1.beta、cci@phase.start 這類 email 形
		// 狀的識別字,但「字面 alpha-only TLD」是必要(且不過度)的緊縮 —
		// false negative(漏掉不常見 TLD)優於 false positive(亂遮使用者文字)。
		`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[a-zA-Z]{2,}\b`,
		`\b(?:\d{1,3}\.){3}\d{1,3}\b`,
		`(?i)[\\/].*(?:password|secret|key|token|credential).*[\\/]`,
		`(?i)"[^"]*(?:password|secret|key|token|credential)[^"]*"`,
		`(?i)'[^']*(?:password|secret|key|token|credential)[^']*'`,
		`(?i)\btoken\b[^=:]*[=:][^,}\s]+`,
		`(?i)\bbearer_token[^=:]*[=:][^,}\s]+`,

		// 擴展 PII redact — EMG 醫療場景常見的非 keyword PII。
		//
		// IPv6:8 組 hex 用 `:` 分隔。要求完整 8 組(`{7}:` + 結尾 hex)
		// 避免誤抓 MAC 等。Case-insensitive 涵蓋 lower/upper hex。
		// 不涵蓋 IPv6 縮寫 `::` — 縮寫形式 false positive 風險高 (誤抓
		// "fe::" type 字串),保守只擋完整格式。
		`(?i)\b(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}\b`,

		// MAC 地址:6 組 hex byte 用 `:` 分隔。`-` 分隔形式 (Cisco / Windows
		// adapter ID) 額外加另一條 pattern。
		`\b(?:[0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}\b`,

		// JWT:三段 base64url(`.` 分隔),header 必以 `eyJ` 開頭(base64
		// 編碼後的 `{"`)。`[\w-]+` 涵蓋 base64url 字符集(alnum + `-` + `_`)。
		`eyJ[\w-]+\.[\w-]+\.[\w-]+`,
	}

	var compiledPatterns []*regexp.Regexp

	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil {
			compiledPatterns = append(compiledPatterns, re)
		}
	}

	return compiledPatterns
}

// newlineEscaper escapes raw CR / LF in user-controlled strings to their literal
// backslash-escape form. This is the bottom line defense against log-injection
// attacks: in text format the per-entry separator is a single trailing
// "\n" written by writeText / writeJSON; if a message / error / context value
// carries its own "\n" or "\r" an attacker can forge entire fake log lines
// (e.g. fake [FATAL] / [ERROR] entries). escape — not strip — to preserve the
// original content for forensics. Order matters: replace "\r" before "\n" so
// that a literal CRLF becomes "\r\n" (two escape sequences) rather than
// "\r" followed by something already substituted.
//
//nolint:gochecknoglobals // immutable replacer reused across logger instances
var newlineEscaper = strings.NewReplacer("\r", `\r`, "\n", `\n`)

// sanitizeMessage removes sensitive information and log-injection vectors from
// log messages. 在 sensitive-pattern masking 之前先把 raw \r / \n escape
// 成字面 \\r / \\n,確保 writeText 不會被 user-controlled 字串(filename / error
// msg / dynamic context)注入偽 log line。writeJSON 路徑 stdlib encoding/json 雖
// 已會 escape,但同樣走這條 sanitize path,維持 JSON 與 text 的內容一致並避免
// 後續 path 變更時 regress。
//
// 額外串接 redact.Paths() — 把 absolute path PII (user home / pCloud
// mount / Windows drive-letter / UNC) 換成 `<redacted-path>/`。reuse
// internal/security/redact 的 single source of truth pattern,避免 logger 內
// 自己維護一份重複正則。Path redact 在 keyword-mask 之前跑,因為 path 含的
// 子字串可能被 keyword pattern 誤判 (e.g. "/home/secret-key/" 會被「key=」
// 規則 mask 切碎)。
func (l *Logger) sanitizeMessage(message string) string {
	sanitized := newlineEscaper.Replace(message)

	// 先過 path redact — pathRedactPattern 比 keyword pattern 精準
	// (只切 known system-root prefix + 後續 path 元素),先跑保留更多上下文
	// 給 keyword pattern 處理。
	sanitized = redact.Paths(sanitized)

	if l.sensitivePatterns == nil {
		return sanitized
	}

	for _, pattern := range l.sensitivePatterns {
		sanitized = pattern.ReplaceAllStringFunc(sanitized, maskSensitiveData)
	}

	return sanitized
}

// sanitizeContextValue removes sensitive information from context values.
func (l *Logger) sanitizeContextValue(value any) any {
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case string:
		return l.sanitizeMessage(v)
	case map[string]any:
		sanitized := make(map[string]any)
		for k, val := range v {
			sanitized[k] = l.sanitizeContextValue(val)
		}

		return sanitized
	case []any:
		sanitized := make([]any, len(v))
		for i, val := range v {
			sanitized[i] = l.sanitizeContextValue(val)
		}

		return sanitized
	default:
		return l.sanitizeMessage(fmt.Sprintf("%v", value))
	}
}

// maskSensitiveData masks sensitive data with asterisks.
func maskSensitiveData(data string) string {
	if len(data) <= minMaskLength {
		return "****"
	}

	parts := strings.Split(data, "=")
	if len(parts) == 2 {
		key := parts[0]
		value := parts[1]

		if len(value) <= minMaskLength {
			return key + "=****"
		}

		if len(value) > mediumMaskLength {
			return key + "=" + value[:2] + "****" + value[len(value)-2:]
		}

		return key + "=" + value[:1] + "****"
	}

	if len(data) > mediumMaskLength {
		return data[:2] + "****" + data[len(data)-2:]
	}

	return data[:1] + "****"
}

// WithError adds error context to the logger.
func (l *Logger) WithError(err error) *Logger {
	return l.WithContext("error", err.Error())
}

// logImpl 是所有 log level 的共用實作。skip 由各入口依呼叫深度傳入
// (直接方法與包級 wrapper 繞過方法後都是 3),用於 addCallerInfo 精準定位呼叫端。
// context 為 variadic,在此統一取 context[0](方法與 wrapper 各自把自己的可變參數
// 透傳進來,省去逐處重複的 ctx 提取)。
func (l *Logger) logImpl(skip int, level LogLevel, message string, err error, context ...map[string]any) {
	if level < l.level {
		return
	}

	var ctx map[string]any
	if len(context) > 0 {
		ctx = context[0]
	}

	entry := LogEntry{
		Timestamp: time.Now(),
		Level:     level.String(),
		Message:   l.sanitizeMessage(message),
		Module:    l.module,
		Context:   make(map[string]any),
	}

	addCallerInfo(&entry, skip)
	l.addErrorInfo(&entry, err)

	for k, v := range l.contextData {
		entry.Context[k] = l.sanitizeContextValue(v)
	}

	for k, v := range ctx {
		entry.Context[k] = l.sanitizeContextValue(v)
	}

	if l.jsonFormat {
		l.writeJSON(&entry)
	} else {
		l.writeText(&entry)
	}
}

// addCallerInfo adds caller information to the log entry.
// skip 是傳給 runtime.Caller 的呼叫深度(由 logImpl 依呼叫路徑決定)。
func addCallerInfo(entry *LogEntry, skip int) {
	if pc, file, line, ok := runtime.Caller(skip); ok {
		entry.File = filepath.Base(file)
		entry.Line = line

		if fn := runtime.FuncForPC(pc); fn != nil {
			entry.Function = filepath.Base(fn.Name())
		}
	}
}

// addErrorInfo adds error information to the log entry.
func (l *Logger) addErrorInfo(entry *LogEntry, err error) {
	if err == nil {
		return
	}

	entry.Error = l.sanitizeMessage(err.Error())

	if appErr, ok := stderrors.AsType[*errors.AppError](err); ok {
		entry.Context["error_code"] = appErr.Code
		entry.Context["recoverable"] = appErr.Recoverable

		if appErr.Context != nil {
			for k, v := range appErr.Context {
				entry.Context[k] = l.sanitizeContextValue(v)
			}
		}
	}
}

// writeJSON writes the log entry in JSON format.
//
// defense-in-depth sentinel fallback。
//
// 雖然 sanitizeContextValue 已處理 NaN/Inf/cyclic 等 json.UnsupportedValueError
// 觸發點,但仍有縱深破口:
//  1. context value 含 custom type with Marshaler 介面,Marshaler 內部產 NaN/Inf
//  2. 第三方套件直接塞 *big.Float 等不支援 marshal 的型別
//  3. context value 是 chan / func 等 unsupported kind
//
// 過去版本對 marshal error 走 writeText fallback — 但 writeText 用 %v 輸出,
// 對 NaN 會印字面 "NaN",對 cyclic 直接 stack overflow,且輸出格式不再是 JSON
// (caller 依賴 jsonFormat=true 時固定收 JSON line 做下游 parse,fallback 到
// text 會破壞 grep / log shipper pipeline)。
//
// 改成:marshal 失敗時,輸出固定格式的 sentinel JSON line(error level + 帶
// 原因標記),讓下游 parser 仍能拿到合法 JSON 而不必容錯。原 entry 寫不出去
// 視為 sampling loss,優於把 pipeline 撞壞或留下 invalid data。
//
//nolint:errcheck,revive // log output write errors are intentionally ignored
func (l *Logger) writeJSON(entry *LogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		// 用 fixed-key minimal JSON,確保 sentinel 本身 marshal 永遠成功
		// (string literal + 一個 reason string,無 nested type 沒有 marshal 失敗風險)。
		// reason 仍走 sanitizeMessage 避免 err.Error() 內含 control char 破壞 JSON line。
		sentinel := struct {
			Level  string `json:"level"`
			Msg    string `json:"msg"`
			Reason string `json:"reason"`
		}{
			Level:  "error",
			Msg:    "<encode-failed>",
			Reason: l.sanitizeMessage(err.Error()),
		}
		// sentinel marshal 應該永遠成功 (3 個 string field,無 NaN/cyclic 可能)。
		// 萬一仍失敗 (極端 OOM 等場景),退回 hard-coded raw bytes 至少保 JSON line shape。
		if sentinelData, sentinelErr := json.Marshal(sentinel); sentinelErr == nil {
			fmt.Fprintf(l.output, "%s\n", sentinelData)
		} else {
			fmt.Fprintln(l.output, `{"level":"error","msg":"<encode-failed>","reason":"sentinel-marshal-also-failed"}`)
		}
		return
	}

	fmt.Fprintf(l.output, "%s\n", data)
}

// writeTextBuilderPool pool 化 strings.Builder for writeText hot path。
//
// 原本 writeText 用 []string + 多次 fmt.Sprintf + strings.Join,實測每筆 log entry
// 觸發 5-8 個 allocations(每個 Sprintf 一次,Join 一次,context map iter 視 K 數)。
// hot logging path (per EMG channel callback、per parse warn) 每秒可能跑 10k+ entries,
// 累積 GC pressure 不小。
//
// 改用 sync.Pool 重用 strings.Builder:每 entry 從 pool 取一根 builder,寫完
// fmt.Fprintf 後 Reset+Put 回 pool。穩態下 hot path allocations 從 5-8 降到 0-1
// (僅 context map iteration 的 Sprintf 仍 alloc,但已是最內層 unavoidable 部分)。
//
//nolint:gochecknoglobals // immutable pool reused across logger instances
var writeTextBuilderPool = sync.Pool{
	New: func() any {
		b := &strings.Builder{}
		b.Grow(256) // typical log line size — avoid first-write expand
		return b
	},
}

// writeText writes the log entry in human-readable text format.
//
// 改用 strings.Builder + sync.Pool 取代舊版 []string + Sprintf + Join,
// hot path allocations 從 5-8/entry 降到 0-1/entry(穩態下,context iter 仍 alloc)。
// 行為與舊版完全一致:相同 separator(空格)、相同 prefix bracket、相同 newline 結尾、
// 相同 sanitize 路徑。對既有 test contract 透明。
//
//nolint:errcheck,revive // log output write errors are intentionally ignored
func (l *Logger) writeText(entry *LogEntry) {
	b, ok := writeTextBuilderPool.Get().(*strings.Builder)
	if !ok || b == nil {
		// pool New 永遠回 *strings.Builder;此 fallback 為 defensive。
		b = &strings.Builder{}
		b.Grow(256)
	}
	defer func() {
		b.Reset()
		writeTextBuilderPool.Put(b)
	}()

	b.WriteString(entry.Timestamp.Format("2006-01-02 15:04:05"))
	b.WriteString(" [")
	b.WriteString(entry.Level)
	b.WriteByte(']')

	if entry.Module != "" {
		b.WriteString(" [")
		b.WriteString(entry.Module)
		b.WriteByte(']')
	}

	b.WriteByte(' ')
	b.WriteString(entry.Message)

	if entry.File != "" && entry.Line > 0 {
		b.WriteString(" (")
		b.WriteString(entry.File)
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(entry.Line))
		b.WriteByte(')')
	}

	if entry.Error != "" {
		b.WriteString(" error=")
		b.WriteString(entry.Error)
	}

	if len(entry.Context) > 0 {
		b.WriteString(" context=[")
		first := true
		for k, v := range entry.Context {
			if !first {
				b.WriteByte(' ')
			}
			first = false
			// fmt.Sprintf 仍是 K-loop 內最 readable 的 path;若 v 是 string 等
			// 簡單型別未來可進一步 fast-path。sanitize 必須走 — context value
			// 可能含 raw \r\n。
			b.WriteString(l.sanitizeMessage(fmt.Sprintf("%s=%v", k, v)))
		}
		b.WriteByte(']')
	}

	b.WriteByte('\n')

	// 用 io.WriteString 而非 Fprintf 避免額外 Sprintf alloc。
	_, _ = io.WriteString(l.output, b.String())
}

// Debug logs a debug message.
func (l *Logger) Debug(message string, context ...map[string]any) {
	l.logImpl(3, LevelDebug, message, nil, context...)
}

// Info logs an info message.
func (l *Logger) Info(message string, context ...map[string]any) {
	l.logImpl(3, LevelInfo, message, nil, context...)
}

// Warn logs a warning message.
func (l *Logger) Warn(message string, context ...map[string]any) {
	l.logImpl(3, LevelWarn, message, nil, context...)
}

// Error logs an error message.
func (l *Logger) Error(message string, err error, context ...map[string]any) {
	l.logImpl(3, LevelError, message, err, context...)
}

// exitFunc is the function used to exit the process when Fatal is called.
// Tests can override this to capture exit calls without terminating the test runner.
//
//nolint:gochecknoglobals // intentional indirection for testability
var exitFunc = os.Exit

// syncer 是「能對底層 fd 觸發 fsync」的最小介面。*os.File / *SizeRotatingWriter
// 都實作 Sync() error,Fatal flush 用 interface assertion 偵測即可,不必綁定具體
// 型別。bytes.Buffer / io.MultiWriter 等 in-memory / composite writer 無 Sync
// 介面,assertion 失敗即 skip(no-op fallback)— 對它們而言 flush 也沒實際意義。
type syncer interface {
	Sync() error
}

// flusher 是「能 flush buffered output」的最小介面。stdlib *bufio.Writer 實作 Flush()。
// 與 syncer 並列,讓 Fatal 同時 drain buffered + sync 到 disk(不同 layer 都覆蓋)。
type flusher interface {
	Flush() error
}

// Fatal logs a fatal message and exits.
//
// 透過 exitFunc 注入點允許 test override，不再硬綁 os.Exit。
// 生產環境行為不變（exitFunc 預設為 os.Exit），測試可改寫 exitFunc 捕捉
// Fatal 的觸發次數與 status code，避免 testing harness 直接被殺。
//
// # Flush before exit
//
// os.Exit 不執行任何 deferred function、不 close fd、kernel page cache 不 sync —
// 對 file-backed logger,最後一筆 Fatal log 可能還停留在 page cache 就被截斷。
// 對 audit / crash forensics 場景(EMG 醫療資料 + 系統 panic),最關鍵的 Fatal
// 訊息丟失是不可接受的。
//
// 解法:exitFunc 之前對 l.output 嘗試 Flush + Sync。寫入順序:
//
//  1. l.logImpl(3, LevelFatal, ...) — fmt.Fprintf 到 output(io.Writer)
//  2. flusher.Flush() — drain *bufio.Writer / composite buffered writer
//  3. syncer.Sync() — 對 *os.File 觸發 fsync,page cache → disk
//  4. exitFunc(1) — 不可逆退出
//
// MultiWriter / nopCloser 等不實作 syncer/flusher 的 output 是 no-op fallback,
// 對它們 sync 也沒實際意義(stdout / stderr 由 OS 在 process exit 自己 flush)。
// Sync/Flush 任何失敗都 ignore — Fatal 必須抵達 exit,不能因 IO 失敗 hang。
func (l *Logger) Fatal(message string, err error, context ...map[string]any) {
	l.logImpl(3, LevelFatal, message, err, context...)
	l.flushAndExit()
}

// flushAndExit 在 Fatal 之後執行:drain buffered writer + sync to disk,再 exitFunc 退出。
// 抽出共用,讓 (*Logger).Fatal 與包級 Fatal 都能在 logImpl(skip=3) 之後走相同退出路徑。
//
// os.Exit 不執行 deferred、不 close fd、page cache 不 sync — file-backed logger 的最後
// 一筆 Fatal log 可能停在 page cache 就被截斷。順序:flusher 在前(buffered→underlying)、
// syncer 在後(underlying→fsync)。MultiWriter / stdout 等不實作 syncer/flusher 為 no-op
// fallback。Sync/Flush 任何失敗都 ignore — Fatal 必須抵達 exit,不能因 IO 失敗 hang。
func (l *Logger) flushAndExit() {
	if f, ok := l.output.(flusher); ok {
		_ = f.Flush() //nolint:errcheck // best-effort drain; Fatal must reach exitFunc
	}
	if s, ok := l.output.(syncer); ok {
		_ = s.Sync() //nolint:errcheck // best-effort fsync; Fatal must reach exitFunc
	}

	//nolint:revive // deep-exit is intentional for Fatal level logging
	exitFunc(1)
}

// Default logger instance + initialization guards.
//
// 原本用 `sync.Once + sync.RWMutex` 兩階段組合,InitLogger 與 GetLogger
// 之間有 race window — Once.Do 內部記錄「已執行」是在 callback 結束時,而
// InitLogger 先 Lock/Unlock mutex 再呼叫 Once.Do(noop) 通知 Once 跳過 lazy init,
// 中間有空檔讓並發的 GetLogger 觀察到「Once 還未執行 → 走 lazy init」並寫
// 一個 stderr default logger 蓋過 InitLogger 的 file logger。
//
// 改成 atomic.Bool initialized 旗標:
//   - InitLogger 先寫 defaultLogger,再 store(true) initialized
//   - GetLogger 先 load(initialized),false 才走 once.Do lazy fallback
//   - lazy fallback 內部用 mutex 雙重檢查避免重複建構
//
//nolint:gochecknoglobals // Global logger instance is intentional for convenience
var (
	defaultLogger     *Logger
	defaultLoggerOnce sync.Once
	defaultLoggerMu   sync.RWMutex
	defaultLoggerInit atomic.Bool

	// InitLogger 把 fileLogger 包成 MultiWriter,wrapping 後 caller 拿不到
	// 底層 *os.File 的 Closer。把 fileLogger 另外存一份,讓 ShutdownLogger 可釋放
	// rotation file handle。
	defaultFileLogger *Logger
)

// logFilename 是 InitLogger 固定使用的 log 檔名;抽成常數讓 re-init 的
// same-path 判定 (resolveLogAbsPath) 與 NewFileLogger 用同一個來源。
const logFilename = "app.log"

// resolveLogAbsPath 把 logDir 解析成 InitLogger 實際開檔的 absolute path
// (logDir/app.log),與 NewSizeRotatingWriter 的 registry key 同源。 re-init
// 時用它判斷新舊 logger 是否指向「同一個 log 檔」決定走 reuse 或 reopen 分流。
func resolveLogAbsPath(logDir string) (string, error) {
	abs, err := filepath.Abs(filepath.Join(logDir, logFilename))
	if err != nil {
		return "", fmt.Errorf("無法解析 log file 絕對路徑: %w", err)
	}
	return abs, nil
}

// InitLogger initializes the default logger.
//
// race 修補 — initialized atomic.Bool 在 mutex 解鎖後才 set,確保並發
// GetLogger 看到 initialized=true 時 defaultLogger 已就緒。
//
// re-init 分流 (失敗時一律不動舊 state,舊 logger 保持可用):
//
//   - **同一 logDir/app.log**: registry 在開檔前就會擋同 path 的第二個 writer
//     (ErrLogPathAlreadyOpen,只有 Close 解註冊),所以「先建新再關舊」對同 dir
//     行不通。 改為 reuse 既有 *SizeRotatingWriter,只把 level/format/stdout 包裝層
//     republish — 不重開檔、不 close 舊 writer。
//   - **不同 logDir**: 先 NewFileLogger 開新檔成功、publish 新 state,再 Close 舊的
//     釋放 fd + registry。 新檔開失敗則 return error,defaultLogger / defaultFileLogger
//     維持原樣,舊 logger 仍可寫。
func InitLogger(level LogLevel, logDir string, jsonFormat bool) error {
	// 先抓舊 fileLogger reference (在 mutex 下取一份 snapshot)。 用 RLock 即可,
	// publish 階段才升 Lock。
	defaultLoggerMu.RLock()
	oldFileLogger := defaultFileLogger
	defaultLoggerMu.RUnlock()

	// 同 path 判定:新 logDir/app.log 與舊 writer 的 absPath 相同 → reuse 分流。
	if oldFileLogger != nil {
		newAbsPath, err := resolveLogAbsPath(logDir)
		if err != nil {
			return err
		}
		if oldWriter, ok := oldFileLogger.output.(*SizeRotatingWriter); ok && oldWriter.absPath == newAbsPath {
			return reinitSameLogPath(level, oldWriter, jsonFormat)
		}
	}

	// 不同 logDir (或首次 init):先建新成功才 publish,失敗則完全不動舊 state。
	fileLogger, err := NewFileLogger(level, logDir, logFilename, jsonFormat)
	if err != nil {
		return err
	}

	multiWriter := io.MultiWriter(fileLogger.output, os.Stdout)

	defaultLoggerMu.Lock()
	defaultLogger = NewLogger(level, multiWriter, jsonFormat)
	defaultFileLogger = fileLogger
	defaultLoggerMu.Unlock()

	// publish 完整 state 後才 set initialized,讓 GetLogger 並發看到 true 必然代表
	// defaultLogger 可用 (對齊 "publish before flag" pattern)。
	defaultLoggerInit.Store(true)

	// 對 sync.Once 額外 mark "已執行",避免後續 lazy fallback 路徑再次嘗試初始化。
	defaultLoggerOnce.Do(func() {})

	// 新 state 已 publish,才釋放舊 fd + registry 佔位。 close 失敗只 stderr log,
	// 不 fail init — 新 logger 已就緒可用。
	if oldFileLogger != nil {
		if err := oldFileLogger.Close(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "InitLogger: close old file logger failed (non-fatal): %v\n", err)
		}
	}

	return nil
}

// reinitSameLogPath 處理「re-init 指向同一個 log 檔」的分流:不重開檔(registry
// 會擋同 path 第二個 writer)、不 close 舊 writer,只重用既有 *SizeRotatingWriter
// 並 republish level/format/stdout 包裝層。 defaultFileLogger 換成包同一 writer
// 的新 Logger,讓後續 ShutdownLogger 仍 close 這唯一的 writer。
func reinitSameLogPath(level LogLevel, writer *SizeRotatingWriter, jsonFormat bool) error {
	multiWriter := io.MultiWriter(writer, os.Stdout)

	defaultLoggerMu.Lock()
	defaultLogger = NewLogger(level, multiWriter, jsonFormat)
	defaultFileLogger = NewLogger(level, writer, jsonFormat)
	defaultLoggerMu.Unlock()

	defaultLoggerInit.Store(true)
	defaultLoggerOnce.Do(func() {})

	return nil
}

// ShutdownLogger 釋放 InitLogger 開啟的 file logger 資源。
// 對 default logger 從 stderr / non-Closer writer (lazy fallback init 路徑) 是 no-op。
// main / gui shutdown hook 應該呼叫此函式,避免 file handle leak。
//
// idempotent: 多次 ShutdownLogger 不 error,因為 SizeRotatingWriter.Close 自身 idempotent。
//
// 過去只 nil 掉 defaultFileLogger,但 defaultLogger 仍指向 MultiWriter(closedFile, stdout)
// — 後續 GetLogger().Info(...) 雖然不會 panic (writeJSON nolint errcheck),但底層 fmt.Fprintf
// 會對 closed fd 寫入並產生 sysErr("file already closed")。修補後同時把 defaultLogger 切到
// stdout-only Logger,讓 post-shutdown logging 仍然 functional 而非寫入死掉的 multiwriter。
// defaultLoggerInit 保持 true — GetLogger fast path 繼續走,不會 fallback 到 stderr lazy init。
func ShutdownLogger() error {
	defaultLoggerMu.Lock()
	fl := defaultFileLogger
	defaultFileLogger = nil
	// 把 defaultLogger 切到 stdout-only — Closer-friendly 且不會踩 closed file。
	// 即使 fl == nil (重複 shutdown) 也安全:lazy init 路徑會建 stderr logger,被覆蓋
	// 成 stdout 是 idempotent 無害的(post-shutdown 本來就只是 best-effort log)。
	if fl != nil {
		defaultLogger = NewLogger(LevelInfo, os.Stdout, false)
	}
	defaultLoggerMu.Unlock()

	if fl == nil {
		return nil
	}
	return fl.Close()
}

// GetLogger returns the default logger with optional module context.
//
// 並發安全:用 atomic.Bool initialized + mutex 雙保險,取代 sync.Once 與
// RWMutex 的鬆耦合組合。InitLogger 與 GetLogger 之間的 race window 消除原理:
//
//  1. InitLogger 先 publish defaultLogger,再 Store(true) initialized
//  2. GetLogger 先 Load initialized — true 時讀 defaultLogger 必然 non-nil
//  3. initialized=false 才進 once.Do (lazy fallback) 路徑,內部仍以 mutex 雙重檢查
//
// 為什麼保留 sync.Once: 用 atomic 旗標 + mutex 也可實作,但 sync.Once 更直白地
// 表達「lazy init 只跑一次」的契約。Once + atomic 雙保險不衝突 — atomic 主要
// 解決「InitLogger 半完成被 GetLogger 觀察到」的 race,Once 解決「並發 lazy init
// 重複建構」的 race。
func GetLogger(module ...string) *Logger {
	// Fast path:InitLogger 已跑完 → 直接讀 defaultLogger,免進 Once.Do/Mutex。
	if defaultLoggerInit.Load() {
		defaultLoggerMu.RLock()
		logger := defaultLogger
		defaultLoggerMu.RUnlock()

		if len(module) > 0 {
			return logger.WithModule(module[0])
		}
		return logger
	}

	// Slow path:lazy fallback init。Once.Do 保證 callback 並發只跑一次,
	// mutex 在 callback 內部仍需要,因為 callback 結束後 atomic.Store 與
	// 後續 read 之間若沒 mutex 也能看到一致快照 (atomic.Bool happens-before 全部讀)。
	defaultLoggerOnce.Do(func() {
		defaultLoggerMu.Lock()
		defer defaultLoggerMu.Unlock()
		if defaultLogger == nil {
			defaultLogger = NewLogger(LevelInfo, os.Stderr, false)
		}
		// 把 initialized 標起來 — 後續 GetLogger 走 fast path。
		defaultLoggerInit.Store(true)
	})

	defaultLoggerMu.RLock()
	logger := defaultLogger
	defaultLoggerMu.RUnlock()

	if len(module) > 0 {
		return logger.WithModule(module[0])
	}

	return logger
}

// Debug logs a debug message using the default logger.
func Debug(message string, context ...map[string]any) {
	GetLogger().logImpl(3, LevelDebug, message, nil, context...)
}

// Info logs an info message using the default logger.
func Info(message string, context ...map[string]any) {
	GetLogger().logImpl(3, LevelInfo, message, nil, context...)
}

// Warn logs a warning message using the default logger.
func Warn(message string, context ...map[string]any) {
	GetLogger().logImpl(3, LevelWarn, message, nil, context...)
}

// Error logs an error message using the default logger.
func Error(message string, err error, context ...map[string]any) {
	GetLogger().logImpl(3, LevelError, message, err, context...)
}

// Fatal logs a fatal message using the default logger and exits.
func Fatal(message string, err error, context ...map[string]any) {
	l := GetLogger()
	l.logImpl(3, LevelFatal, message, err, context...)
	l.flushAndExit()
}
