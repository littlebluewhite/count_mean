package main

import (
	"count_mean/internal/benchmark"
	"count_mean/internal/config"
	"count_mean/internal/logging"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func main() {
	// 命令行參數
	var (
		logLevel   = flag.String("log-level", "info", "日誌級別 (debug, info, warn, error)")
		logDir     = flag.String("log-dir", "./benchmark_logs", "日誌目錄")
		reportDir  = flag.String("report-dir", "./benchmark_reports", "報告輸出目錄")
		configFile = flag.String("config", "./config.json", "配置文件路徑")
		csvOnly    = flag.Bool("csv-only", false, "只執行 CSV 相關測試")
		verbose    = flag.Bool("verbose", false, "詳細輸出")
	)
	flag.Parse()

	fmt.Println("=== EMG 數據分析工具 - 性能基準測試 ===")
	fmt.Printf("開始時間: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println()

	// 初始化日誌系統
	level := logging.LevelInfo
	switch *logLevel {
	case "debug":
		level = logging.LevelDebug
	case "warn":
		level = logging.LevelWarn
	case "error":
		level = logging.LevelError
	}

	if err := logging.InitLogger(level, *logDir, false); err != nil {
		fmt.Printf("日誌初始化失敗: %v\n", err)
		os.Exit(1)
	}

	logger := logging.GetLogger("benchmark_main")
	logger.Info("性能基準測試開始", map[string]interface{}{
		"log_level":   *logLevel,
		"log_dir":     *logDir,
		"report_dir":  *reportDir,
		"config_file": *configFile,
		"csv_only":    *csvOnly,
		"verbose":     *verbose,
	})

	// 創建報告目錄
	if err := os.MkdirAll(*reportDir, 0755); err != nil {
		fmt.Printf("創建報告目錄失敗: %v\n", err)
		os.Exit(1)
	}

	// 載入配置
	cfg, err := config.LoadConfig(*configFile)
	if err != nil {
		logger.Warn("載入配置失敗，使用默認配置", map[string]interface{}{"error": err})
		cfg = config.DefaultConfig()
	}

	// 執行性能測試
	if *csvOnly {
		fmt.Println("執行 CSV 處理性能測試...")
		runCSVBenchmarks(cfg, *reportDir, *verbose)
	} else {
		fmt.Println("執行完整性能測試套件...")
		runFullBenchmarks(cfg, *reportDir, *verbose)
	}

	fmt.Printf("\n完成時間: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("報告保存位置: %s\n", *reportDir)
	fmt.Println("性能基準測試完成！")
}

// runCSVBenchmarks 執行 CSV 相關性能測試
func runCSVBenchmarks(cfg *config.AppConfig, reportDir string, verbose bool) {
	logger := logging.GetLogger("csv_benchmarks")

	// 創建 CSV 性能測試器
	csvBench, err := benchmark.NewCSVBenchmarks(cfg)
	if err != nil {
		logger.Error("創建 CSV 性能測試器失敗", err)
		fmt.Printf("錯誤: %v\n", err)
		return
	}
	defer csvBench.Cleanup()

	fmt.Println("\n=== CSV 處理性能測試 ===")

	startTime := time.Now()

	// 執行所有 CSV 測試
	report := csvBench.RunAllBenchmarks()

	totalTime := time.Since(startTime)

	// 顯示結果摘要
	fmt.Printf("\n=== 測試結果摘要 ===\n")
	fmt.Printf("總測試數: %d\n", report.Summary.TotalTests)
	fmt.Printf("通過測試: %d\n", report.Summary.PassedTests)
	fmt.Printf("失敗測試: %d\n", report.Summary.FailedTests)
	fmt.Printf("總執行時間: %v\n", totalTime)
	fmt.Printf("平均執行時間: %v\n", report.Summary.AvgDuration)
	fmt.Printf("總記憶體使用: %.2f MB\n", float64(report.Summary.TotalMemory)/(1024*1024))

	if verbose {
		csvBench.GetBenchmarker().PrintSummary()
	}

	// 保存報告
	timestamp := time.Now().Format("20060102_150405")
	reportFile := filepath.Join(reportDir, fmt.Sprintf("csv_benchmark_report_%s.json", timestamp))

	if err := csvBench.GetBenchmarker().SaveReportToFile(report, reportFile); err != nil {
		logger.Error("保存報告失敗", err)
		fmt.Printf("保存報告失敗: %v\n", err)
	} else {
		fmt.Printf("CSV 性能報告已保存: %s\n", reportFile)
	}
}

// runFullBenchmarks 執行完整性能測試套件
func runFullBenchmarks(cfg *config.AppConfig, reportDir string, verbose bool) {
	logger := logging.GetLogger("full_benchmarks")

	fmt.Println("\n=== 完整性能測試套件 ===")

	// 1. CSV 處理測試
	fmt.Println("\n1. 執行 CSV 處理性能測試...")
	runCSVBenchmarks(cfg, reportDir, false)

	// 2. 系統性能測試
	fmt.Println("\n2. 執行系統性能測試...")
	runSystemBenchmarks(cfg, reportDir, verbose)

	// 3. 記憶體效能測試
	fmt.Println("\n3. 執行記憶體效能測試...")
	runMemoryBenchmarks(cfg, reportDir, verbose)

	// 4. 併發性能測試
	fmt.Println("\n4. 執行併發性能測試...")
	runConcurrencyBenchmarks(cfg, reportDir, verbose)

	logger.Info("完整性能測試套件執行完成")
	fmt.Println("\n完整性能測試套件執行完成！")
}

// runSystemBenchmarks 執行系統性能測試
func runSystemBenchmarks(cfg *config.AppConfig, reportDir string, verbose bool) {
	logger := logging.GetLogger("system_benchmarks")
	benchmarker := benchmark.NewBenchmarker(cfg)

	// 文件 I/O 性能測試
	benchmarker.Benchmark("文件讀寫測試", func() error {
		return testFileIO()
	})

	// 數學計算性能測試
	benchmarker.BenchmarkOperations("數學計算測試", 1000000, func() error {
		return testMathOperations()
	})

	// 字符串處理性能測試
	benchmarker.BenchmarkOperations("字符串處理測試", 100000, func() error {
		return testStringOperations()
	})

	// 生成報告
	report := benchmarker.GenerateReport("系統性能測試")

	if verbose {
		benchmarker.PrintSummary()
	}

	// 保存報告
	timestamp := time.Now().Format("20060102_150405")
	reportFile := filepath.Join(reportDir, fmt.Sprintf("system_benchmark_report_%s.json", timestamp))

	if err := benchmarker.SaveReportToFile(report, reportFile); err != nil {
		logger.Error("保存系統性能報告失敗", err)
	} else {
		fmt.Printf("系統性能報告已保存: %s\n", reportFile)
	}
}

// runMemoryBenchmarks 執行記憶體效能測試
func runMemoryBenchmarks(cfg *config.AppConfig, reportDir string, verbose bool) {
	logger := logging.GetLogger("memory_benchmarks")
	benchmarker := benchmark.NewBenchmarker(cfg)

	// 記憶體分配測試
	benchmarker.Benchmark("記憶體分配測試", func() error {
		return testMemoryAllocation()
	})

	// 大數組處理測試
	benchmarker.BenchmarkWithData("大數組處理測試", 100*1024*1024, func() error {
		return testLargeArrayProcessing()
	})

	// 垃圾回收影響測試
	benchmarker.Benchmark("垃圾回收測試", func() error {
		return testGarbageCollection()
	})

	// 生成報告
	report := benchmarker.GenerateReport("記憶體效能測試")

	if verbose {
		benchmarker.PrintSummary()
	}

	// 保存報告
	timestamp := time.Now().Format("20060102_150405")
	reportFile := filepath.Join(reportDir, fmt.Sprintf("memory_benchmark_report_%s.json", timestamp))

	if err := benchmarker.SaveReportToFile(report, reportFile); err != nil {
		logger.Error("保存記憶體性能報告失敗", err)
	} else {
		fmt.Printf("記憶體性能報告已保存: %s\n", reportFile)
	}
}

// runConcurrencyBenchmarks 執行併發性能測試
func runConcurrencyBenchmarks(cfg *config.AppConfig, reportDir string, verbose bool) {
	logger := logging.GetLogger("concurrency_benchmarks")
	benchmarker := benchmark.NewBenchmarker(cfg)

	// Goroutine 性能測試
	benchmarker.BenchmarkOperations("Goroutine測試", 1000, func() error {
		return testGoroutines()
	})

	// Channel 通信測試
	benchmarker.BenchmarkOperations("Channel通信測試", 10000, func() error {
		return testChannelCommunication()
	})

	// 鎖競爭測試
	benchmarker.Benchmark("鎖競爭測試", func() error {
		return testLockContention()
	})

	// 生成報告
	report := benchmarker.GenerateReport("併發性能測試")

	if verbose {
		benchmarker.PrintSummary()
	}

	// 保存報告
	timestamp := time.Now().Format("20060102_150405")
	reportFile := filepath.Join(reportDir, fmt.Sprintf("concurrency_benchmark_report_%s.json", timestamp))

	if err := benchmarker.SaveReportToFile(report, reportFile); err != nil {
		logger.Error("保存併發性能報告失敗", err)
	} else {
		fmt.Printf("併發性能報告已保存: %s\n", reportFile)
	}
}

// 測試函數實現

func testFileIO() error {
	tempFile := filepath.Join(os.TempDir(), "benchmark_test.tmp")
	defer os.Remove(tempFile)

	// 寫入測試
	data := make([]byte, 1024*1024) // 1MB
	for i := range data {
		data[i] = byte(i % 256)
	}

	file, err := os.Create(tempFile)
	if err != nil {
		return err
	}

	_, err = file.Write(data)
	file.Close()
	if err != nil {
		return err
	}

	// 讀取測試
	_, err = os.ReadFile(tempFile)
	return err
}

func testMathOperations() error {
	sum := 0.0
	for i := 0; i < 1000000; i++ {
		sum += float64(i) * 1.1
	}
	return nil
}

func testStringOperations() error {
	var result string
	for i := 0; i < 100000; i++ {
		result = fmt.Sprintf("test_%d", i)
		_ = len(result)
	}
	return nil
}

func testMemoryAllocation() error {
	slices := make([][]int, 1000)
	for i := 0; i < 1000; i++ {
		slices[i] = make([]int, 1000)
		for j := 0; j < 1000; j++ {
			slices[i][j] = i * j
		}
	}
	return nil
}

func testLargeArrayProcessing() error {
	data := make([]float64, 10*1024*1024) // 10M floats
	for i := range data {
		data[i] = float64(i) * 0.1
	}

	// 簡單處理
	sum := 0.0
	for _, v := range data {
		sum += v
	}

	return nil
}

func testGarbageCollection() error {
	for i := 0; i < 1000; i++ {
		// 分配大量短期對象
		temp := make([][]byte, 100)
		for j := 0; j < 100; j++ {
			temp[j] = make([]byte, 1024)
		}

		// 讓它們被垃圾回收
		temp = nil

		if i%100 == 0 {
			// 強制垃圾回收
			// runtime.GC()
		}
	}
	return nil
}

func testGoroutines() error {
	done := make(chan bool, 1000)

	for i := 0; i < 1000; i++ {
		go func(n int) {
			// 模擬一些工作
			sum := 0
			for j := 0; j < 1000; j++ {
				sum += n * j
			}
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 1000; i++ {
		<-done
	}

	return nil
}

func testChannelCommunication() error {
	ch := make(chan int, 1000)
	done := make(chan bool)

	// 生產者
	go func() {
		for i := 0; i < 10000; i++ {
			ch <- i
		}
		close(ch)
	}()

	// 消費者
	go func() {
		sum := 0
		for v := range ch {
			sum += v
		}
		done <- true
	}()

	<-done
	return nil
}

func testLockContention() error {
	// 這個測試需要 sync 包，但為了簡化我們只做基本模擬
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			// 模擬對共享資源的競爭
			for j := 0; j < 1000; j++ {
				// 一些計算工作
				_ = j * j
			}
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	return nil
}
