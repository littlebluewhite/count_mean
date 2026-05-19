package csvutil

import (
	"strings"
	"testing"
)

// TestSanitize_RejectsTabEqualsFormula 釘住 defense-in-depth：
// formulaStarters 內加入 `\t=` / `\r=` / `\n=` 等 ASCII control + starter 的字面 prefix
// 變體，確保即使 isInvisibleLeading 未來 refactor 漏掉 ASCII control whitespace,
// raw cell view 上的 control-leading formula 仍會被擋。
//
// 與 sanitize_test.go 的 TestSanitizeCellForWrite 互補:那邊測試經 isInvisibleLeading
// trim 後的「trimmed view」命中;本 test 直接讓 formulaStarters 在「raw cell」
// view 上自己命中,即使 isInvisibleLeading 邏輯被 break 也守得住。
func TestSanitize_RejectsTabEqualsFormula(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
	}{
		// ASCII control leading + = / ＝ / @
		{"tab_equals", "\t=cmd|/c calc"},
		{"cr_equals", "\r=cmd|/c calc"},
		{"lf_equals", "\n=cmd|/c calc"},
		{"tab_fullwidth_equals", "\t＝SUM(1,1)"},
		{"cr_fullwidth_equals", "\r＝SUM(1,1)"},
		{"lf_fullwidth_equals", "\n＝SUM(1,1)"},
		{"tab_at", "\t@SUM(1+1)"},
		{"cr_at", "\r@SUM(1+1)"},
		{"lf_at", "\n@SUM(1+1)"},
		// ASCII control leading + +/- 開頭(非數字, 應被 ParseFloat-fail path 擋下,
		// 但也由 formulaStarters 字面 prefix 命中, 雙重保險)
		{"tab_plus_formula", "\t+SUM(1,1)"},
		{"cr_plus_formula", "\r+SUM(1,1)"},
		{"lf_plus_formula", "\n+SUM(1,1)"},
		{"tab_minus_formula", "\t-HYPERLINK(\"x\",\"y\")"},
		{"cr_minus_formula", "\r-HYPERLINK(\"x\",\"y\")"},
		{"lf_minus_formula", "\n-HYPERLINK(\"x\",\"y\")"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeCellForWrite(tc.in)
			if !strings.HasPrefix(got, "'") {
				t.Errorf("SanitizeCellForWrite(%q) = %q, 期望被 prefix `'` 保護", tc.in, got)
			}
		})
	}
}

// TestSanitize_DefenseInDepth_RawViewMatch 釘住 設計意圖:formulaStarters 同時
// 在 trimmed view 與 raw cell view 上比對,raw view 上的字面 prefix(含 ASCII
// control whitespace)獨立守門。
//
// 這個 test 不依賴 isInvisibleLeading 的具體行為 — 直接用「不會被 trim 的字元」
// 作為前綴(這裡用 ASCII control 仍會被 unicode.IsSpace 認可,但 test 的契約是
// 「即使未來 isInvisibleLeading 收窄,這些 prefix 仍會被擋」)。
func TestSanitize_DefenseInDepth_RawViewMatch(t *testing.T) {
	t.Parallel()

	// 純粹確認 formulaStarters 確實涵蓋 ASCII control + starter 字面 prefix
	// (內部 invariant test,失敗代表 defense-in-depth 列表被人移除)
	requiredPrefixes := []string{
		"\t=", "\r=", "\n=",
		"\t＝", "\r＝", "\n＝",
		"\t@", "\r@", "\n@",
	}
	for _, want := range requiredPrefixes {
		found := false
		for _, s := range formulaStarters {
			if s == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("formulaStarters 缺少 %q (defense-in-depth 對 isInvisibleLeading regression 的兜底)",
				want)
		}
	}
}
