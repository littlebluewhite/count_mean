package gui

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/cci"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"count_mean/internal/muscle_ratio"
	"count_mean/internal/phase_sync"
)

// phaseTimelineAgreementOffset 是 fixture 的 EMGMotionOffset。
//   - 力板時間欄位:emg = force − (26−1)/250 = force − 0.100
//   - motion-index 欄位(D/O):emg = (idx − 26)/250
//
// 兩條公式差 1/250 = 4 ms:D/O 若誤走力板公式會落到另一個 1 kHz 格點,下表抓得到。
const phaseTimelineAgreementOffset = 26

// phaseTimelineAgreementTable 是手算的 Phase → EMG 秒數表(全部 10 個 Phase,含 D/O)。
//
// 每個值都刻意落在 1 kHz EMG 取樣格點上:muscle_ratio Output 2 的 Time 欄是 snap 到
// 最近 sample 的時間(ADR-0014),格點上的值 snap 後不變,才能和其他 caller 共用同一張表。
var phaseTimelineAgreementTable = []struct {
	phase    models.PhasePoint
	manifest string  // manifest 欄位原文
	emg      float64 // 手算 EMG 秒數
}{
	{models.PhaseP0, "0.312", 0.212},
	{models.PhaseP1, "0.347", 0.247},
	{models.PhaseP2, "0.405", 0.305},
	{models.PhaseS, "0.523", 0.423},
	{models.PhaseC, "0.618", 0.518},
	{models.PhaseD, "177", 0.604}, // (177−26)/250
	{models.PhaseT0, "0.781", 0.681},
	{models.PhaseT, "0.902", 0.802},
	{models.PhaseO, "277", 1.004}, // (277−26)/250
	{models.PhaseL, "1.236", 1.136},
}

// phaseTimelineAgreementWant 把手算表轉成 Phase → EMG 秒數 lookup。
func phaseTimelineAgreementWant() map[models.PhasePoint]float64 {
	want := make(map[models.PhasePoint]float64, len(phaseTimelineAgreementTable))
	for _, row := range phaseTimelineAgreementTable {
		want[row.phase] = row.emg
	}
	return want
}

// setupPhaseTimelineAgreementFixture 寫出四個 caller 共用的 manifest + data folder:
// 1 個 Subject、全部 10 個 Phase、8 個右側通道的 EMG(0–1.5 s,1 kHz)、motion 2000 列、
// force 2 s。EMG 涵蓋 CCI 的 [S−150ms, L+150ms] 抽取範圍與所有 Phase。
func setupPhaseTimelineAgreementFixture(t *testing.T) (manifestPath, dataFolder string) {
	t.Helper()
	dataFolder = t.TempDir()

	writeChartComposerMinimalMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000)
	writeChartComposerMinimalForce(t, filepath.Join(dataFolder, "force.anc"), 2.0)

	var emg strings.Builder
	emg.WriteString("X [],R.RA: EMG 1,R.ES: EMG 2,R.IL: EMG 3,R.GMax: EMG 4," +
		"R.RF: EMG 5,R.BF: EMG 6,R.TA&IO: EMG 7,R.MF: EMG 8\n")
	for i := 0; i <= 1500; i++ {
		fmt.Fprintf(&emg, "%.3f,1,2,3,4,5,6,7,8\n", float64(i)/1000.0)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dataFolder, "emg.csv"), []byte(emg.String()), 0o600))

	values := make([]string, 0, len(phaseTimelineAgreementTable))
	for _, row := range phaseTimelineAgreementTable {
		values = append(values, row.manifest)
	}
	manifestContent := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		fmt.Sprintf("Agree,motion.csv,force.anc,emg.csv,%d,%s\n",
			phaseTimelineAgreementOffset, strings.Join(values, ","))
	manifestPath = filepath.Join(dataFolder, "manifest.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o600))

	return manifestPath, dataFolder
}

