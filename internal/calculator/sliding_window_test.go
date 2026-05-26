package calculator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staticProvider is a synthetic DataProvider for sliding-window tests. It
// returns deterministic values without dataset allocation overhead and lets
// us scale to large window counts cheaply.
type staticProvider struct {
	length int
}

func (s *staticProvider) GetValue(dataIdx, _ int) float64 { return float64(dataIdx) }
func (s *staticProvider) Length() int                     { return s.length }

// slowProvider stretches per-GetValue cost so mid-flight cancellation tests
// can reliably observe ctx.Done() between 4096-iter checkpoints. The raw
// staticProvider runs ~99k iterations in well under 1ms on a modern CPU,
// finishing before any goroutine-scheduled cancel arrives.
type slowProvider struct {
	length     int
	delayEvery int           // delay one in every N calls
	delay      time.Duration // delay per slowed call
	count      int           // mutated single-threadedly inside CalculateMaxMean
}

func (s *slowProvider) GetValue(dataIdx, _ int) float64 {
	s.count++
	if s.delayEvery > 0 && s.count%s.delayEvery == 0 {
		time.Sleep(s.delay)
	}

	return float64(dataIdx)
}

func (s *slowProvider) Length() int { return s.length }

// TestSlidingWindow_CalculateMaxMean_PreCancelledCtx pins the new contract
// : SlidingWindow.CalculateMaxMean accepts a context and aborts
// when ctx is already cancelled before the first ctx check fires. Note that
// the periodic check happens every 4096 iterations, so the loop may run up
// to that many windows before observing cancel — but it must still return
// the cancel error (not a silent success).
func TestSlidingWindow_CalculateMaxMean_PreCancelledCtx(t *testing.T) {
	sw := NewSlidingWindowCalculator()
	provider := &staticProvider{length: 200_000}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := sw.CalculateMaxMean(ctx, provider, 0, 100, 0, provider.Length()-1)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"expected ctx.Canceled, got %v", err)
	assert.Less(t, elapsed, 1*time.Second,
		"pre-cancelled CalculateMaxMean should return quickly (got %v)", elapsed)
}

// TestSlidingWindow_CalculateMaxMean_MidFlightCancel exercises cancellation
// after the loop is already running. We use a slowProvider that injects a
// short sleep every 1000 GetValue calls so the loop reliably outlives the
// cancel delay; otherwise on a fast machine the entire sliding-window
// computation finishes in < 1ms before the cancel goroutine wakes.
//
// Contract: must return non-nil ctx.Err() within a reasonable bound, not
// finish the full sliding window.
func TestSlidingWindow_CalculateMaxMean_MidFlightCancel(t *testing.T) {
	sw := NewSlidingWindowCalculator()
	provider := &slowProvider{
		length:     50_000,
		delayEvery: 1000,
		delay:      200 * time.Microsecond,
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := sw.CalculateMaxMean(ctx, provider, 0, 100, 0, provider.Length()-1)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"expected ctx.Canceled propagation, got %v", err)
	assert.Less(t, elapsed, 5*time.Second,
		"mid-flight cancelled CalculateMaxMean should return within 5s (got %v)",
		elapsed)
}

// TestSlidingWindow_CancelsOnShortRange 守護 對 < 4096 row 的 trimmed range,
// caller cancel ctx 後 CalculateMaxMean 必須能在合理時間內回傳 ctx.Err(),不能
// 因為 ctx check 卡在「每 4096 次才看一次」的 batch 邊界而錯過 cancel 信號。
//
// 過去實作:if iter%slidingWindowCtxCheckInterval == 0 — 對 500 row 的 range,
// iter 永遠 < 4096,ctx.Done() 永遠不被 poll,中途 cancel 必須等整個 sliding
// window 跑完才回傳 — 雖然 500 row 跑完 < 1ms 在快機上看似 OK,但 contract 上
// 已違反「caller 可隨時取消」的 ctx 義務,且若 provider 是 slow IO-bound
// (例:lazy DB cursor) 就會 unresponsive。
//
// 測試策略:用 slowProvider 注入 sleep 拉長執行時間到 200ms 級,給 100ms
// deadline ctx,assert 在 cancel 後 200ms 內回 ctx.Err。對 500 row range 不能
// 用「整段跑完」掩蓋契約違反。
func TestSlidingWindow_CancelsOnShortRange(t *testing.T) {
	sw := NewSlidingWindowCalculator()
	// 500 row << slidingWindowCtxCheckInterval (4096),每 GetValue 注入 sleep
	// 確保中途 cancel 一定能被觀察到。delayEvery=10 → 50 次 sleep × 4ms = 200ms,
	// 遠超 100ms deadline。
	provider := &slowProvider{
		length:     500,
		delayEvery: 10,
		delay:      4 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := sw.CalculateMaxMean(ctx, provider, 0, 50, 0, provider.Length()-1)
	elapsed := time.Since(start)

	require.Error(t, err, "短 range (<4096 row) 也必須觀察 ctx 取消")
	assert.True(t,
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded),
		"expected ctx.Canceled or DeadlineExceeded, got %v", err)
	assert.Less(t, elapsed, 200*time.Millisecond,
		"短 range cancel 必須在 200ms 內回 (100ms deadline + 容忍),實際 %v", elapsed)
}

// TestSlidingWindow_CalculateMaxMean_NoCancel_HappyPath verifies the
// non-cancellation path still returns the correct max-mean and best start
// index. Pinned values: input values are i = 0..9, window=3, range full.
// Best window is the last (idx 7..9) with mean = (7+8+9)/3 = 8.0.
func TestSlidingWindow_CalculateMaxMean_NoCancel_HappyPath(t *testing.T) {
	sw := NewSlidingWindowCalculator()
	provider := &staticProvider{length: 10}

	result, err := sw.CalculateMaxMean(
		context.Background(), provider, 0, 3, 0, provider.Length()-1,
	)
	require.NoError(t, err)
	assert.Equal(t, 8.0, result.MaxMean)
	assert.Equal(t, 7, result.BestStartIdx)
}
