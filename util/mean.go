package util

func ArrayMean[T Number](a []T) T {
	var sum T
	l := len(a)
	for i := 0; i < l; i++ {
		sum += a[i]
	}
	return sum / T(l)
}
