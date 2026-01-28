package phase_sync_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/models"
	"count_mean/internal/phase_sync"
)

func TestPhaseSyncAnalysis_NSF2(t *testing.T) {
	// 定義測試路徑
	projectRoot := "/Users/wilson08/IdeaProjects/count_mean"
	manifestPath := filepath.Join(projectRoot, "input", "test_manifest.csv")
	dataFolder := filepath.Join(projectRoot, "input", "TEAT_NSF2_20250716")
	outputDir := filepath.Join(projectRoot, "output")

	// Skip if test data doesn't exist
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Skipf("Skipping test: test data file not found: %s", manifestPath)
	}
	if _, err := os.Stat(dataFolder); os.IsNotExist(err) {
		t.Skipf("Skipping test: test data folder not found: %s", dataFolder)
	}

	// 確保輸出目錄存在
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("無法創建輸出目錄: %v", err)
	}

	// 創建分析器
	analyzer := phase_sync.NewPhaseSyncAnalyzer()

	// 設置分析參數
	params := &models.AnalysisParams{
		ManifestFile: manifestPath,
		DataFolder:   dataFolder,
		StartPhase:   "T",
		EndPhase:     "O",
		SubjectIndex: 0, // 第一個主題（NSF2）
	}

	// 執行分析
	stats, err := analyzer.AnalyzePhaseSync(params)
	if err != nil {
		t.Fatalf("分析失敗: %v", err)
	}

	// 輸出結果
	t.Logf("主題: %s", stats.Subject)
	t.Logf("開始分期點: %s, 開始時間: %.6f", stats.StartPhase, stats.StartTime)
	t.Logf("結束分期點: %s, 結束時間: %.6f", stats.EndPhase, stats.EndTime)
	t.Logf("時間差值: %.6f 秒", stats.EndTime-stats.StartTime)
	t.Logf("通道數: %d", len(stats.ChannelNames))

	// 驗證時間同步計算
	// EMGMotionOffset = 600
	// T = 13.025 (Force Plate 秒數)
	// O = 3363 (Motion index)
	//
	// 對於 T (Force Plate 時間):
	// EMG時間 = ForceTime - (EMGMotionOffset - 1) / 250
	// EMG時間 = 13.025 - (600 - 1) / 250
	// EMG時間 = 13.025 - 599 / 250
	// EMG時間 = 13.025 - 2.396
	// EMG時間 = 10.629
	//
	// 對於 O (Motion index):
	// EMG時間 = (MotionIndex - EMGMotionOffset) / 250
	// EMG時間 = (3363 - 600) / 250
	// EMG時間 = 2763 / 250
	// EMG時間 = 11.052

	expectedStartTime := 13.025 - float64(600-1)/250.0
	expectedEndTime := float64(3363-600) / 250.0

	t.Logf("預期開始時間 (T): %.6f", expectedStartTime)
	t.Logf("預期結束時間 (O): %.6f", expectedEndTime)

	// 驗證計算結果（允許小誤差）
	tolerance := 0.001
	if diff := stats.StartTime - expectedStartTime; diff > tolerance || diff < -tolerance {
		t.Errorf("開始時間計算錯誤: 期望 %.6f, 實際 %.6f", expectedStartTime, stats.StartTime)
	}
	if diff := stats.EndTime - expectedEndTime; diff > tolerance || diff < -tolerance {
		t.Errorf("結束時間計算錯誤: 期望 %.6f, 實際 %.6f", expectedEndTime, stats.EndTime)
	}

	// 導出結果
	outputPath, err := analyzer.ExportResults(stats, outputDir)
	if err != nil {
		t.Fatalf("導出失敗: %v", err)
	}

	t.Logf("輸出檔案: %s", outputPath)

	// 驗證輸出檔案存在
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Errorf("輸出檔案不存在: %s", outputPath)
	}

	// 讀取並顯示輸出內容
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Logf("無法讀取輸出檔案: %v", err)
	} else {
		t.Logf("輸出內容:\n%s", string(content))
	}
}

func TestTimeSynchronization_Verification(t *testing.T) {
	// 測試時間同步計算的正確性
	// 使用 NSF2 的數據進行驗證

	emgMotionOffset := 600
	motionFreq := 250.0

	// 測試 Force Plate 時間轉 EMG 時間
	// T = 13.025 秒 (Force Plate)
	forceTime := 13.025
	expectedEMGTime := forceTime - float64(emgMotionOffset-1)/motionFreq

	t.Logf("Force Plate 時間: %.6f", forceTime)
	t.Logf("EMGMotionOffset: %d", emgMotionOffset)
	t.Logf("Motion 頻率: %.0f Hz", motionFreq)
	t.Logf("計算的 EMG 時間: %.6f", expectedEMGTime)

	// 測試 Motion index 轉 EMG 時間
	// O = 3363 (Motion index)
	motionIndex := 3363
	expectedEMGTimeFromMotion := float64(motionIndex-emgMotionOffset) / motionFreq

	t.Logf("Motion index: %d", motionIndex)
	t.Logf("計算的 EMG 時間 (from Motion): %.6f", expectedEMGTimeFromMotion)

	// 驗證時間順序
	if expectedEMGTime >= expectedEMGTimeFromMotion {
		t.Errorf("時間順序錯誤: T (%.6f) 應該小於 O (%.6f)",
			expectedEMGTime, expectedEMGTimeFromMotion)
	}

	t.Logf("時間差 (O - T): %.6f 秒", expectedEMGTimeFromMotion-expectedEMGTime)
}

func TestAllPhases_P0_to_L(t *testing.T) {
	// 測試所有分期點的時間計算

	emgMotionOffset := 600
	motionFreq := 250.0

	// NSF2 的分期點數據
	phases := map[string]struct {
		value         float64
		isMotionIndex bool
	}{
		"P0": {10.667, false},
		"P1": {12.029, false},
		"P2": {12.237, false},
		"S":  {12.384, false},
		"C":  {12.651, false},
		"D":  {3173, true},
		"T0": {12.992, false},
		"T":  {13.025, false},
		"O":  {3363, true},
	}

	fmt.Println("=== 各分期點的 EMG 時間 ===")
	fmt.Printf("%-5s %-15s %-15s %-15s\n", "分期", "原始值", "類型", "EMG時間")
	fmt.Println("----------------------------------------")

	for _, phaseName := range []string{"P0", "P1", "P2", "S", "C", "D", "T0", "T", "O"} {
		phase := phases[phaseName]
		var emgTime float64
		var typeStr string

		if phase.isMotionIndex {
			emgTime = (phase.value - float64(emgMotionOffset)) / motionFreq
			typeStr = "Motion Index"
		} else {
			emgTime = phase.value - float64(emgMotionOffset-1)/motionFreq
			typeStr = "Force Time"
		}

		fmt.Printf("%-5s %-15.3f %-15s %-15.6f\n", phaseName, phase.value, typeStr, emgTime)
	}
}
