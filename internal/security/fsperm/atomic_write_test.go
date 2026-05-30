//go:build !windows

package fsperm_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/security/fsperm"
)

// tmpFor 產出與 target 同目錄、basename 後綴固定的 tmp 路徑 — 模擬
// csvutil.makeTmpPath 的「同 dir、改 basename」契約 (本測試不需要 crypto 隨機,
// 固定後綴即可,primitive 不重算 tmp)。
func tmpFor(target string) string {
	return target + ".tmp.deadbeefcafef00d"
}

// TestOpenAtomicWriteValidated_HappyPath 釘住 open → write → sync → close →
// commit 的完整流程:commit 後 target 存在且內容正確、tmp 消失。在 darwin host 上
// 這條走 openat + O_NOFOLLOW_ANY + renameat 的 dirfd 錨定路徑 (Linux 由 Docker
// 另測 openat2)。
func TestOpenAtomicWriteValidated_HappyPath(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target := filepath.Join(base, "out.csv")
	tmp := tmpFor(target)

	h, err := fsperm.OpenAtomicWriteValidated(target, tmp, []string{base})
	if err != nil {
		t.Fatalf("OpenAtomicWriteValidated: %v", err)
	}

	payload := []byte("col1,col2\na,b\n")
	if _, err := h.File().Write(payload); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := h.File().Sync(); err != nil {
		t.Fatalf("sync tmp: %v", err)
	}
	if err := h.File().Close(); err != nil {
		t.Fatalf("close tmp: %v", err)
	}
	if err := h.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	got, err := os.ReadFile(target) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read committed target: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("target content = %q, want %q", got, payload)
	}
	if _, err := os.Lstat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("tmp 應在 commit 後消失 (rename 走掉), Lstat err = %v", err)
	}

	// commit 後 mode 應為 FilePerm (0600) — openat 帶 FilePerm,rename 不改 mode。
	fi, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != fsperm.FilePerm {
		t.Errorf("target mode = %o, want %o", perm, fsperm.FilePerm)
	}
}

// TestOpenAtomicWriteValidated_Abort 釘住 open → abort:tmp 被刪、target 不存在,
// 且 abort 後再 abort/commit 為 no-op (idempotent)。
func TestOpenAtomicWriteValidated_Abort(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target := filepath.Join(base, "out.csv")
	tmp := tmpFor(target)

	h, err := fsperm.OpenAtomicWriteValidated(target, tmp, []string{base})
	if err != nil {
		t.Fatalf("OpenAtomicWriteValidated: %v", err)
	}

	// tmp 此刻應存在 (open 已建立)。
	if _, err := os.Lstat(tmp); err != nil {
		t.Fatalf("tmp 應在 open 後存在, Lstat err = %v", err)
	}
	// 寫一點再 close,模擬中途放棄。
	if _, err := h.File().Write([]byte("partial")); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := h.File().Close(); err != nil {
		t.Fatalf("close tmp: %v", err)
	}

	if err := h.Abort(); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if _, err := os.Lstat(tmp); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("tmp 應在 abort 後消失, Lstat err = %v", err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("target 不該存在 (從未 commit), Lstat err = %v", err)
	}

	// idempotent:再 Abort / Commit 皆 no-op nil。
	if err := h.Abort(); err != nil {
		t.Errorf("second Abort 應為 no-op nil, got %v", err)
	}
	if err := h.Commit(); err != nil {
		t.Errorf("Commit after Abort 應為 no-op nil, got %v", err)
	}
}

// TestOpenAtomicWriteValidated_CommitThenAbortNoop 釘住 commit 後 Abort 為 no-op
// (committed handle 的 tmp 已 rename 走、dirfd 已關)。defer abort-on-error pattern
// 仰賴此性質。
func TestOpenAtomicWriteValidated_CommitThenAbortNoop(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target := filepath.Join(base, "out.csv")
	tmp := tmpFor(target)

	h, err := fsperm.OpenAtomicWriteValidated(target, tmp, []string{base})
	if err != nil {
		t.Fatalf("OpenAtomicWriteValidated: %v", err)
	}
	if err := h.File().Close(); err != nil {
		t.Fatalf("close tmp: %v", err)
	}
	if err := h.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// commit 後 Abort 必須 no-op,且不得誤刪已 commit 的 target。
	if err := h.Abort(); err != nil {
		t.Errorf("Abort after Commit 應為 no-op nil, got %v", err)
	}
	if _, err := os.Lstat(target); err != nil {
		t.Errorf("target 應仍存在 (Abort 不得碰已 commit 的檔), Lstat err = %v", err)
	}
}

