package benchmark_test

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// TestBenchmarkPlaceholder is a placeholder test to satisfy go test.
func TestBenchmarkPlaceholder(_ *testing.T) {
	// This package contains only benchmarks.
	// Run benchmarks with: go test -bench=. ./test/benchmark
}

// BenchmarkMathCalculation 數學計算基準測試.
func BenchmarkMathCalculation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		sum := 0.0
		for j := 0; j < 100000; j++ {
			sum += float64(j) * 1.1
		}
	}
}

// BenchmarkStringProcessing 字符串處理基準測試.
func BenchmarkStringProcessing(b *testing.B) {
	for i := 0; i < b.N; i++ {
		for j := 0; j < 10000; j++ {
			result := fmt.Sprintf("test_string_%d", j)
			_ = len(result)
		}
	}
}

// BenchmarkArrayProcessing 陣列處理基準測試.
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

// BenchmarkCSVReading CSV讀取基準測試.
func BenchmarkCSVReading(b *testing.B) {
	// 設置測試數據
	tempFile := createTestCSV(b)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		f, err := os.Open(tempFile)
		if err != nil {
			b.Fatalf("開啟CSV失敗: %v", err)
		}
		_, err = parsers.ReadCSVRecords(f)
		_ = f.Close()
		if err != nil {
			b.Fatalf("讀取CSV失敗: %v", err)
		}
	}
}

// BenchmarkCSVParsing CSV解析基準測試.
func BenchmarkCSVParsing(b *testing.B) {
	// 設置測試數據
	tempFile := createTestCSV(b)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		f, err := os.Open(tempFile)
		if err != nil {
			b.Fatalf("開啟CSV失敗: %v", err)
		}
		_, err = parsers.ReadCSVRecords(f)
		_ = f.Close()
		if err != nil {
			b.Fatalf("解析CSV失敗: %v", err)
		}
	}
}

// BenchmarkMaxMeanCalculation 最大平均值計算基準測試.
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
		_, err := calc.Calculate(context.Background(), dataset, 100)
		if err != nil {
			b.Fatalf("計算失敗: %v", err)
		}
	}
}

// BenchmarkNormalizer_Standardize_PreallocSlice 在 30 萬點 × 16 channel 規模下
// 量測 Normalizer.Normalize 的 alloc 量，驗證 buffer 預配對 GC 壓力的優化。
// 對照 BenchmarkDataNormalization (10k × 3 channel)，這個 benchmark 接近真實 EMG 場景。
func BenchmarkNormalizer_Standardize_PreallocSlice(b *testing.B) {
	const points = 300_000
	const channelCount = 16

	headers := make([]string, channelCount+1)
	headers[0] = "time"

	for i := 1; i <= channelCount; i++ {
		headers[i] = fmt.Sprintf("ch%d", i)
	}

	dataset := &models.EMGDataset{
		Headers: headers,
		Data:    make([]models.EMGData, points),
	}

	for i := range dataset.Data {
		ch := make([]float64, channelCount)
		for k := range ch {
			ch[k] = float64(i%1000+1) * 1.1
		}

		dataset.Data[i] = models.EMGData{
			Time:     float64(i) * 0.001,
			Channels: ch,
		}
	}

	reference := &models.EMGDataset{
		Headers: headers,
		Data: []models.EMGData{
			{
				Time: 0,
				Channels: func() []float64 {
					ch := make([]float64, channelCount)
					for k := range ch {
						ch[k] = 100.0
					}

					return ch
				}(),
			},
		},
	}

	normalizer := calculator.NewNormalizer(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := normalizer.Normalize(dataset, reference)
		if err != nil {
			b.Fatalf("標準化失敗: %v", err)
		}
	}
}

// BenchmarkDataNormalization 數據標準化基準測試.
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

	// 創建參考數據集（H21：MVC/MAX 參考一律單行）
	reference := &models.EMGDataset{
		Headers: []string{"time", "channel1", "channel2", "channel3"},
		Data: []models.EMGData{
			{Time: 0, Channels: []float64{100.0, 100.0, 100.0}},
		},
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

// BenchmarkPhaseAnalysis 階段分析基準測試.
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

// BenchmarkConcurrentDataProcessing 並行數據處理基準測試.
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

// BenchmarkMemoryIntensiveOperation 記憶體密集操作基準測試.
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

// BenchmarkLargeFileHandler_SlidingWindow PR-D streaming sliding window 重寫
// 後的效能基準。windowSize=1000、N=100_000、channels=16 模擬實際 EMG 大檔場景。
//
// 對照：plan 描述舊版 O(n × windowSize² × channels) 在此規模屬災難級
// （cross-validation test `large_w500_n3000_c16` 即跑 8.68s）；新版
// O(n × channels) rolling sum 在同尺度應 < 100ms。
func BenchmarkLargeFileHandler_SlidingWindow(b *testing.B) {
	const (
		records    = 100_000
		channels   = 16
		windowSize = 1000
	)

	tempFile := createSlidingWindowCSV(b, records, channels)
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler := io.NewLargeFileHandler(cfg)
		if _, err := handler.ProcessLargeFileInChunks(tempFile, windowSize, nil); err != nil {
			b.Fatalf("ProcessLargeFileInChunks 失敗: %v", err)
		}
	}
}

