package integration

import (
	"count_mean/internal/benchmark"
	"count_mean/internal/config"
	"count_mean/internal/logging"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 性能基準測試整合測試
func TestBenchmarkIntegration(t *testing.T) {
	// 初始化日誌
	if err := logging.InitLogger(logging.LevelInfo, "./benchmark_test_logs", false); err != nil {
		t.Fatalf("日誌初始化失敗: %v", err)
	}

	logger := logging.GetLogger("benchmark_integration_test")
	logger.Info("性能基準測試整合測試開始")

	// 創建測試配置
	cfg := config.DefaultConfig()

	// 確保測試目錄存在
	testDir := "./benchmark_test_reports"
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("無法創建測試目錄: %v", err)
	}
	defer os.RemoveAll(testDir)
	defer os.RemoveAll("./benchmark_test_logs")

	t.Run("基本性能測試", func(t *testing.T) {
		benchmarker := benchmark.NewBenchmarker(cfg)

		// 測試簡單函數
		metrics := benchmarker.Benchmark("測試計算", func() error {
			sum := 0
			for i := 0; i < 10000; i++ {
				sum += i
			}
			return nil
		})

		if !metrics.Success {
			t.Errorf("基本測試失敗: %s", metrics.Error)
		}

		if metrics.Duration <= 0 {
			t.Error("執行時間應該大於0")
		}

		logger.Info("基本性能測試完成", map[string]interface{}{
			"duration": metrics.Duration,
			"memory":   metrics.MemoryUsage,
		})
	})

	t.Run("數據吞吐量測試", func(t *testing.T) {
		benchmarker := benchmark.NewBenchmarker(cfg)

		dataSize := int64(1024 * 1024) // 1MB
		metrics := benchmarker.BenchmarkWithData("數據處理", dataSize, func() error {
			// 模擬數據處理
			data := make([]byte, dataSize)
			for i := range data {
				data[i] = byte(i % 256)
			}

			// 簡單處理
			sum := 0
			for _, b := range data {
				sum += int(b)
			}
			return nil
		})

		if !metrics.Success {
			t.Errorf("數據吞吐量測試失敗: %s", metrics.Error)
		}

		if metrics.ThroughputData <= 0 {
			t.Error("數據吞吐量應該大於0")
		}

		logger.Info("數據吞吐量測試完成", map[string]interface{}{
			"throughput": metrics.ThroughputData,
		})
	})

	t.Run("操作吞吐量測試", func(t *testing.T) {
		benchmarker := benchmark.NewBenchmarker(cfg)

		operationCount := 100000
		metrics := benchmarker.BenchmarkOperations("操作測試", operationCount, func() error {
			sum := 0
			for i := 0; i < operationCount; i++ {
				sum += i * i
			}
			return nil
		})

		if !metrics.Success {
			t.Errorf("操作吞吐量測試失敗: %s", metrics.Error)
		}

		if metrics.ThroughputOps <= 0 {
			t.Error("操作吞吐量應該大於0")
		}

		logger.Info("操作吞吐量測試完成", map[string]interface{}{
			"throughput_ops": metrics.ThroughputOps,
		})
	})

	t.Run("測試報告生成", func(t *testing.T) {
		benchmarker := benchmark.NewBenchmarker(cfg)

		// 執行幾個測試
		benchmarker.Benchmark("測試1", func() error {
			time.Sleep(1 * time.Millisecond)
			return nil
		})

		benchmarker.Benchmark("測試2", func() error {
			time.Sleep(2 * time.Millisecond)
			return nil
		})

		benchmarker.Benchmark("失敗測試", func() error {
			return fmt.Errorf("模擬錯誤")
		})

		// 生成報告
		report := benchmarker.GenerateReport("整合測試")

		if report.Summary.TotalTests != 3 {
			t.Errorf("期望總測試數 3，實際: %d", report.Summary.TotalTests)
		}

		if report.Summary.PassedTests != 2 {
			t.Errorf("期望通過測試數 2，實際: %d", report.Summary.PassedTests)
		}

		if report.Summary.FailedTests != 1 {
			t.Errorf("期望失敗測試數 1，實際: %d", report.Summary.FailedTests)
		}

		// 保存報告
		reportFile := filepath.Join(testDir, "integration_test_report.json")
		err := benchmarker.SaveReportToFile(report, reportFile)
		if err != nil {
			t.Errorf("保存報告失敗: %v", err)
		}

		// 檢查文件是否存在
		if _, err := os.Stat(reportFile); os.IsNotExist(err) {
			t.Error("報告文件未創建")
		}

		logger.Info("測試報告生成完成", map[string]interface{}{
			"report_file": reportFile,
		})
	})

	t.Run("CSV性能測試器創建", func(t *testing.T) {
		csvBench, err := benchmark.NewCSVBenchmarks(cfg)
		if err != nil {
			t.Errorf("創建CSV性能測試器失敗: %v", err)
			return
		}
		defer csvBench.Cleanup()

		// 測試模擬文件生成
		tempFile := filepath.Join(os.TempDir(), "integration_test.csv")
		file, err := os.Create(tempFile)
		if err != nil {
			t.Errorf("創建測試文件失敗: %v", err)
			return
		}
		defer os.Remove(tempFile)

		// 寫入測試數據
		file.WriteString("time,ch1,ch2\n")
		for i := 0; i < 100; i++ {
			file.WriteString(fmt.Sprintf("%.2f,%.2f,%.2f\n",
				float64(i)*0.01, float64(i)*1.1, float64(i)*1.2))
		}
		file.Close()

		info, err := os.Stat(tempFile)
		if err != nil {
			t.Errorf("獲取文件資訊失敗: %v", err)
			return
		}

		if info.Size() <= 0 {
			t.Error("文件大小應該大於0")
		}

		logger.Info("CSV性能測試器測試完成", map[string]interface{}{
			"file_size": info.Size(),
		})
	})

	t.Run("系統資訊測試", func(t *testing.T) {
		benchmarker := benchmark.NewBenchmarker(cfg)
		report := benchmarker.GenerateReport("系統資訊測試")

		env := report.Environment

		if env.OS == "" {
			t.Error("作業系統資訊不應為空")
		}

		if env.Arch == "" {
			t.Error("系統架構資訊不應為空")
		}

		if env.CPUs <= 0 {
			t.Error("CPU 核心數應該大於0")
		}

		if env.GoVersion == "" {
			t.Error("Go 版本資訊不應為空")
		}

		logger.Info("系統資訊測試完成", map[string]interface{}{
			"os":           env.OS,
			"arch":         env.Arch,
			"cpus":         env.CPUs,
			"go_version":   env.GoVersion,
			"total_memory": env.TotalMemory,
		})
	})

	logger.Info("性能基準測試整合測試完成")
}


