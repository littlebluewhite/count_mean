// Package benchmark provides performance testing and benchmarking utilities
// for measuring execution time, memory usage, and throughput of operations.
package benchmark

import (
	"fmt"
	"os"
	"runtime"
	"time"

	"count_mean/internal/config"
	"count_mean/internal/errors"
	"count_mean/internal/logging"
)

// Bytes conversion constants.
const (
	bytesPerMB = 1024 * 1024
)

// Metrics 性能測試指標.
type Metrics struct {
	Name           string        `json:"name"`            // 測試名稱
	Duration       time.Duration `json:"duration"`        // 執行時間
	MemoryUsage    uint64        `json:"memory_usage"`    // 記憶體使用量 (bytes)
	AllocCount     uint64        `json:"alloc_count"`     // 記憶體分配次數
	ThroughputOps  float64       `json:"throughput_ops"`  // 操作吞吐量 (ops/sec)
	ThroughputData float64       `json:"throughput_data"` // 數據吞吐量 (MB/sec)
	CPUUsage       float64       `json:"cpu_usage"`       // CPU 使用率 (%)
	Success        bool          `json:"success"`         // 是否成功
	Error          string        `json:"error,omitempty"` // 錯誤訊息
	StartTime      time.Time     `json:"start_time"`      // 開始時間
	EndTime        time.Time     `json:"end_time"`        // 結束時間
}

// Result 基準測試結果.
type Result struct {
	TestSuite   string     `json:"test_suite"`  // 測試套件名稱
	Timestamp   time.Time  `json:"timestamp"`   // 測試時間戳
	Environment SystemInfo `json:"environment"` // 系統環境
	Metrics     []Metrics  `json:"metrics"`     // 測試指標
	Summary     Summary    `json:"summary"`     // 測試摘要
}

// SystemInfo 系統資訊.
type SystemInfo struct {
	OS          string `json:"os"`           // 作業系統
	Arch        string `json:"arch"`         // 系統架構
	CPUs        int    `json:"cpus"`         // CPU 核心數
	GoVersion   string `json:"go_version"`   // Go 版本
	TotalMemory uint64 `json:"total_memory"` // 總記憶體
}

// Summary 測試摘要.
type Summary struct {
	TotalTests    int           `json:"total_tests"`    // 總測試數
	PassedTests   int           `json:"passed_tests"`   // 通過測試數
	FailedTests   int           `json:"failed_tests"`   // 失敗測試數
	TotalDuration time.Duration `json:"total_duration"` // 總執行時間
	AvgDuration   time.Duration `json:"avg_duration"`   // 平均執行時間
	MaxDuration   time.Duration `json:"max_duration"`   // 最大執行時間
	MinDuration   time.Duration `json:"min_duration"`   // 最小執行時間
	TotalMemory   uint64        `json:"total_memory"`   // 總記憶體使用
	AvgMemory     uint64        `json:"avg_memory"`     // 平均記憶體使用
	AvgThroughput float64       `json:"avg_throughput"` // 平均吞吐量
}

// Benchmarker 性能測試器.
type Benchmarker struct {
	logger  *logging.Logger
	config  *config.AppConfig
	results []Metrics
	sysInfo SystemInfo
}

// NewBenchmarker 創建新的性能測試器.
func NewBenchmarker(cfg *config.AppConfig) *Benchmarker {
	return &Benchmarker{
		logger:  logging.GetLogger("benchmark"),
		config:  cfg,
		results: make([]Metrics, 0),
		sysInfo: getSystemInfo(),
	}
}

// getSystemInfo 獲取系統資訊.
func getSystemInfo() SystemInfo {
	var memStats runtime.MemStats

	runtime.ReadMemStats(&memStats)

	return SystemInfo{
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		CPUs:        runtime.NumCPU(),
		GoVersion:   runtime.Version(),
		TotalMemory: memStats.Sys,
	}
}

// Benchmark 執行性能測試.
func (b *Benchmarker) Benchmark(name string, fn func() error) *Metrics {
	b.logger.Info("開始性能測試", map[string]any{"test": name})

	// 強制垃圾回收以獲得準確的記憶體測量
	runtime.GC()

	var m1, m2 runtime.MemStats

	runtime.ReadMemStats(&m1)

	startTime := time.Now()

	// 執行測試函數
	err := fn()

	endTime := time.Now()
	duration := endTime.Sub(startTime)

	runtime.ReadMemStats(&m2)

	metrics := Metrics{
		Name:        name,
		Duration:    duration,
		MemoryUsage: m2.TotalAlloc - m1.TotalAlloc,
		AllocCount:  m2.Mallocs - m1.Mallocs,
		Success:     err == nil,
		StartTime:   startTime,
		EndTime:     endTime,
	}

	if err != nil {
		metrics.Error = err.Error()
		b.logger.Error("性能測試失敗", err, map[string]any{"test": name})
	} else {
		b.logger.Info("性能測試完成", map[string]any{
			"test":     name,
			"duration": duration,
			"memory":   metrics.MemoryUsage,
		})
	}

	b.results = append(b.results, metrics)

	return &metrics
}

