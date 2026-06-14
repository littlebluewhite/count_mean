package parsers

import (
	"errors"
	"math"
	"testing"
)

// 6 exported helpers + 1 unexported (joinDataName)
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

func TestParseTimeAndChannels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		record          []string
		channelStartIdx int
		wantTime        float64
		wantChannels    []float64
		wantOk          bool
	}{
		{
			name:            "happy_3_channels",
			record:          []string{"1.0", "10", "20", "30"},
			channelStartIdx: 1,
			wantTime:        1.0,
			wantChannels:    []float64{10, 20, 30},
			wantOk:          true,
		},
		{
			name:            "empty_record",
			record:          []string{},
			channelStartIdx: 1,
			wantOk:          false,
		},
		{
			name:            "unparseable_time",
			record:          []string{"abc", "10"},
			channelStartIdx: 1,
			wantOk:          false,
		},
		{
			name:            "channel_start_beyond_record_clamps_to_zero",
			record:          []string{"1.0"},
			channelStartIdx: 5,
			wantTime:        1.0,
			wantChannels:    []float64{},
			wantOk:          true,
		},
		{
			name:            "bad_channel_falls_back_to_zero",
			record:          []string{"1.0", "10", "bad", "30"},
			channelStartIdx: 1,
			wantTime:        1.0,
			wantChannels:    []float64{10, 0, 30},
			wantOk:          true,
		},
		{
			name:            "whitespace_in_cells",
			record:          []string{" 1.0 ", "  10 ", " 20"},
			channelStartIdx: 1,
			wantTime:        1.0,
			wantChannels:    []float64{10, 20},
			wantOk:          true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotTime, gotChannels, gotOk := ParseTimeAndChannels(tc.record, tc.channelStartIdx)
			if gotOk != tc.wantOk {
				t.Fatalf("ok = %v, want %v", gotOk, tc.wantOk)
			}
			if !gotOk {
				return
			}
			if gotTime != tc.wantTime {
				t.Errorf("time = %v, want %v", gotTime, tc.wantTime)
			}
			if len(gotChannels) != len(tc.wantChannels) {
				t.Fatalf("len(channels) = %d, want %d", len(gotChannels), len(tc.wantChannels))
			}
			for i, want := range tc.wantChannels {
				if gotChannels[i] != want {
					t.Errorf("channels[%d] = %v, want %v", i, gotChannels[i], want)
				}
			}
		})
	}
}

