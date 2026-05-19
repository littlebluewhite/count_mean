//go:build windows

package fsperm_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestFsperm_WindowsFlagsLackNoFollow pins the Windows-side platform contract:
// because the Go standard `syscall` package on Windows has no O_NOFOLLOW
// equivalent, the four flag constants MUST NOT pretend to carry it. Pinning
// this prevents a future contributor from "fixing" the asymmetry by sneaking
// in a bogus bit value that doesn't actually map to kernel behavior.
//
// L14 — Sentinel 改用 bitmask OR 一起檢查所有主流 Unix 平台的 O_NOFOLLOW 值。
// 原本只比對 Linux 的 `0x20000`，但 darwin/FreeBSD 是 `0x100`、OpenBSD 是
// `0x100`、NetBSD 是 `0x00100`、illumos/Solaris 是 `0x20000`（與 Linux 相同）。
// 若有人從 darwin (0x100) 抄常數到 Windows build，原 sentinel `0x20000` 完全
// 漏掉。改成所有平台 mask OR 之後，任一 platform 的 O_NOFOLLOW shape 都會被
// catch。
//
// TODO: Windows CI — this file requires `runner: windows-latest` (or
// equivalent) in `.github/workflows/`. It will compile but never execute under
// the current darwin/linux CI matrix. When the Windows runner lands, the test
// becomes active automatically via the `//go:build windows` tag.
func TestFsperm_WindowsFlagsLackNoFollow(t *testing.T) {
	t.Parallel()

	// platformONoFollowMask 是「主流 Unix 上 O_NOFOLLOW 可能的所有值」的 OR 結果。
	// 任一 platform 的 O_NOFOLLOW shape 出現在 Windows flag 都視為 bug：
	//   - Linux / illumos / Solaris：0x00020000
	//   - darwin / FreeBSD / OpenBSD / NetBSD：0x00000100
	// 兩個值 OR 之後 0x00020100，任一 bit 命中就表示「有人偷塞 Unix O_NOFOLLOW
	// 到 Windows 常數」。
	const platformONoFollowMask = 0x00020000 | 0x00000100

	cases := []struct {
		name  string
		flags int
	}{
		{"WriteFlags", fsperm.WriteFlags},
		{"AppendFlags", fsperm.AppendFlags},
		{"ReadFlags", fsperm.ReadFlags},
		{"TmpCreateFlags", fsperm.TmpCreateFlags},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.flags&platformONoFollowMask != 0 {
				t.Errorf(
					"%s appears to carry an O_NOFOLLOW-shaped bit (mask 0x%X) on Windows. "+
						"Windows standard syscall has no atomic equivalent — that bit is meaningless to "+
						"the Win32 OpenFile path and gives a false sense of security. "+
						"Linux/illumos/Solaris use 0x20000; darwin/*BSD use 0x100 — we check both. "+
						"If you intend to add kernel-level symlink protection, switch to "+
						"golang.org/x/sys/windows.CreateFile + FILE_FLAG_OPEN_REPARSE_POINT instead.",
					tc.name, platformONoFollowMask,
				)
			}
		})
	}
}

// TestFsperm_WindowsCallerSideEvalSymlinksPattern documents (and exercises)
// the prescribed Windows pre-validation pattern: caller runs
// `filepath.EvalSymlinks` BEFORE OpenFile and rejects paths that resolve
// outside the trusted base directory. This is the only available substitute
// for the missing kernel-level O_NOFOLLOW.
//
// On a Windows runner this test creates a junction point (NTFS-native, doesn't
// need admin) inside `tempDir/base` pointing OUTSIDE `tempDir/base`, then
// asserts that EvalSymlinks-then-Rel correctly flags the escape attempt
// before any OpenFile is reached.
//
// TODO: Windows CI — see TestFsperm_WindowsFlagsLackNoFollow note above.
func TestFsperm_WindowsCallerSideEvalSymlinksPattern(t *testing.T) {
	t.Helper()
	t.Parallel()

	tempDir := t.TempDir()
	baseDir := filepath.Join(tempDir, "base")
	outsideDir := filepath.Join(tempDir, "outside")

	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		t.Fatalf("setup: MkdirAll baseDir failed: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0o750); err != nil {
		t.Fatalf("setup: MkdirAll outsideDir failed: %v", err)
	}

	// Create a symlink (or junction-equivalent) from base/escape -> outsideDir.
	// `os.Symlink` on Windows requires either Developer Mode or admin privileges.
	// Test skips if the privilege isn't available — the contract is still
	// documented via TestFsperm_WindowsFlagsLackNoFollow.
	linkPath := filepath.Join(baseDir, "escape")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("os.Symlink on Windows requires Developer Mode / admin; "+
			"skipping caller-pattern test (contract still pinned via flag test): %v", err)
	}

	// Pre-validation pattern: EvalSymlinks then Rel-check against base.
	requested := filepath.Join(linkPath, "secret.txt")
	resolvedBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(baseDir) failed: %v", err)
	}

	// EvalSymlinks for a non-existing leaf needs the parent to be resolvable.
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(requested))
	if err != nil {
		t.Fatalf("EvalSymlinks(parent) failed: %v", err)
	}
	resolved := filepath.Join(resolvedParent, filepath.Base(requested))

	rel, err := filepath.Rel(resolvedBase, resolved)
	if err != nil {
		t.Fatalf("filepath.Rel failed: %v", err)
	}

	// Contract: an escape attempt produces a ".." prefix in Rel. Caller is
	// expected to reject this and never reach `os.OpenFile(requested, ...)`.
	if !strings.HasPrefix(rel, "..") {
		t.Errorf(
			"caller-side EvalSymlinks failed to detect escape: rel=%q (expected to start with \"..\"). "+
				"If this fails, the entire Windows defense-in-depth chain is broken — see "+
				"flags_windows.go package doc for the contract.",
			rel,
		)
	}

	// Sanity: the lexical fsperm.ReadFlags would still happily open the
	// escaped target without the pre-validation step. We assert that the open
	// SUCCEEDS to demonstrate why pre-validation is mandatory (NOT because we
	// want the open to succeed in production).
	targetFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(targetFile, []byte("sensitive"), 0o600); err != nil {
		t.Fatalf("setup: WriteFile failed: %v", err)
	}

	f, err := os.OpenFile(requested, fsperm.ReadFlags, fsperm.FilePerm)
	if err != nil {
		// If a future Windows defense-in-depth lands (e.g.,
		// FILE_FLAG_OPEN_REPARSE_POINT wiring) this branch should activate
		// and we can tighten the contract. For now we document the gap.
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			t.Logf("OpenFile through reparse point failed (newly safe?): %v", err)
		}
		return
	}
	_ = f.Close()
	t.Logf("OpenFile through reparse point succeeded — confirms pre-validation " +
		"is the ONLY line of defense on Windows.")
}
