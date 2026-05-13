package calculator_test

import (
	"path/filepath"
	"strings"
	"testing"

	"count_mean/internal/calculator"
)

// TestSanitizeFileName_PathTraversal 是 cross-compare review 的 regression test
// （d45ee1f Wave 2 修了 cci_handlers.go params.Subject 但漏修 chart_helpers.go params.Title，
// 同樣症狀的對稱漏洞）。本 test 釘住：SanitizeFileName 的輸出無論如何不可被 filepath.Join
// 解讀為「跳出目錄」— 把 / 與 \ 都替換為 _，剩餘的 .. 只會成為合法 filename 的一部分。
func TestSanitizeFileName_PathTraversal(t *testing.T) {
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := calculator.SanitizeFileName(tc.input)
			if !strings.Contains(got, tc.expectIn) {
				t.Errorf("SanitizeFileName(%q) = %q; expected to contain %q",
					tc.input, got, tc.expectIn)
			}
			if tc.mustNot != "" && strings.Contains(got, tc.mustNot) {
				t.Errorf("SanitizeFileName(%q) = %q; must NOT contain %q (path-traversal risk)",
					tc.input, got, tc.mustNot)
			}
			// 額外驗證：sanitized 結果與 OutputDir 組合後，filepath.Clean 不會逃出。
			// 用 t.TempDir() 取代寫死 /tmp/output，cross-platform 自動正確。
			outputDir := t.TempDir()
			joined := filepath.Join(outputDir, got)
			cleaned := filepath.Clean(joined)
			if !strings.HasPrefix(cleaned, outputDir) {
				t.Errorf("filepath.Join(%q, SanitizeFileName(%q)=%q) → %q escapes OutputDir",
					outputDir, tc.input, got, cleaned)
			}
		})
	}
}

// TestSanitizeFileName_EmptyAndUnicode 確保邊界輸入不 panic。
func TestSanitizeFileName_EmptyAndUnicode(t *testing.T) {
	t.Run("empty_string", func(t *testing.T) {
		if got := calculator.SanitizeFileName(""); got != "" {
			t.Errorf("empty input should produce empty output, got %q", got)
		}
	})
	t.Run("unicode_preserved", func(t *testing.T) {
		// 中文等 Unicode 字元不在 replacement table 中，應原樣保留
		got := calculator.SanitizeFileName("受測者甲")
		if got != "受測者甲" {
			t.Errorf("unicode passthrough: want %q got %q", "受測者甲", got)
		}
	})
}
