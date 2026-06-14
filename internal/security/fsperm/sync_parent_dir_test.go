package fsperm_test

import (
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestSyncParentDir_ValidPath 釘住:對 t.TempDir() 內的真實檔案路徑呼叫
// SyncParentDir 應回 nil。linux/darwin 走真實 unix.Fsync(dirfd);
// windows/other_unix 為 no-op,同樣回 nil。
func TestSyncParentDir_ValidPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "target.csv")

	// 建立真實檔案(parent dir 已存在 = t.TempDir())。
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := fsperm.SyncParentDir(path); err != nil {
		t.Errorf("SyncParentDir on existing parent = %v, want nil", err)
	}
}

// TestSyncParentDir_NonExistentParent 釘住 best-effort 語意:父目錄不存在時
// SyncParentDir 回 non-nil error(open 失敗),而不是 panic 或 silent ignore。
// Caller 可用此 error 做 log-and-continue。
func TestSyncParentDir_NonExistentParent(t *testing.T) {
	t.Parallel()

	// 指向一個絕對不存在的目錄下的路徑。
	path := filepath.Join(t.TempDir(), "nonexistent_subdir", "file.csv")

	err := fsperm.SyncParentDir(path)
	if err == nil {
		t.Errorf("SyncParentDir on non-existent parent = nil, want error")
	}
}
