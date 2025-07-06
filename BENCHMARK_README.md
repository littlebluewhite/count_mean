# 性能基準測試文檔

## 概述

本項目包含一個全面的性能基準測試系統，用於評估 EMG 數據分析工具的各項性能指標，包括：

- CSV 文件處理性能
- 數據計算性能 
- 記憶體使用效率
- 併發處理能力
- 系統資源利用率

## 測試結構

### 核心組件

1. **Benchmarker** (`internal/benchmark/benchmark.go`)
   - 基礎性能測試框架
   - 提供時間、記憶體、吞吐量測量
   - 支援測試報告生成和保存

2. **CSVBenchmarks** (`internal/benchmark/csv_benchmarks.go`)
   - CSV 文件處理專用性能測試
   - 包含讀取、計算、正規化等測試
   - 支援大文件和併發處理測試

3. **測試程序**
   - `benchmark_demo.go` - 簡單演示程序
   - `benchmark_test_main.go` - 完整測試套件
   - `benchmark_test.go` - 單元測試

## 使用方法

### 1. 快速演示

執行基本性能測試演示：

```bash
go run benchmark_demo.go
```

這將執行：
- 基本計算性能測試
- 簡單的 CSV 處理測試
- 系統環境資訊顯示

### 2. 完整測試套件

執行所有性能測試：

```bash
go run benchmark_test_main.go
```

#### 命令行選項

```bash
go run benchmark_test_main.go [選項]

選項:
  -log-level string    日誌級別 (debug, info, warn, error) (預設 "info")
  -log-dir string      日誌目錄 (預設 "./benchmark_logs")
  -report-dir string   報告輸出目錄 (預設 "./benchmark_reports")
  -config string       配置文件路徑 (預設 "./config.json")
  -csv-only           只執行 CSV 相關測試
  -verbose            詳細輸出
```

#### 使用範例

```bash
# 執行所有測試，詳細輸出
go run benchmark_test_main.go -verbose

# 只執行 CSV 測試
go run benchmark_test_main.go -csv-only

# 指定報告目錄
go run benchmark_test_main.go -report-dir ./my_reports

# 調試模式
go run benchmark_test_main.go -log-level debug -verbose
```

### 3. 單元測試

執行性能測試的單元測試：

```bash
cd internal/benchmark
go test -v
```

## 測試類別

### CSV 處理性能測試

1. **CSV 讀取測試**
   - 小文件 (100行×10列)
   - 中文件 (1,000行×20列)  
   - 大文件 (10,000行×50列)
   - 超大文件 (50,000行×100列)

2. **最大均值計算測試**
   - 不同數據集大小
   - 不同窗口大小 (50, 100, 200, 500)
   - 測量計算吞吐量

3. **數據正規化測試**
   - 小、中、大數據集
   - 記憶體使用分析

4. **大文件處理測試**
   - 流式處理性能
   - 分塊處理效率
   - 記憶體控制測試

5. **併發處理測試**
   - 多文件同時處理
   - 併發安全性驗證
   - 資源競爭測試

6. **記憶體使用測試**
   - 記憶體分配模式
   - 垃圾回收影響
   - 記憶體洩漏檢測

### 系統性能測試

1. **文件 I/O 測試**
   - 讀寫速度
   - 緩衝效率

2. **數學計算測試**
   - 浮點運算性能
   - 大量計算吞吐量

3. **字符串處理測試**
   - 字符串操作效率
   - 記憶體分配模式

### 記憶體效能測試

1. **記憶體分配測試**
   - 分配速度
   - 分配模式

2. **大數組處理測試**
   - 大記憶體塊處理
   - 快取效率

3. **垃圾回收測試**
   - GC 對性能的影響
   - 記憶體回收效率

### 併發性能測試

1. **Goroutine 測試**
   - Goroutine 創建和銷毀
   - 併發任務處理

2. **Channel 通信測試**
   - Channel 傳輸效率
   - 同步開銷

3. **鎖競爭測試**
   - 併發安全性
   - 鎖競爭影響

## 測試報告

### 報告格式

測試報告以 JSON 格式保存，包含：

