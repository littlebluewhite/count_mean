package io

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/calculator"
	"count_mean/internal/cci"
	"count_mean/internal/config"
	"count_mean/internal/csvutil"
	"count_mean/internal/models"
	"count_mean/internal/security/redact"
)

// placementCase 描述一種 CSV 輸出(Output placement 的一列),在 subDir 下寫出。
// wantName 是逐字的輸出檔名:Subject-based 由 subject "subj_01" 推導(golden harness 比對的
// byte-identity 契約,不可用實作自身算);File-based 就是 caller 給的檔名。
type placementCase struct {
	name     string
	wantName string
	write    func(h *CSVHandler, subDir string) (string, error)
}

// placementCases 是全部 10 種 CSV:7 種 Subject-based + 3 種 File-based。
func placementCases() []placementCase {
	return append(subjectPlacementCases(), filePlacementCases()...)
}

// filePlacementCases 是 3 種 File-based writer(檔名由 caller 給,SubDir 有作用)。
func filePlacementCases() []placementCase {
	headers := []string{"Time", "Ch1"}
	maxMean := []models.MaxMeanResult{{ColumnIndex: 1, StartTime: 1, EndTime: 2, MaxMean: 100}}
	dataset := &models.EMGDataset{
		Headers: headers,
		Data:    []models.EMGData{{Time: 0.1, Channels: []float64{1}}},
	}
	phases := &calculator.AnalyzeResult{
		PhaseResults: []models.PhaseAnalysisResult{
			{PhaseName: "Stance", MaxValues: map[int]float64{0: 2}, MeanValues: map[int]float64{0: 1}},
		},
	}

	return []placementCase{
		{
			name: "MaxMean", wantName: "trial_maxmean.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WriteMaxMean(WriteRequest{Filename: "trial_maxmean.csv", SubDir: sub},
					headers, maxMean, 0.5, 3.0)
			},
		},
		{
			name: "Normalized", wantName: "trial_normalized.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WriteNormalized(WriteRequest{Filename: "trial_normalized.csv", SubDir: sub}, dataset)
			},
		},
		{
			name: "PhaseAnalysis", wantName: "trial_phase.csv",
			write: func(h *CSVHandler, sub string) (string, error) {
				return h.WritePhaseAnalysis(WriteRequest{Filename: "trial_phase.csv", SubDir: sub}, headers, phases)
			},
		},
	}
}

// subjectPlacementCases 是 7 種 Subject-based writer(檔名由 subject 推導)。
func subjectPlacementCases() []placementCase {
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

// Output placement 的共用契約(10 種 CSV 全部適用):
// 檔名逐字、敏感目錄被拒(先驗證再建目錄)、不留 tmp、BOM 照 config.BOMEnabled、回傳路徑存在。
func TestOutputPlacement(t *testing.T) {
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

			// BOM 只照 config.BOMEnabled:開 → 帶 BOM,關 → 不帶。
			t.Run("BOM", func(t *testing.T) {
				t.Parallel()
				for _, bom := range []bool{true, false} {
					dir := t.TempDir()
					cfg := config.DefaultConfig()
					cfg.InputDir, cfg.OperateDir, cfg.OutputDir = dir, dir, dir
					cfg.BOMEnabled = bom

					got, err := tc.write(NewCSVHandler(cfg), "")
					require.NoError(t, err)

					content, readErr := os.ReadFile(got) //nolint:gosec // t.TempDir 內的測試檔
					require.NoError(t, readErr)
					require.Equal(t, bom, bytes.HasPrefix(content, csvutil.BOMBytes()),
						"BOMEnabled=%v", bom)
				}
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

// File-based writer 的 SubDir:落在 OutputDir/SubDir 內;逸出的 SubDir 被拒,且 OutputDir 不留任何東西。
func TestOutputPlacement_SubDir(t *testing.T) {
	t.Parallel()

	for _, tc := range filePlacementCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

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
func TestOutputPlacement_EscapeErrorCarriesNoSubDir(t *testing.T) {
	t.Parallel()

	const secret = "PatientX_escape"

	for _, tc := range filePlacementCases() {
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
func TestOutputPlacement_MkdirFailureCarriesNoDirPath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("以一般檔案佔住目錄位置的 ENOTDIR 行為僅在 unix 驗證")
	}

	const secret = "PatientAlice_PHI_out"

	for _, tc := range filePlacementCases() {
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
