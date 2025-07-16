# EMG 數據分析工具 - 常見用法模式

## 概述

本文檔提供 EMG 數據分析工具的常見用法模式和最佳實踐指南，幫助開發者高效地使用系統進行 EMG 數據分析。

## 目錄

- [基本數據處理流程](#基本數據處理流程)
- [大文件處理模式](#大文件處理模式)
- [批量處理模式](#批量處理模式)
- [實時數據分析](#實時數據分析)
- [圖表生成最佳實踐](#圖表生成最佳實踐)
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
    "count_mean/internal/calculator"
    "count_mean/internal/chart"
    "count_mean/internal/io"
    "count_mean/internal/logging"
    "log"
)

func StandardEMGAnalysis() {
    // 1. 初始化日誌記錄
    logger := logging.GetLogger("analysis")
    
    // 2. 讀取 CSV 數據
    csvHandler := io.NewCSVHandler()
    dataset, err := csvHandler.ReadCSV("data/emg_data.csv")
    if err != nil {
        logger.Error("數據讀取失敗", err, map[string]interface{}{
            "file": "emg_data.csv",
        })
        return
    }
    
    // 3. 計算最大平均值
    calculator := calculator.NewMaxMeanCalculator()
    results, err := calculator.Calculate(dataset, 100)
    if err != nil {
        logger.Error("計算失敗", err, nil)
        return
    }
    
    // 4. 保存結果
    csvData, err := csvHandler.ConvertMaxMeanResultsToCSV(results)
    if err != nil {
        logger.Error("結果轉換失敗", err, nil)
        return
    }
    
    err = csvHandler.WriteCSVToOutput(csvData, "max_mean_results.csv")
    if err != nil {
        logger.Error("結果保存失敗", err, nil)
        return
    }
    
    // 5. 生成圖表
    chartGenerator := chart.NewChartGenerator()
    config := chart.ChartConfig{
        Title:      "EMG 分析結果",
        XAxisLabel: "時間 (秒)",
        YAxisLabel: "EMG 值",
        Width:      vg.Points(1200),
        Height:     vg.Points(800),
        Columns:    []string{"Channel1", "Channel2", "Channel3"},
    }
    
    err = chartGenerator.GenerateLineChart(dataset, config, "output/emg_chart.png")
    if err != nil {
        logger.Error("圖表生成失敗", err, nil)
        return
    }
    
    logger.Info("分析完成", map[string]interface{}{
        "results_count": len(results),
        "channels": len(dataset.Headers) - 1,
    })
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
func ProcessLargeEMGFile() {
    logger := logging.GetLogger("large_file")
    
    // 1. 初始化大文件處理器
    maxMemory := int64(1024 * 1024 * 1024) // 1GB
    chunkSize := 10000
    handler := io.NewLargeFileHandler(maxMemory, chunkSize)
    
    // 2. 準備流式處理回調
    var totalRecords int
    var processedChunks int
    
    processChunk := func(chunk []models.EMGData) error {
        totalRecords += len(chunk)
        processedChunks++
        
        // 處理數據塊
        calculator := calculator.NewMaxMeanCalculator()
        
        // 為此塊創建臨時數據集
        tempDataset := &models.EMGDataset{
            Headers: []string{"Time", "Channel1", "Channel2", "Channel3"},
            Data:    chunk,
        }
        
        // 計算此塊的結果
        results, err := calculator.Calculate(tempDataset, 100)
        if err != nil {
            return err
        }
        
        // 保存中間結果
        csvHandler := io.NewCSVHandler()
        csvData, err := csvHandler.ConvertMaxMeanResultsToCSV(results)
        if err != nil {
            return err
        }
        
        filename := fmt.Sprintf("chunk_%d_results.csv", processedChunks)
        err = csvHandler.WriteCSVToOutput(csvData, filename)
        if err != nil {
            return err
        }
        
        // 記錄進度
        logger.Info("處理進度", map[string]interface{}{
            "chunk":           processedChunks,
            "records_in_chunk": len(chunk),
            "total_records":    totalRecords,
        })
        
        return nil
    }
    
    // 3. 執行流式處理
    dataset, err := handler.ReadCSVStreaming("large_emg_file.csv", processChunk)
    if err != nil {
        logger.Error("大文件處理失敗", err, nil)
        return
    }
    
    // 4. 合併結果（可選）
    err = mergeLargeFileResults(processedChunks)
    if err != nil {
        logger.Error("結果合併失敗", err, nil)
        return
    }
    
    logger.Info("大文件處理完成", map[string]interface{}{
        "total_records": totalRecords,
        "processed_chunks": processedChunks,
    })
}

func mergeLargeFileResults(chunkCount int) error {
    // 合併所有塊的結果
    csvHandler := io.NewCSVHandler()
    var allResults []models.MaxMeanResult
    
    for i := 1; i <= chunkCount; i++ {
        filename := fmt.Sprintf("chunk_%d_results.csv", i)
        chunkData, err := csvHandler.ReadCSVFromOutput(filename)
        if err != nil {
            return err
        }
        
        // 將 CSV 轉換回 MaxMeanResult
        // 這裡需要實現轉換邏輯
        // ... 轉換邏輯
        
        // 清理臨時文件
        os.Remove(filepath.Join("output", filename))
    }
    
    // 保存最終結果
    finalCSV, err := csvHandler.ConvertMaxMeanResultsToCSV(allResults)
    if err != nil {
        return err
    }
    
    return csvHandler.WriteCSVToOutput(finalCSV, "final_large_file_results.csv")
}
```

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
func BatchProcessEMGFiles() {
    logger := logging.GetLogger("batch")
    
    // 1. 獲取所有 CSV 文件
    csvHandler := io.NewCSVHandler()
    files, err := csvHandler.ListInputFiles()
    if err != nil {
        logger.Error("獲取文件列表失敗", err, nil)
        return
    }
    
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

func batchWorker(jobs <-chan string, results chan<- ProcessResult, config BatchConfig) {
    for fileName := range jobs {
        startTime := time.Now()
        
        result := ProcessResult{
            FileName: fileName,
        }
        
        // 處理單個文件
        err := processSingleFile(fileName, config)
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

func processSingleFile(fileName string, config BatchConfig) error {
    // 讀取文件
    csvHandler := io.NewCSVHandler()
    dataset, err := csvHandler.ReadCSVFromInput(fileName)
    if err != nil {
        return err
    }
    
    // 計算最大平均值
    calculator := calculator.NewMaxMeanCalculator()
    results, err := calculator.Calculate(dataset, config.WindowSize)
    if err != nil {
        return err
    }
    
    // 保存結果
    csvData, err := csvHandler.ConvertMaxMeanResultsToCSV(results)
    if err != nil {
        return err
    }
    
    outputName := fmt.Sprintf("%s%s", config.OutputPrefix, fileName)
    err = csvHandler.WriteCSVToOutput(csvData, outputName)
    if err != nil {
        return err
    }
    
    // 生成圖表（可選）
    if config.GenerateCharts {
        chartGenerator := chart.NewEChartsGenerator()
        chartConfig := chart.InteractiveChartConfig{
            Title:           fmt.Sprintf("EMG 分析 - %s", fileName),
            XAxisLabel:      "時間 (秒)",
            YAxisLabel:      "EMG 值",
            ShowAllColumns:  true,
            Width:           "1200px",
            Height:          "800px",
        }
        
        chartName := fmt.Sprintf("%s%s.html", config.OutputPrefix, 
            strings.TrimSuffix(fileName, ".csv"))
        err = chartGenerator.GenerateInteractiveChart(dataset, chartConfig, 
            filepath.Join("output", chartName))
        if err != nil {
            return err
        }
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
    calculator := calculator.NewMaxMeanCalculator()
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
                results, err := calculator.Calculate(dataset, p.WindowSize)
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

## 圖表生成最佳實踐

### 模式：多樣化圖表生成

展示如何根據不同需求生成各種類型的圖表。

```go
func ComprehensiveChartGeneration() {
    logger := logging.GetLogger("charts")
    
    // 1. 讀取數據
    csvHandler := io.NewCSVHandler()
    dataset, err := csvHandler.ReadCSV("data/emg_data.csv")
    if err != nil {
        logger.Error("數據讀取失敗", err, nil)
        return
    }
    
    // 2. 生成基本線圖
    generateBasicLineChart(dataset)
    
    // 3. 生成互動式圖表
    generateInteractiveChart(dataset)
    
    // 4. 生成比較圖表
    generateComparisonChart(dataset)
    
    // 5. 生成批量圖表
    generateBatchCharts(dataset)
    
    // 6. 生成自定義主題圖表
    generateCustomThemedChart(dataset)
    
    logger.Info("圖表生成完成", nil)
}

func generateBasicLineChart(dataset *models.EMGDataset) {
    generator := chart.NewChartGenerator()
    
    config := chart.ChartConfig{
        Title:      "EMG 數據分析 - 基本線圖",
        XAxisLabel: "時間 (秒)",
        YAxisLabel: "EMG 值",
        Width:      vg.Points(1200),
        Height:     vg.Points(800),
        Columns:    []string{"Channel1", "Channel2", "Channel3"},
    }
    
    err := generator.GenerateLineChart(dataset, config, "output/basic_line_chart.png")
    if err != nil {
        log.Printf("基本線圖生成失敗: %v", err)
    }
}

func generateInteractiveChart(dataset *models.EMGDataset) {
    generator := chart.NewEChartsGenerator()
    
    // 獲取可用通道信息
    columns := generator.GetAvailableColumns(dataset)
    
    config := chart.InteractiveChartConfig{
        Title:           "EMG 數據分析 - 互動式圖表",
        XAxisLabel:      "時間 (秒)",
        YAxisLabel:      "EMG 值",
        SelectedColumns: []int{1, 2, 3},
        ColumnNames:     extractColumnNames(columns),
        ShowAllColumns:  false,
        Width:           "1400px",
        Height:          "900px",
    }
    
    err := generator.GenerateInteractiveChart(dataset, config, "output/interactive_chart.html")
    if err != nil {
        log.Printf("互動式圖表生成失敗: %v", err)
    }
}

func generateComparisonChart(dataset *models.EMGDataset) {
    generator := chart.NewEChartsGenerator()
    
    // 創建比較數據集（正常 vs 標準化）
    normalizer := calculator.NewNormalizer()
    normalizedData, err := normalizer.Normalize(dataset, []float64{0.5, 0.6, 0.7})
    if err != nil {
        log.Printf("數據標準化失敗: %v", err)
        return
    }
    
    datasets := []*models.EMGDataset{dataset, normalizedData}
    labels := []string{"原始數據", "標準化數據"}
    
    config := chart.InteractiveChartConfig{
        Title:           "EMG 數據比較 - 原始 vs 標準化",
        XAxisLabel:      "時間 (秒)",
        YAxisLabel:      "EMG 值",
        SelectedColumns: []int{1, 2},
        Width:           "1400px",
        Height:          "900px",
    }
    
    err = generator.GenerateComparisonChart(datasets, labels, config, "output/comparison_chart.html")
    if err != nil {
        log.Printf("比較圖表生成失敗: %v", err)
    }
}

func generateBatchCharts(dataset *models.EMGDataset) {
    generator := chart.NewEChartsGenerator()
    
    // 定義通道組合
    columnGroups := [][]int{
        {1, 2},    // 腿部肌群
        {2, 3},    // 核心肌群
        {1, 3},    // 混合肌群
    }
    
    baseConfig := chart.InteractiveChartConfig{
        Title:      "EMG 分析 - 通道組合",
        XAxisLabel: "時間 (秒)",
        YAxisLabel: "EMG 值",
        Width:      "1200px",
        Height:     "800px",
    }
    
    err := generator.BatchExportCharts(dataset, columnGroups, baseConfig, "output/batch_charts/")
    if err != nil {
        log.Printf("批量圖表生成失敗: %v", err)
    }
}

func generateCustomThemedChart(dataset *models.EMGDataset) {
    generator := chart.NewEChartsGenerator()
    
    // 生成自定義主題
    customTheme := generator.GenerateCustomTheme()
    
    config := chart.InteractiveChartConfig{
        Title:           "EMG 數據分析 - 自定義主題",
        XAxisLabel:      "時間 (秒)",
        YAxisLabel:      "EMG 值",
        SelectedColumns: []int{1, 2, 3},
        Width:           "1400px",
        Height:          "900px",
    }
    
    // 使用自定義主題生成圖表
    var buf bytes.Buffer
    err := generator.RenderChartToWriter(dataset, config, &buf)
    if err != nil {
        log.Printf("自定義主題圖表生成失敗: %v", err)
        return
    }
    
    // 將主題插入到 HTML 中
    htmlContent := buf.String()
    themedContent := strings.Replace(htmlContent, 
        "<script>", 
        fmt.Sprintf("<script>\n%s\n", customTheme), 
        1)
    
    err = os.WriteFile("output/custom_themed_chart.html", []byte(themedContent), 0644)
    if err != nil {
        log.Printf("自定義主題圖表保存失敗: %v", err)
    }
}

func extractColumnNames(columns []chart.ColumnInfo) []string {
    names := make([]string, len(columns))
    for i, col := range columns {
        names[i] = col.Name
    }
    return names
}
```

### 使用場景
- 研究報告圖表生成
- 數據可視化展示
- 批量圖表生成需求

### 最佳實踐
1. 根據數據特點選擇合適的圖表類型
2. 使用一致的顏色方案和樣式
3. 提供互動功能提升用戶體驗
4. 優化大數據集的圖表性能

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
        
        err := p.fallbackProcess(filePath)
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
    csvHandler := io.NewCSVHandler()
    dataset, err := csvHandler.ReadCSV(filePath)
    if err != nil {
        return &errors.AppError{
            Code:    errors.ErrFileNotFound,
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
            Code:    errors.ErrInvalidInput,
            Message: "數據驗證失敗",
            Cause:   err,
            Context: map[string]interface{}{
                "file_path": filePath,
                "stage":     "validate_data",
            },
        }
    }
    
    // 階段 3: 數據處理
    calculator := calculator.NewMaxMeanCalculator()
    results, err := calculator.Calculate(dataset, 100)
    if err != nil {
        return &errors.AppError{
            Code:    errors.ErrProcessingFailed,
            Message: "數據處理失敗",
            Cause:   err,
            Context: map[string]interface{}{
                "file_path": filePath,
                "stage":     "calculate",
            },
        }
    }
    
    // 階段 4: 結果保存
    csvData, err := csvHandler.ConvertMaxMeanResultsToCSV(results)
    if err != nil {
        return &errors.AppError{
            Code:    errors.ErrProcessingFailed,
            Message: "結果轉換失敗",
            Cause:   err,
            Context: map[string]interface{}{
                "file_path": filePath,
                "stage":     "convert_results",
            },
        }
    }
    
    outputName := fmt.Sprintf("recovered_%s", filepath.Base(filePath))
    err = csvHandler.WriteCSVToOutput(csvData, outputName)
    if err != nil {
        return &errors.AppError{
            Code:    errors.ErrFileNotFound,
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
        case errors.ErrFileNotFound:
            return false // 文件不存在不需要重試
        case errors.ErrInvalidInput:
            return false // 數據格式錯誤不需要重試
        case errors.ErrMemoryLimit:
            return true // 記憶體不足可以重試
        case errors.ErrProcessingFailed:
            return true // 處理失敗可以重試
        default:
            return true
        }
    }
    
    return errors.IsRecoverable(err)
}

func (p *ErrorHandlingProcessor) fallbackProcess(filePath string) error {
    p.Logger.Info("開始降級處理", map[string]interface{}{
        "file": filePath,
    })
    
    // 降級處理：使用大文件處理器
    handler := io.NewLargeFileHandler(512*1024*1024, 1000) // 降低記憶體使用
    
    var results []models.MaxMeanResult
    
    processChunk := func(chunk []models.EMGData) error {
        if len(chunk) < 10 {
            return nil // 跳過太小的塊
        }
        
        // 為此塊創建簡化的數據集
        tempDataset := &models.EMGDataset{
            Headers: []string{"Time", "Channel1"},
            Data:    chunk[:len(chunk)/2], // 只處理一半數據
        }
        
        calculator := calculator.NewMaxMeanCalculator()
        chunkResults, err := calculator.Calculate(tempDataset, 50) // 降低窗口大小
        if err != nil {
            return err
        }
        
        results = append(results, chunkResults...)
        return nil
    }
    
    _, err := handler.ReadCSVStreaming(filePath, processChunk)
    if err != nil {
        return err
    }
    
    // 保存降級結果
    csvHandler := io.NewCSVHandler()
    csvData, err := csvHandler.ConvertMaxMeanResultsToCSV(results)
    if err != nil {
        return err
    }
    
    outputName := fmt.Sprintf("fallback_%s", filepath.Base(filePath))
    return csvHandler.WriteCSVToOutput(csvData, outputName)
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
    
    // 6. 保存結果
    return saveOptimizedResults(finalResults, filePath, monitor)
}

type DataChunk struct {
    Data      []models.EMGData
    ChunkID   int
    Timestamp time.Time
}

func processWorker(workerID int, jobs <-chan DataChunk, results chan<- []models.MaxMeanResult,
                  options PerformanceOptions, monitor *PerformanceMonitor, 
                  dataPool, resultPool *sync.Pool) {
    
    calculator := calculator.NewMaxMeanCalculator()
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
            
            results_calc, err := calculator.Calculate(dataset, windowSize)
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
    
    handler := io.NewLargeFileHandler(options.MemoryLimit, options.ChunkSize)
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
    
    _, err := handler.ReadCSVStreaming(filePath, processChunk)
    return err
}

func processSequential(filePath string, options PerformanceOptions, monitor *PerformanceMonitor) error {
    // 順序處理實現
    handler := io.NewLargeFileHandler(options.MemoryLimit, options.ChunkSize)
    calculator := calculator.NewMaxMeanCalculator()
    
    var allResults []models.MaxMeanResult
    
    processChunk := func(chunk []models.EMGData) error {
        startTime := time.Now()
        
        if len(chunk) >= 50 {
            dataset := &models.EMGDataset{
                Headers: []string{"Time", "Channel1", "Channel2", "Channel3"},
                Data:    chunk,
            }
            
            results, err := calculator.Calculate(dataset, 50)
            if err == nil {
                allResults = append(allResults, results...)
            }
        }
        
        monitor.RecordProcessingTime(time.Since(startTime))
        monitor.RecordMemoryUsage()
        
        return nil
    }
    
    _, err := handler.ReadCSVStreaming(filePath, processChunk)
    if err != nil {
        return err
    }
    
    return saveOptimizedResults(allResults, filePath, monitor)
}

func saveOptimizedResults(results []models.MaxMeanResult, filePath string, monitor *PerformanceMonitor) error {
    csvHandler := io.NewCSVHandler()
    
    csvData, err := csvHandler.ConvertMaxMeanResultsToCSV(results)
    if err != nil {
        return err
    }
    
    outputName := fmt.Sprintf("optimized_%s", filepath.Base(filePath))
    return csvHandler.WriteCSVToOutput(csvData, outputName)
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
- [TODO 清單](../TODO.md) - 項目開發計劃
- [測試指南](../test/) - 單元測試和集成測試

---

*最後更新：2025-07-16*