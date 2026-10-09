package fsperm_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestIsWithin 釘住詞法包含判定的語義:等於 base 算內;`..foo` / `foo..bar` 是合法子項;
// `base/../x`、前綴相同但非子項的 `base2` 不算內;大小寫比對沿用 filepath.Rel 在該 OS 的行為。
func TestIsWithin(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "base")
	// 與 base 同層、名稱以 base 為前綴的兄弟目錄。
	sibling := base + "2"
	// 僅大小寫不同的 base:Windows 視為同一路徑,其餘 OS 為不同路徑。
	upperBase := filepath.Join(filepath.Dir(base), strings.ToUpper(filepath.Base(base)))

	tests := []struct {
		name   string
		base   string
		target string
		want   bool
	}{
		{"equal to base", base, base, true},
		{"equal after clean", base, base + string(filepath.Separator) + ".", true},
		{"child", base, filepath.Join(base, "a.csv"), true},
		{"deep child", base, filepath.Join(base, "a", "b", "c.csv"), true},
		{"child named ..foo", base, filepath.Join(base, "..foo"), true},
		{"child under ..foo dir", base, filepath.Join(base, "..foo", "x.csv"), true},
		{"child named foo..bar", base, filepath.Join(base, "foo..bar"), true},
		{"parent", base, filepath.Dir(base), false},
		{"dotdot escape (uncleaned)", base, base + string(filepath.Separator) + ".." + string(filepath.Separator) + "x", false},
		{"dotdot escape (joined)", base, filepath.Join(base, "..", "x"), false},
		{"prefix but not child", base, sibling, false},
		{"child of prefix sibling", base, filepath.Join(sibling, "a.csv"), false},
		{"unrelated", base, filepath.Join(t.TempDir(), "other"), false},
		{"case differs", base, filepath.Join(upperBase, "a.csv"), runtime.GOOS == "windows"},
		{"abs target vs relative base", "rel", filepath.Join(base, "a.csv"), false},
		{"relative both, child", "output", filepath.Join("output", "sub", "a.csv"), true},
		{"relative both, escape", "output", filepath.Join("output", "..", "x"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := fsperm.IsWithin(tc.base, tc.target); got != tc.want {
				t.Errorf("IsWithin(%q, %q) = %v, want %v", tc.base, tc.target, got, tc.want)
			}
		})
	}
}

// TestIsWithinResolved 釘住 symlink-aware 版:解析後比對、回傳解析後的 base、
// symlink 逸出 / 空路徑 fail-closed、尚未存在的尾段可判定。
func TestIsWithinResolved(t *testing.T) {
	t.Parallel()

	realBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	outside, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("equal to base", func(t *testing.T) {
		t.Parallel()
		got, ok := fsperm.IsWithinResolved(realBase, realBase)
		if !ok || got != realBase {
			t.Errorf("got (%q, %v), want (%q, true)", got, ok, realBase)
		}
	})

	t.Run("not-yet-existing child", func(t *testing.T) {
		t.Parallel()
		got, ok := fsperm.IsWithinResolved(realBase, filepath.Join(realBase, "new", "deep", "a.csv"))
		if !ok || got != realBase {
			t.Errorf("got (%q, %v), want (%q, true)", got, ok, realBase)
		}
	})

	t.Run("not-yet-existing base", func(t *testing.T) {
		t.Parallel()
		futureBase := filepath.Join(realBase, "future")
		got, ok := fsperm.IsWithinResolved(futureBase, filepath.Join(futureBase, "a.csv"))
		if !ok || got != futureBase {
			t.Errorf("got (%q, %v), want (%q, true)", got, ok, futureBase)
		}
	})

	t.Run("child named ..foo", func(t *testing.T) {
		t.Parallel()
		if _, ok := fsperm.IsWithinResolved(realBase, filepath.Join(realBase, "..foo")); !ok {
			t.Error("base/..foo 應在 base 內")
		}
	})

	t.Run("dotdot escape", func(t *testing.T) {
		t.Parallel()
		got, ok := fsperm.IsWithinResolved(realBase, filepath.Join(realBase, "..", "x"))
		if ok || got != "" {
			t.Errorf("got (%q, %v), want (\"\", false)", got, ok)
		}
	})

	t.Run("prefix but not child", func(t *testing.T) {
		t.Parallel()
		if _, ok := fsperm.IsWithinResolved(realBase, realBase+"2"); ok {
			t.Error("base2 不應在 base 內")
		}
	})

	t.Run("empty paths fail closed", func(t *testing.T) {
		t.Parallel()
		if _, ok := fsperm.IsWithinResolved("", realBase); ok {
			t.Error("空 base 應 fail-closed")
		}
		if _, ok := fsperm.IsWithinResolved(realBase, ""); ok {
			t.Error("空 target 應 fail-closed")
		}
	})

	t.Run("symlinked base resolves", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("Windows symlink 需要 admin 權限")
		}
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(realBase, link); err != nil {
			t.Fatal(err)
		}
		got, ok := fsperm.IsWithinResolved(link, filepath.Join(realBase, "a.csv"))
		if !ok || got != realBase {
			t.Errorf("got (%q, %v), want (%q, true)", got, ok, realBase)
		}
	})

	t.Run("symlink escaping base", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("Windows symlink 需要 admin 權限")
		}
		if err := os.Symlink(outside, filepath.Join(realBase, "escape")); err != nil {
			t.Fatal(err)
		}
		// 詞法上在 base 內,解析後逸出。
		target := filepath.Join(realBase, "escape", "a.csv")
		if !fsperm.IsWithin(realBase, target) {
			t.Fatal("前置條件:詞法上應在 base 內")
		}
		if got, ok := fsperm.IsWithinResolved(realBase, target); ok || got != "" {
			t.Errorf("got (%q, %v), want (\"\", false)", got, ok)
		}
	})
}
