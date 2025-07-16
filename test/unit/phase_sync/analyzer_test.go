package phase_sync

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/models"
	"count_mean/internal/phase_sync"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to create a temporary test file
func createTempFile(t *testing.T, content string, suffix string) string {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test_file"+suffix)
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	require.NoError(t, err)
	return tmpFile
}

func TestNewPhaseSyncAnalyzer(t *testing.T) {
	analyzer := phase_sync.NewPhaseSyncAnalyzer()
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
			manifestFile := createTempFile(t, tt.manifestContent, ".csv")

			analyzer := phase_sync.NewPhaseSyncAnalyzer()
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
	analyzer := phase_sync.NewPhaseSyncAnalyzer()

	// Test with non-existent file
	subjects, err := analyzer.LoadManifestSubjects("/non/existent/file.csv")
	assert.Error(t, err)
	assert.Nil(t, subjects)
	assert.Contains(t, err.Error(), "解析分期總檔案失敗")
}

func TestPhaseSyncAnalyzer_ExportResults(t *testing.T) {
	analyzer := phase_sync.NewPhaseSyncAnalyzer()

	// Create test statistics
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

	// Create temporary output directory
	outputDir := t.TempDir()

	// Test export
	outputPath, err := analyzer.ExportResults(stats, outputDir)
	assert.NoError(t, err)
	assert.NotEmpty(t, outputPath)

	// Verify file exists
	_, err = os.Stat(outputPath)
	assert.NoError(t, err)

	// Verify file is in correct directory
	assert.True(t, filepath.IsAbs(outputPath))
	assert.Contains(t, outputPath, outputDir)
}

func TestPhaseSyncAnalyzer_AnalyzePhaseSync_InvalidParams(t *testing.T) {
	analyzer := phase_sync.NewPhaseSyncAnalyzer()

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
				manifestFile := createTempFile(t, "Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\nTest,m.csv,f.csv,e.csv,100,1,2,3,4,5,250,6,7,350,8", ".csv")
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
				manifestFile := createTempFile(t, "Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\nTest,m.csv,f.csv,e.csv,100,1,2,3,4,5,250,6,7,350,8", ".csv")
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
				manifestFile := createTempFile(t, "Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\nTest,m.csv,f.csv,e.csv,100,1,2,3,4,5,250,6,7,350,8", ".csv")
				return &models.AnalysisParams{
					ManifestFile: manifestFile,
					DataFolder:   "/tmp",
					StartPhase:   "P2",
					EndPhase:     "P0", // Invalid: P2 should come before P0
					SubjectIndex: 0,
				}
			}(),
			expectedError: "開始分期點 P2 必須在結束分期點 P0 之前",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stats, err := analyzer.AnalyzePhaseSync(tt.params)
			assert.Error(t, err)
			assert.Nil(t, stats)
			assert.Contains(t, err.Error(), tt.expectedError)
		})
	}
}

func TestFindDataFiles(t *testing.T) {
	// Create temporary directory with test files
	tmpDir := t.TempDir()

	// Create test files
	testFiles := []string{
		"test1.csv",
		"test2.csv",
		"data1.txt",
		"data2.txt",
		"other.log",
	}

	for _, filename := range testFiles {
		filepath := filepath.Join(tmpDir, filename)
		err := os.WriteFile(filepath, []byte("test content"), 0644)
		require.NoError(t, err)
	}

	tests := []struct {
		name        string
		patterns    []string
		expectedNum int
	}{
		{
			name:        "find CSV files",
			patterns:    []string{"*.csv"},
			expectedNum: 2,
		},
		{
			name:        "find TXT files",
			patterns:    []string{"*.txt"},
			expectedNum: 2,
		},
		{
			name:        "find multiple patterns",
			patterns:    []string{"*.csv", "*.txt"},
			expectedNum: 4,
		},
		{
			name:        "no matching files",
			patterns:    []string{"*.pdf"},
			expectedNum: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := phase_sync.FindDataFiles(tmpDir, tt.patterns)
			assert.NoError(t, err)
			assert.Len(t, files, tt.expectedNum)

			// Verify all files exist
			for _, file := range files {
				_, err := os.Stat(file)
				assert.NoError(t, err)
			}
		})
	}
}

func TestFindDataFiles_InvalidPattern(t *testing.T) {
	// Test with invalid pattern
	files, err := phase_sync.FindDataFiles("/tmp", []string{"["})
	assert.Error(t, err)
	assert.Nil(t, files)
	assert.Contains(t, err.Error(), "搜索檔案失敗")
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

	report := phase_sync.GenerateAnalysisReport(stats)
	assert.NotEmpty(t, report)
	assert.Contains(t, report, "TestSubject")
	assert.Contains(t, report, "P0")
	assert.Contains(t, report, "P2")
}

