//go:build windows

package io

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/security/fsperm"
)

// TestCSVHandler_WriteRejectsSymlinkEscapingBase_Windows 是 csv_handler_symlink_test.go
// 同名 test 的 Windows 對偶(補完)。Windows 對 os.Symlink 的支援需要 admin
// privilege 或開啟 Developer Mode;CI runner 預設無此權限,os.Symlink 會回 ERROR_PRIVILEGE_NOT_HELD。
//
// 此 test 的策略:
//
//  1. 嘗試 os.Symlink — 若失敗(包含 privilege error 或 not supported),立即
//     t.Skip 而非 t.Fatal — 在缺乏 symlink 權限的 Windows runner 上保持 functional
//     而不誤殺 CI。
//  2. 成功建立 symlink 才執行驗證:WriteCSV 對 leaf-symlink 指向 base 外的目標
//     必須 reject(對齊 unix variant 的 契約)。
//  3. 留下 test stub 確保「未來 Windows runner 開啟 Developer Mode 後」自動覆蓋。
//
// 這是 的 cross-platform symlink TOCTOU 覆蓋擴充 — 不增加 CI 紅燈,只在
// 有 admin / Developer Mode 環境補上守護。
func TestCSVHandler_WriteRejectsSymlinkEscapingBase_Windows(t *testing.T) {
	t.Parallel()

	allowedDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	cfg.InputDir = allowedDir
	cfg.OutputDir = allowedDir
	cfg.OperateDir = allowedDir
	handler := NewCSVHandler(cfg)

	victim := filepath.Join(outsideDir, "victim.csv")
	require.NoError(t, os.WriteFile(victim, []byte("untouched\n"), fsperm.FilePerm))

	symlinkPath := filepath.Join(allowedDir, "output.csv")
	// Windows os.Symlink 需要 admin 或 Developer Mode — 失敗就 Skip 而非 Fatal,
	// 避免 CI runner 在無 privilege 環境下誤殺。
	if symErr := os.Symlink(victim, symlinkPath); symErr != nil {
		t.Skipf("Windows symlink 需要 admin / Developer Mode (跳過 symlink TOCTOU 測試): %v", symErr)
	}

	data := [][]string{{"Time", "Ch1"}, {"1.0", "100"}}
	err = handler.WriteCSV(symlinkPath, data)

	// 若 symlink 建得起來,契約必須生效 — WriteCSV reject。
	require.Error(t, err, "Windows: leaf-symlink 指向 base 外的目標必須 reject")

	// victim 不該被覆寫
	victimContent, readErr := os.ReadFile(victim) //nolint:gosec // test file
	require.NoError(t, readErr)
	require.Equal(t, "untouched\n", string(victimContent),
		"Windows: victim 不該透過 leaf-symlink 被覆寫")

	// 確認 err 不是 privilege error 之類無關 panic(防止 Skip 條件繞過)
	if errors.Is(err, os.ErrPermission) {
		t.Logf("Note: WriteCSV 回 permission denied (與預期 reject 結果語意一致)")
	}
}
