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
		{"nbsp_equals", " =BAD()", "' =BAD()"},                       // U+00A0 NBSP
		{"zero_width_space_equals", "\u200b=BAD()", "'\u200b=BAD()"}, // U+200B
		{"ideographic_space_equals", "　=BAD()", "'　=BAD()"},          // U+3000 全形空格
		{"fullwidth_equals", "＝SUM(1+1)", "'＝SUM(1+1)"},              // U+FF1D 全形等號
		{"fullwidth_equals_with_space", " ＝BAD()", "' ＝BAD()"},       // 空白 + 全形等號
		{"unicode_minus_alpha", "−HYPERLINK(x)", "'−HYPERLINK(x)"},   // U+2212 + 非數字
		{"unicode_minus_number_escaped", "−1", "'−1"},                // U+2212 + 數字（ParseFloat 不過 → escape，trade-off）

		// 確認 ASCII 數字仍然 round-trip（防止 unicode minus 邏輯誤傷）
		{"ascii_minus_number_pass", "-1", "-1"},
		{"ascii_plus_number_pass", "+1", "+1"},
	}

	for _, tc := range cases {
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

// TestSanitizeBodyRow_BehaviorEquivalentToHeader 釘住 修法:
// SanitizeBodyRow 是 caller-意圖 alias,行為必須與 SanitizeHeaderRow 完全等價
// (per-cell sanitize + fresh slice + 不修改 input)。
// 若未來分歧(例如 body 不再 sanitize 某些 prefix),test 會立刻 fail 把分歧
// 點固定下來。
func TestSanitizeBodyRow_BehaviorEquivalentToHeader(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		nil,
		{},
		{"plain", "data"},
		{"=BAD()", "@SUM", " +CMD", "-1.5", "ok"},
		{"肌肉群A", "0.5"},
		{"'=already_escaped", "'@quoted"},
	}

	for i, in := range cases {
		gotHeader := SanitizeHeaderRow(in)
		gotBody := SanitizeBodyRow(in)

		if (gotHeader == nil) != (gotBody == nil) {
			t.Errorf("case %d nil mismatch: header=%v body=%v", i, gotHeader == nil, gotBody == nil)
			continue
		}

		if len(gotHeader) != len(gotBody) {
			t.Errorf("case %d len mismatch: header=%d body=%d", i, len(gotHeader), len(gotBody))
			continue
		}

		for j := range gotHeader {
			if gotHeader[j] != gotBody[j] {
				t.Errorf("case %d cell[%d]: header=%q body=%q (alias 必須行為等價)",
					i, j, gotHeader[j], gotBody[j])
			}
		}
	}
}

// TestSanitizeBodyRow_DoesNotAliasInput 同 SanitizeHeaderRow_DoesNotAliasInput:
// SanitizeBodyRow 也必須回 fresh slice。
func TestSanitizeBodyRow_DoesNotAliasInput(t *testing.T) {
	t.Parallel()

	in := []string{"=BAD"}
	got := SanitizeBodyRow(in)
	got[0] = "MUTATED"

	if in[0] != "=BAD" {
		t.Errorf("input was mutated: %q (SanitizeBodyRow must return fresh slice)", in[0])
	}
}

// TestSanitizeBodyRow_NilInput 同 SanitizeHeaderRow_NilInput:nil → nil。
func TestSanitizeBodyRow_NilInput(t *testing.T) {
	t.Parallel()

	if got := SanitizeBodyRow(nil); got != nil {
		t.Errorf("SanitizeBodyRow(nil) = %v, want nil", got)
	}
}

// TestUnsanitizeCell 鎖定 修法：提供 round-trip helper 反轉 SanitizeCellForWrite
// 加上的單引號前綴。
//
// 設計約束（與 spec 一致）：
//   - 僅當去掉開頭 `'` 後的剩餘部分**會**被 SanitizeCellForWrite 再次標記成 dangerous 時，
//     才視為「sanitized 前綴」並 strip。
//   - 原本就是 `'foo`（純文字、不會觸發 sanitize）的 input — 例如 `'hello` — 保持原樣。
//     這是不可避免的 ambiguity：sanitize 不是完全可逆的。
func TestUnsanitizeCell(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		// Round-trip happy path: sanitized output unsanitizes back to original
		{"equals_unsanitize", "'=BAD()", "=BAD()"},
		{"at_unsanitize", "'@SUM(1+1)", "@SUM(1+1)"},
		{"plus_alpha_unsanitize", "'+abc", "+abc"},
		{"minus_alpha_unsanitize", "'-abc", "-abc"},
		{"space_equals_unsanitize", "' =BAD()", " =BAD()"},
		{"nbsp_equals_unsanitize", "' =BAD()", " =BAD()"}, // U+00A0
		{"fullwidth_equals_unsanitize", "'＝SUM", "＝SUM"},
		{"unicode_minus_alpha_unsanitize", "'−HYPERLINK(x)", "−HYPERLINK(x)"},

		// Cells with leading `'` whose remainder is NOT dangerous: must stay verbatim
		// — sanitize wouldn't have added that prefix, so we can't tell it from user data.
		{"quoted_plain_text", "'hello", "'hello"},
		{"quoted_number", "'123", "'123"},
		{"quoted_empty_after_quote", "'", "'"},

		// Cells without leading `'`: identity
		{"plain_text", "Hello", "Hello"},
		{"empty_string", "", ""},
		{"number", "1.234", "1.234"},
		{"chinese_text", "肌肉群A", "肌肉群A"},
		{"plain_equals_no_prefix", "=BAD", "=BAD"}, // we don't add quotes; passthrough
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := UnsanitizeCell(tc.in)
			if got != tc.want {
				t.Errorf("UnsanitizeCell(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestUnsanitizeCell_RoundTrip 驗證 dangerous 輸入經 Sanitize → Unsanitize 後完全還原。
// 注意：對「已經是 'foo 但 foo 安全」的 input，round-trip 不對稱（見 spec）。
func TestUnsanitizeCell_RoundTrip(t *testing.T) {
	t.Parallel()

	dangerousInputs := []string{
		"=cmd|/c calc!A1",
		"@SUM(1+1)",
		"+SUM(1,1)",
		"-HYPERLINK(\"http://e\",\"x\")",
		" =BAD()",
		"\u200b=BAD()",
		"＝SUM(1+1)",
		"−HYPERLINK(x)",
	}

	for _, original := range dangerousInputs {
		sanitized := SanitizeCellForWrite(original)
		recovered := UnsanitizeCell(sanitized)
		if recovered != original {
			t.Errorf("round-trip mismatch for %q: sanitized=%q recovered=%q",
				original, sanitized, recovered)
		}
	}
}
