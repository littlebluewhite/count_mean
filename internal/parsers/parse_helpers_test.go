package parsers

import (
	"errors"
	"math"
	"testing"
)

// 3 exported helpers + 1 unexported (joinDataName)
// refactor 改 trim/Fields/rounding 邏輯時靠上游 parser test 間接覆蓋造成 silent
// regression。

func TestParseFloatCell(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		in     string
		wantV  float64
		wantOk bool
	}{
		{"empty", "", 0, false},
		{"whitespace_only", "   \t  ", 0, false},
		{"plain_int", "42", 42, true},
		{"plain_float", "1.234", 1.234, true},
		{"positive_signed", "+1.5", 1.5, true},
		{"negative_signed", "-3", -3, true},
		{"scientific", "1e10", 1e10, true},
		{"negative_scientific", "-2.5e-3", -2.5e-3, true},
		{"leading_whitespace", "  0.001", 0.001, true},
		{"trailing_whitespace", "0.001  ", 0.001, true},
		{"surround_whitespace", "  0.001  ", 0.001, true},
		{"surround_tab", "\t0.001\t", 0.001, true},
		{"alphabetic", "abc", 0, false},
		{"mixed_garbage", "12abc", 0, false},
		{"nan_literal", "NaN", math.NaN(), true},
		{"inf_literal", "Inf", math.Inf(1), true},
		{"negative_inf", "-Inf", math.Inf(-1), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotV, gotOk := ParseFloatCell(tc.in)
			if gotOk != tc.wantOk {
				t.Errorf("ParseFloatCell(%q) ok = %v, want %v", tc.in, gotOk, tc.wantOk)
			}
			// NaN comparison: math.NaN() != math.NaN() so check separately.
			if math.IsNaN(tc.wantV) {
				if !math.IsNaN(gotV) {
					t.Errorf("ParseFloatCell(%q) = %v, want NaN", tc.in, gotV)
				}
			} else if gotV != tc.wantV {
				t.Errorf("ParseFloatCell(%q) = %v, want %v", tc.in, gotV, tc.wantV)
			}
		})
	}
}

// TestParseTimeCell 驗證時間欄位解析器拒絕 NaN/Inf，接受合法數值。
// 並確認回歸守：ParseFloatCell 仍接受 NaN/Inf（通道值語意不變）。
func TestParseTimeCell(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		in     string
		wantV  float64
		wantOk bool
	}{
		// 時間欄位必須拒絕 NaN/Inf
		{"nan_rejected", "NaN", 0, false},
		{"plus_inf_rejected", "+Inf", 0, false},
		{"minus_inf_rejected", "-Inf", 0, false},
		{"bare_inf_rejected", "Inf", 0, false},
		// 合法數值照常接受
		{"plain_float", "1.5", 1.5, true},
		{"whitespace_trimmed", " 0.001 ", 0.001, true},
		{"zero", "0", 0, true},
		{"negative", "-3.14", -3.14, true},
		{"scientific", "1e-3", 1e-3, true},
		// 無效字串照常拒絕
		{"empty", "", 0, false},
		{"garbage", "abc", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotV, gotOk := ParseTimeCell(tc.in)
			if gotOk != tc.wantOk {
				t.Errorf("ParseTimeCell(%q) ok = %v, want %v", tc.in, gotOk, tc.wantOk)
			}
			if gotOk && gotV != tc.wantV {
				t.Errorf("ParseTimeCell(%q) = %v, want %v", tc.in, gotV, tc.wantV)
			}
		})
	}
}

// TestParseFloatCell_StillAcceptsNaN 回歸守：ParseFloatCell 必須仍接受 NaN/Inf，
// 因為通道值列刻意依賴此行為（EMG missing-data 輸出字面 "NaN"）。
func TestParseFloatCell_StillAcceptsNaN(t *testing.T) {
	t.Parallel()

	t.Run("NaN_accepted", func(t *testing.T) {
		t.Parallel()
		v, ok := ParseFloatCell("NaN")
		if !ok {
			t.Error("ParseFloatCell(\"NaN\") ok = false, want true（通道值語意不得破壞）")
		}
		if !math.IsNaN(v) {
			t.Errorf("ParseFloatCell(\"NaN\") = %v, want NaN", v)
		}
	})

	t.Run("plus_Inf_accepted", func(t *testing.T) {
		t.Parallel()
		v, ok := ParseFloatCell("+Inf")
		if !ok {
			t.Error("ParseFloatCell(\"+Inf\") ok = false, want true")
		}
		if !math.IsInf(v, 1) {
			t.Errorf("ParseFloatCell(\"+Inf\") = %v, want +Inf", v)
		}
	})
}

