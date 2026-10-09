package io

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/cci"
	"count_mean/internal/config"
	"count_mean/internal/models"
	"count_mean/internal/security/redact"
)

// placementCase 描述一個 Subject-based writer:在 subDir 下以 subject "subj_01" 寫出,
// wantName 是逐字的輸出檔名(golden harness 比對的 byte-identity 契約,不可用實作自身算)。
type placementCase struct {
	name     string
	wantName string
	write    func(h *CSVHandler, subDir string) (string, error)
}

func placementCases() []placementCase {
	stats := &models.EMGStatistics{
		Subject:      "subj_01",
		StartPhase:   models.PhaseP1,
		EndPhase:     models.PhaseC,
		StartTime:    0,
		EndTime:      1,
		ChannelNames: []string{"Ch1"},
		ChannelMeans: map[string]float64{"Ch1": 1},
		ChannelMaxes: map[string]float64{"Ch1": 2},
	}
	cciResult := &cci.CCIAnalysisResult{
		Subject:       "subj_01",
		GaitStartTime: 0,
		GaitEndTime:   1,
		TimeValues:    []float64{0, 0.5, 1},
		PairResults:   []cci.CCIResult{{PairName: "P1", Values: []float64{0.1, 0.2, 0.3}}},
		PhaseStats: []cci.CCIPhaseStatRow{
			{Item: "IC", Metric: "mean", Values: []float64{1}},
		},
	}
	emg := &models.PhaseSyncEMGData{
		Time:     []float64{0, 0.001},
		Channels: map[string][]float64{"MuscleA": {0.1, 0.2}},
		Headers:  []string{"MuscleA"},
	}
	ctx := context.Background()

	return []placementCase{
		{
			name: "PhaseSyncResult", wantName: "subj_01_P1-C_statistics.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WritePhaseSyncResult(WriteRequest{SubDir: sub}, stats)
			},
		},
		{
			name: "NormalizedPhaseSyncResult", wantName: "subj_01_normalized_norm-P0-L_stats-P1-C.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WriteNormalizedPhaseSyncResult(WriteRequest{SubDir: sub}, stats,
					models.PhaseP0, models.PhaseL)
			},
		},
		{
			name: "NormalizedPhaseSyncEMG", wantName: "subj_01_normalized.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WriteNormalizedPhaseSyncEMG(WriteRequest{SubDir: sub}, emg, "subj_01")
			},
		},
		{
			name: "CCIResult", wantName: "subj_01_CCI_Rudolph.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WriteCCIResult(ctx, WriteRequest{SubDir: sub}, cciResult)
			},
		},
		{
			name: "CCIPhasesResult", wantName: "subj_01_CCI_Rudolph_phases.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WriteCCIPhasesResult(ctx, WriteRequest{SubDir: sub}, cciResult)
			},
		},
		{
			name: "MuscleRatioOutputAll", wantName: "subj_01_muscle_ratio.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WriteMuscleRatioOutputAll(WriteRequest{SubDir: sub}, MuscleRatioOutputAllPayload{
					Subject: "subj_01", PairLabels: []string{"R1"},
					Times: []float64{0, 1}, Ratios: [][]float64{{1, 2}},
				})
			},
		},
		{
			name: "MuscleRatioOutputPhases", wantName: "subj_01_muscle_ratio_phases_avg11.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WriteMuscleRatioOutputPhases(WriteRequest{SubDir: sub}, MuscleRatioOutputPhasesPayload{
					Subject: "subj_01", PairLabels: []string{"R1"},
					Points: []MuscleRatioPhasePoint{{Name: "P0", Time: 0, Values: []float64{1}}},
				})
			},
		},
	}
}

