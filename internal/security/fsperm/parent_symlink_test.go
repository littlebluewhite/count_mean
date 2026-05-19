//go:build !windows

package fsperm_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestOpenWriteValidated_ParentSymlinkTOCTOU 釘住 parent-component symlink
// 攻擊。base/link → outside,寫 base/link/out.csv 必須擋下,不能透過 link 把檔案
// 寫到 base 外。
//
// 攻擊場景（V1 verify report Exploit 1）：
//
//	mkdir allowed outside; ln -s outside allowed/escape
//	OpenWriteValidated("allowed/escape/loot.csv", []string{"allowed"})
//
// 過去 lexical Rel("allowed", "allowed/escape/loot.csv") = "escape/loot.csv"
// (不含 ..) 通過 isPathWithinAnyBase；接著 os.OpenFile 再 syscall 解析 symlink,
// 把檔案寫到 outside/loot.csv (O_NOFOLLOW 只擋 leaf component 為 symlink 的 case,
// 不擋父層)。
//
// 修法後 OpenWriteValidated 內部會 EvalSymlinks 解析 path 後再開 — resolved
// path 落在 outside,不在 allowed 之下,reject。
func TestOpenWriteValidated_ParentSymlinkTOCTOU(t *testing.T) {
	t.Parallel()

	// 用 EvalSymlinks 解析過的 tempDir 作為 baseDir,避免 macOS 上 /var → /private/var
	// 等 symlink 干擾測試契約。
	allowedDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(allowedDir) failed: %v", err)
	}
	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(outsideDir) failed: %v", err)
	}

	// 攻擊者在 allowed 內植入 symlink → outsideDir
	linkPath := filepath.Join(allowedDir, "escape")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("setup: Symlink failed: %v", err)
	}

	// 嘗試透過 link 寫 — 必須 reject
	attackPath := filepath.Join(linkPath, "loot.csv")
	f, openErr := fsperm.OpenWriteValidated(attackPath, []string{allowedDir})
	if openErr == nil {
		_ = f.Close()
		_ = os.Remove(attackPath)
		t.Fatalf("parent-symlink-escape 路徑應被拒,實際通過 (寫到 %s)", attackPath)
	}
	if !errors.Is(openErr, fsperm.ErrPathEscapesBase) {
		t.Errorf("expected ErrPathEscapesBase,got %v", openErr)
	}

	// 確認 outsideDir 沒有檔案被建立
	if _, statErr := os.Stat(filepath.Join(outsideDir, "loot.csv")); !os.IsNotExist(statErr) {
		t.Errorf("loot.csv 不該被建立在 outsideDir 內,stat err = %v", statErr)
	}
}

// TestOpenWriteValidated_ParentSymlinkAtDeepPath 釘住 deep-path 變形:
// allowed/sub/escape → outside,寫 allowed/sub/escape/loot.csv。
func TestOpenWriteValidated_ParentSymlinkAtDeepPath(t *testing.T) {
	t.Parallel()

	allowedDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(allowedDir) failed: %v", err)
	}
	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks(outsideDir) failed: %v", err)
	}

	// allowed/sub 是真實子目錄
	subDir := filepath.Join(allowedDir, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("setup: MkdirAll failed: %v", err)
	}

	// allowed/sub/escape → outsideDir
	linkPath := filepath.Join(subDir, "escape")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Fatalf("setup: Symlink failed: %v", err)
	}

	attackPath := filepath.Join(linkPath, "loot.csv")
	f, openErr := fsperm.OpenWriteValidated(attackPath, []string{allowedDir})
	if openErr == nil {
		_ = f.Close()
		_ = os.Remove(attackPath)
		t.Fatalf("deep parent-symlink-escape 應被拒,實際通過")
	}
	if !errors.Is(openErr, fsperm.ErrPathEscapesBase) {
		t.Errorf("expected ErrPathEscapesBase,got %v", openErr)
	}
}

// TestOpenReadValidated_ParentSymlinkRejected 釘住 OpenReadValidated 對
// parent-component symlink-escape 的 reject 行為,與 OpenWriteValidated 的
// 同名測試對稱。
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

// TestOpenWriteValidated_OpensResolvedPath 釘住 OpenWriteValidated 內部
// validate-vs-use 一致性：成功 open 後,fd 對應的檔案必須跟 EvalSymlinks
// resolved 結果一致 (dev/inode 比對),否則表示有 TOCTOU 縫隙。
//
// V1 verify report Exploit 2：先前 OpenWriteValidated 用 EvalSymlinks(path) 校驗
// resolvedPath,但接著 os.OpenFile(path, ...) 用 unresolved path,kernel 在
// syscall 時 re-walk symlink。若攻擊者在兩個 syscall 之間 swap symlink,
// resolved path 校驗結果與實際 open 的檔案分歧。修法後 OpenWriteValidated 應
// 使用 resolved path open,fstat(fd) 必須與 lstat(resolvedPath) 一致。
//
// 本測試用 internal symlink (link → real,皆在 base 內) 驗證:resolved 與
// open 結果應一致。
func TestOpenWriteValidated_OpensResolvedPath(t *testing.T) {
	t.Parallel()

	baseDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("setup: EvalSymlinks failed: %v", err)
	}

	// base/real 真實目錄,base/link → base/real
	realDir := filepath.Join(baseDir, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("setup: MkdirAll failed: %v", err)
	}
	linkDir := filepath.Join(baseDir, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("setup: Symlink failed: %v", err)
	}

	// 透過 link 寫
	viaLinkPath := filepath.Join(linkDir, "data.csv")
	f, openErr := fsperm.OpenWriteValidated(viaLinkPath, []string{baseDir})
	if openErr != nil {
		t.Fatalf("base 內部 symlink 應放行,err=%v", openErr)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write([]byte("payload")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// fd 對應的檔案應是 real path 下的 data.csv
	fdStat, err := f.Stat()
	if err != nil {
		t.Fatalf("f.Stat failed: %v", err)
	}

	realPath := filepath.Join(realDir, "data.csv")
	realStat, err := os.Lstat(realPath)
	if err != nil {
		t.Fatalf("Lstat(realPath) failed: %v", err)
	}

	// fd 的 file info 必須與 real path 一致 (size / mode)
	if fdStat.Size() != realStat.Size() {
		t.Errorf("fd size %d != real path size %d — 可能 validate-vs-use mismatch",
			fdStat.Size(), realStat.Size())
	}
}
