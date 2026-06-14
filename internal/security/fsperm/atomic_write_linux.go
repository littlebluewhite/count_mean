//go:build linux

package fsperm

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// openAtomicWrite 在 Linux 上以單一 dirfd 錨定整段 tmp-create → rename。dirfd 是
// target 的「已驗證 leaf 父目錄」:
//
//  1. unix.Open(baseDir, O_DIRECTORY|O_CLOEXEC) 拿 trust-root base 的 dirfd
//  2. (subdir target) unix.Openat2(baseDirfd, relParent, O_DIRECTORY|
//     RESOLVE_BENEATH|NO_MAGICLINKS|NO_SYMLINKS) 從 base 下行到 leaf、關掉 baseDirfd;
//     relParent=="." 時 leaf 即 base (見 openLeafAnchor)
//  3. unix.Openat2(leafDirfd, tmpBase, RESOLVE_…) 以 basename 建 .tmp
//  4. (commit 時) unix.Renameat(leafDirfd, tmpBase, leafDirfd, targetBase)
//
// 兩個關鍵安全性質:
//   - 下行用 openat2(RESOLVE_NO_SYMLINKS) 從 base 開始、不重走絕對路徑 → matchAnyBase
//     之後被 swap 進來的 parent-component symlink 會被 ELOOP 擋 (codex P1)。
//   - leaf dirfd PIN 住 leaf inode;tmp-create 與 rename 都以 basename 相對它,故 rename
//     沒有中間 component 可被 swap,且 open 之後抽換 leaf 名也不影響經 fd 的操作
//     (codex r3 P2)。
//
// RESOLVE_NO_SYMLINKS 的安全論證與 openValidated 一致:resolvedParent 是
// evalSymlinksWithFallback 的產物,合法 in-base symlink 早已展開;此 flag 擋的是
// EvalSymlinks→openat2 之間 swap 進來、survive 到 syscall 的 symlink (TOCTOU),
// 與 Darwin O_NOFOLLOW_ANY 對齊。RESOLVE_BENEATH 兜底,任何 walk 出 dirfd 的企圖回 EXDEV。
//
// O_CLOEXEC:openat2 raw syscall 不像 os.OpenFile 自動帶 CLOEXEC,明確 OR 進 Flags
// (與 openValidated:108 相同) — Wails spawn 子行程時避免 dirfd / tmp fd 外洩。
//
// fallback:openat2 在 < 5.6 kernel 或 LSM 拒絕時回 ENOSYS (leaf 下行或 tmp-create 任一
// 站點)。降級為 os.OpenFile(tmpFull, TmpCreateFlags, FilePerm) + dirfd=fdNone,Commit
// 改走 os.Rename(tmpFull, targetFull)、Abort 改走 os.Remove(tmpFull) — 等同今日無 dirfd
// 行為。tmpFull 父目錄已 EvalSymlinks 校驗、TmpCreateFlags 帶 O_NOFOLLOW 擋 leaf symlink;
// subdir parent-component swap 在此 ancient-kernel fallback 仍有殘餘 TOCTOU (與讀/寫端
// 一致、已知 deferred)。透過 emitOpenat2FallbackWarning 觸發一次性降級警告 (與讀/寫端
// 共用同一 sync.Once)。EXDEV/ELOOP → ErrPathEscapesBase。
func openAtomicWrite(baseDir, relParent, tmpBase, targetBase, tmpFull, targetFull string) (*AtomicWriteHandle, error) {
	anchorFD, err := openLeafAnchor(baseDir, relParent)
	if errors.Is(err, unix.ENOSYS) {
		emitOpenat2FallbackWarning()
		return openAtomicWriteFallback(tmpFull, targetFull)
	}
	if err != nil {
		return nil, err // ErrPathEscapesBase (ELOOP/EXDEV) 或已 wrap 的 open 錯誤
	}

	how := &unix.OpenHow{
		Flags:   uint64(TmpCreateFlags) | unix.O_CLOEXEC,
		Mode:    uint64(FilePerm),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS,
	}

	fd, err := unix.Openat2(anchorFD, tmpBase, how)
	if err != nil {
		_ = unix.Close(anchorFD) //nolint:errcheck // cleanup-only;nothing written through dirfd

		// ENOSYS:舊 kernel / LSM 無 openat2。降級為無 dirfd 的 os.OpenFile + os.Rename。
		if errors.Is(err, unix.ENOSYS) {
			emitOpenat2FallbackWarning()
			return openAtomicWriteFallback(tmpFull, targetFull)
		}
		// EXDEV (RESOLVE_BENEATH 違反) / ELOOP (NO_SYMLINKS / NO_MAGICLINKS / O_NOFOLLOW)
		// → 一律當 escape,回 ErrPathEscapesBase 讓 caller 收到一致錯誤型別。
		if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("%w: openat2(%s under leaf, tmp-create) rejected: %w",
				ErrPathEscapesBase, tmpBase, err)
		}
		return nil, fmt.Errorf("openat2(%s under leaf, tmp-create): %w", tmpBase, err)
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

// openLeafAnchor 開 baseDir 為 dirfd;relParent != "." 時再以單一 openat2
// (RESOLVE_BENEATH|NO_SYMLINKS) 從 base 下行到 target 的 leaf 父目錄,回傳該 leaf 作為
// anchor 並關掉 baseDir。relParent=="." (或 "") → baseDir 本身即 anchor。
//
// 回傳的 fd PIN 住 leaf inode:後續 tmp-create / renameat / unlinkat 以 basename 相對它,
// open 之後抽換任一 path component 都無法把這些操作導向別處 (codex r3 P2)。下行從 base
// dirfd 開始 (非重開絕對 resolvedParent),故 matchAnyBase 之後 swap 進來的 parent symlink
// 會被 RESOLVE_NO_SYMLINKS 擋 (codex P1)。openat2 ENOSYS 原樣回傳供 caller fallback;
// ELOOP/EXDEV → ErrPathEscapesBase。
func openLeafAnchor(baseDir, relParent string) (int, error) {
	baseDirFD, err := unix.Open(baseDir, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return fdNone, fmt.Errorf("open base dirfd(%s): %w", baseDir, err)
	}
	if relParent == "." || relParent == "" {
		return baseDirFD, nil
	}

	how := &unix.OpenHow{
		Flags:   uint64(unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS,
	}
	leafFD, err := unix.Openat2(baseDirFD, relParent, how)
	_ = unix.Close(baseDirFD) //nolint:errcheck // base 只用來錨定 leaf 下行,leaf 開完即釋放
	if err != nil {
		if errors.Is(err, unix.ENOSYS) {
			// 包成 %w 保留 ENOSYS(caller 的 errors.Is 透過 wrap 仍偵測得到)→ 走 non-dirfd fallback。
			return fdNone, fmt.Errorf("leaf openat2 ENOSYS, fall back to os.Rename: %w", err)
		}
		if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ELOOP) {
			return fdNone, fmt.Errorf("%w: leaf openat2(%s under %s) rejected: %w",
				ErrPathEscapesBase, relParent, baseDir, err)
		}
		return fdNone, fmt.Errorf("leaf openat2(%s under %s): %w", relParent, baseDir, err)
	}
	return leafFD, nil
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

// fsyncDir 對已開啟的 dirfd 觸發 fsync,讓先前的 renameat metadata 變更 crash-durable。
func fsyncDir(fd int) error {
	//nolint:wrapcheck // caller (SyncParentDir) 已 wrap 帶 context;Commit dirfd 路徑 best-effort 忽略
	return unix.Fsync(fd)
}

// renameatDir 以 dirfd 為來源與目的錨點原子改名 tmpRel → targetRel。只在 dirfd
// 路徑 (非 fallback) 被呼叫。
func renameatDir(dirfd int, tmpRel, targetRel string) error {
	//nolint:wrapcheck // caller (Commit) 已 wrap 帶 context
	return unix.Renameat(dirfd, tmpRel, dirfd, targetRel)
}

// unlinkatDir 以 dirfd 為錨點刪除 tmpRel。只在 dirfd 路徑 (非 fallback) 被呼叫。
func unlinkatDir(dirfd int, tmpRel string) error {
	//nolint:wrapcheck // caller (Abort) 已 wrap 帶 context
	return unix.Unlinkat(dirfd, tmpRel, 0)
}

// closeFD 關閉 raw dirfd。close 錯誤對 dirfd 無 actionable 處理 (未透過它寫入任何
// 資料),刻意忽略。
func closeFD(fd int) {
	_ = unix.Close(fd) //nolint:errcheck // cleanup-only Close
}
