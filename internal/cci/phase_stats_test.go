package cci

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
	"count_mean/internal/synchronizer"
	"count_mean/internal/windowmean"
)

// makePhaseStatsFixture builds a controlled CCIAnalysisResult literal (no pipeline
// run) for hand-computable golden assertions.
//
//   - TimeValues: 101 points at 100 Hz → t[i] = i*0.01 (so i-5 = -50ms, i-10 = -100ms).
//   - PairResults: pair "A" ramp Values[i]=i, pair "B" Values[i]=2*i — exercises the
//     per-pair loop and column order, and (B = 2×A) catches a swapped-column bug.
//   - PhaseTimes: S..L at indices 10,20,30,40,50,60,70. Landing windows reach index 80
//     (< 101, no high clamp).
//
// On a consecutive-integer ramp, MeanRange(a,b) = (a+b)/2 and weighted25(ramp,i)=i,
// so every expected value below is derived by hand, not printed-and-pasted.
func makePhaseStatsFixture() *CCIAnalysisResult {
	const n = 101
	times := make([]float64, n)
	rampA := make([]float64, n)
	rampB := make([]float64, n)
	for i := 0; i < n; i++ {
		times[i] = float64(i) * 0.01
		rampA[i] = float64(i)
		rampB[i] = 2 * float64(i)
	}

	return &CCIAnalysisResult{
		Subject: "FixtureRamp",
		PairResults: []CCIResult{
			{PairName: "A", Values: rampA},
			{PairName: "B", Values: rampB},
		},
		TimeValues: times,
		PhaseTimes: map[string]float64{
			string(models.PhaseS):  times[10],
			string(models.PhaseC):  times[20],
			string(models.PhaseD):  times[30],
			string(models.PhaseT0): times[40],
			string(models.PhaseT):  times[50],
			string(models.PhaseO):  times[60],
			string(models.PhaseL):  times[70],
		},
	}
}

// expectedRow captures the full golden expectation for one of the 32 rows.
// valA is the pair-A value; pair B is asserted as 2*valA. nan means Values must be
// NaN×nPairs (absent point / interval endpoint).
type expectedRow struct {
	item    string
	metric  string
	hasTime bool
	timeIdx int // index into TimeValues whose time should equal row.Time (only when hasTime)
	valA    float64
	nan     bool
}

