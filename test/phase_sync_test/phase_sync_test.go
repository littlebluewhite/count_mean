package phase_sync_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"count_mean/internal/phase_sync"
	"count_mean/internal/security/fsperm"
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
	if err := os.MkdirAll(outputDir, fsperm.DirPerm); err != nil {
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
	stats, err := analyzer.AnalyzePhaseSync(context.Background(), params)
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

	// 導出結果 — ADR-0001: 走 csvHandler 統一寫檔路徑。
	cfg := config.DefaultConfig()
	cfg.OutputDir = outputDir
	cfg.InputDir = outputDir
	cfg.OperateDir = outputDir
	csvHandler := io.NewCSVHandler(cfg)

	outputPath, err := csvHandler.WritePhaseSyncResult(io.WriteRequest{}, stats)
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
