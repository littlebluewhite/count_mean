// Package muscle_ratio computes right-side EMG channel ratios (R.RA/R.ES,
// R.IL/R.GMax, R.RF/R.BF, R.TA&IO/R.MF) over a full EMG recording and at the
// 10 canonical phase points + 9 inter-phase midpoints.
//
// 與 internal/cci 的差異：本套件計算純比值 num/den，不套 Rudolph 公式；通道映射
// 限定 "R." 前綴，避免 "L.RA" 與 "R.RA" 互相覆蓋。
package muscle_ratio

import (
	"fmt"
	"math"
	"strings"

	"count_mean/internal/models"
	"count_mean/internal/musclemap"
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

// requiredMuscles lists the 8 short names that must all be present in the channel map.
//
//nolint:gochecknoglobals // domain constants
var requiredMuscles = []string{"RA", "ES", "IL", "GMax", "RF", "BF", "TAIO", "MF"}

// shortNameMap normalizes prefix variants → canonical short name (case-insensitive lookup via upper).
// 保留 package-private map（lookup 不需 copy）；外部僅透過 lookupShortName accessor 取值，
// 避免任何 importer 直接 mutate。
//
//nolint:gochecknoglobals // domain constants
var shortNameMap = map[string]string{
	"RA":    "RA",
	"ES":    "ES",
	"IL":    "IL",
	"GMAX":  "GMax",
	"RF":    "RF",
	"BF":    "BF",
	"TAIO":  "TAIO",
	"TA&IO": "TAIO",
	"MF":    "MF",
}

// lookupShortName returns the canonical short muscle name for a normalized prefix.
// 唯一 caller 是同 package 的 mapHeaderToRightShortName；外部呼叫者請走 BuildRightSideChannelMap。
func lookupShortName(key string) (string, bool) {
	s, ok := shortNameMap[key]
	return s, ok
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

// BuildRightSideChannelMap maps the 8 right-side EMG channels (short → full header).
//
// 與 cci.BuildChannelMap 不同的是：本函式僅接受 "R." 前綴的標頭（例如 "R.RA: EMG 1 ..."）；
// 看到 "L.XXX" 或沒有 "R." 前綴的標頭一律跳過，避免 "R.RA" 與 "L.RA" 都被映射到 "RA" 互覆蓋。
// 必要 8 個肌肉（RA/ES/IL/GMax/RF/BF/TAIO/MF）任一缺失即 fail-fast。
//
//nolint:err113 // dynamic errors for user-facing output
func BuildRightSideChannelMap(headers []string) (map[string]string, error) {
	channelMap := make(map[string]string, len(headers))

	for _, header := range headers {
		short := mapHeaderToRightShortName(header)
		if short == "" {
			continue
		}

		// 偵測重複 short name 即 error,避免「兩個 R.RA: ...」silent
		// overwrite 造成下游用錯通道。改用 musclemap.AssignShort 共用 helper —
		// 與 cci.BuildChannelMap 對稱,行為由單一實作保證。
		if err := musclemap.AssignShort(channelMap, short, header); err != nil {
			return nil, err
		}
	}

	for _, name := range requiredMuscles {
		if _, ok := channelMap[name]; !ok {
			return nil, fmt.Errorf("缺少必要的肌肉通道: %s", name)
		}
	}

	return channelMap, nil
}

// mapHeaderToRightShortName extracts the canonical short name from a right-side EMG header.
// Returns "" if the header does not start with "R." prefix or the muscle name is unknown.
//
// Examples:
//
//	"R.RA: EMG 1 (from ...) ->Filter->RMS []" → "RA"
//	"R.TA&IO: EMG 7 (...)"                    → "TAIO"
//	"L.RA: EMG 1 ..."                         → "" (left side skipped)
//	"R RECTUS ABDOMINIS: ..."                 → "" (no "R." dot prefix)
func mapHeaderToRightShortName(header string) string {
	colonIdx := strings.Index(header, ":")
	if colonIdx < 0 {
		return ""
	}

	prefix := strings.TrimSpace(header[:colonIdx])
	if !strings.HasPrefix(prefix, "R.") {
		return ""
	}

	name := strings.ToUpper(prefix[2:])
	if short, ok := lookupShortName(name); ok {
		return short
	}

	return ""
}

// ComputeAllRatios computes the 4 ratio time-series. ratios[k][i] corresponds to
// DefaultRatios[k] at EMG sample i. channelMap must contain all 8 required short names
// (built via BuildRightSideChannelMap).
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
