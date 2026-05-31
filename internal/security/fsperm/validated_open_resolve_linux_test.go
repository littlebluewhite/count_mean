//go:build linux

// Package fsperm internal test: exercises the RESOLVE_NO_SYMLINKS edge that the
// public OpenWriteValidated / OpenReadValidated wrappers cannot reach. Lives in
// the internal `fsperm` package (not `fsperm_test`) so it can call the
// unexported openValidated / openReadValidated directly — the only way to place
// a surviving symlink at the openat2 boundary deterministically.
//
// Why the public API cannot exercise this: OpenWriteValidated runs
// evalSymlinksWithFallback(path) FIRST, which fully resolves every symlink
// component. By the time openat2 runs, a legal in-base symlink has already been
// expanded to its real target, so openat2 never sees a symlink. To assert that
// RESOLVE_NO_SYMLINKS rejects a symlink that DOES survive to the syscall — the
// TOCTOU swap that the flag exists to close — we must hand an unresolved path
// whose intermediate (parent) component is still an in-base symlink straight to
// openValidated, simulating "a symlink was swapped in after EvalSymlinks". The
// parent component (not the leaf) is what isolates RESOLVE_NO_SYMLINKS: the
// leaf-symlink case would already be rejected by O_NOFOLLOW alone (see the
// secondary subtest), so it cannot prove the new flag. This is deterministic
// (no racing goroutine, no timing) and non-flaky.
package fsperm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// requireOpenat2 skips on kernels (< 5.6 or LSM-blocked) where openat2 returns
// ENOSYS and openValidated falls back to os.OpenFile. The RESOLVE_NO_SYMLINKS
// guarantee is a property of the raw openat2 syscall; the fallback relies on
// O_NOFOLLOW (leaf-only) + caller-side EvalSymlinks, so the surviving-symlink
// edge below is only meaningful on the openat2 happy path.
func requireOpenat2(t *testing.T) {
	t.Helper()
	dirfd, err := unix.Open("/", unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("openat2 probe: open root dirfd failed: %v", err)
	}
	defer func() { _ = unix.Close(dirfd) }()

	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_BENEATH,
	}
	fd, err := unix.Openat2(dirfd, ".", how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) {
			t.Skip("openat2 not supported on this kernel (< 5.6 or LSM blocked); " +
				"RESOLVE_NO_SYMLINKS edge cannot be exercised on the os.OpenFile fallback path")
		}
		t.Fatalf("openat2 probe failed unexpectedly: %v", err)
	}
	_ = unix.Close(fd)
}

