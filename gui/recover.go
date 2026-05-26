package gui

import (
	"errors"
	"fmt"
	"runtime/debug"

	"github.com/google/uuid"

	"count_mean/internal/logging"
	"count_mean/internal/security/redact"
)

// ErrInternalPanic 是 Wails handler 在 goroutine 內 panic 時轉成的對外 error。
// 對 frontend 不洩漏 stack（只 log），避免攻擊面擴大。
var ErrInternalPanic = errors.New("內部錯誤，請聯絡技術支援（無需重啟）")

// panicErrWithChain 是 recoverHandlerPanic 的回傳 error type,兼顧兩條看似
// 矛盾的契約:
//
//  1. PII redaction:returned err 的 .Error() 文字**絕不可**含 caller 的 ok-path
//     err 字面(那條訊息常帶 filesystem path 等 PII,直送 webview 會洩漏);
//     只能含 ErrInternalPanic 包裝後的 redacted 訊息。
//  2. Chain preservation:errors.Is(returned, callerOkPathSentinel) 必須回 true —
//     上游 caller 若依賴 errors.Is 偵測特定 ok-path sentinel(例如 ErrNotFound)
//     做後續處理,直接覆寫會導致該分支永遠走不到。
//
// 解法:自訂 error type 同時實作
//   - Error() string:只回 panic-wrap 訊息(redacted),完全不含 caller err 文字
//   - Unwrap() []error:同時 return panic-wrap + caller ok-path err,讓
//     errors.Is/As 可走訪兩條 chain
//
// **stdlib errors.Join 不能用**:`errors.Join(a, b).Error()` 是
// `a.Error() + "\n" + b.Error()` — 兩段字串都會出現,破壞 PII contract。
type panicErrWithChain struct {
	wrap     error // panic-wrap err(ErrInternalPanic + handler + uuid),Error() 用此
	original error // caller named-return err(ok-path 預期 err),只供 errors.Is/As 走訪
}

// Error 只回 panic-wrap 訊息 — 不含 caller original err 文字(PII contract)。
func (e *panicErrWithChain) Error() string {
	return e.wrap.Error()
}

// Unwrap 同時 return wrap + original,讓 errors.Is/As 可以走訪兩條 chain。
// stdlib errors.Is/As 對 multi-error Unwrap() []error 已原生支援(Go 1.20+)。
func (e *panicErrWithChain) Unwrap() []error {
	if e.original == nil {
		return []error{e.wrap}
	}
	return []error{e.wrap, e.original}
}

// ErrAppNotReady 是 Wails Startup 設 a.ctx 之前 frontend 已啟動並呼叫 RPC 時
// 對外回傳的 sentinel。caller 可用 errors.Is 判斷並引導使用者稍候再試。
var ErrAppNotReady = errors.New("App 尚未完成啟動，請稍候再試")

