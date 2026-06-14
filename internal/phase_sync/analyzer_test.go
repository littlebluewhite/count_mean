package phase_sync //nolint:revive // underscore in package name matches directory structure

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security/fsperm"
)

// Helper function to create a temporary test CSV file.
func createTempFile(t *testing.T, content string) string {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_file.csv")
	err := os.WriteFile(tmpFile, []byte(content), fsperm.FilePerm)
	require.NoError(t, err)

	return tmpFile
}

func TestNewPhaseSyncAnalyzer(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()
	assert.NotNil(t, analyzer)
}

func TestPhaseSyncAnalyzer_LoadManifestSubjects(t *testing.T) {
	tests := []struct {
		name             string
		manifestContent  string
		expectedSubjects []string
		expectError      bool
	}{
		{
			name: "valid manifest with multiple subjects",
			manifestContent: `Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject1,motion1.csv,force1.csv,emg1.csv,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0
TestSubject2,motion2.csv,force2.csv,emg2.csv,120,1.5,2.5,3.5,4.5,5.5,280,6.5,7.5,380,8.5`,
			expectedSubjects: []string{"TestSubject1", "TestSubject2"},
			expectError:      false,
		},
		{
			name: "manifest with single subject",
			manifestContent: `Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
SingleSubject,motion.csv,force.csv,emg.csv,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`,
			expectedSubjects: []string{"SingleSubject"},
			expectError:      false,
		},
		{
			name:             "empty manifest",
			manifestContent:  `Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L`,
			expectedSubjects: []string{},
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary manifest file
			manifestFile := createTempFile(t, tt.manifestContent)

			analyzer := NewPhaseSyncAnalyzer()
			subjects, err := analyzer.LoadManifestSubjects(manifestFile)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedSubjects, subjects)
		})
	}
}

func TestPhaseSyncAnalyzer_LoadManifestSubjects_InvalidFile(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()

	// Test with non-existent file
	subjects, err := analyzer.LoadManifestSubjects("/non/existent/file.csv")
	assert.Error(t, err)
	assert.Nil(t, subjects)
	assert.Contains(t, err.Error(), "解析分期總檔案失敗")
}

