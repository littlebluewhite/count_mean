//go:build !windows

package fsperm_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestOpenReadValidated_ParentSymlinkRejected 釘住 OpenReadValidated 對
// parent-component symlink-escape 的 reject 行為(base/link → outside,讀
// base/link/secret.txt 必須擋下)。寫入端對映為
// TestOpenAtomicWriteValidated_ParentSymlinkEscapesBase。
func TestOpenReadValidated_ParentSymlinkRejected(t *testing.T) {
	t.Parallel()

	allowedDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(allowedDir) failed: %v", err)
	}
	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(outsideDir) failed: %v", err)
	}

	// outside 放一個檔案,attacker 想透過 link 騙程式讀到
	if err := os.WriteFile(filepath.Join(outsideDir, "secret.txt"),
		[]byte("sensitive"), 0o600); err != nil {
		t.Fatalf("setup: WriteFile failed: %v", err)
	}

	linkPath := filepath.Join(allowedDir, "escape")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("setup: Symlink failed: %v", err)
	}

	attackPath := filepath.Join(linkPath, "secret.txt")
	f, openErr := fsperm.OpenReadValidated(attackPath, []string{allowedDir})
	if openErr == nil {
		_ = f.Close()
		t.Fatalf("parent-symlink-escape read 應拒,實際通過")
	}
	if !errors.Is(openErr, fsperm.ErrPathEscapesBase) {
		t.Errorf("expected ErrPathEscapesBase,got %v", openErr)
	}
}

// TestOpenReadValidated_HappyPath 釘住 sanity:base 之下的合法 path
// 應放行並真的讀到內容。
func TestOpenReadValidated_HappyPath(t *testing.T) {
	t.Parallel()

	baseDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks failed: %v", err)
	}

	target := filepath.Join(baseDir, "data.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o600); err != nil {
		t.Fatalf("setup: WriteFile failed: %v", err)
	}

	f, err := fsperm.OpenReadValidated(target, []string{baseDir})
	if err != nil {
		t.Fatalf("base 內合法 path 不該被拒,err=%v", err)
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, 5)
	n, err := f.Read(buf)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("expected 'hello',got %q", string(buf[:n]))
	}
}

// TestOpenReadValidated_EmptyBasePathsRejected 釘住 對稱契約:basePaths
// 空為 caller error。
func TestOpenReadValidated_EmptyBasePathsRejected(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "x.txt")

	_, err := fsperm.OpenReadValidated(target, nil)
	if err == nil {
		t.Fatalf("nil basePaths 應 reject,實際通過")
	}
	if !errors.Is(err, fsperm.ErrBasePathsEmpty) {
		t.Errorf("expected ErrBasePathsEmpty,got %v", err)
	}
}
