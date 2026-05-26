package phase_sync_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"count_mean/internal/phase_sync"
	"count_mean/internal/security/fsperm"
)

const (
	projectRoot = "/Users/wilson08/IdeaProjects/count_mean"
	dataFolder  = "/Users/wilson08/IdeaProjects/count_mean/input/TEAT_NSF2_20250716"
	outputDir   = "/Users/wilson08/IdeaProjects/count_mean/output"
)

// TestNSF2_T_to_O_Analysis tests the T to O phase analysis.
func TestNSF2_T_to_O_Analysis(t *testing.T) {
	manifestPath := filepath.Join(projectRoot, "input", "test_manifest_nsf2.csv")

	// Skip if test data doesn't exist
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: test data file not found: %s", manifestPath)
	}

	if _, err := os.Stat(dataFolder); os.IsNotExist(err) {
		t.Skipf("Skipping test: test data folder not found: %s", dataFolder)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, fsperm.DirPerm); err != nil {
		t.Fatalf("Failed to create output directory: %v", err)
	}

	// Create analyzer
	analyzer := phase_sync.NewPhaseSyncAnalyzer()

	// Set analysis parameters for T -> O
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   dataFolder,
		StartPhase:   "T",
		EndPhase:     "O",
		SubjectIndex: 0, // NSF2
	}

	// Execute analysis
	stats, err := analyzer.AnalyzePhaseSync(context.Background(), params)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Log results
	t.Logf("Subject: %s", stats.Subject)
	t.Logf("Start Phase: %s, Start Time: %.6f", stats.StartPhase, stats.StartTime)
	t.Logf("End Phase: %s, End Time: %.6f", stats.EndPhase, stats.EndTime)
	t.Logf("Time Duration: %.6f seconds", stats.EndTime-stats.StartTime)
	t.Logf("Channel count: %d", len(stats.ChannelNames))

	// Expected time calculations:
	// EMGMotionOffset = 600
	// T = 13.025 (Force Plate time)
	// O = 3363 (Motion index)
	//
	// For T (Force Plate time):
	// EMG_Time = ForceTime - (EMGMotionOffset - 1) / 250
	// EMG_Time = 13.025 - 599 / 250
	// EMG_Time = 13.025 - 2.396
	// EMG_Time = 10.629
	//
	// For O (Motion index):
	// EMG_Time = (MotionIndex - EMGMotionOffset) / 250
	// EMG_Time = (3363 - 600) / 250
	// EMG_Time = 2763 / 250
	// EMG_Time = 11.052

	expectedStartTime := 13.025 - float64(600-1)/250.0 // 10.629
	expectedEndTime := float64(3363-600) / 250.0       // 11.052
	expectedDuration := expectedEndTime - expectedStartTime

	t.Logf("Expected Start Time (T): %.6f", expectedStartTime)
	t.Logf("Expected End Time (O): %.6f", expectedEndTime)
	t.Logf("Expected Duration: %.6f", expectedDuration)

	// Verify time calculations (allow small tolerance)
	tolerance := 0.001
	if diff := math.Abs(stats.StartTime - expectedStartTime); diff > tolerance {
		t.Errorf("Start time calculation error: expected %.6f, got %.6f (diff: %.6f)",
			expectedStartTime, stats.StartTime, diff)
	}

	if diff := math.Abs(stats.EndTime - expectedEndTime); diff > tolerance {
		t.Errorf("End time calculation error: expected %.6f, got %.6f (diff: %.6f)",
			expectedEndTime, stats.EndTime, diff)
	}

	// Verify duration
	actualDuration := stats.EndTime - stats.StartTime
	if diff := math.Abs(actualDuration - expectedDuration); diff > tolerance {
		t.Errorf("Duration calculation error: expected %.6f, got %.6f",
			expectedDuration, actualDuration)
	}

	// Export results — ADR-0001: write via csvHandler unified path.
	cfg := config.DefaultConfig()
	cfg.OutputDir = outputDir
	cfg.InputDir = outputDir
	cfg.OperateDir = outputDir
	csvHandler := io.NewCSVHandler(cfg)

	outputPath, err := csvHandler.WritePhaseSyncResult(io.WriteRequest{}, stats)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	t.Logf("Output file: %s", outputPath)

	// Verify output file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("Output file does not exist: %s", outputPath)
	}

	// Read and verify output content
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	contentStr := string(content)
	t.Logf("Output content:\n%s", contentStr)

	// Verify CSV structure
	verifyCSVStructure(t, contentStr, "T", "O", expectedStartTime, expectedEndTime)

	// Verify statistics are reasonable
	verifyStatistics(t, stats)
}

