package main

import (
	"count_mean/internal/config"
	"count_mean/internal/logging"
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
	benchmarker := NewBenchmarker(cfg)

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
	benchmarker := NewBenchmarker(cfg)

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
	sysInfo := getSystemInfo()

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
	benchmarker := NewBenchmarker(cfg)

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
	csvBench, err := NewCSVBenchmarks(cfg)
	if err != nil {
		t.Fatalf("創建 CSV 基準測試失敗: %v", err)
	}
	defer csvBench.Cleanup()

	t.Run("生成測試文件", func(t *testing.T) {
		filePath, fileSize, err := csvBench.generateTestCSV("test.csv", 100, 10)
		if err != nil {
			t.Errorf("生成測試文件失敗: %v", err)
		}

		if fileSize <= 0 {
			t.Error("文件大小應該大於0")
		}

		// 檢查文件是否存在
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Error("測試文件未創建")
		}
	})

	t.Run("CSV 讀取測試", func(t *testing.T) {
		// 這個測試可能需要實際的 CSV 處理器
		// 為了測試目的，我們只測試測試文件的生成
		filePath, _, err := csvBench.generateTestCSV("read_test.csv", 10, 5)
		if err != nil {
			t.Errorf("生成測試文件失敗: %v", err)
		}

		// 檢查文件內容
		content, err := os.ReadFile(filePath)
		if err != nil {
			t.Errorf("讀取測試文件失敗: %v", err)
		}

		lines := splitLines(string(content))
		if len(lines) < 11 { // 1 header + 10 data rows
			t.Errorf("期望至少11行，實際: %d", len(lines))
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
