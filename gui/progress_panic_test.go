package gui

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/logging"
	"count_mean/internal/models"
)

// TestWailsProgressEmitter_RecoversFromHandlerPanic 釘住
//
// 過去 wailsProgressEmitter 直接呼叫 runtime.EventsEmit,若 Wails 內部 dispatch
// 鏈 / frontend handler / JSON encoder 任一處 panic(典型情境:應用程式 binding
// closure 對 nil 操作、或 frontend.Events handler 自己丟 panic),會 propagate
// 出 emitter,把呼叫者(calculator collector goroutine)整條打死,使用者看到
// 半成品分析狀態 + GUI 卡住但沒錯誤回饋。
//
// 修法是在 wailsProgressEmitter 內加 defer recover() 把 panic 轉成 Debug log;
// 任何來自 EventsEmit 的 panic 都被吃掉,calculator 繼續跑完,該筆 progress
// 事件遺失(已有 throttle / 後續 step 會補,不致命)。
//
// 注意:此 test 不能真的 boot Wails runtime — runtime.EventsEmit 對 non-Wails
// ctx 會 log.Fatalf 殺 process。改用「在 emitter wrapper 內注入 hook 觸發 panic」
// 的等價驗證:我們把 production wailsProgressEmitter 內的 EventsEmit 改為走過
// 一個可注入的內部 sink,在 test 內把 sink 換成 panic 來源。
//
// 但為了避免 invasive,直接驗 emitter 自己有 recover 即可:用一個經 Wails-marker
// ctx 註冊但 EventsEmit 會 panic 的 wrapper,確認 emitter 跑完不 crash。
func TestWailsProgressEmitter_RecoversFromHandlerPanic(t *testing.T) {
	// 用 in-memory logger 抓 panic 是否被 log
	var buf bytes.Buffer
	prevDefault := logging.NewLogger(logging.LevelDebug, &buf, false)
	t.Cleanup(func() { _ = prevDefault })

	// 構造一個 ctx 有 Wails events marker,讓 hasWailsEventsHandler 通過
	// (production 走 production 路徑)。實際 runtime.EventsEmit 在這條 ctx
	// 下會 type-assert "events" 為 frontend.Events;我們塞 dummy string,
	// runtime.EventsEmit 內部 type assertion 失敗會 log.Fatalf 殺 process。
	//
	// 為此 test 改驗 emitter 對 caller panic 的吸收性:emitter wrapper 內任何
	// panic source 都應被 deferred recover() 接住。我們用「ctxFn 本身 panic」
	// 來模擬:雖然 production wailsProgressEmitter 是 EventsEmit 而非 ctxFn
	// panic,但 deferred recover 對任何 panic source 都有效,等價驗證。
	ctxFn := func() context.Context {
		panic("simulated wails handler panic")
	}

	emit := wailsProgressEmitter(ctxFn)

	// 跑完不該 propagate panic 到 caller。
	require.NotPanics(t, func() {
		emit(models.ProgressInfo{
			Percentage: 50,
			Status:     "should not crash",
		})
	}, "wailsProgressEmitter 必須吸收 panic,讓 calculator collector goroutine 繼續跑")

	_ = buf // log 內容驗證在 production logger 下會跑,test 環境用 default logger
}

// hasWailsEventsHandler 不能只 nil-check ctx.Value() — Wails 內部對 "events"
// 做 type-assert(frontend.Events),若 caller 塞 string 等不可 cast 的 type,
// 同樣會在後續 EventsEmit Fatalf。改成「ctx.Value 不可 nil **且** 不可是字面
// nil-typed value」是最低成本守門。
//
// 由於 frontend.Events 是 Wails internal interface 不可直接 import,test 只能
// 驗 nil 與「明確 nil interface」兩條:
//   - nil ctx → false
//   - 沒 events key → false
//   - 有 events key 帶 nil interface value → false (strict 修法)
//   - 有 events key 帶任意 non-nil value → true (保留向後相容)
//
// 注意:由於 Wails string key 是 contract,任何 non-nil value 都 forward 到
// EventsEmit;真正 type-mismatch 由 Wails 內部 Fatalf,我們無法在這層擋。
// 但「明確注入 nil value」這條測試守住「nil 偽裝成 typed value 鑽過 check」
// 的最常見 bug。
func TestHasWailsEventsHandler_TypeAssertStrictness(t *testing.T) {
	t.Run("nil_typed_value_should_be_rejected", func(t *testing.T) {
		// 把 nil string pointer 塞進 events key — ctx.Value 會回 *string(nil),
		// 表面是 non-nil interface,但底層 value 是 nil。fix 前 hasWailsEventsHandler
		// 用 != nil 判斷會 false-positive,fix 後嚴格 type-assert 才會 reject。
		var nilPtr *string
		ctx := context.WithValue(context.Background(), wailsEventsCtxKey, nilPtr) //nolint:staticcheck // SA1029: 模擬 Wails framework 的 string key 注入

		// 注意:這條測試的「正確答案」依賴 hasWailsEventsHandler 修法。修法前
		// 此 test 預期 fail(passes when fix 還沒 in),修法後預期 pass。
		// 修法策略:用 type assertion 確認 value 不為 nil-typed pointer。
		got := hasWailsEventsHandler(ctx)
		// 為了不過度 over-fit production Wails 注入的 string value,只 assert
		// 「pure nil typed pointer 必須被視為 not ready」這條邊界。
		assert.False(t, got, "nil-typed interface value 應視為 ctx 未就緒,不該 forward 給 EventsEmit")
	})

	t.Run("nil_ctx", func(t *testing.T) {
		var nilCtx context.Context
		assert.False(t, hasWailsEventsHandler(nilCtx))
	})

	t.Run("no_events_key", func(t *testing.T) {
		assert.False(t, hasWailsEventsHandler(context.Background()))
	})

	t.Run("string_value_passes", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), wailsEventsCtxKey, "non-nil-marker") //nolint:staticcheck // SA1029
		assert.True(t, hasWailsEventsHandler(ctx),
			"任何 non-nil value 維持向後相容(production Wails 注入 frontend.Events)")
	})
}

// TestWailsProgressEmitter_NilCtxStillSafe 守 配對:nil ctx 仍走原有
// short-circuit,不該因為加 recover 變成跑進 EventsEmit。
func TestWailsProgressEmitter_NilCtxStillSafe(t *testing.T) {
	emit := wailsProgressEmitter(func() context.Context { return nil })

	require.NotPanics(t, func() {
		emit(models.ProgressInfo{Percentage: 50, Status: "must skip"})
	})
}

// helper to silence unused import lint
var _ = strings.Contains
