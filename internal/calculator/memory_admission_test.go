package calculator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWaitForMemoryCapacity_ReturnsImmediatelyBelowThreshold pins the fast-path
// contract: when heap usage is below defaultThrottleThreshold, the admission gate
// returns nil without entering the ticker loop. Verified by elapsed-time bound
// rather than by counting ticker fires (which would be flaky on slow CI).
//
// Test envs run with allocs well under 1GB * 0.9 = 921MB; the ratio check trips
// `nil` immediately. Bound is generous (50ms) to survive -race + -coverpkg
// instrumentation overhead on shared CI runners while still failing if a
// regression makes the gate enter the ticker loop (which would cost at least
// one memoryCheckInterval = 100ms — so 50ms keeps a 2× safety margin).
func TestWaitForMemoryCapacity_ReturnsImmediatelyBelowThreshold(t *testing.T) {
	calc := NewMaxMeanCalculator(10)
	ctx := context.Background()

	start := time.Now()
	err := calc.waitForMemoryCapacity(ctx)
	elapsed := time.Since(start)

	require.NoError(t, err, "fast path should return nil when below threshold")
	assert.Less(t, elapsed, 50*time.Millisecond,
		"fast path must skip ticker; actual elapsed = %v (one ticker tick is %v)",
		elapsed, memoryCheckInterval)
}

// TestWaitForMemoryCapacity_RespectsCtxCancellation pins the cancellation
// contract: when the helper is forced into the ticker-loop path (threshold=0
// so memoryUsageRatio >= threshold trivially) and ctx is cancelled, the call
// must return ctx.Err() promptly (within ~1 ticker tick = 100ms).
//
// Round-2 codex review pointed out that the earlier version of this test
// called waitForMemoryCapacity with default threshold (0.9), which on a
// normal CI heap falls into the fast-path `< threshold` branch and returns
// nil before inspecting ctx. The previous assertion accepted nil-or-Canceled
// — so the test would still pass even if production code dropped the
// `<-ctx.Done()` case from the ticker-loop select.
//
// Fix: use threshold=0 to deterministically enter the ticker loop without
// allocating real memory pressure (which would perturb sibling parallel
// tests). Now the only path that satisfies the assertion is the
// `<-ctx.Done()` branch returning context.Canceled.
func TestWaitForMemoryCapacity_RespectsCtxCancellation(t *testing.T) {
	calc := NewMaxMeanCalculator(10)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel slightly after entering the ticker loop. We use a goroutine + a
	// small sleep so the test exercises the contended-loop cancellation
	// (rather than the pre-cancelled ctx shortcut, which is already locked
	// down by TestCalculate_PreCancelledContext_DoesNotStartWorkers).
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := calc.waitForMemoryCapacityWithThreshold(ctx, 0)
	elapsed := time.Since(start)

	require.Error(t, err, "ticker-loop path must surface ctx cancellation")
	assert.ErrorIs(t, err, context.Canceled,
		"expected context.Canceled from ticker-loop cancellation, got %v", err)
	// 1 ticker tick is 100ms; allow up to 2× plus scheduling slack.
	assert.Less(t, elapsed, 250*time.Millisecond,
		"cancellation must be observed within ~1 ticker tick; elapsed = %v", elapsed)
}
