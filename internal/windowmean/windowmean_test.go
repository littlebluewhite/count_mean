package windowmean_test

import (
	"math"
	"testing"

	"count_mean/internal/windowmean"
)

func TestWindowMean(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)
	cases := []struct {
		name    string
		series  []float64
		center  int
		half    int
		wantNaN bool
		want    float64
	}{
		{
			name:   "happy path all-finite window",
			series: []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			center: 2, half: 1,
			// window [1,3]: 2+3+4 = 9 / 3 = 3.0
			wantNaN: false, want: 3.0,
		},
		{
			name:   "clamp-low partial window center=0",
			series: []float64{10.0, 1.0, 2.0, 3.0},
			center: 0, half: 1,
			// lo=-1 clamps to 0, hi=1: window [0,1] = (10+1)/2 = 5.5
			wantNaN: false, want: 5.5,
		},
		{
			name:   "clamp-high partial window center=n-1",
			series: []float64{1.0, 2.0, 3.0, 10.0},
			center: 3, half: 1,
			// lo=2, hi=4 clamps to 3: window [2,3] = (3+10)/2 = 6.5
			wantNaN: false, want: 6.5,
		},
		{
			name:   "skip-NaN window contains some NaN",
			series: []float64{1.0, nan, 3.0, nan, 5.0},
			center: 2, half: 2,
			// window [0,4]: 1+NaN+3+NaN+5 → finite: 1+3+5=9 / 3 = 3.0
			wantNaN: false, want: 3.0,
		},
		{
			name:   "all-NaN every entry in window is NaN",
			series: []float64{nan, nan, nan},
			center: 1, half: 1,
			wantNaN: true,
		},
		{
			name:   "skip-Inf window contains +Inf",
			series: []float64{2.0, inf, 4.0},
			center: 1, half: 1,
			// window [0,2]: 2+Inf+4 → finite: 2+4=6 / 2 = 3.0 (Inf skipped, ADR-0014)
			wantNaN: false, want: 3.0,
		},
		{
			name:   "skip mixed NaN and Inf",
			series: []float64{1.0, nan, inf, 5.0},
			center: 1, half: 2,
			// window [0,3]: 1+NaN+Inf+5 → finite: 1+5=6 / 2 = 3.0
			wantNaN: false, want: 3.0,
		},
		{
			name:   "all-Inf window is empty of finite values",
			series: []float64{inf, inf},
			center: 0, half: 1,
			wantNaN: true,
		},
		{
			name:   "empty series nil",
			series: nil,
			center: 0, half: 5,
			wantNaN: true,
		},
		{
			name:   "empty series empty slice",
			series: []float64{},
			center: 0, half: 5,
			wantNaN: true,
		},
		{
			name:   "single element",
			series: []float64{7.0},
			center: 0, half: 0,
			wantNaN: false, want: 7.0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := windowmean.WindowMean(tc.series, tc.center, tc.half)
			if tc.wantNaN {
				if !math.IsNaN(got) {
					t.Fatalf("WindowMean(%v, %d, %d) = %v, want NaN", tc.series, tc.center, tc.half, got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("WindowMean(%v, %d, %d) = %v, want %v", tc.series, tc.center, tc.half, got, tc.want)
			}
		})
	}
}