// goldenRows is the authoritative 32-row expectation for the full fixture.
// Every valA is hand-derived: interval = WindowMean at the endpoints' time-midpoint
// (linear ramp: == whole-range mean, so valA unchanged);
// ±50ms = MeanRange(i-5,i+5) = i; ±25ms = weighted25 = i; 前100ms = MeanRange(i-10,i) = i-5;
// landing = MeanRange(iL+a, iL+b) = (140+a+b)/2 with iL=70.
func goldenRows() []expectedRow {
	return []expectedRow{
		// INTERVAL rows: WindowMean at the endpoints' time-midpoint (linear ramp: == whole-range mean).
		{item: "S-C", metric: metricInterval, hasTime: true, timeIdx: 15, valA: 15},
		{item: "S-D", metric: metricInterval, hasTime: true, timeIdx: 20, valA: 20},
		{item: "C-D", metric: metricInterval, hasTime: true, timeIdx: 25, valA: 25},
		{item: "D-T", metric: metricInterval, hasTime: true, timeIdx: 40, valA: 40},
		{item: "D-T0", metric: metricInterval, hasTime: true, timeIdx: 35, valA: 35},
		{item: "T0-T", metric: metricInterval, hasTime: true, timeIdx: 45, valA: 45},
		{item: "T-O", metric: metricInterval, hasTime: true, timeIdx: 55, valA: 55},
		{item: "O-L", metric: metricInterval, hasTime: true, timeIdx: 65, valA: 65},

		// ±50ms = i, ±25ms = i, interleaved.
		{item: "S", metric: metricBand50ms, hasTime: true, timeIdx: 10, valA: 10},
		{item: "S", metric: metricBand25ms, hasTime: true, timeIdx: 10, valA: 10},
		{item: "C", metric: metricBand50ms, hasTime: true, timeIdx: 20, valA: 20},
		{item: "C", metric: metricBand25ms, hasTime: true, timeIdx: 20, valA: 20},
		{item: "D", metric: metricBand50ms, hasTime: true, timeIdx: 30, valA: 30},
		{item: "D", metric: metricBand25ms, hasTime: true, timeIdx: 30, valA: 30},
		{item: "T0", metric: metricBand50ms, hasTime: true, timeIdx: 40, valA: 40},
		{item: "T0", metric: metricBand25ms, hasTime: true, timeIdx: 40, valA: 40},
		{item: "T", metric: metricBand50ms, hasTime: true, timeIdx: 50, valA: 50},
		{item: "T", metric: metricBand25ms, hasTime: true, timeIdx: 50, valA: 50},
		{item: "O", metric: metricBand50ms, hasTime: true, timeIdx: 60, valA: 60},
		{item: "O", metric: metricBand25ms, hasTime: true, timeIdx: 60, valA: 60},
		{item: "L", metric: metricBand50ms, hasTime: true, timeIdx: 70, valA: 70},
		{item: "L", metric: metricBand25ms, hasTime: true, timeIdx: 70, valA: 70},

		// 前100ms = MeanRange(i-10,i) = i-5.
		{item: "S", metric: metricPre100ms, hasTime: true, timeIdx: 10, valA: 5},
		{item: "C", metric: metricPre100ms, hasTime: true, timeIdx: 20, valA: 15},
		{item: "D", metric: metricPre100ms, hasTime: true, timeIdx: 30, valA: 25},
		{item: "T0", metric: metricPre100ms, hasTime: true, timeIdx: 40, valA: 35},
		{item: "T", metric: metricPre100ms, hasTime: true, timeIdx: 50, valA: 45},
		{item: "O", metric: metricPre100ms, hasTime: true, timeIdx: 60, valA: 55},
		{item: "L", metric: metricPre100ms, hasTime: true, timeIdx: 70, valA: 65},

		// Landing (iL=70): MeanRange over inclusive integer range = mean of those ints.
		{item: "L", metric: metricLand0to100, hasTime: true, timeIdx: 70, valA: 75},    // MeanRange(70,80)=75
		{item: "L", metric: metricLand20to50, hasTime: true, timeIdx: 70, valA: 73.5},  // MeanRange(72,75)=(72+73+74+75)/4
		{item: "L", metric: metricLand50to100, hasTime: true, timeIdx: 70, valA: 77.5}, // MeanRange(75,80)=465/6
	}
}

const goldenDelta = 1e-9

// assertRow checks one materialized row against its golden expectation (pair A and
// pair B = 2×A), including label, order, HasTime, and Time.
func assertRow(t *testing.T, idx int, got CCIPhaseStatRow, want expectedRow, times []float64) {
	t.Helper()
	assert.Equalf(t, want.item, got.Item, "row %d Item", idx)
	assert.Equalf(t, want.metric, got.Metric, "row %d Metric", idx)
	assert.Equalf(t, want.hasTime, got.HasTime, "row %d HasTime", idx)
	require.Lenf(t, got.Values, 2, "row %d Values len (must equal len(PairResults))", idx)

	if want.hasTime {
		assert.InDeltaf(t, times[want.timeIdx], got.Time, goldenDelta, "row %d Time", idx)
	}

	if want.nan {
		assert.Truef(t, math.IsNaN(got.Values[0]), "row %d %s/%s pairA expected NaN, got %v",
			idx, got.Item, got.Metric, got.Values[0])
		assert.Truef(t, math.IsNaN(got.Values[1]), "row %d %s/%s pairB expected NaN, got %v",
			idx, got.Item, got.Metric, got.Values[1])
		return
	}

	assert.InDeltaf(t, want.valA, got.Values[0], goldenDelta,
		"row %d %s/%s pairA", idx, got.Item, got.Metric)
	assert.InDeltaf(t, 2*want.valA, got.Values[1], goldenDelta,
		"row %d %s/%s pairB (must be 2×pairA — guards column order)", idx, got.Item, got.Metric)
}

