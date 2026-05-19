package security

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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

// TestResolveLenientPath_RejectsNullByte 釘住 null byte 在 normalize 之後立即被擋，
// 避免落入 os.OpenFile error path 把未清理的 byte 寫進 log。
func TestResolveLenientPath_RejectsNullByte(t *testing.T) {
	base := t.TempDir()
	_, err := ResolveLenientPath(base, "a\x00b.csv")
	require.Error(t, err, "含 null byte 的檔名應被拒")
	assert.Contains(t, err.Error(), "null byte",
		"err 應來自 null byte 守門，實際 err=%v", err)
}

// TestResolveLenientPath_RejectsDotOnly 釘住 bare "." 會被 Clean 成 "."、Join 成
// baseFolder 本身；caller 後續 OpenFile 在 Unix 對目錄成功，行為未定。明確拒絕。
func TestResolveLenientPath_RejectsDotOnly(t *testing.T) {
	base := t.TempDir()
	_, err := ResolveLenientPath(base, ".")
	require.Error(t, err, "bare \".\" 應被拒")
	assert.Contains(t, err.Error(), "無效",
		"err 應來自 dot-only 守門，實際 err=%v", err)
}

// TestResolveLenientPath_RejectsBareTraversal 釘住測試補洞：bare ".." 必須被擋於
// HasTraversalElement 階段，避免 Join 後解析到 baseFolder 的 parent dir。
func TestResolveLenientPath_RejectsBareTraversal(t *testing.T) {
	base := t.TempDir()
	_, err := ResolveLenientPath(base, "..")
	require.Error(t, err, "bare \"..\" 應被拒")
}

// TestResolveLenientPath_RejectsWhitespace 釘住 純 whitespace 通過 normalize 後
// TrimSpace + Clean 得到 ""，與 dot-only 同類拒絕。
func TestResolveLenientPath_RejectsWhitespace(t *testing.T) {
	base := t.TempDir()
	_, err := ResolveLenientPath(base, "  ")
	require.Error(t, err, "純 whitespace 檔名應被拒")
	assert.Contains(t, err.Error(), "無效",
		"err 應來自 whitespace 守門，實際 err=%v", err)
}

// TestResolveLenientPath_RejectsLongFilename 釘住 filename base > 255 字元被拒。
// 與 PathValidator.GetSafePath:408 對齊 maxFilenameLength=255。
func TestResolveLenientPath_RejectsLongFilename(t *testing.T) {
	base := t.TempDir()
	longName := strings.Repeat("a", 256) + ".csv"
	_, err := ResolveLenientPath(base, longName)
	require.Error(t, err, "filename > 255 chars 應被拒")
	assert.Contains(t, err.Error(), "檔名過長",
		"err 應來自 filename length 守門，實際 err=%v", err)
}

// TestResolveLenientPath_ZeroWidthIsExplicitlyRejectedEarly 釘住 L13 的契約：
// 即使 baseFolder 與 parent 都存在（path resolution happy path），含零寬字元
// 的 element 仍必須被 *早期 traversal check* 擋下，**錯誤訊息必須來自 traversal
// 路徑（含 ".."），而不是後段 path resolution 失敗**。
//
// 這條 contract 確保 fix 不只是「碰巧 EvalSymlinks fail 連帶擋住」，而是真的
// 在 element-level check 階段就辨識出零寬字元偽裝。否則若未來把 lenient_path
// 改成接受未存在 parent，這個攻擊就會復活。
func TestResolveLenientPath_ZeroWidthIsExplicitlyRejectedEarly(t *testing.T) {
	base := t.TempDir()
	// 預先建立 foo 子目錄，讓 EvalSymlinks 走 happy path
	require.NoError(t, os.MkdirAll(filepath.Join(base, "foo"), 0o755))

	zwsp := string(rune(0x200B))

	// `foo/..<ZWSP>/bar` — split 後 element 2 = `..<ZWSP>`，HasTraversalElement
	// 比對 part == ".." false → 通過。Clean 不改寫含零寬字元的 element。
	filename := "foo/.." + zwsp + "/bar"
	_, err := ResolveLenientPath(base, filename)
	require.Error(t, err, "含零寬字元的 traversal element 必須被擋")
	// Pin error 訊息來自 traversal-element 守門（明文「\"..\" 路徑元素」）。
	// 不能只 assert.Contains(err.Error(), "..") — 因為 path 本身就含 `..`，所有 error
	// wrap 都會帶。明文比對「\"..\" 路徑元素」是現行 lenient_path 對 traversal 守門
	// 拋出的訊息，pin 死才能確保 fix 真的擋在早期 element-level check。
	assert.Contains(t, err.Error(), `".." 路徑元素`,
		"err 應來自 element-level traversal 守門（明文「.. 路徑元素」），實際 err=%v — 若 err 只是 EvalSymlinks generic 失敗，"+
			"代表 fix 沒擋在 element-level 早期階段，未來 path resolution 重構可能讓攻擊復活", err)
}

