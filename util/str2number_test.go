package util

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStr2int(t *testing.T) {
	t.Run("test 1", func(t *testing.T) {
		r, err := Str2Number[int, int]("3.70188E-05", 10)
		require.NoError(t, err)
		require.Equal(t, 370188, r)
	})
	t.Run("test 2", func(t *testing.T) {
		r, err := Str2Number[int, int]("0.001356", 6)
		require.NoError(t, err)
		require.Equal(t, 1356, r)
	})
	t.Run("test error", func(t *testing.T) {
		_, err := Str2Number[int, int]("invalid", 6)
		require.Error(t, err)
	})
}

// TestStr2Number_LocaleSafety pins the contract that Str2Number stays
// locale-independent. Go's strconv.ParseFloat / ParseInt are documented as
// locale-agnostic (always expects "." as decimal separator), but we want a
// sentinel regression test so a future refactor that swaps in
// fmt.Sscanf-with-locale or a third-party parser can't silently flip
// semantics — e.g. on a German locale where "3,14" might otherwise look like
// a valid decimal mantissa.
//
// Contract:
//   - "3.14" → parses (always, regardless of host LC_NUMERIC)
//   - "3,14" → error (the comma must not be silently treated as decimal)
//   - "1.234,56" / "1,234.56" → error (European thousands-grouping rejected)
func TestStr2Number_LocaleSafety(t *testing.T) {
	t.Run("dot_decimal_parses", func(t *testing.T) {
		r, err := Str2Number[float64, int]("3.14", 0)
		require.NoError(t, err)
		require.InDelta(t, 3.14, r, 1e-9)
	})

	t.Run("comma_decimal_rejected", func(t *testing.T) {
		_, err := Str2Number[float64, int]("3,14", 0)
		require.Error(t, err)
	})

	t.Run("european_thousands_comma_rejected", func(t *testing.T) {
		_, err := Str2Number[float64, int]("1.234,56", 0)
		require.Error(t, err)
	})

	t.Run("european_dot_thousands_comma_decimal_rejected", func(t *testing.T) {
		_, err := Str2Number[float64, int]("1,234.56", 0)
		require.Error(t, err)
	})
}

// TestStr2Number_ParseFloatParity (P1-A2-5 / P1-A16-3) pins the contract that
// Str2Number is just `strconv.ParseFloat(input) * 10^move` — no manual
// mantissa decomposition. The previous implementation split on uppercase "E"
// and rebuilt mantissa × 10^(move+exp) by hand, which:
//   - silently misbehaved on lowercase "e" inputs in edge cases (because the
//     split returned a single part), and
//   - introduced precision drift vs ParseFloat for pure decimal inputs.
//
// Pure ParseFloat is locale-independent in Go (always "." decimal) and
// natively handles E / e / +exp / -exp uniformly.
func TestStr2Number_ParseFloatParity(t *testing.T) {
	cases := []struct {
		name  string
		input string
		move  int
	}{
		{"pure_decimal_0.1", "0.1", 0},
		{"pure_decimal_0.2", "0.2", 0},
		{"pure_decimal_0.3", "0.3", 0},
		{"pure_decimal_with_move", "0.001356", 6},
		{"upper_E_negative_exp", "3.70188E-05", 10},
		{"lower_e_negative_exp", "3.70188e-05", 10},
		{"upper_E_positive_exp", "1.5E2", 0},
		{"lower_e_positive_exp", "1.5e2", 0},
		{"upper_E_with_move", "1.5E-3", 10},
		{"lower_e_with_move", "1.5e-3", 10},
		{"signed_exp_plus", "1.5E+2", 0},
		{"integer_no_decimal", "42", 0},
		{"integer_with_move", "42", 3},
		{"negative_decimal", "-0.5", 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Str2Number[float64, int](tc.input, tc.move)
			require.NoError(t, err)

			parsed, err := strconv.ParseFloat(tc.input, 64)
			require.NoError(t, err)
			expected := parsed * math.Pow10(tc.move)

			// Use exact equality on bit patterns when finite; both should
			// produce the same FP result via identical (ParseFloat * Pow10)
			// path.
			require.Equalf(t, expected, got,
				"Str2Number(%q, %d) = %v, want %v",
				tc.input, tc.move, got, expected)
		})
	}
}

