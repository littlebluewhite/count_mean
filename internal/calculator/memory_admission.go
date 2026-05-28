package calculator

import (
	"context"
	"runtime"
	"time"
)

// Memory admission gate constants. Inlined from removed BackpressureController per ADR-0006.
// Threshold/throttle ratios stay fixed defaults; production never overrode them (zero callers
// of the previous DefaultBackpressureConfig() outside calculator), so the config struct + sweep
// logic were pure ceremony.
const (
	defaultMaxMemoryMB       uint64        = 1024
	defaultMemoryThreshold   float64       = 0.8
	defaultThrottleThreshold float64       = 0.9
	memoryCheckInterval      time.Duration = 100 * time.Millisecond
)

// memoryUsageRatio returns the current heap usage as a fraction of the
// defaultMaxMemoryMB budget. Pure read of runtime.MemStats — no shared state,
// safe to call from any goroutine without a mutex.
func memoryUsageRatio(memStats *runtime.MemStats) float64 {
	maxBytes := defaultMaxMemoryMB * 1024 * 1024
	return float64(memStats.Alloc) / float64(maxBytes)
}

// waitForMemoryCapacity gates a new job's admission on the heap staying below
// defaultThrottleThreshold. Mirrors the previous BackpressureController.WaitForCapacity
// semantics: this is an admission throttle for *new* jobs only — already in-flight
// goroutines are not paused. When ratio < threshold, returns nil immediately.
// Otherwise polls every memoryCheckInterval until either the ratio drops or ctx
// is cancelled (in which case ctx.Err() is returned).
func (c *MaxMeanCalculator) waitForMemoryCapacity(ctx context.Context) error {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	if memoryUsageRatio(&memStats) < defaultThrottleThreshold {
		return nil
	}

	ticker := time.NewTicker(memoryCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			runtime.ReadMemStats(&memStats)
			if memoryUsageRatio(&memStats) < defaultThrottleThreshold {
				return nil
			}
			runtime.Gosched()
		}
	}
}
