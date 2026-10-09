package io

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/csvutil"
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
	handler := NewLargeFileHandler(cfg)

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

func TestLargeFileHandler_ErrorHandling(t *testing.T) {
	// 創建測試配置
	cfg := config.DefaultConfig()
	cfg.InputDir = "./nonexistent_dir"
	handler := NewLargeFileHandler(cfg)

	// 測試不存在的文件
	_, err := handler.GetFileInfo("./nonexistent_file.csv")
	if err == nil {
		t.Errorf("期望獲取不存在文件信息時返回錯誤")
	}
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
	handler := NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "with_bom.csv")
	body := "Time,Ch1,Ch2\n0.1,1.0,2.0\n0.2,1.5,2.5\n"
	content := append(append([]byte{}, csvutil.BOMBytes()...), []byte(body)...)
	require.NoError(t, os.WriteFile(testFile, content, fsperm.FilePerm))

	info, err := handler.GetFileInfo(testFile)
	require.NoError(t, err, "GetFileInfo 失敗")
	require.Equal(t, int64(3), info.LineCount, "BOM 不應改變行數計算")
	require.Equal(t, 3, info.ColumnCount, "BOM 不應讓首欄被算成額外欄位")
}
