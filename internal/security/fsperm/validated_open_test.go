//go:build !windows

package fsperm_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestOpenWriteValidated_RejectsEmptyBasePaths 釘住 basePaths empty 視為
// caller error，silent 退化到「無 base 守門」會把 wrapper 設計打掉。
func TestOpenWriteValidated_RejectsEmptyBasePaths(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "ok.txt")

	cases := [][]string{
		nil,
		{},
		{""},
		{"  "},
	}

	for _, bp := range cases {
		t.Run(toCaseName(bp), func(t *testing.T) {
			t.Parallel()
			f, err := fsperm.OpenWriteValidated(target, bp)
			if err == nil {
				_ = f.Close()
				_ = os.Remove(target)
				t.Fatalf("empty basePaths %v 應拒絕，實際通過", bp)
			}
			if !errors.Is(err, fsperm.ErrBasePathsEmpty) {
				// {""} / {"  "} 通過第一道 len check 但無有效 base，會走 EscapesBase
				if !errors.Is(err, fsperm.ErrPathEscapesBase) {
					t.Errorf("expected ErrBasePathsEmpty 或 ErrPathEscapesBase，got %v", err)
				}
			}
		})
	}
}

func toCaseName(bp []string) string {
	if bp == nil {
		return "nil"
	}
	if len(bp) == 0 {
		return "empty"
	}
	return "blank-strings"
}

// TestOpenWriteValidated_AllowsPathInBase 釘住 happy path：path 在 base 之下
// 應成功 OpenFile 並寫入。
func TestOpenWriteValidated_AllowsPathInBase(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "data.csv")

	f, err := fsperm.OpenWriteValidated(target, []string{tempDir})
	if err != nil {
		t.Fatalf("base 之下的合法 path 不該被拒，err=%v", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// TestOpenWriteValidated_RejectsPathOutsideBase 釘住 boundary check：
// path 落在 base 之外應 reject。
func TestOpenWriteValidated_RejectsPathOutsideBase(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	outsideDir := t.TempDir() // 不同的 temp dir，不在 baseDir 之下
	outsidePath := filepath.Join(outsideDir, "escape.csv")

	f, err := fsperm.OpenWriteValidated(outsidePath, []string{baseDir})
	if err == nil {
		_ = f.Close()
		_ = os.Remove(outsidePath)
		t.Fatalf("base 之外的 path 應被拒，實際通過")
	}
	if !errors.Is(err, fsperm.ErrPathEscapesBase) {
		t.Errorf("expected ErrPathEscapesBase，got %v", err)
	}
}

// TestOpenWriteValidated_RejectsSymlinkEscapingBase 釘住 核心安全契約：
// base 之下的 symlink 指向 base 之外，OpenWriteValidated 必須擋下。即使
// O_NOFOLLOW (Unix) 已能擋「最終 component 是 symlink」，本 wrapper 也守住
// 「parent 是 symlink」這條 Windows/Unix 共同的攻擊面（EvalSymlinks 完整解析）。
func TestOpenWriteValidated_RejectsSymlinkEscapingBase(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	outsideDir := t.TempDir()

	// base/link → outsideDir
	linkPath := filepath.Join(baseDir, "link")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("setup: Symlink failed: %v", err)
	}

	// 攻擊：嘗試透過 base/link/escape.csv 寫到 outsideDir 內
	attackPath := filepath.Join(linkPath, "escape.csv")
	f, err := fsperm.OpenWriteValidated(attackPath, []string{baseDir})
	if err == nil {
		_ = f.Close()
		_ = os.Remove(attackPath)
		t.Fatalf("symlink-escape 路徑應被拒，實際通過")
	}
	if !errors.Is(err, fsperm.ErrPathEscapesBase) {
		t.Errorf("expected ErrPathEscapesBase，got %v", err)
	}
}

// TestOpenWriteValidated_AcceptsInternalSymlink 釘住 sanity：base 內部
// symlink（指向同 base 下另一個目錄）應放行。
func TestOpenWriteValidated_AcceptsInternalSymlink(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// base/real 是真實目錄，base/link → base/real
	realDir := filepath.Join(baseDir, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("setup: MkdirAll failed: %v", err)
	}
	if err := os.Symlink(realDir, filepath.Join(baseDir, "link")); err != nil {
		t.Fatalf("setup: Symlink failed: %v", err)
	}

	// 透過 link 寫到 real 內 — 解析後仍在 base，應放行
	target := filepath.Join(baseDir, "link", "data.csv")
	f, err := fsperm.OpenWriteValidated(target, []string{baseDir})
	if err != nil {
		t.Fatalf("base 內部 symlink 應放行，err=%v", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write([]byte("internal")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
}

// TestOpenWriteValidated_AcceptsNonExistentPathInBase 釘住 path 尚未存在
// 但 parent 在 base 之下，wrapper 仍應放行（典型 case：first write 還沒建檔）。
func TestOpenWriteValidated_AcceptsNonExistentPathInBase(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()
	subDir := filepath.Join(baseDir, "newsub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("setup: MkdirAll failed: %v", err)
	}

	target := filepath.Join(subDir, "not_yet_existing.csv")
	f, err := fsperm.OpenWriteValidated(target, []string{baseDir})
	if err != nil {
		t.Fatalf("尚未存在的 path（parent 在 base 內）應放行，err=%v", err)
	}
	defer func() { _ = f.Close() }()
}

// TestOpenWriteValidated_MultipleBasePaths 釘住 basePaths 可有多個，至少
// 一個命中即放行。常見場景：caller 同時允許 InputDir 與 OutputDir。
func TestOpenWriteValidated_MultipleBasePaths(t *testing.T) {
	t.Parallel()

	base1 := t.TempDir()
	base2 := t.TempDir()
	outside := t.TempDir()

	// 在 base2 寫 — 應放行（base2 命中）
	target := filepath.Join(base2, "data.csv")
	f, err := fsperm.OpenWriteValidated(target, []string{base1, base2})
	if err != nil {
		t.Fatalf("base2 之下 path 不該被拒（即使 base1 不命中），err=%v", err)
	}
	_ = f.Close()

	// 在 outside 寫 — 兩個 base 都不命中應拒
	outPath := filepath.Join(outside, "escape.csv")
	f, err = fsperm.OpenWriteValidated(outPath, []string{base1, base2})
	if err == nil {
		_ = f.Close()
		_ = os.Remove(outPath)
		t.Fatalf("outside path 兩個 base 都不命中應拒，實際通過")
	}
}
