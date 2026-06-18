package cci

import (
	"math"

	"count_mean/internal/models"
	"count_mean/internal/synchronizer"
	"count_mean/internal/windowmean"
)

// Metric labels for the phase-window statistics table (ADR-0018 Output 2).
const (
	metricInterval    = "中點±50ms"
	metricBand50ms    = "±50ms"
	metricBand25ms    = "±25ms"
	metricPre100ms    = "前100ms"
	metricLand0to100  = "落地0~100ms"
	metricLand20to50  = "落地20~50ms"
	metricLand50to100 = "落地50~100ms"
)

// Window point-count offsets. The ms↔index mapping assumes 100 Hz / 10 ms per
// sample: ±5 points = ±50 ms, 10 points back = 100 ms, etc. The math is fixed in
// point counts; estimateSampleInterval only drives a single audit warn (see below).
const (
	band50Half   = 5  // ±50 ms  → MeanRange(i-5, i+5)
	pre100Span   = 10 // 100 ms back → MeanRange(i-10, i)
	land100Span  = 10 // landing 0~100 ms  → MeanRange(iL, iL+10)
	land20Offset = 2  // landing 20~50 ms  → MeanRange(iL+2, iL+5)
	land50Offset = 5  // landing 50~100 ms → MeanRange(iL+5, iL+10)
)

// rowKind selects how a row's per-pair Values are computed.
type rowKind int

const (
	kindInterval rowKind = iota // ±band50Half window around the two endpoints' time-midpoint (ADR-0022)
	kindBand50                  // MeanRange(i-5, i+5) around a single point
	kindBand25                  // weighted25 at a single point
	kindPre100                  // MeanRange(i-10, i) before a single point
	kindLand                    // landing window relative to L (MeanRange(iL+a, iL+b))
)

// phaseStatRowSpec is the declarative description of one of the 32 rows.
//
// Table-driven: the 32 rows are defined as data; computeRow dispatches on
// kind. Interval rows name two endpoints (from/to); point rows name one (point);
// landing rows carry the iL-relative span [landLo, landHi].
type phaseStatRowSpec struct {
	item   string
	metric string
	kind   rowKind

	// Interval rows.
	from models.PhasePoint
	to   models.PhasePoint

	// Single-point rows (band50 / band25 / pre100).
	point models.PhasePoint

	// Landing rows (iL-relative offsets, inclusive).
	landLo int
	landHi int
}