func TestPhaseSyncAnalyzer_AnalyzePhaseSync_InvalidParams(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()

	tests := []struct {
		name          string
		params        *models.AnalysisParams
		expectedError string
	}{
		{
			name: "invalid manifest file",
			params: &models.AnalysisParams{
				ManifestFile: "/non/existent/file.csv",
				DataFolder:   "/tmp",
				StartPhase:   "P0",
				EndPhase:     "P2",
				SubjectIndex: 0,
			},
			expectedError: "解析分期總檔案失敗",
		},
		{
			name: "invalid subject index - negative",
			params: func() *models.AnalysisParams {
				manifestContent := "Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset," +
					"P0,P1,P2,S,C,D,T0,T,O,L\nTest,m.csv,f.csv,e.csv,100,1,2,3,4,5,250,6,7,350,8"
				manifestFile := createTempFile(t, manifestContent)
				return &models.AnalysisParams{
					ManifestFile: manifestFile,
					DataFolder:   "/tmp",
					StartPhase:   "P0",
					EndPhase:     "P2",
					SubjectIndex: -1,
				}
			}(),
			expectedError: "無效的主題索引",
		},
		{
			name: "invalid subject index - too large",
			params: func() *models.AnalysisParams {
				manifestContent := "Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset," +
					"P0,P1,P2,S,C,D,T0,T,O,L\nTest,m.csv,f.csv,e.csv,100,1,2,3,4,5,250,6,7,350,8"
				manifestFile := createTempFile(t, manifestContent)
				return &models.AnalysisParams{
					ManifestFile: manifestFile,
					DataFolder:   "/tmp",
					StartPhase:   "P0",
					EndPhase:     "P2",
					SubjectIndex: 10,
				}
			}(),
			expectedError: "無效的主題索引",
		},
		{
			name: "invalid phase order",
			params: func() *models.AnalysisParams {
				manifestContent := "Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset," +
					"P0,P1,P2,S,C,D,T0,T,O,L\nTest,m.csv,f.csv,e.csv,100,1,2,3,4,5,250,6,7,350,8"
				manifestFile := createTempFile(t, manifestContent)
				return &models.AnalysisParams{
					ManifestFile: manifestFile,
					DataFolder:   "/tmp",
					StartPhase:   "P2",
					EndPhase:     "P0", // Invalid: P2 should come before P0
					SubjectIndex: 0,
				}
			}(),
			expectedError: "start phase must be before end phase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats, err := analyzer.AnalyzePhaseSync(context.Background(), tt.params)
			assert.Error(t, err)
			assert.Nil(t, stats)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestGenerateAnalysisReport(t *testing.T) {
	stats := &models.EMGStatistics{
		Subject:      "TestSubject",
		StartPhase:   "P0",
		EndPhase:     "P2",
		StartTime:    0.0,
		EndTime:      2.0,
		ChannelNames: []string{"Ch1", "Ch2"},
		ChannelMeans: map[string]float64{
			"Ch1": 100.5,
			"Ch2": 200.3,
		},
		ChannelMaxes: map[string]float64{
			"Ch1": 150.0,
			"Ch2": 250.0,
		},
	}

	report := GenerateAnalysisReport(stats)
	assert.NotEmpty(t, report)
	assert.Contains(t, report, "TestSubject")
	assert.Contains(t, report, "P0")
	assert.Contains(t, report, "P2")
}

func TestPhaseSyncAnalyzer_AnalyzePhaseSync_Integration(t *testing.T) {
	// This test exercises the basic error-handling path: with EMG present but Motion
	// absent, the pipeline must surface "Motion 檔案不存在".
	//
	// Pipeline order is EMG → Motion → Force. After migrating to the manifest.OpenDataFile
	// door, EMG validation opens the file atomically and so now also checks existence
	// (the old lexical resolver did not). We therefore provide a real EMG file in the
	// data folder so EMG validation passes and Motion becomes the first *missing* file —
	// keeping this test focused on the Motion-not-found error contract.
	analyzer := NewPhaseSyncAnalyzer()

	dataFolder := t.TempDir()
	emgContent := `Time,Ch1,Ch2,Ch3,Ch4,Ch5,Ch6
0.0,10,20,30,40,50,60
0.001,11,21,31,41,51,61
`
	require.NoError(t, os.WriteFile(filepath.Join(dataFolder, "emg.csv"),
		[]byte(emgContent), fsperm.FilePerm))

	// Create a basic manifest
	manifestContent := `Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,emg.csv,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`

	manifestFile := createTempFile(t, manifestContent)

	params := &models.AnalysisParams{
		ManifestFile: manifestFile,
		DataFolder:   dataFolder, // EMG present; motion.csv inside doesn't exist → "Motion 檔案不存在"
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	// EMG validation passes (file present); Motion file is absent and is validated next.
	stats, err := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, err)
	assert.Nil(t, stats)
	// The error should be about Motion file validation since EMG passed and Motion is next.
	assert.Contains(t, err.Error(), "Motion 檔案不存在")
}

func TestPhaseSyncAnalyzer_AnalyzePhaseSync_AbsolutePath(t *testing.T) {
	// manifest 檔名契約為「相對於 DataFolder」。phase_sync 改走 security.ResolveLenientPath
	// 後，manifest 內的 absolute path 一律被拒（Unix `/...` 與 Windows `C:\...` 皆然，
	// 見 lenient_path.go 的 IsAbs / leading-slash 守門），對齊 cci/muscle_ratio。
	//
	// 這同時消除了舊 GetSafePath 對 absolute manifest path 的跨平台不一致（Unix 把
	// 絕對路徑 Join 進 baseFolder 巧合 nested-pass、Windows 產生 syntax-error 路徑），
	// 因此本 test 不再需要 Windows skip——拒絕行為現在跨平台一致。
	analyzer := NewPhaseSyncAnalyzer()

	tmpDir := t.TempDir()
	emgFile := filepath.Join(tmpDir, "emg.csv")
	motionFile := filepath.Join(tmpDir, "motion.csv")

	manifestContent := fmt.Sprintf(`Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,%s,force.csv,%s,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`, motionFile, emgFile)

	manifestFile := createTempFile(t, manifestContent)

	params := &models.AnalysisParams{
		ManifestFile: manifestFile,
		DataFolder:   t.TempDir(),
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	// EMG 在 pipeline 中先於 Motion 驗證；absolute EMG path 直接被 ResolveLenientPath 拒，
	// 錯誤是 EMG 路徑驗證失敗（相對路徑契約），而非下游的「Motion 檔案不存在」。
	stats, err := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, err)
	assert.Nil(t, stats)
	assert.Contains(t, err.Error(), "EMG 檔案路徑驗證失敗",
		"manifest 內 absolute path 應被拒（檔名須相對於 DataFolder）")
}

// TestPhaseSyncAnalyzer_ResolvePhaseRange_RejectsNegativeForceTime釘住:
// phase_sync 入口對「對應到 force-time 的負 phase point」必須 fail-fast。manifest
// parseFloat 允許負值(機械校準偏移,muscle_ratio batch 走時間序列 dump 仍可用),
// 但 phase_sync 用「time × frequency」算 motion-index 對負時間沒有有效意義,
// silently 滑入下游可能撞 boundary 或產出污染結果,fail-fast 反映實際 invariant。
//
// motion-index 型 phase point(D/O)是 frame number 不是時間,負值已由
// ValidatePhaseManifest 攔下,此 test 只覆蓋 force-time 路徑(P0/P1/P2/S/C/T0/T/L)。
func TestPhaseSyncAnalyzer_ResolvePhaseRange_RejectsNegativeForceTime(t *testing.T) {
	cases := []struct {
		name       string
		startPhase models.PhasePoint
		endPhase   models.PhasePoint
		negField   string
		points     models.PhasePoints
	}{
		{
			name:       "negative P0 rejected",
			startPhase: models.PhaseP0,
			endPhase:   models.PhaseP2,
			negField:   "P0",
			points: models.PhasePoints{
				P0: models.MakeOpt(-0.5),
				P1: models.MakeOpt(0.5),
				P2: models.MakeOpt(1.0),
			},
		},
		{
			name:       "negative P2 (end) rejected",
			startPhase: models.PhaseP0,
			endPhase:   models.PhaseP2,
			negField:   "P2",
			points: models.PhasePoints{
				P0: models.MakeOpt(0.0),
				P2: models.MakeOpt(-0.3),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			analyzer := NewPhaseSyncAnalyzer()

			loaded := &LoadedPhaseSyncContext{
				Manifest: &models.PhaseManifest{
					Subject:         "T",
					MotionFile:      "m.csv",
					ForceFile:       "f.csv",
					EMGFile:         "e.csv",
					EMGMotionOffset: 100,
					PhasePoints:     tc.points,
				},
				EMGData: &models.PhaseSyncEMGData{
					Time: []float64{0.0, 1.0, 2.0},
				},
			}

			_, err := analyzer.ResolvePhaseRange(loaded, tc.startPhase, tc.endPhase)
			require.Error(t, err, "ResolvePhaseRange 必須 reject 負 force-time")
			require.ErrorIs(t, err, ErrNegativePhaseTime,
				"必須是 ErrNegativePhaseTime sentinel,方便 caller 用 errors.Is 區分")
		})
	}
}

// TestPhaseSyncAnalyzer_ResolvePhaseRange_AllowsMotionIndex (配套) 釘住:
// motion-index 型 phase point(D/O)不被 force-time reject 影響,因 D/O 是 frame
// number 而非時間,負值由 ValidatePhaseManifest 攔下,ResolvePhaseRange 不重複
// 檢查。此 case 用 D > 0 / O > 0 normal motion-index 走完路徑,確保 不誤殺。
func TestPhaseSyncAnalyzer_ResolvePhaseRange_AllowsMotionIndex(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()

	loaded := &LoadedPhaseSyncContext{
		Manifest: &models.PhaseManifest{
			Subject:         "T",
			MotionFile:      "m.csv",
			ForceFile:       "f.csv",
			EMGFile:         "e.csv",
			EMGMotionOffset: 100,
			PhasePoints: models.PhasePoints{
				D: 200, // 合法 motion-index
				O: 300, // 合法 motion-index
			},
		},
		EMGData: &models.PhaseSyncEMGData{
			Time: []float64{0.0, 1.0, 2.0, 3.0, 4.0},
		},
	}

	// D / O 都是 motion-index,合法值不該被 reject。後續 GetPhaseTimeRange 取 EMG time
	// 可能因為 EMGMotionOffset / motion-index 換算超出 [0, 4] 而 fail,但**錯誤類型**
	// 不能是 ErrNegativePhaseTime。
	_, err := analyzer.ResolvePhaseRange(loaded, models.PhaseD, models.PhaseO)
	if err != nil {
		require.NotErrorIs(t, err, ErrNegativePhaseTime,
			"motion-index 不該觸發 ErrNegativePhaseTime; err=%v", err)
	}
}

// Benchmark test.
func BenchmarkPhaseSyncAnalyzer_LoadManifestSubjects(b *testing.B) {
	// Create a large manifest file
	content := `Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
`
	for i := 0; i < 1000; i++ {
		content += fmt.Sprintf(
			"Subject%d,motion%d.csv,force%d.csv,emg%d.csv,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0\n",
			i, i, i, i)
	}

	tmpDir := b.TempDir()
	manifestFile := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestFile, []byte(content), fsperm.FilePerm)
	require.NoError(b, err)

	analyzer := NewPhaseSyncAnalyzer()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := analyzer.LoadManifestSubjects(manifestFile)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// TestValidateEMGTimeRange_EmptyTimeFailFast 釘住 Finding [10]:
// validateEMGTimeRange 對 nil EMGData 或空 Time slice 必須 fail-fast 回
// parsers.ErrNilData。舊行為:空 Time 時 emgMinTime=emgMaxTime=0.0 靜默通過
// → 0.0 範圍比對讓任何 StartTime<0 都誤判合法;nil 指標直接 panic。
//
// 呼叫路徑已確認:ResolvePhaseRange(:464) → validateEMGTimeRange,
// 且 LoadedPhaseSyncContext 的 EMGData 欄位可由 callers 直接注入 nil。
func TestValidateEMGTimeRange_EmptyTimeFailFast(t *testing.T) {
	// 建立最小可通過 ResolvePhaseRange 前置檢查的 manifest。
	// P0=1.0, P1=2.0 → 正時間,通過 rejectNegativeForceTime 與 ValidatePhaseOrder。
	baseManifest := &models.PhaseManifest{
		Subject:         "T",
		MotionFile:      "m.csv",
		ForceFile:       "f.csv",
		EMGFile:         "e.csv",
		EMGMotionOffset: 0,
		PhasePoints: models.PhasePoints{
			P0: models.MakeOpt(1.0),
			P1: models.MakeOpt(2.0),
		},
	}

	cases := []struct {
		name    string
		emgData *models.PhaseSyncEMGData
	}{
		{
			name:    "nil EMGData 不 panic、回 ErrNilData",
			emgData: nil,
		},
		{
			name:    "空 Time slice 回 ErrNilData",
			emgData: &models.PhaseSyncEMGData{Time: []float64{}},
		},
		{
			name:    "nil Time slice 回 ErrNilData",
			emgData: &models.PhaseSyncEMGData{Time: nil},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			analyzer := NewPhaseSyncAnalyzer()
			loaded := &LoadedPhaseSyncContext{
				Manifest: baseManifest,
				EMGData:  tc.emgData,
			}

			_, err := analyzer.ResolvePhaseRange(loaded, models.PhaseP0, models.PhaseP1)
			require.Error(t, err, "空/nil EMG 必須 fail-fast")
			require.True(t, errors.Is(err, parsers.ErrNilData),
				"error 必須包裝 parsers.ErrNilData;實際 err=%v", err)
		})
	}
}
