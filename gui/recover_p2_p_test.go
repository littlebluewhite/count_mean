package gui

import (
	"errors"
	"strings"
	"testing"
)

// TestRecoverHandlerPanic_PreservesOriginalErrorChain 釘住 panic 發生時
// returned err 的 chain 必須同時走訪 ErrInternalPanic + caller 原 ok-path err。
//
// 為何此契約重要:caller 上游若用 errors.Is(err, somesentinel) 偵測 ok-path err
// 做後續分流(典型:對 ErrNotFound 走 fallback 分支),原本 panic 「完全丟棄」原 err
// 後 errors.Is 永遠 false,fallback 永遠走不到 — caller 與 normal-err 無法區分。
//
// 解法:panicErrWithChain.Unwrap() 同時 return wrap + original,errors.Is/As
// 可走訪兩者;同時 .Error() 只渲染 wrap(PII contract 不破)。
func TestRecoverHandlerPanic_PreservesOriginalErrorChain(t *testing.T) {
	// 構造 caller ok-path sentinel
	callerSentinel := errors.New("caller-sentinel:not-found-fallback")

	err := error(callerSentinel)

	func() {
		defer recoverHandlerPanic("TestHandler", nil, &err)
		// caller 已塞 ok-path err 但隨後 panic 發生
		panic("post-ok-path panic")
	}()

	// errors.Is 必須同時可走訪兩條 chain
	if !errors.Is(err, ErrInternalPanic) {
		t.Errorf("returned err 應可 errors.Is(ErrInternalPanic),實際 err=%v", err)
	}
	if !errors.Is(err, callerSentinel) {
		t.Errorf("returned err 應可 errors.Is(callerSentinel) 走訪原 caller chain,實際 err=%v", err)
	}

	// 對偶守護:.Error() 文字仍不可含 caller 原 err 字面(PII contract)
	errMsg := err.Error()
	if strings.Contains(errMsg, "caller-sentinel:not-found-fallback") {
		t.Errorf("returned err.Error() 不應含 caller 原 err 字面 (PII regression),實際=%q", errMsg)
	}

	// 仍應含 panic_uuid + handler 識別
	if !strings.Contains(errMsg, "panic_uuid=") {
		t.Errorf("returned err 應含 panic_uuid,實際=%q", errMsg)
	}
	if !strings.Contains(errMsg, "handler=TestHandler") {
		t.Errorf("returned err 應含 handler 名稱,實際=%q", errMsg)
	}
}

// TestRecoverHandlerPanic_NilCallerErr_ChainStillWorks 邊界:caller 原 err 為 nil
// (handler 在 ok-path 沒塞 err 才 panic),Unwrap() 應退化為單元素 chain,errors.Is
// 仍可走訪 ErrInternalPanic。
func TestRecoverHandlerPanic_NilCallerErr_ChainStillWorks(t *testing.T) {
	var err error // 起始為 nil

	func() {
		defer recoverHandlerPanic("TestHandler", nil, &err)
		// 不塞 ok-path err 就 panic
		panic("clean panic, no pre-err")
	}()

	if !errors.Is(err, ErrInternalPanic) {
		t.Errorf("nil caller err 情境下 errors.Is(ErrInternalPanic) 仍應為 true,實際 err=%v", err)
	}

	// nil original 不該破壞 .Error() 渲染
	if err.Error() == "" {
		t.Errorf("returned err.Error() 不該為空字串")
	}
}

// TestRecoverHandlerPanic_TypedCallerErr_ErrorsAsWorks 驗證 errors.As 對 typed
// caller err 仍可走訪 — 比 errors.Is 更嚴格,確保 Unwrap() 鏈完整。
type customCallerErr struct {
	Code string
}

func (c *customCallerErr) Error() string {
	return "custom-err-code:" + c.Code
}

func TestRecoverHandlerPanic_TypedCallerErr_ErrorsAsWorks(t *testing.T) {
	original := &customCallerErr{Code: "EBOUNDS"}

	var err error = original

	func() {
		defer recoverHandlerPanic("LookupHandler", nil, &err)
		panic("oops")
	}()

	// errors.AsType 應可從 chain 撈出 *customCallerErr (Go 1.26+ 泛型 API)
	got, ok := errors.AsType[*customCallerErr](err)
	if !ok {
		t.Fatalf("errors.AsType 應可走訪到 *customCallerErr,實際 err=%v", err)
	}
	if got.Code != "EBOUNDS" {
		t.Errorf("errors.AsType 撈到的 *customCallerErr.Code 應為 EBOUNDS,實際=%q", got.Code)
	}

	// 同時 ErrInternalPanic 仍在 chain 內
	if !errors.Is(err, ErrInternalPanic) {
		t.Errorf("ErrInternalPanic 應仍可被 errors.Is 走訪")
	}
}
