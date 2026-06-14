package cci

// computeCCI_test.go — file-free wiring test for the extracted computeCCI method
// (ADR-0024). All inputs are built in-memory; no disk I/O.
//
// Key discrimination: EMGMotionOffset=26 makes ForceTimeToEMGTime subtract
// (26-1)/250 = 0.1, so converted EMG phase times differ from raw force-plate
// times by exactly 0.1. Assertions on PhaseTimes prove the conversion is wired
// (offset=1 would make them equal, defeating the discrimination).

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

// makeConstantChannel returns a length-n slice filled with the constant value v.
func makeConstantChannel(n int, v float64) []float64 {
	s := make([]float64, n)
	for i := range s {
		s[i] = v
	}
	return s
}

// buildInMemoryCCIFixture constructs in-memory EMG data and a PhaseManifest
// with known phase values that produce hand-computable expected outputs.
//
//   - 101 time points at 100 Hz: t[i] = i*0.01, i=0..100.
//   - 8 right-side channels with distinct constant EMG values (1..8).
//   - EMGMotionOffset=26, force-plate phase times S=0.5,T0=0.6,C=0.7,T=0.8,L=0.9.
//     → converted EMG times = force - (26-1)/250 = force - 0.1:
//     S=0.4, T0=0.5, C=0.6, T=0.7, L=0.8.
//   - D and O left as the zero/unset sentinel (int 0) — not MakeOpt.
func buildInMemoryCCIFixture() (*models.PhaseSyncEMGData, *models.PhaseManifest) {
	const n = 101
	times := make([]float64, n)
	for i := range times {
		times[i] = float64(i) * 0.01
	}

	// Channel constants tied to header NAME, not slice position. Headers below are
	// deliberately NOT in DefaultMusclePairs() / RA-ES-IL-... order, so an order- or
	// index-based channel-map regression (e.g. "first header => RA") would bind the
	// constants to the wrong muscles and shift the CCI values — proving the mapping
	// is name-based (via MapHeaderToShortName), not file-order based. [codex R2]
	headerConst := map[string]float64{
		"R.RA: EMG 1":    1.0,
		"R.ES: EMG 2":    2.0,
		"R.IL: EMG 3":    3.0,
		"R.GMax: EMG 4":  4.0,
		"R.RF: EMG 5":    5.0,
		"R.BF: EMG 6":    6.0,
		"R.TA&IO: EMG 7": 7.0, // TA&IO short name = TAIO
		"R.MF: EMG 8":    8.0,
	}
	headers := []string{
		"R.MF: EMG 8",
		"R.RA: EMG 1",
		"R.BF: EMG 6",
		"R.IL: EMG 3",
		"R.TA&IO: EMG 7",
		"R.ES: EMG 2",
		"R.GMax: EMG 4",
		"R.RF: EMG 5",
	}

	channels := make(map[string][]float64, len(headers))
	for _, h := range headers {
		channels[h] = makeConstantChannel(n, headerConst[h])
	}

	emgData := &models.PhaseSyncEMGData{
		Time:     times,
		Headers:  headers,
		Channels: channels,
	}

	manifest := &models.PhaseManifest{
		Subject:         "TST",
		EMGMotionOffset: 26, // offset = (26-1)/250 = 0.1 → EMG = force - 0.1
		PhasePoints: models.PhasePoints{
			// Force-plate times — MakeOpt marks them as set.
			S:  models.MakeOpt(0.5),
			T0: models.MakeOpt(0.6),
			C:  models.MakeOpt(0.7),
			T:  models.MakeOpt(0.8),
			L:  models.MakeOpt(0.9),
			// D and O are int fields, left as zero (unset sentinel).
		},
	}

	return emgData, manifest
}

