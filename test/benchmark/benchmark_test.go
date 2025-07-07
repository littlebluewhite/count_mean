package benchmark_test

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

func TestMain(m *testing.M) {
	// 初始化日誌系統
	if err := logging.InitLogger(logging.LevelInfo, "./test_logs", false); err != nil {
		panic("日誌初始化失敗: " + err.Error())
	}

	// 運行測試
	code := m.Run()

	// 清理
	os.RemoveAll("./test_logs")

	os.Exit(code)
}

func TestBenchmarker(t *testing.T) {
	cfg := config.DefaultConfig()
	benchmarker := benchmark.NewBenchmarker(cfg)

	t.Run("基本性能測試", func(t *testing.T) {
		metrics := benchmarker.Benchmark("測試函數", func() error {
			// 模擬一些工作
			time.Sleep(10 * time.Millisecond)
			sum := 0
			for i := 0; i < 1000; i++ {
				sum += i
			}
			return nil
		})

		if !metrics.Success {
			t.Errorf("測試應該成功，但失敗了: %s", metrics.Error)
		}

		if metrics.Duration < 10*time.Millisecond {
			t.Errorf("執行時間應該至少10ms，實際: %v", metrics.Duration)
		}

		if metrics.MemoryUsage == 0 {
			t.Log("記憶體使用量為0，這可能是正常的")
		}
	})

	t.Run("錯誤處理測試", func(t *testing.T) {
		metrics := benchmarker.Benchmark("錯誤函數", func() error {
			return &TestError{Message: "測試錯誤"}
		})

		if metrics.Success {
			t.Error("測試應該失敗，但成功了")
		}

		if metrics.Error != "測試錯誤" {
			t.Errorf("期望錯誤訊息 '測試錯誤'，實際: %s", metrics.Error)
		}
	})

	t.Run("數據吞吐量測試", func(t *testing.T) {
		dataSize := int64(1024 * 1024) // 1MB
		metrics := benchmarker.BenchmarkWithData("數據處理", dataSize, func() error {
			// 模擬數據處理
			time.Sleep(100 * time.Millisecond)
			return nil
		})

		if !metrics.Success {
			t.Errorf("數據處理測試失敗: %s", metrics.Error)
		}

		if metrics.ThroughputData <= 0 {
			t.Error("數據吞吐量應該大於0")
		}

		expectedThroughput := float64(dataSize) / (1024 * 1024) / 0.1 // 約10 MB/s
		if metrics.ThroughputData < expectedThroughput*0.5 ||
			metrics.ThroughputData > expectedThroughput*2 {
			t.Logf("吞吐量可能不在預期範圍內: %.2f MB/s", metrics.ThroughputData)
		}
	})

	t.Run("操作吞吐量測試", func(t *testing.T) {
		operationCount := 1000
		metrics := benchmarker.BenchmarkOperations("操作處理", operationCount, func() error {
			// 模擬1000次操作
			for i := 0; i < operationCount; i++ {
				_ = i * i
			}
			return nil
		})

		if !metrics.Success {
			t.Errorf("操作處理測試失敗: %s", metrics.Error)
		}

		if metrics.ThroughputOps <= 0 {
			t.Error("操作吞吐量應該大於0")
		}

		if metrics.ThroughputOps < 1000 { // 至少1000 ops/s
			t.Logf("操作吞吐量較低: %.0f ops/s", metrics.ThroughputOps)
		}
	})
}

func TestBenchmarkReport(t *testing.T) {
	cfg := config.DefaultConfig()
	benchmarker := benchmark.NewBenchmarker(cfg)

	// 添加一些測試結果
	benchmarker.Benchmark("快速測試", func() error {
		time.Sleep(1 * time.Millisecond)
		return nil
	})

	benchmarker.Benchmark("慢速測試", func() error {
		time.Sleep(50 * time.Millisecond)
		return nil
	})

	benchmarker.Benchmark("失敗測試", func() error {
		return &TestError{Message: "模擬失敗"}
	})

	t.Run("生成報告", func(t *testing.T) {
		report := benchmarker.GenerateReport("測試套件")

		if report.TestSuite != "測試套件" {
			t.Errorf("期望測試套件名 '測試套件'，實際: %s", report.TestSuite)
		}

		if report.Summary.TotalTests != 3 {
			t.Errorf("期望總測試數 3，實際: %d", report.Summary.TotalTests)
		}

		if report.Summary.PassedTests != 2 {
			t.Errorf("期望通過測試數 2，實際: %d", report.Summary.PassedTests)
		}

		if report.Summary.FailedTests != 1 {
			t.Errorf("期望失敗測試數 1，實際: %d", report.Summary.FailedTests)
		}

		if report.Summary.MaxDuration <= report.Summary.MinDuration {
			t.Error("最大執行時間應該大於最小執行時間")
		}
	})

	t.Run("保存報告", func(t *testing.T) {
		report := benchmarker.GenerateReport("測試套件")

		tempDir := filepath.Join(os.TempDir(), "benchmark_test")
		os.MkdirAll(tempDir, 0755)
		defer os.RemoveAll(tempDir)

		reportFile := filepath.Join(tempDir, "test_report.json")
		err := benchmarker.SaveReportToFile(report, reportFile)

		if err != nil {
			t.Errorf("保存報告失敗: %v", err)
		}

		// 檢查文件是否存在
		if _, err := os.Stat(reportFile); os.IsNotExist(err) {
			t.Error("報告文件未創建")
		}

		// 檢查文件內容
		content, err := os.ReadFile(reportFile)
		if err != nil {
			t.Errorf("讀取報告文件失敗: %v", err)
		}

		if len(content) == 0 {
			t.Error("報告文件為空")
		}

		// 簡單檢查 JSON 格式
		if !containsString(string(content), "test_suite") ||
			!containsString(string(content), "metrics") ||
			!containsString(string(content), "summary") {
			t.Error("報告文件格式不正確")
		}
	})
}