// TestNSF2_P0_to_P1_Analysis tests the P0 to P1 phase analysis.
func TestNSF2_P0_to_P1_Analysis(t *testing.T) {
	manifestPath := filepath.Join(projectRoot, "input", "test_manifest_nsf2.csv")

	// Skip if test data doesn't exist
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: test data file not found: %s", manifestPath)
	}

	if _, err := os.Stat(dataFolder); os.IsNotExist(err) {
		t.Skipf("Skipping test: test data folder not found: %s", dataFolder)
	}

	// Ensure output directory exists
	if err := os.MkdirAll(outputDir, fsperm.DirPerm); err != nil {
		t.Fatalf("Failed to create output directory: %v", err)
	}

	// Create analyzer
	analyzer := phase_sync.NewPhaseSyncAnalyzer()

	// Set analysis parameters for P0 -> P1
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   dataFolder,
		StartPhase:   "P0",
		EndPhase:     "P1",
		SubjectIndex: 0, // NSF2
	}

	// Execute analysis
	stats, err := analyzer.AnalyzePhaseSync(context.Background(), params)
	if err != nil {
		t.Fatalf("Analysis failed: %v", err)
	}

	// Log results
	t.Logf("Subject: %s", stats.Subject)
	t.Logf("Start Phase: %s, Start Time: %.6f", stats.StartPhase, stats.StartTime)
	t.Logf("End Phase: %s, End Time: %.6f", stats.EndPhase, stats.EndTime)
	t.Logf("Time Duration: %.6f seconds", stats.EndTime-stats.StartTime)
	t.Logf("Channel count: %d", len(stats.ChannelNames))

	// Expected time calculations:
	// EMGMotionOffset = 600
	// P0 = 10.667 (Force Plate time)
	// P1 = 12.029 (Force Plate time)
	//
	// For P0 (Force Plate time):
	// EMG_Time = ForceTime - (EMGMotionOffset - 1) / 250
	// EMG_Time = 10.667 - 599 / 250
	// EMG_Time = 10.667 - 2.396
	// EMG_Time = 8.271
	//
	// For P1 (Force Plate time):
	// EMG_Time = 12.029 - 599 / 250
	// EMG_Time = 12.029 - 2.396
	// EMG_Time = 9.633

	expectedStartTime := 10.667 - float64(600-1)/250.0 // 8.271
	expectedEndTime := 12.029 - float64(600-1)/250.0   // 9.633
	expectedDuration := expectedEndTime - expectedStartTime

	t.Logf("Expected Start Time (P0): %.6f", expectedStartTime)
	t.Logf("Expected End Time (P1): %.6f", expectedEndTime)
	t.Logf("Expected Duration: %.6f", expectedDuration)

	// Verify time calculations (allow small tolerance)
	tolerance := 0.001
	if diff := math.Abs(stats.StartTime - expectedStartTime); diff > tolerance {
		t.Errorf("Start time calculation error: expected %.6f, got %.6f (diff: %.6f)",
			expectedStartTime, stats.StartTime, diff)
	}

	if diff := math.Abs(stats.EndTime - expectedEndTime); diff > tolerance {
		t.Errorf("End time calculation error: expected %.6f, got %.6f (diff: %.6f)",
			expectedEndTime, stats.EndTime, diff)
	}

	// Verify duration
	actualDuration := stats.EndTime - stats.StartTime
	if diff := math.Abs(actualDuration - expectedDuration); diff > tolerance {
		t.Errorf("Duration calculation error: expected %.6f, got %.6f",
			expectedDuration, actualDuration)
	}

	// Export results — ADR-0001: write via csvHandler unified path.
	cfg := config.DefaultConfig()
	cfg.OutputDir = outputDir
	cfg.InputDir = outputDir
	cfg.OperateDir = outputDir
	csvHandler := io.NewCSVHandler(cfg)

	outputPath, err := csvHandler.WritePhaseSyncResult(io.WriteRequest{}, stats)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	t.Logf("Output file: %s", outputPath)

	// Verify output file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("Output file does not exist: %s", outputPath)
	}

	// Read and verify output content
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	contentStr := string(content)
	t.Logf("Output content:\n%s", contentStr)

	// Verify CSV structure
	verifyCSVStructure(t, contentStr, "P0", "P1", expectedStartTime, expectedEndTime)

	// Verify statistics are reasonable
	verifyStatistics(t, stats)
}

