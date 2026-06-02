package cci

import (
	"math"
	"testing"
)

// TestWeighted25 covers the two-layer window mean algorithm.
// Each numeric case includes a hand-computed expected value in a comment.
//
// Algorithm recap: the window [i-4, i+4] is treated as 7 "quantities":
//   - A (outer-left pair {i-4,i-3}): mean of usable values (or single value, or omitted)
//   - 5 middle indices {i-2,i-1,i,i+1,i+2}: each usable value is one quantity
//   - B (outer-right pair {i+3,i+4}): same rule as A
//
// Result = sum(quantities) / len(quantities), or NaN when no quantities.
func TestWeighted25(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)

	t.Run("G1c discriminator - quadratic series", func(t *testing.T) {
		// series[k] = k*k for k=0..8: [0,1,4,9,16,25,36,49,64], i=4
		// A = mean(series[0],series[1]) = mean(0,1)    = 0.5
		// middle: series[2]=4, [3]=9, [4]=16, [5]=25, [6]=36
		// B = mean(series[7],series[8]) = mean(49,64)  = 56.5
		// 7 quantities {0.5,4,9,16,25,36,56.5} → sum=147.0 / 7 = 21.0
		// WRONG equal-weight-9 gives (0+1+4+9+16+25+36+49+64)/9 = 204/9 ≈ 22.667 — must FAIL
		series := []float64{0, 1, 4, 9, 16, 25, 36, 49, 64}
		got := weighted25(series, 4)
		if math.Abs(got-21.0) > 1e-9 {
			t.Fatalf("G1c: weighted25(k²[0..8], 4) = %.6f, want 21.0 (wrong impl gives 22.667)", got)
		}
	})

	t.Run("left-boundary A omitted (i=0)", func(t *testing.T) {
		// series = [1,2,3,4,5], i=0
		// A: i-4=-4, i-3=-3 → OOB → omitted
		// middle: i-2=-2 OOB, i-1=-1 OOB, series[0]=1, series[1]=2, series[2]=3
		// B: mean(series[3],series[4]) = mean(4,5) = 4.5
		// 4 quantities {1,2,3,4.5} → sum=10.5 / 4 = 2.625
		series := []float64{1, 2, 3, 4, 5}
		got := weighted25(series, 0)
		want := 2.625 // 10.5/4
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("left-boundary: weighted25([1..5], 0) = %.9f, want %.9f", got, want)
		}
	})

	t.Run("right-boundary B omitted (i=n-1)", func(t *testing.T) {
		// series = [1,2,3,4,5], i=4
		// A: mean(series[0],series[1]) = mean(1,2) = 1.5
		// middle: series[2]=3, series[3]=4, series[4]=5
		// B: i+3=7 OOB, i+4=8 OOB → omitted
		// 4 quantities {1.5,3,4,5} → sum=13.5 / 4 = 3.375
		series := []float64{1, 2, 3, 4, 5}
		got := weighted25(series, 4)
		want := 3.375 // 13.5/4
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("right-boundary: weighted25([1..5], 4) = %.9f, want %.9f", got, want)
		}
	})

	t.Run("A single-usable (i-4 is NaN)", func(t *testing.T) {
		// series = [NaN,2,3,4,5,6,7,8,9], i=4
		// A: series[0]=NaN skip, series[1]=2 → A = 2.0 (single value, not mean of pair)
		// middle: series[2]=3, [3]=4, [4]=5, [5]=6, [6]=7
		// B: mean(series[7],series[8]) = mean(8,9) = 8.5
		// 7 quantities {2,3,4,5,6,7,8.5} → sum=35.5 / 7 = 5.071428571...
		series := []float64{nan, 2, 3, 4, 5, 6, 7, 8, 9}
		got := weighted25(series, 4)
		want := 35.5 / 7 // ≈ 5.071428571
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("A-single-usable: weighted25([NaN,2..9], 4) = %.9f, want %.9f (35.5/7)", got, want)
		}
	})

	t.Run("B single-usable (i+3 is NaN)", func(t *testing.T) {
		// series = [1,2,3,4,5,6,7,NaN,11], i=4 — mirror of the A-single case on the B pair
		// A: mean(series[0],series[1]) = mean(1,2) = 1.5
		// middle: series[2]=3, [3]=4, [4]=5, [5]=6, [6]=7
		// B: series[7]=NaN skip, series[8]=11 → B = 11.0 (single value, not mean of pair)
		// 7 quantities {1.5,3,4,5,6,7,11} → sum=37.5 / 7 ≈ 5.357142857
		series := []float64{1, 2, 3, 4, 5, 6, 7, nan, 11}
		got := weighted25(series, 4)
		want := 37.5 / 7 // ≈ 5.357142857
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("B-single-usable: weighted25([1..7,NaN,11], 4) = %.9f, want %.9f (37.5/7)", got, want)
		}
	})

	t.Run("middle NaN skip", func(t *testing.T) {
		// series = [1,2,NaN,4,5,6,7,8,9], i=4
		// A: mean(series[0],series[1]) = mean(1,2) = 1.5
		// middle: series[2]=NaN skip, [3]=4, [4]=5, [5]=6, [6]=7
		// B: mean(series[7],series[8]) = mean(8,9) = 8.5
		// 6 quantities {1.5,4,5,6,7,8.5} → sum=32.0 / 6 = 16/3 ≈ 5.333...
		series := []float64{1, 2, nan, 4, 5, 6, 7, 8, 9}
		got := weighted25(series, 4)
		want := 32.0 / 6 // = 16/3 ≈ 5.333333333
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("middle-NaN-skip: weighted25([1,2,NaN,4..9], 4) = %.9f, want %.9f (32/6)", got, want)
		}
	})

	t.Run("all unusable returns NaN", func(t *testing.T) {
		// All NaN: no quantities collected → NaN
		series := []float64{nan, nan, nan, nan, nan}
		got := weighted25(series, 2)
		if !math.IsNaN(got) {
			t.Fatalf("all-NaN: weighted25([NaN×5], 2) = %v, want NaN", got)
		}
	})

	t.Run("i so far out of bounds nothing usable returns NaN", func(t *testing.T) {
		// series=[1,2,3], i=100: all window indices OOB → NaN
		series := []float64{1, 2, 3}
		got := weighted25(series, 100)
		if !math.IsNaN(got) {
			t.Fatalf("OOB-i: weighted25([1,2,3], 100) = %v, want NaN", got)
		}
	})

	t.Run("Inf skip same as NaN skip", func(t *testing.T) {
		// series = [1,2,3,Inf,5,6,7,8,9], i=4
		// A: mean(series[0],series[1]) = mean(1,2) = 1.5
		// middle: series[2]=3, [3]=Inf skip, [4]=5, [5]=6, [6]=7
		// B: mean(series[7],series[8]) = mean(8,9) = 8.5
		// 6 quantities {1.5,3,5,6,7,8.5} → sum=31.0 / 6 ≈ 5.1666...
		series := []float64{1, 2, 3, inf, 5, 6, 7, 8, 9}
		got := weighted25(series, 4)
		want := 31.0 / 6 // ≈ 5.166666...
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("Inf-skip: weighted25([1,2,3,Inf,5..9], 4) = %.9f, want %.9f (31/6)", got, want)
		}
	})
}
