package gui

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// TestApp_CtxConcurrentReadWrite_NoRace 釘住 修法:
// 過去 a.ctx 是裸 context.Context 欄位,Wails Startup (writer) 與 RPC
// dispatch (reader) 在不同 goroutine,無同步機制 — race detector 必抓
// "DATA RACE on App.ctx"。修法改 atomic.Pointer[context.Context],
// loadCtx 走 atomic.Load,Startup 走 atomic.Store,應 clean。
//
// 此 test 模擬 worst case:writer 高頻替換 ctx (在 production 中 Wails
// 只 Store 一次,但測試用密集寫法把 race window 放大),readers 透過
// loadCtx / context() 高頻讀。
//
// 注意:不直接觸發 SelectFile / ShowMessage 等會打到 runtime.OpenFileDialog /
// MessageDialog 的 method — Wails runtime 收到非 Wails-created ctx 會直接
// log.Fatalf 整個 process。讀寫 race 在 a.ctx atomic 操作層面已完整捕獲,
// 不需要實際觸發 dialog code path。
func TestApp_CtxConcurrentReadWrite_NoRace(t *testing.T) {
	if !raceEnabled {
		t.Skip("requires -race; without race detector this test is a no-op")
	}

	app := NewApp(config.DefaultConfig(), "test-ctx-race")

	const writers = 2
	const readers = 8
	const iters = 200

	var wg sync.WaitGroup

	// writers: 反覆替換 ctx,模擬 Startup race window 被無限放大的情境
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				ctx := context.Background()
				app.Startup(ctx)
			}
		}()
	}

	// readers: 透過 loadCtx / context() 反覆讀 a.ctx — 這些是所有 RPC method
	// 讀 ctx 的共同路徑 (SelectFile / SelectDirectory / ShowMessage / ShowError
	// 內部都先走 loadCtx 才用 ctx)。race detector 看的是 a.ctx 欄位讀寫;
	// loadCtx 覆蓋了所有 method 共享的讀路徑。
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				_ = app.context() // 透過 accessor 讀
				_ = app.loadCtx() // 直接 atomic.Load
			}
		}()
	}

	wg.Wait()

	// 完成後 ctx 應仍可讀(寫入順序未定但都是非 nil)
	require.NotNil(t, app.loadCtx(), "writers 跑完後 ctx 必非 nil")
}

// TestApp_CtxStartupRead_AtomicVisibility 守護 atomic store 後 reader 立刻看見:
// 即使在 race detector 關閉下也能驗證 happens-before 語意 — Startup Store 完成後,
// 同 goroutine 後續 loadCtx 必須回傳剛 Store 的 ctx (非 nil)。
func TestApp_CtxStartupRead_AtomicVisibility(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "test-ctx-visibility")

	require.Nil(t, app.loadCtx(), "Startup 前 loadCtx 應為 nil")

	ctx := context.Background()
	app.Startup(ctx)

	got := app.loadCtx()
	require.NotNil(t, got, "Startup 後 loadCtx 應非 nil")

	// context() accessor 也應走 atomic.Load,不再回 Background()
	require.Equal(t, got, app.context(), "context() 應 return loadCtx 同一個 ctx")
}