// verifyCSVStructure checks the CSV output structure.
func verifyCSVStructure(t *testing.T, content, startPhase, endPhase string, _, _ float64) {
	t.Helper()

	lines := strings.Split(content, "\n")

	// Check for required rows (using Chinese labels as per the actual output)
	requiredRows := []string{
		"開始分期點", "開始時間", "結束分期點", "結束時間", "時間差值",
		"平均值", "最大值",
	}

	for _, required := range requiredRows {
		found := false

		for _, line := range lines {
			if strings.Contains(line, required) {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Missing required row: %s", required)
		}
	}

	// Check that start and end phase values are present
	for _, line := range lines {
		if strings.Contains(line, "開始分期點") {
			if !strings.Contains(line, startPhase) {
				t.Errorf("Start Phase row should contain '%s', got: %s", startPhase, line)
			}
		}

		if strings.Contains(line, "結束分期點") {
			if !strings.Contains(line, endPhase) {
				t.Errorf("End Phase row should contain '%s', got: %s", endPhase, line)
			}
		}
	}
}

// verifyStatistics checks that statistics values are reasonable.
func verifyStatistics(t *testing.T, stats *models.EMGStatistics) {
	t.Helper()

	if stats == nil {
		t.Fatal("Statistics is nil")
	}

	// Check that we have channel data
	if len(stats.ChannelNames) == 0 {
		t.Error("No channel names in statistics")
	}

	// Check mean values are not NaN or Inf
	for channelName, mean := range stats.ChannelMeans {
		if math.IsNaN(mean) {
			t.Errorf("Mean for channel %s is NaN", channelName)
		}

		if math.IsInf(mean, 0) {
			t.Errorf("Mean for channel %s is Inf", channelName)
		}
	}

	// Check max values are not NaN or Inf
	for channelName, max := range stats.ChannelMaxes {
		if math.IsNaN(max) {
			t.Errorf("Max for channel %s is NaN", channelName)
		}

		if math.IsInf(max, 0) {
			t.Errorf("Max for channel %s is Inf", channelName)
		}
	}

	// Check that max >= mean for each channel (for positive values)
	for channelName, mean := range stats.ChannelMeans {
		if maxVal, ok := stats.ChannelMaxes[channelName]; ok {
			if mean > 0 && maxVal < mean {
				t.Errorf("Channel %s: Max (%.6f) should be >= Mean (%.6f)",
					channelName, maxVal, mean)
			}
		}
	}

	// Log some statistics for verification
	t.Logf("Channel names: %v", stats.ChannelNames)

	for _, channelName := range stats.ChannelNames {
		if mean, ok := stats.ChannelMeans[channelName]; ok {
			t.Logf("Channel %s - Mean: %.10f, Max: %.10f",
				channelName, mean, stats.ChannelMaxes[channelName])
		}
	}
}

// TestTimeCalculationVerification verifies the time synchronization formulas.
func TestTimeCalculationVerification(t *testing.T) {
	emgMotionOffset := 600
	motionFreq := 250.0

	// Test cases
	testCases := []struct {
		name          string
		phase         string
		value         float64
		isMotionIndex bool
		expectedTime  float64
	}{
		{"T (Force Plate)", "T", 13.025, false, 10.629},
		{"O (Motion Index)", "O", 3363, true, 11.052},
		{"P0 (Force Plate)", "P0", 10.667, false, 8.271},
		{"P1 (Force Plate)", "P1", 12.029, false, 9.633},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var calculatedTime float64
			if tc.isMotionIndex {
				calculatedTime = (tc.value - float64(emgMotionOffset)) / motionFreq
			} else {
				calculatedTime = tc.value - float64(emgMotionOffset-1)/motionFreq
			}

			tolerance := 0.001
			if diff := math.Abs(calculatedTime - tc.expectedTime); diff > tolerance {
				t.Errorf("Time calculation for %s: expected %.3f, got %.6f (diff: %.6f)",
					tc.phase, tc.expectedTime, calculatedTime, diff)
			} else {
				t.Logf("Time calculation for %s: %.6f (expected: %.3f)", tc.phase, calculatedTime, tc.expectedTime)
			}
		})
	}
}

