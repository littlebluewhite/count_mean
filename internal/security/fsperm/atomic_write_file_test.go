//go:build !windows

package fsperm_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"count_mean/internal/security/fsperm"
)

// resolvedTempDir 回傳 t.TempDir() 經 filepath.EvalSymlinks 解析後的絕對路徑。
// macOS 上 t.TempDir() 回 /var/folders/... 是 /private/var/folders/... 的 symlink;
// OpenAtomicWriteValidated 內部對父目錄呼 EvalSymlinksWithFallback,故 basePaths
// 必須已是 resolved 形式,否則 matchAnyBase 比較兩個不同路徑 → ErrPathEscapesBase。
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolvedTempDir: EvalSymlinks: %v", err)
	}
	return resolved
}

// tmpOrphanGlob 回傳目錄 dir 下所有 *.tmp.* 殘留檔案的清單。
// AtomicWriteFile 成功完成後,crypto-random tmp 已被 rename 走,此清單應為空。
func tmpOrphanGlob(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp.*"))
	if err != nil {
		t.Fatalf("tmpOrphanGlob: Glob: %v", err)
	}
	return matches
}

// TestAtomicWriteFile_Fallback_WritesExactBytes 釘住 basePaths=nil 走 legacy
// fallback 路徑:AtomicWriteFile 完成後 path 含正確 bytes、無 *.tmp.* 孤兒殘留。
func TestAtomicWriteFile_Fallback_WritesExactBytes(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "out.csv")
	payload := "hello\nworld\n"

	err := fsperm.AtomicWriteFile(path, nil, func(w io.Writer) error {
		_, writeErr := io.WriteString(w, payload)
		return writeErr
	})
	if err != nil {
		t.Fatalf("AtomicWriteFile (fallback): %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != payload {
		t.Errorf("content = %q, want %q", got, payload)
	}

	if orphans := tmpOrphanGlob(t, dir); len(orphans) != 0 {
		t.Errorf("tmp 孤兒應在 rename 後消失, 找到: %v", orphans)
	}
}

// TestAtomicWriteFile_Validated_WritesExactBytes 釘住 basePaths 非空走 validated
// 路徑:觀察到正確輸出 bytes、無 *.tmp.* 孤兒殘留。
// 不斷言是否使用 dirfd(平台相依;Windows/舊 kernel 退化 validated-parent+os.Rename)。
func TestAtomicWriteFile_Validated_WritesExactBytes(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "out.csv")
	payload := "hello\nworld\n"

	err := fsperm.AtomicWriteFile(path, []string{dir}, func(w io.Writer) error {
		_, writeErr := io.WriteString(w, payload)
		return writeErr
	})
	if err != nil {
		t.Fatalf("AtomicWriteFile (validated): %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != payload {
		t.Errorf("content = %q, want %q", got, payload)
	}

	if orphans := tmpOrphanGlob(t, dir); len(orphans) != 0 {
		t.Errorf("tmp 孤兒應在 rename 後消失, 找到: %v", orphans)
	}
}

// TestAtomicWriteFile_WriteCallbackError_LeavesNoTarget 釘住 write callback 回 error
// 時的清理語意:orchestrator 傳播 callback 的原始 error、path 不存在、無 tmp 孤兒殘留。
func TestAtomicWriteFile_WriteCallbackError_LeavesNoTarget(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "out.csv")
	errBoom := errors.New("boom")

	err := fsperm.AtomicWriteFile(path, nil, func(_ io.Writer) error {
		return errBoom
	})
	if err == nil {
		t.Fatal("callback 回 error 時 AtomicWriteFile 應回 error")
	}
	if !errors.Is(err, errBoom) {
		t.Errorf("errors.Is(err, errBoom) = false; err = %v", err)
	}

	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("callback error 後 path 不應存在, Stat err = %v", statErr)
	}

	if orphans := tmpOrphanGlob(t, dir); len(orphans) != 0 {
		t.Errorf("cleanup 後不應有 tmp 孤兒, 找到: %v", orphans)
	}
}

// TestAtomicWriteFile_EscapingBasePaths_PreservesErrPathEscapesBase 釘住 target 父目錄
// 不在任何 base 之下時 ErrPathEscapesBase sentinel 穿透 orchestrator 的 %w 包裝。
// path 不應被建立。
func TestAtomicWriteFile_EscapingBasePaths_PreservesErrPathEscapesBase(t *testing.T) {
	t.Parallel()

	base := resolvedTempDir(t)    // basePaths 只包含此 dir
	outside := resolvedTempDir(t) // 另一個 temp dir,不在 base 下
	path := filepath.Join(outside, "out.csv")

	err := fsperm.AtomicWriteFile(path, []string{base}, func(_ io.Writer) error {
		return nil
	})
	if err == nil {
		t.Fatal("parent 不在 base 下應回 error")
	}
	if !errors.Is(err, fsperm.ErrPathEscapesBase) {
		t.Errorf("errors.Is(err, ErrPathEscapesBase) = false; err = %v", err)
	}

	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("逸出 base 的 path 不應被建立, Stat err = %v", statErr)
	}
}

