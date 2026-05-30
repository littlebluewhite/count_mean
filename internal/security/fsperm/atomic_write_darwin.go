//go:build darwin

package fsperm

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openAtomicWrite 在 macOS 上以單一 dirfd 錨定整段 tmp-create → rename:
//
//  1. unix.Open(anchorDir, O_DIRECTORY|O_CLOEXEC) 拿 validated base 的 dirfd
//  2. unix.Openat(dirfd, tmpRel, TmpCreateFlags|O_NOFOLLOW_ANY, FilePerm) 建 .tmp
//  3. (commit 時) unix.Renameat(dirfd, tmpRel, dirfd, targetRel)
//
// create 與 rename 都相對同一 dirfd → 攻擊者無法在兩步間 swap path component,關死
// parent-swap TOCTOU (ADR-0017 Decision 6)。這是 openValidated (darwin) 的「寫一個
// 檔」對映,差別在 dirfd 須跨 create→rename 存活 (由 handle 持有,Commit/Abort 關)。
//
// anchorDir 是 caller 已 boundary-check 的 base (trust root);tmpRel/targetRel 是
// tmp/target 相對該 base 的路徑 (subdir target 時為多段)。macOS 無 RESOLVE_BENEATH
// 等價;boundary 已在 caller-side matchAnyBase 完成。O_NOFOLLOW_ANY (macOS 12+) 讓
// kernel 在 openat 階段拒絕「任一 component 為 symlink」,擋 EvalSymlinks→openat 之間
// swap 進來的 parent-component symlink TOCTOU,與 Linux RESOLVE_NO_SYMLINKS 對齊。
//
// O_CLOEXEC:dirfd 帶 O_CLOEXEC;tmp fd 則因 TmpCreateFlags 已含 O_CLOEXEC? — 不,
// TmpCreateFlags 不含 O_CLOEXEC。但 unix.Openat 與 os.OpenFile 不同,raw syscall 不
// 自動帶 CLOEXEC。darwin 上 openValidated 透過 os.OpenFile 取得自動 CLOEXEC;此處走
// raw openat 故顯式 OR O_CLOEXEC 進 flags,避免 Wails 子行程 inherit tmp fd。
//
// macOS 11 與更舊 (Go 1.26 baseline 為 macOS 12+,理論上不 hit) 不認 O_NOFOLLOW_ANY,
// openat 回 EINVAL;fallback 移除該 flag 重試,dirfd 錨定仍在 (只失去 per-component
// symlink rejection,與 validated_open_darwin.go EINVAL fallback 同語義)。
func openAtomicWrite(anchorDir, tmpRel, targetRel, tmpFull, targetFull string) (*AtomicWriteHandle, error) {
	dirfd, err := unix.Open(anchorDir, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open dirfd(%s): %w", anchorDir, err)
	}

	fd, err := openatTmp(dirfd, tmpRel, unix.O_NOFOLLOW_ANY)
	if errors.Is(err, unix.EINVAL) {
		// macOS 11 / 更舊不認 O_NOFOLLOW_ANY → 移除 flag 重試 (dirfd 錨定保留)。
		fd, err = openatTmp(dirfd, tmpRel, 0)
	}
	if err != nil {
		_ = unix.Close(dirfd) //nolint:errcheck // cleanup-only;nothing written through dirfd
		// ELOOP:O_NOFOLLOW_ANY 觸發 (path 含 symlink component)。當 escape 處理,
		// 回 ErrPathEscapesBase 與 Linux EXDEV/ELOOP 路徑一致。
		if errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("%w: openat(%s under %s, tmp-create) rejected: %w",
				ErrPathEscapesBase, tmpRel, anchorDir, err)
		}
		return nil, fmt.Errorf("openat(%s under %s, tmp-create): %w", tmpRel, anchorDir, err)
	}

	return &AtomicWriteHandle{
		file:       os.NewFile(uintptr(fd), tmpFull),
		dirfd:      dirfd,
		tmpRel:     tmpRel,
		targetRel:  targetRel,
		tmpPath:    tmpFull,
		targetPath: targetFull,
	}, nil
}

// openatTmp 以 dirfd 為錨點 + TmpCreateFlags|extraFlags|O_CLOEXEC 建立 tmpRel。
// extraFlags 用來在第一輪帶 O_NOFOLLOW_ANY、EINVAL fallback 第二輪帶 0。
func openatTmp(dirfd int, tmpRel string, extraFlags int) (int, error) {
	//nolint:wrapcheck // caller (openAtomicWrite) 已 wrap 帶 context;errors.Is 需透傳原始 errno
	return unix.Openat(dirfd, tmpRel, TmpCreateFlags|extraFlags|unix.O_CLOEXEC, uint32(FilePerm))
}

// renameatDir 以 dirfd 為來源與目的錨點原子改名 tmpRel → targetRel。只在 dirfd
// 路徑被呼叫 (darwin 無 ENOSYS fallback,恆走 dirfd)。
func renameatDir(dirfd int, tmpRel, targetRel string) error {
	//nolint:wrapcheck // caller (Commit) 已 wrap 帶 context
	return unix.Renameat(dirfd, tmpRel, dirfd, targetRel)
}

// unlinkatDir 以 dirfd 為錨點刪除 tmpRel。只在 dirfd 路徑被呼叫。
func unlinkatDir(dirfd int, tmpRel string) error {
	//nolint:wrapcheck // caller (Abort) 已 wrap 帶 context
	return unix.Unlinkat(dirfd, tmpRel, 0)
}

// closeFD 關閉 raw dirfd。close 錯誤對 dirfd 無 actionable 處理,刻意忽略。
func closeFD(fd int) {
	_ = unix.Close(fd) //nolint:errcheck // cleanup-only Close
}