// TestBuildPhaseStats_FullFixture_Golden is the authoritative correctness lock:
// the full 32-row table on the ramp fixture, every value hand-derived above.
func TestBuildPhaseStats_FullFixture_Golden(t *testing.T) {
	require.Equal(t, "中點±50ms", metricInterval, "ADR-0022 relabel 必須落實")
	result := makePhaseStatsFixture()
	a := NewCCIAnalyzer()

	rows := a.buildPhaseStats(result)
	want := goldenRows()

	require.Len(t, rows, 32, "must always be 32 rows")
	require.Len(t, want, 32, "golden table must define all 32 rows")

	for i := range want {
		assertRow(t, i, rows[i], want[i], result.TimeValues)
	}
}

// TestBuildPhaseStats_MissingMiddlePoint_KeepsRowsAsNaN omits C: its dependent rows
// become NaN×nPairs with HasTime=false, the row slots still exist (32 total), and
// rows not depending on C (e.g. S-D, S, L, landing) are unaffected.
func TestBuildPhaseStats_MissingMiddlePoint_KeepsRowsAsNaN(t *testing.T) {
	result := makePhaseStatsFixture()
	delete(result.PhaseTimes, string(models.PhaseC))

	a := NewCCIAnalyzer()
	rows := a.buildPhaseStats(result)
	require.Len(t, rows, 32, "fixed 32 rows even with C absent")

	// Build golden but flip C-dependent rows to NaN.
	want := goldenRows()
	cDependent := map[[2]string]bool{
		{"S-C", metricInterval}: true,
		{"C-D", metricInterval}: true,
		{"C", metricBand50ms}:   true,
		{"C", metricBand25ms}:   true,
		{"C", metricPre100ms}:   true,
	}
	for i := range want {
		if cDependent[[2]string{want[i].item, want[i].metric}] {
			want[i].nan = true
			want[i].hasTime = false
		}
	}

	for i := range want {
		assertRow(t, i, rows[i], want[i], result.TimeValues)
	}

	// Explicit spec checks: S-D needs only S,D and must still equal 20.
	sd := findRow(t, rows, "S-D", metricInterval)
	assert.InDelta(t, 20.0, sd.Values[0], goldenDelta, "S-D unaffected by missing C")
	assert.InDelta(t, 40.0, sd.Values[1], goldenDelta)
}

// TestBuildPhaseStats_ClampBoundary verifies MeanRange's low clamp (lo<0→0) and high
// clamp (hi>n-1→n-1) flow through the ±50ms window.
func TestBuildPhaseStats_ClampBoundary(t *testing.T) {
	result := makePhaseStatsFixture()
	// Low clamp: S at index 2 (< 5) → ±50ms = MeanRange(-3→0, 7) = MeanRange(0,7) = 3.5.
	result.PhaseTimes[string(models.PhaseS)] = result.TimeValues[2]
	// High clamp: O at index 99 (n-1=100) → ±50ms = MeanRange(94, 104→100) = MeanRange(94,100) = 97.
	result.PhaseTimes[string(models.PhaseO)] = result.TimeValues[99]

	a := NewCCIAnalyzer()
	rows := a.buildPhaseStats(result)

	sBand := findRow(t, rows, "S", metricBand50ms)
	assert.InDelta(t, 3.5, sBand.Values[0], goldenDelta, "low clamp: MeanRange(0,7)=3.5")
	assert.InDelta(t, 7.0, sBand.Values[1], goldenDelta)

	oBand := findRow(t, rows, "O", metricBand50ms)
	assert.InDelta(t, 97.0, oBand.Values[0], goldenDelta, "high clamp: MeanRange(94,100)=97")
	assert.InDelta(t, 194.0, oBand.Values[1], goldenDelta)
}

// findRow returns the first row matching item+metric, failing if absent.
func findRow(t *testing.T, rows []CCIPhaseStatRow, item, metric string) CCIPhaseStatRow {
	t.Helper()
	for _, r := range rows {
		if r.Item == item && r.Metric == metric {
			return r
		}
	}
	t.Fatalf("row %s/%s not found", item, metric)
	return CCIPhaseStatRow{}
}

