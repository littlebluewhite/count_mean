package benchmark

import (
	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CSVBenchmarks CSV 相關性能測試
type CSVBenchmarks struct {
	benchmarker *Benchmarker
	config      *config.AppConfig
	tempDir     string
}

// NewCSVBenchmarks 創建 CSV 性能測試器
func NewCSVBenchmarks(cfg *config.AppConfig) (*CSVBenchmarks, error) {
	tempDir := filepath.Join(os.TempDir(), "emg_benchmark_"+strconv.FormatInt(time.Now().Unix(), 10))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("無法創建臨時目錄: %v", err)
	}

	return &CSVBenchmarks{
		benchmarker: NewBenchmarker(cfg),
		config:      cfg,
		tempDir:     tempDir,
	}, nil
}

// generateTestCSV 生成測試用的 CSV 文件
func (cb *CSVBenchmarks) generateTestCSV(filename string, rows, cols int) (string, int64, error) {
	filePath := filepath.Join(cb.tempDir, filename)
	file, err := os.Create(filePath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	// 寫入標題行
	header := make([]string, cols)
	for i := 0; i < cols; i++ {
		header[i] = fmt.Sprintf("column_%d", i+1)
	}
	file.WriteString(strings.Join(header, ",") + "\n")

	// 寫入數據行
	rand.Seed(time.Now().UnixNano())
	for i := 0; i < rows; i++ {
		row := make([]string, cols)
		for j := 0; j < cols; j++ {
			// 生成隨機數值（模擬 EMG 數據）
			value := rand.Float64()*1000 - 500 // -500 到 500 的隨機數
			row[j] = fmt.Sprintf("%.6f", value)
		}
		file.WriteString(strings.Join(row, ",") + "\n")
	}

	// 獲取文件大小
	info, err := file.Stat()
	if err != nil {
		return filePath, 0, err
	}

	return filePath, info.Size(), nil
}

// BenchmarkCSVReading 測試 CSV 讀取性能
func (cb *CSVBenchmarks) BenchmarkCSVReading() {
	testCases := []struct {
		name string
		rows int
		cols int
	}{
		{"小文件_100行_10列", 100, 10},
		{"中文件_1000行_20列", 1000, 20},
		{"大文件_10000行_50列", 10000, 50},
		{"超大文件_50000行_100列", 50000, 100},
	}

	csvHandler := io.NewCSVHandler(cb.config)

	for _, tc := range testCases {
		filePath, fileSize, err := cb.generateTestCSV(
			fmt.Sprintf("test_%s.csv", tc.name), tc.rows, tc.cols)
		if err != nil {
			cb.benchmarker.logger.Error("生成測試文件失敗", err)
			continue
		}

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("CSV讀取_%s", tc.name),
			fileSize,
			func() error {
				_, err := csvHandler.ReadCSV(filePath)
				return err
			},
		)
	}
}

// BenchmarkMaxMeanCalculation 測試最大均值計算性能
func (cb *CSVBenchmarks) BenchmarkMaxMeanCalculation() {
	testCases := []struct {
		name       string
		rows       int
		cols       int
		windowSize int
	}{
		{"小數據集_窗口50", 1000, 10, 50},
		{"中數據集_窗口100", 5000, 20, 100},
		{"大數據集_窗口200", 20000, 50, 200},
		{"超大數據集_窗口500", 50000, 100, 500},
	}

	csvHandler := io.NewCSVHandler(cb.config)

	for _, tc := range testCases {
		// 生成測試文件
		filePath, fileSize, err := cb.generateTestCSV(
			fmt.Sprintf("maxmean_%s.csv", tc.name), tc.rows, tc.cols)
		if err != nil {
			cb.benchmarker.logger.Error("生成測試文件失敗", err)
			continue
		}

		// 先讀取數據
		data, err := csvHandler.ReadCSV(filePath)
		if err != nil {
			cb.benchmarker.logger.Error("讀取測試數據失敗", err)
			continue
		}

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("最大均值計算_%s", tc.name),
			fileSize,
			func() error {
				calc := calculator.NewMaxMeanCalculator(cb.config.ScalingFactor)
				_, err := calc.CalculateFromRawDataWithRange(data, tc.windowSize, 0.0, float64(tc.rows))
				return err
			},
		)
	}
}

// BenchmarkNormalization 測試數據正規化性能
func (cb *CSVBenchmarks) BenchmarkNormalization() {
	testCases := []struct {
		name string
		rows int
		cols int
	}{
		{"正規化_小數據", 1000, 10},
		{"正規化_中數據", 10000, 20},
		{"正規化_大數據", 50000, 50},
	}

	csvHandler := io.NewCSVHandler(cb.config)

	for _, tc := range testCases {
		// 生成測試文件
		filePath, fileSize, err := cb.generateTestCSV(
			fmt.Sprintf("norm_%s.csv", tc.name), tc.rows, tc.cols)
		if err != nil {
			cb.benchmarker.logger.Error("生成測試文件失敗", err)
			continue
		}

		// 先讀取數據
		data, err := csvHandler.ReadCSV(filePath)
		if err != nil {
			cb.benchmarker.logger.Error("讀取測試數據失敗", err)
			continue
		}

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("數據正規化_%s", tc.name),
			fileSize,
			func() error {
				normalizer := calculator.NewNormalizer(cb.config.ScalingFactor)
				// 使用自身作為參考數據進行正規化（演示用途）
				_, err := normalizer.NormalizeFromRawData(data, data)
				return err
			},
		)
	}
}

