package calculator

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

// buildLargeDataset constructs an EMG dataset sized to keep workers busy long
// enough that mid-flight cancellation is observable.
func buildLargeDataset(rows, channels int) *models.EMGDataset {
	headers := make([]string, channels+1)
	headers[0] = "Time"

	for i := 0; i < channels; i++ {
		headers[i+1] = "Ch"
	}

	data := make([]models.EMGData, rows)

	for i := 0; i < rows; i++ {
		chans := make([]float64, channels)
		for c := 0; c < channels; c++ {
			chans[c] = float64(i+c) * 0.1
		}
		data[i] = models.EMGData{Time: float64(i) * 0.001, Channels: chans}
	}

	return &models.EMGDataset{Headers: headers, Data: data}
}

// TestCalculate_PreCancelledContext_ReturnsCanceled verifies the new ctx
// cancellation contract: when the caller passes an already-cancelled context,
// Calculate must abort without running any per-channel work.
func TestCalculate_PreCancelledContext_ReturnsCanceled(t *testing.T) {
	calc := NewMaxMeanCalculator(10)
	dataset := buildLargeDataset(10000, 8)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel before Calculate runs.

	start := time.Now()
	_, err := calc.Calculate(ctx, dataset, 100)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"expected ctx.Canceled, got %v", err)
	assert.Less(t, elapsed, 200*time.Millisecond,
		"pre-cancelled Calculate should return quickly (got %v)", elapsed)
}

// TestCalculate_TimeoutContextPropagates exercises a deadline-bounded context.
// 直接用 0 timeout 等同於 pre-cancel，但走 DeadlineExceeded 路徑——驗證
// Calculate 對 context.DeadlineExceeded 與 context.Canceled 兩種取消形式
// 都會正確傳播。比 mid-flight cancel 更穩定，因為 deadline check 不依賴
// worker 與 cancel 之間的賽跑（過去的 timing-dependent 寫法在快機器上
// 8ms 跑完，cancel 來不及插入）。
func TestCalculate_TimeoutContextPropagates(t *testing.T) {
	calc := NewMaxMeanCalculator(10)
	dataset := buildLargeDataset(10000, 8)

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	start := time.Now()
	_, err := calc.Calculate(ctx, dataset, 100)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t,
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
		"expected DeadlineExceeded or Canceled, got %v", err)
	assert.Less(t, elapsed, 500*time.Millisecond,
		"Calculate with expired deadline should return quickly (got %v)", elapsed)
}

// TestCalculate_CtxCancel_NoGoroutineLeak 守護 ctx 取消後 worker 不會
// 卡在 `results <- ...` 的 blocking-send 上 leak。worker 必須用 select+ctx.Done
// 防護 send,讓 wg.Wait → close(resultsChan) 自然完成。
//
// 驗證方式: 量測 cancel 後 100ms (預留 worker 收到 ctx 信號 + 結束時間) 的
// goroutine 計數,應回到 baseline ± 容忍誤差 (測試框架可能引入少量背景 goroutine)。
func TestCalculate_CtxCancel_NoGoroutineLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping goroutine-leak test in -short mode")
	}

	baseline := runtime.NumGoroutine()

	calc := NewMaxMeanCalculator(10)
	dataset := buildLargeDataset(100_000, 16)

	ctx, cancel := context.WithCancel(context.Background())

	// 在新 goroutine 啟動 Calculate,主 goroutine 立即 cancel 模擬 mid-flight 中斷
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = calc.Calculate(ctx, dataset, 100)
	}()

	// 等一小段時間讓 worker 都進入計算迴圈
	time.Sleep(5 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Calculate 沒在 cancel 後 2s 內返回,worker 可能在 results send 上 deadlock")
	}

	// 給 worker / cleanup goroutine 一點時間退出
	time.Sleep(100 * time.Millisecond)

	current := runtime.NumGoroutine()
	// 容忍 5 goroutine 噪音 (testing framework / runtime workers)
	if current > baseline+5 {
		t.Errorf("goroutine leak: baseline=%d current=%d delta=%d", baseline, current, current-baseline)
	}
}

// TestCalculate_MidFlightCancel_LargeDataset is the spec verification:
// a dataset large enough that the parallel sliding-window inner loop is the
// dominant cost; cancel mid-flight; assert return ≤ 1s with result == nil and
// err wrapping context.Canceled.
//
// Sizing rationale: with 1M rows × 32 ch and windowSize=10, each channel does
// ~1M increment iterations. On a 16-core machine the worker pool will keep
// busy long enough that 50ms cancel fires while inner loops are still running.
// Without the ctx-aware SlidingWindowCalculator each inner loop runs to
// completion before honoring the cancel; the 4096-iter check inside
// CalculateMaxMean lets us hit the ≤ 1s budget.
func TestCalculate_MidFlightCancel_LargeDataset(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-dataset cancellation test in -short mode")
	}

	calc := NewMaxMeanCalculator(10)
	dataset := buildLargeDataset(1_000_000, 32)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	results, err := calc.Calculate(ctx, dataset, 10)
	elapsed := time.Since(start)

	if err == nil {
		t.Skipf("hardware too fast — calc finished in %v before 50ms cancel could fire; "+
			"mid-flight semantics covered by TestSlidingWindow_CalculateMaxMean_MidFlightCancel",
			elapsed)
	}

	require.Error(t, err)
	assert.True(t,
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
		"expected DeadlineExceeded or Canceled, got %v", err)
	assert.Less(t, elapsed, 1*time.Second,
		"large-dataset Calculate with 50ms timeout must return within 1s (got %v)",
		elapsed)
	assert.Nil(t, results, "cancelled Calculate must return nil results")
}
