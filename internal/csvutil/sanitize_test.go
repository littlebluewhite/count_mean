package csvutil

import (
	"testing"
)

func TestSanitizeCellForWrite(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		// Direct formula starters — all prefixed with single quote
		{"equals_prefix", "=cmd|/c calc!A1", "'=cmd|/c calc!A1"},
		{"at_prefix", "@SUM(1+1)", "'@SUM(1+1)"},
		{"tab_equals", "\t=BAD()", "'\t=BAD()"},
		{"cr_equals", "\r=BAD()", "'\r=BAD()"},
		{"lf_equals", "\n=BAD()", "'\n=BAD()"},

		// +/- prefix on non-numeric content is ALWAYS escaped after Wave 6 review
		// (codex C-2): previous policy only escaped when content also contained
		// `=@|!`, which let `+SUM(1,1)` / `-HYPERLINK(...)` through.
		{"plus_with_pipe", "+1|cmd", "'+1|cmd"},
		{"minus_with_eq", "-a=b", "'-a=b"},
		{"plus_with_at", "+@abc", "'+@abc"},
		{"minus_with_bang", "-!fn", "'-!fn"},
		{"plus_SUM_formula", "+SUM(1,1)", "'+SUM(1,1)"},
		{"minus_HYPERLINK_formula", "-HYPERLINK(\"http://e\",\"x\")", "'-HYPERLINK(\"http://e\",\"x\")"},
		{"plus_alpha_only", "+abc", "'+abc"},
		{"minus_alpha_only", "-abc", "'-abc"},

		// +/- prefix NOT triggering (valid number, regardless of suffix)
		{"plus_number", "+1.5", "+1.5"},
		{"minus_number", "-3", "-3"},
		{"plus_scientific", "+1e10", "+1e10"},
		{"minus_zero", "-0", "-0"},

		// Leading-whitespace-then-formula is escaped (codex C-2): TrimLeft before
		// starter detection so attackers can't smuggle formulas behind a space.
		{"space_equals", " =BAD()", "' =BAD()"},
		{"space_at", "  @SUM", "'  @SUM"},
		{"space_plus_SUM", " +SUM(1)", "' +SUM(1)"},

		// Safe content
		{"empty", "", ""},
		{"plain_text", "Hello", "Hello"},
		{"header_name", "EMG-Channel-1", "EMG-Channel-1"},
		{"number_no_prefix", "1.234", "1.234"},
		{"chinese_header", "肌肉群A", "肌肉群A"},
		{"already_quoted", "'=BAD", "'=BAD"},

		// Edge: tab/cr/lf NOT followed by '=' should not trigger (mirrors read-side)
		{"tab_only", "\tplain", "\tplain"},
		{"cr_only", "\rplain", "\rplain"},

		// Wave 7 security: Unicode whitespace / Unicode operator bypass — 攻擊者
		// 用 NBSP / 零寬空格 / 全形等號 / Unicode minus 等繞過 ASCII-only 偵測。
		{"nbsp_equals", " =BAD()", "' =BAD()"},                 // U+00A0 NBSP
		{"zero_width_space_equals", "​=BAD()", "'​=BAD()"},     // U+200B
		{"ideographic_space_equals", "　=BAD()", "'　=BAD()"},    // U+3000 全形空格
		{"fullwidth_equals", "＝SUM(1+1)", "'＝SUM(1+1)"},                  // U+FF1D 全形等號
		{"fullwidth_equals_with_space", " ＝BAD()", "' ＝BAD()"},           // 空白 + 全形等號
		{"unicode_minus_alpha", "−HYPERLINK(x)", "'−HYPERLINK(x)"},        // U+2212 + 非數字
		{"unicode_minus_number_escaped", "−1", "'−1"},                     // U+2212 + 數字（ParseFloat 不過 → escape，trade-off）

		// 確認 ASCII 數字仍然 round-trip（防止 unicode minus 邏輯誤傷）
		{"ascii_minus_number_pass", "-1", "-1"},
		{"ascii_plus_number_pass", "+1", "+1"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := SanitizeCellForWrite(tc.in)
			if got != tc.want {
				t.Errorf("SanitizeCellForWrite(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeCellForWrite_Idempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"=BAD", "@SUM", "+1|x", "-a=b", "'=BAD", "safe",
		" =BAD", "＝SUM", "−HYPERLINK(x)", // Wave 7 Unicode 也要 idempotent
	}
	for _, in := range inputs {
		once := SanitizeCellForWrite(in)
		twice := SanitizeCellForWrite(once)
		if once != twice {
			t.Errorf("not idempotent for %q: once=%q twice=%q", in, once, twice)
		}
	}
}

// TestSanitizeAllRows 鎖定 csv_handler.WriteCSV 用此函式做單一 chokepoint 的契約：
// header + body 每 cell 都過 sanitize，且不修改原 input。
func TestSanitizeAllRows(t *testing.T) {
	t.Parallel()

	in := [][]string{
		{"Time", "EMG-1"},                  // headers (safe)
		{"=cmd|/c calc!A1 最大值", "0.5"},     // body-row label (formula injection vector)
		{" +SUM(1)", "1.0"},           // NBSP-prefixed formula in body
	}
	got := SanitizeAllRows(in)

	want := [][]string{
		{"Time", "EMG-1"},
		{"'=cmd|/c calc!A1 最大值", "0.5"},
		{"' +SUM(1)", "1.0"},
	}

	if len(got) != len(want) {
		t.Fatalf("row count mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("row %d cell count mismatch: got %d, want %d", i, len(got[i]), len(want[i]))
		}
		for j := range got[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("[%d][%d]: got %q, want %q", i, j, got[i][j], want[i][j])
			}
		}
	}

	// 原 input 不被改寫（fresh slice 契約）
	if in[1][0] != "=cmd|/c calc!A1 最大值" {
		t.Errorf("input row 1 was mutated: %q", in[1][0])
	}
}

func TestSanitizeAllRows_NilInput(t *testing.T) {
	t.Parallel()
	if got := SanitizeAllRows(nil); got != nil {
		t.Errorf("SanitizeAllRows(nil) = %v, want nil", got)
	}
}

func TestSanitizeHeaderRow(t *testing.T) {
	t.Parallel()

	in := []string{"Time", "=BAD()", "EMG-1", "@SUM(1)", ""}
	want := []string{"Time", "'=BAD()", "EMG-1", "'@SUM(1)", ""}
	got := SanitizeHeaderRow(in)

	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSanitizeHeaderRow_NilInput(t *testing.T) {
	t.Parallel()

	if got := SanitizeHeaderRow(nil); got != nil {
		t.Errorf("SanitizeHeaderRow(nil) = %v, want nil", got)
	}
}

func TestSanitizeHeaderRow_DoesNotAliasInput(t *testing.T) {
	t.Parallel()

	in := []string{"=BAD"}
	got := SanitizeHeaderRow(in)
	got[0] = "MUTATED"

	if in[0] != "=BAD" {
		t.Errorf("input was mutated: %q (sanitizer must return fresh slice)", in[0])
	}
}
