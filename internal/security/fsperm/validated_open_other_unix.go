//go:build !linux && !darwin && !windows

package fsperm

import "os"

// openValidated 在其他 Unix (FreeBSD / OpenBSD / NetBSD 等) 沒有特殊 atomic
// flag — 直接 open resolvedPath,O_NOFOLLOW (來自 WriteFlags) 擋 leaf-symlink
// TOCTOU,parent component symlink 在這條 fallback 仍有殘餘 TOCTOU 風險。
//
// 專案主要支援 linux / darwin / windows;此 fallback 僅為編譯完整性。若未來需
// 在 *BSD 強化,可改用 FreeBSD 的 O_RESOLVE_BENEATH (見 sys/unix 的
// zerrors_freebsd_*.go) — 模仿 linux openat2 路徑。
func openValidated(resolvedPath, _ /* hitBase */ string) (*os.File, error) {
	//nolint:gosec // resolvedPath 已校驗
	return os.OpenFile(resolvedPath, WriteFlags, FilePerm)
}

// openReadValidated 是 openValidated 的 read-side 對稱版本:其他 Unix
// 沒有 openat2 / O_NOFOLLOW_ANY,直接 os.OpenFile + ReadFlags (含 O_NOFOLLOW),
// parent-component symlink 殘餘 TOCTOU 風險與寫盤對稱。
func openReadValidated(resolvedPath, _ /* hitBase */ string) (*os.File, error) {
	//nolint:gosec // resolvedPath 已校驗
	return os.OpenFile(resolvedPath, ReadFlags, 0)
}
