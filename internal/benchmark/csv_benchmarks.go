package benchmark

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
)

// CSV benchmark constants.
const (
	randomValueRange = 500  // Range for random value generation (-500 to 500)
	randomValueScale = 1000 // Scale factor for random values
)

// CSVBenchmarks CSV 相關性能測試.
type CSVBenchmarks struct {
	benchmarker *Benchmarker
	config      *config.AppConfig
	tempDir     string
}

// NewCSVBenchmarks 創建 CSV 性能測試器.
//
// 改用 os.MkdirTemp 取代手動拼 `tempdir + unix-second`。
// 舊 path 有兩個問題:
//  1. 同一秒內兩次呼叫會撞 dir name → MkdirAll 接受已存在 dir 卻不告知,後續測試
//     可能誤跑在前一輪未清的 fixture 上。
//  2. unix-second 不夠唯一,連續 CI run 偶發產生 orphan dir + panic。
//
// MkdirTemp 內部用 random suffix,保證每次呼叫都拿到全新 dir,collision-free。
// 同時自動繼承 system temp 的 0700 權限,不必再手動 chmod。
func NewCSVBenchmarks(cfg *config.AppConfig) (*CSVBenchmarks, error) {
	tempDir, err := os.MkdirTemp("", "emg_benchmark_*")
	if err != nil {
		return nil, fmt.Errorf("無法創建臨時目錄: %w", err)
	}

	return &CSVBenchmarks{
		benchmarker: NewBenchmarker(cfg),
		config:      cfg,
		tempDir:     tempDir,
	}, nil
}

// generateTestCSV 生成測試用的 CSV 文件.
func (cb *CSVBenchmarks) generateTestCSV(filename string, rows, cols int) (string, int64, error) {
	filePath := filepath.Join(cb.tempDir, filename)

	//nolint:gosec // G304: filepath is constructed from tempDir for benchmark purposes
	file, err := os.Create(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("failed to create file: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	header := make([]string, cols)
	for i := 0; i < cols; i++ {
		header[i] = fmt.Sprintf("column_%d", i+1)
	}

	if _, writeErr := file.WriteString(strings.Join(header, ",") + "\n"); writeErr != nil {
		return "", 0, fmt.Errorf("failed to write header: %w", writeErr)
	}

	for i := 0; i < rows; i++ {
		row := make([]string, cols)

		for j := 0; j < cols; j++ {
			value := rand.Float64()*randomValueScale - randomValueRange //nolint:gosec // G404: benchmark synthetic data, not cryptographic
			row[j] = fmt.Sprintf("%.6f", value)
		}

		if _, writeErr := file.WriteString(strings.Join(row, ",") + "\n"); writeErr != nil {
			return "", 0, fmt.Errorf("failed to write row: %w", writeErr)
		}
	}

	info, err := file.Stat()
	if err != nil {
		return filePath, 0, fmt.Errorf("failed to stat file: %w", err)
	}

	return filePath, info.Size(), nil
}

// BenchmarkCSVReading 測試 CSV 讀取性能.
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
				_, readErr := csvHandler.ReadCSV(filePath)
				return readErr //nolint:wrapcheck // benchmark errors don't need wrapping
			},
		)
	}
}

// BenchmarkMaxMeanCalculation 測試最大均值計算性能.
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
		filePath, fileSize, err := cb.generateTestCSV(
			fmt.Sprintf("maxmean_%s.csv", tc.name), tc.rows, tc.cols)
		if err != nil {
			cb.benchmarker.logger.Error("生成測試文件失敗", err)
			continue
		}

		data, err := csvHandler.ReadCSV(filePath)
		if err != nil {
			cb.benchmarker.logger.Error("讀取測試數據失敗", err)
			continue
		}

		windowSize := tc.windowSize
		rowCount := tc.rows

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("最大均值計算_%s", tc.name),
			fileSize,
			func() error {
				calc := calculator.NewMaxMeanCalculator(cb.config.ScalingFactor)
				_, calcErr := calc.CalculateFromRawDataWithRange(context.Background(), data, windowSize, 0.0, float64(rowCount))

				return calcErr //nolint:wrapcheck // benchmark errors don't need wrapping
			},
		)
	}
}

// BenchmarkNormalization 測試數據正規化性能.
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
		filePath, fileSize, err := cb.generateTestCSV(
			fmt.Sprintf("norm_%s.csv", tc.name), tc.rows, tc.cols)
		if err != nil {
			cb.benchmarker.logger.Error("生成測試文件失敗", err)
			continue
		}

		data, err := csvHandler.ReadCSV(filePath)
		if err != nil {
			cb.benchmarker.logger.Error("讀取測試數據失敗", err)
			continue
		}

		// reference 必須單行（MVC/MAX 模式）。取 data[0]=header + data[1]=首筆樣本。
		reference := singleRowReference(data)

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("數據正規化_%s", tc.name),
			fileSize,
			func() error {
				normalizer := calculator.NewNormalizer(cb.config.ScalingFactor)
				_, normErr := normalizer.NormalizeFromRawData(data, reference)

				return normErr //nolint:wrapcheck // benchmark errors don't need wrapping
			},
		)
	}
}

