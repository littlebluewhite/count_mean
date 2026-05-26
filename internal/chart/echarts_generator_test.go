package chart

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"count_mean/internal/models"
)

// TestEChartsGenerator 測試 ECharts 圖表生成器.
func TestEChartsGenerator(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestNewEChartsGenerator", testNewEChartsGenerator},
		{"TestGenerateInteractiveChart", testGenerateInteractiveChart},
		{"TestGetAvailableColumns", testGetAvailableColumns},
		{"TestRenderChartToWriter", testRenderChartToWriter},
		{"TestUniformSubsample", testUniformSubsample},
		{"TestCalculateOptimalSampling", testCalculateOptimalSampling},
		{"TestGenerateComparisonChart", testGenerateComparisonChart},
		{"TestConvertToJSON", testConvertToJSON},
		{"TestGetChartStatistics", testGetChartStatistics},
		{"TestGenerateExportScript", testGenerateExportScript},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testNewEChartsGenerator(t *testing.T) {
	generator := NewEChartsGenerator()

	if generator == nil {
		t.Fatal("NewEChartsGenerator() returned nil")
	}

	// 測試基本功能是否正常
	_ = createTestDataset()
}

func testGenerateInteractiveChart(t *testing.T) {
	generator := NewEChartsGenerator()
	dataset := createTestDataset()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "interactive_chart.html")

	config := InteractiveChartConfig{
		Title:           "Test Interactive Chart",
		XAxisLabel:      "Time (s)",
		YAxisLabel:      "EMG Value",
		SelectedColumns: []int{1, 2},
		ColumnNames:     []string{"Channel1", "Channel2"},
		ShowAllColumns:  false,
		Width:           "800px",
		Height:          "600px",
	}

	err := generator.GenerateInteractiveChart(dataset, &config, outputPath)
	if err != nil {
		t.Errorf("GenerateInteractiveChart() failed: %v", err)
	}

	// 檢查文件是否已創建
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("GenerateInteractiveChart() did not create output file")
	}

	// 檢查文件內容
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Errorf("Cannot read generated file: %v", err)
	}

	htmlContent := string(content)
	if !strings.Contains(htmlContent, "Test Interactive Chart") {
		t.Error("Generated HTML does not contain expected title")
	}

	if !strings.Contains(htmlContent, "echarts") {
		t.Error("Generated HTML does not contain ECharts references")
	}
}

