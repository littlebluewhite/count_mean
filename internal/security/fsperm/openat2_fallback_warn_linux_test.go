//go:build linux

// Package fsperm internal test: exercises the openat2 ENOSYS fallback warning
// hook. Lives in the internal `fsperm` package (not `fsperm_test`) so
// it can swap warnOpenat2Fallback / reset openat2FallbackOnce — both
// package-private. This file is only compiled under `go test` so production
// callers cannot accidentally tamper with the singleton.
package fsperm

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestOpenat2_ENOSYSFallback_WarnsOnce 釘住 openat2 在 ENOSYS 降級到
// os.OpenFile 時必須以 sync.Once 守護的一次性警告通知 operator,且即便短時間
// 內被觸發多次,警告函式僅執行一次。
//
// 我們不模擬真實 ENOSYS syscall (那需要 LSM/kernel 配合,在 CI 上不可行),
// 而是直接呼 emitOpenat2FallbackWarning — 它是 fallback 路徑的唯一入口,釘
// 住「N 次呼叫只觸發 1 次 warning」就等同釘住 production 行為。
func TestOpenat2_ENOSYSFallback_WarnsOnce(t *testing.T) {
	// 保留原始 hook,測試結束後復原 — 避免污染後續 test (sync.Once 是 package-
	// level,跨 test 共享)。
	originalHook := warnOpenat2Fallback
	originalOnce := openat2FallbackOnce
	t.Cleanup(func() {
		warnOpenat2Fallback = originalHook
		openat2FallbackOnce = originalOnce
	})

	// 重置 sync.Once 才能在 test 內觀察「第一次 fire」(原始 once 可能在
	// 前一個 test 已被 trigger 過)。指標型 once 才能整顆 swap 而不觸發 vet
	// "assignment copies lock value" 警告。
	openat2FallbackOnce = &sync.Once{}

	var callCount atomic.Int32
	warnOpenat2Fallback = func() {
		callCount.Add(1)
	}

	const triggerCount = 50

	// 並發觸發以驗證 sync.Once 在 race condition 下也只跑一次。
	var wg sync.WaitGroup
	wg.Add(triggerCount)
	for i := 0; i < triggerCount; i++ {
		go func() {
			defer wg.Done()
			emitOpenat2FallbackWarning()
		}()
	}
	wg.Wait()

	if got := callCount.Load(); got != 1 {
		t.Fatalf("emitOpenat2FallbackWarning fired %d times across %d concurrent calls; "+
			"P1-1 contract requires exactly 1", got, triggerCount)
	}

	// 額外 serial 觸發 — 確認 once 在後續呼叫仍是「鎖死」狀態。
	for i := 0; i < 10; i++ {
		emitOpenat2FallbackWarning()
	}
	if got := callCount.Load(); got != 1 {
		t.Fatalf("emitOpenat2FallbackWarning fired %d times after additional serial calls; "+
			"sync.Once must remain latched after first fire", got)
	}
}
