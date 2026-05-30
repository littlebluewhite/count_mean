package fsperm

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWriteHandle holds an open tmp file anchored under a validated parent
// directory, ready to be written, then atomically committed (renameat tmp →
// target) or aborted (unlink tmp). The whole tmp-create → rename sequence is
// performed relative to a single O_DIRECTORY dirfd of the *validated, resolved*
// parent, so an attacker cannot swap a path component between the tmp-create and
// the rename — the classic atomic-write parent-swap TOCTOU race (ADR-0017
// Decision 6, #34 fold-in).
//
// Lifecycle (caller's contract — mirrors WriteCSVAtomic's flow):
//
//	h, err := OpenAtomicWriteValidated(target, tmp, basePaths)
//	// ... defer { if !committed { h.Abort() } } ...
//	// write to h.File(); h.File().Sync(); h.File().Close()
//	h.Commit() // renameat + close dirfd; does NOT re-close the file
//
// Ownership:
//   - OpenAtomicWriteValidated owns dirfd from creation; on any error after the
//     dirfd is opened it closes the dirfd before returning (no fd leak).
//   - The caller owns h.File() — the caller Sync()s and Close()s it before
//     Commit. Commit MUST NOT re-close the file.
//   - Commit closes the dirfd after the rename. Abort closes the dirfd after the
//     unlink. Both are exactly-once on the dirfd: a committed handle's Abort is a
//     no-op, and Abort/Commit are safe to call in the deferred-cleanup pattern.
//
// On the openat2-ENOSYS / Windows / other-Unix fallback path there is no dirfd
// (dirfd == fdNone); Commit degrades to os.Rename(tmpPath, targetPath) and Abort
// to os.Remove(tmpPath), preserving today's behaviour on those platforms.
type AtomicWriteHandle struct {
	// file is the open tmp *os.File the caller writes to. nil only after a
	// failed open (handle is never returned to the caller in that case).
	file *os.File
	// dirfd anchors both the tmp-create and the rename. fdNone on the fallback
	// path (no dirfd available).
	dirfd int
	// tmpBase / targetBase are the basenames used for the dirfd-relative
	// renameat/unlinkat. Empty on the fallback path.
	tmpBase    string
	targetBase string
	// tmpPath / targetPath are the full paths used by the fallback path's
	// os.Rename / os.Remove. Always populated.
	tmpPath    string
	targetPath string
	// done flips true after the first successful Commit or Abort, making the
	// dirfd close exactly-once and a committed handle's Abort a no-op.
	done bool
}

// fdNone marks "no dirfd" on the fallback path (openat2 ENOSYS / Windows /
// other-Unix). A real dirfd is always >= 0.
const fdNone = -1

// File returns the open tmp *os.File the caller writes to. The caller is
// responsible for Sync()+Close() before calling Commit.
func (h *AtomicWriteHandle) File() *os.File { return h.file }

// OpenAtomicWriteValidated validates the parent directory of targetPath, opens
// it as a dirfd, and creates tmpPath's basename anchored under that dirfd, ready
// for the caller to write then Commit (renameat) or Abort (unlink).
//
// targetPath and tmpPath MUST share a parent directory (makeTmpPath guarantees
// this — it keeps the same dir and only mutates the basename). The primitive
// accepts the already-computed tmpPath and never recomputes it; the random
// crypto suffix + NAME_MAX truncation stay in csvutil.makeTmpPath.
//
// Flow (cross-platform, mirrors OpenWriteValidated):
//  1. len(basePaths)==0 → ErrBasePathsEmpty.
//  2. Require filepath.Dir(targetPath) == filepath.Dir(tmpPath) — else error.
//  3. evalSymlinksWithFallback(parent) — resolve the parent's symlinks.
//  4. matchAnyBase(resolvedParent, basePaths) — confirm parent is under a base;
//     miss → ErrPathEscapesBase. (Common case: parent == hitBase, since every
//     WriteCSVAtomic caller targets the OutputDir root — but the parent is
//     validated generically, not assumed to be a base.)
//  5. openAtomicWrite(resolvedParent, tmpBase, targetBase, tmpPath, targetPath)
//     — platform dispatch. See atomic_write_<os>.go.
func OpenAtomicWriteValidated(targetPath, tmpPath string, basePaths []string) (*AtomicWriteHandle, error) {
	if len(basePaths) == 0 {
		return nil, ErrBasePathsEmpty
	}

	// Normalize to absolute before boundary-matching. basePaths arrive already
	// abs-ified (PathValidator.NewPathValidator runs filepath.Abs on each), but a
	// relative targetPath — which the default config produces (OutputDir
	// "./output" → safeJoinOutput returns "output/...") — would leave resolvedParent
	// relative, and matchAnyBase's filepath.Rel(absBase, relTarget) then errors out
	// and wrongly rejects the write with ErrPathEscapesBase. filepath.Abs is a
	// no-op on an already-absolute path, so absolute callers are unaffected.
	absTarget, absErr := filepath.Abs(targetPath)
	if absErr != nil {
		return nil, fmt.Errorf("fsperm.OpenAtomicWriteValidated: 無法絕對化 target %s: %w", targetPath, absErr)
	}
	absTmp, absErr := filepath.Abs(tmpPath)
	if absErr != nil {
		return nil, fmt.Errorf("fsperm.OpenAtomicWriteValidated: 無法絕對化 tmp %s: %w", tmpPath, absErr)
	}
	targetPath, tmpPath = absTarget, absTmp

	targetDir := filepath.Dir(targetPath)
	tmpDir := filepath.Dir(tmpPath)
	if targetDir != tmpDir {
		//nolint:err113 // caller-facing API-misuse message; not a sentinel callers match on (mirrors OpenWriteValidated's dynamic path errors)
		return nil, fmt.Errorf(
			"fsperm.OpenAtomicWriteValidated: tmp 與 target 必須同目錄 (tmpDir=%s, targetDir=%s)",
			tmpDir, targetDir)
	}

	resolvedParent, err := evalSymlinksWithFallback(targetDir)
	if err != nil {
		return nil, fmt.Errorf("fsperm.OpenAtomicWriteValidated: 無法解析父目錄 %s: %w", targetDir, err)
	}

	hitBase, ok := matchAnyBase(resolvedParent, basePaths)
	if !ok {
		return nil, fmt.Errorf("%w: 父目錄 %s 不在 %v 之下", ErrPathEscapesBase, resolvedParent, basePaths)
	}
	// hitBase is the resolved base that contains resolvedParent; for the dirfd
	// anchor we open resolvedParent itself (the validated, fully-resolved parent
	// directory), so renameat/openat2 operate inside the exact directory we
	// boundary-checked, not merely somewhere under hitBase.
	_ = hitBase

	h, err := openAtomicWrite(resolvedParent, filepath.Base(tmpPath), filepath.Base(targetPath), tmpPath, targetPath)
	if err != nil {
		return nil, fmt.Errorf("fsperm.OpenAtomicWriteValidated: %w", err)
	}
	return h, nil
}

