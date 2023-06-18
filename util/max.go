package util

func ArrayMax[T Number](a []T) (T, int) {
	max := a[0]
	index := 0
	for i, value := range a {
		if value > max {
			max = value
			index = i
		}
	}
	return max, index
}
