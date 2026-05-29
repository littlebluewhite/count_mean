package filename

import (
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSanitize_PathTraversal 是 cross-compare review 的 regression test
// （d45ee1f Wave 2 修了 cci_handlers.go params.Subject 但漏修 chart_helpers.go params.Title，
// 同樣症狀的對稱漏洞）。本 test 釘住：Sanitize 的輸出無論如何不可被 filepath.Join
// 解讀為「跳出目錄」— 把 / 與 \ 都替換為 _，剩餘的 .. 只會成為合法 filename 的一部分。
func TestSanitize_PathTraversal(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		expectIn string // 結果應包含此 substring（已 sanitize 後）
		mustNot  string // 結果絕對不能含此 substring（會造成 traversal）
	}{
		{"single_dotdot", "../etc/passwd", "..", "/"},
		{"nested_traversal", "../../../etc/passwd", "..", "/"},
		{"backslash_traversal", `..\..\Windows\System32`, "..", `\`},
		{"absolute_unix", "/etc/passwd", "_etc_passwd", "/"},
		{"absolute_windows", `C:\Windows\Temp`, "C_", `\`},
		{"colon_drive", "C:foo", "C_foo", ":"},
		{"wildcards", `*?<>|"`, "______", "*"},
		{"space_normalization", "Subject Name 1", "Subject_Name_1", " "},
		{"safe_input_passthrough", "subject-001_RA-ES", "subject-001_RA-ES", "/"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Sanitize(tc.input)
			if !strings.Contains(got, tc.expectIn) {
				t.Errorf("Sanitize(%q) = %q; expected to contain %q",
					tc.input, got, tc.expectIn)
			}
			if tc.mustNot != "" && strings.Contains(got, tc.mustNot) {
				t.Errorf("Sanitize(%q) = %q; must NOT contain %q (path-traversal risk)",
					tc.input, got, tc.mustNot)
			}
			// 額外驗證：sanitized 結果與 OutputDir 組合後，filepath.Clean 不會逃出。
			// 用 t.TempDir() 取代寫死 /tmp/output，cross-platform 自動正確。
			outputDir := t.TempDir()
			joined := filepath.Join(outputDir, got)
			cleaned := filepath.Clean(joined)
			if !strings.HasPrefix(cleaned, outputDir) {
				t.Errorf("filepath.Join(%q, Sanitize(%q)=%q) → %q escapes OutputDir",
					outputDir, tc.input, got, cleaned)
			}
		})
	}
}

// TestSanitize_EmptyAndUnicode 確保邊界輸入不 panic。
func TestSanitize_EmptyAndUnicode(t *testing.T) {
	t.Run("empty_string", func(t *testing.T) {
		// empty string fallback 為 "untitled" 避免空 filename 觸發 OS 錯誤。
		if got := Sanitize(""); got != "untitled" {
			t.Errorf("empty input should fallback to 'untitled', got %q", got)
		}
	})
	t.Run("reserved_name_avoided", func(t *testing.T) {
		// Windows reserved name 加 _safe 後綴避開保留字
		if got := Sanitize("CON"); got != "CON_safe" {
			t.Errorf("reserved name CON should get _safe suffix, got %q", got)
		}
	})
	t.Run("unicode_preserved", func(t *testing.T) {
		// 中文等 Unicode 字元不在 replacement table 中，應原樣保留
		got := Sanitize("受測者甲")
		if got != "受測者甲" {
			t.Errorf("unicode passthrough: want %q got %q", "受測者甲", got)
		}
	})
}

