// Package muscle_ratio computes right-side EMG channel ratios (R.RA/R.ES,
// R.IL/R.GMax, R.RF/R.BF, R.TA&IO/R.MF) over a full EMG recording and at the
// 10 canonical phase points + 9 inter-phase midpoints.
//
// 與 internal/cci 的差異：本套件計算純比值 num/den，不套 Rudolph 公式。右側通道映射
// 由 internal/musclemap.RightSideChannels 提供 (與 cci 共用同一份規則)。
package muscle_ratio

import (
	"math"

	"count_mean/internal/models"
)

// MuscleRatio defines one ratio column: numerator/denominator by short muscle name.
type MuscleRatio struct {
	Name        string // CSV column label, e.g. "RA/ES"
	Numerator   string // short muscle name, e.g. "RA"
	Denominator string // short muscle name, e.g. "ES"
}

// DefaultRatios returns the canonical 4-pair list, mapping to EMG channels (1/2, 3/4, 5/6, 7/8).
// Order is part of the CSV column contract — TestDefaultRatios_OrderLocked guards it.
//
// 每次回 fresh slice：避免 caller 改動共享 state（過去是 mutable package var，無語言層強制
// immutability；//nolint:gochecknoglobals 只是文件而非強制）。
func DefaultRatios() []MuscleRatio {
	return []MuscleRatio{
		{Name: "RA/ES", Numerator: "RA", Denominator: "ES"},
		{Name: "IL/GMax", Numerator: "IL", Denominator: "GMax"},
		{Name: "RF/BF", Numerator: "RF", Denominator: "BF"},
		{Name: "TAIO/MF", Numerator: "TAIO", Denominator: "MF"},
	}
}

// Ratio computes num/den with EMG-specific guard rails:
//   - NaN/±Inf input → NaN
//   - negative input → NaN (rectified+RMS EMG is non-negative by convention)
//   - den == 0 → NaN (CSV writes empty cell)
func Ratio(num, den float64) float64 {
	if math.IsNaN(num) || math.IsNaN(den) ||
		math.IsInf(num, 0) || math.IsInf(den, 0) ||
		num < 0 || den < 0 {
		return math.NaN()
	}

	if den == 0 {
		return math.NaN()
	}

	return num / den
}

// ComputeAllRatios computes the 4 ratio time-series. ratios[k][i] corresponds to
// DefaultRatios[k] at EMG sample i. channelMap must contain all 8 required short names
// (built via musclemap.RightSideChannels).
func ComputeAllRatios(emg *models.PhaseSyncEMGData, channelMap map[string]string) [][]float64 {
	n := len(emg.Time)
	pairs := DefaultRatios()
	ratios := make([][]float64, len(pairs))

	for k, r := range pairs {
		numHeader := channelMap[r.Numerator]
		denHeader := channelMap[r.Denominator]
		numData := emg.Channels[numHeader]
		denData := emg.Channels[denHeader]

		out := make([]float64, n)
		for i := 0; i < n; i++ {
			if i >= len(numData) || i >= len(denData) {
				out[i] = math.NaN()
				continue
			}

			out[i] = Ratio(numData[i], denData[i])
		}

		ratios[k] = out
	}

	return ratios
}