// TestComputeCCI_AssemblesResult verifies the full wiring of computeCCI with
// hand-built in-memory inputs. All 5 set phase points are present and in range.
func TestComputeCCI_AssemblesResult(t *testing.T) {
	emgData, manifest := buildInMemoryCCIFixture()
	a := NewCCIAnalyzer()

	got, err := a.computeCCI(context.Background(), emgData, manifest)
	require.NoError(t, err)

	// --- Gait anchor times ---
	// S_emg = 0.5 - 0.1 = 0.4; L_emg = 0.9 - 0.1 = 0.8.
	assert.InDelta(t, 0.4, got.GaitStartTime, 1e-9, "GaitStartTime must be S converted to EMG time")
	assert.InDelta(t, 0.8, got.GaitEndTime, 1e-9, "GaitEndTime must be L converted to EMG time")

	// --- Phase percents (affine-invariant to the 0.1 offset) ---
	// pct = (emgTime - 0.4) / 0.4 * 100
	require.Len(t, got.PhasePercents, 5, "all five set points should be present")
	assert.InDelta(t, 0.0, got.PhasePercents["S"], 1e-9, "S at 0%")
	assert.InDelta(t, 25.0, got.PhasePercents["T0"], 1e-9, "T0 at 25%")
	assert.InDelta(t, 50.0, got.PhasePercents["C"], 1e-9, "C at 50%")
	assert.InDelta(t, 75.0, got.PhasePercents["T"], 1e-9, "T at 75%")
	assert.InDelta(t, 100.0, got.PhasePercents["L"], 1e-9, "L at 100%")

	// --- Phase times (converted EMG, must differ from raw force-plate by 0.1) ---
	// This is the primary discrimination: proves ForceTimeToEMGTime was called.
	require.Len(t, got.PhaseTimes, 5, "all five set points should be present")
	assert.InDelta(t, 0.4, got.PhaseTimes["S"], 1e-9, "S EMG time = 0.5 - 0.1")
	assert.InDelta(t, 0.5, got.PhaseTimes["T0"], 1e-9, "T0 EMG time = 0.6 - 0.1")
	assert.InDelta(t, 0.6, got.PhaseTimes["C"], 1e-9, "C EMG time = 0.7 - 0.1")
	assert.InDelta(t, 0.7, got.PhaseTimes["T"], 1e-9, "T EMG time = 0.8 - 0.1")
	assert.InDelta(t, 0.8, got.PhaseTimes["L"], 1e-9, "L EMG time = 0.9 - 0.1")

	// --- Time-slice shape ---
	// extractStart = 0.4 - 0.15 = 0.25 = t[25]; extractEnd = 0.8 + 0.15 = 0.95 = t[95].
	// Indices 25..95 inclusive = 71 points.
	require.Len(t, got.TimeValues, 71, "extracted window [0.25, 0.95] at 100Hz = 71 points")
	assert.InDelta(t, 0.25, got.TimeValues[0], 1e-9, "TimeValues[0] must be 0.25")
	assert.InDelta(t, 0.95, got.TimeValues[70], 1e-9, "TimeValues[70] must be 0.95")

	// Every PairResult Values length must equal TimeValues length.
	for _, pr := range got.PairResults {
		assert.Lenf(t, pr.Values, len(got.TimeValues),
			"pair %s Values length must equal TimeValues length", pr.PairName)
	}

	// --- Pair completeness vs a LITERAL 12-name contract. NOT DefaultMusclePairs():
	// that is the same production function computeAllPairs uses, so a dropped pair
	// would shrink both sides and hide the regression. [codex R2]
	contractPairNames := []string{
		"RA/ES", "IL/GMax", "RF/BF", "TAIO/MF", "RA/GMax", "RA/MF",
		"IL/ES", "IL/BF", "IL/MF", "RF/GMax", "TAIO/ES", "TAIO/GMax",
	}
	require.Len(t, got.PairResults, 12, "must have exactly 12 contractual pairs")
	gotNames := make(map[string]struct{}, len(got.PairResults))
	for _, pr := range got.PairResults {
		gotNames[pr.PairName] = struct{}{}
	}
	for _, name := range contractPairNames {
		assert.Contains(t, gotNames, name, "contractual pair %s must be present", name)
	}

	// --- Phase stats shape ---
	require.Len(t, got.PhaseStats, 32, "must always be exactly 32 phase stat rows")

	// The S ±50ms row must be present (HasTime=true), carry all 12 per-pair columns,
	// and have every value finite. The explicit length check keeps the finite loop
	// non-vacuous (an empty Values slice would otherwise pass trivially). [codex R2]
	sBand := findRow(t, got.PhaseStats, "S", metricBand50ms)
	assert.True(t, sBand.HasTime, "S ±50ms row must have HasTime=true")
	require.Len(t, sBand.Values, 12, "S band must carry 12 per-pair columns aligned to PairResults")
	for k, v := range sBand.Values {
		assert.Falsef(t, math.IsNaN(v) || math.IsInf(v, 0),
			"S ±50ms Values[%d] must be finite, got %v", k, v)
	}

	// --- Per-pair hand-computed CCI values ---
	// Each channel is constant, so every time point has the same CCI value.
	// Formula: CalculateCCIRudolph(x, y) = (min/max) * (min+max).
	// Channel constants: RA=1, ES=2, IL=3, GMax=4, RF=5, BF=6, TAIO=7, MF=8.
	expectedCCI := map[string]float64{
		"RA/ES":     1.5,                // (1/2)*(1+2)
		"IL/GMax":   5.25,               // (3/4)*(3+4)
		"RF/BF":     9.166666666666666,  // (5/6)*(5+6)
		"TAIO/MF":   13.125,             // (7/8)*(7+8)
		"RA/GMax":   1.25,               // (1/4)*(1+4)
		"RA/MF":     1.125,              // (1/8)*(1+8)
		"IL/ES":     3.3333333333333335, // (2/3)*(2+3)
		"IL/BF":     4.5,                // (3/6)*(3+6)
		"IL/MF":     4.125,              // (3/8)*(3+8)
		"RF/GMax":   7.2,                // (4/5)*(4+5)
		"TAIO/ES":   2.5714285714285716, // (2/7)*(2+7)
		"TAIO/GMax": 6.285714285714286,  // (4/7)*(4+7)
	}

	for _, pr := range got.PairResults {
		want, exists := expectedCCI[pr.PairName]
		require.Truef(t, exists, "unexpected pair name %q", pr.PairName)
		require.NotEmpty(t, pr.Values, "pair %s must have values", pr.PairName)
		assert.InDeltaf(t, want, pr.Values[0], 1e-9,
			"pair %s CCI at index 0 (constant signal → uniform series)", pr.PairName)
	}
}

