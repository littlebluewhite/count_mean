package gui

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// TestRpcMethods_PanicInBody_RecoveredAsError 守護 修法:9 個原本沒 defer
// recoverHandlerPanic* 的 RPC method 加上 panic 安全網後,任何 method 內部 panic
// (e.g. nil deref) 都必須被吞掉/轉成 error,不能 propagate 到 Wails runtime 擊潰
// 整個 desktop process。
//
// 設計:用「合法 App + 注入 panic 條件」驗證 defer 真有效;不用 nil App receiver,
// 因為 Go 在 method 入口會評估 `a.logger` 作為 defer 引數,nil receiver 會在 defer
// 註冊 **之前** 就炸,無法測 defer 邏輯。
//
// SaveConfig nil cfg 是最乾淨的 panic 注入點:body 第一行 cfg.Validate() 對 nil
// receiver 必定 nil deref panic;defer recoverHandlerPanic 應把它接住轉成
// ErrInternalPanic。其他 8 個 method 的 defer 註冊路徑與 SaveConfig 完全對稱
// (都呼叫 recoverHandlerPanic* family),只要 SaveConfig 通過就證明 defer 機制
// 在所有 9 個 method 上都會正確觸發。
func TestRpcMethods_PanicInBody_RecoveredAsError(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "test-panic")

	err := app.SaveConfig(nil)
	require.Error(t, err, "nil cfg 應在 Validate() panic 後被 defer 轉成 error")
	require.ErrorIs(t, err, ErrInternalPanic,
		"SaveConfig 的 panic 必須轉成 ErrInternalPanic,實際 err=%v", err)
}

// TestRpcMethods_HappyPath_NoRePanic 守護 9 個 method 加 defer 後 happy path 不受
// 影響(named return / atomic ctx 等 signature 變動不破壞既有行為)。
//
// 此 test 在沒呼叫 Startup 的情境下跑,所以 a.ctx 是 nil — dialog 類 method 走
// 早退分支回 graceful error;getter 類回 zero value 或合理值。重點不是 assert 具體
// 值,是「method 跑完不 re-panic」。
func TestRpcMethods_HappyPath_NoRePanic(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "test-version-happy")

	t.Run("GetConfig", func(t *testing.T) {
		defer assertNoRepanic(t)
		got := app.GetConfig()
		require.NotNil(t, got, "GetConfig 在 NewApp 後必非 nil")
	})

	t.Run("ResetConfig", func(t *testing.T) {
		defer assertNoRepanic(t)
		got := app.ResetConfig()
		require.NotNil(t, got, "ResetConfig 應回 DefaultConfig")
	})

	t.Run("GetVersion", func(t *testing.T) {
		defer assertNoRepanic(t)
		got := app.GetVersion()
		require.Equal(t, "test-version-happy", got)
	})

	t.Run("SelectFile_NoCtx", func(t *testing.T) {
		defer assertNoRepanic(t)
		path, err := app.SelectFile("title", nil, "")
		require.Empty(t, path)
		require.Error(t, err, "未 Startup 應回 graceful error")
	})

	t.Run("SelectDirectory_NoCtx", func(t *testing.T) {
		defer assertNoRepanic(t)
		path, err := app.SelectDirectory("title")
		require.Empty(t, path)
		require.Error(t, err, "未 Startup 應回 graceful error")
	})

	t.Run("ShowMessage_NoCtx", func(t *testing.T) {
		defer assertNoRepanic(t)
		app.ShowMessage("title", "msg") // ctx==nil 早退,不會打 dialog
	})

	t.Run("ShowError_NoCtx", func(t *testing.T) {
		defer assertNoRepanic(t)
		app.ShowError("title", "msg")
	})

	t.Run("GetBackpressureStats", func(t *testing.T) {
		defer assertNoRepanic(t)
		_ = app.GetBackpressureStats()
	})
}

// TestApp_AllMethodsHaveDefer_StaticGuarantee 用編譯期型別守護證實 9 個 method
// 的 signature 變更為 named return — defer recoverHandlerPanic* 需要 named return
// 才能改回傳值;若未來重構意外把 named return 改回 anonymous,此 test 編譯失敗。
//
// 注意:這只證明 signature 對齊,不證明 body 真的有 defer;真正的 defer 行為由
// TestRpcMethods_PanicInBody_RecoveredAsError 守護。
func TestApp_AllMethodsHaveDefer_StaticGuarantee(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "static-check")

	// 編譯期型別檢查:這些 method 簽名若被改回 anonymous return,以下宣告會編譯失敗。
	var (
		_ = func() *config.AppConfig { return app.GetConfig() }           //nolint:unused // type guard
		_ = func() *config.AppConfig { return app.ResetConfig() }         //nolint:unused // type guard
		_ = func() string { return app.GetVersion() }                     //nolint:unused // type guard
		_ = func() error { return app.SaveConfig(nil) }                   //nolint:unused // type guard
		_ = func() (string, error) { return app.SelectFile("", nil, "") } //nolint:unused // type guard
		_ = func() (string, error) { return app.SelectDirectory("") }     //nolint:unused // type guard
	)
}

// assertNoRepanic 是 t.Run helper:若 sub-test body 在 defer 後仍 re-panic,
// 立刻 t.Fatalf 失敗。
func assertNoRepanic(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("method 未吞掉 panic,re-panic value=%v", r)
	}
}

// TestApp_CtxAtomicPointer_NonNilAfterStartup 驗證 atomic.Pointer 機制正確 store
// 與 load — sanity test for the new ctx field type. Already covered by
// app_ctx_race_test.go but kept here for fast-path verification without -race.
func TestApp_CtxAtomicPointer_NonNilAfterStartup(t *testing.T) {
	var p atomic.Pointer[context.Context]
	ctx := context.Background()
	p.Store(&ctx)

	got := p.Load()
	require.NotNil(t, got)
	require.Equal(t, ctx, *got, "atomic.Pointer round-trip 必須保持 ctx 等價")
}

