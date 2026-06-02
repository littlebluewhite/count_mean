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
// Every valA is hand-derived: interval = MeanRange(iFrom,iTo) = (iFrom+iTo)/2;
// ±50ms = MeanRange(i-5,i+5) = i; ±25ms = weighted25 = i; 前100ms = MeanRange(i-10,i) = i-5;
// landing = MeanRange(iL+a, iL+b) = (140+a+b)/2 with iL=70.
func goldenRows() []expectedRow {
	return []expectedRow{
		// INTERVAL rows (區間平均): (iFrom+iTo)/2.
		{item: "S-C", metric: metricInterval, valA: 15},  // (10+20)/2
		{item: "S-D", metric: metricInterval, valA: 20},  // (10+30)/2
		{item: "C-D", metric: metricInterval, valA: 25},  // (20+30)/2
		{item: "D-T", metric: metricInterval, valA: 40},  // (30+50)/2
		{item: "D-T0", metric: metricInterval, valA: 35}, // (30+40)/2
		{item: "T0-T", metric: metricInterval, valA: 45}, // (40+50)/2
		{item: "T-O", metric: metricInterval, valA: 55},  // (50+60)/2
		{item: "O-L", metric: metricInterval, valA: 65},  // (60+70)/2

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
