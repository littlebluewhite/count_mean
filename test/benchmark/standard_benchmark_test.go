package benchmark_test

import (
	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkMathCalculation 數學計算基準測試
func BenchmarkMathCalculation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sum := 0.0
		for j := 0; j < 100000; j++ {
			sum += float64(j) * 1.1
		}
	}
}

// BenchmarkStringProcessing 字符串處理基準測試
func BenchmarkStringProcessing(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for j := 0; j < 10000; j++ {
			result := fmt.Sprintf("test_string_%d", j)
			_ = len(result)
		}
	}
}

// BenchmarkArrayProcessing 陣列處理基準測試
func BenchmarkArrayProcessing(b *testing.B) {
	for i := 0; i < b.N; i++ {
		data := make([]int, 100000)
		for j := range data {
			data[j] = j * 2
		}

		sum := 0
		for _, v := range data {
			sum += v
		}
	}
}

// BenchmarkCSVReading CSV讀取基準測試
func BenchmarkCSVReading(b *testing.B) {
	// 設置測試數據
	tempFile := createTestCSV(b)
	defer os.Remove(tempFile)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := parsers.ReadCSVDirect(tempFile)
		if err != nil {
			b.Fatalf("讀取CSV失敗: %v", err)
		}
	}
}

// BenchmarkCSVParsing CSV解析基準測試
func BenchmarkCSVParsing(b *testing.B) {
	// 設置測試數據
	tempFile := createTestCSV(b)
	defer os.Remove(tempFile)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := parsers.ReadCSVDirect(tempFile)
		if err != nil {
			b.Fatalf("解析CSV失敗: %v", err)
		}
	}
}

// BenchmarkMaxMeanCalculation 最大平均值計算基準測試
func BenchmarkMaxMeanCalculation(b *testing.B) {
	// 準備測試數據
	dataset := &models.EMGDataset{
		Headers: []string{"time", "channel1", "channel2", "channel3"},
		Data:    make([]models.EMGData, 10000),
	}

	for i := range dataset.Data {
		dataset.Data[i] = models.EMGData{
			Time:     float64(i) * 0.01,
			Channels: []float64{float64(i) * 1.1, float64(i) * 1.2, float64(i) * 1.3},
		}
	}

	calc := calculator.NewMaxMeanCalculator(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := calc.Calculate(dataset, 100)
		if err != nil {
			b.Fatalf("計算失敗: %v", err)
		}
	}
}

// BenchmarkDataNormalization 數據標準化基準測試
func BenchmarkDataNormalization(b *testing.B) {
	// 準備測試數據
	dataset := &models.EMGDataset{
		Headers: []string{"time", "channel1", "channel2", "channel3"},
		Data:    make([]models.EMGData, 10000),
	}

	for i := range dataset.Data {
		dataset.Data[i] = models.EMGData{
			Time:     float64(i) * 0.01,
			Channels: []float64{float64(i) * 1.1, float64(i) * 1.2, float64(i) * 1.3},
		}
	}

	// 創建參考數據集
	reference := &models.EMGDataset{
		Headers: []string{"time", "channel1", "channel2", "channel3"},
		Data:    make([]models.EMGData, 100),
	}

	for i := range reference.Data {
		reference.Data[i] = models.EMGData{
			Time:     float64(i) * 0.01,
			Channels: []float64{100.0, 100.0, 100.0},
		}
	}

	normalizer := calculator.NewNormalizer(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := normalizer.Normalize(dataset, reference)
		if err != nil {
			b.Fatalf("標準化失敗: %v", err)
		}
	}
}

// BenchmarkPhaseAnalysis 階段分析基準測試
func BenchmarkPhaseAnalysis(b *testing.B) {
	// 準備測試數據
	dataset := &models.EMGDataset{
		Headers: []string{"time", "channel1", "channel2", "channel3"},
		Data:    make([]models.EMGData, 10000),
	}

	for i := range dataset.Data {
		dataset.Data[i] = models.EMGData{
			Time:     float64(i) * 0.01,
			Channels: []float64{float64(i) * 1.1, float64(i) * 1.2, float64(i) * 1.3},
		}
	}

	// 創建階段
	phases := []models.TimeRange{
		{Start: 0.0, End: 30.0},
		{Start: 30.0, End: 60.0},
		{Start: 60.0, End: 90.0},
	}

	phaseLabels := []string{"phase1", "phase2", "phase3"}
	analyzer := calculator.NewPhaseAnalyzer(100, phaseLabels)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := analyzer.Analyze(dataset, phases)
		if err != nil {
			b.Fatalf("階段分析失敗: %v", err)
		}
	}
}

