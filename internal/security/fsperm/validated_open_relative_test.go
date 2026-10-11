//go:build !windows

package fsperm_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestOpenValidated_RelativePathAbsoluteBase 釘住 OpenReadValidated 入口絕對化:預設
// config 的相對目錄(如 "./output")會讓 caller 傳相對 path,而 basePaths 已被
// NewPathValidator abs-ify。各平台必須一致:相對 path 在 abs base 之下 → 開到正確檔案;
// 逸出 → ErrPathEscapesBase。
// 非 parallel:t.Chdir 改 process CWD。
func TestOpenValidated_RelativePathAbsoluteBase(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks: %v", err)
	}
	t.Chdir(tmp)
	if err := os.Mkdir("output", 0o750); err != nil {
		t.Fatalf("setup: Mkdir: %v", err)
	}
	absBase, err := filepath.Abs("output")
	if err != nil {
		t.Fatalf("setup: Abs: %v", err)
	}
	bases := []string{absBase}

	t.Run("read accepted", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(absBase, "r.csv"), []byte("r"), 0o600); err != nil {
			t.Fatalf("setup: %v", err)
		}
		f, err := fsperm.OpenReadValidated(filepath.Join("output", "r.csv"), bases)
		if err != nil {
			t.Fatalf("相對 path 在 abs base 下應被接受: %v", err)
		}
		defer f.Close()
		buf := make([]byte, 4)
		n, _ := f.Read(buf)
		if string(buf[:n]) != "r" {
			t.Errorf("read %q, want r", buf[:n])
		}
	})

	t.Run("escape rejected", func(t *testing.T) {
		rel := filepath.Join("output", "..", "evil.csv")
		if _, err := fsperm.OpenReadValidated(rel, bases); !errors.Is(err, fsperm.ErrPathEscapesBase) {
			t.Errorf("read: want ErrPathEscapesBase, got %v", err)
		}
	})
}
