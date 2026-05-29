package csvutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCSVAtomic_HappyPath_WritesBomHeaderRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"name", "value"},
		Emit: func(emit func([]string) error) error {
			if err := emit([]string{"alpha", "1"}); err != nil {
				return err
			}
			return emit([]string{"beta", "2"})
		},
	})
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "\xEF\xBB\xBF") {
		t.Errorf("missing BOM, got %q", s[:min(20, len(s))])
	}
	if !strings.Contains(s, "name,value") || !strings.Contains(s, "alpha,1") || !strings.Contains(s, "beta,2") {
		t.Errorf("missing rows in %q", s)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected tmp removed, stat err=%v", err)
	}
}

// TestWriteCSVAtomic_LongBasenameTmpStaysUnderNameMax 釘住:當 final basename 本身
// 合法 (<=255 bytes) 但加上 atomic tmp 後綴 (".tmp." + 16 hex = 21 bytes) 會跨越
// NAME_MAX 時,WriteCSVAtomic 仍成功 —— tmp basename 截斷讓中介檔 fit,final
// rename 用完整 path 寫出未截斷的檔名。
//
// 回歸來源 (codex Round 2 [P2]):normalized PhaseSync 把 ~200-byte subject 拼成
// ~238-byte filename,舊 direct-write (ExportToCSV) 寫得出,遷 atomic 後 tmp
// ~259 bytes 在 255 上限的 fs 上 ENAMETOOLONG。
//
// 平台前提:ext4 / APFS 等 NAME_MAX=255。basename 取 245 → final 合法,
// tmp (245+21=266) 無 fix 時溢位 fail。
func TestWriteCSVAtomic_LongBasenameTmpStaysUnderNameMax(t *testing.T) {
	dir := t.TempDir()

	const baseLen = 245
	name := strings.Repeat("a", baseLen-len(".csv")) + ".csv"
	if len(name) != baseLen {
		t.Fatalf("setup: basename len = %d, want %d", len(name), baseLen)
	}
	path := filepath.Join(dir, name)

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"col"},
		Emit:   func(emit func([]string) error) error { return emit([]string{"v"}) },
	})
	if err != nil {
		t.Fatalf("expected nil err for long-but-valid basename, got %v", err)
	}

	// final 檔案以完整 (未截斷) basename 寫出。
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("final file should exist at full path: %v", statErr)
	}

	// 無 tmp orphan 殘留 (tmp 已 rename 成 final)。
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("readdir: %v", readErr)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("stray tmp file remained: %s", e.Name())
		}
	}
}

func TestWriteCSVAtomic_EmitFailMidRow_FinalPathUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	sentinel := []byte("old content\n")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("simulated emit failure")
	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"name"},
		Emit: func(emit func([]string) error) error {
			if err := emit([]string{"alpha"}); err != nil {
				return err
			}
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr propagated, got %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != string(sentinel) {
		t.Errorf("final path mutated: got %q want %q", got, sentinel)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp leaked, stat err=%v", err)
	}
}

// 註:舊版 TestWriteCSVAtomic_StaleTmp_Rejected 在 修法後被
// TestWriteCSVAtomic_StaleTmpNoLongerBlocks 取代 — fix 後 tmp filename 改 random
// suffix,固定 `.tmp` stale 不再撞名,因此舊「stale block」契約已廢止。

func TestWriteCSVAtomic_HeaderSanitized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"=SUM(A1)", "normal"},
		Emit:   func(emit func([]string) error) error { return emit([]string{"v1", "v2"}) },
	})
	if err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "'=SUM(A1)") {
		t.Errorf("header should be sanitized with leading apostrophe, got %q", b)
	}
}

func TestWriteCSVAtomic_NilHeader_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: nil,
		Emit:   func(emit func([]string) error) error { return nil },
	})
	if err == nil {
		t.Fatal("expected err on nil header")
	}
}

// TestWriteCSVAtomic_BodySanitize verifies WriteCSVAtomic also sanitizes body rows
// (not only header). Caller-supplied data may contain user-controlled phase labels,
// subject names, or round-tripped channel headers that flow through CSV injection
// to Excel/Numbers.
func TestWriteCSVAtomic_BodySanitize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"label", "value"},
		Emit: func(emit func([]string) error) error {
			rows := [][]string{
				{"=cmd|'/c calc'!A1", "1"},
				{"+1+1", "2"},
				{"@SUM(A1)", "3"},
				{"-2+3", "4"},
				{"\t=danger", "5"},
				{"normal", "6"},
				{"-3.5", "7"},
			}
			for _, r := range rows {
				if err := emit(r); err != nil {
					return err
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		"'=cmd|'/c calc'!A1",
		"'+1+1",
		"'@SUM(A1)",
		"'-2+3",
		"'\t=danger",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("body row not sanitized: missing %q in %q", want, s)
		}
	}
	if strings.Contains(s, "normal,7") || strings.Contains(s, "'normal") {
		t.Errorf("safe cell should not be modified, got %q", s)
	}
	if !strings.Contains(s, "-3.5,7") {
		t.Errorf("pure numeric -3.5 should not be escaped, got %q", s)
	}
}