// TestAtomicWriteFile_EmptyBasePaths_UsesFallbackNotSentinel 釘住 basePaths=[]string{}
// (非 nil 但空) 走 fallback 而非 validated primitive —— orchestrator 以 len>0 判斷
// 分支,空 slice 與 nil 行為相同,不回 ErrBasePathsEmpty(那是 primitive 的 sentinel)。
// 此測試文件此行為:AtomicWriteFile 對空 basePaths 成功完成,不傳遞 ErrBasePathsEmpty。
func TestAtomicWriteFile_EmptyBasePaths_UsesFallbackNotSentinel(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "out.csv")

	// basePaths 為非 nil 空 slice;orchestrator 的 len(basePaths)>0 為 false
	// → 走 legacy fallback,不呼 OpenAtomicWriteValidated → 不觸發 ErrBasePathsEmpty。
	err := fsperm.AtomicWriteFile(path, []string{}, func(w io.Writer) error {
		_, writeErr := io.WriteString(w, "ok\n")
		return writeErr
	})
	if err != nil {
		t.Fatalf("empty basePaths 應走 fallback 並成功, got: %v", err)
	}
	if errors.Is(err, fsperm.ErrBasePathsEmpty) {
		t.Errorf("empty basePaths 不應傳遞 ErrBasePathsEmpty")
	}

	got, readErr := os.ReadFile(path) //nolint:gosec // test path
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(got) != "ok\n" {
		t.Errorf("content = %q, want %q", got, "ok\n")
	}
}

// TestAtomicWriteFile_OverwritesExistingTarget 釘住 atomic replace:path 預先存在時,
// AtomicWriteFile 以 rename 原子替換,最終檔案含新 bytes。
func TestAtomicWriteFile_OverwritesExistingTarget(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "out.csv")

	// 預先建立舊內容。
	if err := os.WriteFile(path, []byte("old content\n"), fsperm.FilePerm); err != nil {
		t.Fatalf("setup: WriteFile: %v", err)
	}

	newPayload := "new content\n"
	err := fsperm.AtomicWriteFile(path, nil, func(w io.Writer) error {
		_, writeErr := io.WriteString(w, newPayload)
		return writeErr
	})
	if err != nil {
		t.Fatalf("AtomicWriteFile (overwrite): %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != newPayload {
		t.Errorf("content after overwrite = %q, want %q", got, newPayload)
	}

	if orphans := tmpOrphanGlob(t, dir); len(orphans) != 0 {
		t.Errorf("tmp 孤兒應在 rename 後消失, 找到: %v", orphans)
	}
}

// TestAtomicWriteFile_Validated_WriteCallbackError_CleansUp 釘住 validated 分支在
// write callback 回 error 時的清理:走 handle.Abort()(dirfd 路徑 unlinkat tmp + 關
// dirfd;非 openat2 平台 fallback 走 os.Remove)—— path 不應存在、無 *.tmp.* 孤兒、
// 無 dirfd 洩漏。補 WriteCallbackError 的 fallback (basePaths=nil) 版未覆蓋的 dirfd
// cleanup wiring(唯一有 fd-leak 風險的清理分支)。
func TestAtomicWriteFile_Validated_WriteCallbackError_CleansUp(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "out.csv")
	errBoom := errors.New("boom")

	err := fsperm.AtomicWriteFile(path, []string{dir}, func(_ io.Writer) error {
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Errorf("errors.Is(err, errBoom) = false; err = %v", err)
	}

	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("callback error 後 path 不應存在, Stat err = %v", statErr)
	}
	if orphans := tmpOrphanGlob(t, dir); len(orphans) != 0 {
		t.Errorf("validated 分支 cleanup 後不應有 tmp 孤兒, 找到: %v", orphans)
	}
}