func TestSystemInfo(t *testing.T) {
	// Create a benchmarker to test system info
	cfg := config.DefaultConfig()
	b := benchmark.NewBenchmarker(cfg)

	// Generate report to get system info
	report := b.GenerateReport("Test")
	sysInfo := report.Environment

	if sysInfo.OS == "" {
		t.Error("作業系統資訊不應為空")
	}

	if sysInfo.Arch == "" {
		t.Error("系統架構資訊不應為空")
	}

	if sysInfo.CPUs <= 0 {
		t.Error("CPU 核心數應該大於0")
	}

	if sysInfo.GoVersion == "" {
		t.Error("Go 版本資訊不應為空")
	}

	if sysInfo.TotalMemory == 0 {
		t.Error("總記憶體應該大於0")
	}
}

func TestBenchmarkReset(t *testing.T) {
	cfg := config.DefaultConfig()
	benchmarker := benchmark.NewBenchmarker(cfg)

	// 添加一些測試結果
	benchmarker.Benchmark("測試1", func() error { return nil })
	benchmarker.Benchmark("測試2", func() error { return nil })

	results := benchmarker.GetResults()
	if len(results) != 2 {
		t.Errorf("期望結果數量 2，實際: %d", len(results))
	}

	// 重置
	benchmarker.Reset()

	results = benchmarker.GetResults()
	if len(results) != 0 {
		t.Errorf("重置後結果數量應為 0，實際: %d", len(results))
	}
}

func TestCSVBenchmarks(t *testing.T) {
	cfg := config.DefaultConfig()
	csvBench, err := benchmark.NewCSVBenchmarks(cfg)
	if err != nil {
		t.Fatalf("創建 CSV 基準測試失敗: %v", err)
	}
	defer csvBench.Cleanup()

	t.Run("CSV 基準測試執行", func(t *testing.T) {
		// 創建一個測試 CSV 文件
		tempDir := filepath.Join(os.TempDir(), "csv_benchmark_test")
		os.MkdirAll(tempDir, 0755)
		defer os.RemoveAll(tempDir)

		testFile := filepath.Join(tempDir, "test.csv")
		file, err := os.Create(testFile)
		if err != nil {
			t.Fatalf("創建測試文件失敗: %v", err)
		}

		// 寫入測試數據
		file.WriteString("time,channel1,channel2,channel3\n")
		for i := 0; i < 100; i++ {
			file.WriteString(fmt.Sprintf("%.2f,%.2f,%.2f,%.2f\n",
				float64(i)*0.01, float64(i)*1.1, float64(i)*1.2, float64(i)*1.3))
		}
		file.Close()

		// 獲取文件大小
		info, err := os.Stat(testFile)
		if err != nil {
			t.Fatalf("獲取文件信息失敗: %v", err)
		}

		if info.Size() <= 0 {
			t.Error("文件大小應該大於0")
		}

		// 運行基準測試
		report := csvBench.RunAllBenchmarks()
		if report.Summary.TotalTests <= 0 {
			t.Error("應該執行至少一個測試")
		}
	})

	t.Run("CSV 報告生成", func(t *testing.T) {
		// 獲取基準測試器並生成報告
		benchmarker := csvBench.GetBenchmarker()
		report := benchmarker.GenerateReport("CSV測試")

		if report.TestSuite != "CSV測試" {
			t.Errorf("期望測試套件名稱 'CSV測試'，實際: %s", report.TestSuite)
		}

		// 確保有系統信息
		if report.Environment.OS == "" {
			t.Error("缺少系統信息")
		}
	})
}

// TestError 測試錯誤類型
type TestError struct {
	Message string
}

func (e *TestError) Error() string {
	return e.Message
}

// 輔助函數
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr) >= 0
}

func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, char := range s {
		if char == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
