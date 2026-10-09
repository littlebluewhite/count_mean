package composer

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/manifest"
)

// manifestHeaderV10 / V14:V.14 多一欄 MuscleRatioFile。
const (
	manifestHeaderV10 = "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n"
	manifestHeaderV14 = "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L,MuscleRatioFile\n"
	// manifestPhasesV10 是 EMGMotionOffset 之後的 10 個分期點欄位。
	manifestPhasesV10 = "0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8"
)

// pairLabels 是 muscle_ratio Output-1 fixture 的 4 個 pair。
var pairLabels = []string{"RA/ES", "IL/GMax", "RF/BF", "TAIO/MF"}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// writeEMG 建立最小 EMG CSV(0–2 s、1 kHz、R.RA / R.ES 兩通道)。
func writeEMG(t *testing.T, path string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("Time,R.RA,R.ES\n")
	for i := 0; i <= 2000; i++ {
		fmt.Fprintf(&b, "%.6f,%.4f,%.4f\n", float64(i)/1000.0, 100.0, 50.0)
	}
	writeFile(t, path, b.String())
}

// writeMotion 建立 motion CSV(3 行 metadata + header + rows 筆資料,Index 從 1 起)。
// channels 為 Index 之後的資料欄名。
func writeMotion(t *testing.T, path string, rows int, channels ...string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("Line 1: Metadata\nLine 2: More metadata\nLine 3: Additional info\n")
	b.WriteString("Index," + strings.Join(channels, ",") + "\n")
	for i := 1; i <= rows; i++ {
		fmt.Fprintf(&b, "%d", i)
		for range channels {
			b.WriteString(",10.5")
		}
		b.WriteString("\n")
	}
	writeFile(t, path, b.String())
}

// writeMuscleRatio 以 CSVHandler.WriteMuscleRatioOutputAll(真 writer)產出 Output-1 並搬到 path;
// NaN 由 writer 寫成空 cell。
func writeMuscleRatio(t *testing.T, path string, times []float64, ratios [][]float64) {
	t.Helper()

	dir := filepath.Dir(path)
	cfg := config.DefaultConfig()
	cfg.InputDir, cfg.OutputDir, cfg.OperateDir = dir, dir, dir
	written, err := io.NewCSVHandler(cfg).WriteMuscleRatioOutputAll(io.WriteRequest{}, io.MuscleRatioOutputAllPayload{
		Subject:    "fixture",
		PairLabels: pairLabels,
		Times:      times,
		Ratios:     ratios,
	})
	require.NoError(t, err)
	require.NoError(t, os.Rename(written, path))
}

// muscleRatioFixture 回傳 0..1 s、101 點、4 個 pair 各為常數的 Output-1 資料。
func muscleRatioFixture() ([]float64, [][]float64) {
	times := make([]float64, 0, 101)
	for i := 0; i <= 100; i++ {
		times = append(times, float64(i)/100.0)
	}
	ratios := make([][]float64, len(pairLabels))
	for k, v := range []float64{1.2, 0.8, 1.5, 0.5} {
		ratios[k] = make([]float64, len(times))
		for i := range times {
			ratios[k][i] = v
		}
	}
	return times, ratios
}

// setupV14 建 V.14 fixture:emg.csv、motion.csv(X/Y/Z、2000 列)、muscle_ratio.csv 與一列
// manifest(Subject "S1"、EMGMotionOffset 1)。回傳 manifest path 與 data folder。
func setupV14(t *testing.T) (manifestPath, dataFolder string) {
	t.Helper()
	dataFolder = t.TempDir()

	writeEMG(t, filepath.Join(dataFolder, "emg.csv"))
	writeMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000, "X", "Y", "Z")
	times, ratios := muscleRatioFixture()
	writeMuscleRatio(t, filepath.Join(dataFolder, "muscle_ratio.csv"), times, ratios)

	manifestPath = filepath.Join(dataFolder, "manifest.csv")
	writeFile(t, manifestPath, manifestHeaderV14+
		"S1,motion.csv,force.anc,emg.csv,1,"+manifestPhasesV10+",muscle_ratio.csv")
	return manifestPath, dataFolder
}

