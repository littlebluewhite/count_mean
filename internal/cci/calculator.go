package cci

import (
	"context"
	"errors"
	"fmt"
	"math"

	"count_mean/internal/i18n"
	"count_mean/internal/musclemap"
)

// cciCancelCheckInterval 控制 CalculateCCITimeSeries 在 hot loop 多久檢查一次 ctx.Done()。
// 4096 與 internal/calculator/sliding_window.slidingWindowCtxCheckInterval 對齊：
// throughput / cancel-latency 折衷，300k 點上 mid-flight cancel 在 < 1s 內回應，
// 同時 hot-loop overhead 約 0.02%（每 4096 次 +1 select）。
const cciCancelCheckInterval = 4096

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
	TimeValues    []float64          // actual time in seconds per data point
	PhasePercents map[string]float64 // phase name -> % position in gait cycle
	PhaseTimes    map[string]float64 // phase name -> actual time in seconds
	GaitStartTime float64            // actual EMG time at gait cycle 0%
	GaitEndTime   float64            // actual EMG time at gait cycle 100%
	PhaseStats    []CCIPhaseStatRow  // ADR-0018 Output 2: fixed 32-row phase-window statistics
}

// gaitPercent 把 EMG 時間 t 換算成 [[Gait cycle (CCI)]] 百分比:0% = start、100% = end。
// 不 clamp(<0% / >100% 為 ±150ms 延伸範圍的合法值);運算順序固定為
// (t - start) / (end - start) * 100,CSV(%.2f)與圖表軸標(%.1f)的輸出位元組依賴它。
func gaitPercent(t, start, end float64) float64 {
	return (t - start) / (end - start) * 100
}

// GaitPercent 回傳 EMG 時間 t 在本結果 gait cycle 上的百分比(不 clamp)。
// 先經 Validate 才保證 duration > 0 且為有限值。見 [[ADR-0053]]。
func (r *CCIAnalysisResult) GaitPercent(t float64) float64 {
	return gaitPercent(t, r.GaitStartTime, r.GaitEndTime)
}

// Validate 檢查結果的不變式(ADR-0053),computeCCI、CSV writer、chart 共用同一份:
//   - 非 nil(ErrNilResult)
//   - GaitStartTime / GaitEndTime 為有限值且 duration > 0(ErrInvalidGaitCycle)
//   - 每個 PairResults[i].Values 長度 == len(TimeValues)(ErrPairLengthMismatch)
func (r *CCIAnalysisResult) Validate() error {
	if r == nil {
		return ErrNilResult
	}

	// NaN 與 +Inf 會讓 `d <= 0` 恆為 false 而 silently 通過,先擋非有限值。
	if math.IsNaN(r.GaitStartTime) || math.IsInf(r.GaitStartTime, 0) ||
		math.IsNaN(r.GaitEndTime) || math.IsInf(r.GaitEndTime, 0) {
		return fmt.Errorf("%w: start=%v end=%v(含非有限值)",
			ErrInvalidGaitCycle, r.GaitStartTime, r.GaitEndTime)
	}

	if d := r.GaitEndTime - r.GaitStartTime; d <= 0 {
		return fmt.Errorf("%w: start=%v end=%v",
			ErrInvalidGaitCycle, r.GaitStartTime, r.GaitEndTime)
	}

	timeLen := len(r.TimeValues)
	for i, pr := range r.PairResults {
		if len(pr.Values) != timeLen {
			return fmt.Errorf("%w: pair[%d] %q values=%d, time=%d",
				ErrPairLengthMismatch, i, pr.PairName, len(pr.Values), timeLen)
		}
	}

	return nil
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
// 負值輸入回傳 math.NaN()，使下游 (io.WriteCCIResult 等) 可以偵測並回報錯誤，
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
func CalculateCCITimeSeries(ctx context.Context, ch1Data, ch2Data []float64) ([]float64, error) {
	if len(ch1Data) != len(ch2Data) {
		return nil, i18n.NewError(i18n.KeyErrorCCIChannelLenMismatch, len(ch1Data), len(ch2Data))
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

// BuildChannelMap maps short muscle names to their actual header strings
// as stored in PhaseSyncEMGData.Channels。規則 (僅右側、重複 fail-fast、缺任一必要肌肉
// fail-fast) 由 musclemap.RightSideChannels 擁有;本函式只把缺失錯誤轉成帶 i18n key 的錯誤。
func BuildChannelMap(headers []string) (map[string]string, error) {
	channelMap, err := musclemap.RightSideChannels(headers)
	if err != nil {
		var missing *musclemap.MissingMuscleError
		if errors.As(err, &missing) {
			return nil, i18n.NewError(i18n.KeyErrorCCIMissingMuscleChannel, missing.Muscle)
		}

		return nil, err
	}

	return channelMap, nil
}