func TestFindTimeRangeIndices(t *testing.T) {
	t.Parallel()

	t.Run("typical_inside_range", func(t *testing.T) {
		t.Parallel()
		times := []float64{0.0, 0.5, 1.0, 1.5, 2.0}
		start, end, err := FindTimeRangeIndices(times, 0.5, 1.5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if start != 1 || end != 3 {
			t.Errorf("got (%d, %d), want (1, 3)", start, end)
		}
	})

	t.Run("empty_times", func(t *testing.T) {
		t.Parallel()
		_, _, err := FindTimeRangeIndices([]float64{}, 0, 1)
		if !errors.Is(err, ErrTimeRangeNotFound) {
			t.Errorf("expected ErrTimeRangeNotFound, got %v", err)
		}
	})

	t.Run("range_fully_before_data", func(t *testing.T) {
		t.Parallel()
		_, _, err := FindTimeRangeIndices([]float64{5.0, 6.0}, 0, 1.0)
		if !errors.Is(err, ErrTimeRangeNotFound) {
			t.Errorf("expected ErrTimeRangeNotFound, got %v", err)
		}
	})

	t.Run("ms_rounding_edge", func(t *testing.T) {
		t.Parallel()
		// 1.0004 round to ms = 1; 1.0005 round to ms = 1 (banker's rounding in Go)
		// 1.0006 round to ms = 1 (1006 / 1000 = 1, since math.Round(1.0006*1000) = 1001)
		// Actually 1.0005 * 1000 = 1000.5 → math.Round → 1001
		times := []float64{1.0004, 1.0005, 1.0006}
		// startTime = 1.0005 → startMs = 1001
		// times[0] = 1.0004 → tMs = 1 (math.Round(1000.4) = 1000? actually 1000.4 → 1000)
		// times[1] = 1.0005 → tMs = 1001 (math.Round(1000.5) = 1001 — banker's would round to 1000 but Go's math.Round rounds half-away-from-zero so 1001)
		start, end, err := FindTimeRangeIndices(times, 1.0005, 1.001)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if start != 1 {
			t.Errorf("ms-rounding boundary: start = %d, want 1", start)
		}
		_ = end
	})
}

func TestFindIndexRangeIndices(t *testing.T) {
	t.Parallel()

	t.Run("typical", func(t *testing.T) {
		t.Parallel()
		indices := []int{1, 2, 3, 4, 5}
		start, end, err := FindIndexRangeIndices(indices, 2, 4)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if start != 1 || end != 3 {
			t.Errorf("got (%d, %d), want (1, 3)", start, end)
		}
	})

	t.Run("range_fully_outside", func(t *testing.T) {
		t.Parallel()
		_, _, err := FindIndexRangeIndices([]int{10, 20}, 100, 200)
		if !errors.Is(err, ErrIndexRangeNotFound) {
			t.Errorf("expected ErrIndexRangeNotFound, got %v", err)
		}
	})

	t.Run("single_match", func(t *testing.T) {
		t.Parallel()
		start, end, err := FindIndexRangeIndices([]int{5, 10, 15}, 10, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if start != 1 || end != 1 {
			t.Errorf("got (%d, %d), want (1, 1)", start, end)
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

// TestFloatCell_RoundTrip 釘住 ParseFloatCellWithMissing + FormatFloatCell 對稱性
// （P1-15）：4 個維度的 cell 字面（missing / NaN value / ±Inf / 一般數）皆能
// 在 parse → format → re-parse → format 兩輪後字面與 (v, isMissing) 全部不變。
//
// 對照 legacy `ParseFloatCell` + emg_writer formatCell 的 lossy round-trip：
//   - 「NaN」 → (NaN, true) → 寫 ""    → re-parse (0, false)   ← 失去 NaN value
//   - 「""」  → (0, false)  → 寫 "0.0..." → re-parse (0, true) ← 缺值升真實 0
//
// 新 helper 取消這兩個 lossy pathway:
//   - 「NaN」 → (NaN, isMissing=false) → 寫 "NaN" → re-parse 一致
//   - 「""」  → (NaN, isMissing=true)  → 寫 ""    → re-parse 一致
func TestFloatCell_RoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		input       string // 原始 cell 字面
		wantV       float64
		wantMissing bool
		wantFormatV string // 第一次 format 結果
		isNaNFamily bool   // wantV 是 NaN（含 missing），比較需走 IsNaN
		precision   int
	}{
		{
			name:        "missing_empty_cell_round_trip_stable",
			input:       "",
			wantV:       math.NaN(),
			wantMissing: true,
			wantFormatV: "",
			isNaNFamily: true,
			precision:   6,
		},
		{
			name:        "literal_NaN_value_round_trip_stable",
			input:       "NaN",
			wantV:       math.NaN(),
			wantMissing: false,
			wantFormatV: "NaN",
			isNaNFamily: true,
			precision:   6,
		},
		{
			name:        "positive_inf_round_trip_stable",
			input:       "+Inf",
			wantV:       math.Inf(1),
			wantMissing: false,
			wantFormatV: "+Inf",
			precision:   6,
		},
		{
			name:        "negative_inf_round_trip_stable",
			input:       "-Inf",
			wantV:       math.Inf(-1),
			wantMissing: false,
			wantFormatV: "-Inf",
			precision:   6,
		},
		{
			name:        "plain_number_one_point_five",
			input:       "1.5",
			wantV:       1.5,
			wantMissing: false,
			wantFormatV: "1.500000",
			precision:   6,
		},
		{
			name:        "negative_number_with_custom_precision",
			input:       "-3.14",
			wantV:       -3.14,
			wantMissing: false,
			wantFormatV: "-3.140",
			precision:   3,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// 第一輪 parse
			v1, missing1 := ParseFloatCellWithMissing(tc.input)
			if missing1 != tc.wantMissing {
				t.Errorf("first parse isMissing = %v, want %v", missing1, tc.wantMissing)
			}
			if tc.isNaNFamily {
				if !math.IsNaN(v1) {
					t.Errorf("first parse v = %v, want NaN", v1)
				}
			} else if v1 != tc.wantV {
				t.Errorf("first parse v = %v, want %v", v1, tc.wantV)
			}

			// 第一次 format
			s1 := FormatFloatCell(v1, missing1, tc.precision)
			if s1 != tc.wantFormatV {
				t.Errorf("first format = %q, want %q", s1, tc.wantFormatV)
			}

			// 第二輪 parse（再走一次 — round-trip 不損失）
			v2, missing2 := ParseFloatCellWithMissing(s1)
			if missing2 != missing1 {
				t.Errorf("second parse isMissing = %v, want %v (round-trip drifted)", missing2, missing1)
			}
			if tc.isNaNFamily {
				if !math.IsNaN(v2) {
					t.Errorf("second parse v = %v, want NaN (round-trip drifted)", v2)
				}
			} else if v2 != v1 {
				t.Errorf("second parse v = %v, want %v (round-trip drifted)", v2, v1)
			}

			// 第二次 format（應與第一次完全相同 — 字面穩定）
			s2 := FormatFloatCell(v2, missing2, tc.precision)
			if s2 != s1 {
				t.Errorf("second format = %q, want %q (字面 round-trip 不穩)", s2, s1)
			}
		})
	}
}

// TestParseFloatCellWithMissing_EdgeCases 補強 ParseFloatCellWithMissing 在 trim /
// 大小寫 / unparseable garbage 邊界的行為，避免 future refactor 把 strconv
// 換成手刻 parser 後 silent 改語意。
func TestParseFloatCellWithMissing_EdgeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		in          string
		wantNaN     bool // v should be NaN
		wantMissing bool
		wantV       float64 // only checked when !wantNaN
	}{
		// 「missing」訊號 — empty / whitespace 都歸入
		{"empty_string_is_missing", "", true, true, 0},
		{"whitespace_only_is_missing", "   ", true, true, 0},
		{"tab_only_is_missing", "\t\t", true, true, 0},
		{"mixed_whitespace_is_missing", " \t \n ", true, true, 0},

		// 字面 NaN — Go strconv 接受 NaN / nan / NAN
		{"nan_uppercase_is_nan_value", "NaN", true, false, 0},
		{"nan_lowercase_is_nan_value", "nan", true, false, 0},
		{"nan_with_surrounding_space", "  NaN  ", true, false, 0},

		// Inf 字面
		{"inf_positive_implicit", "Inf", false, false, math.Inf(1)},
		{"inf_positive_explicit", "+Inf", false, false, math.Inf(1)},
		{"inf_negative", "-Inf", false, false, math.Inf(-1)},

		// 一般數字
		{"plain_zero", "0", false, false, 0},
		{"plain_int", "42", false, false, 42},
		{"plain_float_with_spaces", "  3.14  ", false, false, 3.14},

		// Garbage 視為 (NaN, false) — 非 missing
		{"garbage_is_nan_not_missing", "abc", true, false, 0},
		{"partial_number_is_nan_not_missing", "12abc", true, false, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotV, gotMissing := ParseFloatCellWithMissing(tc.in)
			if gotMissing != tc.wantMissing {
				t.Errorf("ParseFloatCellWithMissing(%q) isMissing = %v, want %v", tc.in, gotMissing, tc.wantMissing)
			}
			if tc.wantNaN {
				if !math.IsNaN(gotV) {
					t.Errorf("ParseFloatCellWithMissing(%q) v = %v, want NaN", tc.in, gotV)
				}
			} else if gotV != tc.wantV {
				t.Errorf("ParseFloatCellWithMissing(%q) v = %v, want %v", tc.in, gotV, tc.wantV)
			}
		})
	}
}