// singleRowReference 從 CSV records 提取「標頭 + 首行樣本」作為單行 MVC reference，
// 用於 benchmark 場景。若 records 不足兩行則回傳 nil（caller 應自行處理 NormalizeFromRawData
// 的 ErrInvalidReferenceData）。
func singleRowReference(records [][]string) [][]string {
	if len(records) < 2 {
		return nil
	}

	return [][]string{records[0], records[1]}
}

// BenchmarkLargeFileProcessing 測試大文件處理性能.
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
				_, procErr := csvHandler.ProcessLargeFile(filePath, 100, func(_, _ int64, _ float64) {})

				return procErr //nolint:wrapcheck // benchmark errors don't need wrapping
			},
		)
	}
}

// BenchmarkConcurrentProcessing 測試並發處理性能.
//
//nolint:gocognit // complexity is acceptable for benchmark setup
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

		fileCount := tc.fileCount
		rowCount := tc.rows

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("並發處理_%s", tc.name),
			totalSize,
			func() error {
				results := make(chan error, fileCount)

				for _, file := range files {
					go func(f string) {
						data, readErr := csvHandler.ReadCSV(f)
						if readErr != nil {
							results <- readErr
							return
						}

						calc := calculator.NewMaxMeanCalculator(cb.config.ScalingFactor)
						_, calcErr := calc.CalculateFromRawDataWithRange(context.Background(), data, 100, 0.0, float64(rowCount))
						results <- calcErr
					}(file)
				}

				for i := 0; i < fileCount; i++ {
					if err := <-results; err != nil {
						return err
					}
				}

				return nil
			},
		)
	}
}

// BenchmarkMemoryUsage 測試記憶體使用性能.
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
		filePath, fileSize, err := cb.generateTestCSV(
			fmt.Sprintf("memory_%s.csv", tc.name), tc.rows, tc.cols)
		if err != nil {
			cb.benchmarker.logger.Error("生成測試文件失敗", err)
			continue
		}

		rowCount := tc.rows

		cb.benchmarker.BenchmarkWithData(
			fmt.Sprintf("記憶體使用_%s", tc.name),
			fileSize,
			func() error {
				data, readErr := csvHandler.ReadCSV(filePath)
				if readErr != nil {
					return readErr //nolint:wrapcheck // benchmark errors don't need wrapping
				}

				calc := calculator.NewMaxMeanCalculator(cb.config.ScalingFactor)

				_, calcErr := calc.CalculateFromRawDataWithRange(context.Background(), data, 100, 0.0, float64(rowCount))
				if calcErr != nil {
					return calcErr //nolint:wrapcheck // benchmark errors don't need wrapping
				}

				normalizer := calculator.NewNormalizer(cb.config.ScalingFactor)
				_, normErr := normalizer.NormalizeFromRawData(data, singleRowReference(data))

				return normErr //nolint:wrapcheck // benchmark errors don't need wrapping
			},
		)
	}
}

// RunAllBenchmarks 執行所有 CSV 相關的性能測試.
func (cb *CSVBenchmarks) RunAllBenchmarks() *Result {
	cb.benchmarker.logger.Info("開始執行 CSV 性能基準測試套件")

	cb.BenchmarkCSVReading()
	cb.BenchmarkMaxMeanCalculation()
	cb.BenchmarkNormalization()
	cb.BenchmarkLargeFileProcessing()
	cb.BenchmarkConcurrentProcessing()
	cb.BenchmarkMemoryUsage()

	report := cb.benchmarker.GenerateReport("CSV處理性能測試")

	cb.benchmarker.logger.Info("CSV 性能基準測試完成", map[string]interface{}{
		"total_tests":  report.Summary.TotalTests,
		"passed_tests": report.Summary.PassedTests,
		"failed_tests": report.Summary.FailedTests,
	})

	return report
}

// Cleanup 清理臨時文件.
func (cb *CSVBenchmarks) Cleanup() error {
	err := os.RemoveAll(cb.tempDir)
	if err != nil {
		cb.benchmarker.logger.Error("清理臨時文件失敗", err)

		return fmt.Errorf("failed to cleanup temp directory: %w", err)
	}

	cb.benchmarker.logger.Info("臨時文件已清理", map[string]interface{}{"temp_dir": cb.tempDir})

	return nil
}

// GetBenchmarker 獲取基準測試器.
func (cb *CSVBenchmarks) GetBenchmarker() *Benchmarker {
	return cb.benchmarker
}
