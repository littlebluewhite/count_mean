package util

// ArrayMax returns the maximum value in a slice and its index.
//
//nolint:ireturn // Generic function must return interface type T
func ArrayMax[T Number](data []T) (T, int) {
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
