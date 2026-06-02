package cci

import (
	"math"

	"count_mean/internal/models"
	"count_mean/internal/synchronizer"
	"count_mean/internal/windowmean"
)

// Metric labels for the phase-window statistics table (ADR-0018 Output 2).
const (
	metricInterval    = "區間平均"
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
	kindInterval rowKind = iota // mean over [from, to] (order-guarded for motion inversion)
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
	// INTERVAL rows (區間平均). S-D and D-T intentionally skip the intervening point.
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

// buildPhaseStats builds the fixed 32-row phase-window statistics table (ADR-0018
// Output 2). The table always has 32 rows in the documented order; the number of
// columns equals len(result.PairResults) (NOT forced to 12) so Output 2 stays
// aligned with Output 1. Rows for absent phase points (or intervals with an absent
// endpoint) keep their slot with Values = NaN×nPairs and HasTime=false.
func (a *CCIAnalyzer) buildPhaseStats(result *CCIAnalysisResult) []CCIPhaseStatRow {
	a.warnIfNon100Hz(result.TimeValues)

	nPairs := len(result.PairResults)

	// phaseIndex resolves a present phase point to its index in TimeValues; the
	// second result reports presence (in PhaseTimes AND within the extracted range).
	//
	// A phase whose time falls OUTSIDE the extracted [S-150ms, L+150ms] window — e.g.
	// a malformed manifest puts a mid-point (C/D/T0/T/O) beyond L — would otherwise be
	// silently clamped by FindNearestTimeIndex to index 0 / len-1 and emit boundary-
	// window stats at the wrong location. Treat it as absent (→ NaN row), consistent
	// with the missing-point policy.
	//
	// The tolerance MUST match validateEMGBounds' boundsEpsilon: S/L are accepted up to
	// 1e-6 outside [emgMin, emgMax] (tolerated ULP/sync drift), and extraction clamps
	// them to the first/last sample. A strict (zero-tolerance) check would then blank
	// the S/L anchor rows + landing windows for an analysis that explicitly succeeded.
	const boundsEpsilon = 1e-6
	phaseIndex := func(p models.PhasePoint) (int, bool) {
		t, ok := result.PhaseTimes[string(p)]
		if !ok {
			return 0, false
		}
		n := len(result.TimeValues)
		if n == 0 || t < result.TimeValues[0]-boundsEpsilon || t > result.TimeValues[n-1]+boundsEpsilon {
			return 0, false
		}
		return synchronizer.FindNearestTimeIndex(result.TimeValues, t), true
	}

	rows := make([]CCIPhaseStatRow, 0, len(phaseStatRowSpecs))
	for _, spec := range phaseStatRowSpecs {
		rows = append(rows, a.computeRow(result, spec, nPairs, phaseIndex))
	}

	return rows
}

// computeRow materializes one row from its spec. Returns NaN×nPairs with
// HasTime=false when a required phase point is absent.
func (a *CCIAnalyzer) computeRow(
	result *CCIAnalysisResult,
	spec phaseStatRowSpec,
	nPairs int,
	phaseIndex func(models.PhasePoint) (int, bool),
) CCIPhaseStatRow {
	row := CCIPhaseStatRow{Item: spec.item, Metric: spec.metric}

	if spec.kind == kindInterval {
		iFrom, okFrom := phaseIndex(spec.from)
		iTo, okTo := phaseIndex(spec.to)
		if !okFrom || !okTo {
			row.Values = nanValues(nPairs)
			return row
		}
		// min/max guards motion-index inversion (D and O are motion-derived).
		lo, hi := iFrom, iTo
		if lo > hi {
			lo, hi = hi, lo
		}
		row.Values = perPair(result.PairResults, func(s []float64) float64 {
			return windowmean.MeanRange(s, lo, hi)
		})
		return row
	}

	// Single-point and landing rows share point presence + HasTime/Time handling.
	i, ok := phaseIndex(spec.point)
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
