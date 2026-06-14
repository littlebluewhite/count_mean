package fsperm

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AtomicWriteHandle holds an open tmp file anchored under a validated base
// directory, ready to be written, then atomically committed (renameat tmp →
// target) or aborted (unlink tmp). The tmp-create → rename sequence is performed
// relative to a single O_DIRECTORY dirfd of the target's *validated leaf parent*,
// reached by descending from the trust-root base (matchAnyBase) via openat2
// RESOLVE_NO_SYMLINKS / openat O_NOFOLLOW_ANY. tmp/target are addressed by their
// BASENAME relative to that pinned dirfd, so (a) the descent follows no swapped
// parent component (codex P1) and (b) the rename has no intermediate component a
// post-create swap could redirect through (codex r3 P2) — closing the atomic-write
// parent-swap TOCTOU race (ADR-0017 Decision 6, #34 fold-in).
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
	// dirfd anchors both the tmp-create and the rename on the target's validated
	// leaf parent (the immediate parent of tmp/target). fdNone on the fallback path
	// (no dirfd available).
	dirfd int
	// tmpRel / targetRel are the BASENAMES of tmp/target relative to dirfd (the leaf
	// parent), used for the dirfd-relative renameat/unlinkat. Single-component by
	// construction — the leaf parent IS the dirfd, so a rename through it traverses
	// no swappable intermediate component (codex r3 P2). Empty on the fallback path.
	tmpRel    string
	targetRel string
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
//     miss → ErrPathEscapesBase.
//  5. relParent = Rel(hitBase, resolvedParent) ("." when the parent IS the base);
//     belt-and-suspenders reject a ".." escape.
//  6. openAtomicWrite(hitBase, relParent, tmpBase, targetBase, resolvedTmp,
//     resolvedTarget) — platform dispatch. It opens hitBase, descends relParent to
//     the leaf with RESOLVE_NO_SYMLINKS / O_NOFOLLOW_ANY, PINS it as the dirfd, and
//     addresses tmp/target by basename — so neither the descent nor the rename can
//     follow a swapped component (codex P1 + r3 P2). See atomic_write_<os>.go.
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

	// Anchor on the target's *validated leaf parent*, reached by descending from the
	// trust-root base. The platform layer opens hitBase, then (for a subdir target)
	// descends relParent to the leaf with RESOLVE_NO_SYMLINKS / O_NOFOLLOW_ANY and
	// PINS it as the dirfd; tmp-create + rename + unlink then use the leaf-relative
	// BASENAMES. Descending from the base (not reopening the absolute resolvedParent)
	// keeps a post-matchAnyBase component swap from redirecting the dirfd (codex P1);
	// anchoring on the leaf (not the base) keeps the rename free of intermediate
	// components a post-create swap could follow at commit time (codex r3 P2).
	// matchAnyBase guarantees resolvedParent is under hitBase, so relParent never
	// starts with ".." ("." when the parent IS the base).
	relParent, relErr := filepath.Rel(hitBase, resolvedParent)
	if relErr != nil {
		return nil, fmt.Errorf("fsperm.OpenAtomicWriteValidated: rel(%s,%s): %w", hitBase, resolvedParent, relErr)
	}
	// belt-and-suspenders: the leaf must stay within hitBase.
	if relParent == ".." || strings.HasPrefix(relParent, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%w: leaf %s escapes base %s", ErrPathEscapesBase, resolvedParent, hitBase)
	}

	tmpBase := filepath.Base(tmpPath)
	targetBase := filepath.Base(targetPath)
	resolvedTmp := filepath.Join(resolvedParent, tmpBase)
	resolvedTarget := filepath.Join(resolvedParent, targetBase)

	h, err := openAtomicWrite(hitBase, relParent, tmpBase, targetBase, resolvedTmp, resolvedTarget)
	if err != nil {
		return nil, fmt.Errorf("fsperm.OpenAtomicWriteValidated: %w", err)
	}
	return h, nil
}

// Commit atomically renames the tmp file to the target, anchored under the same
// dirfd the tmp was created in (renameat(dirfd, tmpRel, dirfd, targetRel)) so
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
		_ = SyncParentDir(h.targetPath) //nolint:errcheck // best-effort dir durability
		h.done = true
		return nil
	}

	if err := renameatDir(h.dirfd, h.tmpRel, h.targetRel); err != nil {
		// dirfd stays open for the caller's deferred Abort to clean the tmp up.
		return fmt.Errorf("fsperm.AtomicWriteHandle.Commit: renameat(%s → %s): %w",
			h.tmpRel, h.targetRel, err)
	}
	_ = fsyncDir(h.dirfd) //nolint:errcheck // best-effort dir durability; rename 已成功
	h.closeDirfd()
	h.done = true
	return nil
}

// Abort removes the tmp file and closes the dirfd. It is best-effort and
// idempotent: on a committed handle it is a no-op (the tmp no longer exists — it
// was renamed to the target — and the dirfd is already closed). Errors removing
// the tmp are returned but the dirfd is always closed.
//
// On the dirfd path Abort uses unlinkat(dirfd, tmpRel); on the fallback path it
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

	err := unlinkatDir(h.dirfd, h.tmpRel)
	h.closeDirfd()
	if err != nil {
		return fmt.Errorf("fsperm.AtomicWriteHandle.Abort: unlinkat tmp %s: %w", h.tmpRel, err)
	}
	return nil
}

// SyncParentDir 開啟 path 的父目錄、fsync、close,讓先前的 rename/unlink crash-durable。
// best-effort:open/sync 失敗回 error 供 caller log,絕不讓(已成功的)寫入失敗。
//
// 內部委派 fsyncDir — 於 windows/other_unix 為 no-op,故在那些平台退化為 open+close
// (目錄 fsync 不可移植;rename 本身仍是 atomic)。比直接 d.Sync() 好:後者在 Windows
// 對目錄 handle 會 spurious 報錯。
func SyncParentDir(path string) error {
	dir := filepath.Dir(path)
	//nolint:gosec // G304: dir is filepath.Dir of caller-validated path
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("fsperm.SyncParentDir: open parent dir %s: %w", dir, err)
	}
	defer func() { _ = d.Close() }() //nolint:errcheck // best-effort

	if syncErr := fsyncDir(int(d.Fd())); syncErr != nil {
		return fmt.Errorf("fsperm.SyncParentDir: sync parent dir %s: %w", dir, syncErr)
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
