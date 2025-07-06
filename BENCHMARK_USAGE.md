# 性能基準測試使用指南

## 快速開始

### 1. 基本演示

運行簡單的性能測試演示：

```bash
go run benchmark_demo.go
```

### 2. 整合測試演示

運行完整的整合測試演示：

```bash
go run integration_test_benchmark.go
```

### 3. 完整性能測試套件

運行所有性能測試：

```bash
go run benchmark_test_main.go
```

## 詳細使用說明

### 測試類型

#### 1. 基本性能測試
- 執行時間測量
- 記憶體使用分析
- 成功/失敗狀態追蹤

#### 2. 數據吞吐量測試
- 測量數據處理速度 (MB/s)
- 大文件處理性能
- I/O 效率評估

#### 3. 操作吞吐量測試
- 測量操作執行速度 (ops/s)
- CPU 密集型任務性能
- 計算效率分析

#### 4. CSV 專項測試
- CSV 讀取性能
- 最大均值計算效率
- 數據正規化速度
- 大文件流式處理
- 併發處理能力

### 命令行選項

```bash
go run benchmark_test_main.go [選項]

可用選項:
  -log-level string    日誌級別 (debug, info, warn, error) [預設: info]
  -log-dir string      日誌目錄 [預設: ./benchmark_logs]
  -report-dir string   報告輸出目錄 [預設: ./benchmark_reports]  
  -config string       配置文件路徑 [預設: ./config.json]
  -csv-only           只執行 CSV 相關測試
  -verbose            詳細輸出
```

### 使用範例

#### 基本使用

```bash
# 執行所有測試
go run benchmark_test_main.go

# 詳細輸出模式
go run benchmark_test_main.go -verbose

# 只執行 CSV 測試
go run benchmark_test_main.go -csv-only
```

#### 自定義配置

```bash
# 指定日誌級別和目錄
go run benchmark_test_main.go -log-level debug -log-dir ./my_logs

# 指定報告輸出目錄
go run benchmark_test_main.go -report-dir ./performance_reports

# 使用自定義配置文件
go run benchmark_test_main.go -config ./my_config.json
```

#### 調試模式

```bash
# 完整調試輸出
go run benchmark_test_main.go -log-level debug -verbose

# CSV 調試測試
go run benchmark_test_main.go -csv-only -log-level debug -verbose
```

## 結果解讀

### 測試指標

1. **執行時間 (Duration)**
   - 測量單位：毫秒 (ms) 或微秒 (μs)
   - 越低越好
   - 代表操作完成所需時間

2. **記憶體使用 (Memory Usage)**
   - 測量單位：位元組 (bytes) 或 KB/MB
   - 越低越好
   - 代表操作過程中分配的記憶體

3. **數據吞吐量 (Data Throughput)**
   - 測量單位：MB/s
   - 越高越好
   - 代表每秒處理的數據量

4. **操作吞吐量 (Operation Throughput)**
   - 測量單位：ops/s (operations per second)
   - 越高越好
   - 代表每秒執行的操作次數

### 性能基準

#### 參考值 (一般桌面系統)

- **小文件讀取** (< 1MB): < 10ms
- **中文件讀取** (1-10MB): < 100ms
- **大文件讀取** (10-100MB): < 1000ms
- **記憶體效率**: < 文件大小的 3 倍
- **數據吞吐量**: > 50 MB/s (SSD), > 10 MB/s (HDD)

#### CSV 處理基準

- **100行×10列**: < 5ms
- **1000行×20列**: < 50ms
- **10000行×50列**: < 500ms
- **50000行×100列**: < 5000ms

### 報告分析

#### JSON 報告結構

```json
{
  "test_suite": "測試套件名稱",
  "timestamp": "執行時間戳",
  "environment": {
    "os": "作業系統",
    "arch": "系統架構",
    "cpus": "CPU核心數",
    "go_version": "Go版本"
  },
  "summary": {
    "total_tests": "總測試數",
    "passed_tests": "通過測試數", 
    "failed_tests": "失敗測試數",
    "total_duration_ms": "總執行時間",
    "avg_duration_ms": "平均執行時間",
    "total_memory_bytes": "總記憶體使用",
    "avg_throughput_mbps": "平均吞吐量"
  },
  "metrics": [
    {
      "name": "測試名稱",
      "duration_ms": "執行時間",
      "memory_bytes": "記憶體使用",
      "throughput_mbps": "吞吐量",
      "success": "是否成功"
    }
  ]
}
```

#### 關鍵分析點

1. **通過率**: passed_tests / total_tests
   - 應該接近 100%
   - 低通過率表示穩定性問題

2. **性能一致性**: 檢查各測試的執行時間變異
   - 過大變異表示性能不穩定

3. **記憶體效率**: memory_bytes vs 數據大小
   - 過高表示記憶體使用不當

