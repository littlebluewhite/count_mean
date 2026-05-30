//go:build windows

package fsperm

import (
	"fmt"
	"os"
)

// openAtomicWrite 在 Windows 上沒有 dirfd / openat2 / renameat 等價物,直接以
// os.OpenFile(tmpPath, TmpCreateFlags, FilePerm) 建 .tmp,dirfd 設 fdNone 讓
// Commit / Abort 走 os.Rename(tmpPath, targetPath) / os.Remove(tmpPath) — 與今日
// csvutil.WriteCSVAtomic 在 Windows 上的行為完全一致。
//
// 安全性:parent-swap TOCTOU 的 kernel-level 防護在 Windows 不可得 (需
// CreateFile + FILE_FLAG_OPEN_REPARSE_POINT,ADR-0017 Decision 8 明確 defer)。
// defense-in-depth 由 caller-side EvalSymlinks (OpenAtomicWriteValidated 內已對
// 父目錄做 evalSymlinksWithFallback + matchAnyBase) 提供;殘餘 NTFS junction /
// reparse-point 風險與 flags_windows.go package doc 所述相同。
//
// 此處不嘗試 CreateFile reparse-point 處理 — Windows symlink-atomic 列為 follow-up
// (見 flags_windows.go "Future work",需 Windows CI 才能可信驗證)。
func openAtomicWrite(_ /* resolvedParent */, _ /* tmpBase */, _ /* targetBase */, tmpPath, targetPath string) (
	*AtomicWriteHandle, error,
) {
	//nolint:gosec // tmpPath 父目錄已 EvalSymlinks + matchAnyBase 校驗 (caller-side)
	f, err := os.OpenFile(tmpPath, TmpCreateFlags, FilePerm)
	if err != nil {
		return nil, fmt.Errorf("OpenFile(%s): %w", tmpPath, err)
	}
	return &AtomicWriteHandle{
		file:       f,
		dirfd:      fdNone,
		tmpPath:    tmpPath,
		targetPath: targetPath,
	}, nil
}

// renameatDir / unlinkatDir / closeFD 在 Windows 上不可達 — openAtomicWrite 永遠
// 回 dirfd==fdNone,Commit / Abort 因此只走 os.Rename / os.Remove 分支,從不進
// dirfd 分支。它們存在僅為滿足跨平台 Commit / Abort 的符號需求;若被呼叫代表
// dirfd 生命週期有 bug,回明確錯誤而非 panic。
//
//nolint:err113 // unreachable defensive stub; static sentinel would be dead weight (dirfd path never taken on Windows)
func renameatDir(_ int, _, _ string) error {
	return fmt.Errorf("fsperm: renameatDir 在 Windows 不該被呼叫 (dirfd 路徑不存在)")
}

//nolint:err113 // unreachable defensive stub; see renameatDir
func unlinkatDir(_ int, _ string) error {
	return fmt.Errorf("fsperm: unlinkatDir 在 Windows 不該被呼叫 (dirfd 路徑不存在)")
}

func closeFD(_ int) {}
