package parsers

import (
	"math"
	"testing"
)

// TestClampANCCapacity covers the Wave 5 PR1 hardening against attacker-controlled
// `Duration(Sec.):` × `PreciseRate:` values in ANC headers. Without the clamp the
// product can be +Inf / NaN / negative / absurdly large — feeding straight into
// `make([]float64, 0, capacity)` triggers either panic or OOM.
func TestClampANCCapacity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   float64
		want int
	}{
		// Malicious inputs — must fall back to default.
		{"positive_infinity", math.Inf(1), defaultANCCapacity},
		{"negative_infinity", math.Inf(-1), defaultANCCapacity},
		{"nan", math.NaN(), defaultANCCapacity},
		{"negative", -1.0, defaultANCCapacity},
		{"over_max", float64(maxANCSampleCapacity) + 1, defaultANCCapacity},
		{"absurd_1e308", 1e308, defaultANCCapacity},

		// Edge values — must pass through.
		{"zero", 0.0, 0},
		{"one", 1.0, 1},
		{"at_max", float64(maxANCSampleCapacity), maxANCSampleCapacity},
		{"typical_60s_1khz", 60.0 * 1000.0, 60_000},
		{"long_practice_1hr_10khz", 3600.0 * 10000.0, 36_000_000},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := clampANCCapacity(tc.in)
			if got != tc.want {
				t.Errorf("clampANCCapacity(%v) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestClampANCCapacity_NoPanicOnMaliciousProduct guards against the
// `int(+Inf)` undefined-behaviour path that pre-clamp code would hit:
// `make([]float64, 0, capacity)` would either panic with negative-cap or OOM.
func TestClampANCCapacity_NoPanicOnMaliciousProduct(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("clampANCCapacity panicked on malicious input: %v", r)
		}
	}()

	maliciousValues := []float64{
		math.Inf(1) * math.Inf(1), // Inf
		math.Inf(1) - math.Inf(1), // NaN
		-math.Inf(1),              // -Inf
		1e154 * 1e154,             // overflow to +Inf
		1e154 * -1e154,            // overflow to -Inf
	}

	for _, v := range maliciousValues {
		_ = clampANCCapacity(v) // must not panic
	}
}