// TestPhaseTimelineAgreement_AllCallers 是 characterization:同一份 manifest row 下,
// CCI、PhaseSync、MuscleRatio、Chart Composer 四個 caller 算出的 Phase EMG 秒數
// 都必須等於手算表。各 caller 只差在挑哪些 Phase(policy),秒數本身不得分歧。
func TestPhaseTimelineAgreement_AllCallers(t *testing.T) {
	manifestPath, dataFolder := setupPhaseTimelineAgreementFixture(t)
	want := phaseTimelineAgreementWant()

	t.Run("CCI 排除 P0–P2,其餘 Phase 秒數等於手算表", func(t *testing.T) {
		result, err := cci.NewCCIAnalyzer().AnalyzeCCI(context.Background(), &cci.CCIParams{
			ManifestFile: manifestPath,
			DataFolder:   dataFolder,
			SubjectIndex: 0,
		})
		require.NoError(t, err)

		for _, row := range phaseTimelineAgreementTable {
			got, ok := result.PhaseTimes[string(row.phase)]
			switch row.phase {
			case models.PhaseP0, models.PhaseP1, models.PhaseP2:
				assert.Falsef(t, ok, "CCI 步態週期不含 %s", row.phase)
			default:
				require.Truef(t, ok, "CCI PhaseTimes 缺 %s", row.phase)
				assert.InDeltaf(t, row.emg, got, 1e-9, "CCI %s", row.phase)
			}
		}
		assert.InDelta(t, want[models.PhaseS], result.GaitStartTime, 1e-9, "步態起點 = S")
		assert.InDelta(t, want[models.PhaseL], result.GaitEndTime, 1e-9, "步態終點 = L")
	})

	t.Run("PhaseSync (S,L) 與 (D,O) 區間端點等於手算表", func(t *testing.T) {
		analyzer := phase_sync.NewPhaseSyncAnalyzer()
		loaded, err := analyzer.Load(&models.AnalysisParams{
			ManifestFile: manifestPath,
			DataFolder:   dataFolder,
			StartPhase:   models.PhaseS,
			EndPhase:     models.PhaseL,
			SubjectIndex: 0,
		})
		require.NoError(t, err)

		for _, pair := range [][2]models.PhasePoint{
			{models.PhaseS, models.PhaseL},
			{models.PhaseD, models.PhaseO},
		} {
			r, err := analyzer.ResolvePhaseRange(loaded, pair[0], pair[1])
			require.NoErrorf(t, err, "ResolvePhaseRange(%s, %s)", pair[0], pair[1])
			assert.InDeltaf(t, want[pair[0]], r.StartTime, 1e-9, "PhaseSync start %s", pair[0])
			assert.InDeltaf(t, want[pair[1]], r.EndTime, 1e-9, "PhaseSync end %s", pair[1])
		}
	})

	t.Run("MuscleRatio Output 2 的 Phase 列時間等於手算表", func(t *testing.T) {
		outDir := t.TempDir()
		results, err := muscle_ratio.NewAnalyzer().Analyze(context.Background(), &muscle_ratio.Params{
			ManifestFile: manifestPath,
			DataFolder:   dataFolder,
			OutputDir:    outDir,
			CSVHandler: io.NewCSVHandler(&config.AppConfig{
				OutputDir: outDir, ScalingFactor: 3, Precision: 6,
			}),
		})
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.True(t, results[0].Success, "Error=%s", results[0].Error)
		require.Empty(t, results[0].Error, "Output 2 不得被跳過")

		f, err := os.Open(results[0].OutputPhasePath)
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		rows, err := csv.NewReader(f).ReadAll()
		require.NoError(t, err)

		gotTime := make(map[string]string, len(rows))
		for _, r := range rows[1:] {
			gotTime[r[0]] = r[1]
		}
		for _, row := range phaseTimelineAgreementTable {
			assert.Equalf(t, fmt.Sprintf("%.4f", row.emg), gotTime[string(row.phase)],
				"MuscleRatio Output 2 %s", row.phase)
		}
	})

	t.Run("Chart Composer phaseTimes 含全部 10 個 Phase 且等於手算表", func(t *testing.T) {
		app := setupChartComposerTestApp(t)
		result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
			ManifestPath: manifestPath,
			DataFolder:   dataFolder,
			Subject:      "Agree",
		})
		require.NoError(t, err)
		require.True(t, result.Success, "Message: %s", result.Message)

		require.Len(t, result.PhaseTimes, len(phaseTimelineAgreementTable))
		for _, row := range phaseTimelineAgreementTable {
			assert.InDeltaf(t, row.emg, result.PhaseTimes[string(row.phase)], 1e-9,
				"Composer %s", row.phase)
		}
	})
}