// TestSanitize_ReservedNameWithExtension 守護
// Windows reserved name 規則是「stem (first dot 前) 匹配」,不是「整個 filename 匹配」。
// "CON.csv" / "CON.tar.gz" / "PRN.log" 在 Windows 上會被 OS 當成 reserved device
// 開啟,過去只擋 "CON" alone 是不夠的。
func TestSanitize_ReservedNameWithExtension(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"CON.csv", "CON_safe.csv"},
		{"con.csv", "con_safe.csv"}, // case-insensitive
		{"CON.tar.gz", "CON_safe.tar.gz"},
		{"PRN.log", "PRN_safe.log"},
		{"AUX.txt", "AUX_safe.txt"},
		{"NUL.dat", "NUL_safe.dat"},
		{"COM1.bin", "COM1_safe.bin"},
		{"LPT9.tmp", "LPT9_safe.tmp"},
		// 非 reserved stem 不該被改
		{"CONCAT.csv", "CONCAT.csv"},
		{"PRINT.log", "PRINT.log"},
		// reserved word 在 stem 中間不算 reserved
		{"my_CON_file.csv", "my_CON_file.csv"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := Sanitize(tc.input)
			if got != tc.expected {
				t.Errorf("Sanitize(%q) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// TestSanitize_RuneBoundaryTruncation 守護
// 200 byte 截斷必須在 valid UTF-8 rune 邊界,不能切到 multi-byte rune 中間。
// 過去用 `cleaned[:maxLen]` 可能切到 3-byte 中文中間,產生 invalid UTF-8。
func TestSanitize_RuneBoundaryTruncation(t *testing.T) {
	t.Run("chinese_runes_no_truncation_within_rune", func(t *testing.T) {
		// 每個中文字 3 bytes,200 bytes ≈ 66.67 個中文字。
		// 構造剛好需要截斷的長度 (70 個中文字 = 210 bytes > 200)。
		input := strings.Repeat("一", 70)
		got := Sanitize(input)

		// 結果必須 <= 200 bytes
		if len(got) > 200 {
			t.Errorf("truncated length %d exceeds maxLen 200", len(got))
		}
		// 結果必須是 valid UTF-8 (沒有切到 rune 中間)
		if !utf8.ValidString(got) {
			t.Errorf("truncated result is not valid UTF-8: %q (%v bytes)", got, len(got))
		}
		// 應該保留 66 個中文字 (198 bytes) — 不能多收第 67 個 (會超過 200)
		runeCount := utf8.RuneCountInString(got)
		if runeCount != 66 {
			t.Errorf("expected 66 chinese runes (198 bytes), got %d (%d bytes)", runeCount, len(got))
		}
	})

	t.Run("mixed_ascii_chinese", func(t *testing.T) {
		// 100 個 ASCII (100 bytes) + 50 個中文 (150 bytes) = 250 bytes
		input := strings.Repeat("a", 100) + strings.Repeat("一", 50)
		got := Sanitize(input)

		if len(got) > 200 {
			t.Errorf("truncated length %d exceeds maxLen 200", len(got))
		}
		if !utf8.ValidString(got) {
			t.Errorf("truncated result is not valid UTF-8: %q", got)
		}
	})

	t.Run("pure_ascii_truncation_unchanged", func(t *testing.T) {
		// 全 ASCII (1 byte per rune) 行為與舊版完全一致
		input := strings.Repeat("a", 250)
		got := Sanitize(input)
		if len(got) != 200 {
			t.Errorf("pure ASCII truncation should give exactly 200 bytes, got %d", len(got))
		}
	})

	t.Run("under_max_unchanged", func(t *testing.T) {
		// 200 bytes 以下不該被改
		input := strings.Repeat("一", 60) // 180 bytes
		got := Sanitize(input)
		if got != input {
			t.Errorf("short input should be unchanged: want %q got %q", input, got)
		}
	})
}

// TestSanitize_LeadingDotReservedName 守護
//
// past stem 取法 `SplitN(name, ".", 2)[0]` 對 ".CON.csv" 會回 ""
// → 空 stem 跑進 reserved-name switch 永遠不命中 → leading-dot 變形完全
// 逃過 reserved-name 守門。Windows NT path parser 在比對 reserved name table
// 之前會 strip leading dot,所以 ".CON.csv" 在 Windows 上仍會被當成 device file
// 而 reject(或在某些版本寫入打開實體 console)— 這是實質的 bypass。
//
// 修法:先 TrimLeft "." 再取 stem 比對,保留原 leading dot 在輸出。
func TestSanitize_LeadingDotReservedName(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		// 基本 leading-dot reserved name
		{".CON.csv", ".CON_safe.csv"},
		{".PRN.log", ".PRN_safe.log"},
		{".NUL.txt", ".NUL_safe.txt"},
		{".AUX", ".AUX_safe"},
		// case-insensitive
		{".con.csv", ".con_safe.csv"},
		{".Nul.dat", ".Nul_safe.dat"},
		// multi-dot prefix(極端情況,NT 仍 strip)
		{"..CON.csv", "..CON_safe.csv"},
		{"...NUL.txt", "...NUL_safe.txt"},
		// 多 numbered reserved
		{".COM1.bin", ".COM1_safe.bin"},
		{".LPT9.tmp", ".LPT9_safe.tmp"},
		// 多副檔名
		{".CON.tar.gz", ".CON_safe.tar.gz"},
		// 控制組:dot prefix + 非 reserved stem 不該被改
		{".concat.csv", ".concat.csv"},
		{".myfile.csv", ".myfile.csv"},
		// 控制組:無 dot prefix 的 reserved name 走原本路徑(既有測試覆蓋,此處再次驗證仍正常)
		{"CON.csv", "CON_safe.csv"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := Sanitize(tc.input)
			if got != tc.expected {
				t.Errorf("Sanitize(%q) = %q, want %q (leading-dot reserved-name bypass)",
					tc.input, got, tc.expected)
			}
		})
	}
}
