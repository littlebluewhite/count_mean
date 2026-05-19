//go:build !windows

package io

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// TestCSVHandler_ParentSymlinkRejected 釘住 WriteCSV 必須擋 parent-symlink
// escape 攻擊。allowed/link → outsideDir,寫 allowed/link/out.csv 必須 reject 而非
// 把檔案寫到 outsideDir/out.csv。
//
// V1 verify report (.review_reports/verify_01_parent_symlink_toctou.md) 的
// Exploit 1：lexical isPathWithinBase + O_NOFOLLOW 只擋 leaf,留下 parent component
// 為 symlink 的攻擊面。WriteCSV 過去走 lexical-only,本 test 過去會 false-pass,
// 修法後切到 fsperm.OpenWriteValidated 並 EvalSymlinks resolve 後再 boundary
// check,parent-symlink 解析後落在 outsideDir 外於 allowedDir,reject。
func TestCSVHandler_ParentSymlinkRejected(t *testing.T) {
	t.Parallel()

	// EvalSymlinks 解析 tempDir,避免 macOS /var → /private/var 干擾 boundary check
	allowedDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	cfg.InputDir = allowedDir
	cfg.OutputDir = allowedDir
	cfg.OperateDir = allowedDir
	handler := NewCSVHandler(cfg)

	// 攻擊者在 allowedDir 內植入 symlink → outsideDir
	linkPath := filepath.Join(allowedDir, "link")
	require.NoError(t, os.Symlink(outsideDir, linkPath))

	// 嘗試 WriteCSV 到 allowed/link/out.csv — 寫盤 path 解析後落在 outsideDir,
	// 應被 OpenWriteValidated 阻擋。
	attackPath := filepath.Join(linkPath, "out.csv")
	data := [][]string{{"Time", "Ch1"}, {"1.0", "100"}}
	writeErr := handler.WriteCSV(attackPath, data)
	require.Error(t, writeErr, "parent-symlink escape 必須 reject")

	// outsideDir 內不該被建立任何檔案
	_, statErr := os.Stat(filepath.Join(outsideDir, "out.csv"))
	require.True(t, os.IsNotExist(statErr),
		"out.csv 不該透過 link 寫到 outsideDir 內,stat err = %v", statErr)
}
