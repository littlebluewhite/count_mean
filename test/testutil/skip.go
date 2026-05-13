//go:build !windows

// Package testutil provides shared helpers for cross-platform test setup.
package testutil

import (
	"os"
	"testing"
)

// SkipIfChmodIneffective 跳過依賴 POSIX chmod 0o000 語意的測試。
// Unix root user 仍可讀取 0o000 檔案，故 root 環境也 skip。
func SkipIfChmodIneffective(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("跳過權限測試：以 root 用戶運行，chmod 0o000 無法阻擋讀取")
	}
}
