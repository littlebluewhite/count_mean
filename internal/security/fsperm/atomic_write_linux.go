//go:build linux

package fsperm

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openAtomicWrite 在 Linux 上以單一 dirfd 錨定整段 tmp-create → rename:
//
//  1. unix.Open(resolvedParent, O_DIRECTORY|O_CLOEXEC) 拿到 validated 父目錄的 dirfd
//  2. unix.Openat2(dirfd, tmpBase, RESOLVE_BENEATH|NO_MAGICLINKS|NO_SYMLINKS) 建 .tmp
//  3. (commit 時) unix.Renameat(dirfd, tmpBase, dirfd, targetBase)
//
// 因為 create 與 rename 都相對於同一個 dirfd,攻擊者無法在兩步之間 swap 任一 path
// component — 把 atomic-write 的 parent-swap TOCTOU 競態關死 (ADR-0017 Decision 6)。
// 這是 openValidated 的「寫一個檔」對映,但多了 dirfd 必須跨 create→rename 存活的
// 生命週期:dirfd 由 handle 持有,Commit / Abort 負責關閉。
//
// RESOLVE_NO_SYMLINKS 的安全論證與 openValidated 一致:resolvedParent 是
// evalSymlinksWithFallback 的產物,合法 in-base symlink 早已展開;此 flag 擋的是
// EvalSymlinks→openat2 之間 swap 進來、survive 到 syscall 的 symlink (TOCTOU),
// 與 Darwin O_NOFOLLOW_ANY 對齊。tmpBase 是 makeTmpPath 產的單一 basename,理應
// 不含 path separator;RESOLVE_BENEATH 仍兜底,任何 walk 出 dirfd 的企圖回 EXDEV。
//
// O_CLOEXEC:openat2 raw syscall 不像 os.OpenFile 自動帶 CLOEXEC,明確 OR 進 Flags
// (與 openValidated:108 相同) — Wails spawn 子行程時避免 tmp fd 外洩。
//
// fallback:openat2 在 < 5.6 kernel 或 LSM 拒絕時回 ENOSYS。降級為
// os.OpenFile(tmpPath, TmpCreateFlags, FilePerm) + dirfd=fdNone,Commit 改走
// os.Rename(tmpPath, targetPath)、Abort 改走 os.Remove(tmpPath) — 等同今日無 dirfd
// 的行為。tmpPath 的父目錄已 EvalSymlinks 校驗,TmpCreateFlags 帶 O_NOFOLLOW 擋
// leaf symlink。透過 emitOpenat2FallbackWarning 觸發一次性降級警告 (與讀/寫端共用
// 同一 sync.Once)。EXDEV/ELOOP → ErrPathEscapesBase。
func openAtomicWrite(resolvedParent, tmpBase, targetBase, tmpPath, targetPath string) (*AtomicWriteHandle, error) {
	dirfd, err := unix.Open(resolvedParent, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open dirfd(%s): %w", resolvedParent, err)
	}

	how := &unix.OpenHow{
		Flags:   uint64(TmpCreateFlags) | unix.O_CLOEXEC,
		Mode:    uint64(FilePerm),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS,
	}

	fd, err := unix.Openat2(dirfd, tmpBase, how)
	if err != nil {
		_ = unix.Close(dirfd) //nolint:errcheck // cleanup-only;nothing written through dirfd

		// ENOSYS:舊 kernel / LSM 無 openat2。降級為無 dirfd 的 os.OpenFile + os.Rename。
		if errors.Is(err, unix.ENOSYS) {
			emitOpenat2FallbackWarning()
			return openAtomicWriteFallback(tmpPath, targetPath)
		}
		// EXDEV (RESOLVE_BENEATH 違反) / ELOOP (NO_SYMLINKS / NO_MAGICLINKS / O_NOFOLLOW)
		// → 一律當 escape,回 ErrPathEscapesBase 讓 caller 收到一致錯誤型別。
		if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("%w: openat2(%s under %s, tmp-create) rejected: %w",
				ErrPathEscapesBase, tmpBase, resolvedParent, err)
		}
		return nil, fmt.Errorf("openat2(%s under %s, tmp-create): %w", tmpBase, resolvedParent, err)
	}

	return &AtomicWriteHandle{
		file:       os.NewFile(uintptr(fd), tmpPath),
		dirfd:      dirfd,
		tmpBase:    tmpBase,
		targetBase: targetBase,
		tmpPath:    tmpPath,
		targetPath: targetPath,
	}, nil
}

// openAtomicWriteFallback 是 openat2 ENOSYS 時的無-dirfd 降級:沿用今日
// os.OpenFile(TmpCreateFlags) + os.Rename 流程。dirfd 設 fdNone 讓 Commit / Abort
// 走 os.Rename / os.Remove 分支。tmpPath 父目錄已 EvalSymlinks 校驗,O_NOFOLLOW
// (TmpCreateFlags) 擋 leaf symlink。
func openAtomicWriteFallback(tmpPath, targetPath string) (*AtomicWriteHandle, error) {
	//nolint:gosec // tmpPath 父目錄已 EvalSymlinks + matchAnyBase 校驗;TmpCreateFlags 帶 O_NOFOLLOW
	f, err := os.OpenFile(tmpPath, TmpCreateFlags, FilePerm)
	if err != nil {
		return nil, fmt.Errorf("fallback OpenFile(%s): %w", tmpPath, err)
	}
	return &AtomicWriteHandle{
		file:       f,
		dirfd:      fdNone,
		tmpPath:    tmpPath,
		targetPath: targetPath,
	}, nil
}

// renameatDir 以 dirfd 為來源與目的錨點原子改名 tmpBase → targetBase。只在 dirfd
// 路徑 (非 fallback) 被呼叫。
func renameatDir(dirfd int, tmpBase, targetBase string) error {
	//nolint:wrapcheck // caller (Commit) 已 wrap 帶 context
	return unix.Renameat(dirfd, tmpBase, dirfd, targetBase)
}

// unlinkatDir 以 dirfd 為錨點刪除 tmpBase。只在 dirfd 路徑 (非 fallback) 被呼叫。
func unlinkatDir(dirfd int, tmpBase string) error {
	//nolint:wrapcheck // caller (Abort) 已 wrap 帶 context
	return unix.Unlinkat(dirfd, tmpBase, 0)
}

// closeFD 關閉 raw dirfd。close 錯誤對 dirfd 無 actionable 處理 (未透過它寫入任何
// 資料),刻意忽略。
func closeFD(fd int) {
	_ = unix.Close(fd) //nolint:errcheck // cleanup-only Close
}