// phaseStatRowSpecs is the EXACT 32-row table (ADR-0018 Output 2), in order:
// 8 interval rows, 14 interleaved ±50ms/±25ms point rows, 7 前100ms point rows,
// 3 L-landing rows. Total 32.
//
//nolint:gochecknoglobals // immutable row schema, read-only at runtime
var phaseStatRowSpecs = []phaseStatRowSpec{
	// INTERVAL rows (中點±50ms). S-D and D-T intentionally skip the intervening point.
	{item: "S-C", metric: metricInterval, kind: kindInterval, from: models.PhaseS, to: models.PhaseC},
	{item: "S-D", metric: metricInterval, kind: kindInterval, from: models.PhaseS, to: models.PhaseD},
	{item: "C-D", metric: metricInterval, kind: kindInterval, from: models.PhaseC, to: models.PhaseD},
	{item: "D-T", metric: metricInterval, kind: kindInterval, from: models.PhaseD, to: models.PhaseT},
	{item: "D-T0", metric: metricInterval, kind: kindInterval, from: models.PhaseD, to: models.PhaseT0},
	{item: "T0-T", metric: metricInterval, kind: kindInterval, from: models.PhaseT0, to: models.PhaseT},
	{item: "T-O", metric: metricInterval, kind: kindInterval, from: models.PhaseT, to: models.PhaseO},
	{item: "O-L", metric: metricInterval, kind: kindInterval, from: models.PhaseO, to: models.PhaseL},

	// PER-POINT ±50ms / ±25ms rows, interleaved per point.
	{item: "S", metric: metricBand50ms, kind: kindBand50, point: models.PhaseS},
	{item: "S", metric: metricBand25ms, kind: kindBand25, point: models.PhaseS},
	{item: "C", metric: metricBand50ms, kind: kindBand50, point: models.PhaseC},
	{item: "C", metric: metricBand25ms, kind: kindBand25, point: models.PhaseC},
	{item: "D", metric: metricBand50ms, kind: kindBand50, point: models.PhaseD},
	{item: "D", metric: metricBand25ms, kind: kindBand25, point: models.PhaseD},
	{item: "T0", metric: metricBand50ms, kind: kindBand50, point: models.PhaseT0},
	{item: "T0", metric: metricBand25ms, kind: kindBand25, point: models.PhaseT0},
	{item: "T", metric: metricBand50ms, kind: kindBand50, point: models.PhaseT},
	{item: "T", metric: metricBand25ms, kind: kindBand25, point: models.PhaseT},
	{item: "O", metric: metricBand50ms, kind: kindBand50, point: models.PhaseO},
	{item: "O", metric: metricBand25ms, kind: kindBand25, point: models.PhaseO},
	{item: "L", metric: metricBand50ms, kind: kindBand50, point: models.PhaseL},
	{item: "L", metric: metricBand25ms, kind: kindBand25, point: models.PhaseL},

	// PRE-100ms rows (前100ms), one per point.
	{item: "S", metric: metricPre100ms, kind: kindPre100, point: models.PhaseS},
	{item: "C", metric: metricPre100ms, kind: kindPre100, point: models.PhaseC},
	{item: "D", metric: metricPre100ms, kind: kindPre100, point: models.PhaseD},
	{item: "T0", metric: metricPre100ms, kind: kindPre100, point: models.PhaseT0},
	{item: "T", metric: metricPre100ms, kind: kindPre100, point: models.PhaseT},
	{item: "O", metric: metricPre100ms, kind: kindPre100, point: models.PhaseO},
	{item: "L", metric: metricPre100ms, kind: kindPre100, point: models.PhaseL},

	// L-LANDING rows (iL-relative). L is always present (B2 fail-fast), so these always compute.
	{item: "L", metric: metricLand0to100, kind: kindLand, point: models.PhaseL, landLo: 0, landHi: land100Span},
	{item: "L", metric: metricLand20to50, kind: kindLand, point: models.PhaseL, landLo: land20Offset, landHi: land50Offset},
	{item: "L", metric: metricLand50to100, kind: kindLand, point: models.PhaseL, landLo: land50Offset, landHi: land100Span},
}

// dropOutOfRangePhases removes phases whose EMG time falls outside the extracted window
// from PhaseTimes + PhasePercents, keeping the chart markers consistent with Output 2:
// an unusable phase (e.g. a manifest typo placing a mid-point beyond L+150ms) neither
// renders a chart marker nor a stats row. Each drop is logged for manifest diagnosis.
// S/L always bound the extracted range, so they are never dropped.
func (a *CCIAnalyzer) dropOutOfRangePhases(result *CCIAnalysisResult) {
	for name, t := range result.PhaseTimes {
		if _, inRange := synchronizer.ResolveTimeIndex(result.TimeValues, t); inRange {
			continue
		}
		delete(result.PhaseTimes, name)
		delete(result.PhasePercents, name)
		if a.logger != nil {
			a.logger.Warn(
				"分期點 EMG time 落在抽取範圍外,已從圖表 marker 與分期統計移除 (請檢查 manifest)",
				map[string]any{"phase": name, "phase_emg_time": t})
		}
	}
}

// buildPhaseStats builds the fixed 32-row phase-window statistics table (ADR-0018
// Output 2). The table always has 32 rows in the documented order; the number of
// columns equals len(result.PairResults) (NOT forced to 12) so Output 2 stays
// aligned with Output 1. Rows for absent phase points (or intervals with an absent
// endpoint) keep their slot with Values = NaN×nPairs and HasTime=false.
// Present interval rows carry HasTime=true with Time set to the endpoints' midpoint sample (ADR-0022).
func (a *CCIAnalyzer) buildPhaseStats(result *CCIAnalysisResult) []CCIPhaseStatRow {
	a.warnIfNon100Hz(result.TimeValues)

	nPairs := len(result.PairResults)

	// phaseAt resolves a present phase point to its index, EMG time, and presence flag
	// (in PhaseTimes AND within the extracted range — both here and dropOutOfRangePhases
	// call synchronizer.ResolveTimeIndex, so Output 2 and the chart markers agree on which
	// phases are usable; its 1e-6 tolerance keeps S/L clamped to the first/last sample
	// present). Defense-in-depth: AnalyzeCCI already drops out-of-range phases from
	// PhaseTimes, but buildPhaseStats stays robust when called directly with an unfiltered
	// result (e.g. in unit tests).
	phaseAt := func(p models.PhasePoint) (int, float64, bool) {
		t, ok := result.PhaseTimes[string(p)]
		if !ok {
			return 0, 0, false
		}
		idx, inRange := synchronizer.ResolveTimeIndex(result.TimeValues, t)
		if !inRange {
			return 0, 0, false
		}
		return idx, t, true
	}

	rows := make([]CCIPhaseStatRow, 0, len(phaseStatRowSpecs))
	for _, spec := range phaseStatRowSpecs {
		rows = append(rows, a.computeRow(result, spec, nPairs, phaseAt))
	}

	return rows
}

