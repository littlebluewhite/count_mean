//go:build windows

package fsperm

import "os"

// openValidated 在 Windows 上沒有 O_NOFOLLOW / RESOLVE_BENEATH 等價 flag,直接
// open resolvedPath。defense-in-depth 由 caller-side EvalSymlinks 提供 (已在
// OpenWriteValidated dispatch 前完成)。
//
// 殘餘 TOCTOU 窗口:caller-side EvalSymlinks 與 syscall.CreateFile 之間若被
// swap symlink,kernel 跟到底。Mitigation:
//   - Windows 建立 reparse point / symlink 需要 SeCreateSymbolicLinkPrivilege
//     (admin 或 Developer Mode);unprivileged 攻擊者通常無權植入。
//   - 寫盤行為仍受 OS ACL 約束,寫不到 user 不該寫的位置。
//   - 升級到 golang.org/x/sys/windows.CreateFile + FILE_FLAG_OPEN_REPARSE_POINT
//     可在 syscall 階段拒絕跟 reparse point — 列為 follow-up,需 Windows CI 才能
//     可信驗證。
//
// hitBase 在 Windows 不用,沿用 *os.File 路徑即可。
func openValidated(resolvedPath, _ /* hitBase */ string) (*os.File, error) {
	//nolint:gosec // resolvedPath 已透過 EvalSymlinks + matchAnyBase 校驗
	return os.OpenFile(resolvedPath, WriteFlags, FilePerm)
}

// openReadValidated 是 openValidated 的 read-side 對稱版本:Windows 同樣
// 沒有 atomic O_NOFOLLOW 等價物,read 路徑也只能仰賴 caller-side EvalSymlinks
// (在 OpenReadValidated dispatch 前完成)。
//
// 與 openValidated 同樣的殘餘 TOCTOU 窗口 / mitigation 適用 — 真正 kernel-level
// guard 要等 windows.CreateFile + FILE_FLAG_OPEN_REPARSE_POINT 改寫,
// 列為 follow-up (見 flags_windows.go package doc "Future work")。
func openReadValidated(resolvedPath, _ /* hitBase */ string) (*os.File, error) {
	//nolint:gosec // resolvedPath 已透過 EvalSymlinks + matchAnyBase 校驗
	return os.OpenFile(resolvedPath, ReadFlags, 0)
}
