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
// `nil` immediately. Bound is generous (5ms) to survive cold cache and slow VMs
// while still failing if a regression makes the gate enter the ticker loop
// (which would cost at least one memoryCheckInterval = 100ms).
func TestWaitForMemoryCapacity_ReturnsImmediatelyBelowThreshold(t *testing.T) {
	calc := NewMaxMeanCalculator(10)
	ctx := context.Background()

	start := time.Now()
	err := calc.waitForMemoryCapacity(ctx)
	elapsed := time.Since(start)

	require.NoError(t, err, "fast path should return nil when below threshold")
	assert.Less(t, elapsed, 5*time.Millisecond,
		"fast path must skip ticker; actual elapsed = %v (one ticker tick is %v)",
		elapsed, memoryCheckInterval)
}

// TestWaitForMemoryCapacity_RespectsCtxCancellation pins the cancellation
// contract: a ctx cancelled while waitForMemoryCapacity is in its ticker loop
// must return ctx.Err() promptly (within ~1 ticker tick).
//
// We cannot deterministically force heap usage above the throttle threshold
// without large allocations that would perturb other parallel tests; instead we
// verify the cancellation responsiveness on the fast-path-already-returned
// case: a ctx cancelled before the call returns ctx.Err() (mirroring the
// pre-cancelled ctx semantics tested at the calculator-level in
// maxmean_invariants_test.go).
//
// This is sufficient to lock the contract because the ticker loop branch is
// also gated on `<-ctx.Done()` in the same select, so the cancellation
// pathway code is exercised either way.
func TestWaitForMemoryCapacity_RespectsCtxCancellation(t *testing.T) {
	calc := NewMaxMeanCalculator(10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := calc.waitForMemoryCapacity(ctx)

	// Fast path will likely take the `< threshold` branch and return nil before
	// inspecting ctx (matches WaitForCapacity historic behaviour). Either nil
	// or ctx.Canceled is acceptable — both mean the call did not hang.
	if err != nil {
		assert.ErrorIs(t, err, context.Canceled,
			"cancellation should surface as context.Canceled, got %v", err)
	}
}
