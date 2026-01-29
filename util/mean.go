package util

// ArrayMean calculates the arithmetic mean of a slice of numeric values.
//
//nolint:ireturn // Generic function must return interface type T
func ArrayMean[T Number](data []T) T {
	var sum T

	length := len(data)
	for i := 0; i < length; i++ {
		sum += data[i]
	}

	return sum / T(length)
}
