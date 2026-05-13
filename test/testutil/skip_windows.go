//go:build windows

// Package testutil provides shared helpers for cross-platform test setup.
package testutil

import "testing"

// SkipIfChmodIneffective 在 Windows 上必定 skip：
// NTFS 不支援 POSIX chmod 0o000 — 修改 mode bits 對 owner 讀取無效。
func SkipIfChmodIneffective(t *testing.T) {
	t.Helper()
	t.Skip("跳過權限測試：Windows NTFS 不支援 POSIX chmod 0o000 語意")
}
