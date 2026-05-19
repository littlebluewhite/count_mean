package io

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/security/fsperm"

	"os"
)

// TestIsLargeFileThreshold_IsConservative 釘住 修法:isLargeFileThreshold
// 必須 ≤ 100MB,絕不能再回退到原本的 200MB(maxFileSize/10)。
// 100MB 是 ReadAll path 對 GUI process 的 OOM 邊界(memory peak 4-10x source bytes)。
func TestIsLargeFileThreshold_IsConservative(t *testing.T) {
	const maxAllowed = 100 * 1024 * 1024 // 100MB

	if isLargeFileThreshold > maxAllowed {
		t.Errorf("isLargeFileThreshold = %d (%dMB), 必須 ≤ 100MB (P2-34 ReadAll OOM 邊界)",
			isLargeFileThreshold, isLargeFileThreshold/1024/1024)
	}

	if isLargeFileThreshold <= 0 {
		t.Errorf("isLargeFileThreshold = %d, 必須 > 0", isLargeFileThreshold)
	}
}

// TestLargeFileHandler_GetFileInfo_FlagsLargeFiles 釘住 修法:
// GetFileInfo 對 > 100MB 檔案標 IsLarge=true,使 csv_handler 拒絕 ReadAll path
// 並指示 caller 走 streaming(ProcessLargeFile)。
//
// 實作策略:不寫真 100MB+ 檔(test runner 太慢且佔磁碟),改用 os.Truncate sparse
// file — file size 報 100MB+1 但實際只佔幾 KiB sparse hole。同樣命中
// fileInfo.Size() > isLargeFileThreshold 條件,且 scanFileStructure 在 nil 內容
// 上能跑(讀到 EOF 0 rows)。
func TestLargeFileHandler_GetFileInfo_FlagsLargeFiles(t *testing.T) {
	cfg := config.DefaultConfig()
	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := NewLargeFileHandler(cfg)

	// 寫一個 sparse file 報 size > isLargeFileThreshold,但實際 disk 用量 ~0
	// (lseek + truncate 產生 hole,read 回 NUL bytes,csv reader 視為單 record)。
	testFile := filepath.Join(testDir, "huge.csv")
	f, err := os.Create(testFile) //nolint:gosec // testDir is t.TempDir()
	require.NoError(t, err, "Create sparse file")

	// 寫 header 確保 scanFileStructure 不 fail
	_, err = f.WriteString("Time,Ch1,Ch2\n0.1,1,2\n")
	require.NoError(t, err)

	// Truncate 到 > threshold (101MB)
	const target = int64(isLargeFileThreshold) + 1024*1024 // +1MB
	require.NoError(t, f.Truncate(target))
	require.NoError(t, f.Close())

	info, err := handler.GetFileInfo(testFile)
	require.NoError(t, err, "GetFileInfo on sparse 101MB file")

	if !info.IsLarge {
		t.Errorf("size %d (>%dMB) 必須標 IsLarge=true",
			info.Size, isLargeFileThreshold/1024/1024)
	}
}

// TestLargeFileHandler_GetFileInfo_SmallFilesNotFlagged 對應 happy path:
// 小檔(<<100MB)不該被標 IsLarge,否則 ReadAll 路徑被誤拒,正常 case 退化。
func TestLargeFileHandler_GetFileInfo_SmallFilesNotFlagged(t *testing.T) {
	cfg := config.DefaultConfig()
	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "small.csv")
	content := strings.Repeat("0.1,1,2\n", 100) // ~800 bytes
	require.NoError(t, os.WriteFile(testFile, []byte("Time,Ch1,Ch2\n"+content), fsperm.FilePerm))

	info, err := handler.GetFileInfo(testFile)
	require.NoError(t, err)

	if info.IsLarge {
		t.Errorf("size %d bytes 不該被標 IsLarge=true", info.Size)
	}
}
