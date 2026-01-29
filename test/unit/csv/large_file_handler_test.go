package csv_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/io"
)

func TestLargeFileHandler_GetFileInfo(t *testing.T) {
	// 創建測試配置
	cfg := config.DefaultConfig()

	// 創建測試文件
	testDir := "./test_temp"
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("無法創建測試目錄: %v", err)
	}
	defer os.RemoveAll(testDir)

	cfg.InputDir = testDir
	handler := io.NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "test.csv")
	testData := []string{
		"Time,Ch1,Ch2",
		"0.1,100.5,50.2",
		"0.2,120.3,55.1",
		"0.3,110.8,52.3",
	}

	content := strings.Join(testData, "\n")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("無法創建測試文件: %v", err)
	}

	// 測試獲取文件信息
	info, err := handler.GetFileInfo(testFile)
	if err != nil {
		t.Errorf("GetFileInfo 失敗: %v", err)
		return
	}

	if info.LineCount != 4 {
		t.Errorf("期望行數 4，實際 %d", info.LineCount)
	}

	if info.ColumnCount != 3 {
		t.Errorf("期望列數 3，實際 %d", info.ColumnCount)
	}

	if info.IsLarge {
		t.Errorf("小文件不應該被標記為大文件")
	}
}

func TestLargeFileHandler_ReadCSVStreaming(t *testing.T) {
	// 創建測試配置
	cfg := config.DefaultConfig()

	testDir := "./test_temp"
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("無法創建測試目錄: %v", err)
	}

	defer os.RemoveAll(testDir)

	cfg.InputDir = testDir
	handler := io.NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "test_streaming.csv")
	testData := []string{
		"Time,Ch1,Ch2",
		"0.1,100.5,50.2",
		"0.2,120.3,55.1",
		"0.3,110.8,52.3",
		"0.4,130.2,58.7",
		"0.5,125.6,56.9",
	}

	content := strings.Join(testData, "\n")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("無法創建測試文件: %v", err)
	}

	// 測試流式讀取
	progressCalled := false
	callback := func(processed, total int64, percentage float64) {
		progressCalled = true

		if processed < 0 || total < 0 || percentage < 0 || percentage > 100 {
			t.Errorf("進度回調參數無效: processed=%d, total=%d, percentage=%.2f",
				processed, total, percentage)
		}
	}

	result, err := handler.ReadCSVStreaming(testFile, callback)
	if err != nil {
		t.Errorf("ReadCSVStreaming 失敗: %v", err)
		return
	}

	if result.ProcessedLines != 6 {
		t.Errorf("期望處理 6 行，實際 %d", result.ProcessedLines)
	}

	if len(result.Headers) != 3 {
		t.Errorf("期望 3 個標題，實際 %d", len(result.Headers))
	}

	if !progressCalled {
		t.Errorf("進度回調未被調用")
	}
}

func TestLargeFileHandler_ProcessLargeFileInChunks(t *testing.T) {
	// 創建測試配置
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1

	testDir := "./test_temp"
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("無法創建測試目錄: %v", err)
	}

	defer os.RemoveAll(testDir)

	cfg.InputDir = testDir
	handler := io.NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "test_chunks.csv")
	testData := []string{
		"Time,Ch1,Ch2",
		"0.1,100,50",
		"0.2,120,55",
		"0.3,110,52",
		"0.4,130,58",
		"0.5,125,56",
		"0.6,115,54",
		"0.7,135,60",
	}

	content := strings.Join(testData, "\n")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("無法創建測試文件: %v", err)
	}

	// 測試分塊處理
	windowSize := 3
	progressCalled := false
	callback := func(_, total int64, percentage float64) {
		progressCalled = true
		_ = total
		_ = percentage
	}

	result, err := handler.ProcessLargeFileInChunks(testFile, windowSize, callback)
	if err != nil {
		t.Errorf("ProcessLargeFileInChunks 失敗: %v", err)
		return
	}

	if result.ProcessedLines != 8 {
		t.Errorf("期望處理 8 行，實際 %d", result.ProcessedLines)
	}

	if len(result.Results) != 2 {
		t.Errorf("期望 2 個結果（2個通道），實際 %d", len(result.Results))
	}

	if !progressCalled {
		t.Errorf("進度回調未被調用")
	}

	// 驗證結果有效性
	for i, result := range result.Results {
		if result.MaxMean <= 0 {
			t.Errorf("結果 %d 最大平均值應該大於 0，實際 %f", i, result.MaxMean)
		}

		if result.StartTime < 0 || result.EndTime < 0 {
			t.Errorf("結果 %d 時間範圍無效: 開始=%f, 結束=%f", i, result.StartTime, result.EndTime)
		}

		if result.StartTime >= result.EndTime {
			t.Errorf("結果 %d 開始時間應該小於結束時間", i)
		}
	}
}