// createSlidingWindowCSV 產生 records 列 × (1+channels) 欄合成 EMG CSV
// 給 BenchmarkLargeFileHandler_SlidingWindow 用，sine 波保證資料變化。
func createSlidingWindowCSV(b *testing.B, records, channels int) string {
	b.Helper()
	tempFile := filepath.Join(b.TempDir(),
		fmt.Sprintf("sliding_w_n%d_c%d_%d.csv", records, channels, time.Now().UnixNano()))

	file, err := os.Create(tempFile) //nolint:gosec // benchmark fixture in tempdir
	if err != nil {
		b.Fatalf("創建測試檔案失敗: %v", err)
	}
	defer func() { _ = file.Close() }()

	writer := bufio.NewWriterSize(file, 64*1024)

	if _, err := writer.WriteString("time"); err != nil {
		b.Fatalf("寫入標題失敗: %v", err)
	}
	for c := 1; c <= channels; c++ {
		if _, err := fmt.Fprintf(writer, ",ch%d", c); err != nil {
			b.Fatalf("寫入標題欄位失敗: %v", err)
		}
	}
	if _, err := writer.WriteString("\n"); err != nil {
		b.Fatalf("寫入標題換行失敗: %v", err)
	}

	for i := 0; i < records; i++ {
		if _, err := fmt.Fprintf(writer, "%.4f", float64(i)*0.001); err != nil {
			b.Fatalf("寫入時間失敗: %v", err)
		}
		for c := 1; c <= channels; c++ {
			val := math.Sin(float64(i*c)*0.001) * 100
			if _, err := fmt.Fprintf(writer, ",%.4f", val); err != nil {
				b.Fatalf("寫入通道資料失敗: %v", err)
			}
		}
		if _, err := writer.WriteString("\n"); err != nil {
			b.Fatalf("寫入記錄換行失敗: %v", err)
		}
	}
	if err := writer.Flush(); err != nil {
		b.Fatalf("flush 失敗: %v", err)
	}

	return tempFile
}

// BenchmarkLargeFileProcessing 大檔案處理基準測試.
func BenchmarkLargeFileProcessing(b *testing.B) {
	// 創建大檔案
	tempFile := createLargeTestCSV(b)

	cfg := config.DefaultConfig()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		handler := io.NewLargeFileHandler(cfg)

		_, err := handler.ProcessLargeFileInChunks(tempFile, 1000, func(_, total int64, percentage float64) {
			// 模擬進度回調
			_ = total
			_ = percentage
		})
		if err != nil {
			b.Fatalf("處理大檔案失敗: %v", err)
		}
	}
}

// createTestCSV 創建測試CSV檔案.
func createTestCSV(b *testing.B) string {
	tempFile := filepath.Join(b.TempDir(), fmt.Sprintf("test_benchmark_%d.csv", time.Now().UnixNano()))

	file, err := os.Create(tempFile)
	if err != nil {
		b.Fatalf("創建測試檔案失敗: %v", err)
	}
	defer file.Close()

	// 寫入標題
	if _, err := file.WriteString("time,channel1,channel2,channel3\n"); err != nil {
		b.Fatalf("寫入標題失敗: %v", err)
	}

	// 寫入測試數據
	for i := 0; i < 1000; i++ {
		if _, err := fmt.Fprintf(file, "%.2f,%.2f,%.2f,%.2f\n",
			float64(i)*0.01, float64(i)*1.1, float64(i)*1.2, float64(i)*1.3); err != nil {
			b.Fatalf("寫入數據失敗: %v", err)
		}
	}

	return tempFile
}

// createLargeTestCSV 創建大測試CSV檔案.
func createLargeTestCSV(b *testing.B) string {
	tempFile := filepath.Join(b.TempDir(), fmt.Sprintf("large_test_benchmark_%d.csv", time.Now().UnixNano()))

	file, err := os.Create(tempFile)
	if err != nil {
		b.Fatalf("創建大測試檔案失敗: %v", err)
	}
	defer file.Close()

	// 寫入標題
	if _, err := file.WriteString("time,channel1,channel2,channel3\n"); err != nil {
		b.Fatalf("寫入標題失敗: %v", err)
	}

	// 寫入大量測試數據
	for i := 0; i < 10000; i++ {
		if _, err := fmt.Fprintf(file, "%.2f,%.2f,%.2f,%.2f\n",
			float64(i)*0.01, float64(i)*1.1, float64(i)*1.2, float64(i)*1.3); err != nil {
			b.Fatalf("寫入數據失敗: %v", err)
		}
	}

	return tempFile
}

// 子基準測試示例.
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
			_, err := calc.Calculate(context.Background(), dataset, 100)
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

		// MVC/MAX 參考一律單行
		reference := &models.EMGDataset{
			Headers: []string{"time", "channel1", "channel2", "channel3"},
			Data: []models.EMGData{
				{Time: 0, Channels: []float64{100.0, 100.0, 100.0}},
			},
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