// TestStr2Number_LowercaseE_NoMantissaSplit guards the precise A16-3 finding:
// the previous implementation split on "E" only, so lowercase "e" inputs that
// happened to also include "E" elsewhere (not realistic but possible) or that
// otherwise tripped the old code path would silently misparse. With ParseFloat
// throughout, both casings yield identical results.
func TestStr2Number_LowercaseE_NoMantissaSplit(t *testing.T) {
	upper, err := Str2Number[float64, int]("3.70188E-05", 10)
	require.NoError(t, err)
	lower, err := Str2Number[float64, int]("3.70188e-05", 10)
	require.NoError(t, err)
	require.Equal(t, upper, lower,
		"uppercase E and lowercase e must produce identical Str2Number output")
}

// TestStr2Number_A16_3_LocaleBlindAndExponentCase locks the exact
// specification: both lowercase "e" and uppercase "E" exponent forms
// must parse to identical numeric output, while a locale-style "1,5" mantissa
// (European comma decimal) must be rejected. The previous mantissa-splitting
// implementation could silently misbehave on lowercase "e" and was locale-blind
// in a fragile way — Batch H (A2-5) replaced it with strconv.ParseFloat which
// has well-defined locale-independent semantics. This test pins those three
// concrete cases so a future refactor can't regress.
func TestStr2Number_A16_3_LocaleBlindAndExponentCase(t *testing.T) {
	t.Run("upper_E_parses", func(t *testing.T) {
		r, err := Str2Number[float64, int]("1.5E-3", 0)
		require.NoError(t, err)
		require.InDelta(t, 0.0015, r, 1e-12)
	})

	t.Run("lower_e_parses_identically", func(t *testing.T) {
		r, err := Str2Number[float64, int]("1.5e-3", 0)
		require.NoError(t, err)
		require.InDelta(t, 0.0015, r, 1e-12)
	})

	t.Run("comma_decimal_rejected", func(t *testing.T) {
		_, err := Str2Number[float64, int]("1,5", 0)
		require.Error(t, err, "comma mantissa (European locale) must not be silently accepted as 1.5")
	})
}

// TestStr2Number_WhitespaceTolerance preserves the previous implementation's
// behaviour for surrounding whitespace. CSV cells and phase-string user
// inputs commonly carry leading/trailing spaces; ParseFloat alone would
// reject them. We TrimSpace before parsing to maintain backward compat.
//
// Internal whitespace ("1. 5") was historically stripped by the old
// ReplaceAll-based mantissa path, but is now rejected — that was never a
// documented contract and accepting it conflicts with strict numeric parsing.
func TestStr2Number_WhitespaceTolerance(t *testing.T) {
	t.Run("trailing_space_accepted", func(t *testing.T) {
		r, err := Str2Number[float64, int]("3.14 ", 0)
		require.NoError(t, err)
		require.InDelta(t, 3.14, r, 1e-9)
	})

	t.Run("leading_space_accepted", func(t *testing.T) {
		r, err := Str2Number[float64, int](" 3.14", 0)
		require.NoError(t, err)
		require.InDelta(t, 3.14, r, 1e-9)
	})

	t.Run("both_sides_space_accepted", func(t *testing.T) {
		r, err := Str2Number[float64, int]("\t 3.14 \n", 0)
		require.NoError(t, err)
		require.InDelta(t, 3.14, r, 1e-9)
	})

	t.Run("internal_space_rejected", func(t *testing.T) {
		_, err := Str2Number[float64, int]("1. 5", 0)
		require.Error(t, err)
	})
}

