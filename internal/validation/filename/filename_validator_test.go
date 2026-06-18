package filename

import (
	"strings"
	"testing"
)

func TestValidator_RejectsPathSeparators(t *testing.T) {
	v := NewValidator()
	tests := []string{
		"../etc/passwd",
		"..\\Windows\\System32",
		"foo/bar.csv",
		"foo\\bar.csv",
		"/absolute.csv",
		"\\windows.csv",
	}
	for _, in := range tests {
		t.Run(in, func(t *testing.T) {
			err := v.ValidateFilename(in)
			if err == nil {
				t.Errorf("expected error for path-separator-containing %q, got nil", in)
				return
			}
			if !strings.Contains(err.Error(), "路徑分隔符") {
				t.Errorf("expected '路徑分隔符' error message for %q, got %v", in, err)
			}
		})
	}
}

// 用 numeric rune literal 組合字串，避免 Go compiler 對 source 內字面 BOM / RTL
// override / ZWSP 的「illegal byte order mark」與相關 lint 警告。
func mkFmtFilename(r rune) string {
	return "a" + string(r) + "b.csv"
}

func TestValidator_RejectsUnicodeFormatChars(t *testing.T) {
	v := NewValidator()
	tests := []struct {
		name string
		in   string
	}{
		{"RTL override (U+202E)", "evil" + string(rune(0x202E)) + "exe.csv"},
		{"zero-width space (U+200B)", mkFmtFilename(rune(0x200B))},
		{"zero-width joiner (U+200D)", "a" + string(rune(0x200D)) + "CON.csv"},
		{"bidi LRI (U+2066)", mkFmtFilename(rune(0x2066))},
		{"word joiner (U+2060)", mkFmtFilename(rune(0x2060))},
		{"BOM (U+FEFF) mid-name", mkFmtFilename(rune(0xFEFF))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := v.ValidateFilename(tc.in)
			if err == nil {
				t.Errorf("expected error for Unicode-format-char-containing %q, got nil", tc.in)
				return
			}
			if !strings.Contains(err.Error(), "Unicode") {
				t.Errorf("expected 'Unicode' error message for %q, got %v", tc.in, err)
			}
		})
	}
}

func TestValidator_AcceptsNormalCSVFilename(t *testing.T) {
	v := NewValidator()
	cases := []string{
		"data.csv",
		"SF8_2026_05_17.csv",
		"normalize_result.csv",
		"中文檔名.csv",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if err := v.ValidateFilename(in); err != nil {
				t.Errorf("expected nil err for safe filename %q, got %v", in, err)
			}
		})
	}
}

func TestValidator_RejectsEmptyAndWhitespace(t *testing.T) {
	v := NewValidator()
	for _, in := range []string{"", "   ", "\t\t"} {
		if err := v.ValidateFilename(in); err == nil {
			t.Errorf("expected error for empty/whitespace %q, got nil", in)
		}
	}
}

func TestValidator_RejectsControlChars(t *testing.T) {
	v := NewValidator()
	if err := v.ValidateFilename("a\x01b.csv"); err == nil {
		t.Errorf("expected error for control char, got nil")
	}
	if err := v.ValidateFilename("a\x00b.csv"); err == nil {
		t.Errorf("expected error for null byte, got nil")
	}
}

// TestValidator_RejectsTabInFilename 釘住 L15：base filename 中的 tab character
// 在實務上不合理（檔案總管 / CLI 對含 tab 的檔名顯示破碎，shell completion 與
// 拷貝貼上幾乎一定壞掉），且 tab 也算 unicode.IsControl，與其他控制字元同類但
// 原本被「\t 例外」放行。
//
// 修法：移除 checkControlChars 內 `r != '\t'` 的例外，與 cell-level 的 H10
// 鬆綁無關（那是 CSV cell 內容的 quote/escape 場景；filename 不同）。
func TestValidator_RejectsTabInFilename(t *testing.T) {
	v := NewValidator()

	tabCases := []string{
		"data\t.csv",
		"\tdata.csv",
		"data.csv\t",
		"hello\tworld.csv",
	}
	for _, in := range tabCases {
		t.Run(in, func(t *testing.T) {
			if err := v.ValidateFilename(in); err == nil {
				t.Errorf("expected error for tab-containing filename %q, got nil", in)
			}
		})
	}
}

// TestValidator_RejectsWindowsDriveLetterPrefix 釘住 regression：
// `C:test.csv` 在 Windows 上 (legacy DOS 行為) 解析為「drive C 的 cwd 下的 test.csv」
// — 即便不含 path separator,仍會在 Windows OpenFile 走 drive-relative resolution,
// 跳出 caller 認知的 base directory。原本實作只靠 DetectDangerousChars 偵測 `:`
// 字元間接擋下,defense 過於 fragile:若日後 `:` 為了 ISO timestamp / time literal 鬆綁,
// drive letter 立刻 bypass。
//
// 修法:在 path-separator check 之後加 explicit `^[A-Za-z]:` regex guard,
// 提供獨立的明確 error message,defense-in-depth 不依賴 dangerous char list。
func TestValidator_RejectsWindowsDriveLetterPrefix(t *testing.T) {
	v := NewValidator()

	rejectCases := []string{
		"C:test.csv",
		"c:foo.csv", // 小寫也要擋
		"D:bar.csv",
		"Z:final.csv",
		// drive letter 後接看似合法的內容
		"A:data.csv",
	}
	for _, in := range rejectCases {
		t.Run("reject/"+in, func(t *testing.T) {
			err := v.ValidateFilename(in)
			if err == nil {
				t.Errorf("expected error for Windows drive letter prefix %q, got nil", in)
				return
			}
			if !strings.Contains(err.Error(), "Windows 磁碟代號") &&
				!strings.Contains(err.Error(), "drive letter") {
				t.Errorf("expected drive-letter-specific error for %q, got %v "+
					"(must NOT rely on generic ':' dangerous-char detection — that's fragile if `:` is ever unlisted)",
					in, err)
			}
		})
	}

	// Pass cases: 含 `:` 但非 drive letter prefix
	passCases := []string{
		// 注意:單一 `:` 雖然不在 drive letter 形式,但目前 DetectDangerousChars 仍會擋,
		// 這個 pass case list 是「明確不是 drive letter 也不該被 drive letter check 擋」的範例。
		// (若日後 `:` 從 DangerousChars 移除,以下 case 仍需透過其他守門擋下 — 不是本 fix 範圍。)
	}
	for _, in := range passCases {
		t.Run("pass/"+in, func(t *testing.T) {
			if err := v.ValidateFilename(in); err != nil {
				// 若 `:` 仍在 DangerousChars,這條會 err 但不該是 drive-letter error
				if strings.Contains(err.Error(), "Windows 磁碟代號") {
					t.Errorf("filename %q 不是 drive letter,不該被 drive-letter check 誤擋",
						in)
				}
			}
		})
	}
}