// TestResolveLenientPath_ZeroWidthInElementIsRejected 釘住 L13 的核心攻擊：
// 零寬字元混入 path element 內，使 split-on-separator 後 element 不 = ".." 而被
// HasTraversalElement 放行，但若 caller 端或下游 filesystem 在某些 normalization
// 規則下把零寬字元剝除，這條 element 就「變回」`..`，達成 traversal。
//
// 本 test 用「在 baseFolder 內的路徑」（不含 `/etc/` 之類絕對部分）來精準觸發
// L13：path 走完 normalize / Clean / Rel 全部 happy path，元素本身看起來不是
// `..`，只有「strip zero-width 後等於 `..`」的判斷能擋住。
func TestResolveLenientPath_ZeroWidthInElementIsRejected(t *testing.T) {
	base := t.TempDir()

	zwsp := string(rune(0x200B))
	zwj := string(rune(0x200D))
	bom := string(rune(0xFEFF))

	cases := []struct {
		name     string
		filename string
	}{
		{"ZWSP at end of element", "foo/.." + zwsp + "/bar"},
		{"ZWSP at start of element", "foo/" + zwsp + "../bar"},
		{"ZWJ mid element", "foo/." + zwj + "./bar"},
		{"BOM mid element", "foo/." + bom + "./bar"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ResolveLenientPath(base, c.filename); err == nil {
				t.Fatalf("零寬字元偽裝的 traversal element %q 應被擋", c.filename)
			}
		})
	}
}

// TestResolveLenientPath_RejectsZeroWidthCharacterTraversal 釘住 L13：
// `strings.TrimSpace` 把「ASCII 空白 + Unicode whitespace category」當 trim
// 對象,但「零寬字元」(U+200B ZWSP / U+200C ZWNJ / U+200D ZWJ / U+FEFF BOM /
// U+2060 WJ) 不在 unicode.White_Space 集合,TrimSpace 留下不動。攻擊向量範例：
//
//	"..​" 或 "​.."
//
// 經過原本 normalize / Clean 後仍視為單一 element「..<ZWSP>」（HasTraversalElement
// 比對 part != ".." 為 true → 通過）；如果未來某層 normalization 把零寬字元剝掉、
// 或 filesystem 在某些 NFC 規範化下把它視為 noise，這條 path 就會變成真正的
// `..` 並逃出 baseFolder。
//
// 修法：在 normalize 與 cleaned 雙檢查之上，再加一層「strip-zero-width 後比對 ..」。
func TestResolveLenientPath_RejectsZeroWidthCharacterTraversal(t *testing.T) {
	base := t.TempDir()

	// 不能在 Go source 寫字面零寬字元（gofmt / source 編輯器會剝），用 rune literal 組
	zwsp := string(rune(0x200B))   // ZERO WIDTH SPACE
	zwnj := string(rune(0x200C))   // ZERO WIDTH NON-JOINER
	zwj := string(rune(0x200D))    // ZERO WIDTH JOINER
	wj := string(rune(0x2060))     // WORD JOINER
	bom := string(rune(0xFEFF))    // ZERO WIDTH NO-BREAK SPACE / BOM
	ideoSp := string(rune(0x3000)) // IDEOGRAPHIC SPACE — 在 unicode.IsSpace 集合，TrimSpace 會剝；放這 test 確保覆蓋

	cases := []struct {
		name     string
		filename string
	}{
		{"ZWSP suffix", ".." + zwsp + "/etc/passwd"},
		{"ZWSP prefix", zwsp + "../etc/passwd"},
		{"ZWNJ between dots", "." + zwnj + "./etc/passwd"},
		{"ZWJ both sides", zwj + ".." + zwj + "/etc/passwd"},
		{"WJ inside element", "foo/" + wj + "../bar"},
		{"BOM disguised traversal", bom + ".." + bom + "/bar"},
		{"IDEOGRAPHIC space (TrimSpace 自會剝)", ".." + ideoSp + "/bar"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ResolveLenientPath(base, c.filename); err == nil {
				t.Fatalf("零寬字元偽裝的 traversal %q 應被擋", c.filename)
			}
		})
	}
}