// TestBuildPhaseStats_OutOfRangePhaseTimeTreatedAbsent guards a re-anchor edge case
// (codex P2): a PRESENT mid-point whose time falls OUTSIDE the extracted
// [S-150ms, L+150ms] window (malformed manifest — e.g. C typo'd past L) must be
// treated as absent (NaN row, HasTime=false), NOT silently clamped by
// FindNearestTimeIndex to a boundary index and emitted as boundary-window stats at
// the wrong location. S/L always bound the extracted range so they never trip this.
func TestBuildPhaseStats_OutOfRangePhaseTimeTreatedAbsent(t *testing.T) {
	fixture := makePhaseStatsFixture()
	// Push C beyond TimeValues[last]=1.0 (S/D/T0/T/O/L stay in range). Without the
	// range guard, FindNearestTimeIndex clamps C to index 100 and C's windows would
	// emit non-NaN boundary values; with the guard, C is treated as absent.
	fixture.PhaseTimes[string(models.PhaseC)] = 1.5

	a := NewCCIAnalyzer()
	rows := a.buildPhaseStats(fixture)
	require.Len(t, rows, 32, "still a fixed 32-row table")

	assertBlank := func(item, metric string) {
		r := findRow(t, rows, item, metric)
		assert.Falsef(t, r.HasTime, "%s/%s must have HasTime=false (C out of range)", item, metric)
		for k, v := range r.Values {
			assert.Truef(t, math.IsNaN(v),
				"%s/%s pair %d must be NaN (C out of extracted range), got %v", item, metric, k, v)
		}
	}
	// Every C-dependent row is blanked: C's three point rows + the two intervals
	// whose endpoint is C (S-C, C-D).
	assertBlank("S-C", metricInterval)
	assertBlank("C-D", metricInterval)
	assertBlank("C", metricBand50ms)
	assertBlank("C", metricBand25ms)
	assertBlank("C", metricPre100ms)

	// Rows independent of C remain valid — proves only C was blanked, not the table.
	sd := findRow(t, rows, "S-D", metricInterval)
	require.False(t, math.IsNaN(sd.Values[0]), "S-D needs only S,D (both in range)")
	assert.InDelta(t, 20.0, sd.Values[0], 1e-9, "S-D midpoint window = WindowMean(mid=20,±5)=20")
	sBand := findRow(t, rows, "S", metricBand50ms)
	assert.True(t, sBand.HasTime)
	assert.InDelta(t, 10.0, sBand.Values[0], 1e-9)
	lLand := findRow(t, rows, "L", metricLand0to100)
	require.False(t, math.IsNaN(lLand.Values[0]), "L landing stays valid")
}

// TestBuildPhaseStats_ToleratedBoundaryDriftStaysPresent guards the codex-R2 follow-up
// to the out-of-range fix: an anchor whose time drifts past the extracted-range boundary
// by ≤ the 1e-6 tolerance that validateEMGBounds accepts (ULP/sync drift) must STAY
// present — only drift BEYOND tolerance (a real typo) is treated as absent. Without the
// matching tolerance, a strict check would blank valid S/L anchors (+ landing) for an
// analysis that explicitly succeeded.
func TestBuildPhaseStats_ToleratedBoundaryDriftStaysPresent(t *testing.T) {
	fixture := makePhaseStatsFixture()
	// S drifts 0.5e-6 below TimeValues[0]=0 — within validateEMGBounds' 1e-6 tolerance.
	fixture.PhaseTimes[string(models.PhaseS)] = fixture.TimeValues[0] - 0.5e-6

	a := NewCCIAnalyzer()
	rows := a.buildPhaseStats(fixture)

	sBand := findRow(t, rows, "S", metricBand50ms)
	assert.True(t, sBand.HasTime, "S within 1e-6 tolerance must stay present")
	// S maps to index 0; ±50ms = MeanRange(rampA, 0-5→0, 0+5) = mean(0..5) = 2.5.
	require.False(t, math.IsNaN(sBand.Values[0]), "tolerated S drift must not blank the row")
	assert.InDelta(t, 2.5, sBand.Values[0], 1e-9)
}

