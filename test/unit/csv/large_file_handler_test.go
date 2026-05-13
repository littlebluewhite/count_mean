package csv_test

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/csvutil"
	"count_mean/internal/io"
	"count_mean/internal/security/fsperm"
)

func TestLargeFileHandler_GetFileInfo(t *testing.T) {
	// 創建測試配置
	cfg := config.DefaultConfig()

	// 創建測試文件
	testDir := "./test_temp"
	if err := os.MkdirAll(testDir, fsperm.DirPerm); err != nil {
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
	if err := os.WriteFile(testFile, []byte(content), fsperm.FilePerm); err != nil {
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

func TestLargeFileHandler_ProcessLargeFileInChunks(t *testing.T) {
	// 創建測試配置
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1

	testDir := "./test_temp"
	if err := os.MkdirAll(testDir, fsperm.DirPerm); err != nil {
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
	if err := os.WriteFile(testFile, []byte(content), fsperm.FilePerm); err != nil {
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

	if err := os.WriteFile(testFile, []byte(strings.Join(testData, "\n")), fsperm.FilePerm); err != nil {
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
	if err := os.MkdirAll(testDir, fsperm.DirPerm); err != nil {
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
	if err := os.WriteFile(testFile, []byte(content), fsperm.FilePerm); err != nil {
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

// TestLargeFileHandler_BufferPool_NoLeak 防止 pool 洩漏 regression：
// parseDataRow 從 BufferPool 借出 []float64 給 EMGData.Channels，
// 但 manageDataBuffer drop 舊 row 與 defer PutEMGDataSlice 都沒同步 PutFloat64Slice，
// 造成 streaming 期間 Float64Gets 持續 >> Float64Puts。
// 修正後：每個淘汰的 Channels 都會 PutFloat64Slice 歸還。
//
// Assertion：跑完整個 ProcessLargeFileInChunks 後，Float64Puts 應該 ~= Float64Gets
// （允許少量誤差來自結束時保留尾段，但 Puts 應該明顯 > 0）。
func TestLargeFileHandler_BufferPool_NoLeak(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1

	testDir := t.TempDir()
	cfg.InputDir = testDir

	handler := io.NewLargeFileHandler(cfg)
	testFile := filepath.Join(testDir, "test_pool_leak.csv")

	// 建 60 行資料以確保 manageDataBuffer 觸發數次（windowSize=10 → bufferLimit=30）
	lines := []string{"Time,Ch1,Ch2,Ch3"}
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("%d,%d,%d,%d", i, i*10, i*20, i*30))
	}

	if err := os.WriteFile(testFile, []byte(strings.Join(lines, "\n")), fsperm.FilePerm); err != nil {
		t.Fatalf("無法建立測試檔: %v", err)
	}

	_, err := handler.ProcessLargeFileInChunks(testFile, 10, nil)
	if err != nil {
		t.Fatalf("ProcessLargeFileInChunks 失敗: %v", err)
	}

	stats := handler.GetBufferPoolStats()

	if stats.Float64Gets == 0 {
		t.Fatalf("沒有 Get 任何 Float64 slice — test 沒實際走 channel-slice 流程")
	}

	// 修正後 Puts 應該 ~= Gets。給少量緩衝：定義 leak ratio < 5%。
	leaked := stats.Float64Gets - stats.Float64Puts
	if leaked < 0 {
		leaked = 0 // 不可能
	}

	leakRatio := float64(leaked) / float64(stats.Float64Gets)
	if leakRatio > 0.05 {
		t.Errorf("BufferPool channel-slice leak: Gets=%d, Puts=%d, leaked=%d (%.1f%% > 5%%)",
			stats.Float64Gets, stats.Float64Puts, leaked, leakRatio*100)
	}

	t.Logf("BufferPool stats: Gets=%d, Puts=%d, leak=%d (%.1f%%)",
		stats.Float64Gets, stats.Float64Puts, leaked, leakRatio*100)
}

// TestLargeFileHandler_ScanFileStructure_StripsBOM 釘住 Wave 6 PR1 BOM 對稱補完:
// Wave 4 PR-E (c3b94ef) 把 parsers/csv_reader.go 與 csv_handler.go 改用 PeekBOM,
// 但 large_file_handler.go:180 (scanFileStructure) 沒同步修。Excel 匯出的 UTF-8
// CSV 帶 0xEF 0xBB 0xBF 前綴,若不剝除 firstRow[0] 會帶 U+FEFF — line count / column
// count 數值還會對(因為只算長度),但 headers[0] 進入 GetFileInfo 之後的下游路徑
// (例如 streaming pipeline 取 headers) 就會看到怪字元。
//
// 此 test 透過寫入含 BOM 的 CSV 後呼叫 GetFileInfo(內部會 scanFileStructure),
// 確認 LineCount 與 ColumnCount 計算正確(BOM 不被當成額外欄位/列)。
func TestLargeFileHandler_ScanFileStructure_StripsBOM(t *testing.T) {
	cfg := config.DefaultConfig()
	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := io.NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "with_bom.csv")
	body := "Time,Ch1,Ch2\n0.1,1.0,2.0\n0.2,1.5,2.5\n"
	content := append(append([]byte{}, csvutil.BOMBytes()...), []byte(body)...)
	require.NoError(t, os.WriteFile(testFile, content, fsperm.FilePerm))

	info, err := handler.GetFileInfo(testFile)
	require.NoError(t, err, "GetFileInfo 失敗")
	require.Equal(t, int64(3), info.LineCount, "BOM 不應改變行數計算")
	require.Equal(t, 3, info.ColumnCount, "BOM 不應讓首欄被算成額外欄位")
}

// TestLargeFileHandler_ProcessLargeFileInChunks_StripsBOM 對稱守護
// processStreamingFile 的 BOM 處理 — Headers[0] 不能帶 U+FEFF。
//
// 完整 streaming 路徑:GetFileInfo → processStreamingFile → reader.Read() 取
// headers。修法前 headers[0] 開頭含 U+FEFF (BOM),修法後應為純 "Time"。
func TestLargeFileHandler_ProcessLargeFileInChunks_StripsBOM(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1
	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := io.NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "stream_bom.csv")
	var b bytes.Buffer
	b.Write(csvutil.BOMBytes())
	b.WriteString("Time,RA,LA\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&b, "%d,%d,%d\n", i, i*10, i*20)
	}
	require.NoError(t, os.WriteFile(testFile, b.Bytes(), fsperm.FilePerm))

	result, err := handler.ProcessLargeFileInChunks(testFile, 3, nil)
	require.NoError(t, err, "ProcessLargeFileInChunks 失敗")
	require.Len(t, result.Headers, 3, "標題行欄位數")
	require.Equal(t, "Time", result.Headers[0], "headers[0] 不能帶 U+FEFF — BOM 必須被 PeekBOM 剝除")
	require.Equal(t, "RA", result.Headers[1])
	require.Equal(t, "LA", result.Headers[2])
}