// Subject-based writer 的 placement 契約(Subject output placement):
// 檔名逐字、落在 OutputDir(/SubDir)內、逸出與敏感目錄被拒、不留 tmp、帶 BOM、回傳路徑存在。
func TestSubjectWriters_Placement(t *testing.T) {
	t.Parallel()

	for _, tc := range placementCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			t.Run("Filename", func(t *testing.T) {
				t.Parallel()
				h, dir := newFormatAwareTestHandler(t)
				got, err := tc.write(h, "")
				require.NoError(t, err)
				require.Equal(t, filepath.Join(dir, tc.wantName), got)
			})

			t.Run("StaysInOutputDir", func(t *testing.T) {
				t.Parallel()
				h, dir := newFormatAwareTestHandler(t)
				got, err := tc.write(h, "")
				require.NoError(t, err)
				require.Equal(t, dir, filepath.Dir(got))

				got, err = tc.write(h, "sub")
				require.NoError(t, err)
				require.Equal(t, filepath.Join(dir, "sub"), filepath.Dir(got))
			})

			t.Run("SubDirEscapeRejected", func(t *testing.T) {
				t.Parallel()
				for _, sub := range []string{"../evil", "../../etc", "../x"} {
					h, dir := newFormatAwareTestHandler(t)
					got, err := tc.write(h, sub)
					require.Error(t, err, sub)
					require.Empty(t, got, sub)
					require.Contains(t, err.Error(), "輸出路徑", sub)

					entries, readErr := os.ReadDir(dir)
					require.NoError(t, readErr)
					require.Empty(t, entries, "拒絕時 OutputDir 內不得有任何殘留: %s", sub)
				}
			})

			t.Run("SensitiveOutputDirRejected", func(t *testing.T) {
				t.Parallel()
				if runtime.GOOS == "windows" {
					t.Skip("敏感位置字面值以 POSIX 路徑表示")
				}
				// 目錄根本身(/etc、<tmp>/.ssh)與其子孫等價命中敏感位置(ADR-0038)。
				for _, outDir := range []string{"/etc", filepath.Join(t.TempDir(), ".ssh")} {
					cfg := config.DefaultConfig()
					cfg.InputDir, cfg.OperateDir, cfg.OutputDir = outDir, outDir, outDir
					h := NewCSVHandler(cfg)

					got, err := tc.write(h, "")
					require.Error(t, err, outDir)
					require.Empty(t, got, outDir)
					require.Contains(t, err.Error(), "輸出路徑無效", outDir)
					if outDir != "/etc" {
						require.NoDirExists(t, outDir, "拒絕時不得建立敏感目錄")
					}
				}
			})

			t.Run("NoStrayTmp", func(t *testing.T) {
				t.Parallel()
				h, dir := newFormatAwareTestHandler(t)
				_, err := tc.write(h, "")
				require.NoError(t, err)

				entries, readErr := os.ReadDir(dir)
				require.NoError(t, readErr)
				require.Len(t, entries, 1)
				require.False(t, strings.Contains(entries[0].Name(), ".tmp"), entries[0].Name())
			})

			t.Run("BOM", func(t *testing.T) {
				t.Parallel()
				h, _ := newFormatAwareTestHandler(t)
				got, err := tc.write(h, "")
				require.NoError(t, err)

				content, readErr := os.ReadFile(got) //nolint:gosec // t.TempDir 內的測試檔
				require.NoError(t, readErr)
				require.True(t, len(content) >= 3 &&
					content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF)
			})

			t.Run("ReturnedPathExists", func(t *testing.T) {
				t.Parallel()
				h, _ := newFormatAwareTestHandler(t)
				got, err := tc.write(h, "sub")
				require.NoError(t, err)
				require.FileExists(t, got)
			})
		})
	}
}

// subject 帶分隔符 → Sanitize 收斂成單一 segment(原 calculator.GenerateOutputFileName 的逐字特徵)。
func TestWritePhaseSyncResult_SubjectWithSeparatorStaysOneSegment(t *testing.T) {
	t.Parallel()

	h, dir := newFormatAwareTestHandler(t)
	stats := &models.EMGStatistics{
		Subject: "a/b", StartPhase: models.PhaseS, EndPhase: models.PhaseT,
		ChannelNames: []string{"Ch1"},
		ChannelMeans: map[string]float64{"Ch1": 1}, ChannelMaxes: map[string]float64{"Ch1": 2},
	}

	got, err := h.WritePhaseSyncResult(WriteRequest{}, stats)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "a_b_S-T_statistics.csv"), got)
}

// 逸出被拒時,錯誤文字不得帶出 SubDir(可能是病患資料夾名)—— 只留固定訊息 + 哨兵 error。
func TestSubjectWriters_EscapeErrorCarriesNoSubDir(t *testing.T) {
	t.Parallel()

	const secret = "PatientX_escape"

	for _, tc := range placementCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, _ := newFormatAwareTestHandler(t)

			_, err := tc.write(h, "../"+secret)
			require.ErrorIs(t, err, errOutputPathEscapesOutputDir)
			require.NotContains(t, err.Error(), secret)
		})
	}
}

// MkdirAll 失敗時,錯誤文字不得帶出目錄路徑(末段可能是病患資料夾名,sink 只會遮到最後一段之前):
// 只包底層 errno,errors.Is 仍可比對。
func TestSubjectWriters_MkdirFailureCarriesNoDirPath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("以一般檔案佔住目錄位置的 ENOTDIR 行為僅在 unix 驗證")
	}

	const secret = "PatientAlice_PHI_out"

	for _, tc := range placementCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, dir := newFormatAwareTestHandler(t)
			// 以一般檔案佔住 SubDir 位置 → MkdirAll 失敗(ENOTDIR)
			require.NoError(t, os.WriteFile(filepath.Join(dir, secret), []byte("x"), 0o600))

			_, err := tc.write(h, secret)
			require.Error(t, err)
			require.ErrorIs(t, err, syscall.ENOTDIR)
			require.Contains(t, err.Error(), "輸出目錄建立失敗")
			require.NotContains(t, redact.Paths(err.Error()), secret)
		})
	}
}
