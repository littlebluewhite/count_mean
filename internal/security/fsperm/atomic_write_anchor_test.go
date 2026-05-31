//go:build !windows

// Package fsperm internal test: pins the dirfd-anchor contract of the public
// OpenAtomicWriteValidated entry. Lives in the internal `fsperm` package (not
// `fsperm_test`) so it can read the unexported AtomicWriteHandle.tmpRel/targetRel
// fields to assert that, for a subdir target, the entry anchors the dirfd on the
// target's VALIDATED LEAF PARENT (reached by descending from the base) and
// addresses tmp/target by their BASENAME — so the commit renameat traverses no
// intermediate component a post-create swap could follow (codex r3 P2 / ADR-0017
// Process-note pt14). The companion black-box test
// TestOpenAtomicWriteValidated_SubdirCommitSurvivesParentSwap pins the runtime
// security outcome; this one pins the internal representation.
package fsperm

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenAtomicWriteValidated_AnchorsSubdirTargetOnValidatedLeaf 釘住 codex r3 P2 的
// Option A 修法:對 base 子目錄 (base/sub) 的 target,OpenAtomicWriteValidated 把 dirfd
// 錨定在「從 base 下行解析出的 leaf 父目錄 (base/sub)」,並以 BASENAME 定址 tmp/target
// (tmpRel="out.csv.tmp.x"、targetRel="out.csv",而非 round-2 的多段 "sub/out.csv.tmp.x")
// —— 這樣 commit 的 renameat(leafDirfd, basename, leafDirfd, basename) 沒有中間 component
// 可被 swap 跟穿,且 leaf dirfd PIN 住 inode。完整 open → write → commit 仍落在
// base/sub/out.csv。
func TestOpenAtomicWriteValidated_AnchorsSubdirTargetOnValidatedLeaf(t *testing.T) {
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

	// 核心斷言:Option A 以 leaf 為 anchor,tmpRel/targetRel 是 BASENAME (單段),非
	// round-2 的 base-relative 多段。基名單段 → commit renameat 無中間 component 可 swap。
	if h.tmpRel != "out.csv.tmp.x" {
		t.Fatalf("handle.tmpRel = %q, want %q (Option A: leaf-anchored basename, codex r3 P2)",
			h.tmpRel, "out.csv.tmp.x")
	}
	if h.targetRel != "out.csv" {
		t.Errorf("handle.targetRel = %q, want %q", h.targetRel, "out.csv")
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
