package cci

import "math"

// usable reports whether series[idx] exists and is a finite number.
func usable(series []float64, idx int) bool {
	if idx < 0 || idx >= len(series) {
		return false
	}
	v := series[idx]
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// weighted25 computes the "±25 ms" two-layer weighted mean centered at index i.
//
// The window spans [i-4, i+4] (9 raw positions), but the two outermost pairs
// are pre-averaged so they each contribute as a single quantity:
//
//   - A (outer-left pair {i-4, i-3}): mean of usable values, single value, or omitted
//   - middle {i-2, i-1, i, i+1, i+2}: each usable value is one quantity
//   - B (outer-right pair {i+3, i+4}): same rule as A
//
// Returns NaN when no quantities were collected.
// Non-finite (NaN/Inf) values are skipped throughout.
func weighted25(series []float64, i int) float64 {
	quantities := make([]float64, 0, 7)

	// A: outer-left pair {i-4, i-3}
	aL, aR := i-4, i-3
	if usable(series, aL) && usable(series, aR) {
		quantities = append(quantities, (series[aL]+series[aR])/2)
	} else if usable(series, aL) {
		quantities = append(quantities, series[aL])
	} else if usable(series, aR) {
		quantities = append(quantities, series[aR])
	}
	// else: both unusable — A omitted entirely

	// Middle: {i-2, i-1, i, i+1, i+2}
	for _, idx := range []int{i - 2, i - 1, i, i + 1, i + 2} {
		if usable(series, idx) {
			quantities = append(quantities, series[idx])
		}
	}

	// B: outer-right pair {i+3, i+4}
	bL, bR := i+3, i+4
	if usable(series, bL) && usable(series, bR) {
		quantities = append(quantities, (series[bL]+series[bR])/2)
	} else if usable(series, bL) {
		quantities = append(quantities, series[bL])
	} else if usable(series, bR) {
		quantities = append(quantities, series[bR])
	}
	// else: both unusable — B omitted entirely

	if len(quantities) == 0 {
		return math.NaN()
	}

	sum := 0.0
	for _, q := range quantities {
		sum += q
	}
	return sum / float64(len(quantities))
}