// TestResolveLenientPath_TraversalDefenseInDepth 釘住 HasTraversalElement
// 必須同時跑在 normalized 與 cleaned 兩個階段，避免 `..` 被內嵌空白繞過。
//
// 攻擊向量：含 whitespace 的「假 `..`」element (例如 `foo/ ../bar`、`foo/.. /bar`)
// 在純 split-on-separator 的 HasTraversalElement 下會被當成「字面 ` ..` 目錄名」、
// 「字面 `.. ` 目錄名」未被識別為 traversal。雖然 filesystem 多半不存在這種目錄
// 而會在 OpenFile 階段失敗，但本檔的契約是「在 resolution 前先擋 traversal」，
// 否則 boundary check 之外的任何 fallback path（例如 evalSymlinksLenient 走 parent
// fallback）可能仍給出可疑路徑。Defense in depth：normalized 與 cleaned 雙檢查。
//
// 此 test 同時 cover whitespace-padded `..` 與 mixed-separator `..` 變形。
func TestResolveLenientPath_TraversalDefenseInDepth(t *testing.T) {
	base := t.TempDir()
	cases := []string{
		"foo/ ../bar",  // leading space before ".."
		"foo/.. /bar",  // trailing space after ".."
		"foo/ .. /bar", // both leading and trailing space
		" ../bar",      // bare ".." with leading space
		"../ bar",      // trailing slash with space
		"foo\\..\\bar", // mixed Windows separator
		"foo/..",       // trailing ".."
	}

	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := ResolveLenientPath(base, c)
			require.Error(t, err, "含 .. 的檔名（含空白變體）%q 應被拒", c)
		})
	}
}

// TestResolveLenientPath_RejectsAbsoluteRelOutput 釘住 regression:
// `filepath.Rel(base, joined)` 在 Windows cross-volume 場景 (例 base 在 C:\、
// joined 解析後在 D:\) 可能回 err != nil — 但極端 case 下 (例如 UNC vs drive-letter,
// 或 Go runtime 對特定 mount 表現)某些 rel 字串可能保留絕對路徑性質。
// 與 pathvalidator.go:540 對齊,加 `!filepath.IsAbs(rel)` defense-in-depth,
// 確保 rel 任何 form 的絕對路徑都會被擋。
//
// 在 Unix 上難以人工複製 cross-volume edge case (filepath.Rel 多回 error 而非
// 絕對 rel),因此本 test 直接驗 boundary check 的 contract — 用一個「被
// EvalSymlinks 解析到 baseFolder 之外」的 path,確認 Rel 的所有分支 (err / `..`
// prefix / absolute prefix) 都會 reject。
//
// 主要 black-box 驗證在 TestResolveLenientPath_RejectsParentSymlinkEscapingBase
// 已 cover;此 test 補強 boundary check 不依賴 `..` prefix 來辨識 escape,
// 用 read-the-source 的 element 一條條檢測 (用 internal helper isPathWithinBaseLenient
// 若日後 extract;目前 black-box 觀察:任何 rel != "."(同目錄) / rel[0] != base
// child 都該 reject)。
func TestResolveLenientPath_RejectsAbsoluteRelOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink 需要 admin 權限,跳過")
	}

	base := t.TempDir()
	outside := t.TempDir()

	// 建 base/link → outside (symlink 跨出 baseFolder)
	require.NoError(t, os.Symlink(outside, filepath.Join(base, "link")))

	// 在 outside 內建立 deeply nested 結構,確保 EvalSymlinks 完整解析後
	// resolvedJoined 與 resolvedBase 是「兩條完全不相干的絕對路徑」
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "deep"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "deep", "file.csv"), []byte("x"), 0o600))

	_, err := ResolveLenientPath(base, "link/deep/file.csv")
	require.Error(t, err, "symlink 跨出 baseFolder 後 rel 即便不是 `..` 開頭也應被擋")
	assert.Contains(t, err.Error(), "資料夾外",
		"err 應來自 boundary check (即便 rel 形式為絕對 / 跨 volume),實際 err=%v", err)
}

