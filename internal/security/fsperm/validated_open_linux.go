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

	"count_mean/internal/security/redact"
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

// openReadValidated 在 Linux 上以
// openat2(RESOLVE_BENEATH | RESOLVE_NO_MAGICLINKS | RESOLVE_NO_SYMLINKS) + ReadFlags
// (O_RDONLY | O_NOFOLLOW) 開啟 resolvedPath,kernel-level 保證:
//
//   - 開出來的 fd 不會逃出 hitBase 目錄
//   - 任何 path component 為 symlink 都被 reject (含 magic links /proc/$$/...)
//
// 這比 caller-side EvalSymlinks + os.OpenFile 兩段式 atomic 得多 — 即便攻擊者
// 在 EvalSymlinksWithFallback 與 openat 之間 swap symlink,kernel 在 syscall
// 階段重新檢查,RESOLVE_BENEATH 違反回 EXDEV、symlink 違反回 ELOOP。
//
// fallback:openat2 是 Linux 5.6+ syscall,舊 kernel 回 ENOSYS。此時 fallback 回
// os.OpenFile(resolvedPath, ReadFlags, 0) — resolvedPath 已透過 EvalSymlinks 校驗,
// O_NOFOLLOW 至少能擋 leaf-symlink TOCTOU。進 fallback 時 emitOpenat2FallbackWarning
// 觸發一次性警告;與 atomic write 端共用同一 sync.Once,對 operator 是同一個 kernel
// 降級事件。
//
// hitBase 用作 dirfd anchor:RESOLVE_BENEATH 需要 resolvedPath 表達為相對於 hitBase
// 的 path,kernel 才能校驗不逃出 anchor。
func openReadValidated(resolvedPath, hitBase string) (*os.File, error) {
	relPath, err := filepath.Rel(hitBase, resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("Rel(base, %s): %w", redact.Paths(resolvedPath), err)
	}

	dirfd, err := unix.Open(hitBase, unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("open dirfd(%s): %w", redactDir(hitBase), err)
	}
	defer func() { _ = unix.Close(dirfd) }() //nolint:errcheck // cleanup-only Close;error 無 actionable 處理

	how := &unix.OpenHow{
		// O_CLOEXEC:openat2 raw syscall 不會像 os.OpenFile 自動帶 CLOEXEC,明確 OR
		// 補回,避免 Wails spawn 的子行程 inherit read fd 後讀走 config / translations
		// 等內部檔。
		Flags: uint64(ReadFlags) | unix.O_CLOEXEC,
		Mode:  0,
		// RESOLVE_BENEATH:不允許 walk 出 dirfd 範圍。
		// RESOLVE_NO_MAGICLINKS:擋 /proc/$$/fd 等 magic link 攻擊。
		// RESOLVE_NO_SYMLINKS:resolvedPath 已 EvalSymlinks 展開,合法 in-base symlink
		// 不會誤殺;此 flag 擋 EvalSymlinks→openat2 之間 swap 進來的 symlink,與 Darwin
		// O_NOFOLLOW_ANY 對齊 (ADR-0017 Decision 7)。
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_SYMLINKS,
	}

	fd, err := unix.Openat2(dirfd, relPath, how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) {
			emitOpenat2FallbackWarning()
			return os.OpenFile(resolvedPath, ReadFlags, 0) //nolint:gosec,wrapcheck // resolvedPath 已校驗;fallback 透出原始 *PathError 利 caller 偵測 OS 錯誤
		}
		// EXDEV (RESOLVE_BENEATH 違反) / ELOOP (NO_SYMLINKS / NO_MAGICLINKS / O_NOFOLLOW)
		// 一律當 escape,回 ErrPathEscapesBase。訊息只帶 redact.Paths(resolvedPath)
		// (保留檔名):relPath 的目錄段與 hitBase 的末段都是病患目錄名,不列出。
		if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ELOOP) {
			return nil, fmt.Errorf("%w: openat2(%s, read) rejected: %w",
				ErrPathEscapesBase, redact.Paths(resolvedPath), err)
		}
		return nil, fmt.Errorf("openat2(%s, read): %w", redact.Paths(resolvedPath), err)
	}

	return os.NewFile(uintptr(fd), resolvedPath), nil
}
