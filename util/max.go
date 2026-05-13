package util

// ArrayMax returns the maximum value in a slice and its index.
//
// Precondition: len(data) > 0. Empty input panics with a clear message because
// max is undefined for the empty set and the previous implementation would have
// panicked with an opaque index-out-of-range on data[0] (cross-compare review P2 —
// this guard documents the precondition explicitly and fails loudly).
//
//nolint:ireturn // Generic function must return interface type T
func ArrayMax[T Number](data []T) (T, int) {
	if len(data) == 0 {
		panic("util.ArrayMax: empty slice — caller must guard len(data) > 0")
	}

	maxVal := data[0]
	maxIdx := 0

	for i, value := range data {
		if value > maxVal {
			maxVal = value
			maxIdx = i
		}
	}

	return maxVal, maxIdx
}