// BenchmarkConcurrentDataProcessing 並行數據處理基準測試
func BenchmarkConcurrentDataProcessing(b *testing.B) {
	// 準備測試數據
	data := make([][]float64, 10)
	for i := range data {
		data[i] = make([]float64, 1000)
		for j := range data[i] {
			data[i][j] = float64(j) * 1.1
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 模擬並行處理
		done := make(chan bool)
		for _, row := range data {
			go func(r []float64) {
				sum := 0.0
				for _, v := range r {
					sum += v
				}
				done <- true
			}(row)
		}

		for range data {
			<-done
		}
	}
}

// BenchmarkMemoryIntensiveOperation 記憶體密集操作基準測試
func BenchmarkMemoryIntensiveOperation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 大量記憶體分配
		data := make([][]float64, 1000)
		for j := range data {
			data[j] = make([]float64, 1000)
			for k := range data[j] {
				data[j][k] = float64(k) * 1.1
			}
		}

		// 處理數據
		sum := 0.0
		for _, row := range data {
			for _, v := range row {
				sum += v
			}
		}
	}
}

// BenchmarkLargeFileProcessing 大檔案處理基準測試
func BenchmarkLargeFileProcessing(b *testing.B) {
	// 創建大檔案
	tempFile := createLargeTestCSV(b)
	defer os.Remove(tempFile)

	cfg := config.DefaultConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler := io.NewLargeFileHandler(cfg)
		_, err := handler.ProcessLargeFileInChunks(tempFile, 1000, func(processed int64, total int64, percentage float64) {
			// 模擬進度回調
		})
		if err != nil {
			b.Fatalf("處理大檔案失敗: %v", err)
		}
	}
}

// createTestCSV 創建測試CSV檔案
func createTestCSV(b *testing.B) string {
	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("test_benchmark_%d.csv", time.Now().UnixNano()))

	file, err := os.Create(tempFile)
	if err != nil {
		b.Fatalf("創建測試檔案失敗: %v", err)
	}
	defer file.Close()

	// 寫入標題
	file.WriteString("time,channel1,channel2,channel3\n")

	// 寫入測試數據
	for i := 0; i < 1000; i++ {
		file.WriteString(fmt.Sprintf("%.2f,%.2f,%.2f,%.2f\n",
			float64(i)*0.01, float64(i)*1.1, float64(i)*1.2, float64(i)*1.3))
	}

	return tempFile
}

// createLargeTestCSV 創建大測試CSV檔案
func createLargeTestCSV(b *testing.B) string {
	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("large_test_benchmark_%d.csv", time.Now().UnixNano()))

	file, err := os.Create(tempFile)
	if err != nil {
		b.Fatalf("創建大測試檔案失敗: %v", err)
	}
	defer file.Close()

	// 寫入標題
	file.WriteString("time,channel1,channel2,channel3\n")

	// 寫入大量測試數據
	for i := 0; i < 10000; i++ {
		file.WriteString(fmt.Sprintf("%.2f,%.2f,%.2f,%.2f\n",
			float64(i)*0.01, float64(i)*1.1, float64(i)*1.2, float64(i)*1.3))
	}

	return tempFile
}

// 子基準測試示例
func BenchmarkCalculatorSuite(b *testing.B) {
	b.Run("MaxMean", func(b *testing.B) {
		dataset := &models.EMGDataset{
			Headers: []string{"time", "channel1", "channel2", "channel3"},
			Data:    make([]models.EMGData, 1000),
		}

		for i := range dataset.Data {
			dataset.Data[i] = models.EMGData{
				Time:     float64(i) * 0.01,
				Channels: []float64{float64(i) * 1.1, float64(i) * 1.2, float64(i) * 1.3},
			}
		}

		calc := calculator.NewMaxMeanCalculator(100)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := calc.Calculate(dataset, 100)
			if err != nil {
				b.Fatalf("計算失敗: %v", err)
			}
		}
	})

	b.Run("Normalization", func(b *testing.B) {
		dataset := &models.EMGDataset{
			Headers: []string{"time", "channel1", "channel2", "channel3"},
			Data:    make([]models.EMGData, 1000),
		}

		for i := range dataset.Data {
			dataset.Data[i] = models.EMGData{
				Time:     float64(i) * 0.01,
				Channels: []float64{float64(i) * 1.1, float64(i) * 1.2, float64(i) * 1.3},
			}
		}

		reference := &models.EMGDataset{
			Headers: []string{"time", "channel1", "channel2", "channel3"},
			Data:    make([]models.EMGData, 100),
		}

		for i := range reference.Data {
			reference.Data[i] = models.EMGData{
				Time:     float64(i) * 0.01,
				Channels: []float64{100.0, 100.0, 100.0},
			}
		}

		normalizer := calculator.NewNormalizer(100)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := normalizer.Normalize(dataset, reference)
			if err != nil {
				b.Fatalf("標準化失敗: %v", err)
			}
		}
	})
}