// TestApplyChartOptions_XYAxisLabelRetained 釘住
// applySimpleChartOptions (RenderChartToWriter 路徑) 與 setComparisonChartOptions
// (GenerateComparisonChart 路徑) 原本 silently drop XAxisLabel / YAxisLabel —
// caller config 帶了 axis label 但 chart HTML 不會渲染出來。修法是把 config 的
// XAxisLabel / YAxisLabel 傳給 echarts 的 XAxis.Name / YAxis.Name (對應 echarts
// xAxisName / yAxisName)。
//
// 兩個 path 一起測:
//   - RenderChartToWriter → 走 applySimpleChartOptions
//   - GenerateComparisonChart → 走 setComparisonChartOptions
func TestApplyChartOptions_XYAxisLabelRetained(t *testing.T) {
	generator := NewEChartsGenerator()
	dataset := createTestDataset()

	const xLabel = "AxisLabelXTest123"
	const yLabel = "AxisLabelYTest456"

	config := InteractiveChartConfig{
		Title:           "Axis Label Test",
		XAxisLabel:      xLabel,
		YAxisLabel:      yLabel,
		SelectedColumns: []int{1, 2},
		Width:           "800px",
		Height:          "600px",
	}

	t.Run("RenderChartToWriter retains axis labels", func(t *testing.T) {
		var buf bytes.Buffer
		if err := generator.RenderChartToWriter(dataset, config, &buf); err != nil {
			t.Fatalf("RenderChartToWriter() failed: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, xLabel) {
			t.Errorf("rendered HTML missing X axis label %q", xLabel)
		}
		if !strings.Contains(out, yLabel) {
			t.Errorf("rendered HTML missing Y axis label %q", yLabel)
		}
	})

	t.Run("GenerateComparisonChart retains axis labels", func(t *testing.T) {
		ds1 := createTestDataset()
		ds2 := createTestDataset()
		datasets := []*models.EMGDataset{ds1, ds2}
		labels := []string{"A", "B"}
		tempDir := t.TempDir()
		outputPath := filepath.Join(tempDir, "axis_label_comparison.html")

		if err := generator.GenerateComparisonChart(datasets, labels, &config, outputPath); err != nil {
			t.Fatalf("GenerateComparisonChart() failed: %v", err)
		}
		content, err := os.ReadFile(outputPath)
		if err != nil {
			t.Fatalf("read generated file: %v", err)
		}
		out := string(content)
		if !strings.Contains(out, xLabel) {
			t.Errorf("comparison chart HTML missing X axis label %q", xLabel)
		}
		if !strings.Contains(out, yLabel) {
			t.Errorf("comparison chart HTML missing Y axis label %q", yLabel)
		}
	})
}

// TestGenerateInteractiveChart_NegativeColIndex_NoPanic 防止 panic regression：
// 原本 colIndex 檢查 `colIndex >= len(...) || colIndex == 0` 漏接負值，
// extractColumnLineData 走 Channels[colIdx-1] 在 colIdx=-1 時索引 -2 → panic。
// 修正後 colIndex <= 0 一律 skip；extractColumnLineData 也有 defensive check。
func TestGenerateInteractiveChart_NegativeColIndex_NoPanic(t *testing.T) {
	generator := NewEChartsGenerator()
	dataset := createTestDataset()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "negative_colindex.html")

	config := InteractiveChartConfig{
		Title:           "Negative colIndex regression",
		XAxisLabel:      "Time (s)",
		YAxisLabel:      "EMG Value",
		SelectedColumns: []int{-1, -100, 1, 0, 2}, // 含負值、零、合法索引
		ColumnNames:     []string{"NegOne", "NegHundred", "Channel1", "Zero", "Channel2"},
		ShowAllColumns:  false,
		Width:           "800px",
		Height:          "600px",
	}

	// 不該 panic
	err := generator.GenerateInteractiveChart(dataset, &config, outputPath)
	if err != nil {
		t.Errorf("GenerateInteractiveChart() with negative colIndex returned error: %v", err)
	}
}

func testGetAvailableColumns(t *testing.T) {
	_ = NewEChartsGenerator() // Keep generator creation for coverage
	dataset := createTestDataset()

	columns := GetAvailableColumns(dataset)

	expectedCount := 3 // Channel1, Channel2, Channel3
	if len(columns) != expectedCount {
		t.Errorf("GetAvailableColumns() returned %d columns, expected %d", len(columns), expectedCount)
	}

	// 檢查第一個欄位的統計信息
	if len(columns) > 0 {
		col := columns[0]
		if col.Name != "Channel1" {
			t.Errorf("First column name = %s, expected 'Channel1'", col.Name)
		}

		if col.Index != 1 {
			t.Errorf("First column index = %d, expected 1", col.Index)
		}

		if col.DataPoints != 10 {
			t.Errorf("First column data points = %d, expected 10", col.DataPoints)
		}

		// 檢查統計值是否合理
		if col.Min > col.Max {
			t.Errorf("Column min (%f) > max (%f)", col.Min, col.Max)
		}

		if col.Mean < col.Min || col.Mean > col.Max {
			t.Errorf("Column mean (%f) not between min (%f) and max (%f)", col.Mean, col.Min, col.Max)
		}
	}
}

