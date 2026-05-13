// Package util provides utility functions for the EMG data analysis application,
// including generic numeric operations (ArrayMax / ArrayMean), file permission
// constants, BOM helpers, precision handling, and string-to-number conversion.
package util

import "cmp"

// Number is a constraint that permits any integer or floating-point type.
// 適用於 max / min / sum 等 operations，這些對整數型別無精度損失。
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// Float 是 Number 的浮點子集。
// 適用於 ArrayMean 等含除法的 operations，避免整數截斷（[]int{1,2} → 1 而非 1.5）。
// 所有 production callers 都傳 []float64，此 constraint 將潛在 bug 在編譯期擋下。
type Float interface {
	~float32 | ~float64
}

// Ordered is a constraint that permits any ordered type (for comparisons).
// It's an alias to the standard library's cmp.Ordered.
type Ordered = cmp.Ordered