// TestAllPhasesEMGTime prints EMG times for all phases.
func TestAllPhasesEMGTime(t *testing.T) {
	emgMotionOffset := 600
	motionFreq := 250.0

	phases := []struct {
		name          string
		value         float64
		isMotionIndex bool
	}{
		{"P0", 10.667, false},
		{"P1", 12.029, false},
		{"P2", 12.237, false},
		{"S", 12.384, false},
		{"C", 12.651, false},
		{"D", 3173, true},
		{"T0", 12.992, false},
		{"T", 13.025, false},
		{"O", 3363, true},
	}

	t.Log("=== Phase Point EMG Times ===")
	t.Logf("%-5s %-15s %-15s %-15s", "Phase", "Original Value", "Type", "EMG Time")
	t.Log("----------------------------------------")

	for _, phase := range phases {
		var emgTime float64

		var typeStr string

		if phase.isMotionIndex {
			emgTime = (phase.value - float64(emgMotionOffset)) / motionFreq
			typeStr = "Motion Index"
		} else {
			emgTime = phase.value - float64(emgMotionOffset-1)/motionFreq
			typeStr = "Force Time"
		}

		t.Logf("%-5s %-15.3f %-15s %-15.6f", phase.name, phase.value, typeStr, emgTime)
	}
}

// TestOutputFileContent verifies the output file contains expected content.
func TestOutputFileContent(t *testing.T) {
	// Skip if test data doesn't exist (this test depends on output from previous tests)
	manifestPath := filepath.Join(projectRoot, "input", "test_manifest_nsf2.csv")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: test data file not found: %s", manifestPath)
	}

	// Check if output files exist from previous tests
	files := []string{
		filepath.Join(outputDir, "NSF2_T-O_statistics.csv"),
		filepath.Join(outputDir, "NSF2_P0-P1_statistics.csv"),
	}

	foundFiles := 0

	for _, file := range files {
		if _, err := os.Stat(file); os.IsNotExist(err) {
			t.Logf("File not found (run phase analysis tests first): %s", file)
			continue
		}

		foundFiles++

		content, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("Failed to read file %s: %v", file, err)
			continue
		}

		contentStr := string(content)
		t.Logf("=== Content of %s ===\n%s", filepath.Base(file), contentStr)

		// Verify structure (using Chinese labels)
		if !strings.Contains(contentStr, "時間差值") {
			t.Errorf("File %s missing 'Duration' row", file)
		}
	}

	if foundFiles == 0 {
		t.Skip("Skipping test: no output files found from previous tests")
	}
}