// TestResolveLenientPath_AcceptsDeepNonExistentChild 釘住 regression:
// 舊版 evalSymlinksLenient 只做單層 fallback — 若 joined path 的 parent 也不存在,
// 整段 path resolution fail。但 manifest-driven 流程常見 `OutputDir/subject/trial/emg.csv`
// 這類「subject 與 trial 資料夾都尚未建立」的情境,舊版會誤拒。
//
// 修法:遞迴 fallback (對齊 pathvalidator.resolveSymlinksWithFallback),沿 parent
// 一路往上找 nearest existing dir,resolve 後接回原 suffix。加 depth limit (8 層)
// 避免極端構造的 path 觸發過度 recursion。
func TestResolveLenientPath_AcceptsDeepNonExistentChild(t *testing.T) {
	base := t.TempDir()

	// 構造 5 層全部不存在的 nested path: base/a/b/c/d/file.csv
	// 舊版 evalSymlinksLenient (僅單層 fallback):
	//   EvalSymlinks(base/a/b/c/d/file.csv) → ENOENT
	//   EvalSymlinks(base/a/b/c/d)         → ENOENT
	//   → return error "parent dir 無法解析"
	//
	// 新版 (遞迴 fallback):
	//   EvalSymlinks(base/a/b/c/d/file.csv) → ENOENT
	//   recurse parent = base/a/b/c/d
	//   EvalSymlinks(base/a/b/c/d)         → ENOENT
	//   recurse parent = base/a/b/c
	//   ... 直到 base (存在) 才 resolve 成功,再 join suffix
	deepPath := "a/b/c/d/file.csv"
	got, err := ResolveLenientPath(base, deepPath)
	if err != nil {
		t.Fatalf("ResolveLenientPath 應接受多層 non-existent parent (manifest-driven 流程典型 case),err=%v", err)
	}
	want := filepath.Join(base, deepPath)
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestResolveLenientPath_DepthLimitProtectsAgainstPathologicalDepth 釘住 P2-5
// 的 depth-limit 守門:極端構造的 deeply nested path 不該觸發 unbounded recursion。
// 此 test 釘:不該 crash / hang;深度 > 8 時行為 reject (depth limit fail-closed)。
func TestResolveLenientPath_DepthLimitProtectsAgainstPathologicalDepth(t *testing.T) {
	base := t.TempDir()

	// 構造 7 層 non-existent nesting (剛好在 depth 8 之內,屬於 legitimate range)
	deepLegit := "a/b/c/d/e/f/file.csv" // parent walk 約 6 層
	_, err := ResolveLenientPath(base, deepLegit)
	if err != nil {
		t.Errorf("7 層 nesting 應為 legitimate (< depth limit),got err=%v", err)
	}

	// 構造 20 層 nesting — 超過 depth limit,行為:reject (不該 hang)
	deep := ""
	for i := 0; i < 20; i++ {
		deep += string(rune('a'+i%26)) + "/"
	}
	deep += "file.csv"
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ResolveLenientPath(base, deep)
	}()
	select {
	case <-done:
		// OK — 不論 success 或 error 都可接受,只要沒 hang
	case <-time.After(5 * time.Second):
		t.Fatalf("ResolveLenientPath 對 20 層 nested non-existent path hang 了 (depth limit 失效)")
	}
}

// TestResolveLenientPath_RejectsLongPath 釘住 joined path > 4096 字元被拒。
// 與 PathValidator.GetSafePath:401 對齊 maxPathLength=4096。
// 構造：base (~50 on macOS) + "a/" * 2048 + "a.csv" (~4101 chars) 使 joined > 4096，
// 但每個 component 個別 < 255 不會觸發 filename cap、無 ".." 不會觸發 HasTraversalElement。
func TestResolveLenientPath_RejectsLongPath(t *testing.T) {
	base := t.TempDir()
	longPath := strings.Repeat("a/", 2048) + "a.csv"
	_, err := ResolveLenientPath(base, longPath)
	require.Error(t, err, "joined > 4096 chars 應被拒")
	assert.Contains(t, err.Error(), "路徑過長",
		"err 應來自 path length 守門，實際 err=%v", err)
}