// Commit atomically renames the tmp file to the target, anchored under the same
// dirfd the tmp was created in (renameat(dirfd, tmpBase, dirfd, targetBase)) so
// no path component can be swapped between create and rename, then closes the
// dirfd. The caller MUST have already Sync()+Close()'d h.File(); Commit does NOT
// re-close the file.
//
// On the fallback path (dirfd == fdNone) Commit degrades to
// os.Rename(tmpPath, targetPath) — identical to the pre-existing behaviour on
// kernels without openat2 and on Windows / other-Unix.
//
// Commit is exactly-once: a second Commit (or a Commit after Abort) is a no-op
// returning nil, so the deferred abort-on-error pattern is safe.
func (h *AtomicWriteHandle) Commit() error {
	if h.done {
		return nil
	}

	if h.dirfd == fdNone {
		// Fallback: no dirfd anchor; plain rename of the validated full paths.
		if err := os.Rename(h.tmpPath, h.targetPath); err != nil {
			return fmt.Errorf("fsperm.AtomicWriteHandle.Commit: rename tmp → target: %w", err)
		}
		h.done = true
		return nil
	}

	if err := renameatDir(h.dirfd, h.tmpBase, h.targetBase); err != nil {
		// dirfd stays open for the caller's deferred Abort to clean the tmp up.
		return fmt.Errorf("fsperm.AtomicWriteHandle.Commit: renameat(%s → %s): %w",
			h.tmpBase, h.targetBase, err)
	}
	h.closeDirfd()
	h.done = true
	return nil
}

// Abort removes the tmp file and closes the dirfd. It is best-effort and
// idempotent: on a committed handle it is a no-op (the tmp no longer exists — it
// was renamed to the target — and the dirfd is already closed). Errors removing
// the tmp are returned but the dirfd is always closed.
//
// On the dirfd path Abort uses unlinkat(dirfd, tmpBase); on the fallback path it
// uses os.Remove(tmpPath).
func (h *AtomicWriteHandle) Abort() error {
	if h.done {
		return nil
	}
	h.done = true

	if h.dirfd == fdNone {
		if err := os.Remove(h.tmpPath); err != nil {
			return fmt.Errorf("fsperm.AtomicWriteHandle.Abort: remove tmp %s: %w", h.tmpPath, err)
		}
		return nil
	}

	err := unlinkatDir(h.dirfd, h.tmpBase)
	h.closeDirfd()
	if err != nil {
		return fmt.Errorf("fsperm.AtomicWriteHandle.Abort: unlinkat tmp %s: %w", h.tmpBase, err)
	}
	return nil
}

// closeDirfd closes the anchor dirfd if it is real (>= 0) and flips it to fdNone
// so a later close is a no-op. Close errors on a dirfd are not actionable
// (nothing was written through it) and are intentionally ignored.
func (h *AtomicWriteHandle) closeDirfd() {
	if h.dirfd == fdNone {
		return
	}
	closeFD(h.dirfd)
	h.dirfd = fdNone
}
