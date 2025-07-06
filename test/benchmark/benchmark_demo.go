package main

import (
	"count_mean/internal/benchmark"
	"count_mean/internal/config"
	"count_mean/internal/logging"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// 性能基準測試演示程序
func main() {
	fmt.Println("=== EMG 數據分析工具 - 性能基準測試演示 ===")
	fmt.Printf("開始時間: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	// 初始化日誌系統
	if err := logging.InitLogger(logging.LevelInfo, "./benchmark_logs", false); err != nil {
		fmt.Printf("日誌初始化失敗: %v\n", err)
		os.Exit(1)
	}

	logger := logging.GetLogger("benchmark_demo")
	logger.Info("性能基準測試演示開始")

	// 載入配置
	cfg := config.DefaultConfig()

	// 創建報告目錄
	reportDir := "./benchmark_reports"
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		fmt.Printf("創建報告目錄失敗: %v\n", err)
		os.Exit(1)
	}

	// 演示基本性能測試
	fmt.Println("1. 基本性能測試演示")
	demonstrateBasicBenchmarks(cfg)

	fmt.Println("\n" + strings.Repeat("=", 50))

	// 演示 CSV 性能測試
	fmt.Println("\n2. CSV 處理性能測試演示")
	demonstrateCSVBenchmarks(cfg, reportDir)

	fmt.Println("\n" + strings.Repeat("=", 50))

	// 演示系統資訊
	fmt.Println("\n3. 系統環境資訊")
	demonstrateSystemInfo()

	fmt.Printf("\n完成時間: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("報告保存位置: %s\n", reportDir)
	fmt.Println("\n性能基準測試演示完成！")
	fmt.Println("您可以運行 'go run benchmark_test_main.go' 來執行完整的性能測試套件。")
}

// demonstrateBasicBenchmarks 演示基本性能測試
func demonstrateBasicBenchmarks(cfg *config.AppConfig) {
	benchmarker := benchmark.NewBenchmarker(cfg)

	fmt.Println("正在執行基本性能測試...")

	// 測試 1: 簡單計算
	fmt.Print("  - 數學計算測試... ")
	metrics1 := benchmarker.Benchmark("數學計算", func() error {
		sum := 0.0
		for i := 0; i < 100000; i++ {
			sum += float64(i) * 1.1
		}
		return nil
	})
	fmt.Printf("完成 (%v, 記憶體: %.2f KB)\n",
		metrics1.Duration, float64(metrics1.MemoryUsage)/1024)

	// 測試 2: 字符串操作
	fmt.Print("  - 字符串處理測試... ")
	metrics2 := benchmarker.Benchmark("字符串處理", func() error {
		var result string
		for i := 0; i < 10000; i++ {
			result = fmt.Sprintf("test_string_%d", i)
			_ = len(result)
		}
		return nil
	})
	fmt.Printf("完成 (%v, 記憶體: %.2f KB)\n",
		metrics2.Duration, float64(metrics2.MemoryUsage)/1024)

	// 測試 3: 陣列處理
	fmt.Print("  - 陣列處理測試... ")
	metrics3 := benchmarker.BenchmarkWithData("陣列處理", 1024*1024, func() error {
		data := make([]int, 100000)
		for i := range data {
			data[i] = i * 2
		}

		sum := 0
		for _, v := range data {
			sum += v
		}
		return nil
	})
	fmt.Printf("完成 (%v, 記憶體: %.2f KB, 吞吐量: %.2f MB/s)\n",
		metrics3.Duration, float64(metrics3.MemoryUsage)/1024, metrics3.ThroughputData)

	// 測試 4: 錯誤處理演示
	fmt.Print("  - 錯誤處理測試... ")
	metrics4 := benchmarker.Benchmark("錯誤測試", func() error {
		return fmt.Errorf("這是一個測試錯誤")
	})
	if metrics4.Success {
		fmt.Println("意外成功")
	} else {
		fmt.Printf("如期失敗 (%v, 錯誤: %s)\n", metrics4.Duration, metrics4.Error)
	}

	// 顯示摘要
	fmt.Println("\n基本測試摘要:")
	results := benchmarker.GetResults()
	for _, result := range results {
		status := "✓"
		if !result.Success {
			status = "✗"
		}
		fmt.Printf("  %s %s: %v\n", status, result.Name, result.Duration)
	}
}

// demonstrateCSVBenchmarks 演示 CSV 性能測試
func demonstrateCSVBenchmarks(cfg *config.AppConfig, reportDir string) {
	fmt.Println("正在執行 CSV 處理性能測試...")

	csvBench, err := benchmark.NewCSVBenchmarks(cfg)
	if err != nil {
		fmt.Printf("創建 CSV 測試器失敗: %v\n", err)
		return
	}
	defer csvBench.Cleanup()

	// 模擬測試數據生成（簡化版本）
	fmt.Print("  - 模擬測試數據生成... ")
	tempFile := filepath.Join(os.TempDir(), "demo_benchmark.csv")
	file, err := os.Create(tempFile)
	if err != nil {
		fmt.Printf("失敗: %v\n", err)
		return
	}
	defer file.Close()

	// 寫入簡單的測試數據
	file.WriteString("time,channel1,channel2,channel3\n")
	for i := 0; i < 100; i++ {
		file.WriteString(fmt.Sprintf("%.2f,%.2f,%.2f,%.2f\n",
			float64(i)*0.01, float64(i)*1.1, float64(i)*1.2, float64(i)*1.3))
	}

	info, _ := file.Stat()
	fileSize := info.Size()
	fmt.Printf("完成 (檔案大小: %.2f KB)\n", float64(fileSize)/1024)

	// 執行一個簡單的性能測試
	benchmarker := csvBench.GetBenchmarker()

	fmt.Print("  - CSV 讀取測試... ")
	metrics := benchmarker.BenchmarkWithData("CSV讀取演示", fileSize, func() error {
		// 模擬 CSV 讀取（這裡簡化實現）
		content, err := os.ReadFile(tempFile)
		if err != nil {
			return err
		}

		// 簡單的行計數
		lines := 0
		for _, b := range content {
			if b == '\n' {
				lines++
			}
		}

		return nil
	})
	fmt.Printf("完成 (%v, 吞吐量: %.2f MB/s)\n",
		metrics.Duration, metrics.ThroughputData)

	// 保存簡單報告
	report := benchmarker.GenerateReport("CSV演示測試")
	timestamp := time.Now().Format("20060102_150405")
	reportFile := filepath.Join(reportDir, fmt.Sprintf("csv_demo_report_%s.json", timestamp))

	if err := benchmarker.SaveReportToFile(report, reportFile); err != nil {
		fmt.Printf("  - 保存報告失敗: %v\n", err)
	} else {
		fmt.Printf("  - 報告已保存: %s\n", reportFile)
	}
}

// demonstrateSystemInfo 演示系統資訊
func demonstrateSystemInfo() {
	// 使用內部函數獲取系統資訊（需要導出或創建公共接口）
	benchmarker := benchmark.NewBenchmarker(config.DefaultConfig())
	report := benchmarker.GenerateReport("系統資訊測試")

	env := report.Environment

	fmt.Printf("作業系統: %s\n", env.OS)
	fmt.Printf("系統架構: %s\n", env.Arch)
	fmt.Printf("CPU 核心數: %d\n", env.CPUs)
	fmt.Printf("Go 版本: %s\n", env.GoVersion)
	fmt.Printf("系統記憶體: %.2f MB\n", float64(env.TotalMemory)/(1024*1024))

	// 顯示當前時間
	fmt.Printf("測試時間: %s\n", report.Timestamp.Format("2006-01-02 15:04:05"))
}

// strings package simulation for simple string operations
var strings = struct {
	Repeat func(s string, count int) string
}{
	Repeat: func(s string, count int) string {
		if count <= 0 {
			return ""
		}
		result := ""
		for i := 0; i < count; i++ {
			result += s
		}
		return result
	},
}
