//go:build !windows

// Package fsperm internal test: pins the dirfd-anchor contract of the public
// OpenAtomicWriteValidated entry. Lives in the internal `fsperm` package (not
// `fsperm_test`) so it can read the unexported AtomicWriteHandle.tmpRel field to
// assert that the entry now anchors the dirfd on the validated BASE and addresses
// tmp/target by their path RELATIVE to that base (multi-component for a subdir
// target) — closing the parent-component swap TOCTOU (codex P1).
package fsperm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenAtomicWriteValidated_AnchorsSubdirTargetRelativeToBase 釘住 codex P1:
// 對位於 base 子目錄 (base/sub) 的 target,OpenAtomicWriteValidated 不可再把
// resolvedParent (= base/sub) 當 dirfd 錨點 + basename 定址,而必須錨定在 base 並以
// base-relative 多段路徑 (sub/out.csv.tmp.x) 定址 — 這樣 platform 層的
// openat2(RESOLVE_BENEATH|NO_SYMLINKS) / openat(O_NOFOLLOW_ANY) 才會在「受信任的
// base 之下」解析整段 rel path,杜絕 matchAnyBase 之後 parent component 被 swap 把
// dirfd 導向攻擊者目錄。
//
// 紅綠:修正前 entry 設 handle.tmpRel = "out.csv.tmp.x" (僅 basename,錨在 parent);
// 修正後設為 filepath.Join("sub","out.csv.tmp.x") (base-relative、多段)。本測試對前者
// 紅、對後者綠。
func TestOpenAtomicWriteValidated_AnchorsSubdirTargetRelativeToBase(t *testing.T) {
	t.Parallel()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(base): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(base, "sub"), 0o750); err != nil {
		t.Fatalf("setup: MkdirAll sub: %v", err)
	}

	target := filepath.Join(base, "sub", "out.csv")
	tmp := filepath.Join(base, "sub", "out.csv.tmp.x")

	h, err := OpenAtomicWriteValidated(target, tmp, []string{base})
	if err != nil {
		t.Fatalf("OpenAtomicWriteValidated(subdir target): %v", err)
	}

	// 核心斷言:tmpRel 必須是 base-relative 多段路徑,而非 parent-anchored basename。
	wantRel := filepath.Join("sub", "out.csv.tmp.x")
	if h.tmpRel != wantRel {
		t.Fatalf("handle.tmpRel = %q, want %q (entry 必須錨定 base 並以 base-relative 多段定址, codex P1)",
			h.tmpRel, wantRel)
	}
	wantTargetRel := filepath.Join("sub", "out.csv")
	if h.targetRel != wantTargetRel {
		t.Errorf("handle.targetRel = %q, want %q", h.targetRel, wantTargetRel)
	}

	// 完整 open → write → commit:檔案應落在 base/sub/out.csv。
	if _, err := h.File().Write([]byte("ok")); err != nil {
		t.Fatalf("write tmp: %v", err)
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
	if string(got) != "ok" {
		t.Errorf("committed content = %q, want %q", got, "ok")
	}
}