func TestMeanRange(t *testing.T) {
	nan := math.NaN()
	inf := math.Inf(1)

	t.Run("explicit valid range", func(t *testing.T) {
		// MeanRange(series, 1, 3) over [1,2,3,4,5]: elements at indices 1,2,3 = 2+3+4 = 9/3 = 3.0
		got := windowmean.MeanRange([]float64{1.0, 2.0, 3.0, 4.0, 5.0}, 1, 3)
		if got != 3.0 {
			t.Fatalf("MeanRange([1,2,3,4,5], 1, 3) = %v, want 3.0", got)
		}
	})

	// lo > hi → NaN (inverted range from out-of-bounds center or D/O motion-index inversion).
	// This is the discriminator between MeanRange and a naive slice: no cross-clamping means
	// an already-inverted [lo, hi] pair yields an empty loop → count==0 → NaN.
	t.Run("lo > hi inverted range yields NaN", func(t *testing.T) {
		// lo=3, hi=1: loop body never executes → count==0 → NaN
		got := windowmean.MeanRange([]float64{1.0, 2.0, 3.0, 4.0, 5.0}, 3, 1)
		if !math.IsNaN(got) {
			t.Fatalf("MeanRange([1..5], 3, 1) = %v, want NaN (lo > hi)", got)
		}
	})

	// Out-of-bounds center + large half: WindowMean would clamp hi to n-1, but MeanRange
	// receives raw lo/hi. lo=8 > hi (clamped to 4) for series of length 5 → empty loop → NaN.
	t.Run("out-of-bounds lo > clamped hi yields NaN", func(t *testing.T) {
		// lo=8, hi=10 → hi clamped to 4, lo stays 8 → 8 > 4 → NaN
		got := windowmean.MeanRange([]float64{1.0, 2.0, 3.0, 4.0, 5.0}, 8, 10)
		if !math.IsNaN(got) {
			t.Fatalf("MeanRange(len5, 8, 10) = %v, want NaN (lo 8 > hi clamped 4)", got)
		}
	})

	t.Run("skip NaN entries", func(t *testing.T) {
		got := windowmean.MeanRange([]float64{1.0, nan, 3.0}, 0, 2)
		if got != 2.0 {
			t.Fatalf("MeanRange([1,NaN,3], 0, 2) = %v, want 2.0", got)
		}
	})

	t.Run("skip Inf entries", func(t *testing.T) {
		got := windowmean.MeanRange([]float64{2.0, inf, 4.0}, 0, 2)
		if got != 3.0 {
			t.Fatalf("MeanRange([2,Inf,4], 0, 2) = %v, want 3.0", got)
		}
	})

	t.Run("all NaN in range yields NaN", func(t *testing.T) {
		got := windowmean.MeanRange([]float64{nan, nan}, 0, 1)
		if !math.IsNaN(got) {
			t.Fatalf("MeanRange([NaN,NaN], 0, 1) = %v, want NaN", got)
		}
	})

	t.Run("all Inf in range yields NaN", func(t *testing.T) {
		got := windowmean.MeanRange([]float64{inf, inf}, 0, 1)
		if !math.IsNaN(got) {
			t.Fatalf("MeanRange([Inf,Inf], 0, 1) = %v, want NaN", got)
		}
	})

	t.Run("empty series nil yields NaN", func(t *testing.T) {
		got := windowmean.MeanRange(nil, 0, 0)
		if !math.IsNaN(got) {
			t.Fatalf("MeanRange(nil, 0, 0) = %v, want NaN", got)
		}
	})

	t.Run("empty series empty slice yields NaN", func(t *testing.T) {
		got := windowmean.MeanRange([]float64{}, 0, 0)
		if !math.IsNaN(got) {
			t.Fatalf("MeanRange([], 0, 0) = %v, want NaN", got)
		}
	})

	t.Run("clamp lo up from negative", func(t *testing.T) {
		// lo=-2 clamps to 0, hi=1: (10+1)/2 = 5.5
		got := windowmean.MeanRange([]float64{10.0, 1.0, 2.0}, -2, 1)
		if got != 5.5 {
			t.Fatalf("MeanRange([10,1,2], -2, 1) = %v, want 5.5", got)
		}
	})

	t.Run("clamp hi down from beyond end", func(t *testing.T) {
		// lo=1, hi=99 clamps to 2: (2+3)/2 = 2.5
		got := windowmean.MeanRange([]float64{1.0, 2.0, 3.0}, 1, 99)
		if got != 2.5 {
			t.Fatalf("MeanRange([1,2,3], 1, 99) = %v, want 2.5", got)
		}
	})
}
