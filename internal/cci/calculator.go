package cci

import (
	"fmt"
	"math"
	"strings"
)

// Gait cycle normalization constants.
const (
	GaitCyclePoints = 101 // 0% to 100% inclusive
)

// MusclePair defines a pair of EMG channels for CCI calculation.
type MusclePair struct {
	Name    string // e.g., "RA/ES"
	Muscle1 string // short name: "RA"
	Muscle2 string // short name: "ES"
}

// CCIResult holds the CCI calculation result for one muscle pair.
type CCIResult struct {
	PairName string
	Values   []float64 // CCI time-series values; length aligned with CCIAnalysisResult.TimeValues
}

// CCIAnalysisResult holds the complete analysis result.
type CCIAnalysisResult struct {
	Subject       string
	PairResults   []CCIResult
	TimeValues    []float64            // actual time in seconds per data point
	PhasePercents map[string]float64   // phase name -> % position in gait cycle
	PhaseTimes    map[string]float64   // phase name -> actual time in seconds
	MeanCurves    map[string][]float64 // pair name -> mean CCI curve
	GaitStartTime float64              // actual EMG time at gait cycle 0%
	GaitEndTime   float64              // actual EMG time at gait cycle 100%
}

// DefaultMusclePairs returns the 12 standard muscle pair definitions.
func DefaultMusclePairs() []MusclePair {
	return []MusclePair{
		{Name: "RA/ES", Muscle1: "RA", Muscle2: "ES"},
		{Name: "IL/GMax", Muscle1: "IL", Muscle2: "GMax"},
		{Name: "RF/BF", Muscle1: "RF", Muscle2: "BF"},
		{Name: "TAIO/MF", Muscle1: "TAIO", Muscle2: "MF"},
		{Name: "RA/GMax", Muscle1: "RA", Muscle2: "GMax"},
		{Name: "RA/MF", Muscle1: "RA", Muscle2: "MF"},
		{Name: "IL/ES", Muscle1: "IL", Muscle2: "ES"},
		{Name: "IL/BF", Muscle1: "IL", Muscle2: "BF"},
		{Name: "IL/MF", Muscle1: "IL", Muscle2: "MF"},
		{Name: "RF/GMax", Muscle1: "RF", Muscle2: "GMax"},
		{Name: "TAIO/ES", Muscle1: "TAIO", Muscle2: "ES"},
		{Name: "TAIO/GMax", Muscle1: "TAIO", Muscle2: "GMax"},
	}
}

// CalculateCCIRudolph computes CCI for a single time point.
// Formula: CCI = (EMG_s / EMG_l) * (EMG_s + EMG_l)
// where EMG_s is the smaller value and EMG_l is the larger value.
//
// Rudolph 公式假設輸入為 rectified EMG（非負、有限數值）。對於 NaN / ±Inf /
// 負值輸入回傳 math.NaN()，使下游 (writeCSVFile 等) 可以偵測並回報錯誤，
// 避免污染統計結果。
func CalculateCCIRudolph(emg1, emg2 float64) float64 {
	if math.IsNaN(emg1) || math.IsNaN(emg2) ||
		math.IsInf(emg1, 0) || math.IsInf(emg2, 0) ||
		emg1 < 0 || emg2 < 0 {
		return math.NaN()
	}

	emgS := math.Min(emg1, emg2)
	emgL := math.Max(emg1, emg2)

	if emgL == 0 {
		return 0
	}

	return (emgS / emgL) * (emgS + emgL)
}

// CalculateCCITimeSeries computes CCI Rudolph for two channel data arrays.
//
//nolint:err113 // dynamic error for user-facing output
func CalculateCCITimeSeries(ch1Data, ch2Data []float64) ([]float64, error) {
	if len(ch1Data) != len(ch2Data) {
		return nil, fmt.Errorf("通道數據長度不一致: %d vs %d", len(ch1Data), len(ch2Data))
	}

	result := make([]float64, len(ch1Data))
	for i := range ch1Data {
		result[i] = CalculateCCIRudolph(ch1Data[i], ch2Data[i])
	}

	return result, nil
}

// shortNameMap maps normalized prefixes to standard short muscle names.
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

// MapHeaderToShortName extracts the short muscle name from an EMG header.
// e.g., "R.RA: EMG 1 (from ...)" -> "RA"
// e.g., "R.TA&IO: EMG 7 (...)" -> "TAIO"
// e.g., "R.GMax: EMG 4 (...)" -> "GMax"
func MapHeaderToShortName(header string) string {
	// Extract part before ":"
	colonIdx := strings.Index(header, ":")
	if colonIdx < 0 {
		return header
	}

	prefix := strings.TrimSpace(header[:colonIdx])

	// Strip "R." or "L." prefix
	if len(prefix) > 2 && prefix[1] == '.' {
		prefix = prefix[2:]
	}

	// Normalize and look up
	upper := strings.ToUpper(prefix)
	if short, ok := shortNameMap[upper]; ok {
		return short
	}

	// Also try the original case for partial matches
	if short, ok := shortNameMap[prefix]; ok {
		return short
	}

	return prefix
}

// BuildChannelMap maps short muscle names to their actual header strings
// as stored in PhaseSyncEMGData.Channels.
//
//nolint:err113 // dynamic error for user-facing output
func BuildChannelMap(headers []string) (map[string]string, error) {
	channelMap := make(map[string]string, len(headers))

	for _, header := range headers {
		shortName := MapHeaderToShortName(header)
		channelMap[shortName] = header
	}

	required := []string{"RA", "ES", "IL", "GMax", "RF", "BF", "TAIO", "MF"}
	for _, name := range required {
		if _, ok := channelMap[name]; !ok {
			return nil, fmt.Errorf("缺少必要的肌肉通道: %s", name)
		}
	}

	return channelMap, nil
}