// recoverHandlerPanic 是所有 Wails RPC method 的 defer 安全網。
//
// 為何需要:Wails 把 Go method 直接 expose 為 RPC,任何 panic 會 propagate 到
// Wails runtime 並擊潰整個 desktop process — 使用者必須重啟、未存 UI state 丟失。
//
// 使用方式(named return error 必要):
//
//	func (a *App) Foo(...) (result *X, err error) {
//	    defer recoverHandlerPanic("Foo", a.logger, &err)
//	    ...
//	}
//
// 對於不回 error 的 method(如 ShowMessage / GetVersion),改用
// recoverHandlerPanicVoid(只 log 不 propagate)。
//
// # errPtr 設計意圖
//
// errPtr 在 panic 發生時:
//   - **.Error() 顯示**:覆寫為 ErrInternalPanic + handler + panic_uuid;
//     不含 caller 原本 ok-path err 字面(PII contract)
//   - **errors.Is/As chain**:同時可走訪 ErrInternalPanic-wrap 與 caller 原 err
//
// 為何雙 chain:caller 上游可能用 errors.Is(err, sentinel) 偵測 ok-path err 做
// fallback(如 errors.Is(err, ErrNotFound) 走 fallback 分支);panic 直接丟棄原
// err 會讓 fallback 永遠走不到。但對前端只能露 ErrInternalPanic(panic 是
// 不可預期內部錯誤,優先級高於 caller 預期 err)。panicErrWithChain
// (Unwrap() []error)同時保留兩條 chain 解決矛盾。
//
// # PII / Stack redaction
//
// debug.Stack() 會輸出完整 file path,內含 user-controlled 路徑(EMG 病患資料、
// pCloud 路徑等);直接寫進 Error log 等同把 PII 散播到 log file / aggregator /
// crash dump。`original_err=%v` 把 caller err 內容直送 webview 同樣會 leak
// (如 `open /Users/<name>/patient/case_xxxx/emg_raw.csv: permission denied`)。
//
//   - Stack 改寫 Debug level(預設 production log level=Info 不會輸出)
//   - Stack 內容 redactPathsInStack() 把 absolute paths 換成 `<redacted-path>`
//   - generate panic UUID 寫進 Error log + Debug stack,Debug 環境下用 UUID 對應
//   - 對外 error message 只帶 panic_uuid + handler,不帶路徑、不帶 caller err 字面
//   - 原始 err 經 redactPathsInStack 過濾後寫進 internal Error log 的
//     `original_err` field,保留 audit trail
//
// Default production 模式下,log file 只看到 panic_uuid + handler +
// original_err(redacted),前端只看到 ErrInternalPanic + handler + panic_uuid。
func recoverHandlerPanic(handlerName string, logger *logging.Logger, errPtr *error) {
	r := recover()
	if r == nil {
		return
	}

	panicUUID := newPanicUUID()

	// original err 進 internal log 前必須 redact 路徑 — caller 的 err 訊息經常帶
	// `open /Users/<name>/.../foo.csv: permission denied` 之類 PII。
	var redactedOriginal string
	if errPtr != nil && *errPtr != nil {
		redactedOriginal = redactPathsInStack((*errPtr).Error())
	}

	logPanic(logger, "handler panic recovered", handlerName, panicUUID, r, redactedOriginal)

	if errPtr != nil {
		// 對前端只回 ErrInternalPanic + handler + panic_uuid;不把 caller 原本
		// 的 err 字面塞進 wrap message(會把 PII 送進 webview)。oncall 從
		// internal Error log 用 panic_uuid 對應 redacted original_err。
		panicWrap := fmt.Errorf("%w (handler=%s, panic_uuid=%s)",
			ErrInternalPanic, handlerName, panicUUID)

		// panicErrWithChain 同時保留兩條 chain — .Error() 只渲染 panicWrap,
		// errors.Is/As 可走訪原 errPtr 與 panicWrap。原 errPtr 可能是 nil
		// (常見 happy-path),Unwrap() 對 nil original 退化為單元素 chain。
		*errPtr = &panicErrWithChain{
			wrap:     panicWrap,
			original: *errPtr,
		}
	}
}

// recoverHandlerPanicVoid 是給 void return method 的安全網 — 只 log，不 propagate。
// 用於 ShowMessage / ShowError 等沒 error return 的 handler。
func recoverHandlerPanicVoid(handlerName string, logger *logging.Logger) {
	r := recover()
	if r == nil {
		return
	}

	panicUUID := newPanicUUID()
	logPanic(logger, "void handler panic recovered", handlerName, panicUUID, r, "")
}

// recoverHandlerPanicValue 是給「只回單一 value(沒 error)」的 RPC method 安全網。
// 適用於 GetVersion (string) / GetConfig (*AppConfig) / GetBackpressureStats (struct)
// 等沒 error return 的 getter — panic 時把 T zero value 寫回,避免 Wails runtime
// 收到 unrecovered panic 把 desktop process 擊潰。
//
// 使用方式(named return 必要,因為 defer 要能改回傳值):
//
//	func (a *App) GetVersion() (out string) {
//	    defer recoverHandlerPanicValue("GetVersion", a.logger, &out)
//	    return a.version
//	}
//
// 與 recoverHandlerPanic (error 版本) / recoverHandlerPanicVoid (void) 構成 3
// 個 typed variant,涵蓋所有 Wails RPC method return type 組合。
func recoverHandlerPanicValue[T any](handlerName string, logger *logging.Logger, outPtr *T) {
	r := recover()
	if r == nil {
		return
	}

	panicUUID := newPanicUUID()
	logPanic(logger, "typed handler panic recovered", handlerName, panicUUID, r, "")

	if outPtr != nil {
		var zero T
		*outPtr = zero
	}
}

