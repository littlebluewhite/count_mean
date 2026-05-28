# EMG 數據分析工具 - 常見用法模式

## 概述

本文檔提供 EMG 數據分析工具的常見用法模式和最佳實踐指南，幫助開發者高效地使用系統進行 EMG 數據分析。

> **範例 code 慣例**：以下範例片段假設下列符號已在 scope 內：
> - `cfg *config.AppConfig`（由 `config.LoadConfig` 或 `config.DefaultConfig()` 取得）
> - `ctx context.Context`（GUI/CLI 端通常傳入 `context.Background()`，待加入取消按鈕後改為可取消的 context）
> - 必要的 logger / handler 已初始化
>
> 精確的函式簽名請以 `go doc count_mean/internal/<package>` 或 `docs/api.md` 為準；
> 本文檔重點在「**使用流程模式**」而非每行可直接 copy-paste 的 boilerplate。

## 目錄

- [基本數據處理流程](#基本數據處理流程)
- [大文件處理模式](#大文件處理模式)
- [批量處理模式](#批量處理模式)
- [實時數據分析](#實時數據分析)
- [錯誤處理與恢復](#錯誤處理與恢復)
- [性能優化技巧](#性能優化技巧)
- [配置管理模式](#配置管理模式)
- [並行處理模式](#並行處理模式)
- [數據驗證與安全](#數據驗證與安全)

---

## 基本數據處理流程

### 模式：標準 EMG 數據分析工作流程

這是最常見的 EMG 數據分析模式，適用於一般的研究和分析需求。

```go
package main

import (
    "context"
    "log"

    "count_mean/internal/calculator"
    "count_mean/internal/chart"
    "count_mean/internal/config"
    "count_mean/internal/io"
    "count_mean/internal/logging"
)

func StandardEMGAnalysis() {
    // 1. 載入設定與初始化日誌
    cfg, err := config.LoadConfig("./config.json")
    if err != nil {
        cfg = config.DefaultConfig()
    }
    logger := logging.GetLogger("analysis")

    // 2. 讀取 CSV 數據（取得原始 [][]string）
    csvHandler := io.NewCSVHandler(cfg)
    records, err := csvHandler.ReadCSV("emg_data.csv")
    if err != nil {
        logger.Error("數據讀取失敗", err, map[string]interface{}{
            "file": "emg_data.csv",
        })
        return
    }

    // 3. 計算最大平均值（傳 ctx 以支援取消）
    calc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
    results, err := calc.CalculateFromRawData(context.Background(), records, 100)
    if err != nil {
        logger.Error("計算失敗", err, nil)
        return
    }

    // 4. 轉成 CSV 列陣列並寫入 OutputDir
    csvData := csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, 0, 0)
    if err := csvHandler.WriteCSVToOutput("max_mean_results.csv", csvData); err != nil {
        logger.Error("結果保存失敗", err, nil)
        return
    }

    logger.Info("分析完成", map[string]interface{}{
        "results_count": len(results),
        "rows":          len(records) - 1,
    })
    _ = log.Default
}
```

### 使用場景
- 單個 EMG 文件的標準分析
- 研究項目的基本數據處理
- 教學演示

### 最佳實踐
1. 始終使用結構化日誌記錄
2. 在每個步驟後檢查錯誤
3. 使用有意義的輸出文件名
4. 記錄處理統計信息

---

## 大文件處理模式

### 模式：流式處理大型 EMG 文件

適用於處理超過 500MB 的大型 EMG 數據文件。

```go
func ProcessLargeEMGFile(cfg *config.AppConfig) {
    logger := logging.GetLogger("large_file")

    // 1. 初始化大文件處理器：chunk size / memory limit / backpressure 由 handler
    //    內部以工程經驗預設（記憶體上限 512 MB），caller 只傳 cfg。
    handler := io.NewLargeFileHandler(cfg)

    // 2. 進度回報：每 chunkSize 筆觸發一次（預設 1000；傳 nil 跳過回報）
    progressCallback := func(processed, total int64, percentage float64) {
        logger.Info("處理進度", map[string]interface{}{
            "processed":  processed,
            "total":      total,
            "percentage": percentage,
        })
    }

    // 3. 一次性執行串流滑動窗口計算：內部自動 streaming、ring buffer、backpressure
    //    達到 memoryLimit 時 fail-fast。回傳 *StreamingResult 含每通道最大平均值。
    result, err := handler.ProcessLargeFileInChunks("large_emg_file.csv", 500, progressCallback)
    if err != nil {
        logger.Error("大文件處理失敗", err, nil)
        return
    }

    // 4. 結果寫回 CSV（headers + results 都從 StreamingResult 取，無需手動 merge）
    csvHandler := io.NewCSVHandler(cfg)
    csvData := csvHandler.ConvertMaxMeanResultsToCSV(result.Headers, result.Results, 0, 0)
    if err := csvHandler.WriteCSVToOutput("large_file_results.csv", csvData); err != nil {
        logger.Error("結果保存失敗", err, nil)
        return
    }

    logger.Info("大文件處理完成", map[string]interface{}{
        "processed_lines": result.ProcessedLines,
        "duration":        result.Duration,
        "results_count":   len(result.Results),
    })
}
```

> **設計演進：** 早期 API 暴露 `ReadCSVStreaming(file, chunkCallback)` 讓 caller
> 自行於 callback 內計算並 merge — chunk 邊界處理複雜且容易出錯。現行
> `ProcessLargeFileInChunks` 把分塊、ring buffer、結果 merge 都內封在 handler，
> caller 只需呼叫 + 取 `result.Results`。

### 使用場景
- 處理大於 500MB 的 EMG 文件
- 記憶體有限的環境
- 需要實時處理進度反饋

### 最佳實踐
1. 根據系統記憶體調整塊大小
2. 實現進度回調顯示處理狀態
3. 使用臨時文件存儲中間結果
4. 處理完成後清理臨時文件

---

## 批量處理模式

### 模式：目錄中多個文件的批量處理

適用於處理多個 EMG 文件的批量分析需求。

```go
func BatchProcessEMGFiles(cfg *config.AppConfig) {
    logger := logging.GetLogger("batch")

    // 1. 列出 InputDir 下的 CSV 檔案（CSVHandler 不直接提供 ListFiles，
    //    透過 filepath.Glob 或 os.ReadDir 即可，路徑驗證由後續 ReadCSV 負責）
    pattern := filepath.Join(cfg.InputDir, "*.csv")
    files, err := filepath.Glob(pattern)
    if err != nil {
        logger.Error("獲取文件列表失敗", err, nil)
        return
    }

    csvHandler := io.NewCSVHandler(cfg)
    _ = csvHandler

    // 2. 批量處理配置
    batchConfig := BatchConfig{
        WindowSize:      100,
        OutputPrefix:    "batch_",
        GenerateCharts:  true,
        ParallelWorkers: 3,
    }
    
    // 3. 創建工作池
    jobs := make(chan string, len(files))
    results := make(chan ProcessResult, len(files))
    
    // 啟動工作協程
    for w := 0; w < batchConfig.ParallelWorkers; w++ {
        go batchWorker(jobs, results, batchConfig)
    }
    
    // 4. 發送作業
    for _, file := range files {
        jobs <- file
    }
    close(jobs)
    
    // 5. 收集結果
    var successCount, failureCount int
    var totalProcessingTime time.Duration
    
    for i := 0; i < len(files); i++ {
        result := <-results
        
        if result.Error != nil {
            failureCount++
            logger.Error("文件處理失敗", result.Error, map[string]interface{}{
                "file": result.FileName,
            })
        } else {
            successCount++
            totalProcessingTime += result.ProcessingTime
            logger.Info("文件處理成功", map[string]interface{}{
                "file":           result.FileName,
                "processing_time": result.ProcessingTime,
                "results_count":   result.ResultsCount,
            })
        }
    }
    
    // 6. 生成批量處理報告
    generateBatchReport(successCount, failureCount, totalProcessingTime)
    
    logger.Info("批量處理完成", map[string]interface{}{
        "total_files": len(files),
        "success":     successCount,
        "failures":    failureCount,
        "avg_time":    totalProcessingTime / time.Duration(successCount),
    })
}

type BatchConfig struct {
    WindowSize      int
    OutputPrefix    string
    GenerateCharts  bool
    ParallelWorkers int
}

type ProcessResult struct {
    FileName       string
    ProcessingTime time.Duration
    ResultsCount   int
    Error          error
}

func batchWorker(ctx context.Context, cfg *config.AppConfig, jobs <-chan string, results chan<- ProcessResult, config BatchConfig) {
    for fileName := range jobs {
        startTime := time.Now()

        result := ProcessResult{
            FileName: fileName,
        }

        // 處理單個文件
        err := processSingleFile(ctx, cfg, fileName, config)
        if err != nil {
            result.Error = err
        } else {
            result.ProcessingTime = time.Since(startTime)
            // 獲取結果計數邏輯
            result.ResultsCount = getResultsCount(fileName, config.OutputPrefix)
        }
        
        results <- result
    }
}

func processSingleFile(ctx context.Context, cfg *config.AppConfig, fileName string, config BatchConfig) error {
    // 讀取文件（CSVHandler.ReadCSVFromInput 回 [][]string，非 *EMGDataset；
    // 需用 DataParser 解析成 dataset 才能餵給 MaxMeanCalculator.Calculate）
    csvHandler := io.NewCSVHandler(cfg)
    records, err := csvHandler.ReadCSVFromInput(fileName)
    if err != nil {
        return err
    }

    parser := parsers.NewDataParser(cfg.ScalingFactor)
    dataset, err := parser.ParseRawData(records)
    if err != nil {
        return err
    }

    // 計算最大平均值
    calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
    results, err := calculator.Calculate(ctx, dataset, config.WindowSize)
    if err != nil {
        return err
    }

    // 保存結果（ConvertMaxMeanResultsToCSV 四參數版：headers / results / startRange / endRange）
    csvData := csvHandler.ConvertMaxMeanResultsToCSV(dataset.Headers, results, 0, 0)

    outputName := fmt.Sprintf("%s%s", config.OutputPrefix, fileName)
    err = csvHandler.WriteCSVToOutput(outputName, csvData)
    if err != nil {
        return err
    }
    
    return nil
}
```

### 使用場景
- 處理多個實驗的 EMG 數據
- 自動化數據處理流程
- 批量生成報告

### 最佳實踐
1. 使用工作池限制並行度
2. 記錄每個文件的處理結果
3. 生成批量處理報告
4. 實現錯誤恢復機制

---

## 實時數據分析

### 模式：實時 EMG 數據流處理

適用於需要實時處理 EMG 數據的場景。

```go
func RealTimeEMGAnalysis() {
    logger := logging.GetLogger("realtime")
    
    // 1. 初始化實時處理器
    processor := &RealTimeProcessor{
        WindowSize:    100,
        UpdateInterval: 1 * time.Second,
        DataBuffer:    make([]models.EMGData, 0, 1000),
        Results:       make(chan models.MaxMeanResult, 10),
        Stop:          make(chan bool),
    }
    
    // 2. 啟動數據接收協程
    go processor.StartDataReceiver()
    
    // 3. 啟動實時分析協程
    go processor.StartAnalysis()
    
    // 4. 啟動結果處理協程
    go processor.StartResultProcessor()
    
    // 5. 模擬實時數據流
    go simulateRealTimeData(processor)
    
    // 6. 運行指定時間
    time.Sleep(30 * time.Second)
    
    // 7. 停止處理
    processor.Stop <- true
    
    logger.Info("實時分析結束", nil)
}

type RealTimeProcessor struct {
    WindowSize     int
    UpdateInterval time.Duration
    DataBuffer     []models.EMGData
    Results        chan models.MaxMeanResult
    Stop           chan bool
    mutex          sync.RWMutex
}

func (p *RealTimeProcessor) StartDataReceiver() {
    // 實際應用中，這裡會連接到實時數據源
    // 例如串口、網絡連接等
}

func (p *RealTimeProcessor) StartAnalysis() {
    calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
    ticker := time.NewTicker(p.UpdateInterval)
    
    for {
        select {
        case <-ticker.C:
            // 分析當前緩衝區的數據
            p.mutex.RLock()
            if len(p.DataBuffer) >= p.WindowSize {
                // 創建數據集
                dataset := &models.EMGDataset{
                    Headers: []string{"Time", "Channel1", "Channel2", "Channel3"},
                    Data:    p.DataBuffer[len(p.DataBuffer)-p.WindowSize:],
                }
                
                // 計算結果
                results, err := calculator.Calculate(ctx, dataset, p.WindowSize)
                if err == nil && len(results) > 0 {
                    // 發送結果
                    select {
                    case p.Results <- results[0]:
                    default:
                        // 結果通道滿，丟棄舊結果
                    }
                }
            }
            p.mutex.RUnlock()
            
        case <-p.Stop:
            ticker.Stop()
            return
        }
    }
}

func (p *RealTimeProcessor) StartResultProcessor() {
    logger := logging.GetLogger("realtime-results")
    
    for {
        select {
        case result := <-p.Results:
            // 處理實時結果
            logger.Info("實時結果", map[string]interface{}{
                "channel":     result.ColumnIndex,
                "max_mean":    result.MaxMean,
                "start_time":  result.StartTime,
                "end_time":    result.EndTime,
            })
            
            // 可以在這裡觸發警報、更新UI等
            p.handleRealTimeResult(result)
            
        case <-p.Stop:
            return
        }
    }
}

func (p *RealTimeProcessor) AddData(data models.EMGData) {
    p.mutex.Lock()
    defer p.mutex.Unlock()
    
    p.DataBuffer = append(p.DataBuffer, data)
    
    // 限制緩衝區大小
    if len(p.DataBuffer) > 10000 {
        p.DataBuffer = p.DataBuffer[1000:]
    }
}

func (p *RealTimeProcessor) handleRealTimeResult(result models.MaxMeanResult) {
    // 實時結果處理邏輯
    // 例如：閾值警報、數據可視化更新等
    
    threshold := 0.1
    if result.MaxMean > threshold {
        // 觸發警報
        logger := logging.GetLogger("alert")
        logger.Warn("EMG 值超過閾值", map[string]interface{}{
            "channel":   result.ColumnIndex,
            "value":     result.MaxMean,
            "threshold": threshold,
        })
    }
}

func simulateRealTimeData(processor *RealTimeProcessor) {
    // 模擬實時數據生成
    ticker := time.NewTicker(10 * time.Millisecond)
    startTime := time.Now()
    
    for {
        select {
        case <-ticker.C:
            // 生成模擬數據
            currentTime := time.Since(startTime).Seconds()
            data := models.EMGData{
                Time: currentTime,
                Channels: []float64{
                    0.1 + 0.05*math.Sin(currentTime*2*math.Pi),
                    0.2 + 0.03*math.Cos(currentTime*3*math.Pi),
                    0.15 + 0.02*math.Sin(currentTime*4*math.Pi),
                },
            }
            
            processor.AddData(data)
            
        case <-processor.Stop:
            ticker.Stop()
            return
        }
    }
}
```

### 使用場景
- 實時 EMG 監控系統
- 生物反饋應用
- 運動分析系統

### 最佳實踐
1. 使用緩衝區管理實時數據
2. 實現非阻塞的結果處理
3. 設置適當的更新間隔
4. 監控系統性能指標

---

## 錯誤處理與恢復

### 模式：健壯的錯誤處理機制

展示如何實現全面的錯誤處理和恢復機制。

```go
func RobustErrorHandling() {
    logger := logging.GetLogger("error-handling")
    
    // 1. 設置錯誤恢復機制
    defer func() {
        if r := recover(); r != nil {
            logger.Fatal("系統異常", map[string]interface{}{
                "panic": r,
                "stack": string(debug.Stack()),
            })
        }
    }()
    
    // 2. 創建錯誤處理管道
    processor := &ErrorHandlingProcessor{
        MaxRetries:    3,
        RetryDelay:    1 * time.Second,
        FallbackMode:  true,
        Logger:        logger,
    }
    
    // 3. 執行帶錯誤恢復的處理
    err := processor.ProcessWithRecovery("data/problematic_data.csv")
    if err != nil {
        logger.Error("處理失敗", err, nil)
        return
    }
    
    logger.Info("錯誤處理測試完成", nil)
}

type ErrorHandlingProcessor struct {
    MaxRetries   int
    RetryDelay   time.Duration
    FallbackMode bool
    Logger       *logging.Logger
}

func (p *ErrorHandlingProcessor) ProcessWithRecovery(filePath string) error {
    var lastErr error
    
    for attempt := 1; attempt <= p.MaxRetries; attempt++ {
        p.Logger.Info("嘗試處理", map[string]interface{}{
            "attempt": attempt,
            "file":    filePath,
        })
        
        err := p.processFile(filePath)
        if err == nil {
            p.Logger.Info("處理成功", map[string]interface{}{
                "attempt": attempt,
                "file":    filePath,
            })
            return nil
        }
        
        lastErr = err
        
        // 錯誤分類和處理
        if p.shouldRetry(err) {
            p.Logger.Warn("處理失敗，準備重試", map[string]interface{}{
                "attempt": attempt,
                "error":   err.Error(),
                "file":    filePath,
            })
            
            if attempt < p.MaxRetries {
                time.Sleep(p.RetryDelay)
                continue
            }
        } else {
            p.Logger.Error("不可恢復的錯誤", err, map[string]interface{}{
                "file": filePath,
            })
            break
        }
    }
    
    // 嘗試降級處理
    if p.FallbackMode {
        p.Logger.Info("嘗試降級處理", map[string]interface{}{
            "file": filePath,
        })
        
        err := p.fallbackProcess(ctx, cfg, filePath)
        if err == nil {
            p.Logger.Info("降級處理成功", map[string]interface{}{
                "file": filePath,
            })
            return nil
        }
        
        p.Logger.Error("降級處理也失敗", err, map[string]interface{}{
            "file": filePath,
        })
    }
    
    return fmt.Errorf("最終處理失敗: %w", lastErr)
}

func (p *ErrorHandlingProcessor) processFile(filePath string) error {
    // 階段 1: 文件讀取
    csvHandler := io.NewCSVHandler(cfg)
    dataset, err := csvHandler.ReadCSV(filePath)
    if err != nil {
        return &errors.AppError{
            Code:    errors.ErrCodeFileNotFound,
            Message: "文件讀取失敗",
            Cause:   err,
            Context: map[string]interface{}{
                "file_path": filePath,
                "stage":     "read_csv",
            },
        }
    }
    
    // 階段 2: 數據驗證
    validator := validation.NewInputValidator()
    err = validator.ValidateCSVData(dataset)
    if err != nil {
        return &errors.AppError{
            Code:    errors.ErrCodeDataValidation,
            Message: "數據驗證失敗",
            Cause:   err,
            Context: map[string]interface{}{
                "file_path": filePath,
                "stage":     "validate_data",
            },
        }
    }
    
    // 階段 3: 數據處理
    calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
    results, err := calculator.Calculate(ctx, dataset, 100)
    if err != nil {
        return &errors.AppError{
            Code:    errors.ErrCodeCalculation,
            Message: "數據處理失敗",
            Cause:   err,
            Context: map[string]interface{}{
                "file_path": filePath,
                "stage":     "calculate",
            },
        }
    }
    
    // 階段 4: 結果保存（ConvertMaxMeanResultsToCSV 為 4 參數版，回 [][]string 無 error）
    csvData := csvHandler.ConvertMaxMeanResultsToCSV(dataset.Headers, results, 0, 0)

    outputName := fmt.Sprintf("recovered_%s", filepath.Base(filePath))
    err = csvHandler.WriteCSVToOutput(outputName, csvData)
    if err != nil {
        return &errors.AppError{
            Code:    errors.ErrCodeFileNotFound,
            Message: "結果保存失敗",
            Cause:   err,
            Context: map[string]interface{}{
                "file_path":   filePath,
                "output_name": outputName,
                "stage":       "save_results",
            },
        }
    }
    
    return nil
}

func (p *ErrorHandlingProcessor) shouldRetry(err error) bool {
    // 檢查錯誤類型，決定是否重試
    if appErr, ok := err.(*errors.AppError); ok {
        switch appErr.Code {
        case errors.ErrCodeFileNotFound:
            return false // 文件不存在不需要重試
        case errors.ErrCodeDataValidation:
            return false // 數據格式錯誤不需要重試
        case errors.ErrCodeFileTooLarge:
            return true // 記憶體不足可以重試
        case errors.ErrCodeCalculation:
            return true // 處理失敗可以重試
        default:
            return true
        }
    }
    
    return errors.IsRecoverable(err)
}

func (p *ErrorHandlingProcessor) fallbackProcess(ctx context.Context, cfg *config.AppConfig, filePath string) error {
    p.Logger.Info("開始降級處理", map[string]interface{}{
        "file": filePath,
    })

    // 降級處理：改用 LargeFileHandler 走串流路徑，降低 in-memory 峰值
    handler := io.NewLargeFileHandler(cfg)

    // 降低窗口大小換取較快回應；progressCallback 為 nil 跳過進度回報
    result, err := handler.ProcessLargeFileInChunks(filePath, 50, nil)
    if err != nil {
        return err
    }

    // 保存降級結果（Headers / Results 由 StreamingResult 提供）
    csvHandler := io.NewCSVHandler(cfg)
    csvData := csvHandler.ConvertMaxMeanResultsToCSV(result.Headers, result.Results, 0, 0)

    outputName := fmt.Sprintf("fallback_%s", filepath.Base(filePath))
    return csvHandler.WriteCSVToOutput(outputName, csvData)
}
```

### 使用場景
- 生產環境的健壯性要求
- 處理不可靠的數據源
- 自動化系統的容錯需求

### 最佳實踐
1. 實現分層錯誤處理機制
2. 使用結構化錯誤提供詳細信息
3. 實現智能重試邏輯
4. 提供降級處理選項
5. 記錄詳細的錯誤日誌

---

## 性能優化技巧

### 模式：系統性能優化

展示如何優化系統性能，特別是處理大型數據集時。

> **教學範例 caveat：** 以下 worker-pool + chunk channel 設計為**教學用 pattern**，
> 展示 backpressure / sync.Pool / 多 worker 協調等概念。
> 現實使用上 `LargeFileHandler.ProcessLargeFileInChunks` 已內封 worker pool、
> ring buffer、backpressure 與 -Inf channel skip — 外部 caller 通常無需自行
> wrap layer。若僅需大檔串流處理，請優先採用「大文件處理模式」段的簡單寫法。

```go
func OptimizedPerformanceProcessing() {
    logger := logging.GetLogger("performance")
    
    // 1. 初始化性能監控
    monitor := &PerformanceMonitor{
        StartTime: time.Now(),
        Logger:    logger,
    }
    
    // 2. 設置性能優化選項
    options := PerformanceOptions{
        UseParallelProcessing: true,
        MaxWorkers:           runtime.NumCPU(),
        ChunkSize:            5000,
        MemoryLimit:          2 * 1024 * 1024 * 1024, // 2GB
        EnableCaching:        true,
        OptimizeForSpeed:     true,
    }
    
    // 3. 執行優化處理
    err := processWithOptimization("large_dataset.csv", options, monitor)
    if err != nil {
        logger.Error("優化處理失敗", err, nil)
        return
    }
    
    // 4. 生成性能報告
    monitor.GenerateReport()
    
    logger.Info("性能優化處理完成", nil)
}

type PerformanceOptions struct {
    UseParallelProcessing bool
    MaxWorkers           int
    ChunkSize            int
    MemoryLimit          int64
    EnableCaching        bool
    OptimizeForSpeed     bool
}

type PerformanceMonitor struct {
    StartTime        time.Time
    Logger           *logging.Logger
    MemoryUsage      []int64
    ProcessingTimes  []time.Duration
    ThroughputPoints []float64
}

func processWithOptimization(filePath string, options PerformanceOptions, monitor *PerformanceMonitor) error {
    // 1. 記憶體優化設置
    if options.OptimizeForSpeed {
        // 調整 GC 設置
        debug.SetGCPercent(200) // 減少 GC 頻率
        defer debug.SetGCPercent(100)
    }
    
    // 2. 使用物件池減少記憶體分配
    dataPool := &sync.Pool{
        New: func() interface{} {
            return make([]models.EMGData, 0, options.ChunkSize)
        },
    }
    
    resultPool := &sync.Pool{
        New: func() interface{} {
            return make([]models.MaxMeanResult, 0, 100)
        },
    }
    
    // 3. 設置並行處理
    if options.UseParallelProcessing {
        return processParallel(filePath, options, monitor, dataPool, resultPool)
    }
    
    return processSequential(filePath, options, monitor)
}

func processParallel(filePath string, options PerformanceOptions, monitor *PerformanceMonitor, 
                    dataPool, resultPool *sync.Pool) error {
    
    // 1. 創建工作管道
    jobs := make(chan DataChunk, options.MaxWorkers*2)
    results := make(chan []models.MaxMeanResult, options.MaxWorkers*2)
    
    // 2. 啟動工作協程
    var wg sync.WaitGroup
    for i := 0; i < options.MaxWorkers; i++ {
        wg.Add(1)
        go func(workerID int) {
            defer wg.Done()
            processWorker(workerID, jobs, results, options, monitor, dataPool, resultPool)
        }(i)
    }
    
    // 3. 啟動結果收集協程
    var finalResults []models.MaxMeanResult
    resultWg := sync.WaitGroup{}
    resultWg.Add(1)
    go func() {
        defer resultWg.Done()
        for result := range results {
            finalResults = append(finalResults, result...)
        }
    }()
    
    // 4. 讀取和分發數據
    err := distributeData(filePath, options, jobs, monitor)
    if err != nil {
        return err
    }
    
    // 5. 等待處理完成
    close(jobs)
    wg.Wait()
    close(results)
    resultWg.Wait()
    
    // 6. 保存結果（headers 在實務上應從 dataset / StreamingResult 取，這裡示意）
    headers := []string{"Time", "Channel1", "Channel2", "Channel3"}
    return saveOptimizedResults(headers, finalResults, filePath, monitor)
}

type DataChunk struct {
    Data      []models.EMGData
    ChunkID   int
    Timestamp time.Time
}

func processWorker(workerID int, jobs <-chan DataChunk, results chan<- []models.MaxMeanResult,
                  options PerformanceOptions, monitor *PerformanceMonitor, 
                  dataPool, resultPool *sync.Pool) {
    
    calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
    logger := logging.GetLogger(fmt.Sprintf("worker-%d", workerID))
    
    for chunk := range jobs {
        startTime := time.Now()
        
        // 1. 獲取結果緩衝區
        chunkResults := resultPool.Get().([]models.MaxMeanResult)
        chunkResults = chunkResults[:0] // 重置切片
        
        // 2. 處理數據塊
        if len(chunk.Data) >= 50 {
            dataset := &models.EMGDataset{
                Headers: []string{"Time", "Channel1", "Channel2", "Channel3"},
                Data:    chunk.Data,
            }
            
            windowSize := 50
            if options.OptimizeForSpeed {
                windowSize = 30 // 使用較小的窗口提高速度
            }
            
            results_calc, err := calculator.Calculate(ctx, dataset, windowSize)
            if err == nil {
                chunkResults = append(chunkResults, results_calc...)
            }
        }
        
        // 3. 記錄性能指標
        processingTime := time.Since(startTime)
        monitor.RecordProcessingTime(processingTime)
        
        throughput := float64(len(chunk.Data)) / processingTime.Seconds()
        monitor.RecordThroughput(throughput)
        
        logger.Debug("塊處理完成", map[string]interface{}{
            "chunk_id":        chunk.ChunkID,
            "data_points":     len(chunk.Data),
            "results_count":   len(chunkResults),
            "processing_time": processingTime,
            "throughput":      throughput,
        })
        
        // 4. 發送結果
        results <- chunkResults
        
        // 5. 歸還緩衝區
        resultPool.Put(chunkResults)
    }
}

func distributeData(filePath string, options PerformanceOptions, jobs chan<- DataChunk, 
                   monitor *PerformanceMonitor) error {
    
    handler := io.NewLargeFileHandler(cfg) // chunk size / memory limit 由 handler 內部預設
    chunkID := 0
    
    processChunk := func(chunk []models.EMGData) error {
        // 監控記憶體使用
        monitor.RecordMemoryUsage()
        
        // 發送數據塊
        jobs <- DataChunk{
            Data:      chunk,
            ChunkID:   chunkID,
            Timestamp: time.Now(),
        }
        
        chunkID++
        return nil
    }
    
    // 註：實際 API 為 ProcessLargeFileInChunks(filePath, windowSize, progressCallback)，
    // 它不暴露 chunk callback；這段教學範例假想存在 chunk-level hook。
    _, err := handler.ProcessLargeFileInChunks(filePath, 50, nil)
    _ = processChunk // 教學示意：實際 API 不接 chunk callback
    return err
}

func processSequential(filePath string, options PerformanceOptions, monitor *PerformanceMonitor) error {
    // 順序處理實現
    handler := io.NewLargeFileHandler(cfg) // chunk size / memory limit 由 handler 內部預設
    calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
    
    var allResults []models.MaxMeanResult
    
    processChunk := func(chunk []models.EMGData) error {
        startTime := time.Now()
        
        if len(chunk) >= 50 {
            dataset := &models.EMGDataset{
                Headers: []string{"Time", "Channel1", "Channel2", "Channel3"},
                Data:    chunk,
            }
            
            results, err := calculator.Calculate(ctx, dataset, 50)
            if err == nil {
                allResults = append(allResults, results...)
            }
        }
        
        monitor.RecordProcessingTime(time.Since(startTime))
        monitor.RecordMemoryUsage()
        
        return nil
    }
    
    // 註：實際 API 為 ProcessLargeFileInChunks(filePath, windowSize, progressCallback)，
    // 它不暴露 chunk callback；這段教學範例假想存在 chunk-level hook。
    _, err := handler.ProcessLargeFileInChunks(filePath, 50, nil)
    _ = processChunk // 教學示意：實際 API 不接 chunk callback
    if err != nil {
        return err
    }
    
    headers := []string{"Time", "Channel1", "Channel2", "Channel3"}
    return saveOptimizedResults(headers, allResults, filePath, monitor)
}

func saveOptimizedResults(headers []string, results []models.MaxMeanResult, filePath string, monitor *PerformanceMonitor) error {
    csvHandler := io.NewCSVHandler(cfg)

    // ConvertMaxMeanResultsToCSV 為四參數版（headers / results / startRange / endRange），
    // 回 [][]string 無 error。
    csvData := csvHandler.ConvertMaxMeanResultsToCSV(headers, results, 0, 0)

    outputName := fmt.Sprintf("optimized_%s", filepath.Base(filePath))
    return csvHandler.WriteCSVToOutput(outputName, csvData)
}

func (m *PerformanceMonitor) RecordProcessingTime(duration time.Duration) {
    m.ProcessingTimes = append(m.ProcessingTimes, duration)
}

func (m *PerformanceMonitor) RecordMemoryUsage() {
    var mem runtime.MemStats
    runtime.ReadMemStats(&mem)
    m.MemoryUsage = append(m.MemoryUsage, int64(mem.Alloc))
}

func (m *PerformanceMonitor) RecordThroughput(throughput float64) {
    m.ThroughputPoints = append(m.ThroughputPoints, throughput)
}

func (m *PerformanceMonitor) GenerateReport() {
    totalTime := time.Since(m.StartTime)
    
    // 計算統計信息
    avgProcessingTime := m.calculateAverage(m.ProcessingTimes)
    maxMemory := m.calculateMaxMemory()
    avgThroughput := m.calculateAverageThroughput()
    
    m.Logger.Info("性能報告", map[string]interface{}{
        "total_time":           totalTime,
        "avg_processing_time":  avgProcessingTime,
        "max_memory_usage":     maxMemory,
        "avg_throughput":       avgThroughput,
        "processing_samples":   len(m.ProcessingTimes),
        "memory_samples":       len(m.MemoryUsage),
    })
}

func (m *PerformanceMonitor) calculateAverage(durations []time.Duration) time.Duration {
    if len(durations) == 0 {
        return 0
    }
    
    var total time.Duration
    for _, d := range durations {
        total += d
    }
    return total / time.Duration(len(durations))
}

func (m *PerformanceMonitor) calculateMaxMemory() int64 {
    var max int64
    for _, usage := range m.MemoryUsage {
        if usage > max {
            max = usage
        }
    }
    return max
}

func (m *PerformanceMonitor) calculateAverageThroughput() float64 {
    if len(m.ThroughputPoints) == 0 {
        return 0
    }
    
    var total float64
    for _, tp := range m.ThroughputPoints {
        total += tp
    }
    return total / float64(len(m.ThroughputPoints))
}
```

### 使用場景
- 大型數據集處理
- 高性能計算需求
- 實時系統優化

### 最佳實踐
1. 使用物件池減少記憶體分配
2. 實現並行處理提高吞吐量
3. 監控系統性能指標
4. 根據硬體資源調整參數
5. 使用分析工具識別性能瓶頸

---

## 結論

本文檔提供了 EMG 數據分析工具的常見用法模式和最佳實踐。通過遵循這些模式，開發者可以：

1. **提高開發效率** - 使用經過驗證的解決方案
2. **保證代碼質量** - 遵循最佳實踐標準
3. **增強系統健壯性** - 實現完善的錯誤處理
4. **優化系統性能** - 根據具體需求調整配置
5. **簡化維護工作** - 使用一致的代碼風格

這些模式可以根據具體需求進行調整和擴展，為不同場景提供靈活的解決方案。

---

## 相關文檔

- [API 文檔](api.md) - 完整的 API 參考
- [測試指南](../test/) - 單元測試和集成測試

---

*最後更新：2025-07-16*