// TestDropOutOfRangePhases_RemovesFromChartFacingMaps guards the codex-R3 consistency
// fix: an out-of-window phase must be dropped from PhaseTimes AND PhasePercents (the
// chart/marker source), not just blanked in Output 2 — otherwise the chart renders a
// misleading edge-clamped marker for a phase the stats row reports as absent.
func TestDropOutOfRangePhases_RemovesFromChartFacingMaps(t *testing.T) {
	a := NewCCIAnalyzer()
	result := makePhaseStatsFixture() // TimeValues [0..1.0], PhaseTimes S..L in range
	// A malformed mid-point: C pushed beyond TimeValues[last]=1.0; its PhasePercent was
	// already clamped to 100 by calculateGaitCycle — that is the misleading marker.
	result.PhaseTimes[string(models.PhaseC)] = 1.5
	result.PhasePercents = map[string]float64{
		string(models.PhaseS): 0,
		string(models.PhaseC): 100, // clamped edge marker the chart would draw
		string(models.PhaseL): 100,
	}

	a.dropOutOfRangePhases(result)

	_, hasCTime := result.PhaseTimes[string(models.PhaseC)]
	assert.False(t, hasCTime, "out-of-range C dropped from PhaseTimes (no chart marker)")
	_, hasCPct := result.PhasePercents[string(models.PhaseC)]
	assert.False(t, hasCPct, "out-of-range C dropped from PhasePercents")
	// In-range anchors/points retained — only the out-of-range phase is removed.
	_, hasS := result.PhaseTimes[string(models.PhaseS)]
	assert.True(t, hasS, "in-range S retained")
	_, hasD := result.PhaseTimes[string(models.PhaseD)]
	assert.True(t, hasD, "in-range D retained")
}

