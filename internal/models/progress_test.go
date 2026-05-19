package models

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProgressTracker_SingleGoroutineCallbackContract 鎖定 修補的契約：
// ProgressTracker 的 mutating method（Start / UpdateProgress / Complete）
// 必須由單一 goroutine 驅動。本測試以 -race 跑時，驅動 100 次序列呼叫，
// 確認在「滿足契約」的情況下不會誤觸 race detector，並校驗 callback 觀察到的
// 進度確實隨 UpdateProgress 推進。
//
// 跑法：go test -race ./internal/models -run TestProgressTracker_SingleGoroutineCallback
func TestProgressTracker_SingleGoroutineCallbackContract(t *testing.T) {
	var (
		callCount atomic.Int32
		lastStep  atomic.Int32
	)

	totalSteps := 100
	tracker := NewProgressTracker(totalSteps, func(info ProgressInfo) {
		callCount.Add(1)
		lastStep.Store(int32(info.CurrentStep))
	})
	tracker.SetUpdateBuffer(0) // 取消節流，讓每筆 update 都觸發 callback

	tracker.Start("init")

	for step := 1; step <= totalSteps; step++ {
		tracker.UpdateProgress(step, "running", 0, "ch1")
	}

	tracker.Complete("done")

	assert.True(t, tracker.IsCompleted(), "tracker should report completed after final UpdateProgress")
	assert.Equal(t, float64(100), tracker.GetProgress(), "GetProgress should reach 100%% at completion")
	assert.GreaterOrEqual(t, callCount.Load(), int32(totalSteps),
		"expected callback to be invoked at least %d times (got %d)", totalSteps, callCount.Load())
	assert.Equal(t, int32(totalSteps), lastStep.Load(),
		"last callback should observe the final step value")
}

// TestProgressTracker_UpdateBufferThrottles 校驗 updateBuffer 的節流行為：
// 短時間多次 UpdateProgress 只觸發少量 callback，但 totalSteps 達標時必須觸發
// （避免「99% 卡住」UX 退化）。
func TestProgressTracker_UpdateBufferThrottles(t *testing.T) {
	var callCount atomic.Int32

	tracker := NewProgressTracker(10, func(_ ProgressInfo) {
		callCount.Add(1)
	})
	tracker.SetUpdateBuffer(200 * time.Millisecond)

	// 連續 9 次（未到 totalSteps，且發生在 200ms 內）只應觸發 1 次（第 1 次未經過時間檢查）
	for step := 1; step <= 9; step++ {
		tracker.UpdateProgress(step, "running", 0, "ch1")
	}

	// 第 10 次達 totalSteps，必須繞過 buffer 觸發
	tracker.UpdateProgress(10, "running", 0, "ch1")

	assert.LessOrEqual(t, callCount.Load(), int32(3),
		"updateBuffer 內的高頻更新應被節流（觀察到 %d 次）", callCount.Load())
	assert.GreaterOrEqual(t, callCount.Load(), int32(1),
		"最終步驟必須觸發 callback")
}

// TestProgressTracker_StartEmitsInitialFrame 釘住 核心 fix:
// Start() 必須立刻 emit 一筆「進度=0」的初始 frame。舊版 lastUpdateAt
// 被 NewProgressTracker 初始化為 time.Now(),導致 Start 內立刻呼叫
// UpdateProgress(0, ...) 被自身節流抑制 — 前端 / callback 永遠收不到
// 「分析已開始」訊號,使用者誤以為按鈕沒回應。
func TestProgressTracker_StartEmitsInitialFrame(t *testing.T) {
	var (
		seenSteps   []int
		seenStatuses []string
	)
	tracker := NewProgressTracker(10, func(info ProgressInfo) {
		seenSteps = append(seenSteps, info.CurrentStep)
		seenStatuses = append(seenStatuses, info.Status)
	})
	// 故意設超大 updateBuffer 證明 initial frame 不被節流抑制
	tracker.SetUpdateBuffer(10 * time.Second)

	tracker.Start("初始化")

	require.Len(t, seenSteps, 1,
		"Start() 必須觸發一筆 callback (P1-J H27); 觀察到 %d 筆", len(seenSteps))
	assert.Equal(t, 0, seenSteps[0],
		"第一筆 callback 的 CurrentStep 應為 0")
	assert.Equal(t, "初始化", seenStatuses[0],
		"第一筆 callback 的 Status 應為 initial status")
}

