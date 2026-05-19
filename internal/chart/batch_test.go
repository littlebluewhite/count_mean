package chart

import (
	"strings"
	"testing"

	"count_mean/internal/models"
)

// TestUniformSubsample_StrideAndEdgePreserved 釘住 原函式名暗示 LTTB
// (Largest-Triangle-Three-Buckets) 視覺最佳化降採樣，實際只是 uniform stride 取樣，
// 名稱誤導內部 caller。Rename 後本 test 釘住新名稱可用 + 首末資料點被保留 + rate
// == 1 時 noop pass-through。
func TestUniformSubsample_StrideAndEdgePreserved(t *testing.T) {
	dataset := createLargeTestDataset()

	// new name 直接可用
	sampled := UniformSubsample(dataset, 2)
	if sampled == nil {
		t.Fatal("UniformSubsample returned nil")
	}
	// 第一筆 / 最後一筆必須保留
	if sampled.Data[0].Time != dataset.Data[0].Time {
		t.Errorf("first sample time mismatch: got %v want %v",
			sampled.Data[0].Time, dataset.Data[0].Time)
	}
	last := sampled.Data[len(sampled.Data)-1]
	want := dataset.Data[len(dataset.Data)-1]
	if last.Time != want.Time {
		t.Errorf("last sample time mismatch: got %v want %v", last.Time, want.Time)
	}

	// 採樣率 1 應 noop（pass-through）
	noop := UniformSubsample(dataset, 1)
	if len(noop.Data) != len(dataset.Data) {
		t.Errorf("UniformSubsample(_,1) should be noop; got %d want %d",
			len(noop.Data), len(dataset.Data))
	}
}

// TestGetChartStatistics_AnnotatesFullDataOrigin 釘住 當 caller 提供
// downsampled chart 但 stats 來自完整資料時，回傳 map 必須帶 sample-count 註記，
// 讓上層 UI 可顯示「Statistics from full data, chart shows N/M points」。
// 修復前 map 內無相關欄位，本 test 會 fail。
func TestGetChartStatistics_AnnotatesFullDataOrigin(t *testing.T) {
	dataset := createTestDataset()
	stats := GetChartStatistics(dataset, []int{1, 2})

	src, ok := stats["statistics_source"].(string)
	if !ok || !strings.Contains(src, "full") {
		t.Errorf("expected statistics_source to indicate 'full' data origin; got %q (type %T)",
			stats["statistics_source"], stats["statistics_source"])
	}
	if got, want := stats["statistics_points"], len(dataset.Data); got != want {
		t.Errorf("statistics_points = %v want %d", got, want)
	}
}

// TestExtractColumnLineData_RaggedChannelsBoundsSafe 釘住 當 row.Channels
// 短於 colIdx-1（資料 ragged，部分 row 通道數少），舊邏輯 silently 用 0.0 填補，
// 雖不 panic 但混雜真實 / 偽造資料。修復後必須以 LineData{} （nil-equivalent zero
// value）回填，且函式不可 panic 即使 colIdx 超過 Headers 長度。
func TestExtractColumnLineData_RaggedChannelsBoundsSafe(t *testing.T) {
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1", "Ch2", "Ch3"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{1.0, 2.0, 3.0}}, // 完整
			{Time: 0.1, Channels: []float64{1.5}},           // 只有一個 channel
			{Time: 0.2, Channels: []float64{}},              // 空
			{Time: 0.3, Channels: []float64{1.7, 2.7, 3.7}}, // 完整
		},
	}

	t.Run("col_index_within_headers", func(t *testing.T) {
		// colIdx=3 對應 Ch3；只有 row[0] / row[3] 真的有
		line := extractColumnLineData(dataset, 3)
		if len(line) != len(dataset.Data) {
			t.Fatalf("line len mismatch: got %d want %d", len(line), len(dataset.Data))
		}
		// row[1] / row[2] 通道不足，必須是 zero-value LineData（Value == nil 或 0
		// 都接受，重點是不可 panic 且 Value 不為 row[0] 的值）
		got1 := line[1]
		if v, ok := got1.Value.(float64); ok && v != 0 {
			t.Errorf("ragged row should not produce non-zero value; got %v", v)
		}
		got2 := line[2]
		if v, ok := got2.Value.(float64); ok && v != 0 {
			t.Errorf("empty channels row should not produce non-zero value; got %v", v)
		}
	})

	t.Run("col_index_beyond_headers_no_panic", func(t *testing.T) {
		// caller 未過濾的情況：colIdx 超過 Headers 長度也不可 panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("extractColumnLineData panicked with out-of-range colIdx: %v", r)
			}
		}()
		line := extractColumnLineData(dataset, 99)
		if len(line) != len(dataset.Data) {
			t.Errorf("line len mismatch even on bad input: got %d want %d",
				len(line), len(dataset.Data))
		}
	})
}