4. **吞吐量趨勢**: 數據大小 vs 吞吐量關係
   - 應該相對穩定，不應大幅下降

## 性能最佳化建議

### 根據測試結果進行最佳化

#### 1. 記憶體最佳化

如果記憶體使用過高：

```go
// 使用流式處理
func processLargeFile(filename string) error {
    file, err := os.Open(filename)
    if err != nil {
        return err
    }
    defer file.Close()
    
    scanner := bufio.NewScanner(file)
    for scanner.Scan() {
        // 逐行處理，而非全部載入記憶體
        line := scanner.Text()
        processLine(line)
    }
    return scanner.Err()
}
```

#### 2. I/O 最佳化

如果 I/O 性能不佳：

```go
// 使用緩衝 I/O
func readWithBuffer(filename string) error {
    file, err := os.Open(filename)
    if err != nil {
        return err
    }
    defer file.Close()
    
    // 使用較大的緩衝區
    reader := bufio.NewReaderSize(file, 64*1024)
    // ... 處理邏輯
}
```

#### 3. 併發最佳化

如果單線程性能不足：

```go
// 使用工作池模式
func processConcurrently(data []Item) {
    const numWorkers = 4
    jobs := make(chan Item, len(data))
    results := make(chan Result, len(data))
    
    // 啟動工作協程
    for w := 0; w < numWorkers; w++ {
        go worker(jobs, results)
    }
    
    // 發送任務
    for _, item := range data {
        jobs <- item
    }
    close(jobs)
    
    // 收集結果
    for i := 0; i < len(data); i++ {
        <-results
    }
}
```

### 監控和調優

#### 1. 持續監控

建議定期執行性能測試：

```bash
# 每日自動化測試
#!/bin/bash
go run benchmark_test_main.go -report-dir ./daily_reports/$(date +%Y%m%d)
```

#### 2. 性能回歸檢測

比較不同版本的性能：

```bash
# 比較兩個報告
diff report_v1.json report_v2.json
```

#### 3. 瓶頸識別

查看最慢的測試：

```bash
# 從報告中提取最慢的操作
jq '.metrics | sort_by(.duration_ms) | reverse | .[0:5]' report.json
```

## 故障排除

### 常見問題

#### 1. 記憶體不足

**症狀**: 測試失敗，錯誤訊息包含 "out of memory"

**解決方案**:
- 減少測試數據大小
- 增加系統記憶體
- 使用流式處理模式

```bash
# 減少測試規模
go run benchmark_test_main.go -csv-only
```

#### 2. 測試超時

**症狀**: 測試長時間未完成

**解決方案**:
- 檢查系統資源使用率
- 減少併發測試數量
- 最佳化測試邏輯

#### 3. 結果不穩定

**症狀**: 重複測試結果差異很大

**解決方案**:
- 關閉其他應用程式
- 多次測試取平均值
- 檢查系統負載

### 調試技巧

#### 1. 詳細日誌

```bash
go run benchmark_test_main.go -log-level debug -verbose
```

#### 2. 單項測試

```bash
# 只測試特定組件
go run benchmark_test_main.go -csv-only
```

#### 3. 環境檢查

```bash
# 檢查系統資源
free -h    # 記憶體
df -h      # 磁盤空間
top        # CPU使用率
```

## 擴展開發

### 添加自定義測試

#### 1. 創建測試函數

```go
func (cb *CSVBenchmarks) BenchmarkCustomOperation() {
    cb.benchmarker.Benchmark("自定義測試", func() error {
        // 您的測試邏輯
        return nil
    })
}
```

#### 2. 集成到測試套件

```go
func (cb *CSVBenchmarks) RunAllBenchmarks() *BenchmarkResult {
    // 現有測試...
    cb.BenchmarkCustomOperation()
    
    return cb.benchmarker.GenerateReport("CSV處理性能測試")
}
```

### 自定義指標

```go
// 添加自定義指標
type CustomMetrics struct {
    BenchmarkMetrics
    CustomField1 float64 `json:"custom_field1"`
    CustomField2 string  `json:"custom_field2"`
}
```

### 測試自動化

#### 1. CI/CD 集成

```yaml
# .github/workflows/benchmark.yml
name: Performance Tests
on: [push, pull_request]
jobs:
  benchmark:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v2
    - uses: actions/setup-go@v2
      with:
        go-version: 1.24
    - name: Run benchmarks
      run: go run benchmark_test_main.go -report-dir ./reports
    - name: Upload reports
      uses: actions/upload-artifact@v2
      with:
        name: benchmark-reports
        path: ./reports
```

#### 2. 定期測試

```bash
# crontab 設定
0 2 * * * cd /path/to/project && go run benchmark_test_main.go -report-dir ./nightly_reports
```

---

此性能測試系統提供全面的 EMG 數據分析工具性能評估，幫助識別性能瓶頸並指導最佳化工作。