// TestProgressTracker_FastSecondRunNotSuppressed 釘住 fast second run:
// 同一程式內接連啟動兩個 Tracker(模擬使用者快速連按「執行」),第二個
// Tracker 的 Start() 必須能立即 emit;舊版 lastUpdateAt = time.Now()
// 雖然每個 Tracker 是新 instance 不繼承前次 state,但若有未來 caller 把
// Tracker 改成共用,initial frame bypass 仍應守住第一筆必出。
func TestProgressTracker_FastSecondRunNotSuppressed(t *testing.T) {
	makeTracker := func() (*ProgressTracker, *atomic.Int32) {
		var count atomic.Int32
		t := NewProgressTracker(5, func(_ ProgressInfo) {
			count.Add(1)
		})
		t.SetUpdateBuffer(10 * time.Second)
		return t, &count
	}

	t1, c1 := makeTracker()
	t1.Start("run1")
	assert.Equal(t, int32(1), c1.Load(), "第一個 Tracker 的 Start 必須觸發 1 筆 callback")

	// 緊接著另一個 Tracker,即使在 updateBuffer 內也必須立即 emit。
	t2, c2 := makeTracker()
	t2.Start("run2")
	assert.Equal(t, int32(1), c2.Load(),
		"第二個 Tracker 的 Start 必須觸發 1 筆 callback (initial frame bypass)")
}

// TestProgressTracker_StepZeroBypassThrottle 釘住 step=0(initial signal)
// 不論 lastUpdateAt 多近都必過 throttle。模擬 calculator orchestrator 在
// 同一個 Tracker 上連續觸發 Start + step1 + ... 的 hot path 邊界。
func TestProgressTracker_StepZeroBypassThrottle(t *testing.T) {
	var count atomic.Int32
	tracker := NewProgressTracker(10, func(_ ProgressInfo) {
		count.Add(1)
	})
	tracker.SetUpdateBuffer(10 * time.Second)

	// 連續兩次 step=0 都應 emit(第一次 lastUpdateAt zero / 第二次 step==0 bypass)
	tracker.UpdateProgress(0, "stage A", 0, "")
	tracker.UpdateProgress(0, "stage B", 0, "")

	assert.GreaterOrEqual(t, count.Load(), int32(2),
		"step=0 必須無條件 bypass throttle (observed %d)", count.Load())
}

// TestProgressTracker_ZeroTotalStepsGuard 釘住 修法:totalSteps == 0
// 不應產出 +Inf / NaN percentage(過去 float64(step) / float64(0) * 100
// 直接 division-by-zero,被 encoding/json marshal 失敗時整條 GUI callback
// payload 拋出)。修法後 Percentage 為 0,EstimatedTime 標「計算中...」
// 而非誤回「完成」 — caller 拿到的 ProgressInfo 必須仍是 valid 的 zero-value
// 安全 payload。
func TestProgressTracker_ZeroTotalStepsGuard(t *testing.T) {
	var seen []ProgressInfo
	tracker := NewProgressTracker(0, func(info ProgressInfo) {
		seen = append(seen, info)
	})

	// Start (step=0) — initial frame 必須 emit,即使 totalSteps==0
	tracker.Start("starting")

	// 後續任意 UpdateProgress 也不該 panic 或回 NaN
	tracker.UpdateProgress(0, "running", 0, "ch1")

	require.NotEmpty(t, seen, "Start 必須 emit 一筆 callback,即使 totalSteps==0")

	for i, info := range seen {
		// Inf / NaN 都不該出現 — 用 != info.Percentage 比 self 偵測 NaN
		if info.Percentage != info.Percentage { //nolint:staticcheck // NaN self-check intentional
			t.Errorf("frame %d Percentage 為 NaN: %v", i, info.Percentage)
		}
		if info.Percentage < 0 || info.Percentage > 100 {
			t.Errorf("frame %d Percentage 越界 (期望 [0,100]):%v", i, info.Percentage)
		}
		// EstimatedTime 不該是「完成」(totalSteps==0 退化情形不是完成)
		if info.EstimatedTime == "完成" {
			t.Errorf("frame %d EstimatedTime 不該標「完成」(totalSteps==0):%q", i, info.EstimatedTime)
		}
	}
}

// TestProgressTracker_ZeroTotalStepsGetProgress 釘住 GetProgress 在 totalSteps==0
// 時不被 修法回退影響 — 既有 nil-guard 邏輯維持 (回 0,不 NaN)。
func TestProgressTracker_ZeroTotalStepsGetProgress(t *testing.T) {
	tracker := NewProgressTracker(0, nil)
	got := tracker.GetProgress()
	if got != 0 {
		t.Errorf("GetProgress() with totalSteps==0 = %v, want 0", got)
	}
}