// computeRow materializes one row from its spec. Returns NaN×nPairs with
// HasTime=false when a required phase point is absent.
func (a *CCIAnalyzer) computeRow(
	result *CCIAnalysisResult,
	spec phaseStatRowSpec,
	nPairs int,
	phaseAt func(models.PhasePoint) (int, float64, bool),
) CCIPhaseStatRow {
	row := CCIPhaseStatRow{Item: spec.item, Metric: spec.metric}

	if spec.kind == kindInterval {
		_, tFrom, okFrom := phaseAt(spec.from)
		_, tTo, okTo := phaseAt(spec.to)
		if !okFrom || !okTo {
			row.Values = nanValues(nPairs)
			return row
		}
		// 視窗以兩端點時間中點為中心(ADR-0022)。中點可交換,D/O motion 反序無需 order guard。
		mid, _ := synchronizer.ResolveTimeIndex(result.TimeValues, (tFrom+tTo)/2)
		row.HasTime = true
		row.Time = result.TimeValues[mid]
		row.Values = perPair(result.PairResults, func(s []float64) float64 {
			return windowmean.WindowMean(s, mid, band50Half)
		})
		return row
	}

	// Single-point and landing rows share point presence + HasTime/Time handling.
	i, _, ok := phaseAt(spec.point)
	if !ok {
		row.Values = nanValues(nPairs)
		return row
	}
	row.HasTime = true
	row.Time = result.PhaseTimes[string(spec.point)]
	row.Values = perPair(result.PairResults, pointValueFn(spec, i))

	return row
}

// pointValueFn returns the per-pair window function for a present single-point or
// landing row at index i.
func pointValueFn(spec phaseStatRowSpec, i int) func([]float64) float64 {
	switch spec.kind {
	case kindBand50:
		return func(s []float64) float64 { return windowmean.MeanRange(s, i-band50Half, i+band50Half) }
	case kindBand25:
		return func(s []float64) float64 { return weighted25(s, i) }
	case kindPre100:
		return func(s []float64) float64 { return windowmean.MeanRange(s, i-pre100Span, i) }
	case kindLand:
		return func(s []float64) float64 { return windowmean.MeanRange(s, i+spec.landLo, i+spec.landHi) }
	default:
		// kindInterval is handled in computeRow; unreachable here.
		return func([]float64) float64 { return math.NaN() }
	}
}

// perPair applies fn to each pair's Values series, preserving PairResults order.
func perPair(pairs []CCIResult, fn func([]float64) float64) []float64 {
	out := make([]float64, len(pairs))
	for k, pr := range pairs {
		out[k] = fn(pr.Values)
	}
	return out
}

// nanValues returns a length-n slice filled with NaN (for absent-point/interval rows).
func nanValues(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	return out
}

// non100HzToleranceSeconds is how far the estimated sample interval may deviate
// from 0.01 s (100 Hz) before buildPhaseStats logs the nominal-window audit warn.
const non100HzToleranceSeconds = 0.001

// warnIfNon100Hz emits a single audit warning when the sample interval deviates
// from 100 Hz, since the ms-labelled windows are then nominal: the window
// point-counts stay fixed, so the actual span = pointcount × sample interval.
// This does NOT change the index math.
func (a *CCIAnalyzer) warnIfNon100Hz(times []float64) {
	if a.logger == nil {
		return
	}
	interval := estimateSampleInterval(times, a.logger)
	if math.Abs(interval-0.01) <= non100HzToleranceSeconds {
		return
	}
	a.logger.Warn("分期視窗統計:取樣率非 100Hz,ms 標籤視窗為名目值 (point-count 固定,實際時長 = point-count × sample interval)", map[string]any{
		"sample_interval": interval,
		"expected_100hz":  0.01,
		"tolerance":       non100HzToleranceSeconds,
	})
}