// TestAtomicWriteFile_Validated_OverwritesExistingTarget 釘住 validated 分支的 atomic
// replace:預先存在的 target 被 renameat(dirfd 路徑)/ os.Rename(fallback)原子替換成
// 新 bytes,且無 tmp 孤兒殘留。
func TestAtomicWriteFile_Validated_OverwritesExistingTarget(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "out.csv")
	if err := os.WriteFile(path, []byte("old\n"), fsperm.FilePerm); err != nil {
		t.Fatalf("setup: WriteFile: %v", err)
	}

	newPayload := "new validated content\n"
	err := fsperm.AtomicWriteFile(path, []string{dir}, func(w io.Writer) error {
		_, writeErr := io.WriteString(w, newPayload)
		return writeErr
	})
	if err != nil {
		t.Fatalf("AtomicWriteFile (validated overwrite): %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != newPayload {
		t.Errorf("content after validated overwrite = %q, want %q", got, newPayload)
	}
	if orphans := tmpOrphanGlob(t, dir); len(orphans) != 0 {
		t.Errorf("tmp 孤兒應在 rename 後消失, 找到: %v", orphans)
	}
}

// 以下三條承接 ADR-0055 刪除的 csvutil.WriteCSVAtomic 測試中屬於落檔協定的部分。

// TestAtomicWriteFile_LongBasenameTmpStaysUnderNameMax 釘住:final basename 本身合法
// (<=255 bytes)但加上 tmp 後綴(".tmp." + 16 hex = 21 bytes)會跨越 NAME_MAX 時,
// AtomicWriteFile 仍成功 —— tmp basename 截斷讓中介檔 fit,final 以完整檔名寫出。
//
// 回歸來源 (codex Round 2 [P2]):normalized PhaseSync 把 ~200-byte subject 拼成
// ~238-byte filename,遷 atomic 後 tmp ~259 bytes 在 255 上限的 fs 上 ENAMETOOLONG。
// 平台前提:ext4 / APFS 等 NAME_MAX=255。basename 取 245 → final 合法,tmp 無截斷會溢位。
func TestAtomicWriteFile_LongBasenameTmpStaysUnderNameMax(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	name := strings.Repeat("a", 245-len(".csv")) + ".csv"
	path := filepath.Join(dir, name)

	err := fsperm.AtomicWriteFile(path, []string{dir}, func(w io.Writer) error {
		_, writeErr := io.WriteString(w, "v\n")
		return writeErr
	})
	if err != nil {
		t.Fatalf("long-but-valid basename 應成功, got %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("final 應以完整 basename 寫出: %v", statErr)
	}
	if orphans := tmpOrphanGlob(t, dir); len(orphans) != 0 {
		t.Errorf("tmp 孤兒應在 rename 後消失, 找到: %v", orphans)
	}
}

// TestAtomicWriteFile_PredictableStaleTmpDoesNotBlock 釘住 tmp 名不可預測:tmp 走
// crypto/rand 後綴,上次 crash 留下的 `path.tmp`、或攻擊者預植的可預測樣式都不會撞名
// 擋住這次寫入(舊版固定 `path + ".tmp"` 可被 plant 造成 DoS)。
func TestAtomicWriteFile_PredictableStaleTmpDoesNotBlock(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "out.csv")
	for _, stale := range []string{path + ".tmp", path + ".tmp_0", path + ".tmp_1", path + ".tmp_legacy"} {
		if err := os.WriteFile(stale, []byte("stale predictable"), 0o600); err != nil {
			t.Fatalf("setup stale %q: %v", stale, err)
		}
	}

	err := fsperm.AtomicWriteFile(path, []string{dir}, func(w io.Writer) error {
		_, writeErr := io.WriteString(w, "v\n")
		return writeErr
	})
	if err != nil {
		t.Fatalf("可預測的 stale tmp 存在時仍應成功: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("final path 未建立: %v", statErr)
	}
}

// TestAtomicWriteFile_WriteCallbackError_ExistingTargetUntouched 釘住 write 中途失敗時
// 既有 target 不被改寫(tmp 未 commit),且不留 tmp 孤兒。
func TestAtomicWriteFile_WriteCallbackError_ExistingTargetUntouched(t *testing.T) {
	t.Parallel()

	dir := resolvedTempDir(t)
	path := filepath.Join(dir, "out.csv")
	old := []byte("old content\n")
	if err := os.WriteFile(path, old, fsperm.FilePerm); err != nil {
		t.Fatalf("setup: WriteFile: %v", err)
	}

	errBoom := errors.New("boom")
	err := fsperm.AtomicWriteFile(path, []string{dir}, func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("errors.Is(err, errBoom) = false; err = %v", err)
	}

	got, readErr := os.ReadFile(path) //nolint:gosec // test path
	if readErr != nil {
		t.Fatalf("ReadFile: %v", readErr)
	}
	if string(got) != string(old) {
		t.Errorf("既有 target 被改寫: got %q want %q", got, old)
	}
	if orphans := tmpOrphanGlob(t, dir); len(orphans) != 0 {
		t.Errorf("失敗後不應有 tmp 孤兒, 找到: %v", orphans)
	}
}