func TestLargeFileHandler_ProcessLargeFileInChunks_NegativeValues(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1
	testDir := t.TempDir()
	cfg.InputDir = testDir

	handler := io.NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "negative.csv")
	testData := []string{
		"Time,Ch1,Ch2",
		"0.1,-10,-5",
		"0.2,-8,-6",
		"0.3,-9,-7",
	}

	if err := os.WriteFile(testFile, []byte(strings.Join(testData, "\n")), 0o644); err != nil {
		t.Fatalf("無法創建測試文件: %v", err)
	}

	result, err := handler.ProcessLargeFileInChunks(testFile, 2, nil)
	if err != nil {
		t.Fatalf("ProcessLargeFileInChunks 失敗: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("期望 2 個通道結果，實際 %d", len(result.Results))
	}

	// 確認全負值時不會回傳 0（預設值）
	if result.Results[0].MaxMean >= 0 || result.Results[1].MaxMean >= 0 {
		t.Fatalf("全負值資料應產生負的最大平均值，得到 %+v", result.Results)
	}

	// 確認最大平均值取自正確窗口（縮放因子 1 -> 數值放大 10 倍）
	expectedCh1 := -85.0 // (-8 + -9)/2 * 10
	if math.Abs(result.Results[0].MaxMean-expectedCh1) > 1e-9 {
		t.Fatalf("Ch1 最大平均值錯誤，期望 %f，實際 %f", expectedCh1, result.Results[0].MaxMean)
	}
}

func TestLargeFileHandler_WriteCSVStreaming(t *testing.T) {
	// 創建測試配置
	cfg := config.DefaultConfig()

	testDir := "./test_temp"
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("無法創建測試目錄: %v", err)
	}

	defer os.RemoveAll(testDir)

	cfg.OutputDir = testDir
	handler := io.NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "test_output.csv")
	testData := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"0.1", "100.5", "50.2"},
		{"0.2", "120.3", "55.1"},
		{"0.3", "110.8", "52.3"},
	}

	// 測試流式寫入
	progressCalled := false
	callback := func(_, total int64, percentage float64) {
		progressCalled = true
		_ = total
		_ = percentage
	}

	err := handler.WriteCSVStreaming(testFile, testData, callback)
	if err != nil {
		t.Errorf("WriteCSVStreaming 失敗: %v", err)
		return
	}

	// 驗證文件是否創建
	if _, err := os.Stat(testFile); os.IsNotExist(err) {
		t.Errorf("輸出文件未創建")
		return
	}

	// 讀取並驗證內容
	content, err := os.ReadFile(testFile)
	if err != nil {
		t.Errorf("無法讀取輸出文件: %v", err)
		return
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) < 4 {
		t.Errorf("輸出文件行數不足，期望至少 4 行，實際 %d", len(lines))
	}

	if !progressCalled {
		t.Errorf("進度回調未被調用")
	}
}