// TestLoad_AssemblesComposerInput 釘住 V.14 row 組出的 ComposerInput:EMG columnar 原樣、
// motion-index 換到 EMG 時間軸、muscle_ratio 依 pair 欄位序、phase 秒數 map 非空。
func TestLoad_AssemblesComposerInput(t *testing.T) {
	manifestPath, dataFolder := setupV14(t)

	in, err := Load(manifestPath, dataFolder, "S1")
	require.NoError(t, err)

	assert.Equal(t, "S1", in.Subject)

	require.NotNil(t, in.EMG)
	assert.Equal(t, []string{"R.RA", "R.ES"}, in.EMG.Headers)
	assert.Len(t, in.EMG.Time, 2001)
	assert.Len(t, in.EMG.Channels["R.RA"], 2001)

	// EMGMotionOffset=1:emg = (idx − 1) / 250 → idx 1 = 0 s、idx 2 = 0.004 s。
	require.NotNil(t, in.MotionData)
	assert.Equal(t, []string{"X", "Y", "Z"}, in.MotionData.Order)
	require.Len(t, in.MotionData.Time, 2000)
	assert.InDelta(t, 0.0, in.MotionData.Time[0], 1e-12)
	assert.InDelta(t, 0.004, in.MotionData.Time[1], 1e-12)
	assert.Len(t, in.MotionData.Series["X"], 2000)

	times, ratios := muscleRatioFixture()
	require.NotNil(t, in.MuscleRatioData)
	assert.Equal(t, pairLabels, in.MuscleRatioData.Order)
	assert.InDeltaSlice(t, times, in.MuscleRatioData.Time, 1e-9)
	for k, label := range pairLabels {
		assert.InDeltaSlicef(t, ratios[k], in.MuscleRatioData.Series[label], 1e-9, "pair %s", label)
	}

	assert.NotEmpty(t, in.PhaseTimesEMG)
}

// TestLoad_NoMuscleRatioFile V.10 manifest(無 MuscleRatioFile 欄)→ MuscleRatioData 為 nil
// (chart 走 2-grid)。
func TestLoad_NoMuscleRatioFile(t *testing.T) {
	dataFolder := t.TempDir()
	writeEMG(t, filepath.Join(dataFolder, "emg.csv"))
	writeMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000, "X")
	manifestPath := filepath.Join(dataFolder, "manifest.csv")
	writeFile(t, manifestPath, manifestHeaderV10+"S1,motion.csv,force.anc,emg.csv,1,"+manifestPhasesV10)

	in, err := Load(manifestPath, dataFolder, "S1")
	require.NoError(t, err)

	assert.Nil(t, in.MuscleRatioData)
	require.NotNil(t, in.MotionData)
	require.NotNil(t, in.EMG)
}

// TestLoad_PhaseTimesOnEMGAxis 釘住 PhaseTimesEMG 是 EMG 時間軸秒數(原 gui
// TestGenerateChartComposer_PhaseMarkersConvertedToEMGTime,codex P1):
//
//	emgTime = forceTime − (EMGMotionOffset − 1) / 250
//
// EMGMotionOffset=251 → 偏移 1.0 s;P0=0.5 → −0.5、P1=1.5 → 0.5。其餘分期點未提供
// (NA / motion-index 0),不在 map 內。
func TestLoad_PhaseTimesOnEMGAxis(t *testing.T) {
	dataFolder := t.TempDir()
	writeEMG(t, filepath.Join(dataFolder, "emg.csv"))
	writeMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000, "X")
	manifestPath := filepath.Join(dataFolder, "manifest.csv")
	writeFile(t, manifestPath, manifestHeaderV10+
		"S1,motion.csv,force.anc,emg.csv,251,0.5,1.5,NA,NA,NA,0,NA,NA,0,NA")

	in, err := Load(manifestPath, dataFolder, "S1")
	require.NoError(t, err)

	require.Len(t, in.PhaseTimesEMG, 2, "只含已提供的 P0 / P1:%v", in.PhaseTimesEMG)
	assert.InDelta(t, -0.5, in.PhaseTimesEMG["P0"], 1e-12, "P0 force-time 0.5 → EMG −0.5")
	assert.InDelta(t, 0.5, in.PhaseTimesEMG["P1"], 1e-12, "P1 force-time 1.5 → EMG 0.5")
}

