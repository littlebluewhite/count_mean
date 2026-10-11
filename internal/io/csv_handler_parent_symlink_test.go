//go:build !windows

package io

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// TestCSVHandler_ParentSymlinkRejected 釘住 File-based 寫檔必須擋 parent-symlink
// escape 攻擊。allowed/link → outsideDir,寫 allowed/link/out.csv 必須 reject 而非
// 把檔案寫到 outsideDir/out.csv。
//
// V1 verify report (.review_reports/verify_01_parent_symlink_toctou.md) 的
// Exploit 1:lexical containment(fsperm.IsWithin)看不出 parent component 是 symlink。
// placement 的 fsperm.AtomicWriteFile 帶 allow-list,resolve parent 後不在 base 之下
// → ErrPathEscapesBase。leaf symlink 會被 rename 取代(見 csv_handler_symlink_test.go),
// parent symlink 仍然拒寫。
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

	// 嘗試寫到 allowed/link/out.csv — 寫盤 path 解析後落在 outsideDir,必須被擋。
	data := [][]string{{"Time", "Ch1"}, {"1.0", "100"}}
	_, writeErr := handler.WriteFileCSV(WriteRequest{SubDir: "link", Filename: "out.csv"}, data)
	require.Error(t, writeErr, "parent-symlink escape 必須 reject")

	// outsideDir 內不該被建立任何檔案(含 tmp)
	entries, readErr := os.ReadDir(outsideDir)
	require.NoError(t, readErr)
	require.Empty(t, entries, "不該透過 link 在 outsideDir 內留下任何檔案")
}