func TestValidateTimeSeries(t *testing.T) {
	t.Parallel()

	labels := TimeSeriesLabels{
		DataName:     "EMG",
		SeriesName:   "時間序列",
		SeriesPos:    "索引",
		ChannelLabel: "通道",
	}

	t.Run("happy_path", func(t *testing.T) {
		t.Parallel()
		err := ValidateTimeSeries(
			[]float64{0.0, 0.1, 0.2},
			map[string][]float64{"Ch1": {1, 2, 3}, "Ch2": {4, 5, 6}},
			labels,
		)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("empty_series", func(t *testing.T) {
		t.Parallel()
		err := ValidateTimeSeries(
			[]float64{},
			map[string][]float64{"Ch1": {}},
			labels,
		)
		if !errors.Is(err, ErrTimeSequenceEmpty) {
			t.Errorf("expected ErrTimeSequenceEmpty, got %v", err)
		}
	})

	t.Run("no_channels", func(t *testing.T) {
		t.Parallel()
		err := ValidateTimeSeries(
			[]float64{0.0, 0.1},
			map[string][]float64{},
			labels,
		)
		if !errors.Is(err, ErrNoChannels) {
			t.Errorf("expected ErrNoChannels, got %v", err)
		}
	})

	t.Run("non_increasing_time", func(t *testing.T) {
		t.Parallel()
		err := ValidateTimeSeries(
			[]float64{0.0, 0.1, 0.05},
			map[string][]float64{"Ch1": {1, 2, 3}},
			labels,
		)
		if !errors.Is(err, ErrTimeNotIncreasing) {
			t.Errorf("expected ErrTimeNotIncreasing, got %v", err)
		}
	})

	t.Run("mismatched_channel_length", func(t *testing.T) {
		t.Parallel()
		err := ValidateTimeSeries(
			[]float64{0.0, 0.1, 0.2},
			map[string][]float64{"Ch1": {1, 2}}, // 2 values vs 3 time points
			labels,
		)
		if !errors.Is(err, ErrInconsistentLength) {
			t.Errorf("expected ErrInconsistentLength, got %v", err)
		}
	})

	t.Run("int_series_for_motion", func(t *testing.T) {
		t.Parallel()
		motionLabels := TimeSeriesLabels{
			DataName:     "Motion",
			SeriesName:   "index 序列",
			SeriesPos:    "位置",
			ChannelLabel: "列",
		}
		err := ValidateTimeSeries(
			[]int{1, 2, 3},
			map[string][]float64{"Joint1": {0.1, 0.2, 0.3}},
			motionLabels,
		)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestParseFloatCell_LocaleSafety pins the contract that ParseFloatCell stays
// locale-independent. Go's strconv.ParseFloat is documented as locale-agnostic
// (always expects "." as decimal separator), but we want a sentinel regression
// test so a future refactor that swaps in fmt.Sscanf-with-locale or a
// third-party parser can't silently flip semantics — e.g. on a German locale
// where "3,14" might otherwise look like a valid decimal.
//
// Contract:
//   - "3.14" → 3.14 (always parseable, regardless of host LC_NUMERIC)
//   - "3,14" → ok=false (the comma must not be silently treated as decimal)
//   - "1.234,56" (German thousands-comma) → ok=false
//
// We do not call C setlocale() — Go's runtime does not honour it for
// strconv anyway, and pinning the contract via input strings is sufficient.
func TestParseFloatCell_LocaleSafety(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		in     string
		wantV  float64
		wantOk bool
	}{
		{"dot_decimal_parses", "3.14", 3.14, true},
		{"comma_decimal_rejected", "3,14", 0, false},
		{"european_thousands_comma_rejected", "1.234,56", 0, false},
		{"european_dot_thousands_comma_decimal_rejected", "1,234.56", 0, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotV, gotOk := ParseFloatCell(tc.in)
			if gotOk != tc.wantOk {
				t.Errorf("ParseFloatCell(%q) ok = %v, want %v", tc.in, gotOk, tc.wantOk)
			}
			if gotOk && gotV != tc.wantV {
				t.Errorf("ParseFloatCell(%q) = %v, want %v", tc.in, gotV, tc.wantV)
			}
		})
	}
}

func TestJoinDataName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		dataName string
		rest     string
		want     string
	}{
		// ASCII alphanumeric tail → insert space
		{"ascii_letter_tail", "EMG", "時間序列", "EMG 時間序列"},
		{"digit_tail", "Ch1", "資料", "Ch1 資料"},
		{"mixed_case", "abcDef", "tail", "abcDef tail"},

		// CJK tail → direct concat (no space)
		{"chinese_tail", "力板", "時間序列", "力板時間序列"},
		{"japanese_tail", "感測器", "資料", "感測器資料"},

		// Edge case: ASCII punctuation tail — function admits this is undefined,
		// pin the current byte-tail heuristic so refactors don't silently change behavior.
		// TODO: replace byte-tail heuristic with unicode.IsLetter for full correctness.
		{"ascii_punct_tail", "EMG-", "資料", "EMG-資料"},

		// Edge: empty inputs
		{"empty_dataname", "", "rest", "rest"},
		{"empty_rest_ascii", "EMG", "", "EMG "},
		{"empty_rest_chinese", "力板", "", "力板"},
		{"both_empty", "", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := joinDataName(tc.dataName, tc.rest)
			if got != tc.want {
				t.Errorf("joinDataName(%q, %q) = %q, want %q", tc.dataName, tc.rest, got, tc.want)
			}
		})
	}
}