// TestFormatFloatCell_EdgeCases 補強 FormatFloatCell 對 isMissing 旗標、NaN/Inf、
// precision 邊界的行為。
func TestFormatFloatCell_EdgeCases(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		v         float64
		isMissing bool
		precision int
		want      string
	}{
		// isMissing 旗標永遠優先 — 即使 v=NaN/Inf/0 都該寫空字串
		{"missing_with_nan_value_outputs_empty", math.NaN(), true, 6, ""},
		{"missing_with_finite_value_outputs_empty", 1.5, true, 6, ""},
		{"missing_with_pos_inf_outputs_empty", math.Inf(1), true, 6, ""},

		// !isMissing 下 NaN/Inf 字面化
		{"nan_value_writes_NaN_literal", math.NaN(), false, 6, "NaN"},
		{"positive_inf_writes_plus_Inf", math.Inf(1), false, 6, "+Inf"},
		{"negative_inf_writes_minus_Inf", math.Inf(-1), false, 6, "-Inf"},

		// 一般數值 + precision
		{"finite_precision_6", 1.5, false, 6, "1.500000"},
		{"finite_precision_3", 1.5, false, 3, "1.500"},
		{"finite_zero", 0, false, 6, "0.000000"},
		{"finite_negative", -2.71828, false, 4, "-2.7183"},

		// precision <= 0 走預設 6
		{"precision_zero_uses_default_6", 1.5, false, 0, "1.500000"},
		{"precision_negative_uses_default_6", 1.5, false, -1, "1.500000"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FormatFloatCell(tc.v, tc.isMissing, tc.precision)
			if got != tc.want {
				t.Errorf("FormatFloatCell(%v, %v, %d) = %q, want %q",
					tc.v, tc.isMissing, tc.precision, got, tc.want)
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
