package util

func ArrayMean(a []float64) float64 {
	var sum float64
	l := len(a)
	for i := 0; i < l; i++ {
		sum += a[i]
	}
	return sum / float64(l)
}
