//go:build darwin

package fsperm

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openAtomicWrite 在 macOS 上以單一 dirfd 錨定整段 tmp-create → rename。dirfd 是 target
// 的「已驗證 leaf 父目錄」:
//
//  1. unix.Open(baseDir, O_DIRECTORY|O_CLOEXEC) 拿 trust-root base 的 dirfd
//  2. (subdir target) unix.Openat(baseDirfd, relParent, O_DIRECTORY|O_NOFOLLOW_ANY|
//     O_CLOEXEC) 從 base 下行到 leaf、關掉 baseDirfd;relParent=="." 時 leaf 即 base
//     (見 openLeafAnchor)
//  3. openatTmp(leafDirfd, tmpBase, O_NOFOLLOW_ANY) 以 basename 建 .tmp
//  4. (commit 時) unix.Renameat(leafDirfd, tmpBase, leafDirfd, targetBase)
//
// 兩個關鍵安全性質:
//   - 下行用 openat(O_NOFOLLOW_ANY) 從 base 開始 → matchAnyBase 之後 swap 進來、survive
//     到 syscall 的 parent-component symlink 以 ELOOP 被擋 (codex P1),與 Linux
//     RESOLVE_NO_SYMLINKS 對齊。
//   - leaf dirfd PIN 住 leaf inode;tmp-create 與 rename 都以 basename 相對它,故 rename
//     沒有中間 component 可被 swap,open 之後抽換 leaf 名也不影響經 fd 的操作 (codex r3 P2)。
//
// macOS 無 RESOLVE_BENEATH 等價;boundary 已在 caller-side matchAnyBase 完成。O_CLOEXEC:
// unix.Open/Openat raw syscall 不自動帶 CLOEXEC (os.OpenFile 才會),顯式 OR 進 flags,
// 避免 Wails 子行程 inherit dirfd / tmp fd。
//
// macOS 11 與更舊 (Go 1.26 baseline 為 macOS 12+,理論上不 hit) 不認 O_NOFOLLOW_ANY,
// openat 回 EINVAL;leaf 下行與 tmp-create 都移除該 flag 重試,dirfd 錨定仍在 (只失去
// per-component symlink rejection,與 validated_open_darwin.go EINVAL fallback 同語義)。
func openAtomicWrite(baseDir, relParent, tmpBase, targetBase, tmpFull, targetFull string) (*AtomicWriteHandle, error) {
	anchorFD, err := openLeafAnchor(baseDir, relParent)
	if err != nil {
		return nil, err // ErrPathEscapesBase (ELOOP) 或已 wrap 的 open 錯誤
	}

	fd, err := openatTmp(anchorFD, tmpBase, unix.O_NOFOLLOW_ANY)
	if errors.Is(err, unix.EINVAL) {
		// macOS 11 / 更舊不認 O_NOFOLLOW_ANY → 移除 flag 重試 (dirfd 錨定保留)。
		fd, err = openatTmp(anchorFD, tmpBase, 0)
	}
	if err != nil {
		_ = unix.Close(anchorFD) //nolint:errcheck // cleanup-only;nothing written through dirfd
		// ELOOP:O_NOFOLLOW_ANY 觸發 (path 含 symlink component)。當 escape 處理,
		// 回 ErrPathEscapesBase 與 Linux EXDEV/ELOOP 路徑一致。
		if errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("%w: openat(%s under leaf, tmp-create) rejected: %w",
				ErrPathEscapesBase, tmpBase, err)
		}
		return nil, fmt.Errorf("openat(%s under leaf, tmp-create): %w", tmpBase, err)
	}

	return &AtomicWriteHandle{
		file:       os.NewFile(uintptr(fd), tmpFull),
		dirfd:      anchorFD,
		tmpRel:     tmpBase,
		targetRel:  targetBase,
		tmpPath:    tmpFull,
		targetPath: targetFull,
	}, nil
}

// openLeafAnchor 開 baseDir 為 dirfd;relParent != "." 時再以 openat(O_NOFOLLOW_ANY) 從
// base 下行到 target 的 leaf 父目錄,回傳該 leaf 作為 anchor 並關掉 baseDir。
// relParent=="." (或 "") → baseDir 本身即 anchor。
//
// 回傳的 fd PIN 住 leaf inode (codex r3 P2);下行從 base dirfd 開始、O_NOFOLLOW_ANY 擋
// 任一 component 為 symlink (codex P1)。EINVAL (舊 macOS 不認 O_NOFOLLOW_ANY) → 移除 flag
// 重試。ELOOP → ErrPathEscapesBase。
func openLeafAnchor(baseDir, relParent string) (int, error) {
	baseDirFD, err := unix.Open(baseDir, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fdNone, fmt.Errorf("open base dirfd(%s): %w", baseDir, err)
	}
	if relParent == "." || relParent == "" {
		return baseDirFD, nil
	}

	leafFD, err := unix.Openat(baseDirFD, relParent, unix.O_DIRECTORY|unix.O_NOFOLLOW_ANY|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.EINVAL) {
		// 舊 macOS 不認 O_NOFOLLOW_ANY → 移除 flag 重試 (錨定仍在)。
		leafFD, err = unix.Openat(baseDirFD, relParent, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	}
	_ = unix.Close(baseDirFD) //nolint:errcheck // base 只用來錨定 leaf 下行
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return fdNone, fmt.Errorf("%w: leaf openat(%s under %s) rejected: %w",
				ErrPathEscapesBase, relParent, baseDir, err)
		}
		return fdNone, fmt.Errorf("leaf openat(%s under %s): %w", relParent, baseDir, err)
	}
	return leafFD, nil
}

// openatTmp 以 dirfd 為錨點 + TmpCreateFlags|extraFlags|O_CLOEXEC 建立 tmpRel。
// extraFlags 用來在第一輪帶 O_NOFOLLOW_ANY、EINVAL fallback 第二輪帶 0。
func openatTmp(dirfd int, tmpRel string, extraFlags int) (int, error) {
	//nolint:wrapcheck // caller (openAtomicWrite) 已 wrap 帶 context;errors.Is 需透傳原始 errno
	return unix.Openat(dirfd, tmpRel, TmpCreateFlags|extraFlags|unix.O_CLOEXEC, uint32(FilePerm))
}

// fsyncDir 對已開啟的 dirfd 觸發 fsync,讓先前的 renameat metadata 變更 crash-durable。
func fsyncDir(fd int) error {
	//nolint:wrapcheck // caller (SyncParentDir) 已 wrap 帶 context;Commit dirfd 路徑 best-effort 忽略
	return unix.Fsync(fd)
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
