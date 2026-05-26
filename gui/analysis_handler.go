package gui

import (
	"context"

	"count_mean/internal/io"
	"count_mean/internal/logging"
)

// AnalysisHandler 是 [[Analysis pipeline family]] 的泛型樣板(Tier 2):
// 「validate → execute → CSV write」三步管線以 generic struct + 三 closure 表達。
//
// 三個 field 描述 handler 的身分與 dependency:
//   - Name:entry/exit log 用的 handler 名稱(例如「階段分析」「CCI 分析」)
//   - Logger:結構化 logger,委派給 HandlerRun
//   - CSV:format-aware write 入口,由 WriteCSV closure 在第 3 步使用
//
// 三個 closure 描述 handler 的形狀:
//   - Validate:參數驗證(必填、path validation 等);required
//   - Execute:核心分析步驟,樣板統一注入 ctx 給 caller close over;required
//   - WriteCSV:CSV 寫檔步驟,回傳 outputPath;**optional**(nil 時跳過寫檔,
//     對應 batch 變體把 per-subject write 摺進 execute 的情況,例如
//     `AnalyzeMuscleRatio`)
//
// `Run` 把整個三步管線包進 [[HandlerRun]] 的 body,拿 panic recovery + entry/exit log。
// 樣板**不**負責 `state.Load`、result transform、i18n.T — 這三項由 caller 在 Run
// 外處理(與 grilling 結論一致)。
type AnalysisHandler[P, R any] struct {
	Name     string
	Logger   *logging.Logger
	CSV      *io.CSVHandler
	Validate func(P) error                           // required
	Execute  func(context.Context, P) (R, error)     // required
	WriteCSV func(*io.CSVHandler, R) (string, error) // optional; nil = skip
}

// runResult 是 Run 內部包裝 (result, outputPath) 一起進 HandlerRun body 的 wrapper。
// 因為 HandlerRun 只支援單一 result generic,雙返回值需要先在 body 內部聚合一根
// struct 再讓 HandlerRun 沿用 panic recovery / log 路徑;此 struct 不暴露給 caller。
type runResult[R any] struct {
	result     R
	outputPath string
}

// Run 執行三步管線並回傳 (result, outputPath, err)。
//
// 短路規則:
//   - Validate err → return (zero R, "", err)
//   - Execute err → return (zero R, "", err)
//   - WriteCSV != nil & err → return (result, "", err)
//   - WriteCSV == nil → 跳過寫檔,return (result, "", nil)
//   - 全部成功 → return (result, outputPath, nil)
//
// ctx 統一注入 Execute closure(family handler 一致的 cancel 入口);Validate 與
// WriteCSV 不吃 ctx — Validate 為純參數檢查、WriteCSV 內部走 csvHandler 自帶的
// I/O 邊界(不需要 GUI lifecycle cancel 介入)。
//
// panic 路徑由委派的 HandlerRun 透過 `defer recoverHandlerPanic` 接管:body 內
// 任一 closure(Validate / Execute / WriteCSV)panic 都會被攔成 ErrInternalPanic
// wrap,err 走 named return 透過 HandlerRun 上拋給 caller。
func (h *AnalysisHandler[P, R]) Run(ctx context.Context, params P) (result R, outputPath string, err error) {
	wrapped, runErr := HandlerRun(h.Logger, h.Name, func() (runResult[R], error) {
		if validateErr := h.Validate(params); validateErr != nil {
			return runResult[R]{}, validateErr
		}

		execResult, execErr := h.Execute(ctx, params)
		if execErr != nil {
			return runResult[R]{}, execErr
		}

		if h.WriteCSV == nil {
			// batch 變體:write 摺進 execute(per-subject),樣板層不再寫檔。
			return runResult[R]{result: execResult}, nil
		}

		writePath, writeErr := h.WriteCSV(h.CSV, execResult)
		if writeErr != nil {
			// 維持與 PRD「WriteCSV err → return (result, "", err)」一致 — execResult
			// 仍透過 outer return 傳出,outputPath 強制為空字串。
			return runResult[R]{result: execResult}, writeErr
		}

		return runResult[R]{result: execResult, outputPath: writePath}, nil
	})

	return wrapped.result, wrapped.outputPath, runErr
}