// TestOpenAtomicWriteValidated_RejectsEmptyBasePaths 釘住 empty basePaths →
// ErrBasePathsEmpty (與 OpenWriteValidated 對稱,拒絕 silent 退化)。
func TestOpenAtomicWriteValidated_RejectsEmptyBasePaths(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target := filepath.Join(base, "out.csv")
	tmp := tmpFor(target)

	for _, bp := range [][]string{nil, {}} {
		h, err := fsperm.OpenAtomicWriteValidated(target, tmp, bp)
		if err == nil {
			_ = h.Abort()
			t.Fatalf("empty basePaths %v 應拒絕", bp)
		}
		if !errors.Is(err, fsperm.ErrBasePathsEmpty) {
			t.Errorf("expected ErrBasePathsEmpty, got %v", err)
		}
	}
}

// TestOpenAtomicWriteValidated_ParentEscapesBase 釘住 target 父目錄不在任何 base
// 之下 → ErrPathEscapesBase。
func TestOpenAtomicWriteValidated_ParentEscapesBase(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	outside := t.TempDir() // 另一個 temp dir,不在 base 下
	target := filepath.Join(outside, "out.csv")
	tmp := tmpFor(target)

	h, err := fsperm.OpenAtomicWriteValidated(target, tmp, []string{base})
	if err == nil {
		_ = h.Abort()
		t.Fatalf("父目錄在 base 外應被拒")
	}
	if !errors.Is(err, fsperm.ErrPathEscapesBase) {
		t.Errorf("expected ErrPathEscapesBase, got %v", err)
	}
}

// TestOpenAtomicWriteValidated_ParentSymlinkEscapesBase 釘住:base 內 symlink 指向
// base 外,透過 base/link/out.csv 寫 → resolvedParent 落在 base 外 → ErrPathEscapesBase。
// 這是 atomic-write 對 OpenWriteValidated symlink-escape 測試的對映。
func TestOpenAtomicWriteValidated_ParentSymlinkEscapesBase(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(base, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("setup: Symlink: %v", err)
	}

	target := filepath.Join(link, "out.csv")
	tmp := tmpFor(target)
	h, err := fsperm.OpenAtomicWriteValidated(target, tmp, []string{base})
	if err == nil {
		_ = h.Abort()
		t.Fatalf("symlink-escape 父目錄應被拒")
	}
	if !errors.Is(err, fsperm.ErrPathEscapesBase) {
		t.Errorf("expected ErrPathEscapesBase, got %v", err)
	}
}

// TestOpenAtomicWriteValidated_RejectsTmpTargetDirMismatch 釘住 primitive 的前提:
// tmp 與 target 必須同目錄 (makeTmpPath 保證) — 不同目錄應回明確錯誤,不靜默接受。
func TestOpenAtomicWriteValidated_RejectsTmpTargetDirMismatch(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatalf("setup: Mkdir sub: %v", err)
	}
	target := filepath.Join(base, "out.csv")
	tmp := filepath.Join(sub, "out.csv.tmp.deadbeef") // 不同目錄

	h, err := fsperm.OpenAtomicWriteValidated(target, tmp, []string{base})
	if err == nil {
		_ = h.Abort()
		t.Fatalf("tmp/target 不同目錄應被拒")
	}
	// 非 sentinel,但訊息應提到目錄不一致 — 至少確認有錯且非 panic。
	if errors.Is(err, fsperm.ErrBasePathsEmpty) || errors.Is(err, fsperm.ErrPathEscapesBase) {
		t.Errorf("dir-mismatch 不該誤判成 base 相關錯誤, got %v", err)
	}
}

// TestOpenAtomicWriteValidated_AcceptsInternalSymlinkParent 釘住 sanity:base 內
// symlink 指向同 base 下另一真實目錄,透過它寫應放行 (resolvedParent 仍在 base)。
// commit 後檔案落在 real 目錄。
func TestOpenAtomicWriteValidated_AcceptsInternalSymlinkParent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	if err := os.Mkdir(realDir, 0o750); err != nil {
		t.Fatalf("setup: Mkdir real: %v", err)
	}
	if err := os.Symlink(realDir, filepath.Join(base, "link")); err != nil {
		t.Fatalf("setup: Symlink: %v", err)
	}

	target := filepath.Join(base, "link", "out.csv")
	tmp := tmpFor(target)
	h, err := fsperm.OpenAtomicWriteValidated(target, tmp, []string{base})
	if err != nil {
		t.Fatalf("base 內部 symlink 父目錄應放行: %v", err)
	}
	if _, err := h.File().Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := h.File().Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := h.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	// 檔案應在 real 目錄 (symlink 解析後)。
	if _, err := os.Stat(filepath.Join(realDir, "out.csv")); err != nil {
		t.Errorf("commit 後檔案應落在 real 目錄: %v", err)
	}
}