// newPanicUUID 生成 panic 唯一識別碼,用來把對外 error 訊息 與 內部 Debug stack
// log 串起來。短 UUID(8 char)平衡 collision risk 與訊息精簡度:單一 process
// lifecycle 內預期 panic 數量 < 1000,8 char hex 提供 2^32 空間,collision 機率
// 可忽略。
//
// fallback:若 uuid lib 失敗(極不可能,uuid.NewString 不會 fail),fallback 到
// 固定 string,確保 logging 流程不會在 panic recovery 內又自己 panic。
func newPanicUUID() string {
	const uuidShortLen = 8

	s := uuid.NewString()
	if len(s) >= uuidShortLen {
		return s[:uuidShortLen]
	}

	return "no-uuid"
}

// logPanic 是三個 recover variant 共用的 log 邏輯:
//   - Error level:精簡訊息,只含 panic_uuid + handler + recovered value +
//     (可選)redactedOriginalErr。default production log level=Info 會看到此筆
//     但不會看到 stack。
//   - Debug level:完整 redacted stack。default production 不輸出,只在 logger
//     level=Debug 時才寫進 log file。
//
// 為何不直接把 stack 寫進 Error context:logger.sanitizeMessage 只 match
// `password / token / secret` keyword,對「純路徑」(/Users/foo/pCloud/...) 不
// 會 redact。主動先 redact 再餵給 logger,確保 default level 下 log file 乾淨。
//
// `redactedOriginalErr` 由 caller(recoverHandlerPanic)把 caller named return
// err 經 redactPathsInStack 過濾後傳入;空字串代表 caller 沒 ok-path err,此時
// 不寫 `original_err` field 避免 log 出現空欄。void / typed variant 都傳 ""。
func logPanic(
	logger *logging.Logger,
	label, handlerName, panicUUID string,
	recovered any,
	redactedOriginalErr string,
) {
	if logger == nil {
		return
	}

	// Error level:不含 stack,只含 handler + uuid + recovered value + (可選)
	// redacted original err。
	errCtx := map[string]any{
		"handler":    handlerName,
		"panic_uuid": panicUUID,
	}
	if redactedOriginalErr != "" {
		errCtx["original_err"] = redactedOriginalErr
	}

	// recovered value 在塞進 logger.Error 前必須先 redact。caller 可能
	// `panic(fmt.Errorf("讀檔失敗: %w", &fs.PathError{Path:"/Users/alice/.../emg.csv"}))`,
	// 那條 absolute path 經 %v 文字化後會原樣寫進 Error log;
	// logger.sanitizeMessage 只對 password/token/secret keyword 兜底,對純路徑
	// 無感(見上方註解)。先過 redactPathsInStack 再用 errors.New 包成 error。
	redactedRecovered := redactPathsInStack(fmt.Sprintf("%v", recovered))
	logger.Error(label, errors.New(redactedRecovered), errCtx) //nolint:err113 // recovered value is dynamic string after redact

	// Debug level:full redacted stack — 開發者在 Debug 模式可看到完整 trace,
	// 但 production Info level 不會輸出,避免 PII path leak。
	redactedStack := redactPathsInStack(string(debug.Stack()))
	logger.Debug("panic stack (redacted)",
		map[string]any{
			"handler":    handlerName,
			"panic_uuid": panicUUID,
			"stack":      redactedStack,
		})
}

// redactPathsInStack 把 stack trace 內的絕對路徑換成 `<redacted-path>/`,
// 保留檔名與行號(對 debug 而言已足夠識別 source location)。
//
// Thin wrapper over internal/security/redact:
//   - 既有 recover_test.go 不必改 import 即可繼續綁契約
//   - 新加入的 handler error redact 直接呼叫 redact.RedactForMessage,不繞 gui
//   - logger.sanitizeMessage 也能直接 import redact 而不會循環依賴(gui 已依賴
//     logging,反向不行)
//
// 修改 redact pattern 請改 internal/security/redact 一處。
func redactPathsInStack(stack string) string {
	return redact.Paths(stack)
}
