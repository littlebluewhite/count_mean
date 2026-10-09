package i18n

import (
	"errors"
	"strings"
)

// Error 是帶 catalog key 的 error:[[Domain analyzer]] 回 key 與參數,不在 analyzer 內
// 決定語言;handler 層(gui 的 [[Webview envelope]])以 Localize 依目前 locale 渲染
// (ADR-0048)。
//
// 訊息 = Key 的 catalog 文字以 Args 格式化(規則同 T:無 Args 時不經 Sprintf,文字裡的
// `%` 原樣保留);Cause 非 nil 時再接「: 」+ Cause 的文字。
//
// Error() 固定以內建 zh-TW catalog 渲染、與目前 locale 無關:log、err 通道與既有測試
// 看到的文字和 key 化之前逐位元組相同。Unwrap 回 Cause,errors.Is / As 照常走到 cause
// 與 sentinel。
type Error struct {
	Key   string
	Args  []any
	Cause error
}

// NewError 回只有訊息的 *Error(取代 errors.New(T(key, args...)))。
func NewError(key string, args ...any) error {
	return &Error{Key: key, Args: args}
}

// WrapError 回「訊息: cause」形狀的 *Error(取代 fmt.Errorf("%s: %w", T(key, args...), cause))。
func WrapError(cause error, key string, args ...any) error {
	return &Error{Key: key, Args: args, Cause: cause}
}

func (e *Error) Error() string {
	return e.render(zhTWCatalog.T, error.Error)
}

func (e *Error) Unwrap() error { return e.Cause }

// Localize 以目前 locale 渲染 err 給使用者看。
//
// err 鏈上沒有 *Error 時回 err.Error()。有的話,把 err 文字裡第一個 *Error 的 zh-TW
// 文字(它的 Error())換成目前 locale 的文字 —— *Error 之上的一般 wrapper(fmt.Errorf、
// 只轉傳文字的 stage error 等)保留自己的文字;*Error 的 Cause 與 error 型別的 Args
// 也以 Localize 遞迴渲染。wrapper 若改寫了 *Error 的文字(找不到原文),回 err.Error()。
//
// nil 回空字串。目前 locale 為 zh-TW(內建 catalog)時結果與 err.Error() 相同;global
// 未初始化時以內建 zh-TW 渲染(同 GetLocale 的預設)。
func Localize(err error) string {
	if err == nil {
		return ""
	}

	text := err.Error()

	var e *Error
	if !errors.As(err, &e) {
		return text
	}

	zh := e.Error()
	i := strings.LastIndex(text, zh)
	if i < 0 {
		return text
	}

	t := zhTWCatalog.T
	if inst := globalI18n.Load(); inst != nil {
		t = inst.T
	}

	return text[:i] + e.render(t, Localize) + text[i+len(zh):]
}

// render 以 t 取 Key 的文字並格式化;error 型別的 Args 與 Cause 以 text 渲染。
func (e *Error) render(t func(key string, args ...any) string, text func(error) string) string {
	args := make([]any, len(e.Args))
	for i, a := range e.Args {
		if err, ok := a.(error); ok {
			a = text(err)
		}
		args[i] = a
	}

	msg := t(e.Key, args...)
	if e.Cause != nil {
		msg += ": " + text(e.Cause)
	}

	return msg
}

// zhTWCatalog 只含內建 zh-TW catalog 的 instance:Error() 與 global 未初始化時的
// Localize 經它的 T 渲染(格式化與缺 key fallback 規則與 global T 相同)。
//
//nolint:gochecknoglobals // immutable builtin zh-TW catalog for Error()
var zhTWCatalog = &I18n{
	currentLocale: LocaleZhTW,
	messages:      map[Locale]map[string]string{LocaleZhTW: translationsZhTW},
	fallback:      LocaleZhTW,
}