// TestComputeCCI_DropsOutOfRangePhase verifies that a phase point whose converted
// EMG time falls outside the extracted window [S-150ms, L+150ms] is removed from
// PhasePercents and PhaseTimes, and produces NaN/HasTime=false rows in PhaseStats.
//
// Note: S/T0/L are all MakeOpt-set. In TestComputeCCI_AssemblesResult, T0 (converted
// to 0.5, within [0.25, 0.95]) is PRESENT. Here T0 force=0.2 converts to EMG=0.1,
// which is outside the extracted window, so its absence is caused specifically by the
// out-of-range drop — not by being an unset phase point.
//
// The out-of-window T0 (raw percent ≈ -75%) necessarily triggers a clamp Warn log;
// this is expected and not asserted against.
func TestComputeCCI_DropsOutOfRangePhase(t *testing.T) {
	emgData, manifest := buildInMemoryCCIFixture()
	// Override T0 to a force-plate time that converts to EMG 0.1 — outside [0.25, 0.95].
	manifest.PhasePoints.T0 = models.MakeOpt(0.2) // 0.2 - 0.1 = 0.1 EMG (out of range)

	a := NewCCIAnalyzer()
	got, err := a.computeCCI(context.Background(), emgData, manifest)
	require.NoError(t, err)

	// T0 must be absent from both maps.
	_, okPercents := got.PhasePercents["T0"]
	assert.False(t, okPercents, "T0 must be absent from PhasePercents after out-of-range drop")

	_, okTimes := got.PhaseTimes["T0"]
	assert.False(t, okTimes, "T0 must be absent from PhaseTimes after out-of-range drop")

	// T0 phase stat rows must have HasTime=false and NaN values.
	// Each dropped T0 row still carries 12 per-pair columns (all NaN); the explicit
	// length check keeps the NaN loops non-vacuous. [codex R2]
	t0Band := findRow(t, got.PhaseStats, "T0", metricBand50ms)
	assert.False(t, t0Band.HasTime, "T0 ±50ms row must have HasTime=false (dropped)")
	require.Len(t, t0Band.Values, 12, "dropped T0 ±50ms row must still have 12 columns")
	for k, v := range t0Band.Values {
		assert.Truef(t, math.IsNaN(v),
			"T0 ±50ms Values[%d] must be NaN (phase dropped), got %v", k, v)
	}

	t0Band25 := findRow(t, got.PhaseStats, "T0", metricBand25ms)
	assert.False(t, t0Band25.HasTime, "T0 ±25ms row must have HasTime=false (dropped)")
	require.Len(t, t0Band25.Values, 12, "dropped T0 ±25ms row must still have 12 columns")
	for k, v := range t0Band25.Values {
		assert.Truef(t, math.IsNaN(v),
			"T0 ±25ms Values[%d] must be NaN, got %v", k, v)
	}

	t0Pre100 := findRow(t, got.PhaseStats, "T0", metricPre100ms)
	assert.False(t, t0Pre100.HasTime, "T0 前100ms row must have HasTime=false (dropped)")
	require.Len(t, t0Pre100.Values, 12, "dropped T0 前100ms row must still have 12 columns")
	for k, v := range t0Pre100.Values {
		assert.Truef(t, math.IsNaN(v),
			"T0 前100ms Values[%d] must be NaN, got %v", k, v)
	}

	// S and L must remain present and have HasTime=true.
	_, okS := got.PhasePercents["S"]
	assert.True(t, okS, "S must remain present in PhasePercents")

	_, okL := got.PhasePercents["L"]
	assert.True(t, okL, "L must remain present in PhasePercents")

	sBand := findRow(t, got.PhaseStats, "S", metricBand50ms)
	assert.True(t, sBand.HasTime, "S ±50ms row must have HasTime=true")

	lBand := findRow(t, got.PhaseStats, "L", metricBand50ms)
	assert.True(t, lBand.HasTime, "L ±50ms row must have HasTime=true")
}

