package chart_test

import (
	"count_mean/internal/chart"
	"count_mean/internal/models"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gonum.org/v1/plot/vg"
)

// TestChartGenerator 測試圖表生成器
func TestChartGenerator(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testing.T)
	}{
		{"TestNewChartGenerator", testNewChartGenerator},
		{"TestGenerateLineChart", testGenerateLineChart},
		{"TestGenerateLineChartImage", testGenerateLineChartImage},
		{"TestGetCSVColumns", testGetCSVColumns},
		{"TestGenerateLineChartEmptyData", testGenerateLineChartEmptyData},
		{"TestGenerateLineChartInvalidColumns", testGenerateLineChartInvalidColumns},
		{"TestGenerateLineChartMultipleColumns", testGenerateLineChartMultipleColumns},
		{"TestGenerateLineChartColorCycle", testGenerateLineChartColorCycle},
		{"TestGenerateLineChartFileOperations", testGenerateLineChartFileOperations},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func testNewChartGenerator(t *testing.T) {
	generator := chart.NewChartGenerator()

	if generator == nil {
		t.Fatal("NewChartGenerator() returned nil")
	}

	// 測試 logger 是否正確初始化
	// 由於 logger 是私有字段，我們只能測試基本功能
	testConfig := chart.ChartConfig{
		Title:      "Test Chart",
		XAxisLabel: "Time",
		YAxisLabel: "Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"Channel1"},
	}

	dataset := createTestDataset()

	// 測試能否正常調用方法（不會 panic）
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("NewChartGenerator() created invalid generator: %v", r)
		}
	}()

	// 測試生成圖像功能
	_, err := generator.GenerateLineChartImage(dataset, testConfig)
	if err != nil {
		t.Errorf("GenerateLineChartImage() failed: %v", err)
	}
}

func testGenerateLineChart(t *testing.T) {
	generator := chart.NewChartGenerator()
	dataset := createTestDataset()

	// 創建臨時輸出目錄
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "test_chart.png")

	config := chart.ChartConfig{
		Title:      "Test Chart",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"Channel1"},
	}

	err := generator.GenerateLineChart(dataset, config, outputPath)
	if err != nil {
		t.Errorf("GenerateLineChart() failed: %v", err)
	}

	// 檢查文件是否已創建
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("GenerateLineChart() did not create output file")
	}

	// 檢查文件大小是否合理
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Errorf("Cannot get file info: %v", err)
	}

	if info.Size() == 0 {
		t.Error("Generated PNG file is empty")
	}
}

func testGenerateLineChartImage(t *testing.T) {
	generator := chart.NewChartGenerator()
	dataset := createTestDataset()

	config := chart.ChartConfig{
		Title:      "Test Chart Image",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"Channel1"},
	}

	img, err := generator.GenerateLineChartImage(dataset, config)
	if err != nil {
		t.Errorf("GenerateLineChartImage() failed: %v", err)
	}

	if img == nil {
		t.Error("GenerateLineChartImage() returned nil image")
	}

	// 檢查圖像尺寸
	bounds := img.Bounds()
	if bounds.Dx() == 0 || bounds.Dy() == 0 {
		t.Error("Generated image has zero dimensions")
	}
}

func testGetCSVColumns(t *testing.T) {
	generator := chart.NewChartGenerator()

	// 測試正常情況
	dataset := createTestDataset()
	columns := generator.GetCSVColumns(dataset)

	expectedColumns := []string{"Channel1", "Channel2", "Channel3"}
	if len(columns) != len(expectedColumns) {
		t.Errorf("GetCSVColumns() returned %d columns, expected %d", len(columns), len(expectedColumns))
	}

	// 檢查是否包含預期的列（應該按字母順序排列）
	for i, col := range columns {
		if col != expectedColumns[i] {
			t.Errorf("GetCSVColumns() column %d = %s, expected %s", i, col, expectedColumns[i])
		}
	}

	// 測試空數據集
	emptyDataset := &models.EMGDataset{}
	columns = generator.GetCSVColumns(emptyDataset)
	if len(columns) != 0 {
		t.Errorf("GetCSVColumns() with empty dataset returned %d columns, expected 0", len(columns))
	}

	// 測試 nil 數據集
	columns = generator.GetCSVColumns(nil)
	if len(columns) != 0 {
		t.Errorf("GetCSVColumns() with nil dataset returned %d columns, expected 0", len(columns))
	}

	// 測試只有時間列的數據集
	timeOnlyDataset := &models.EMGDataset{
		Headers: []string{"Time"},
	}
	columns = generator.GetCSVColumns(timeOnlyDataset)
	if len(columns) != 0 {
		t.Errorf("GetCSVColumns() with time-only dataset returned %d columns, expected 0", len(columns))
	}
}

func testGenerateLineChartEmptyData(t *testing.T) {
	generator := chart.NewChartGenerator()

	// 測試空數據集
	emptyDataset := &models.EMGDataset{
		Headers: []string{"Time", "Channel1"},
		Data:    []models.EMGData{},
	}

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "empty_chart.png")

	config := chart.ChartConfig{
		Title:      "Empty Chart",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"Channel1"},
	}

	err := generator.GenerateLineChart(emptyDataset, config, outputPath)
	if err != nil {
		t.Errorf("GenerateLineChart() with empty data failed: %v", err)
	}

	// 檢查文件是否已創建
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("GenerateLineChart() with empty data did not create output file")
	}
}

