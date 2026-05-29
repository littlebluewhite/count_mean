package muscle_ratio

import (
	"math"
	"testing"

	"count_mean/internal/models"
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

func TestBuildRightSideChannelMap_RejectsLeftSide(t *testing.T) {
	// L.RA 在前、R.RA 在後 → 不應該被 L.RA 干擾或覆蓋
	headers := []string{
		"L.RA: EMG 1 (left)",
		"R.RA: EMG 1 (from ...) ->Filter->RMS []",
		"R.ES: EMG 2",
		"R.IL: EMG 3",
		"R.GMax: EMG 4",
		"R.RF: EMG 5",
		"R.BF: EMG 6",
		"R.TA&IO: EMG 7",
		"R.MF: EMG 8",
	}

	cm, err := BuildRightSideChannelMap(headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cm["RA"]; got != "R.RA: EMG 1 (from ...) ->Filter->RMS []" {
		t.Errorf("cm[RA] = %q, want the R.RA header (not L.RA)", got)
	}

	// 全部 L. → fail-fast
	leftOnly := []string{
		"L.RA: x", "L.ES: x", "L.IL: x", "L.GMax: x",
		"L.RF: x", "L.BF: x", "L.TA&IO: x", "L.MF: x",
	}

	if _, err := BuildRightSideChannelMap(leftOnly); err == nil {
		t.Fatalf("expected error for left-only headers, got nil")
	}
}

func TestBuildRightSideChannelMap_RejectsFullName(t *testing.T) {
	// "R RECTUS ABDOMINIS" 沒有 "R." dot prefix（是 "R " 空格），應被視為非右側 EMG header
	headers := []string{
		"R RECTUS ABDOMINIS: EMG 1",
		"R ERECTOR SPINAE: EMG 2",
		"R ILIOPSOAS: EMG 3",
		"R GLUTEUS MAXIMUS: EMG 4",
		"R RECTUS FEMORIS: EMG 5",
		"R BICEPS FEMORIS: EMG 6",
		"R TIBIALIS ANTERIOR: EMG 7",
		"R MULTIFIDUS: EMG 8",
	}

	if _, err := BuildRightSideChannelMap(headers); err == nil {
		t.Fatalf("expected error for full-name headers without R. prefix, got nil")
	}
}

func TestBuildRightSideChannelMap_AllChannels(t *testing.T) {
	// 完整 8 個 R.* 標頭 → 全部映射成功
	headers := []string{
		"R.RA: EMG 1 ()",
		"R.ES: EMG 2 ()",
		"R.IL: EMG 3 ()",
		"R.GMax: EMG 4 ()",
		"R.RF: EMG 5 ()",
		"R.BF: EMG 6 ()",
		"R.TA&IO: EMG 7 ()",
		"R.MF: EMG 8 ()",
	}

	cm, err := BuildRightSideChannelMap(headers)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, m := range []string{"RA", "ES", "IL", "GMax", "RF", "BF", "TAIO", "MF"} {
		if _, ok := cm[m]; !ok {
			t.Errorf("missing channel %q in map: %+v", m, cm)
		}
	}
}

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
			got := WindowMean(tc.series, tc.center, tc.half)
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

	cm, err := BuildRightSideChannelMap(emg.Headers)
	if err != nil {
		t.Fatalf("BuildRightSideChannelMap: %v", err)
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
