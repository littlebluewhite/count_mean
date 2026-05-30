//go:build linux

// Package fsperm internal test: exercises the RESOLVE_NO_SYMLINKS edge for the
// dirfd-anchored atomic-write primitive (openAtomicWrite). Lives in the internal
// `fsperm` package (not `fsperm_test`) so it can call the unexported
// openAtomicWrite directly with a crafted multi-component tmpBase whose
// intermediate component is an in-base symlink — the only way to place a
// surviving symlink at the openat2 tmp-create boundary deterministically.
//
// Why the public OpenAtomicWriteValidated cannot exercise this: it runs
// evalSymlinksWithFallback(parent) FIRST and only the tmp/target *basenames*
// reach the platform openAtomicWrite, so the resolved parent never contains a
// symlink component by the time openat2 runs. To assert that RESOLVE_NO_SYMLINKS
// rejects a parent-component symlink that DOES survive to the openat2 syscall —
// the TOCTOU swap the flag exists to close — we hand openAtomicWrite a tmpBase
// of the form "linkdir/out.csv.tmp" where linkdir is an in-base symlink,
// simulating "a symlink was swapped in after EvalSymlinks". The PARENT component
// (not the leaf) isolates RESOLVE_NO_SYMLINKS: a leaf-symlink tmpBase would
// already be rejected by O_NOFOLLOW (from TmpCreateFlags) alone (see the
// secondary subtest), so it cannot prove the new flag. Deterministic, non-flaky.
package fsperm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// TestOpenAtomicWrite_RejectsSurvivingInBaseParentSymlink_Linux 釘住 ADR-0017
// Decision 6 的 atomic-write 安全契約:openAtomicWrite 的 openat2 tmp-create 必須帶
// RESOLVE_NO_SYMLINKS。模擬 parent-swap TOCTOU — 一個「在 evalSymlinksWithFallback
// 之後才被換進來、survive 到 openat2 那一步」的 in-base parent-component symlink —
// openat2 必須以 ELOOP 拒絕,openAtomicWrite 再 wrap 成 ErrPathEscapesBase。
//
// 構型:resolvedParent=base (dirfd 錨點),tmpBase="linkdir/out.csv.tmp" 其中
// base/linkdir → base/realdir (relative、in-base symlink)。
//   - target relative 留在 base 內 → RESOLVE_BENEATH 不先回 EXDEV;
//   - symlink 在中間 (linkdir) 不在 leaf → O_NOFOLLOW (TmpCreateFlags) 不適用;
//   - 於是 reject 完全來自 RESOLVE_NO_SYMLINKS → ELOOP。
//
// mutation:把 openAtomicWrite 的 Resolve 拿掉 RESOLVE_NO_SYMLINKS 後,在真實 Linux
// 上此 parent-symlink tmp-create 會成功 (kernel 跟 linkdir → realdir 建出 .tmp),
// 測試轉紅 — 證明本測例釘的正是該 flag,而非 RESOLVE_BENEATH 或 O_NOFOLLOW。
func TestOpenAtomicWrite_RejectsSurvivingInBaseParentSymlink_Linux(t *testing.T) {
	requireOpenat2(t)

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(base): %v", err)
	}

	// base/realdir 真實目錄;base/linkdir → realdir (relative、in-base)。
	realdir := filepath.Join(base, "realdir")
	if err := os.Mkdir(realdir, 0o750); err != nil {
		t.Fatalf("setup: Mkdir realdir: %v", err)
	}
	if err := os.Symlink("realdir", filepath.Join(base, "linkdir")); err != nil {
		t.Fatalf("setup: Symlink linkdir: %v", err)
	}

	// tmpBase 的中間 component (linkdir) 仍是 symlink — 模擬 EvalSymlinks 之後、
	// openat2 之前被 swap 進來的 parent symlink。targetBase 同樣以 linkdir/ 為前綴
	// (commit 不會走到,open 先被擋)。
	tmpBase := filepath.Join("linkdir", "out.csv.tmp.deadbeef")
	targetBase := filepath.Join("linkdir", "out.csv")
	tmpPath := filepath.Join(base, tmpBase)
	targetPath := filepath.Join(base, targetBase)

	h, err := openAtomicWrite(base, tmpBase, targetBase, tmpPath, targetPath)
	if err == nil {
		_ = h.Abort()
		t.Fatalf("openAtomicWrite: survive-to-openat2 的 in-base parent symlink 應被 " +
			"RESOLVE_NO_SYMLINKS 拒絕,實際建檔成功 (flag 漏 NO_SYMLINKS?)")
	}
	if !errors.Is(err, ErrPathEscapesBase) {
		t.Fatalf("openAtomicWrite: expected ErrPathEscapesBase (ELOOP 包裝), got %v", err)
	}
	if !errors.Is(err, unix.ELOOP) {
		t.Errorf("openAtomicWrite: 底層應為 ELOOP (RESOLVE_NO_SYMLINKS 違反), got %v", err)
	}
	// 確認沒有殘留 .tmp (open 失敗不該留下半成品)。
	if _, statErr := os.Lstat(filepath.Join(realdir, "out.csv.tmp.deadbeef")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("open 被拒後不該留下 .tmp, Lstat err = %v", statErr)
	}
}