func testRenderChartToWriter(t *testing.T) {
	generator := NewEChartsGenerator()
	dataset := createTestDataset()

	config := InteractiveChartConfig{
		Title:           "Writer Test Chart",
		XAxisLabel:      "Time (s)",
		YAxisLabel:      "EMG Value",
		SelectedColumns: []int{1},
		Width:           "800px",
		Height:          "600px",
	}

	var buf bytes.Buffer

	err := generator.RenderChartToWriter(dataset, config, &buf)
	if err != nil {
		t.Errorf("RenderChartToWriter() failed: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("RenderChartToWriter() produced no output")
	}

	content := buf.String()
	if !strings.Contains(content, "Writer Test Chart") {
		t.Error("Rendered content does not contain expected title")
	}
}

func testUniformSubsample(t *testing.T) {
	_ = NewEChartsGenerator() // Keep generator creation for coverage
	dataset := createLargeTestDataset()

	// 測試正常採樣
	sampledData := UniformSubsample(dataset, 2)
	expectedSize := len(dataset.Data)/2 + 1

	if len(sampledData.Data) > expectedSize+1 { // +1 for potential last point
		t.Errorf("UniformSubsample() returned %d points, expected around %d",
			len(sampledData.Data), expectedSize)
	}

	// 檢查標題是否保持不變
	if len(sampledData.Headers) != len(dataset.Headers) {
		t.Error("UniformSubsample() changed headers")
	}

	// 檢查第一個和最後一個數據點
	if len(sampledData.Data) > 0 {
		if sampledData.Data[0].Time != dataset.Data[0].Time {
			t.Error("UniformSubsample() changed first data point time")
		}

		lastSampled := sampledData.Data[len(sampledData.Data)-1]
		lastOriginal := dataset.Data[len(dataset.Data)-1]

		if lastSampled.Time != lastOriginal.Time {
			t.Error("UniformSubsample() did not preserve last data point")
		}
	}

	// 測試採樣率 <= 1
	notSampledData := UniformSubsample(dataset, 1)
	if len(notSampledData.Data) != len(dataset.Data) {
		t.Error("UniformSubsample() with rate 1 should not change data size")
	}
}

func testCalculateOptimalSampling(t *testing.T) {
	_ = NewEChartsGenerator() // Keep generator creation for coverage

	// 測試正常情況
	rate := CalculateOptimalSampling(1000, 500)
	if rate != 2 {
		t.Errorf("CalculateOptimalSampling(1000, 500) = %d, expected 2", rate)
	}

	// 測試數據點少於最大值
	rate = CalculateOptimalSampling(300, 500)
	if rate != 1 {
		t.Errorf("CalculateOptimalSampling(300, 500) = %d, expected 1", rate)
	}

	// 測試邊界情況
	rate = CalculateOptimalSampling(1500, 500)
	if rate != 3 {
		t.Errorf("CalculateOptimalSampling(1500, 500) = %d, expected 3", rate)
	}
}

func testGenerateComparisonChart(t *testing.T) {
	generator := NewEChartsGenerator()
	dataset1 := createTestDataset()
	dataset2 := createTestDataset()

	// 修改第二個數據集的數據
	for i := range dataset2.Data {
		for j := range dataset2.Data[i].Channels {
			dataset2.Data[i].Channels[j] *= 2
		}
	}

	datasets := []*models.EMGDataset{dataset1, dataset2}
	labels := []string{"Dataset1", "Dataset2"}

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "comparison_chart.html")

	config := InteractiveChartConfig{
		Title:           "Comparison Chart",
		XAxisLabel:      "Time (s)",
		YAxisLabel:      "EMG Value",
		SelectedColumns: []int{1, 2},
		Width:           "800px",
		Height:          "600px",
	}

	err := generator.GenerateComparisonChart(datasets, labels, &config, outputPath)
	if err != nil {
		t.Errorf("GenerateComparisonChart() failed: %v", err)
	}

	// 檢查文件是否已創建
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("GenerateComparisonChart() did not create output file")
	}

	// 檢查文件內容
	content, err := os.ReadFile(outputPath)
	if err != nil {
		t.Errorf("Cannot read generated file: %v", err)
	}

	htmlContent := string(content)
	if !strings.Contains(htmlContent, "Comparison Chart") {
		t.Error("Generated HTML does not contain expected title")
	}
}