// BenchmarkWithData 執行帶數據量的性能測試.
func (b *Benchmarker) BenchmarkWithData(name string, dataSize int64, fn func() error) *Metrics {
	metrics := b.Benchmark(name, fn)

	if metrics.Success && metrics.Duration > 0 {
		// 計算吞吐量
		dataSizeMB := float64(dataSize) / bytesPerMB
		metrics.ThroughputData = dataSizeMB / metrics.Duration.Seconds()

		b.logger.Info("數據吞吐量測試完成", map[string]any{
			"test":            name,
			"data_size_mb":    dataSizeMB,
			"throughput_mbps": metrics.ThroughputData,
		})
	}

	return metrics
}

// BenchmarkOperations 執行操作次數性能測試.
func (b *Benchmarker) BenchmarkOperations(name string, operationCount int, fn func() error) *Metrics {
	metrics := b.Benchmark(name, fn)

	if metrics.Success && metrics.Duration > 0 {
		// 計算操作吞吐量
		metrics.ThroughputOps = float64(operationCount) / metrics.Duration.Seconds()

		b.logger.Info("操作吞吐量測試完成", map[string]any{
			"test":            name,
			"operation_count": operationCount,
			"throughput_ops":  metrics.ThroughputOps,
		})
	}

	return metrics
}

// GetResults 獲取測試結果.
func (b *Benchmarker) GetResults() []Metrics {
	return b.results
}

// GenerateReport 生成完整的測試報告.
func (b *Benchmarker) GenerateReport(testSuite string) *Result {
	summary := b.calculateSummary()

	return &Result{
		TestSuite:   testSuite,
		Timestamp:   time.Now(),
		Environment: b.sysInfo,
		Metrics:     b.results,
		Summary:     summary,
	}
}

// calculateSummary 計算測試摘要.
func (b *Benchmarker) calculateSummary() Summary {
	if len(b.results) == 0 {
		return Summary{}
	}

	summary := Summary{
		TotalTests:  len(b.results),
		MinDuration: time.Duration(^uint64(0) >> 1), // 最大值
	}

	var totalDuration time.Duration

	var totalMemory, totalThroughput uint64

	for i := range b.results {
		result := &b.results[i]
		totalDuration += result.Duration
		totalMemory += result.MemoryUsage

		if result.Success {
			summary.PassedTests++
			totalThroughput += uint64(result.ThroughputData)
		} else {
			summary.FailedTests++
		}

		if result.Duration > summary.MaxDuration {
			summary.MaxDuration = result.Duration
		}

		if result.Duration < summary.MinDuration {
			summary.MinDuration = result.Duration
		}
	}

	summary.TotalDuration = totalDuration
	summary.AvgDuration = totalDuration / time.Duration(len(b.results))
	summary.TotalMemory = totalMemory
	summary.AvgMemory = totalMemory / uint64(len(b.results))

	if summary.PassedTests > 0 {
		summary.AvgThroughput = float64(totalThroughput) / float64(summary.PassedTests)
	}

	return summary
}

// Reset 重置測試結果.
func (b *Benchmarker) Reset() {
	b.results = make([]Metrics, 0)
	b.logger.Info("基準測試器已重置")
}