// TestComputeCCI_GaitAnchorsOnSLNotMinMax proves the gait cycle anchors on the S
// and L phase points BY NAME (ADR-0018: 0%=S, 100%=L), not on the min/max of the
// participating phase times. The fixture places a non-anchor phase before S and
// another after L — both still inside the extract window [0.25,0.95] so neither is
// dropped — making S and L non-extremal. If calculateGaitCycle regressed to the
// pre-ADR-0018 min/max anchoring, gaitStart/gaitEnd and the S/L percents would
// shift, failing these assertions. (Percents are stored clamped to [0,100] —
// analyzer.go:352 — so the out-of-[S,L] phases are asserted for presence only, not
// their raw -25% / 125%.) [codex R2]
func TestComputeCCI_GaitAnchorsOnSLNotMinMax(t *testing.T) {
	emgData, manifest := buildInMemoryCCIFixture()
	// T0 force 0.4 → EMG 0.3 (before S_emg=0.4); T force 1.0 → EMG 0.9 (after L_emg=0.8).
	manifest.PhasePoints.T0 = models.MakeOpt(0.4)
	manifest.PhasePoints.T = models.MakeOpt(1.0)

	a := NewCCIAnalyzer()
	got, err := a.computeCCI(context.Background(), emgData, manifest)
	require.NoError(t, err)

	// Anchored on S/L by name: gaitStart=0.4 (S), gaitEnd=0.8 (L) — NOT min(0.3)/max(0.9).
	assert.InDelta(t, 0.4, got.GaitStartTime, 1e-9,
		"gaitStart must anchor on S, not the minimum phase time (0.3)")
	assert.InDelta(t, 0.8, got.GaitEndTime, 1e-9,
		"gaitEnd must anchor on L, not the maximum phase time (0.9)")

	// S=0% / L=100% hold only under name anchoring; min/max anchoring over [0.3,0.9]
	// would give S≈16.7% and L≈83.3%.
	assert.InDelta(t, 0.0, got.PhasePercents["S"], 1e-9, "S at 0% (name-anchored)")
	assert.InDelta(t, 100.0, got.PhasePercents["L"], 1e-9, "L at 100% (name-anchored)")

	// T0 (pre-S) and T (post-L) participate (present, not dropped), confirming S/L
	// really are non-extremal — so the anchoring above is genuinely name-based.
	_, okT0 := got.PhasePercents["T0"]
	assert.True(t, okT0, "T0 (in-window, before S) must be present so S is non-extremal")
	_, okT := got.PhasePercents["T"]
	assert.True(t, okT, "T (in-window, after L) must be present so L is non-extremal")
}
