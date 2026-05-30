//go:build !linux && !darwin && !windows

package fsperm

import (
	"fmt"
	"os"
)

// openAtomicWrite 在其他 Unix (FreeBSD / OpenBSD / NetBSD 等) 沒有走 openat2 /
// O_NOFOLLOW_ANY 的 dirfd 錨定路徑 — 直接 os.OpenFile(tmpPath, TmpCreateFlags) 建
// .tmp,dirfd 設 fdNone 讓 Commit / Abort 走 os.Rename / os.Remove。O_NOFOLLOW
// (TmpCreateFlags) 擋 leaf-symlink TOCTOU;parent-component symlink 在這條 fallback
// 仍有殘餘 TOCTOU 風險,與 validated_open_other_unix.go 對稱。
//
// 專案主要支援 linux / darwin / windows;此 fallback 僅為編譯完整性。若未來需在
// *BSD 強化,可改用 FreeBSD O_RESOLVE_BENEATH 模仿 atomic_write_linux.go 的 dirfd
// 錨定路徑。
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

// renameatDir / unlinkatDir / closeFD 在其他 Unix 上不可達 — openAtomicWrite 永遠
// 回 dirfd==fdNone。存在僅為滿足跨平台 Commit / Abort 的符號需求;若被呼叫代表
// dirfd 生命週期有 bug,回明確錯誤而非 panic。
//nolint:err113 // unreachable defensive stub; static sentinel would be dead weight (dirfd path never taken on this platform)
func renameatDir(_ int, _, _ string) error {
	return fmt.Errorf("fsperm: renameatDir 在此平台不該被呼叫 (dirfd 路徑不存在)")
}

//nolint:err113 // unreachable defensive stub; see renameatDir
func unlinkatDir(_ int, _ string) error {
	return fmt.Errorf("fsperm: unlinkatDir 在此平台不該被呼叫 (dirfd 路徑不存在)")
}

func closeFD(_ int) {}
