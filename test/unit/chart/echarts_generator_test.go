package chart_test

import (
	"bytes"
	"count_mean/internal/chart"
	"count_mean/internal/models"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEChartsGenerator 測試 ECharts 圖表生成器
func TestEChartsGenerator(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestNewEChartsGenerator", testNewEChartsGenerator},
		{"TestGenerateInteractiveChart", testGenerateInteractiveChart},
		{"TestGetAvailableColumns", testGetAvailableColumns},
		{"TestRenderChartToWriter", testRenderChartToWriter},
		{"TestValidateDataset", testValidateDataset},
		{"TestSampleData", testSampleData},
		{"TestCalculateOptimalSampling", testCalculateOptimalSampling},
		{"TestGenerateComparisonChart", testGenerateComparisonChart},
		{"TestBatchExportCharts", testBatchExportCharts},
		{"TestConvertToJSON", testConvertToJSON},
		{"TestGetChartStatistics", testGetChartStatistics},
		{"TestOptimizeForLargeDataset", testOptimizeForLargeDataset},
		{"TestFormatValue", testFormatValue},
		{"TestGenerateExportScript", testGenerateExportScript},
		{"TestGenerateCustomTheme", testGenerateCustomTheme},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testNewEChartsGenerator(t *testing.T) {
	generator := chart.NewEChartsGenerator()

	if generator == nil {
		t.Fatal("NewEChartsGenerator() returned nil")
	}

	// 測試基本功能是否正常
	dataset := createTestDataset()
	err := generator.ValidateDataset(dataset)
	if err != nil {
		t.Errorf("ValidateDataset() failed: %v", err)
	}
}

func testGenerateInteractiveChart(t *testing.T) {
	generator := chart.NewEChartsGenerator()
	dataset := createTestDataset()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "interactive_chart.html")

	config := chart.InteractiveChartConfig{
		Title:           "Test Interactive Chart",
		XAxisLabel:      "Time (s)",
		YAxisLabel:      "EMG Value",
		SelectedColumns: []int{1, 2},
		ColumnNames:     []string{"Channel1", "Channel2"},
		ShowAllColumns:  false,
		Width:           "800px",
		Height:          "600px",
	}

	err := generator.GenerateInteractiveChart(dataset, config, outputPath)
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

func testGetAvailableColumns(t *testing.T) {
	generator := chart.NewEChartsGenerator()
	dataset := createTestDataset()

	columns := generator.GetAvailableColumns(dataset)

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
	generator := chart.NewEChartsGenerator()
	dataset := createTestDataset()

	config := chart.InteractiveChartConfig{
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

func testValidateDataset(t *testing.T) {
	generator := chart.NewEChartsGenerator()

	// 測試正常數據集
	dataset := createTestDataset()
	err := generator.ValidateDataset(dataset)
	if err != nil {
		t.Errorf("ValidateDataset() failed for valid dataset: %v", err)
	}

	// 測試 nil 數據集
	err = generator.ValidateDataset(nil)
	if err == nil {
		t.Error("ValidateDataset() should fail for nil dataset")
	}

	// 測試空標題
	emptyHeaders := &models.EMGDataset{
		Headers: []string{},
		Data:    []models.EMGData{},
	}
	err = generator.ValidateDataset(emptyHeaders)
	if err == nil {
		t.Error("ValidateDataset() should fail for empty headers")
	}

	// 測試空數據
	emptyData := &models.EMGDataset{
		Headers: []string{"Time", "Channel1"},
		Data:    []models.EMGData{},
	}
	err = generator.ValidateDataset(emptyData)
	if err == nil {
		t.Error("ValidateDataset() should fail for empty data")
	}

	// 測試不一致的通道數
	inconsistentData := &models.EMGDataset{
		Headers: []string{"Time", "Channel1", "Channel2"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{0.1}},      // 1 channel
			{Time: 0.1, Channels: []float64{0.2, 0.3}}, // 2 channels
		},
	}
	err = generator.ValidateDataset(inconsistentData)
	if err == nil {
		t.Error("ValidateDataset() should fail for inconsistent channel counts")
	}
}

func testSampleData(t *testing.T) {
	generator := chart.NewEChartsGenerator()
	dataset := createLargeTestDataset()

	// 測試正常採樣
	sampledData := generator.SampleData(dataset, 2)
	expectedSize := len(dataset.Data)/2 + 1
	if len(sampledData.Data) > expectedSize+1 { // +1 for potential last point
		t.Errorf("SampleData() returned %d points, expected around %d", len(sampledData.Data), expectedSize)
	}

	// 檢查標題是否保持不變
	if len(sampledData.Headers) != len(dataset.Headers) {
		t.Error("SampleData() changed headers")
	}

	// 檢查第一個和最後一個數據點
	if len(sampledData.Data) > 0 {
		if sampledData.Data[0].Time != dataset.Data[0].Time {
			t.Error("SampleData() changed first data point time")
		}

		lastSampled := sampledData.Data[len(sampledData.Data)-1]
		lastOriginal := dataset.Data[len(dataset.Data)-1]
		if lastSampled.Time != lastOriginal.Time {
			t.Error("SampleData() did not preserve last data point")
		}
	}

	// 測試採樣率 <= 1
	notSampledData := generator.SampleData(dataset, 1)
	if len(notSampledData.Data) != len(dataset.Data) {
		t.Error("SampleData() with rate 1 should not change data size")
	}
}

func testCalculateOptimalSampling(t *testing.T) {
	generator := chart.NewEChartsGenerator()

	// 測試正常情況
	rate := generator.CalculateOptimalSampling(1000, 500)
	if rate != 2 {
		t.Errorf("CalculateOptimalSampling(1000, 500) = %d, expected 2", rate)
	}

	// 測試數據點少於最大值
	rate = generator.CalculateOptimalSampling(300, 500)
	if rate != 1 {
		t.Errorf("CalculateOptimalSampling(300, 500) = %d, expected 1", rate)
	}

	// 測試邊界情況
	rate = generator.CalculateOptimalSampling(1500, 500)
	if rate != 3 {
		t.Errorf("CalculateOptimalSampling(1500, 500) = %d, expected 3", rate)
	}
}

func testGenerateComparisonChart(t *testing.T) {
	generator := chart.NewEChartsGenerator()
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

	config := chart.InteractiveChartConfig{
		Title:           "Comparison Chart",
		XAxisLabel:      "Time (s)",
		YAxisLabel:      "EMG Value",
		SelectedColumns: []int{1, 2},
		Width:           "800px",
		Height:          "600px",
	}

	err := generator.GenerateComparisonChart(datasets, labels, config, outputPath)
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

func testBatchExportCharts(t *testing.T) {
	generator := chart.NewEChartsGenerator()
	dataset := createTestDataset()

	tempDir := t.TempDir()

	columnGroups := [][]int{
		{1, 2},
		{2, 3},
		{1, 3},
	}

	baseConfig := chart.InteractiveChartConfig{
		Title:      "Batch Export",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      "800px",
		Height:     "600px",
	}

	err := generator.BatchExportCharts(dataset, columnGroups, baseConfig, tempDir)
	if err != nil {
		t.Errorf("BatchExportCharts() failed: %v", err)
	}

	// 檢查是否創建了所有文件
	for i := range columnGroups {
		expectedPath := filepath.Join(tempDir, fmt.Sprintf("chart_group_%d.html", i+1))
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("BatchExportCharts() did not create file %s", expectedPath)
		}
	}
}

func testConvertToJSON(t *testing.T) {
	generator := chart.NewEChartsGenerator()
	dataset := createTestDataset()

	selectedColumns := []int{1, 2}
	jsonData := generator.ConvertToJSON(dataset, selectedColumns)

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
	generator := chart.NewEChartsGenerator()
	dataset := createTestDataset()

	selectedColumns := []int{1, 2}
	stats := generator.GetChartStatistics(dataset, selectedColumns)

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
	columnStats, ok := stats["column_statistics"].([]map[string]interface{})
	if !ok {
		t.Error("GetChartStatistics() column_statistics is not the expected type")
	}

	if len(columnStats) != len(selectedColumns) {
		t.Errorf("GetChartStatistics() column_statistics length = %d, expected %d", len(columnStats), len(selectedColumns))
	}
}

func testOptimizeForLargeDataset(t *testing.T) {
	generator := chart.NewEChartsGenerator()
	dataset := createLargeTestDataset()

	maxPoints := 50
	optimizedData := generator.OptimizeForLargeDataset(dataset, maxPoints)

	if len(optimizedData.Data) > maxPoints {
		t.Errorf("OptimizeForLargeDataset() returned %d points, expected max %d", len(optimizedData.Data), maxPoints)
	}

	// 檢查標題是否保持不變
	if len(optimizedData.Headers) != len(dataset.Headers) {
		t.Error("OptimizeForLargeDataset() changed headers")
	}

	// 測試小數據集不變
	smallDataset := createTestDataset()
	optimizedSmall := generator.OptimizeForLargeDataset(smallDataset, maxPoints)
	if len(optimizedSmall.Data) != len(smallDataset.Data) {
		t.Error("OptimizeForLargeDataset() changed small dataset")
	}
}

func testFormatValue(t *testing.T) {
	generator := chart.NewEChartsGenerator()

	// 測試正常值
	result := generator.FormatValue(123.456, 2)
	if result != "123.46" {
		t.Errorf("FormatValue(123.456, 2) = %s, expected '123.46'", result)
	}

	// 測試小值（科學記數法）
	result = generator.FormatValue(0.0001, 2)
	if !strings.Contains(result, "e") {
		t.Errorf("FormatValue(0.0001, 2) = %s, expected scientific notation", result)
	}

	// 測試大值（科學記數法）
	result = generator.FormatValue(1e7, 2)
	if !strings.Contains(result, "e") {
		t.Errorf("FormatValue(1e7, 2) = %s, expected scientific notation", result)
	}
}

func testGenerateExportScript(t *testing.T) {
	generator := chart.NewEChartsGenerator()

	config := chart.ExportConfig{
		Format:   "png",
		Width:    800,
		Height:   600,
		DPI:      100,
		FileName: "test_chart.png",
	}

	script := generator.GenerateExportScript(config)

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

func testGenerateCustomTheme(t *testing.T) {
	generator := chart.NewEChartsGenerator()

	theme := generator.GenerateCustomTheme()

	if theme == "" {
		t.Error("GenerateCustomTheme() returned empty string")
	}

	if !strings.Contains(theme, "color") {
		t.Error("GenerateCustomTheme() does not contain color configuration")
	}

	if !strings.Contains(theme, "backgroundColor") {
		t.Error("GenerateCustomTheme() does not contain backgroundColor")
	}

	if !strings.Contains(theme, "line") {
		t.Error("GenerateCustomTheme() does not contain line configuration")
	}
}

// 基準測試
func BenchmarkGenerateInteractiveChart(b *testing.B) {
	generator := chart.NewEChartsGenerator()
	dataset := createTestDataset()

	tempDir := b.TempDir()

	config := chart.InteractiveChartConfig{
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
		err := generator.GenerateInteractiveChart(dataset, config, outputPath)
		if err != nil {
			b.Errorf("GenerateInteractiveChart() benchmark failed: %v", err)
		}
	}
}

func BenchmarkSampleData(b *testing.B) {
	generator := chart.NewEChartsGenerator()
	dataset := createLargeTestDataset()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = generator.SampleData(dataset, 5)
	}
}

func BenchmarkGetAvailableColumns(b *testing.B) {
	generator := chart.NewEChartsGenerator()
	dataset := createLargeTestDataset()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = generator.GetAvailableColumns(dataset)
	}
}

// TestInteractiveChartConfig 測試互動式圖表配置
func TestInteractiveChartConfig(t *testing.T) {
	config := chart.InteractiveChartConfig{
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

// TestColumnInfo 測試欄位信息結構
func TestColumnInfo(t *testing.T) {
	generator := chart.NewEChartsGenerator()
	dataset := createTestDataset()

	columns := generator.GetAvailableColumns(dataset)

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
