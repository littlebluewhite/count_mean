package chart

import (
	"bytes"
	"strings"
	"testing"

	"count_mean/internal/models"
)

// TestExtractColumnLineData_RaggedRowEmitsNull 釘住 修補:
// ragged row (row.Channels 短於 colIdx-1) 必須顯式發 nil-valued LineData,
// 不能用 zero-value LineData{} 預設行為 (依賴 echarts version 對 omitempty)。
//
// 修補前:
//
//	if channelIdx >= len(data.Channels) { continue } // ← LineData{} zero value 留在 slice
//
// 修補後:顯式 lineData[i] = opts.LineData{Value: nil}。
//
// 為何重要:
//  1. zero-value LineData{} 的 Value 是 nil interface{},go-echarts omitempty
//     會把整個 {} 序列化,echarts 對 "{}" 的解讀依版本不同 — 有的視為 missing,
//     有的視為 0。
//  2. 顯式 nil Value + Name 仍序列化,讓 echarts 確定走 null 路徑。
//
// 與 series-level ConnectNulls: false 配合 (見 TestGenerateInteractiveChart_ConnectNullsExplicit)。
func TestExtractColumnLineData_RaggedRowEmitsNull(t *testing.T) {
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1", "Ch2", "Ch3"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{1.0, 2.0, 3.0}}, // 完整
			{Time: 0.1, Channels: []float64{1.5}},           // ragged
			{Time: 0.2, Channels: []float64{}},              // empty
			{Time: 0.3, Channels: []float64{1.7, 2.7, 3.7}}, // 完整
		},
	}

	line := extractColumnLineData(dataset, 3)
	if len(line) != len(dataset.Data) {
		t.Fatalf("line len mismatch: got %d want %d", len(line), len(dataset.Data))
	}

	// row[0] (完整) 應有 Value
	if line[0].Value == nil {
		t.Error("row[0] (complete) should have non-nil Value")
	}
	// row[1] (ragged) Value 必須是 nil (echarts null)
	if line[1].Value != nil {
		t.Errorf("row[1] (ragged) Value should be nil; got %v", line[1].Value)
	}
	// row[2] (empty channels) Value 必須是 nil
	if line[2].Value != nil {
		t.Errorf("row[2] (empty channels) Value should be nil; got %v", line[2].Value)
	}
	// row[3] (完整) 應有 Value
	if line[3].Value == nil {
		t.Error("row[3] (complete) should have non-nil Value")
	}
}

// TestGenerateInteractiveChart_ConnectNullsExplicit 釘住 修補:
// chart series option 顯式設 ConnectNulls(false),不依賴 echarts version
// 的預設值。
//
// 修補前:沒設 ConnectNulls,行為依賴 echarts v2.x 與 v3.x 預設不同。
// 修補後:顯式 false,ragged row 一律斷線 (避免誤導使用者「資料連續」)。
func TestGenerateInteractiveChart_ConnectNullsExplicit(t *testing.T) {
	generator := NewEChartsGenerator()
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{1.0}},
			{Time: 0.1, Channels: []float64{2.0}},
		},
	}

	config := InteractiveChartConfig{
		Title:           "ConnectNulls test",
		XAxisLabel:      "Time",
		YAxisLabel:      "Value",
		SelectedColumns: []int{1},
		ColumnNames:     []string{"Ch1"},
		ShowAllColumns:  false,
		Width:           "800px",
		Height:          "600px",
	}

	var buf bytes.Buffer

	if err := generator.RenderChartToWriter(dataset, config, &buf); err != nil {
		t.Fatalf("RenderChartToWriter failed: %v", err)
	}

	html := buf.String()

	// 在 echarts JSON 內,ConnectNulls: false 序列化為 "connectNulls":false。
	// 如果 caller 沒顯式設定,go-echarts 會 omitempty 而不出現此 key。
	if !strings.Contains(html, "connectNulls") {
		t.Errorf("chart HTML should explicitly contain connectNulls option key; got HTML without it")
	}
	if !strings.Contains(html, `"connectNulls":false`) {
		t.Errorf("chart HTML should set connectNulls:false; got HTML without it (excerpt: %s)", excerptAround(html, "connectNulls", 80))
	}
}

// excerptAround 取出 needle 附近 ±radius 字元作為 error message excerpt;
// 找不到時回 "<not found>"。
func excerptAround(s, needle string, radius int) string {
	idx := strings.Index(s, needle)
	if idx < 0 {
		return "<not found>"
	}
	start := idx - radius
	if start < 0 {
		start = 0
	}
	end := idx + len(needle) + radius
	if end > len(s) {
		end = len(s)
	}
	return s[start:end]
}