// TestLoad_MotionFirstChannelKept 釘住 motion.Headers 已不含 Index(parser 剝掉),
// 第一個資料 channel 不可被當成 Index 略過(原 gui TestGenerateChartComposer_MotionFirstChannelRendered,
// codex P2 #3)。
func TestLoad_MotionFirstChannelKept(t *testing.T) {
	dataFolder := t.TempDir()
	writeEMG(t, filepath.Join(dataFolder, "emg.csv"))
	writeMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000, "OnlyChannel")
	manifestPath := filepath.Join(dataFolder, "manifest.csv")
	writeFile(t, manifestPath, manifestHeaderV10+"S1,motion.csv,force.anc,emg.csv,1,"+manifestPhasesV10)

	in, err := Load(manifestPath, dataFolder, "S1")
	require.NoError(t, err)

	assert.Equal(t, []string{"OnlyChannel"}, in.MotionData.Order)
	assert.Len(t, in.MotionData.Series["OnlyChannel"], 2000)
}

// TestLoad_MuscleRatioBlankCellIsNaN 釘住 muscle_ratio Output-1 的空 cell 讀成 NaN 而非 0
// (原 gui TestGenerateChartComposer_EmptyMuscleRatioCellIsNaN,codex P2 #4):0 會在圖上
// 畫出假的真值點,NaN 由 chart 渲染成斷線。
func TestLoad_MuscleRatioBlankCellIsNaN(t *testing.T) {
	manifestPath, dataFolder := setupV14(t)
	times, ratios := muscleRatioFixture()
	for k := range ratios {
		ratios[k][1] = math.NaN() // writer 把 NaN 寫成空 cell
	}
	writeMuscleRatio(t, filepath.Join(dataFolder, "muscle_ratio.csv"), times, ratios)

	in, err := Load(manifestPath, dataFolder, "S1")
	require.NoError(t, err)

	for _, label := range pairLabels {
		vals := in.MuscleRatioData.Series[label]
		require.Len(t, vals, len(times), "pair %s", label)
		assert.Truef(t, math.IsNaN(vals[1]), "pair %s 第 2 列空 cell 應為 NaN,實際 %v", label, vals[1])
		assert.Falsef(t, math.IsNaN(vals[0]), "pair %s 第 1 列應為實值", label)
	}
}

// TestLoad_SubjectNotFound Subject 不在 manifest 是輸入錯誤:wrap ErrSubjectNotFound、
// 不帶 Stage,文字與搬移前的 gui 訊息逐字相同。
func TestLoad_SubjectNotFound(t *testing.T) {
	manifestPath, dataFolder := setupV14(t)

	in, err := Load(manifestPath, dataFolder, "Nope")

	assert.Nil(t, in)
	require.ErrorIs(t, err, ErrSubjectNotFound)
	assert.Equal(t, `Subject "Nope" 不存在於分期總檔案`, err.Error())
	var loadErr *LoadError
	assert.False(t, errors.As(err, &loadErr), "輸入錯誤不帶 Stage")
}