// BenchmarkLargeFileProcessing 測試大文件處理性能
func (cb *CSVBenchmarks) BenchmarkLargeFileProcessing() {
	testCases := []struct {
		name      string
		rows      int
		cols      int
		chunkSize int
	}{
		{"大文件流式_1萬行", 10000, 50, 1000},
		{"大文件流式_5萬行", 50000, 100, 5000},
		{"大文件流式_10萬行", 100000, 200, 10000},
	}

	csvHandler := io.NewCSVHandler(cb.config)

	for _, tc := range testCases {
		// 生成測試文件
		filePath, fileSize, err := cb.generateTestCSV(
			fmt.Sprintf("large_%s.csv", tc.name), tc.rows, tc.cols)
		if err != nil {
			cb.benchmarker.logger.Error("生成測試文件失敗", err)
			continue
		}

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("大文件處理_%s", tc.name),
			fileSize,
			func() error {
				_, err := csvHandler.ProcessLargeFile(filePath, 100, func(processed, total int64, percentage float64) {
					// 進度回調，這裡不做任何操作以專注於性能測試
				})
				return err
			},
		)
	}
}

// BenchmarkConcurrentProcessing 測試並發處理性能
func (cb *CSVBenchmarks) BenchmarkConcurrentProcessing() {
	testCases := []struct {
		name      string
		fileCount int
		rows      int
		cols      int
	}{
		{"並發_5文件", 5, 1000, 20},
		{"並發_10文件", 10, 2000, 30},
		{"並發_20文件", 20, 1500, 25},
	}

	csvHandler := io.NewCSVHandler(cb.config)

	for _, tc := range testCases {
		// 生成多個測試文件
		files := make([]string, tc.fileCount)
		totalSize := int64(0)

		for i := 0; i < tc.fileCount; i++ {
			filePath, fileSize, err := cb.generateTestCSV(
				fmt.Sprintf("concurrent_%s_%d.csv", tc.name, i), tc.rows, tc.cols)
			if err != nil {
				cb.benchmarker.logger.Error("生成測試文件失敗", err)
				continue
			}
			files[i] = filePath
			totalSize += fileSize
		}

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("並發處理_%s", tc.name),
			totalSize,
			func() error {
				// 模擬並發處理多個文件
				results := make(chan error, tc.fileCount)

				for _, file := range files {
					go func(f string) {
						data, err := csvHandler.ReadCSV(f)
						if err != nil {
							results <- err
							return
						}
						calc := calculator.NewMaxMeanCalculator(cb.config.ScalingFactor)
						_, err = calc.CalculateFromRawDataWithRange(data, 100, 0.0, float64(tc.rows))
						results <- err
					}(file)
				}

				// 等待所有處理完成
				for i := 0; i < tc.fileCount; i++ {
					if err := <-results; err != nil {
						return err
					}
				}

				return nil
			},
		)
	}
}

// BenchmarkMemoryUsage 測試記憶體使用性能
func (cb *CSVBenchmarks) BenchmarkMemoryUsage() {
	testCases := []struct {
		name string
		rows int
		cols int
	}{
		{"記憶體測試_1萬行", 10000, 50},
		{"記憶體測試_5萬行", 50000, 100},
		{"記憶體測試_10萬行", 100000, 200},
	}

	csvHandler := io.NewCSVHandler(cb.config)

	for _, tc := range testCases {
		// 生成測試文件
		filePath, fileSize, err := cb.generateTestCSV(
			fmt.Sprintf("memory_%s.csv", tc.name), tc.rows, tc.cols)
		if err != nil {
			cb.benchmarker.logger.Error("生成測試文件失敗", err)
			continue
		}

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("記憶體使用_%s", tc.name),
			fileSize,
			func() error {
				data, err := csvHandler.ReadCSV(filePath)
				if err != nil {
					return err
				}

				// 執行多種操作以測試記憶體使用
				calc := calculator.NewMaxMeanCalculator(cb.config.ScalingFactor)
				_, err = calc.CalculateFromRawDataWithRange(data, 100, 0.0, float64(tc.rows))
				if err != nil {
					return err
				}

				normalizer := calculator.NewNormalizer(cb.config.ScalingFactor)
				_, err = normalizer.NormalizeFromRawData(data, data)
				return err
			},
		)
	}
}

// RunAllBenchmarks 執行所有 CSV 相關的性能測試
func (cb *CSVBenchmarks) RunAllBenchmarks() *BenchmarkResult {
	cb.benchmarker.logger.Info("開始執行 CSV 性能基準測試套件")

	// 執行各種性能測試
	cb.BenchmarkCSVReading()
	cb.BenchmarkMaxMeanCalculation()
	cb.BenchmarkNormalization()
	cb.BenchmarkLargeFileProcessing()
	cb.BenchmarkConcurrentProcessing()
	cb.BenchmarkMemoryUsage()

	// 生成報告
	report := cb.benchmarker.GenerateReport("CSV處理性能測試")

	cb.benchmarker.logger.Info("CSV 性能基準測試完成", map[string]interface{}{
		"total_tests":  report.Summary.TotalTests,
		"passed_tests": report.Summary.PassedTests,
		"failed_tests": report.Summary.FailedTests,
	})

	return report
}

// Cleanup 清理臨時文件
func (cb *CSVBenchmarks) Cleanup() error {
	err := os.RemoveAll(cb.tempDir)
	if err != nil {
		cb.benchmarker.logger.Error("清理臨時文件失敗", err)
		return err
	}

	cb.benchmarker.logger.Info("臨時文件已清理", map[string]interface{}{"temp_dir": cb.tempDir})
	return nil
}

// GetBenchmarker 獲取基準測試器
func (cb *CSVBenchmarks) GetBenchmarker() *Benchmarker {
	return cb.benchmarker
}