// TestOpenAtomicWrite_LeafSymlinkRejectedByOExcl_Linux_Secondary 次要對照:tmpBase
// 的 leaf 為一個已存在的 symlink。與 validated_open (用 WriteFlags = O_CREATE|O_TRUNC)
// 不同,atomic tmp-create 用 TmpCreateFlags = O_CREATE|O_EXCL|O_NOFOLLOW — O_EXCL 對
// 「名稱已存在」一律先回 EEXIST,搶在 O_NOFOLLOW 判定 symlink 之前。攻擊者預先 plant
// 一個與 tmp 同名的 symlink 因此被 O_EXCL collision guard 擋下 (回 EEXIST 而非 ELOOP),
// 且 symlink target 不被寫穿 — 仍是安全拒絕,只是 errno 不同。
//
// 此 case 拔掉 RESOLVE_NO_SYMLINKS 仍綠 (O_EXCL 先回 EEXIST),故釘的是 O_EXCL 而非本
// commit 的 flag — 保留僅記錄 leaf 路徑同樣安全,非主 regression pin。真實 tmp 名含
// crypto/rand 後綴 (makeTmpPath),attacker 預植同名 symlink 機率極小;此測例證明即使
// 撞名也安全 fail。
func TestOpenAtomicWrite_LeafSymlinkRejectedByOExcl_Linux_Secondary(t *testing.T) {
	requireOpenat2(t)

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(base): %v", err)
	}
	// base/real.csv 真實檔;base/swapped → real.csv (已存在的 leaf symlink)。
	realTarget := filepath.Join(base, "real.csv")
	if err := os.WriteFile(realTarget, []byte("untouched"), 0o600); err != nil {
		t.Fatalf("setup: seed real target: %v", err)
	}
	if err := os.Symlink(realTarget, filepath.Join(base, "swapped")); err != nil {
		t.Fatalf("setup: Symlink leaf: %v", err)
	}

	// tmpBase = "swapped" (leaf 即已存在 symlink)。O_EXCL 應以 EEXIST 拒絕。
	tmpBase := "swapped"
	targetBase := "out.csv"
	h, err := openAtomicWrite(base, tmpBase, targetBase,
		filepath.Join(base, tmpBase), filepath.Join(base, targetBase))
	if err == nil {
		_ = h.Abort()
		t.Fatalf("openAtomicWrite: 已存在的 leaf symlink tmpBase 應被 O_EXCL 以 EEXIST 拒絕")
	}
	// O_EXCL 對已存在名稱回 EEXIST (搶在 O_NOFOLLOW 之前);非 escape,故不 wrap 成
	// ErrPathEscapesBase — 原始 errno 透傳。
	if !errors.Is(err, unix.EEXIST) {
		t.Errorf("openAtomicWrite: leaf symlink expected EEXIST (O_EXCL collision guard), got %v", err)
	}
	// 關鍵安全性:symlink target 不得被寫穿。
	got, readErr := os.ReadFile(realTarget) //nolint:gosec // test path
	if readErr != nil {
		t.Fatalf("verify: ReadFile: %v", readErr)
	}
	if string(got) != "untouched" {
		t.Errorf("symlink target 被寫穿了 (應安全拒絕), content = %q", got)
	}
}

// TestOpenAtomicWrite_AcceptsPreResolvedInBase_Linux 正向對照:證明 RESOLVE_NO_SYMLINKS
// 只擋 survive 到 openat2 的 symlink,不誤殺已被 evalSymlinksWithFallback 展開的合法
// in-base 路徑 (公開 API 真正餵進 openAtomicWrite 的就是這種 basename)。若此測試紅,
// 代表 flag 連合法路徑都擋,改動破壞既有契約。完整跑 open → write → commit。
func TestOpenAtomicWrite_AcceptsPreResolvedInBase_Linux(t *testing.T) {
	requireOpenat2(t)

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(base): %v", err)
	}
	tmpBase := "data.csv.tmp.cafef00d"
	targetBase := "data.csv"
	h, err := openAtomicWrite(base, tmpBase, targetBase,
		filepath.Join(base, tmpBase), filepath.Join(base, targetBase))
	if err != nil {
		t.Fatalf("openAtomicWrite: 已解析的合法 in-base 路徑不該被 NO_SYMLINKS 擋: %v", err)
	}
	if _, err := h.File().Write([]byte("ok")); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := h.File().Close(); err != nil {
		t.Fatalf("close tmp: %v", err)
	}
	if err := h.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(base, targetBase)) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read committed: %v", err)
	}
	if string(got) != "ok" {
		t.Errorf("committed content = %q, want %q", got, "ok")
	}
}