func testConvertToJSON(t *testing.T) {
	_ = NewEChartsGenerator() // Keep generator creation for coverage
	dataset := createTestDataset()

	selectedColumns := []int{1, 2}
	jsonData := ConvertToJSON(dataset, selectedColumns)

	if len(jsonData) != len(dataset.Data) {
		t.Errorf("ConvertToJSON() returned %d points, expected %d", len(jsonData), len(dataset.Data))
	}

	// 檢查第一個數據點
	if len(jsonData) > 0 {
		point := jsonData[0]
		if point.Time != dataset.Data[0].Time {
			t.Errorf("ConvertToJSON() first point time = %f, expected %f", point.Time, dataset.Data[0].Time)
		}

		if len(point.Value) != len(selectedColumns) {
			t.Errorf("ConvertToJSON() first point values = %d, expected %d", len(point.Value), len(selectedColumns))
		}

		// 檢查值是否正確
		for i, colIdx := range selectedColumns {
			expected := dataset.Data[0].Channels[colIdx-1]
			if point.Value[i] != expected {
				t.Errorf("ConvertToJSON() value[%d] = %f, expected %f", i, point.Value[i], expected)
			}
		}
	}
}

func testGetChartStatistics(t *testing.T) {
	_ = NewEChartsGenerator() // Keep generator creation for coverage
	dataset := createTestDataset()

	selectedColumns := []int{1, 2}
	stats := GetChartStatistics(dataset, selectedColumns)

	// 檢查基本統計
	if stats["total_points"] != len(dataset.Data) {
		t.Errorf("GetChartStatistics() total_points = %v, expected %d", stats["total_points"], len(dataset.Data))
	}

	if stats["selected_columns"] != len(selectedColumns) {
		t.Errorf("GetChartStatistics() selected_columns = %v, expected %d", stats["selected_columns"], len(selectedColumns))
	}

	// 檢查時間範圍
	if stats["start_time"] != dataset.Data[0].Time {
		t.Errorf("GetChartStatistics() start_time = %v, expected %f", stats["start_time"], dataset.Data[0].Time)
	}

	expectedEndTime := dataset.Data[len(dataset.Data)-1].Time
	if stats["end_time"] != expectedEndTime {
		t.Errorf("GetChartStatistics() end_time = %v, expected %f", stats["end_time"], expectedEndTime)
	}

	// 檢查持續時間
	expectedDuration := expectedEndTime - dataset.Data[0].Time
	if stats["duration"] != expectedDuration {
		t.Errorf("GetChartStatistics() duration = %v, expected %f", stats["duration"], expectedDuration)
	}

	// 檢查欄位統計
	columnStats, ok := stats["column_statistics"].([]map[string]any)
	if !ok {
		t.Error("GetChartStatistics() column_statistics is not the expected type")
	}

	if len(columnStats) != len(selectedColumns) {
		t.Errorf("GetChartStatistics() column_statistics length = %d, expected %d", len(columnStats), len(selectedColumns))
	}
}

func testGenerateExportScript(t *testing.T) {
	_ = NewEChartsGenerator() // Keep generator creation for coverage

	config := ExportConfig{
		Format:   "png",
		Width:    800,
		Height:   600,
		DPI:      100,
		FileName: "test_chart.png",
	}

	script := GenerateExportScript(config)

	if !strings.Contains(script, "png") {
		t.Error("GenerateExportScript() does not contain format")
	}

	if !strings.Contains(script, "test_chart.png") {
		t.Error("GenerateExportScript() does not contain filename")
	}

	if !strings.Contains(script, "function exportChart") {
		t.Error("GenerateExportScript() does not contain export function")
	}
}

// 基準測試.
func BenchmarkGenerateInteractiveChart(b *testing.B) {
	generator := NewEChartsGenerator()
	dataset := createTestDataset()

	tempDir := b.TempDir()

	config := InteractiveChartConfig{
		Title:           "Benchmark Chart",
		XAxisLabel:      "Time (s)",
		YAxisLabel:      "EMG Value",
		SelectedColumns: []int{1, 2, 3},
		Width:           "800px",
		Height:          "600px",
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		outputPath := filepath.Join(tempDir, fmt.Sprintf("bench_chart_%d.html", i))

		err := generator.GenerateInteractiveChart(dataset, &config, outputPath)
		if err != nil {
			b.Errorf("GenerateInteractiveChart() benchmark failed: %v", err)
		}
	}
}

