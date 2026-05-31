package manifest

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// writeFileHelper 在 base 下寫一個含 content 的檔，回傳寫入的 bytes 供讀回比對。
func writeFileHelper(t *testing.T, base, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(base, name), []byte(content), 0o600); err != nil {
		t.Fatalf("setup WriteFile %s: %v", name, err)
	}
}

// TestOpenDataFile_Success 釘住 happy path:in-base 真實檔 → 回非 nil *os.File、
// 內容可讀回比對、三條 sentinel 都不命中。OpenDataFile 交出的是已開啟 fd,caller 自行 Close。
func TestOpenDataFile_Success(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	const want = "col1,col2\n1.0,2.0\n"
	writeFileHelper(t, base, "subject_A.csv", want)

	f, err := OpenDataFile(base, "subject_A.csv")
	if err != nil {
		t.Fatalf("OpenDataFile 不該失敗:%v", err)
	}
	if f == nil {
		t.Fatal("OpenDataFile 成功時必須回 non-nil *os.File")
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("讀回開好的檔失敗:%v", err)
	}
	if string(got) != want {
		t.Errorf("讀回內容 = %q; want %q", got, want)
	}
}

// TestOpenDataFile_LiteralPercent 釘住 BTS 匯出檔名含字面 "%" 不可 regress:
// OpenLenientValidated 端到端接受 "%"(不 URL-decode),OpenDataFile 不另加處理、
// 必須讓含 "%" 的真實檔開得進來且內容正確。
func TestOpenDataFile_LiteralPercent(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	const name = "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv"
	const want = "emg-bts-percent-payload\n"
	writeFileHelper(t, base, name, want)

	f, err := OpenDataFile(base, name)
	if err != nil {
		t.Fatalf("OpenDataFile 對含字面 %% 的檔名不該失敗:%v", err)
	}
	if f == nil {
		t.Fatal("OpenDataFile 成功時必須回 non-nil *os.File")
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("讀回 %% 檔失敗:%v", err)
	}
	if string(got) != want {
		t.Errorf("讀回內容 = %q; want %q", got, want)
	}
}

// TestOpenDataFile_FileMissingErrorsIs 釘住 missing-file 分類:base 存在但 filename
// 指向不存在的檔 → OpenLenientValidated 的 ENOENT 經雙層 %w 仍保留 os.ErrNotExist,
// 故映射到 ErrManifestDataFileMissing,且**互斥於** ErrManifestDataFilePathInvalid。
// file 回 nil。
func TestOpenDataFile_FileMissingErrorsIs(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	f, err := OpenDataFile(base, "nonexistent_subject.csv")
	if err == nil {
		t.Fatal("OpenDataFile 必須對不存在的檔回 error")
	}
	if f != nil {
		t.Error("OpenDataFile 失敗時必須回 nil file")
	}

	if !errors.Is(err, ErrManifestDataFileMissing) {
		t.Errorf("errors.Is(err, ErrManifestDataFileMissing) = false; err=%v", err)
	}
	// distinguishability:missing 不該被誤判成 path-invalid
	if errors.Is(err, ErrManifestDataFilePathInvalid) {
		t.Error("errors.Is(err, ErrManifestDataFilePathInvalid) 不該為 true — file missing 應與路徑驗證失敗區分")
	}
}

// TestOpenDataFile_TraversalErrorsIs 釘住 traversal/escape 分類:filename 含 ".."
// path-traversal element → OpenLenientValidated 在 lenient 解析階段回 bare error
// (非 os.ErrNotExist),fall through 到 ErrManifestDataFilePathInvalid,且**互斥於**
// ErrManifestDataFileMissing。file 回 nil。
func TestOpenDataFile_TraversalErrorsIs(t *testing.T) {
	t.Parallel()

	f, err := OpenDataFile(t.TempDir(), "../escape.csv")
	if err == nil {
		t.Fatal("OpenDataFile 必須對 path traversal input 回 error")
	}
	if f != nil {
		t.Error("OpenDataFile 失敗時必須回 nil file")
	}

	if !errors.Is(err, ErrManifestDataFilePathInvalid) {
		t.Errorf("errors.Is(err, ErrManifestDataFilePathInvalid) = false; err=%v", err)
	}
	// distinguishability:path-invalid 不該被誤判成 file missing
	if errors.Is(err, ErrManifestDataFileMissing) {
		t.Error("errors.Is(err, ErrManifestDataFileMissing) 不該為 true — 路徑驗證失敗應與 file missing 區分")
	}
}

// TestOpenDataFile_BaseFolderNotFound 釘住 base 無法解析的 fail-closed 行為:
// 不存在的 baseFolder → EvalSymlinks 失敗 → 命中 ErrBaseFolderNotFound、file 回 nil。
func TestOpenDataFile_BaseFolderNotFound(t *testing.T) {
	t.Parallel()

	// 在已被清掉的 temp dir 之下構造一條保證不存在的 base 路徑。
	missingBase := filepath.Join(t.TempDir(), "no_such_base_dir")
	f, err := OpenDataFile(missingBase, "subject_A.csv")
	if err == nil {
		t.Fatal("OpenDataFile 必須對不存在的 baseFolder 回 error")
	}
	if f != nil {
		t.Error("OpenDataFile 失敗時必須回 nil file")
	}

	if !errors.Is(err, ErrBaseFolderNotFound) {
		t.Errorf("errors.Is(err, ErrBaseFolderNotFound) = false; err=%v", err)
	}
}
