package cci

import (
	"context"
	"errors"
	"math"
	"strings"

	"count_mean/internal/i18n"
	"count_mean/internal/musclemap"
)

// cciCancelCheckInterval 控制 CalculateCCITimeSeries 在 hot loop 多久檢查一次 ctx.Done()。
// 4096 與 internal/calculator/sliding_window.slidingWindowCtxCheckInterval 對齊：
// throughput / cancel-latency 折衷，300k 點上 mid-flight cancel 在 < 1s 內回應，
// 同時 hot-loop overhead 約 0.02%（每 4096 次 +1 select）。
const cciCancelCheckInterval = 4096

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
	PhaseStats    []CCIPhaseStatRow    // ADR-0018 Output 2: fixed 32-row phase-window statistics
}

// CCIPhaseStatRow is one row of the phase-window statistics table (ADR-0018 Output 2).
// Values has one entry per result.PairResults entry, in PairResults order; NaN where a
// window has no usable value or the phase point is absent.
type CCIPhaseStatRow struct {
	Item    string    // interval label ("S-C") or bare phase point ("S")
	Metric  string    // window description ("中點±50ms", "±50ms", "±25ms", "前100ms", "落地0~100ms", ...)
	Time    float64   // EMG time of the phase point, or the interval midpoint sample; meaningful only when HasTime
	HasTime bool      // true for point/landing rows of a PRESENT point; false for absent points and interval rows with an absent endpoint
	Values  []float64 // len == len(result.PairResults); per-pair window statistic
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
// ctx 在 hot loop 每 cciCancelCheckInterval 點檢查一次 ctx.Done()，caller cancel
// 時提早回傳 ctx.Err()。pre-cancelled ctx 在第一次進入 loop 前就會被偵測；
// mid-flight cancel 最壞情況等下一個 4096 邊界。
//
//nolint:err113 // dynamic error for user-facing output (i18n-backed)
func CalculateCCITimeSeries(ctx context.Context, ch1Data, ch2Data []float64) ([]float64, error) {
	if len(ch1Data) != len(ch2Data) {
		return nil, errors.New(
			i18n.T(i18n.KeyErrorCCIChannelLenMismatch, len(ch1Data), len(ch2Data)))
	}

	// Fast pre-cancel：若 caller 已取消 ctx，避免分配 result slice 與第一次計算。
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	result := make([]float64, len(ch1Data))
	for i := range ch1Data {
		// 每 cciCancelCheckInterval 次迭代檢查 ctx；用 select-default 避免每輪都進
		// runtime select scheduler，hot loop overhead 可忽略。i=0 跳過（已 pre-check）。
		if i > 0 && i%cciCancelCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

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

// MapHeaderToShortName extracts the canonical short muscle name from a right-side EMG header.
// 行為與 muscle_ratio.mapHeaderToRightShortName 對齊 — 跨 analyzer 對同份 EMG 取同一組通道。
// 非 "R." 前綴的 header（含 "L." 與其他）一律回空字串，由 BuildChannelMap 跳過。
//
// Examples:
//
//	"R.RA: EMG 1 (from ...) ->Filter->RMS []" → "RA"
//	"R.TA&IO: EMG 7 (...)"                    → "TAIO"
//	"R.GMax: EMG 4 (...)"                     → "GMax"
//	"L.RA: EMG 1 ..."                         → ""  (左側 skipped)
//	"R RECTUS ABDOMINIS: ..."                 → ""  (無 "R." 點前綴)
//	"EMG without colon"                       → ""  (格式不符)
func MapHeaderToShortName(header string) string {
	colonIdx := strings.Index(header, ":")
	if colonIdx < 0 {
		return ""
	}

	prefix := strings.TrimSpace(header[:colonIdx])
	if !strings.HasPrefix(prefix, "R.") {
		return ""
	}

	upper := strings.ToUpper(prefix[2:])
	if short, ok := shortNameMap[upper]; ok {
		return short
	}

	return ""
}

// BuildChannelMap maps short muscle names to their actual header strings
// as stored in PhaseSyncEMGData.Channels. 與 muscle_ratio.BuildRightSideChannelMap 對稱：
// 僅取右側通道，缺任一必要肌肉即 fail-fast。
//
// 加入重複偵測,與 muscle_ratio.BuildRightSideChannelMap 對稱 — 兩個 R.RA
// 過去 silent overwrite (last-wins),現在透過 musclemap.AssignShort fail-fast。
//
//nolint:err113 // dynamic error for user-facing output (i18n-backed)
func BuildChannelMap(headers []string) (map[string]string, error) {
	channelMap := make(map[string]string, len(headers))

	for _, header := range headers {
		shortName := MapHeaderToShortName(header)
		if shortName == "" {
			continue
		}

		if err := musclemap.AssignShort(channelMap, shortName, header); err != nil {
			return nil, err
		}
	}

	required := []string{"RA", "ES", "IL", "GMax", "RF", "BF", "TAIO", "MF"}
	for _, name := range required {
		if _, ok := channelMap[name]; !ok {
			return nil, errors.New(i18n.T(i18n.KeyErrorCCIMissingMuscleChannel, name))
		}
	}

	return channelMap, nil
}