func BenchmarkUniformSubsample(b *testing.B) {
	_ = NewEChartsGenerator() // Keep generator creation for coverage
	dataset := createLargeTestDataset()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = UniformSubsample(dataset, 5)
	}
}

func BenchmarkGetAvailableColumns(b *testing.B) {
	_ = NewEChartsGenerator() // Keep generator creation for coverage
	dataset := createLargeTestDataset()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = GetAvailableColumns(dataset)
	}
}

// TestInteractiveChartConfig 測試互動式圖表配置.
func TestInteractiveChartConfig(t *testing.T) {
	config := InteractiveChartConfig{
		Title:           "Test Config",
		XAxisLabel:      "X Axis",
		YAxisLabel:      "Y Axis",
		SelectedColumns: []int{1, 2, 3},
		ColumnNames:     []string{"Col1", "Col2", "Col3"},
		ShowAllColumns:  false,
		Width:           "1000px",
		Height:          "700px",
	}

	// 測試配置字段
	if config.Title != "Test Config" {
		t.Errorf("Config.Title = %s, expected 'Test Config'", config.Title)
	}

	if len(config.SelectedColumns) != 3 {
		t.Errorf("Config.SelectedColumns length = %d, expected 3", len(config.SelectedColumns))
	}

	if len(config.ColumnNames) != 3 {
		t.Errorf("Config.ColumnNames length = %d, expected 3", len(config.ColumnNames))
	}

	if config.ShowAllColumns {
		t.Error("Config.ShowAllColumns should be false")
	}

	if config.Width != "1000px" {
		t.Errorf("Config.Width = %s, expected '1000px'", config.Width)
	}
}

// TestColumnInfo 測試欄位信息結構.
func TestColumnInfo(t *testing.T) {
	_ = NewEChartsGenerator() // Keep generator creation for coverage
	dataset := createTestDataset()

	columns := GetAvailableColumns(dataset)

	if len(columns) == 0 {
		t.Fatal("GetAvailableColumns() returned no columns")
	}

	col := columns[0]

	// 測試 JSON 標籤是否正確（通過檢查結構）
	if col.Index == 0 {
		t.Error("Column.Index should not be 0")
	}

	if col.Name == "" {
		t.Error("Column.Name should not be empty")
	}

	if col.DataPoints == 0 {
		t.Error("Column.DataPoints should not be 0")
	}
}

// createTestDataset 為共用測試 fixture，原在 chart_test.go 內，
// 該檔案在 Wave 4 chart.go 整刪後一起移除，helper 移到這裡。
func createTestDataset() *models.EMGDataset {
	return &models.EMGDataset{
		Headers: []string{"Time", "Channel1", "Channel2", "Channel3"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{0.1, 0.2, 0.3}},
			{Time: 0.1, Channels: []float64{0.15, 0.25, 0.35}},
			{Time: 0.2, Channels: []float64{0.12, 0.22, 0.32}},
			{Time: 0.3, Channels: []float64{0.18, 0.28, 0.38}},
			{Time: 0.4, Channels: []float64{0.14, 0.24, 0.34}},
			{Time: 0.5, Channels: []float64{0.16, 0.26, 0.36}},
			{Time: 0.6, Channels: []float64{0.13, 0.23, 0.33}},
			{Time: 0.7, Channels: []float64{0.17, 0.27, 0.37}},
			{Time: 0.8, Channels: []float64{0.11, 0.21, 0.31}},
			{Time: 0.9, Channels: []float64{0.19, 0.29, 0.39}},
		},
	}
}

func createLargeTestDataset() *models.EMGDataset {
	headers := []string{"Time"}
	for i := 1; i <= 10; i++ {
		headers = append(headers, fmt.Sprintf("Channel%d", i))
	}

	data := make([]models.EMGData, 100)

	for i := 0; i < 100; i++ {
		channels := make([]float64, 10)
		for j := 0; j < 10; j++ {
			channels[j] = float64(i) * 0.01 * float64(j+1)
		}

		data[i] = models.EMGData{
			Time:     float64(i) * 0.01,
			Channels: channels,
		}
	}

	return &models.EMGDataset{
		Headers: headers,
		Data:    data,
	}
}
