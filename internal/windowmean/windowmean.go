// Package windowmean provides the shared window-mean kernel used by muscle_ratio
// (ADR-0014 origin) and promoted for reuse by cci PhaseStats (ADR-0018).
//
// Two functions are exported:
//   - MeanRange: mean over an explicit inclusive index range [lo, hi]
//   - WindowMean: thin wrapper computing lo = center-half, hi = center+half
package windowmean

import "math"

// MeanRange returns the mean of series over the inclusive index range [lo, hi],
// skipping non-finite (NaN/Inf) entries.
//
// Clamping rules:
//   - if len(series) == 0 → NaN
//   - lo < 0 → clamped up to 0
//   - hi > n-1 → clamped down to n-1
//   - no cross-clamping: lo is never clamped down to n-1, hi is never clamped up to 0
//
// Consequence: an already-inverted range (lo > hi — from an out-of-bounds center,
// or D/O motion-index inversion after clamping) yields an empty loop → count==0 → NaN.
// Callers relying on this behavior should document their intent.
//
// Returns NaN when series is empty or the (clamped) window contains no finite value.
func MeanRange(series []float64, lo, hi int) float64 {
	n := len(series)
	if n == 0 {
		return math.NaN()
	}
	if lo < 0 {
		lo = 0
	}
	if hi > n-1 {
		hi = n - 1
	}
	sum, count := 0.0, 0
	for i := lo; i <= hi; i++ {
		v := series[i]
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		sum += v
		count++
	}
	if count == 0 {
		return math.NaN()
	}
	return sum / float64(count)
}

// WindowMean returns the mean of series over the centered window
// [center-half, center+half], clamped to [0, len-1], skipping non-finite (NaN/Inf) entries.
// Returns NaN when series is empty or the clamped window has no finite value. (ADR-0014)
//
// This is a thin wrapper over MeanRange; see MeanRange for full semantics.
func WindowMean(series []float64, center, half int) float64 {
	return MeanRange(series, center-half, center+half)
}
