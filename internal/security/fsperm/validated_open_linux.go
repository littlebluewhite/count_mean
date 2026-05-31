//go:build linux

package fsperm

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

// openat2FallbackWarning emits a single-shot warning the first time we observe
// openat2 returning ENOSYS (kernel < 5.6, or LSM blocking the syscall) and have
// to fall back to stdlib os.OpenFile. P1-1.
//
// Why a one-time warning rather than per-call:
//   - On unsupported kernels every Open* call would fall back, which would
//     drown the logs in repeats. A single boot-time signal is the operator-
//     facing signal that matters ("you are running on a kernel without the
//     atomic openat2 path — the RESOLVE_BENEATH guarantee degrades to
//     EvalSymlinks + O_NOFOLLOW two-stage validation").
//   - The fallback path still has reasonable defense in depth (resolvedPath
//     went through EvalSymlinks, O_NOFOLLOW catches leaf-symlink TOCTOU) —
//     this is a degradation, not a vulnerability, so a warning + continue
//     is the correct severity.
//
// Why stdlib log rather than internal/logging:
//   - internal/logging imports internal/security/fsperm for its NewFileLogger
//     atomic-open path. Reversing the dependency would create a cycle. The
//     project-wide convention is that low-level packages (fsperm, errors)
//     stay free of internal logger imports; stdlib log to stderr is the
//     consistent fallback used by other foundation packages.
//
// Why injectable (warnOpenat2Fallback variable + sync.Once):
//   - Allows tests to install their own counter to assert "fires exactly once
//     across multiple opens". Without injection there's no clean way to
//     observe the warning in a unit test (`log.Output` writes to stderr,
//     redirecting stderr in a single-process test is fragile and races with
//     other tests).
//
//nolint:gochecknoglobals // package-private singleton + injection hook
var (
	// openat2FallbackOnce holds the sync.Once via pointer so tests can swap in
	// a fresh Once without copying a noCopy value (vet false-positive guard).
	openat2FallbackOnce = &sync.Once{}
	// warnOpenat2Fallback is the warning hook invoked at most once (guarded by
	// openat2FallbackOnce). Tests may swap this for assertion; production code
	// must not call it directly — go through emitOpenat2FallbackWarning.
	warnOpenat2Fallback = func() {
		log.Printf("[fsperm] WARN: openat2 returned ENOSYS — falling back to " +
			"os.OpenFile + O_NOFOLLOW. Atomic RESOLVE_BENEATH guarantee unavailable " +
			"on this kernel (< 5.6 or LSM-restricted). This is a one-time warning.")
	}
)

// emitOpenat2FallbackWarning fires warnOpenat2Fallback exactly once across the
// process lifetime, regardless of how many times openat2 ENOSYS is observed.
func emitOpenat2FallbackWarning() {
	openat2FallbackOnce.Do(func() {
		warnOpenat2Fallback()
	})
}

