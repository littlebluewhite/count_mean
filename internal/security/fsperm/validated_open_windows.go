//go:build windows

package fsperm

import "os"

// openReadValidated 在 Windows 上沒有 O_NOFOLLOW / RESOLVE_BENEATH 等價 flag,直接
// open resolvedPath。defense-in-depth 由 caller-side EvalSymlinks 提供 (已在
// OpenReadValidated dispatch 前完成)。hitBase 在 Windows 不用。
//
// 殘餘 TOCTOU 窗口:caller-side EvalSymlinks 與 syscall.CreateFile 之間若被
// swap symlink,kernel 跟到底。Mitigation:
//   - Windows 建立 reparse point / symlink 需要 SeCreateSymbolicLinkPrivilege
//     (admin 或 Developer Mode);unprivileged 攻擊者通常無權植入。
//   - 真正 kernel-level guard 要等 windows.CreateFile + FILE_FLAG_OPEN_REPARSE_POINT
//     改寫,列為 follow-up (見 flags_windows.go package doc "Future work")。
func openReadValidated(resolvedPath, _ /* hitBase */ string) (*os.File, error) {
	//nolint:gosec // resolvedPath 已透過 EvalSymlinks + matchAnyBase 校驗
	return os.OpenFile(resolvedPath, ReadFlags, 0)
}