```json
{
  "test_suite": "測試套件名稱",
  "timestamp": "2024-01-01T12:00:00Z",
  "environment": {
    "os": "作業系統",
    "arch": "系統架構", 
    "cpus": "CPU核心數",
    "go_version": "Go版本",
    "total_memory": "總記憶體"
  },
  "summary": {
    "total_tests": "總測試數",
    "passed_tests": "通過測試數",
    "failed_tests": "失敗測試數",
    "total_duration_ms": "總執行時間(毫秒)",
    "avg_duration_ms": "平均執行時間(毫秒)",
    "max_duration_ms": "最大執行時間(毫秒)",
    "min_duration_ms": "最小執行時間(毫秒)",
    "total_memory_bytes": "總記憶體使用(位元組)",
    "avg_memory_bytes": "平均記憶體使用(位元組)",
    "avg_throughput_mbps": "平均吞吐量(MB/s)"
  },
  "metrics": [
    {
      "name": "測試名稱",
      "duration_ms": "執行時間(毫秒)",
      "memory_bytes": "記憶體使用(位元組)",
      "alloc_count": "記憶體分配次數",
      "throughput_ops": "操作吞吐量(ops/s)",
      "throughput_mbps": "數據吞吐量(MB/s)",
      "success": "是否成功",
      "error": "錯誤訊息",
      "start_time": "開始時間",
      "end_time": "結束時間"
    }
  ]
}
```

### 報告位置

- 預設報告目錄：`./benchmark_reports/`
- CSV 測試報告：`csv_benchmark_report_YYYYMMDD_HHMMSS.json`
- 系統測試報告：`system_benchmark_report_YYYYMMDD_HHMMSS.json`
- 記憶體測試報告：`memory_benchmark_report_YYYYMMDD_HHMMSS.json`
- 併發測試報告：`concurrency_benchmark_report_YYYYMMDD_HHMMSS.json`

## 性能指標

### 關鍵指標

1. **執行時間**
   - 平均執行時間
   - 最大/最小執行時間
   - 標準差

2. **記憶體使用**
   - 總記憶體分配
   - 平均記憶體使用
   - 記憶體分配次數

3. **吞吐量**
   - 數據吞吐量 (MB/s)
   - 操作吞吐量 (ops/s)
   - 處理效率

4. **成功率**
   - 測試通過率
   - 錯誤分析
   - 穩定性評估

### 基準值

根據測試結果建立的參考基準值：

- **小文件讀取** (1MB): < 10ms
- **中文件讀取** (10MB): < 100ms  
- **大文件讀取** (100MB): < 1s
- **記憶體使用**: < 文件大小的 2 倍
- **吞吐量**: > 50 MB/s (SSD)

## 最佳化建議

基於測試結果的性能最佳化建議：

1. **檔案處理**
   - 使用緩衝 I/O
   - 實施流式處理
   - 合理設置緩衝區大小

2. **記憶體管理**
   - 及時釋放大對象
   - 重用記憶體池
   - 控制併發度

3. **計算最佳化**
   - 使用向量化操作
   - 快取計算結果
   - 並行處理獨立任務

4. **併發控制**
   - 合理設置 goroutine 數量
   - 使用工作池模式
   - 避免過度同步

## 故障排除

### 常見問題

1. **記憶體不足錯誤**
   - 減少測試數據量
   - 增加系統記憶體
   - 使用流式處理

2. **測試超時**
   - 調整測試參數
   - 檢查系統資源
   - 最佳化測試邏輯

3. **併發測試失敗**
   - 檢查線程安全性
   - 調整併發度
   - 分析競爭條件

### 調試技巧

1. **啟用詳細日誌**
   ```bash
   go run benchmark_test_main.go -log-level debug -verbose
   ```

2. **單獨測試組件**
   ```bash
   go run benchmark_test_main.go -csv-only
   ```

3. **分析測試報告**
   - 查看 JSON 報告詳細資料
   - 比較不同測試結果
   - 識別性能瓶頸

## 擴展測試

### 添加新測試

1. 在 `CSVBenchmarks` 中添加新方法
2. 實現測試邏輯
3. 調用 `benchmarker.Benchmark()` 或相關方法
4. 更新測試套件

### 自定義測試

```go
// 創建自定義測試
func customBenchmark() {
    cfg := config.DefaultConfig()
    benchmarker := benchmark.NewBenchmarker(cfg)
    
    metrics := benchmarker.Benchmark("自定義測試", func() error {
        // 您的測試邏輯
        return nil
    })
    
    fmt.Printf("測試結果: %v, 記憶體: %d bytes\n", 
        metrics.Duration, metrics.MemoryUsage)
}
```

## 注意事項

1. **測試環境**
   - 確保系統資源充足
   - 關閉其他耗資源程序
   - 使用穩定的測試環境

2. **測試數據**
   - 使用代表性測試數據
   - 考慮數據大小影響
   - 測試邊界條件

3. **結果分析**
   - 多次測試取平均值
   - 注意環境因素影響
   - 比較相對性能

4. **安全性**
   - 測試會創建臨時文件
   - 確保有足夠磁盤空間
   - 測試後會自動清理

---

此性能測試系統為 EMG 數據分析工具提供全面的性能評估和最佳化指導。