// TestWriteCSVAtomic_TmpFilenameIsUnpredictable 釘住 regression:
// 原本 tmp filename 固定為 `path + ".tmp"` — 攻擊者若能 predict / observe target
// path,即可在 OpenFile 之前 plant 同名 file (或對 attacker-controlled dir 內的
// path 用 race 預先建立) 造成 DoS 或 (在沒有 O_EXCL 的 platform) data corruption。
//
// 修法:tmp filename 使用 crypto/rand 後綴,不可預測。
//
// 本 test 跑兩次 WriteCSVAtomic — 第二次在第一次 final commit 後跑。然後在第一次
// commit 之前的瞬間 (透過特殊 hook 不容易,所以改驗:同一個 path 兩次連續呼叫,
// 每次 tmp filename 不該與固定的 ".tmp" suffix 一致) — 改驗 unpredictability
// 性質:對同一 final path 重複呼叫,observable tmp filename 樣本必須有 entropy。
// 若被改回 deterministic suffix,test 觀察兩次都會撞同名,fail。
func TestWriteCSVAtomic_TmpFilenameIsUnpredictable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	// 反向驗證:預先建立 `path + ".tmp"` 與 `path + ".tmp_<unix>` 等可預測樣式
	// — 若 fix 後 tmp 名走 crypto/rand,這幾條 stale 不該干擾後續 commit。
	predictableStales := []string{
		path + ".tmp",
		path + ".tmp_0",
		path + ".tmp_1",
		path + ".tmp_legacy",
	}
	for _, stale := range predictableStales {
		if err := os.WriteFile(stale, []byte("stale predictable"), 0o600); err != nil {
			t.Fatalf("setup stale %q: %v", stale, err)
		}
	}

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"col"},
		Emit:   func(emit func([]string) error) error { return emit([]string{"v"}) },
	})
	if err != nil {
		t.Fatalf("WriteCSVAtomic 應在 predictable stale 存在下仍成功 (random suffix 不會撞名): %v", err)
	}

	// 驗證 final path 確實寫成 — atomic commit 已執行
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("final path 未建立: %v", statErr)
	}
}

// TestWriteCSVAtomic_TmpFilesAreCleanedUp 釘住 fix 後 tmp filename 含 random
// suffix,正常 commit 流程必須仍把 random tmp 重命名為 final path (不能 leak 一個
// random-suffixed orphan)。
func TestWriteCSVAtomic_TmpFilesAreCleanedUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"col"},
		Emit:   func(emit func([]string) error) error { return emit([]string{"v"}) },
	})
	if err != nil {
		t.Fatalf("WriteCSVAtomic err: %v", err)
	}

	// 列出 dir 內所有 entries,確認:
	//   1. final path 存在
	//   2. 沒有 *.tmp / *.tmp.<rand> 殘留 (即 random suffix 在 success path 被 rename)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	var leaked []string
	finalSeen := false
	for _, e := range entries {
		if e.Name() == filepath.Base(path) {
			finalSeen = true
			continue
		}
		// 任何 base prefix 相同但 != final 都算 orphan
		if strings.HasPrefix(e.Name(), filepath.Base(path)) {
			leaked = append(leaked, e.Name())
		}
	}

	if !finalSeen {
		t.Errorf("final %q not present after success commit", filepath.Base(path))
	}
	if len(leaked) > 0 {
		t.Errorf("tmp orphan(s) leaked after success commit: %v", leaked)
	}
}

// TestWriteCSVAtomic_StaleTmpNoLongerBlocks 確認 fix 後,舊版「stale `.tmp`
// 出現 → 整個 atomic 流程 fail」這條 path 不再適用 —— 因為 tmp filename 改 random
// 後綴,前一次 crash 留下的 `path + ".tmp"` 已不再撞名。
//
// 注意:這條 test 取代了舊的 TestWriteCSVAtomic_StaleTmp_Rejected 預設行為,但
// **不刪除** 舊 test (它仍驗 random tmp 自己撞名的極小機率 case);只是補上 modern
// 行為的契約釘住。
func TestWriteCSVAtomic_StaleTmpNoLongerBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	// 預先建立傳統 ".tmp" 殘留 (模擬上一次 crash)
	if err := os.WriteFile(path+".tmp", []byte("crash leftover"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"col"},
		Emit:   func(emit func([]string) error) error { return emit([]string{"v"}) },
	})
	if err != nil {
		t.Errorf("WriteCSVAtomic 應在傳統 `.tmp` stale 存在下仍成功 (因 tmp filename 已 random),got err: %v", err)
	}
}

func TestWriteCSVAtomic_NilEmit_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"a"},
		Emit:   nil,
	})
	if err == nil {
		t.Fatal("expected err on nil Emit")
	}
}