func TestLargeFileHandler_MemoryManagement(t *testing.T) {
	// 創建測試配置
	cfg := config.DefaultConfig()
	handler := io.NewLargeFileHandler(cfg)

	// 測試通過公共接口，檢查大文件處理器是否正常工作
	// 無法直接測試私有方法，但可以確保處理器正常初始化
	require.NotNil(t, handler)

	// 這個測試主要驗證LargeFileHandler的初始化是正確的
	t.Log("大文件處理器初始化測試通過")
}

func TestLargeFileHandler_ErrorHandling(t *testing.T) {
	// 創建測試配置
	cfg := config.DefaultConfig()
	cfg.InputDir = "./nonexistent_dir"
	handler := io.NewLargeFileHandler(cfg)

	// 測試不存在的文件
	_, err := handler.GetFileInfo("./nonexistent_file.csv")
	if err == nil {
		t.Errorf("期望獲取不存在文件信息時返回錯誤")
	}

	// 測試流式讀取不存在的文件
	_, err = handler.ReadCSVStreaming("./nonexistent_file.csv", nil)
	if err == nil {
		t.Errorf("期望流式讀取不存在文件時返回錯誤")
	}

	// 測試處理不存在的文件
	_, err = handler.ProcessLargeFileInChunks("./nonexistent_file.csv", 10, nil)
	if err == nil {
		t.Errorf("期望處理不存在文件時返回錯誤")
	}
}

func TestLargeFileHandler_NegativeSignals(t *testing.T) {
	// Test the fix for negative signal handling
	// This verifies that channelMaxMeans is initialized correctly with negative infinity
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1

	testDir := "./test_temp"
	if err := os.MkdirAll(testDir, 0o755); err != nil {
		t.Fatalf("無法創建測試目錄: %v", err)
	}

	defer os.RemoveAll(testDir)

	cfg.InputDir = testDir
	handler := io.NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "test_negative.csv")
	// All values are negative to test the fix
	testData := []string{
		"Time,Ch1,Ch2",
		"0.1,-100,-50",
		"0.2,-120,-55",
		"0.3,-110,-52",
		"0.4,-90,-48",
		"0.5,-95,-49",
		"0.6,-105,-51",
	}

	content := strings.Join(testData, "\n")
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatalf("無法創建測試文件: %v", err)
	}

	// Process with a window size of 3
	windowSize := 3

	result, err := handler.ProcessLargeFileInChunks(testFile, windowSize, nil)
	if err != nil {
		t.Fatalf("ProcessLargeFileInChunks 失敗: %v", err)
	}

	if len(result.Results) != 2 {
		t.Fatalf("期望 2 個結果（2個通道），實際 %d", len(result.Results))
	}

	// Verify that all results have negative MaxMean values (not zero)
	for i, res := range result.Results {
		if res.MaxMean >= 0 {
			t.Errorf("通道 %d: 對於負信號，MaxMean 應該為負值，實際為 %f", i, res.MaxMean)
		}

		if res.StartTime <= 0 || res.EndTime <= 0 {
			t.Errorf("通道 %d: 時間範圍無效: 開始=%f, 結束=%f", i, res.StartTime, res.EndTime)
		}

		if res.StartTime >= res.EndTime {
			t.Errorf("通道 %d: 開始時間應該小於結束時間", i)
		}
	}

	// For Ch1, the window with highest mean should be around -90, -95, -105 (mean = -96.67)
	// or -95, -105, -100 depending on data order
	// The key is that it should be negative, not zero
	t.Logf("通道 0 (Ch1): MaxMean=%f, 時間範圍=[%f, %f]",
		result.Results[0].MaxMean, result.Results[0].StartTime, result.Results[0].EndTime)
	t.Logf("通道 1 (Ch2): MaxMean=%f, 時間範圍=[%f, %f]",
		result.Results[1].MaxMean, result.Results[1].StartTime, result.Results[1].EndTime)
}
