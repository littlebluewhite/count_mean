package phase_sync //nolint:revive // underscore in package name matches directory structure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// createTestMotionFileN creates a test motion file with specified number of rows.
func createTestMotionFileN(t *testing.T, dir string, numRows int) {
	content := `Line 1: Metadata
Line 2: More metadata
Line 3: Additional info
Index,X,Y,Z
`
	for i := 1; i <= numRows; i++ {
		content += fmt.Sprintf("%d,10.5,20.3,30.8\n", i)
	}

	filePath := filepath.Join(dir, "motion.csv")
	err := os.WriteFile(filePath, []byte(content), fsperm.FilePerm)
	require.NoError(t, err)
}

// createTestForceFileN creates a test force plate .anc file with specified duration.
func createTestForceFileN(t *testing.T, dir string, durationSec float64) {
	numRows := int(durationSec * 1000) // 1000Hz sampling rate
	content := fmt.Sprintf(`File_Type:	Analog R/C ASCII	Generation#:	2
Board_Type:	USB-6225	Polarity:	Bipolar
Trial_Name:	Test_Trial	Trial#:	1	Duration(Sec.):	%.6f	#Channels:	2
BitDepth:	16	PreciseRate:	1000.000000




Name	F1X	F1Y
Rate	1000	1000
Range	10000	10000
`, durationSec)

	for i := 0; i < numRows; i++ {
		time := float64(i) / 1000.0
		content += fmt.Sprintf("%.6f\t100\t200\n", time)
	}

	filePath := filepath.Join(dir, "force.anc")
	err := os.WriteFile(filePath, []byte(content), fsperm.FilePerm)
	require.NoError(t, err)
}

// createTestEMGFileN creates a test EMG file with specified time range.
func createTestEMGFileN(t *testing.T, dir string, startTime, endTime float64) {
	content := "Time,Ch1,Ch2\n"

	numRows := int((endTime - startTime) * 1000) // 1000Hz sampling
	for i := 0; i <= numRows; i++ {
		time := startTime + float64(i)/1000.0
		content += fmt.Sprintf("%.6f,100.5,200.3\n", time)
	}

	filePath := filepath.Join(dir, "emg.csv")
	err := os.WriteFile(filePath, []byte(content), fsperm.FilePerm)
	require.NoError(t, err)
}

// TestMotionIndexValidation tests that D and O phase points are validated against motion data range.
func TestMotionIndexValidation_D_OutOfRange(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a motion file with only 100 rows (max index = 100)
	createTestMotionFileN(t, tmpDir, 100)

	// Create force file with 1 second of data
	createTestForceFileN(t, tmpDir, 1.0)

	// Create EMG file
	createTestEMGFileN(t, tmpDir, 0, 1.0)

	// Create manifest with D = 150 (out of range, max is 100). Keep D < O so the
	// P1-A4-3 (D/O) monotonicity check in ValidatePhaseManifest passes and we
	// reach the per-D-range check inside validateMotionPhasePoints. O = 180 is
	// also out of range but D is checked first, so the assertion on "D 分期點"
	// stays meaningful.
	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,150,0.6,0.7,180,0.8`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0,
	}

	_, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, analyzeErr)
	assert.Contains(t, analyzeErr.Error(), "D 分期點 index 150 超出 Motion 數據範圍")
}

// TestMotionIndexValidation_O_OutOfRange tests O phase point validation.
func TestMotionIndexValidation_O_OutOfRange(t *testing.T) {
	tmpDir := t.TempDir()

	// Create motion file with max index 100
	createTestMotionFileN(t, tmpDir, 100)

	// Create force file
	createTestForceFileN(t, tmpDir, 1.0)

	// Create EMG file
	createTestEMGFileN(t, tmpDir, 0, 1.0)

	// Create manifest with O = 200 (out of range, max is 100), D = 50 (valid)
	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,50,0.6,0.7,200,0.8`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0,
	}

	_, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, analyzeErr)
	assert.Contains(t, analyzeErr.Error(), "O 分期點 index 200 超出 Motion 數據範圍")
}

// TestEMGMotionOffsetValidation tests EMGMotionOffset validation.
func TestEMGMotionOffsetValidation_OutOfRange(t *testing.T) {
	tmpDir := t.TempDir()

	// Create motion file with max index 100
	createTestMotionFileN(t, tmpDir, 100)

	// Create force file
	createTestForceFileN(t, tmpDir, 1.0)

	// Create EMG file
	createTestEMGFileN(t, tmpDir, 0, 1.0)

	// Create manifest with EMGMotionOffset = 500 (out of range, max motion index is 100)
	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,500,0.1,0.2,0.3,0.4,0.5,50,0.6,0.7,80,0.8`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0,
	}

	_, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, analyzeErr)
	assert.Contains(t, analyzeErr.Error(), "EMGMotionOffset 500 超出 Motion 數據範圍")
}

// TestForceTimeValidation tests force plate time point validation.
func TestForceTimeValidation_T_OutOfRange(t *testing.T) {
	tmpDir := t.TempDir()

	// Create motion file with 1000 rows
	createTestMotionFileN(t, tmpDir, 1000)

	// Create force file with only 1 second of data (max time = 0.999)
	createTestForceFileN(t, tmpDir, 1.0)

	// Create EMG file
	createTestEMGFileN(t, tmpDir, 0, 2.0)

	// Create manifest with T = 2.0 (out of range, max force time is ~0.999).
	// L 改為 2.1（> T）以確保 monotonicity 通過而 force-range 驗證才 trigger。
	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,50,0.6,2.0,80,2.1`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0,
	}

	_, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, analyzeErr)
	assert.Contains(t, analyzeErr.Error(), "T 分期點時間 2.000 超出 Force Plate 數據範圍")
}