func testGenerateLineChartInvalidColumns(t *testing.T) {
	generator := chart.NewChartGenerator()
	dataset := createTestDataset()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "invalid_columns_chart.png")

	config := chart.ChartConfig{
		Title:      "Invalid Columns Chart",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"NonExistentColumn", "AnotherInvalidColumn"},
	}

	err := generator.GenerateLineChart(dataset, config, outputPath)
	if err != nil {
		t.Errorf("GenerateLineChart() with invalid columns failed: %v", err)
	}

	// 檢查文件是否已創建（即使沒有有效的列）
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("GenerateLineChart() with invalid columns did not create output file")
	}
}

func testGenerateLineChartMultipleColumns(t *testing.T) {
	generator := chart.NewChartGenerator()
	dataset := createTestDataset()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "multi_columns_chart.png")

	config := chart.ChartConfig{
		Title:      "Multiple Columns Chart",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"Channel1", "Channel2", "Channel3"},
	}

	err := generator.GenerateLineChart(dataset, config, outputPath)
	if err != nil {
		t.Errorf("GenerateLineChart() with multiple columns failed: %v", err)
	}

	// 檢查文件是否已創建
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("GenerateLineChart() with multiple columns did not create output file")
	}
}

func testGenerateLineChartColorCycle(t *testing.T) {
	generator := chart.NewChartGenerator()
	dataset := createLargeTestDataset()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "color_cycle_chart.png")

	// 測試超過8個顏色的情況（應該會循環使用顏色）
	config := chart.ChartConfig{
		Title:      "Color Cycle Chart",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"Channel1", "Channel2", "Channel3", "Channel4", "Channel5", "Channel6", "Channel7", "Channel8", "Channel9", "Channel10"},
	}

	err := generator.GenerateLineChart(dataset, config, outputPath)
	if err != nil {
		t.Errorf("GenerateLineChart() with color cycle failed: %v", err)
	}

	// 檢查文件是否已創建
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("GenerateLineChart() with color cycle did not create output file")
	}
}

func testGenerateLineChartFileOperations(t *testing.T) {
	generator := chart.NewChartGenerator()
	dataset := createTestDataset()

	tempDir := t.TempDir()

	// 測試深層目錄創建
	deepPath := filepath.Join(tempDir, "deep", "nested", "directory", "chart.png")

	config := chart.ChartConfig{
		Title:      "File Operations Chart",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"Channel1"},
	}

	err := generator.GenerateLineChart(dataset, config, deepPath)
	if err != nil {
		t.Errorf("GenerateLineChart() with deep directory failed: %v", err)
	}

	// 檢查文件是否已創建
	if _, err := os.Stat(deepPath); os.IsNotExist(err) {
		t.Error("GenerateLineChart() with deep directory did not create output file")
	}

	// 測試文件覆蓋
	err = generator.GenerateLineChart(dataset, config, deepPath)
	if err != nil {
		t.Errorf("GenerateLineChart() file overwrite failed: %v", err)
	}
}

// 輔助函數：創建測試數據集
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

// 輔助函數：創建大型測試數據集（用於測試顏色循環）
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

// BenchmarkGenerateLineChart 基準測試
func BenchmarkGenerateLineChart(b *testing.B) {
	generator := chart.NewChartGenerator()
	dataset := createTestDataset()

	tempDir := b.TempDir()

	config := chart.ChartConfig{
		Title:      "Benchmark Chart",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"Channel1", "Channel2", "Channel3"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		outputPath := filepath.Join(tempDir, fmt.Sprintf("bench_chart_%d.png", i))
		err := generator.GenerateLineChart(dataset, config, outputPath)
		if err != nil {
			b.Errorf("GenerateLineChart() benchmark failed: %v", err)
		}
	}
}

// BenchmarkGenerateLineChartImage 基準測試
func BenchmarkGenerateLineChartImage(b *testing.B) {
	generator := chart.NewChartGenerator()
	dataset := createTestDataset()

	config := chart.ChartConfig{
		Title:      "Benchmark Chart Image",
		XAxisLabel: "Time (s)",
		YAxisLabel: "EMG Value",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    []string{"Channel1", "Channel2", "Channel3"},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := generator.GenerateLineChartImage(dataset, config)
		if err != nil {
			b.Errorf("GenerateLineChartImage() benchmark failed: %v", err)
		}
	}
}

// TestChartConfig 測試圖表配置
func TestChartConfig(t *testing.T) {
	config := chart.ChartConfig{
		Title:      "Test Config",
		XAxisLabel: "X Axis",
		YAxisLabel: "Y Axis",
		Width:      vg.Points(1000),
		Height:     vg.Points(700),
		Columns:    []string{"Col1", "Col2"},
	}

	// 測試配置字段
	if config.Title != "Test Config" {
		t.Errorf("Config.Title = %s, expected 'Test Config'", config.Title)
	}

	if config.XAxisLabel != "X Axis" {
		t.Errorf("Config.XAxisLabel = %s, expected 'X Axis'", config.XAxisLabel)
	}

	if config.YAxisLabel != "Y Axis" {
		t.Errorf("Config.YAxisLabel = %s, expected 'Y Axis'", config.YAxisLabel)
	}

	if config.Width != vg.Points(1000) {
		t.Errorf("Config.Width = %v, expected %v", config.Width, vg.Points(1000))
	}

	if config.Height != vg.Points(700) {
		t.Errorf("Config.Height = %v, expected %v", config.Height, vg.Points(700))
	}

	if len(config.Columns) != 2 {
		t.Errorf("Config.Columns length = %d, expected 2", len(config.Columns))
	}
}
