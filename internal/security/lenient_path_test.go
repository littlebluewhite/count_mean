package security

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveLenientPath_AcceptsLiteralPercent 釘住 codex review P1 (post-impl)：
// BTS 匯出檔名常含字面 "%"，PathValidator.GetSafePath 會誤拒，本 lenient 版本必須接受。
func TestResolveLenientPath_AcceptsLiteralPercent(t *testing.T) {
	base := t.TempDir()
	got, err := ResolveLenientPath(base, "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv")
	if err != nil {
		t.Fatalf("含 %% 的檔名應被接受，error: %v", err)
	}

	want := filepath.Join(base, "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolveLenientPath_RejectsTraversal 確認 P1 fix 仍防 ../ traversal。
func TestResolveLenientPath_RejectsTraversal(t *testing.T) {
	base := t.TempDir()
	cases := []string{
		"../etc/passwd",
		"sub/../../etc/passwd",
		"..\\windows\\system",
	}

	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			if _, err := ResolveLenientPath(base, c); err == nil {
				t.Fatalf("含 .. 的檔名 %q 應該被拒，但通過了", c)
			}
		})
	}
}

// TestResolveLenientPath_RejectsAbsolute 確認絕對路徑被拒。
func TestResolveLenientPath_RejectsAbsolute(t *testing.T) {
	base := t.TempDir()
	if _, err := ResolveLenientPath(base, "/etc/passwd"); err == nil {
		t.Fatalf("絕對路徑應被拒")
	}
}

// TestResolveLenientPath_RejectsEmpty 確認空檔名被拒。
func TestResolveLenientPath_RejectsEmpty(t *testing.T) {
	base := t.TempDir()
	if _, err := ResolveLenientPath(base, ""); err == nil {
		t.Fatalf("空檔名應被拒")
	}
}

// TestResolveLenientPath_AcceptsLegitimateDoubleDot 確認 "..v2.csv" 這類含雙點但非
// path element 的合法檔名不會被誤拒（與 PathValidator HasTraversalElement 同行為）。
func TestResolveLenientPath_AcceptsLegitimateDoubleDot(t *testing.T) {
	base := t.TempDir()
	got, err := ResolveLenientPath(base, "report..v2.csv")
	if err != nil {
		t.Fatalf("含雙點但非 path element 的合法檔名應被接受，error: %v", err)
	}

	want := filepath.Join(base, "report..v2.csv")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolveLenientPath_AcceptsSubdirectory 確認 subdirectory 路徑仍被接受
// （只要結果仍在 baseFolder 之下且不含 ".." element）。
func TestResolveLenientPath_AcceptsSubdirectory(t *testing.T) {
	base := t.TempDir()

	// 在 base 內建立真實 subdir，這樣 EvalSymlinks 才能解析（既存目錄）
	require.NoError(t, os.MkdirAll(filepath.Join(base, "subdir"), 0o755))

	got, err := ResolveLenientPath(base, "subdir/file.csv")
	if err != nil {
		t.Fatalf("subdirectory 檔名應被接受，error: %v", err)
	}

	want := filepath.Join(base, "subdir", "file.csv")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolveLenientPath_RejectsParentSymlinkEscapingBase 釘住 codex review post-impl P2：
// O_NOFOLLOW 只擋「最終 component」是 symlink；若 baseFolder 內有 symlinked 子目錄指向外部，
// manifest 引用 `link/emg.csv` 會通過 lexical Rel 檢查，OpenFile 跟著 parent symlink 讀外部檔。
// 修法：ResolveLenientPath 內 EvalSymlinks(joined / parent) 後再 Rel 檢查 boundary。
func TestResolveLenientPath_RejectsParentSymlinkEscapingBase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink 需要 admin 權限，跳過")
	}

	base := t.TempDir()
	outside := t.TempDir() // 模擬 baseFolder 之外的目錄（含敏感檔）

	// 在 outside 建一個檔（模擬 /etc/passwd 之類）
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.csv"), []byte("secret"), 0o600))

	// 在 base 內建 symlink "link" → outside
	require.NoError(t, os.Symlink(outside, filepath.Join(base, "link")))

	// 攻擊：manifest 引用 "link/secret.csv"，期望被拒
	_, err := ResolveLenientPath(base, "link/secret.csv")
	require.Error(t, err, "parent symlink 跨出 baseFolder 的 path 應被拒")
	// Pin 訊息來自 boundary check（非 EvalSymlinks generic error）— 否則 refactor
	// 把 "資料夾外" 改成模糊「無法解析」也會通過 require.Error。
	assert.Contains(t, err.Error(), "資料夾外",
		"err 應來自 symlink-aware boundary check，實際 err=%v", err)
}

// TestResolveLenientPath_NormalizesWindowsBackslash 釘住 codex review post-impl P2：
// manifest 可能在 Windows 端產生並包含 "\" 作 path separator（例 "trial1\\emg.csv"）。
// HasTraversalElement 已支援 "\" 作為 separator 偵測 ".." element；但 filepath.Join 在
// Unix 不認 "\"，會把整段視為單一檔名（含 literal "\"）。修法：join 前把 "\" 統一換成 "/"，
// 與 HasTraversalElement 行為對稱。
func TestResolveLenientPath_NormalizesWindowsBackslash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 本身用 \\，不需 normalize；此 test 是針對 Unix/macOS 的 cross-platform 場景")
	}

	base := t.TempDir()

	// 建 base/trial1 真實子目錄，供 EvalSymlinks 解析既存路徑
	require.NoError(t, os.MkdirAll(filepath.Join(base, "trial1"), 0o755))

	got, err := ResolveLenientPath(base, `trial1\emg.csv`)
	if err != nil {
		t.Fatalf("Windows-style \\ separator 應被 normalize 成 /，error: %v", err)
	}

	want := filepath.Join(base, "trial1", "emg.csv")
	if got != want {
		t.Errorf("got %q, want %q (expected \\ normalized to OS separator)", got, want)
	}
}

// TestResolveLenientPath_AcceptsInternalSymlink 確認「baseFolder 內部」的 symlink
// （指向同 base 下的另一個子目錄）仍被接受 — symlink 解析後仍在 baseFolder 內就 OK。
func TestResolveLenientPath_AcceptsInternalSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink 需要 admin 權限，跳過")
	}

	base := t.TempDir()

	// base/real 是真實目錄；base/link → base/real
	realDir := filepath.Join(base, "real")
	require.NoError(t, os.MkdirAll(realDir, 0o755))
	require.NoError(t, os.Symlink(realDir, filepath.Join(base, "link")))

	got, err := ResolveLenientPath(base, "link/emg.csv")
	if err != nil {
		t.Fatalf("base 內部的 symlink 應被接受，error: %v", err)
	}

	if got == "" {
		t.Errorf("got empty path")
	}
}
