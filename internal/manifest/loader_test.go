package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveEMGFile_DoubleWrapErrorsIs 釘住 修法:loader 的
// `fmt.Errorf("%w: %w", ErrManifestEMGPathInvalid, err)` 用 Go 1.20+ 雙重 %w
// 語意 — caller 可分別用 errors.Is 命中外層 sentinel(ErrManifestEMGPathInvalid)
// 與 inner cause(原始路徑解析錯誤)。若未來有人為求簡單退回 %s wrap 或單 %w
// 但丟掉外層 sentinel, 此 test 會立刻 fail。
func TestResolveEMGFile_DoubleWrapErrorsIs(t *testing.T) {
	t.Parallel()

	// 觸發 ResolveLenientPath 失敗:emgFile 含 path-traversal segment ".."
	// ResolveLenientPath 會拒絕(下游邊界檢查) — 拿到的 err 經 ErrManifestEMGPathInvalid 包裝。
	_, err := ResolveEMGFile(t.TempDir(), "../escape.csv")
	if err == nil {
		t.Fatal("ResolveEMGFile 必須對 path traversal input 回 error")
	}

	if !errors.Is(err, ErrManifestEMGPathInvalid) {
		t.Errorf("errors.Is(err, ErrManifestEMGPathInvalid) = false; err=%v", err)
	}

	// 連同 inner cause 一起 unwrap:不該命中 ErrManifestEMGFileMissing
	// (這是另一條 sentinel,確認雙 %w 不會把 file-missing sentinel 誤命中)
	if errors.Is(err, ErrManifestEMGFileMissing) {
		t.Error("errors.Is(err, ErrManifestEMGFileMissing) 不該為 true — 路徑驗證失敗應與 file missing 區分")
	}
}

// TestResolveEMGFile_FileMissingErrorsIs 釘住 第二條 sentinel 命中。
// baseFolder 存在但 emgFile 指向不存在的檔 → 應命中 ErrManifestEMGFileMissing。
func TestResolveEMGFile_FileMissingErrorsIs(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	_, err := ResolveEMGFile(base, "nonexistent_subject.csv")
	if err == nil {
		t.Fatal("ResolveEMGFile 必須對不存在的檔回 error")
	}

	if !errors.Is(err, ErrManifestEMGFileMissing) {
		t.Errorf("errors.Is(err, ErrManifestEMGFileMissing) = false; err=%v", err)
	}

	// 互斥 sanity check
	if errors.Is(err, ErrManifestEMGPathInvalid) {
		t.Error("errors.Is(err, ErrManifestEMGPathInvalid) 不該為 true — file missing 應與路徑驗證失敗區分")
	}
}

// TestResolveEMGFile_Success 釘住 happy path,雙 sentinel 都不命中 err==nil。
func TestResolveEMGFile_Success(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	target := filepath.Join(base, "subject_A.csv")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}

	resolved, err := ResolveEMGFile(base, "subject_A.csv")
	if err != nil {
		t.Fatalf("ResolveEMGFile 不該失敗:%v", err)
	}
	if resolved == "" {
		t.Error("ResolveEMGFile 必須回 non-empty resolved path")
	}
}