// openValidated 在 Linux 上以
// openat2(RESOLVE_BENEATH | RESOLVE_NO_MAGICLINKS | RESOLVE_NO_SYMLINKS)
// 開啟 resolvedPath,kernel-level 保證:
//
//   - 開出來的 fd 不會逃出 hitBase 目錄
//   - 任何 path component 為 symlink 都被 reject (含 magic links /proc/$$/...)
//
// 這比 caller-side EvalSymlinks + os.OpenFile 兩段式 atomic 得多 — 即便攻擊者
// 在 evalSymlinksWithFallback 與 openat 之間 swap symlink,kernel 在 syscall
// 階段重新檢查,RESOLVE_BENEATH 違反會回 EXDEV、無 magic link 違反回 ELOOP。
//
// fallback:openat2 是 Linux 5.6+ syscall,舊 kernel 回 ENOSYS。我們在 ENOSYS
// 時 fallback 回 os.OpenFile(resolvedPath, WriteFlags, FilePerm) — resolvedPath
// 已透過 EvalSymlinks 校驗,加上 O_NOFOLLOW (來自 WriteFlags) 至少能擋 leaf-
// symlink TOCTOU。完整 atomic 保證在 5.6+ 自動生效。fallback 進入時透過
// emitOpenat2FallbackWarning 觸發 sync.Once 守護的一次性警告,讓 operator 知道
// kernel 路徑已降級。
//
// hitBase 用作 dirfd anchor:openat2 與 RESOLVE_BENEATH 需要 resolvedPath 表達
// 為相對於 hitBase 的 path,以便 kernel 校驗不逃出 anchor。我們把 resolvedPath
// 改寫為 hitBase + relPath 並用 unix.Open(hitBase, O_DIRECTORY) 拿到 dirfd。
func openValidated(resolvedPath, hitBase string) (*os.File, error) {
	relPath, err := filepath.Rel(hitBase, resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("Rel(%s, %s): %w", hitBase, resolvedPath, err)
	}

	// dirfd 指向 hitBase。O_DIRECTORY 確認真的是 dir;O_CLOEXEC 避免 fork-exec 洩漏。
	dirfd, err := unix.Open(hitBase, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open dirfd(%s): %w", hitBase, err)
	}
	defer func() { _ = unix.Close(dirfd) }() //nolint:errcheck // cleanup-only Close;error 無 actionable 處理

	how := &unix.OpenHow{
		// O_CLOEXEC:openat2 是 raw syscall,與 os.OpenFile 不同 — 不會自動帶
		// O_CLOEXEC。Wails spawn webview / helper 子行程時,fd 會被 inherit,
		// 攻擊者可透過子行程繞過 0600 owner-only 權限改寫應用私有檔。明確 OR
		// 進 Flags 補回 stdlib fallback 路徑 (ENOSYS / darwin / windows / 其他 Unix)
		// 透過 os.OpenFile 自動取得的 CLOEXEC。
		Flags: uint64(WriteFlags) | unix.O_CLOEXEC,
		Mode:  uint64(FilePerm),
		// RESOLVE_BENEATH:不允許 walk 出 dirfd 範圍。
		// RESOLVE_NO_MAGICLINKS:擋 /proc/$$/fd 等 magic link 攻擊。
		// RESOLVE_NO_SYMLINKS:任一 path component 為 symlink 即 reject (ELOOP)。
		// 之所以安全 — resolvedPath 是 caller-side evalSymlinksWithFallback 的產物,
		// hitBase 內部合法 symlink 早已被解析掉 (例如 OutputDir 內使用者建的 symlink
		// 指向同 base 下另一 dir,EvalSymlinks 已展開成 real path),故此 flag 不會誤殺
		// 合法 in-base symlink。它擋的是 EvalSymlinks 與 openat2 之間 TOCTOU 窗口被 swap
		// 進來的 symlink — 把 Linux 提升到與 Darwin O_NOFOLLOW_ANY 一致的嚴格度,消除
		// read/write 平台 symlink 政策不對稱 (ADR-0017 Decision 7)。
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS,
	}

	fd, err := unix.Openat2(dirfd, relPath, how)
	if err != nil {
		// openat2 在舊 kernel (< 5.6) 或某些 LSM 拒絕時回 ENOSYS;fallback 到
		// os.OpenFile(resolvedPath) 仍有 O_NOFOLLOW 保護 leaf,且 resolvedPath
		// 已 EvalSymlinks 校驗。警告 operator kernel 路徑已降級。
		if errors.Is(err, unix.ENOSYS) {
			emitOpenat2FallbackWarning()
			return os.OpenFile(resolvedPath, WriteFlags, FilePerm) //nolint:gosec,wrapcheck // resolvedPath 已校驗;fallback 透出原始 *PathError 利 caller 偵測 OS 錯誤
		}
		// EXDEV:RESOLVE_BENEATH 違反 (路徑逃出 dirfd 範圍)。
		// ELOOP:RESOLVE_NO_SYMLINKS / RESOLVE_NO_MAGICLINKS 違反或 O_NOFOLLOW 觸發。
		// 兩者都當作 escape,回 ErrPathEscapesBase 讓 caller 收到一致錯誤型別。
		if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("%w: openat2(%s under %s) rejected: %w",
				ErrPathEscapesBase, relPath, hitBase, err)
		}
		return nil, fmt.Errorf("openat2(%s under %s): %w", relPath, hitBase, err)
	}

	// 把 raw fd 包成 *os.File 給 caller。filename 用 resolvedPath 方便 log/debug。
	return os.NewFile(uintptr(fd), resolvedPath), nil
}

// openReadValidated 是 openValidated 的 read-side 對稱版本:
// 同樣以 openat2(RESOLVE_BENEATH | RESOLVE_NO_MAGICLINKS | RESOLVE_NO_SYMLINKS)
// 為基礎,但 flags 使用 ReadFlags (O_RDONLY | O_NOFOLLOW),提供與寫盤對稱的
// atomic symlink rejection。fallback / 錯誤碼路徑與 openValidated 一致;警告共用同一
// sync.Once,因 write 與 read fallback 對 operator 屬於同一 kernel 降級事件。
func openReadValidated(resolvedPath, hitBase string) (*os.File, error) {
	relPath, err := filepath.Rel(hitBase, resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("Rel(%s, %s): %w", hitBase, resolvedPath, err)
	}

	dirfd, err := unix.Open(hitBase, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open dirfd(%s): %w", hitBase, err)
	}
	defer func() { _ = unix.Close(dirfd) }() //nolint:errcheck // cleanup-only Close;error 無 actionable 處理

	how := &unix.OpenHow{
		// O_CLOEXEC:與 openValidated 對稱 — openat2 raw syscall 不會像 os.OpenFile
		// 自動帶 CLOEXEC,明確 OR 補回,避免子行程 inherit read fd 後讀走 config /
		// translations 等內部檔。
		Flags: uint64(ReadFlags) | unix.O_CLOEXEC,
		Mode:  0,
		// RESOLVE_NO_SYMLINKS 與 openValidated 寫盤端對稱:resolvedPath 已 EvalSymlinks
		// 展開,合法 in-base symlink 不會誤殺;此 flag 擋 EvalSymlinks→openat2 之間
		// swap 進來的 symlink,與 Darwin O_NOFOLLOW_ANY 對齊 (見 openValidated 詳述)。
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS,
	}

	fd, err := unix.Openat2(dirfd, relPath, how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) {
			emitOpenat2FallbackWarning()
			return os.OpenFile(resolvedPath, ReadFlags, 0) //nolint:gosec,wrapcheck // resolvedPath 已校驗;fallback 透出原始 *PathError 利 caller 偵測 OS 錯誤
		}
		if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("%w: openat2(%s under %s, read) rejected: %w",
				ErrPathEscapesBase, relPath, hitBase, err)
		}
		return nil, fmt.Errorf("openat2(%s under %s, read): %w", relPath, hitBase, err)
	}

	return os.NewFile(uintptr(fd), resolvedPath), nil
}
