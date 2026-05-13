package parsers

import (
	"errors"
	"math"
	"testing"
)

// 6 exported helpers + 1 unexported (joinDataName) — Wave 5 PR2 補測，避免日後
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
		tc := tc
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
		tc := tc
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := joinDataName(tc.dataName, tc.rest)
			if got != tc.want {
				t.Errorf("joinDataName(%q, %q) = %q, want %q", tc.dataName, tc.rest, got, tc.want)
			}
		})
	}
}
