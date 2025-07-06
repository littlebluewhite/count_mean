package main

import (
	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// BenchmarkMaxMeanCalculator_Calculate 基準測試最大平均值計算性能
func BenchmarkMaxMeanCalculator_Calculate(b *testing.B) {
	calc := calculator.NewMaxMeanCalculator(10)

	// 創建測試數據集
	dataset := generateEMGDataset(1000, 8) // 1000行，8個通道

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := calc.Calculate(dataset, 10)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMaxMeanCalculator_CalculateFromRawData 基準測試從原始數據計算性能
func BenchmarkMaxMeanCalculator_CalculateFromRawData(b *testing.B) {
	calc := calculator.NewMaxMeanCalculator(10)

	// 創建原始數據
	records := generateRawData(1000, 8) // 1000行，8個通道

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := calc.CalculateFromRawData(records, 10)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNormalizer_Normalize 基準測試數據標準化性能
func BenchmarkNormalizer_Normalize(b *testing.B) {
	normalizer := calculator.NewNormalizer(10)

	// 創建主數據和參考數據
	mainDataset := generateEMGDataset(1000, 8)
	refDataset := generateEMGDataset(100, 8)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := normalizer.Normalize(mainDataset, refDataset)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkNormalizer_NormalizeFromRawData 基準測試從原始數據標準化性能
func BenchmarkNormalizer_NormalizeFromRawData(b *testing.B) {
	normalizer := calculator.NewNormalizer(10)

	// 創建原始數據
	mainRecords := generateRawData(1000, 8)
	refRecords := generateRawData(100, 8)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := normalizer.NormalizeFromRawData(mainRecords, refRecords)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPhaseAnalyzer_Analyze 基準測試階段分析性能
func BenchmarkPhaseAnalyzer_Analyze(b *testing.B) {
	phaseLabels := []string{"Phase1", "Phase2", "Phase3", "Phase4"}
	analyzer := calculator.NewPhaseAnalyzer(10, phaseLabels)

	// 創建測試數據集
	dataset := generateEMGDataset(1000, 8)

	// 創建階段範圍
	phases := []models.TimeRange{
		{Start: 0, End: 250e+10},
		{Start: 250e+10, End: 500e+10},
		{Start: 500e+10, End: 750e+10},
		{Start: 750e+10, End: 1000e+10},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := analyzer.Analyze(dataset, phases)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPhaseAnalyzer_AnalyzeFromRawData 基準測試從原始數據階段分析性能
func BenchmarkPhaseAnalyzer_AnalyzeFromRawData(b *testing.B) {
	phaseLabels := []string{"Phase1", "Phase2", "Phase3", "Phase4"}
	analyzer := calculator.NewPhaseAnalyzer(10, phaseLabels)

	// 創建原始數據
	records := generateRawData(1000, 8)
	phaseStrings := []string{"0", "250", "500", "750", "1000"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCSVHandler_ReadCSV 基準測試CSV讀取性能
func BenchmarkCSVHandler_ReadCSV(b *testing.B) {
	cfg := config.DefaultConfig()
	handler := io.NewCSVHandler(cfg)

	// 創建臨時CSV文件
	tempFile := createTempCSVFile(b, 1000, 8)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := handler.ReadCSV(tempFile)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCSVHandler_WriteCSV 基準測試CSV寫入性能
func BenchmarkCSVHandler_WriteCSV(b *testing.B) {
	cfg := config.DefaultConfig()
	handler := io.NewCSVHandler(cfg)

	// 創建測試數據
	data := generateCSVData(1000, 8)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		outputFile := fmt.Sprintf("/tmp/benchmark_output_%d.csv", i)
		err := handler.WriteCSV(outputFile, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkFullWorkflow_MaxMean 基準測試完整最大平均值工作流程性能
func BenchmarkFullWorkflow_MaxMean(b *testing.B) {
	cfg := config.DefaultConfig()
	csvHandler := io.NewCSVHandler(cfg)
	calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)

	// 創建測試數據
	records := generateRawData(1000, 8)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// 執行計算
		results, err := calculator.CalculateFromRawData(records, 10)
		if err != nil {
			b.Fatal(err)
		}

		// 轉換為CSV格式
		outputData := csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, 0, 999)
		if len(outputData) == 0 {
			b.Fatal("No output data generated")
		}
	}
}

// BenchmarkMemoryAllocation_LargeDataset 基準測試大數據集內存分配
func BenchmarkMemoryAllocation_LargeDataset(b *testing.B) {
	calc := calculator.NewMaxMeanCalculator(10)

	// 不同規模的數據集
	sizes := []int{100, 500, 1000, 5000, 10000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			dataset := generateEMGDataset(size, 8)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := calc.Calculate(dataset, 10)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkScalingFactor_Impact 基準測試縮放因子對性能的影響
func BenchmarkScalingFactor_Impact(b *testing.B) {
	scalingFactors := []int{1, 6, 10, 12}

	for _, factor := range scalingFactors {
		b.Run(fmt.Sprintf("Factor_%d", factor), func(b *testing.B) {
			calc := calculator.NewMaxMeanCalculator(factor)
			records := generateRawData(1000, 8)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := calc.CalculateFromRawData(records, 10)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// 生成EMG數據集的輔助函數
func generateEMGDataset(rows, channels int) *models.EMGDataset {
	rand.Seed(time.Now().UnixNano())

	headers := make([]string, channels+1)
	headers[0] = "Time"
	for i := 1; i <= channels; i++ {
		headers[i] = fmt.Sprintf("Ch%d", i)
	}

	data := make([]models.EMGData, rows)
	for i := 0; i < rows; i++ {
		channelData := make([]float64, channels)
		for j := 0; j < channels; j++ {
			channelData[j] = rand.Float64() * 1000
		}

		data[i] = models.EMGData{
			Time:     float64(i) * 1e+10, // 已縮放的時間
			Channels: channelData,
		}
	}

	return &models.EMGDataset{
		Headers: headers,
		Data:    data,
	}
}

// 生成原始CSV數據的輔助函數
func generateRawData(rows, channels int) [][]string {
	rand.Seed(time.Now().UnixNano())

	records := make([][]string, rows+1)

	// 標題行
	headers := make([]string, channels+1)
	headers[0] = "Time"
	for i := 1; i <= channels; i++ {
		headers[i] = fmt.Sprintf("Ch%d", i)
	}
	records[0] = headers

	// 數據行
	for i := 1; i <= rows; i++ {
		row := make([]string, channels+1)
		row[0] = fmt.Sprintf("%d", i-1) // 時間
		for j := 1; j <= channels; j++ {
			row[j] = fmt.Sprintf("%.3f", rand.Float64()*1000)
		}
		records[i] = row
	}

	return records
}

// 生成CSV數據的輔助函數
func generateCSVData(rows, columns int) [][]string {
	rand.Seed(time.Now().UnixNano())

	data := make([][]string, rows)
	for i := 0; i < rows; i++ {
		row := make([]string, columns)
		for j := 0; j < columns; j++ {
			row[j] = fmt.Sprintf("%.3f", rand.Float64()*1000)
		}
		data[i] = row
	}

	return data
}

// 創建臨時CSV文件的輔助函數
func createTempCSVFile(b *testing.B, rows, channels int) string {
	b.Helper()

	tempFile := fmt.Sprintf("/tmp/benchmark_input_%d.csv", time.Now().UnixNano())

	cfg := config.DefaultConfig()
	handler := io.NewCSVHandler(cfg)

	data := generateCSVData(rows, channels+1) // +1 for time column

	// 添加標題
	headers := make([]string, channels+1)
	headers[0] = "Time"
	for i := 1; i <= channels; i++ {
		headers[i] = fmt.Sprintf("Ch%d", i)
	}

	fullData := make([][]string, len(data)+1)
	fullData[0] = headers
	copy(fullData[1:], data)

	err := handler.WriteCSV(tempFile, fullData)
	if err != nil {
		b.Fatal(err)
	}

	return tempFile
}
