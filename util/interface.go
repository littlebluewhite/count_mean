// Package util provides utility functions for the EMG data analysis application,
// including generic numeric operations, memory pooling, and SIMD-optimized calculations.
package util

import "cmp"

// Number is a constraint that permits any integer or floating-point type.
type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

// Ordered is a constraint that permits any ordered type (for comparisons).
// It's an alias to the standard library's cmp.Ordered.
type Ordered = cmp.Ordered
