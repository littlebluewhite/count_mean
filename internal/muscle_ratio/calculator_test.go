package muscle_ratio

import (
	"math"
	"testing"

	"count_mean/internal/models"
	"count_mean/internal/musclemap"
)

func TestRatio_NaNOnZeroDenominator(t *testing.T) {
	cases := []struct {
		name  string
		num   float64
		den   float64
		want  float64
		isNaN bool
	}{
		{"normal division", 1.5, 0.5, 3.0, false},
		{"zero denominator", 1.5, 0, 0, true},
		{"zero over zero", 0, 0, 0, true},
		{"zero numerator over positive", 0, 1.5, 0, false},
		{"NaN numerator", math.NaN(), 1.0, 0, true},
		{"NaN denominator", 1.0, math.NaN(), 0, true},
		{"negative numerator", -1.0, 1.0, 0, true},
		{"negative denominator", 1.0, -1.0, 0, true},
		{"+Inf numerator", math.Inf(1), 1.0, 0, true},
		{"-Inf denominator", 1.0, math.Inf(-1), 0, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Ratio(tc.num, tc.den)

			if tc.isNaN {
				if !math.IsNaN(got) {
					t.Fatalf("Ratio(%v, %v) = %v, want NaN", tc.num, tc.den, got)
				}

				return
			}

			if got != tc.want {
				t.Fatalf("Ratio(%v, %v) = %v, want %v", tc.num, tc.den, got, tc.want)
			}
		})
	}
}

// TestDefaultRatios_OrderLocked 釘住 CSV 欄位順序契約：4 個比值對應 EMG 1/2, 3/4, 5/6, 7/8。
// 順序若被改動，下游所有 phases.csv 與 muscle_ratio.csv 的欄位對應會錯位，且因為值本身仍有效
// 不會被任何型別檢查發現，是 silent 重大 bug。
func TestDefaultRatios_OrderLocked(t *testing.T) {
	expected := []MuscleRatio{
		{Name: "RA/ES", Numerator: "RA", Denominator: "ES"},
		{Name: "IL/GMax", Numerator: "IL", Denominator: "GMax"},
		{Name: "RF/BF", Numerator: "RF", Denominator: "BF"},
		{Name: "TAIO/MF", Numerator: "TAIO", Denominator: "MF"},
	}

	got := DefaultRatios()

	if len(got) != len(expected) {
		t.Fatalf("DefaultRatios() length = %d, want %d", len(got), len(expected))
	}

	for i, want := range expected {
		if got[i] != want {
			t.Errorf("DefaultRatios()[%d] = %+v, want %+v", i, got[i], want)
		}
	}
}

func TestComputeAllRatios_NaNAndShape(t *testing.T) {
	emg := &models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.001, 0.002},
		Channels: map[string][]float64{
			"R.RA: EMG 1":    {2.0, 1.0, 0.0},
			"R.ES: EMG 2":    {1.0, 0.0, 1.0}, // index 1 分母 = 0 → NaN
			"R.IL: EMG 3":    {3.0, 3.0, 3.0},
			"R.GMax: EMG 4":  {1.5, 1.5, 1.5},
			"R.RF: EMG 5":    {1.0, 1.0, 1.0},
			"R.BF: EMG 6":    {2.0, 2.0, 2.0},
			"R.TA&IO: EMG 7": {4.0, 4.0, 4.0},
			"R.MF: EMG 8":    {2.0, 2.0, 2.0},
		},
		Headers: []string{
			"R.RA: EMG 1", "R.ES: EMG 2", "R.IL: EMG 3", "R.GMax: EMG 4",
			"R.RF: EMG 5", "R.BF: EMG 6", "R.TA&IO: EMG 7", "R.MF: EMG 8",
		},
	}

	cm, err := musclemap.RightSideChannels(emg.Headers)
	if err != nil {
		t.Fatalf("RightSideChannels: %v", err)
	}

	got := ComputeAllRatios(emg, cm)
	if len(got) != 4 {
		t.Fatalf("ratios length = %d, want 4", len(got))
	}

	// RA/ES at index 0: 2/1 = 2; index 1: 1/0 = NaN; index 2: 0/1 = 0
	if got[0][0] != 2.0 {
		t.Errorf("RA/ES[0] = %v, want 2.0", got[0][0])
	}

	if !math.IsNaN(got[0][1]) {
		t.Errorf("RA/ES[1] (zero denominator) = %v, want NaN", got[0][1])
	}

	if got[0][2] != 0.0 {
		t.Errorf("RA/ES[2] = %v, want 0.0", got[0][2])
	}

	// IL/GMax 應該是 2.0 (3/1.5)
	if got[1][0] != 2.0 {
		t.Errorf("IL/GMax[0] = %v, want 2.0", got[1][0])
	}
}