// TestOpenValidated_RejectsSurvivingInBaseSymlink_Linux 釘住 ADR-0017 Decision 7:
// openat2 的 Resolve 必須含 RESOLVE_NO_SYMLINKS。模擬 TOCTOU swap — 一個「在
// evalSymlinksWithFallback 之後才被換進來、survive 到 openat2 那一步」的 in-base
// symlink — openat2 必須以 ELOOP 拒絕,openValidated / openReadValidated 再 wrap
// 成 ErrPathEscapesBase。
//
// 為何主測例用「中間 (parent) component 為 symlink、且 target relative+in-base」:
// 這是唯一能單獨釘住 RESOLVE_NO_SYMLINKS 的構型 —
//   - target relative 留在 base 內 → RESOLVE_BENEATH 不會先回 EXDEV;
//   - symlink 不在 leaf → ReadFlags/WriteFlags 自帶的 O_NOFOLLOW 不適用 (它只擋
//     leaf component);
//   - 於是 reject 完全來自 RESOLVE_NO_SYMLINKS → ELOOP。
//
// 這點經 mutation 驗證:把兩個 openat2 site 的 RESOLVE_NO_SYMLINKS 拔掉後,在真實
// Linux 上此 parent-symlink open 會成功 (kernel 跟著 linkdir → realdir 走到 leaf),
// 測試轉紅。若改用 leaf symlink,O_NOFOLLOW 自己就會以 ELOOP 拒絕,拔 flag 仍綠 —
// 那種測例釘的是 O_NOFOLLOW,不是本 commit 新增的 flag,故僅留作下方次要對照。
//
// symlink (linkdir) 與其 target (realdir) 都在 base 內,證明 reject 的原因是
// 「中間 component 仍是 symlink」而非「逃出 base」(逃出 base 會是 EXDEV)。
func TestOpenValidated_RejectsSurvivingInBaseSymlink_Linux(t *testing.T) {
	requireOpenat2(t)

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(base): %v", err)
	}

	// base/realdir/leaf.csv 真實檔 (leaf 必須存在,否則「拔 flag」對照組會因檔不存在
	// 而 fail,無法證明 flag 帶來的行為轉變);base/linkdir → realdir (relative、in-base
	// symlink)。最終開 base/linkdir/leaf.csv:中間 component linkdir 是 symlink,leaf 不是。
	realdir := filepath.Join(base, "realdir")
	if err := os.Mkdir(realdir, 0o755); err != nil {
		t.Fatalf("setup: Mkdir realdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(realdir, "leaf.csv"), []byte("payload"), 0o600); err != nil {
		t.Fatalf("setup: seed leaf: %v", err)
	}
	if err := os.Symlink("realdir", filepath.Join(base, "linkdir")); err != nil {
		t.Fatalf("setup: Symlink linkdir: %v", err)
	}
	// 把「中間 component 仍是 symlink」的 unresolved path 直接餵給 unexported open
	// 函式,模擬 EvalSymlinks 之後 openat2 之前被 swap 進來的 parent symlink。
	parentSymlinkPath := filepath.Join(base, "linkdir", "leaf.csv")

	t.Run("write", func(t *testing.T) {
		f, err := openValidated(parentSymlinkPath, base)
		if err == nil {
			_ = f.Close()
			t.Fatalf("openValidated: survive-to-openat2 的 in-base parent symlink 應被 " +
				"RESOLVE_NO_SYMLINKS 拒絕,實際開成功 (flag 漏 NO_SYMLINKS?)")
		}
		if !errors.Is(err, ErrPathEscapesBase) {
			t.Fatalf("openValidated: expected ErrPathEscapesBase (ELOOP 包裝), got %v", err)
		}
		if !errors.Is(err, unix.ELOOP) {
			t.Errorf("openValidated: 底層應為 ELOOP (RESOLVE_NO_SYMLINKS 違反), got %v", err)
		}
	})

	t.Run("read", func(t *testing.T) {
		f, err := openReadValidated(parentSymlinkPath, base)
		if err == nil {
			_ = f.Close()
			t.Fatalf("openReadValidated: survive-to-openat2 的 in-base parent symlink 應被 " +
				"RESOLVE_NO_SYMLINKS 拒絕,實際開成功 (read 端 flag 漏 NO_SYMLINKS?)")
		}
		if !errors.Is(err, ErrPathEscapesBase) {
			t.Fatalf("openReadValidated: expected ErrPathEscapesBase (ELOOP 包裝), got %v", err)
		}
		if !errors.Is(err, unix.ELOOP) {
			t.Errorf("openReadValidated: 底層應為 ELOOP (RESOLVE_NO_SYMLINKS 違反), got %v", err)
		}
	})

	// 次要對照:leaf component 為 symlink。此 case 即使拔掉 RESOLVE_NO_SYMLINKS 也會
	// 被 O_NOFOLLOW (來自 ReadFlags/WriteFlags) 以 ELOOP 拒絕,故它釘的是 O_NOFOLLOW,
	// 不是本 commit 的 flag。保留僅為記錄 leaf 路徑同樣被擋,非主 regression pin。
	t.Run("leaf_symlink_O_NOFOLLOW_secondary", func(t *testing.T) {
		realTarget := filepath.Join(base, "real.csv")
		if err := os.WriteFile(realTarget, []byte("payload"), 0o600); err != nil {
			t.Fatalf("setup: seed real target: %v", err)
		}
		leafSwapped := filepath.Join(base, "swapped")
		if err := os.Symlink(realTarget, leafSwapped); err != nil {
			t.Fatalf("setup: Symlink leaf: %v", err)
		}
		f, err := openValidated(leafSwapped, base)
		if err == nil {
			_ = f.Close()
			t.Fatalf("openValidated: leaf symlink 應被 O_NOFOLLOW 以 ELOOP 拒絕,實際開成功")
		}
		if !errors.Is(err, ErrPathEscapesBase) || !errors.Is(err, unix.ELOOP) {
			t.Errorf("openValidated: leaf symlink expected ErrPathEscapesBase+ELOOP, got %v", err)
		}
	})
}

// TestOpenValidated_AcceptsPreResolvedInBasePath_Linux 是上一個測試的正向對照:
// 證明 RESOLVE_NO_SYMLINKS 只擋「survive 到 openat2 的 symlink」,不誤殺已被
// evalSymlinksWithFallback 展開成 real path 的合法 in-base 路徑 (公開 API 真正
// 餵進 openValidated 的就是這種已解析路徑)。若此測試紅,代表新 flag 連合法路徑
// 都擋,改動就破壞了既有契約。
func TestOpenValidated_AcceptsPreResolvedInBasePath_Linux(t *testing.T) {
	requireOpenat2(t)

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(base): %v", err)
	}
	// real 非 symlink 路徑 — 等同 EvalSymlinks 後的產物。
	realTarget := filepath.Join(base, "data.csv")

	t.Run("write", func(t *testing.T) {
		f, err := openValidated(realTarget, base)
		if err != nil {
			t.Fatalf("openValidated: 已解析的合法 in-base 路徑不該被 NO_SYMLINKS 擋, got %v", err)
		}
		_ = f.Close()
		_ = os.Remove(realTarget)
	})

	t.Run("read", func(t *testing.T) {
		if err := os.WriteFile(realTarget, []byte("seed"), 0o600); err != nil {
			t.Fatalf("setup: WriteFile: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(realTarget) })
		f, err := openReadValidated(realTarget, base)
		if err != nil {
			t.Fatalf("openReadValidated: 已解析的合法 in-base 路徑不該被 NO_SYMLINKS 擋, got %v", err)
		}
		_ = f.Close()
	})
}