// SaveReportToFile 保存報告到文件.
func (b *Benchmarker) SaveReportToFile(report *Result, filename string) error {
	// 使用 JSON 格式保存報告
	content := formatReportAsJSON(report)

	//nolint:gosec // G304: filename is provided by caller for benchmark output
	file, err := os.Create(filename)
	if err != nil {
		return errors.WrapError(err, errors.ErrCodeFilePermission,
			fmt.Sprintf("無法創建報告文件: %s", filename))
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	_, err = file.WriteString(content)
	if err != nil {
		return errors.WrapError(err, errors.ErrCodeFilePermission,
			fmt.Sprintf("無法寫入報告文件: %s", filename))
	}

	b.logger.Info("性能報告已保存", map[string]any{"file": filename})

	return nil
}

// formatReportAsJSON 格式化報告為 JSON.
func formatReportAsJSON(report *Result) string {
	// 手動構建 JSON 字符串以確保格式正確
	content := fmt.Sprintf(`{
  "test_suite": "%s",
  "timestamp": "%s",
  "environment": {
    "os": "%s",
    "arch": "%s",
    "cpus": %d,
    "go_version": "%s",
    "total_memory": %d
  },
  "summary": {
    "total_tests": %d,
    "passed_tests": %d,
    "failed_tests": %d,
    "total_duration_ms": %d,
    "avg_duration_ms": %d,
    "max_duration_ms": %d,
    "min_duration_ms": %d,
    "total_memory_bytes": %d,
    "avg_memory_bytes": %d,
    "avg_throughput_mbps": %.2f
  },
  "metrics": [`,
		report.TestSuite,
		report.Timestamp.Format(time.RFC3339),
		report.Environment.OS,
		report.Environment.Arch,
		report.Environment.CPUs,
		report.Environment.GoVersion,
		report.Environment.TotalMemory,
		report.Summary.TotalTests,
		report.Summary.PassedTests,
		report.Summary.FailedTests,
		report.Summary.TotalDuration.Milliseconds(),
		report.Summary.AvgDuration.Milliseconds(),
		report.Summary.MaxDuration.Milliseconds(),
		report.Summary.MinDuration.Milliseconds(),
		report.Summary.TotalMemory,
		report.Summary.AvgMemory,
		report.Summary.AvgThroughput)

	// 添加每個測試的詳細結果
	for i := range report.Metrics {
		metric := &report.Metrics[i]

		if i > 0 {
			content += ","
		}

		content += fmt.Sprintf(`
    {
      "name": "%s",
      "duration_ms": %d,
      "memory_bytes": %d,
      "alloc_count": %d,
      "throughput_ops": %.2f,
      "throughput_mbps": %.2f,
      "success": %t,
      "error": "%s",
      "start_time": "%s",
      "end_time": "%s"
    }`,
			metric.Name,
			metric.Duration.Milliseconds(),
			metric.MemoryUsage,
			metric.AllocCount,
			metric.ThroughputOps,
			metric.ThroughputData,
			metric.Success,
			metric.Error,
			metric.StartTime.Format(time.RFC3339),
			metric.EndTime.Format(time.RFC3339))
	}

	content += `
  ]
}`

	return content
}

// PrintSummary 打印測試摘要.
//
//nolint:revive // unhandled-error: stdout write errors are intentionally ignored for console output
func (b *Benchmarker) PrintSummary() {
	if len(b.results) == 0 {
		fmt.Println("沒有測試結果")
		return
	}

	summary := b.calculateSummary()

	fmt.Println("=== 性能測試摘要 ===")
	fmt.Printf("總測試數: %d\n", summary.TotalTests)
	fmt.Printf("通過測試: %d\n", summary.PassedTests)
	fmt.Printf("失敗測試: %d\n", summary.FailedTests)
	fmt.Printf("總執行時間: %v\n", summary.TotalDuration)
	fmt.Printf("平均執行時間: %v\n", summary.AvgDuration)
	fmt.Printf("最大執行時間: %v\n", summary.MaxDuration)
	fmt.Printf("最小執行時間: %v\n", summary.MinDuration)
	fmt.Printf("總記憶體使用: %d bytes (%.2f MB)\n",
		summary.TotalMemory, float64(summary.TotalMemory)/bytesPerMB)
	fmt.Printf("平均記憶體使用: %d bytes (%.2f MB)\n",
		summary.AvgMemory, float64(summary.AvgMemory)/bytesPerMB)

	if summary.AvgThroughput > 0 {
		fmt.Printf("平均吞吐量: %.2f MB/s\n", summary.AvgThroughput)
	}

	fmt.Println("\n=== 各項測試結果 ===")

	for i := range b.results {
		result := &b.results[i]

		status := "PASS"
		if !result.Success {
			status = "FAIL"
		}

		fmt.Printf("%s %s: %v (記憶體: %.2f MB",
			status, result.Name, result.Duration,
			float64(result.MemoryUsage)/bytesPerMB)

		if result.ThroughputData > 0 {
			fmt.Printf(", 吞吐量: %.2f MB/s", result.ThroughputData)
		}

		if result.ThroughputOps > 0 {
			fmt.Printf(", 操作數: %.0f ops/s", result.ThroughputOps)
		}

		fmt.Println(")")

		if !result.Success {
			fmt.Printf("  錯誤: %s\n", result.Error)
		}
	}
}
