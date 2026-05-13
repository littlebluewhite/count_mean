package util

// ArrayMean calculates the arithmetic mean of a slice of floating-point values.
//
// Constraint 為 Float 而非 Number：sum / T(len(data)) 對整數型別會無聲截斷
// （[]int{1,2} 應為 1.5 但 T=int 時得到 1），review 發現的 latent bug。所有
// production callers 都傳 []float64，narrow constraint 將該 foot-gun 在編譯期擋下。
//
// Precondition: len(data) > 0. Empty input panics with a clear message because
// arithmetic mean is undefined for the empty set, and silently returning 0 would
// mask caller bugs (cross-compare review P2 — caller was previously relying on
// upstream len-checks to guarantee non-empty input; this guard makes the
// contract explicit and fails loudly when violated).
//
//nolint:ireturn // Generic function must return interface type T
func ArrayMean[T Float](data []T) T {
	if len(data) == 0 {
		panic("util.ArrayMean: empty slice — caller must guard len(data) > 0")
	}

	var sum T
	for i := range data {
		sum += data[i]
	}

	return sum / T(len(data))
}