// TestLoad_FailureStage 釘住每個載入步驟的失敗都是帶對應 Stage 的 *LoadError,
// Error() 與 cause 逐字相同,且各步驟的錯誤文字(前綴 / sentinel 原樣)不變。
func TestLoad_FailureStage(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(t *testing.T, manifestPath, dataFolder string) (manifestArg string)
		wantStage  Stage
		wantPrefix string // 非空時斷言 Error() 前綴
		wantIs     error  // 非 nil 時斷言 errors.Is 且 Error() 與其逐字相同
	}{
		{
			name: "manifest 不存在",
			mutate: func(_ *testing.T, _, dataFolder string) string {
				return filepath.Join(dataFolder, "missing_manifest.csv")
			},
			wantStage: StageManifest,
		},
		{
			name: "EMG 檔不存在",
			mutate: func(t *testing.T, manifestPath, dataFolder string) string {
				require.NoError(t, os.Remove(filepath.Join(dataFolder, "emg.csv")))
				return manifestPath
			},
			wantStage: StageEMGOpen,
		},
		{
			name: "EMG 解析失敗",
			mutate: func(t *testing.T, manifestPath, dataFolder string) string {
				writeFile(t, filepath.Join(dataFolder, "emg.csv"), "Time,R.RA\n")
				return manifestPath
			},
			wantStage: StageEMGParse,
		},
		{
			name: "MotionFile 為空",
			mutate: func(t *testing.T, manifestPath, _ string) string {
				writeFile(t, manifestPath, manifestHeaderV14+
					"S1,,force.anc,emg.csv,1,"+manifestPhasesV10+",muscle_ratio.csv")
				return manifestPath
			},
			wantStage: StageMotion,
			wantIs:    ErrMotionFileEmpty,
		},
		{
			name: "motion 檔不存在",
			mutate: func(t *testing.T, manifestPath, dataFolder string) string {
				require.NoError(t, os.Remove(filepath.Join(dataFolder, "motion.csv")))
				return manifestPath
			},
			wantStage:  StageMotion,
			wantPrefix: "Motion 路徑解析失敗: ",
		},
		{
			name: "motion 解析失敗",
			mutate: func(t *testing.T, manifestPath, dataFolder string) string {
				writeFile(t, filepath.Join(dataFolder, "motion.csv"), "")
				return manifestPath
			},
			wantStage:  StageMotion,
			wantPrefix: "解析 Motion 失敗: ",
		},
		{
			name: "muscle_ratio 檔不存在",
			mutate: func(t *testing.T, manifestPath, dataFolder string) string {
				require.NoError(t, os.Remove(filepath.Join(dataFolder, "muscle_ratio.csv")))
				return manifestPath
			},
			wantStage:  StageMuscleRatio,
			wantPrefix: "muscle_ratio 路徑解析失敗: ",
		},
		{
			name: "muscle_ratio 空檔:sentinel 原樣",
			mutate: func(t *testing.T, manifestPath, dataFolder string) string {
				writeFile(t, filepath.Join(dataFolder, "muscle_ratio.csv"), "Time (s),RA/ES\n")
				return manifestPath
			},
			wantStage: StageMuscleRatio,
			wantIs:    io.ErrMuscleRatioCSVEmpty,
		},
		{
			name: "muscle_ratio 讀取失敗:加前綴",
			mutate: func(t *testing.T, manifestPath, dataFolder string) string {
				// cell > 32KB 由 ReadCSVRecords 的 CheckCells 擋下(非 sentinel)。
				writeFile(t, filepath.Join(dataFolder, "muscle_ratio.csv"),
					"Time (s),RA/ES\n0,"+strings.Repeat("9", 40000)+"\n")
				return manifestPath
			},
			wantStage:  StageMuscleRatio,
			wantPrefix: "讀取 muscle_ratio CSV 失敗: ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manifestPath, dataFolder := setupV14(t)
			manifestArg := tc.mutate(t, manifestPath, dataFolder)

			in, err := Load(manifestArg, dataFolder, "S1")

			assert.Nil(t, in)
			var loadErr *LoadError
			require.ErrorAs(t, err, &loadErr)
			assert.Equal(t, tc.wantStage, loadErr.Stage)
			assert.Equal(t, loadErr.Err.Error(), loadErr.Error(), "Error() 必須與 cause 逐字相同")
			if tc.wantStage == StageEMGParse {
				var parseErr *manifest.EMGParseError
				assert.ErrorAs(t, err, &parseErr, "EMG 解析失敗保有 *manifest.EMGParseError")
			}
			if tc.wantPrefix != "" {
				assert.True(t, strings.HasPrefix(err.Error(), tc.wantPrefix),
					"Error() 應以 %q 開頭,實際 %q", tc.wantPrefix, err.Error())
			}
			if tc.wantIs != nil {
				require.ErrorIs(t, err, tc.wantIs)
				assert.Equal(t, tc.wantIs.Error(), err.Error(), "sentinel 原樣回傳,不加前綴")
			}
		})
	}
}
