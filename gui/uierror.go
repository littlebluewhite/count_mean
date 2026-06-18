package gui

import "errors"

// UIError 是 GUI handler 給前端顯示的 error。
//
// 設計目的：GUI handler 的 err 同時扛兩個職責 —
//   1. err.Error() 字面值會被 failedXxxResult(runErr.Error()) 取走當作
//      result.Message 送 Wails 前端（i18n 翻譯 + redact 過的 PII path）
//   2. 上層仍能 errors.Is(err, ErrXxx) 比對失敗類別
// 把兩者解耦：Error() 回 user-facing message，sentinel 留給 errors.Is。
type UIError struct {
	sentinel error
	message  string
}

func (e *UIError) Error() string {
	return e.message
}

// Is 讓 errors.Is(uiErr, ErrXxx) 走到 UIError 內部存的 sentinel — 即使
// 字面值欄位是 unexported 也能被上層識別失敗類別。
func (e *UIError) Is(target error) bool {
	return errors.Is(e.sentinel, target)
}

func newUIError(sentinel error, message string) error {
	return &UIError{sentinel: sentinel, message: message}
}

// GUI handler 共用 sentinel — 對接 errors.Is 識別失敗類別。
// 命名按 handler+事件,不互通(muscle ratio 跟 cci 是不同 domain)。
var (
	ErrMuscleRatioAnalysisFailed = errors.New("muscle ratio analysis failed")
)