// TestStr2Number_RejectsNaNInf 釘住 strconv.ParseFloat 對 "NaN" / "+Inf" /
// "-Inf" (含大小寫) 與 exponent overflow 會回 finite-but-NaN / ±Inf 值且 err=nil。
// 過去 Str2Number 直接 return T(NaN * Pow10(move)),caller 拿到 NaN 進 sliding
// window 等下游;新行為應該在中央入口 fail-fast 回 ErrNaNInput / ErrInfInput
// sentinel (errors.Is 可辨識),呼應 sliding window/ normalizer的
// fail-fast 模型。
//
// caller 仍可用 errors.Is(err, util.ErrNaNInput) 客製處理 (例:把 NaN 視為
// missing 並 skip row);中央拒絕只是把「靜默 NaN 進管線」這條長尾 close 掉。
func TestStr2Number_RejectsNaNInf(t *testing.T) {
	t.Run("NaN_literal_rejected_with_sentinel", func(t *testing.T) {
		cases := []string{"NaN", "nan", "NAN", "NaN ", " nan"}
		for _, in := range cases {
			t.Run(in, func(t *testing.T) {
				_, err := Str2Number[float64, int](in, 0)
				require.Error(t, err, "Str2Number(%q) should reject NaN literal", in)
				require.True(t, errors.Is(err, ErrNaNInput),
					"expected ErrNaNInput via errors.Is, got %v", err)
			})
		}
	})

	t.Run("PlusInf_literal_rejected_with_sentinel", func(t *testing.T) {
		cases := []string{"Inf", "+Inf", "Infinity", "+Infinity", "inf", "+inf"}
		for _, in := range cases {
			t.Run(in, func(t *testing.T) {
				_, err := Str2Number[float64, int](in, 0)
				require.Error(t, err, "Str2Number(%q) should reject +Inf literal", in)
				require.True(t, errors.Is(err, ErrInfInput),
					"expected ErrInfInput via errors.Is, got %v", err)
			})
		}
	})

	t.Run("MinusInf_literal_rejected_with_sentinel", func(t *testing.T) {
		cases := []string{"-Inf", "-Infinity", "-inf"}
		for _, in := range cases {
			t.Run(in, func(t *testing.T) {
				_, err := Str2Number[float64, int](in, 0)
				require.Error(t, err, "Str2Number(%q) should reject -Inf literal", in)
				require.True(t, errors.Is(err, ErrInfInput),
					"expected ErrInfInput via errors.Is, got %v", err)
			})
		}
	})

	t.Run("ExponentOverflow_rejected_with_InfSentinel", func(t *testing.T) {
		// 1e400 超過 float64 範圍,ParseFloat 回 +Inf + ErrRange (但仍會回 +Inf 值);
		// 過去 Str2Number 把 Inf cast 進 T 並回 nil error;新行為應該轉成 ErrInfInput。
		// 注意:ParseFloat 對 overflow 同時回 +Inf 與 ErrRange (non-nil err);這條
		// 路徑會走 ParseFloat 的 fmt.Errorf 包裝路,但 caller 看到的 err 仍應包含
		// 「值過大」訊息。此測試 pin 住「不會回 nil err + Inf 值」這個 strict 契約。
		_, err := Str2Number[float64, int]("1e400", 0)
		require.Error(t, err,
			"exponent overflow must surface error rather than silently returning Inf")
	})

	t.Run("HappyPath_normal_finite_still_works", func(t *testing.T) {
		// regression guard:NaN/Inf reject 不可破壞合法 input。
		r, err := Str2Number[float64, int]("3.14", 0)
		require.NoError(t, err)
		require.InDelta(t, 3.14, r, 1e-9)

		r2, err := Str2Number[float64, int]("0.001356", 6)
		require.NoError(t, err)
		require.Equal(t, 1356.0, r2)
	})

	t.Run("ResultIsZero_OnNaNInfReject_NoPartialValue", func(t *testing.T) {
		// 確認 reject path 回 zero value (而非 NaN/Inf):caller 若忽略 err 至少
		// 看到 0 而非 NaN 污染。
		r, err := Str2Number[float64, int]("NaN", 0)
		require.Error(t, err)
		require.Equal(t, 0.0, r, "rejected NaN should yield zero value, not NaN")

		r2, err := Str2Number[float64, int]("Inf", 0)
		require.Error(t, err)
		require.Equal(t, 0.0, r2, "rejected Inf should yield zero value, not Inf")
	})
}