// TestBuildPhaseStats_SF8_EndToEnd runs the real SF8 subject through AnalyzeCCI and
// checks shape + anchors. Skipped when the raw EMG file is untracked/absent (CI).
// Window-mean values are NOT pinned here (would be a change-detector); the controlled
// fixture above is the value-level golden.
func TestBuildPhaseStats_SF8_EndToEnd(t *testing.T) {
	const (
		repoRelManifest = "input/分期總檔案V.14_20260527_更正資料作圖_含共收縮比值檔案與落地_BP30450_RMS0.1_0.09.csv"
		repoRelEMGDir   = "input/NSF&SF_論文分析_BP30450_RMS0.1_0.09/SF/SF8"
		sf8EMGFile      = "SF_8_BTS%_6.10_BP30450_RMS0.1_0.09.csv"
		sf8SubjectIndex = 14 // 0-based parsed index of SF8 in V.14 (header skipped; verified end-to-end 2026-06-03)
	)

	repoRoot := repoRootForTest(t)
	manifestPath := filepath.Join(repoRoot, repoRelManifest)
	emgDir := filepath.Join(repoRoot, repoRelEMGDir)
	emgPath := filepath.Join(emgDir, sf8EMGFile)

	if _, err := os.Stat(emgPath); err != nil {
		t.Skip("SF8 raw EMG not present (untracked); end-to-end covered separately")
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Skip("SF8 manifest not present; end-to-end covered separately")
	}

	a := NewCCIAnalyzer()
	result, err := a.AnalyzeCCI(context.Background(), &CCIParams{
		ManifestFile: manifestPath,
		DataFolder:   emgDir,
		SubjectIndex: sf8SubjectIndex,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.PhaseStats, 32, "SF8 must produce the fixed 32-row table")

	// Columns align with Output 1.
	for _, row := range result.PhaseStats {
		assert.Lenf(t, row.Values, len(result.PairResults),
			"row %s/%s columns must equal len(PairResults)", row.Item, row.Metric)
	}

	// Labels/order match the static schema.
	for i, spec := range phaseStatRowSpecs {
		assert.Equalf(t, spec.item, result.PhaseStats[i].Item, "row %d Item", i)
		assert.Equalf(t, spec.metric, result.PhaseStats[i].Metric, "row %d Metric", i)
	}

	// S row Time ≈ 10.633 (force 15.581 → EMG via offset 1238/250 = 4.948 → 15.581-4.948).
	sBand := findRow(t, result.PhaseStats, "S", metricBand50ms)
	require.True(t, sBand.HasTime, "S present (B2 fail-fast guarantees it)")
	assert.InDelta(t, 10.633, sBand.Time, 0.01, "S EMG time")

	// L at the 100% anchor (always present); landing rows finite.
	lBand := findRow(t, result.PhaseStats, "L", metricBand50ms)
	require.True(t, lBand.HasTime, "L present at 100% anchor")
	assert.InDelta(t, result.GaitEndTime, lBand.Time, 0.02, "L row Time at gait end (100% anchor)")

	for _, m := range []string{metricLand0to100, metricLand20to50, metricLand50to100} {
		row := findRow(t, result.PhaseStats, "L", m)
		for k, v := range row.Values {
			assert.Falsef(t, math.IsNaN(v) || math.IsInf(v, 0),
				"landing %s pair %d must be finite, got %v", m, k, v)
		}
	}
}

// TestBuildPhaseStats_Interval_MidpointWindowDiscriminates 證明 ADR-0022 值路徑:interval
// 列現為兩端點時間中點的 ±band50Half 視窗,非整段 [iFrom,iTo]。標準 ramp fixture 證不出
// (相鄰區間半寬==band50Half→整段==視窗),故此 fixture 把端點拉遠(半寬 30≫5)、用非線性資料,
// plateau 落在整段內但視窗外。列須等於視窗 kernel 且異於舊整段 kernel——皆由同一 series 即時算,無魔數。
func TestBuildPhaseStats_Interval_MidpointWindowDiscriminates(t *testing.T) {
	const n = 101
	const (
		fromIdx = 20
		toIdx   = 80
	)
	times := make([]float64, n)
	seriesA := make([]float64, n)
	seriesB := make([]float64, n)
	for i := 0; i < n; i++ {
		times[i] = float64(i) * 0.01
	}
	for i := 25; i <= 35; i++ {
		seriesA[i] = 100 // 整段[20,80]內、視窗[45,55]外
	}
	for i := 48; i <= 52; i++ {
		seriesA[i] = 10 // 視窗內→新值已知非零
	}
	for i := range seriesA {
		seriesB[i] = 2 * seriesA[i]
	}

	result := &CCIAnalysisResult{
		Subject:     "FixtureDiscriminate",
		PairResults: []CCIResult{{PairName: "A", Values: seriesA}, {PairName: "B", Values: seriesB}},
		TimeValues:  times,
		PhaseTimes: map[string]float64{
			string(models.PhaseS):  times[fromIdx],
			string(models.PhaseC):  times[35],
			string(models.PhaseD):  times[toIdx],
			string(models.PhaseT0): times[85],
			string(models.PhaseT):  times[90],
			string(models.PhaseO):  times[95],
			string(models.PhaseL):  times[100],
		},
	}

	rows := NewCCIAnalyzer().buildPhaseStats(result)
	sd := findRow(t, rows, "S-D", metricInterval)

	mid := synchronizer.FindNearestTimeIndex(times, (times[fromIdx]+times[toIdx])/2)
	require.Equal(t, 50, mid, "中點(idx 20,80)須 snap 到 50")
	wantNew := windowmean.WindowMean(seriesA, mid, band50Half) // 即時 kernel == 列值
	oldFull := windowmean.MeanRange(seriesA, fromIdx, toIdx)   // 即時 kernel == 舊行為

	assert.InDelta(t, wantNew, sd.Values[0], goldenDelta, "S-D pairA 須等於 WindowMean(mid, band50Half)")
	assert.InDelta(t, 2*wantNew, sd.Values[1], goldenDelta, "pairB=2×pairA(欄序)")
	require.Greater(t, math.Abs(oldFull-wantNew), 1.0, "fixture 須使整段≠視窗(old=%v new=%v)", oldFull, wantNew)
	assert.False(t, math.Abs(sd.Values[0]-oldFull) < goldenDelta, "S-D 不得等於舊 MeanRange=%v", oldFull)
	assert.True(t, sd.HasTime, "interval 列現帶 HasTime=true")
	assert.InDelta(t, times[mid], sd.Time, goldenDelta, "interval Time = TimeValues[mid]")
}

// repoRootForTest walks up from the test's CWD to the module root (go.mod).
func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test CWD")
		}
		dir = parent
	}
}
