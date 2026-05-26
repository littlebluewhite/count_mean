package gui

import (
	"count_mean/internal/logging"
)

// HandlerRun 是所有 Wails GUI handler 共吃的 cross-cutting wrapper(Tier 1)。
//
// 收乾兩件 cross-cutting:
//  1. `defer recoverHandlerPanic(name, logger, &err)` — panic safety(沿用既有
//     recover.go 模組,不重寫 PII redaction 契約)
//  2. entry/exit log:`logger.Info("開始"+name, nil)` / `logger.Info(name+"完成", nil)`
//     — entry log 在 body 執行前打,exit log 只在 body 成功時打,行為對齊既有
//     5 個 handler 的 `"開始 X"` / `"X 完成"` 字串
//
// Contract-neutral:body 回什麼就傳什麼,單一通道 `(envelope, nil)` 與雙通道
// `(result, err)` 都吃 — 不在這層收斂錯誤通道契約。
//
// 不注入 ctx:body 自己 close over `a.context()`,樣板 surface 維持最小、不被
// 「ctx 該何時何處檢查 cancel」綁住。Tier 2 [[AnalysisHandler[P, R]]] 內部會把
// ctx 注入 Execute closure,給 family handler 一致的 cancel 入口。
//
// 不接 fields callback:entry/exit log 只帶 name,handler body 自行用
// `a.logger.Info(...)` 攜帶 handler-specific metadata(params / output_file /
// subject 數等),避免樣板邊界處理 nil-safety / panic-recovery 邊界。
func HandlerRun[R any](logger *logging.Logger, name string, body func() (R, error)) (result R, err error) {
	defer recoverHandlerPanic(name, logger, &err)

	logger.Info("開始"+name, nil)

	result, err = body()
	if err != nil {
		return result, err
	}

	logger.Info(name+"完成", nil)

	return result, nil
}