func TestValidateDataFiles(t *testing.T) {
	// Create temporary directory and files
	tmpDir := t.TempDir()

	// Create test files
	emgFile := filepath.Join(tmpDir, "emg.csv")
	motionFile := filepath.Join(tmpDir, "motion.csv")
	forceFile := filepath.Join(tmpDir, "force.csv")

	err := os.WriteFile(emgFile, []byte("test"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(motionFile, []byte("test"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(forceFile, []byte("test"), 0644)
	require.NoError(t, err)

	tests := []struct {
		name        string
		manifest    models.PhaseManifest
		expectError bool
		errorMsg    string
	}{
		{
			name: "all files exist",
			manifest: models.PhaseManifest{
				EMGFile:    "emg.csv",
				MotionFile: "motion.csv",
				ForceFile:  "force.csv",
			},
			expectError: false,
		},
		{
			name: "missing EMG file",
			manifest: models.PhaseManifest{
				EMGFile:    "missing_emg.csv",
				MotionFile: "motion.csv",
				ForceFile:  "force.csv",
			},
			expectError: true,
			errorMsg:    "找不到 EMG 檔案",
		},
		{
			name: "missing Motion file",
			manifest: models.PhaseManifest{
				EMGFile:    "emg.csv",
				MotionFile: "missing_motion.csv",
				ForceFile:  "force.csv",
			},
			expectError: true,
			errorMsg:    "找不到 Motion 檔案",
		},
		{
			name: "missing Force file",
			manifest: models.PhaseManifest{
				EMGFile:    "emg.csv",
				MotionFile: "motion.csv",
				ForceFile:  "missing_force.csv",
			},
			expectError: true,
			errorMsg:    "找不到力板檔案",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := phase_sync.ValidateDataFiles(tmpDir, tt.manifest)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPhaseSyncAnalyzer_AnalyzePhaseSync_Integration(t *testing.T) {
	// This test would require setting up complete test data files
	// For now, we'll test the basic error handling paths

	analyzer := phase_sync.NewPhaseSyncAnalyzer()

	// Create a basic manifest
	manifestContent := `Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,emg.csv,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`

	manifestFile := createTempFile(t, manifestContent, ".csv")

	params := &models.AnalysisParams{
		ManifestFile: manifestFile,
		DataFolder:   "/tmp",
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	// This will fail because the EMG file doesn't exist, but it tests the flow
	stats, err := analyzer.AnalyzePhaseSync(params)
	assert.Error(t, err)
	assert.Nil(t, stats)
	// The error should be about parsing the EMG file since it doesn't exist
	assert.Contains(t, err.Error(), "解析 EMG 檔案失敗")
}

func TestPhaseSyncAnalyzer_AnalyzePhaseSync_AbsolutePath(t *testing.T) {
	// Test handling of absolute paths in manifest
	analyzer := phase_sync.NewPhaseSyncAnalyzer()

	// Create a manifest with absolute path
	tmpDir := t.TempDir()
	emgFile := filepath.Join(tmpDir, "emg.csv")

	manifestContent := fmt.Sprintf(`Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,%s,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`, emgFile)

	manifestFile := createTempFile(t, manifestContent, ".csv")

	params := &models.AnalysisParams{
		ManifestFile: manifestFile,
		DataFolder:   "/some/other/path", // This should be ignored for absolute paths
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	// This will fail because the EMG file doesn't exist, but it tests the absolute path handling
	stats, err := analyzer.AnalyzePhaseSync(params)
	assert.Error(t, err)
	assert.Nil(t, stats)
	// The error should mention the absolute path
	assert.Contains(t, err.Error(), emgFile)
}

// Benchmark test
func BenchmarkPhaseSyncAnalyzer_LoadManifestSubjects(b *testing.B) {
	// Create a large manifest file
	content := `Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
`
	for i := 0; i < 1000; i++ {
		content += fmt.Sprintf("Subject%d,motion%d.csv,force%d.csv,emg%d.csv,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0\n", i, i, i, i)
	}

	tmpDir := b.TempDir()
	manifestFile := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestFile, []byte(content), 0644)
	require.NoError(b, err)

	analyzer := phase_sync.NewPhaseSyncAnalyzer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := analyzer.LoadManifestSubjects(manifestFile)
		if err != nil {
			b.Fatal(err)
		}
	}
}