func TestValidator_RejectsReservedNames(t *testing.T) {
	v := NewValidator()
	for _, in := range []string{"CON.csv", "PRN.csv", "COM1.csv", "LPT9.csv"} {
		if err := v.ValidateFilename(in); err == nil {
			t.Errorf("expected error for reserved name %q, got nil", in)
		}
	}
}

// TestFilenameValidator_DoubleExt_ReservedRejected 釘住 V9 PoC:
//
// Windows 上 reserved device name 不限於「stem 緊接最後一個副檔名」的形式。
// `CON.txt.csv` 在 Windows 開啟時 OS 仍會把它 redirect 到 console device(因為
// API 解析時對於含 dot 的檔名,first segment 是 reserved 就照樣 routing)。
//
// 原本實作只用 `TrimSuffix(filename, filepath.Ext(filename))` 剝最後一個副檔名,
// `CON.txt.csv` -> `CON.txt` -> 比對 reserved list 不命中 -> 漏放;
// 同理 `PRN.bak.csv`、`AUX.log.csv`、`NUL.dat.csv`、`COM1.x.csv`、`LPT1.y.csv` 全部 bypass。
//
// 修法:對 `strings.Split(name, ".")` 每一段(case-insensitive)比對 reserved list,
// 任一段命中即 reject。
//
// Pass cases 釘住:
//   - `consult.csv` / `legitimate.txt` 雖含子字串 `CON` / `LPT` 但 first segment
//     是 `consult` / `legitimate` (非 reserved exact match),應通過 reserved check。
//     (note: `legitimate.txt` 因 extension 不是 .csv 仍會被 reject 於 checkExtension,
//     所以這裡只驗 `consult.csv` pass 整段、`subject_id_con.csv` 也 pass — reserved word
//     只能整段比對,子字串/前綴/後綴都不算。)
//   - `.bak.csv` (no name) — 行為自選,目前 first segment 為空 -> reserved 比對不命中
//     -> 走 extension check -> .csv 通過。實務上 `.bak.csv` 是隱藏檔語意,允許通過 OK,
//     若日後要 reject 由其他 check 負責(非本 task 範圍)。
func TestValidator_RejectsTooLongDangerousCharAndBadExtension(t *testing.T) {
	v := NewValidator()

	cases := []struct {
		name string
		in   string
	}{
		// 用可印字元而非 NUL bytes:make([]byte,300) 是 300 個 \x00,會在
		// checkControlChars(null/control 守門)就被拒,根本走不到 len>255 的
		// 長度分支 → 長度檢查被刪測試仍綠,失去鑑別力。改 300 個 'a' 強制走到長度檢查。
		{"too long", strings.Repeat("a", 300) + ".csv"},
		{"dangerous char", "test<.csv"},
		{"invalid extension", "test.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := v.ValidateFilename(tc.in); err == nil {
				t.Errorf("expected error for %q, got nil", tc.in)
			}
		})
	}
}

func TestFilenameValidator_DoubleExt_ReservedRejected(t *testing.T) {
	v := NewValidator()

	rejectCases := []string{
		"CON.txt.csv",
		"PRN.bak.csv",
		"AUX.log.csv",
		"NUL.dat.csv",
		"COM1.x.csv",
		"LPT1.y.csv",
		// case-insensitive: lowercase reserved 也要擋
		"con.tar.csv",
	}
	for _, in := range rejectCases {
		t.Run("reject/"+in, func(t *testing.T) {
			if err := v.ValidateFilename(in); err == nil {
				t.Errorf("expected error for double-ext reserved-name %q, got nil", in)
			} else if !strings.Contains(err.Error(), "保留字") {
				t.Errorf("expected '保留字' error for %q, got %v", in, err)
			}
		})
	}

	passCases := []string{
		// 子字串看起來像 reserved 但 first segment != reserved exact match
		"consult.csv",
		"subject_id_con.csv",
		// 帶 reserved-like prefix 但完整 segment 不是 reserved word
		"CONCAT.csv",
		// 隱藏檔 `.bak.csv`: empty first segment,reserved check 不命中,extension OK
		".bak.csv",
	}
	for _, in := range passCases {
		t.Run("pass/"+in, func(t *testing.T) {
			if err := v.ValidateFilename(in); err != nil {
				t.Errorf("expected nil err for legitimate %q, got %v", in, err)
			}
		})
	}
}