// TestForceTimeValidation_L_OutOfRange tests L phase point validation.
func TestForceTimeValidation_L_OutOfRange(t *testing.T) {
	tmpDir := t.TempDir()

	// Create motion file
	createTestMotionFileN(t, tmpDir, 1000)

	// Create force file with only 1 second of data
	createTestForceFileN(t, tmpDir, 1.0)

	// Create EMG file
	createTestEMGFileN(t, tmpDir, 0, 2.0)

	// Create manifest with L = 5.0 (out of range)
	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,50,0.6,0.7,80,5.0`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0,
	}

	_, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, analyzeErr)
	assert.Contains(t, analyzeErr.Error(), "L 分期點時間 5.000 超出 Force Plate 數據範圍")
}

// TestValidContentValidation tests that valid data passes all validations.
func TestValidContentValidation_Success(t *testing.T) {
	tmpDir := t.TempDir()

	// Create motion file with 1000 rows (indices 1-1000)
	createTestMotionFileN(t, tmpDir, 1000)

	// Create force file with 2 seconds of data
	createTestForceFileN(t, tmpDir, 2.0)

	// Create EMG file with 4 seconds of data (starting from 0)
	createTestEMGFileN(t, tmpDir, 0, 4.0)

	// Create manifest with valid values:
	// D = 500 (within motion range 1-1000)
	// O = 600 (within motion range 1-1000)
	// EMGMotionOffset = 1 (EMG time 0 corresponds to Motion index 1)
	// With EMGMotionOffset = 1, Force time 0.5 -> EMG time = 0.5 - (1-1)/250 = 0.5
	// All force times within 0-1.999 range
	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,500,0.6,0.7,600,0.8`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0,
	}

	// This should pass all content validations
	stats, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.NoError(t, analyzeErr)
	assert.NotNil(t, stats)

	if stats != nil {
		assert.Equal(t, "TestSubject", stats.Subject)
	}
}

// TestEMGTimeRangeValidation_StartTimeTooSmall tests EMG time range validation.
func TestEMGTimeRangeValidation_StartTimeTooSmall(t *testing.T) {
	tmpDir := t.TempDir()

	// Create motion file with 1000 rows
	createTestMotionFileN(t, tmpDir, 1000)

	// Create force file with 2 seconds of data
	createTestForceFileN(t, tmpDir, 2.0)

	// Create EMG file starting from 1.0 second (not 0)
	createTestEMGFileN(t, tmpDir, 1.0, 3.0)

	// Create manifest where EMGMotionOffset = 1 means EMG time 0 corresponds to Motion index 1
	// P0 = 0.1 second (force time), which with EMGMotionOffset = 1 gives EMG time = 0.1 - (1-1)/250 = 0.1
	// But EMG data starts at 1.0, so 0.1 is before EMG data start
	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,500,0.6,0.7,600,0.8`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0,
	}

	_, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, analyzeErr)
	assert.Contains(t, analyzeErr.Error(), "EMG 開始時間")
}

// TestEMGTimeRangeValidation_EndTimeTooLarge tests EMG time range validation when end time exceeds EMG data.
func TestEMGTimeRangeValidation_EndTimeTooLarge(t *testing.T) {
	tmpDir := t.TempDir()

	// Create motion file with 10000 rows (so D/O index 5000 is valid)
	createTestMotionFileN(t, tmpDir, 10000)

	// Create force file with 20 seconds of data
	createTestForceFileN(t, tmpDir, 20.0)

	// Create EMG file with only 1 second of data
	createTestEMGFileN(t, tmpDir, 0, 1.0)

	// Create manifest where D phase uses motion index
	// D = 5000 motion index, with 250Hz that's about 20 seconds
	// With EMGMotionOffset = 1, EMG time = (5000 - 1) / 250 = 19.996 seconds
	// But EMG data only goes to 1.0 second
	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,5000,0.6,0.7,6000,0.8`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err := os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "D", // End at D which is motion index 5000
		SubjectIndex: 0,
	}

	_, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, analyzeErr)
	assert.Contains(t, analyzeErr.Error(), "EMG 結束時間")
}

// TestMotionValidation_EmptyMotionFile tests validation when motion file is specified but empty/invalid.
func TestMotionValidation_EmptyMotionFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create empty motion file
	motionPath := filepath.Join(tmpDir, "motion.csv")
	err := os.WriteFile(motionPath, []byte(""), fsperm.FilePerm)
	require.NoError(t, err)

	// Create force file
	createTestForceFileN(t, tmpDir, 1.0)

	// Create EMG file
	createTestEMGFileN(t, tmpDir, 0, 1.0)

	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,50,0.6,0.7,80,0.8`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err = os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0,
	}

	_, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, analyzeErr)
	assert.Contains(t, analyzeErr.Error(), "Motion")
}

// TestForceValidation_EmptyForceFile tests validation when force file is specified but empty/invalid.
func TestForceValidation_EmptyForceFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create motion file
	createTestMotionFileN(t, tmpDir, 100)

	// Create empty force file
	forcePath := filepath.Join(tmpDir, "force.anc")
	err := os.WriteFile(forcePath, []byte(""), fsperm.FilePerm)
	require.NoError(t, err)

	// Create EMG file
	createTestEMGFileN(t, tmpDir, 0, 1.0)

	manifestContent := `Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,50,0.6,0.7,80,0.8`
	manifestPath := filepath.Join(tmpDir, "manifest.csv")
	err = os.WriteFile(manifestPath, []byte(manifestContent), fsperm.FilePerm)
	require.NoError(t, err)

	analyzer := NewPhaseSyncAnalyzer()
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   tmpDir,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0,
	}

	_, analyzeErr := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, analyzeErr)
	assert.Contains(t, analyzeErr.Error(), "Force Plate")
}
