# EMG 數據分析工具 API 文檔

## 概述

本文檔提供 EMG 數據分析工具的完整 API 參考，包含所有公開的函數、結構體和接口，以及詳細的使用示例和最佳實踐指南。

## 目錄

- [核心計算模組](#核心計算模組)
  - [最大平均值計算](#最大平均值計算)
  - [數據標準化](#數據標準化)
  - [階段分析](#階段分析)
- [I/O 操作](#io-操作)
  - [CSV 處理](#csv-處理)
  - [大文件處理](#大文件處理)
- [圖表生成](#圖表生成)
  - [基本圖表](#基本圖表)
  - [互動式圖表](#互動式圖表)
- [配置管理](#配置管理)
- [錯誤處理](#錯誤處理)
- [日誌記錄](#日誌記錄)
- [安全驗證](#安全驗證)
- [工具函數](#工具函數)

---

## 核心計算模組

### 最大平均值計算

#### MaxMeanCalculator

`MaxMeanCalculator` 提供滑動窗口最大平均值計算功能，是系統的核心計算元件。

```go
type MaxMeanCalculator struct {
    logger *logging.Logger
}
```

##### 方法

**NewMaxMeanCalculator**

```go
func NewMaxMeanCalculator() *MaxMeanCalculator
```

創建新的最大平均值計算器實例。

**示例：**
```go
calculator := calculator.NewMaxMeanCalculator()
```

**Calculate**

```go
func (c *MaxMeanCalculator) Calculate(dataset *models.EMGDataset, windowSize int) ([]models.MaxMeanResult, error)
```

計算指定窗口大小的最大平均值。

**參數：**
- `dataset` (*models.EMGDataset): EMG 數據集
- `windowSize` (int): 滑動窗口大小，範圍：1-10000，建議值：50-200

**返回值：**
- `[]models.MaxMeanResult`: 各通道的最大平均值結果
- `error`: 錯誤信息

**示例：**
```go
// 讀取 EMG 數據
csvHandler := io.NewCSVHandler()
dataset, err := csvHandler.ReadCSV("emg_data.csv")
if err != nil {
    log.Fatal(err)
}

// 計算最大平均值
calculator := calculator.NewMaxMeanCalculator()
results, err := calculator.Calculate(dataset, 100)
if err != nil {
    log.Fatal(err)
}

// 處理結果
for _, result := range results {
    fmt.Printf("通道 %d: 最大平均值 = %.6f, 時間範圍 = %.3f-%.3f\n",
        result.ColumnIndex, result.MaxMean, result.StartTime, result.EndTime)
}
```

**CalculateWithRange**

```go
func (c *MaxMeanCalculator) CalculateWithRange(dataset *models.EMGDataset, windowSize int, startTime, endTime float64) ([]models.MaxMeanResult, error)
```

計算指定時間範圍內的最大平均值。

**參數：**
- `dataset` (*models.EMGDataset): EMG 數據集
- `windowSize` (int): 滑動窗口大小，範圍：1-10000
- `startTime` (float64): 開始時間（秒），範圍：≥0
- `endTime` (float64): 結束時間（秒），範圍：>startTime

**示例：**
```go
// 計算特定時間範圍的最大平均值
results, err := calculator.CalculateWithRange(dataset, 100, 2.0, 5.0)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("時間範圍 2.0-5.0 秒內的最大平均值：\n")
for _, result := range results {
    fmt.Printf("通道 %d: %.6f\n", result.ColumnIndex, result.MaxMean)
}
```

**CalculateFromRawData**

```go
func (c *MaxMeanCalculator) CalculateFromRawData(rawData string, windowSize int) ([]models.MaxMeanResult, error)
```

從原始 CSV 字符串數據計算最大平均值。

**參數：**
- `rawData` (string): 原始 CSV 數據字符串
- `windowSize` (int): 滑動窗口大小

**示例：**
```go
csvData := `Time,Channel1,Channel2,Channel3
0.000,0.001,0.002,0.003
0.001,0.002,0.003,0.004
0.002,0.003,0.004,0.005`

results, err := calculator.CalculateFromRawData(csvData, 2)
if err != nil {
    log.Fatal(err)
}
```

---

### 數據標準化

#### Normalizer

`Normalizer` 提供數據標準化功能，支持多種標準化方法。

```go
type Normalizer struct {
    logger *logging.Logger
}
```

**NewNormalizer**

```go
func NewNormalizer() *Normalizer
```

**示例：**
```go
normalizer := calculator.NewNormalizer()
```

**Normalize**

```go
func (n *Normalizer) Normalize(dataset *models.EMGDataset, referenceValues []float64) (*models.EMGDataset, error)
```

標準化數據集，每個值除以對應的參考值。

**參數：**
- `dataset` (*models.EMGDataset): 原始數據集
- `referenceValues` ([]float64): 參考值陣列，長度必須與數據通道數相同

**示例：**
```go
// 使用 MVIC 值進行標準化
mvicValues := []float64{0.5, 0.6, 0.7} // 各通道的 MVIC 值

normalizedData, err := normalizer.Normalize(dataset, mvicValues)
if err != nil {
    log.Fatal(err)
}

// 保存標準化結果
csvHandler := io.NewCSVHandler()
err = csvHandler.WriteCSVToOutput(normalizedData, "normalized_data.csv")
if err != nil {
    log.Fatal(err)
}
```

**NormalizeFromRawData**

```go
func (n *Normalizer) NormalizeFromRawData(rawData string, referenceValues []float64) (*models.EMGDataset, error)
```

從原始 CSV 字符串數據進行標準化。

**示例：**
```go
csvData := `Time,Channel1,Channel2
0.000,0.1,0.2
0.001,0.2,0.3`

referenceValues := []float64{1.0, 1.5}
normalizedData, err := normalizer.NormalizeFromRawData(csvData, referenceValues)
```

---

### 階段分析

#### PhaseAnalyzer

`PhaseAnalyzer` 提供階段分析功能，可以分析不同階段的數據特徵。

```go
type PhaseAnalyzer struct {
    logger *logging.Logger
}
```

**NewPhaseAnalyzer**

```go
func NewPhaseAnalyzer() *PhaseAnalyzer
```

**Analyze**

```go
func (p *PhaseAnalyzer) Analyze(dataset *models.EMGDataset, phases []models.TimeRange, phaseLabels []string) ([]models.PhaseAnalysisResult, error)
```

分析不同階段的數據特徵。

**參數：**
- `dataset` (*models.EMGDataset): EMG 數據集
- `phases` ([]models.TimeRange): 階段時間範圍陣列
- `phaseLabels` ([]string): 階段標籤陣列

**示例：**
```go
// 定義階段
phases := []models.TimeRange{
    {Start: 0.0, End: 1.0},   // 準備階段
    {Start: 1.0, End: 3.0},   // 動作階段
    {Start: 3.0, End: 4.0},   // 恢復階段
}

phaseLabels := []string{"準備", "動作", "恢復"}

// 執行階段分析
analyzer := calculator.NewPhaseAnalyzer()
results, err := analyzer.Analyze(dataset, phases, phaseLabels)
if err != nil {
    log.Fatal(err)
}

// 顯示結果
for _, result := range results {
    fmt.Printf("階段：%s\n", result.PhaseName)
    fmt.Printf("  最大值：%v\n", result.MaxValues)
    fmt.Printf("  平均值：%v\n", result.MeanValues)
}
```

---

## I/O 操作

### CSV 處理

#### CSVHandler

`CSVHandler` 提供 CSV 文件讀寫功能，支持大文件自動處理。

```go
type CSVHandler struct {
    logger           *logging.Logger
    largeFileHandler *LargeFileHandler
    pathValidator    *security.PathValidator
}
```

**NewCSVHandler**

```go
func NewCSVHandler() *CSVHandler
```

**ReadCSV**

```go
func (h *CSVHandler) ReadCSV(filePath string) (*models.EMGDataset, error)
```

讀取 CSV 文件，自動檢測大文件並使用適當的處理方式。

**參數：**
- `filePath` (string): CSV 文件路徑

**示例：**
```go
handler := io.NewCSVHandler()

// 讀取 CSV 文件
dataset, err := handler.ReadCSV("data/emg_data.csv")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("讀取成功：%d 筆數據，%d 個通道\n", 
    len(dataset.Data), len(dataset.Headers)-1)
```

**WriteCSV**

```go
func (h *CSVHandler) WriteCSV(dataset *models.EMGDataset, filePath string) error
```

寫入 CSV 文件。

**參數：**
- `dataset` (*models.EMGDataset): 要寫入的數據集
- `filePath` (string): 輸出文件路徑

**示例：**
```go
// 寫入處理後的數據
err := handler.WriteCSV(processedData, "output/processed_data.csv")
if err != nil {
    log.Fatal(err)
}
```

**WriteCSVToOutput**

```go
func (h *CSVHandler) WriteCSVToOutput(dataset *models.EMGDataset, filename string) error
```

寫入 CSV 文件到預設輸出目錄。

**參數：**
- `dataset` (*models.EMGDataset): 數據集
- `filename` (string): 文件名稱

**示例：**
```go
// 寫入到輸出目錄
err := handler.WriteCSVToOutput(results, "max_mean_results.csv")
```

**ConvertMaxMeanResultsToCSV**

```go
func (h *CSVHandler) ConvertMaxMeanResultsToCSV(results []models.MaxMeanResult) (*models.EMGDataset, error)
```

將最大平均值結果轉換為 CSV 格式。

**示例：**
```go
// 轉換計算結果為 CSV 格式
csvData, err := handler.ConvertMaxMeanResultsToCSV(maxMeanResults)
if err != nil {
    log.Fatal(err)
}

// 保存結果
err = handler.WriteCSVToOutput(csvData, "max_mean_results.csv")
```

---

### 大文件處理

#### LargeFileHandler

`LargeFileHandler` 專門處理大型 CSV 文件，提供流式讀寫功能。

```go
type LargeFileHandler struct {
    logger        *logging.Logger
    maxMemoryUsage int64
    chunkSize     int
}
```

**NewLargeFileHandler**

```go
func NewLargeFileHandler(maxMemoryUsage int64, chunkSize int) *LargeFileHandler
```

**參數：**
- `maxMemoryUsage` (int64): 最大記憶體使用量（字節），建議值：500MB-2GB
- `chunkSize` (int): 處理塊大小，建議值：1000-10000

**示例：**
```go
// 創建大文件處理器（最大記憶體 1GB，塊大小 5000）
handler := io.NewLargeFileHandler(1024*1024*1024, 5000)
```

**ReadCSVStreaming**

```go
func (h *LargeFileHandler) ReadCSVStreaming(filePath string, callback func(chunk []models.EMGData) error) (*models.EMGDataset, error)
```

流式讀取大型 CSV 文件。

**參數：**
- `filePath` (string): CSV 文件路徑
- `callback` (func(chunk []models.EMGData) error): 數據塊處理回調函數

**示例：**
```go
var totalRecords int

// 定義數據塊處理回調
processChunk := func(chunk []models.EMGData) error {
    totalRecords += len(chunk)
    fmt.Printf("處理了 %d 筆數據，總計：%d\n", len(chunk), totalRecords)
    
    // 在這裡處理數據塊
    for _, data := range chunk {
        // 處理每筆數據
    }
    
    return nil
}

// 流式讀取大文件
dataset, err := handler.ReadCSVStreaming("large_file.csv", processChunk)
if err != nil {
    log.Fatal(err)
}
```

**WriteCSVStreaming**

```go
func (h *LargeFileHandler) WriteCSVStreaming(dataset *models.EMGDataset, filePath string, callback func(progress float64)) error
```

流式寫入大型 CSV 文件。

**示例：**
```go
// 定義進度回調
progressCallback := func(progress float64) {
    fmt.Printf("寫入進度：%.2f%%\n", progress*100)
}

// 流式寫入大文件
err := handler.WriteCSVStreaming(largeDataset, "output_large.csv", progressCallback)
```

---

## 圖表生成

### 基本圖表

#### ChartGenerator

`ChartGenerator` 提供基本的圖表生成功能，生成 PNG 格式的圖表。

```go
type ChartGenerator struct {
    logger *logging.Logger
}
```

**NewChartGenerator**

```go
func NewChartGenerator() *ChartGenerator
```

**GenerateLineChart**

```go
func (c *ChartGenerator) GenerateLineChart(dataset *models.EMGDataset, config ChartConfig, outputPath string) error
```

生成折線圖並保存為 PNG 文件。

**參數：**
- `dataset` (*models.EMGDataset): EMG 數據集
- `config` (ChartConfig): 圖表配置
- `outputPath` (string): 輸出文件路徑

**ChartConfig 結構：**
```go
type ChartConfig struct {
    Title      string      // 圖表標題
    XAxisLabel string      // X 軸標籤
    YAxisLabel string      // Y 軸標籤
    Width      vg.Length   // 圖表寬度
    Height     vg.Length   // 圖表高度
    Columns    []string    // 要繪製的通道名稱
}
```

**示例：**
```go
generator := chart.NewChartGenerator()

// 配置圖表
config := chart.ChartConfig{
    Title:      "EMG 數據分析圖表",
    XAxisLabel: "時間 (秒)",
    YAxisLabel: "EMG 值",
    Width:      vg.Points(800),
    Height:     vg.Points(600),
    Columns:    []string{"Channel1", "Channel2", "Channel3"},
}

// 生成圖表
err := generator.GenerateLineChart(dataset, config, "output/emg_chart.png")
if err != nil {
    log.Fatal(err)
}
```

**GenerateLineChartImage**

```go
func (c *ChartGenerator) GenerateLineChartImage(dataset *models.EMGDataset, config ChartConfig) (image.Image, error)
```

生成折線圖並返回圖像對象。

**示例：**
```go
// 生成圖像對象
img, err := generator.GenerateLineChartImage(dataset, config)
if err != nil {
    log.Fatal(err)
}

// 保存圖像
file, err := os.Create("chart.png")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

png.Encode(file, img)
```

---

### 互動式圖表

#### EChartsGenerator

`EChartsGenerator` 提供互動式圖表生成功能，生成 HTML 格式的圖表。

```go
type EChartsGenerator struct {
    logger *logging.Logger
}
```

**NewEChartsGenerator**

```go
func NewEChartsGenerator() *EChartsGenerator
```

**GenerateInteractiveChart**

```go
func (e *EChartsGenerator) GenerateInteractiveChart(dataset *models.EMGDataset, config InteractiveChartConfig, outputPath string) error
```

生成互動式圖表並保存為 HTML 文件。

**InteractiveChartConfig 結構：**
```go
type InteractiveChartConfig struct {
    Title           string   // 圖表標題
    XAxisLabel      string   // X 軸標籤
    YAxisLabel      string   // Y 軸標籤
    SelectedColumns []int    // 要顯示的通道索引
    ColumnNames     []string // 對應的通道名稱
    ShowAllColumns  bool     // 是否顯示所有通道
    Width           string   // 圖表寬度
    Height          string   // 圖表高度
}
```

**示例：**
```go
generator := chart.NewEChartsGenerator()

// 配置互動式圖表
config := chart.InteractiveChartConfig{
    Title:           "EMG 互動式數據分析",
    XAxisLabel:      "時間 (秒)",
    YAxisLabel:      "EMG 值",
    SelectedColumns: []int{1, 2, 3},
    ColumnNames:     []string{"右腿", "左腿", "腹部"},
    ShowAllColumns:  false,
    Width:           "1200px",
    Height:          "800px",
}

// 生成互動式圖表
err := generator.GenerateInteractiveChart(dataset, config, "output/interactive_chart.html")
if err != nil {
    log.Fatal(err)
}
```

**GetAvailableColumns**

```go
func (e *EChartsGenerator) GetAvailableColumns(dataset *models.EMGDataset) []ColumnInfo
```

獲取數據集的可用通道信息。

**ColumnInfo 結構：**
```go
type ColumnInfo struct {
    Index      int     `json:"index"`      // 通道索引
    Name       string  `json:"name"`       // 通道名稱
    DataPoints int     `json:"dataPoints"` // 數據點數
    Min        float64 `json:"min"`        // 最小值
    Max        float64 `json:"max"`        // 最大值
    Mean       float64 `json:"mean"`       // 平均值
}
```

**示例：**
```go
// 獲取通道信息
columns := generator.GetAvailableColumns(dataset)

fmt.Println("可用通道：")
for _, col := range columns {
    fmt.Printf("  %s: %d 個數據點，範圍 %.6f - %.6f，平均值 %.6f\n",
        col.Name, col.DataPoints, col.Min, col.Max, col.Mean)
}
```

**BatchExportCharts**

```go
func (e *EChartsGenerator) BatchExportCharts(dataset *models.EMGDataset, columnGroups [][]int, baseConfig InteractiveChartConfig, outputDir string) error
```

批量導出多個圖表。

**示例：**
```go
// 定義通道組合
columnGroups := [][]int{
    {1, 2},    // 腿部肌群
    {3, 4},    // 腹部肌群
    {5, 6},    // 背部肌群
}

// 批量導出
err := generator.BatchExportCharts(dataset, columnGroups, baseConfig, "output/charts/")
```

---

## 配置管理

### AppConfig

`AppConfig` 提供應用程序配置管理功能。

```go
type AppConfig struct {
    ScalingFactor      int      `json:"scaling_factor"`       // 縮放因子
    WindowSize         int      `json:"window_size"`          // 視窗大小
    InputDirectory     string   `json:"input_directory"`      // 輸入目錄
    OutputDirectory    string   `json:"output_directory"`     // 輸出目錄
    PhaseLabels        []string `json:"phase_labels"`         // 階段標籤
    MaxMemoryUsage     int64    `json:"max_memory_usage"`     // 最大記憶體使用量
    ChunkSize          int      `json:"chunk_size"`           // 塊大小
    LogLevel           string   `json:"log_level"`            // 日誌級別
    EnableGUI          bool     `json:"enable_gui"`           // 啟用 GUI
    Language           string   `json:"language"`             // 語言設定
}
```

**LoadConfig**

```go
func LoadConfig(configPath string) (*AppConfig, error)
```

從配置文件加載配置。

**示例：**
```go
// 加載配置
config, err := config.LoadConfig("config.json")
if err != nil {
    log.Fatal(err)
}

// 使用配置
fmt.Printf("輸入目錄：%s\n", config.InputDirectory)
fmt.Printf("輸出目錄：%s\n", config.OutputDirectory)
fmt.Printf("視窗大小：%d\n", config.WindowSize)
```

**SaveConfig**

```go
func (c *AppConfig) SaveConfig(configPath string) error
```

保存配置到文件。

**示例：**
```go
// 修改配置
config.WindowSize = 100
config.ScalingFactor = 1000

// 保存配置
err := config.SaveConfig("config.json")
if err != nil {
    log.Fatal(err)
}
```

**Validate**

```go
func (c *AppConfig) Validate() error
```

驗證配置的有效性。

**示例：**
```go
// 驗證配置
if err := config.Validate(); err != nil {
    log.Printf("配置驗證失敗：%v", err)
    return err
}
```

---

## 錯誤處理

### 錯誤類型

系統定義了多種錯誤類型以提供詳細的錯誤信息。

#### AppError

```go
type AppError struct {
    Code    ErrorCode              `json:"code"`
    Message string                 `json:"message"`
    Cause   error                  `json:"cause,omitempty"`
    Context map[string]interface{} `json:"context,omitempty"`
}
```

**ErrorCode 類型：**
```go
const (
    ErrFileNotFound     ErrorCode = "FILE_NOT_FOUND"
    ErrInvalidFormat    ErrorCode = "INVALID_FORMAT"
    ErrPermissionDenied ErrorCode = "PERMISSION_DENIED"
    ErrMemoryLimit      ErrorCode = "MEMORY_LIMIT"
    ErrInvalidInput     ErrorCode = "INVALID_INPUT"
    ErrProcessingFailed ErrorCode = "PROCESSING_FAILED"
)
```

**示例：**
```go
// 創建應用程序錯誤
err := &errors.AppError{
    Code:    errors.ErrFileNotFound,
    Message: "找不到指定的 CSV 文件",
    Context: map[string]interface{}{
        "file_path": filePath,
        "operation": "read_csv",
    },
}

// 檢查錯誤類型
if appErr, ok := err.(*errors.AppError); ok {
    switch appErr.Code {
    case errors.ErrFileNotFound:
        // 處理文件不存在錯誤
    case errors.ErrInvalidFormat:
        // 處理格式錯誤
    }
}
```

#### ValidationError

```go
type ValidationError struct {
    Field   string `json:"field"`
    Message string `json:"message"`
    Value   string `json:"value,omitempty"`
}
```

**示例：**
```go
// 創建驗證錯誤
err := &errors.ValidationError{
    Field:   "window_size",
    Message: "視窗大小必須在 1-10000 之間",
    Value:   "15000",
}

fmt.Printf("驗證錯誤：%v\n", err)
```

**IsRecoverable**

```go
func IsRecoverable(err error) bool
```

判斷錯誤是否可恢復。

**示例：**
```go
if err := processFile(filePath); err != nil {
    if errors.IsRecoverable(err) {
        // 嘗試恢復操作
        log.Printf("錯誤可恢復，嘗試重試：%v", err)
    } else {
        // 嚴重錯誤，停止處理
        log.Fatal("不可恢復的錯誤：", err)
    }
}
```

---

## 日誌記錄

### Logger

`Logger` 提供結構化日誌記錄功能。

```go
type Logger struct {
    module string
    level  LogLevel
    writer io.Writer
}
```

**LogLevel 類型：**
```go
const (
    LogLevelDebug LogLevel = iota
    LogLevelInfo
    LogLevelWarn
    LogLevelError
    LogLevelFatal
)
```

**GetLogger**

```go
func GetLogger(module string) *Logger
```

獲取指定模組的日誌記錄器。

**示例：**
```go
// 獲取模組日誌記錄器
logger := logging.GetLogger("calculator")

// 記錄不同級別的日誌
logger.Debug("開始計算過程", map[string]interface{}{
    "window_size": 100,
    "data_points": 1000,
})

logger.Info("計算完成", map[string]interface{}{
    "results_count": 5,
    "duration": "2.5s",
})

logger.Warn("記憶體使用率較高", map[string]interface{}{
    "usage_percent": 85,
    "threshold": 80,
})

logger.Error("計算失敗", err, map[string]interface{}{
    "file_path": filePath,
    "operation": "max_mean_calculation",
})
```

**WithContext**

```go
func (l *Logger) WithContext(context map[string]interface{}) *Logger
```

為日誌記錄器添加上下文信息。

**示例：**
```go
// 添加上下文信息
contextLogger := logger.WithContext(map[string]interface{}{
    "user_id": "user123",
    "session_id": "session456",
})

// 使用帶上下文的日誌記錄器
contextLogger.Info("用戶操作", map[string]interface{}{
    "action": "calculate_max_mean",
    "file_name": "data.csv",
})
```

---

## 安全驗證

### PathValidator

`PathValidator` 提供路徑安全驗證功能。

```go
type PathValidator struct {
    allowedDirectories []string
    maxPathLength      int
}
```

**NewPathValidator**

```go
func NewPathValidator(allowedDirectories []string) *PathValidator
```

**示例：**
```go
// 創建路徑驗證器
validator := security.NewPathValidator([]string{
    "/app/input",
    "/app/output",
    "/app/config",
})
```

**ValidateFilePath**

```go
func (v *PathValidator) ValidateFilePath(filePath string) error
```

驗證文件路徑是否安全。

**示例：**
```go
// 驗證文件路徑
if err := validator.ValidateFilePath(userInputPath); err != nil {
    log.Printf("路徑驗證失敗：%v", err)
    return err
}

// 路徑安全，可以繼續處理
```

**SanitizePath**

```go
func (v *PathValidator) SanitizePath(path string) string
```

清理路徑中的危險字符。

**示例：**
```go
// 清理用戶輸入的路徑
safePath := validator.SanitizePath(userInputPath)
```

### InputValidator

`InputValidator` 提供輸入驗證功能。

```go
type InputValidator struct {
    maxFileSize        int64
    allowedExtensions  []string
}
```

**NewInputValidator**

```go
func NewInputValidator() *InputValidator
```

**ValidateCSVData**

```go
func (v *InputValidator) ValidateCSVData(dataset *models.EMGDataset) error
```

驗證 CSV 數據的有效性。

**示例：**
```go
validator := validation.NewInputValidator()

// 驗證 CSV 數據
if err := validator.ValidateCSVData(dataset); err != nil {
    log.Printf("數據驗證失敗：%v", err)
    return err
}
```

**ValidateWindowSize**

```go
func (v *InputValidator) ValidateWindowSize(windowSize int) error
```

驗證視窗大小參數。

**示例：**
```go
// 驗證視窗大小
if err := validator.ValidateWindowSize(windowSize); err != nil {
    return fmt.Errorf("視窗大小無效：%w", err)
}
```

---

## 工具函數

### 數學計算

**ArrayMean**

```go
func ArrayMean(arr []float64) float64
```

計算陣列的平均值。

**示例：**
```go
data := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
mean := util.ArrayMean(data)
fmt.Printf("平均值：%.2f\n", mean) // 輸出：平均值：3.00
```

**ArrayMax**

```go
func ArrayMax(arr []float64) float64
```

找出陣列的最大值。

**示例：**
```go
data := []float64{1.0, 5.0, 3.0, 2.0, 4.0}
max := util.ArrayMax(data)
fmt.Printf("最大值：%.2f\n", max) // 輸出：最大值：5.00
```

**Str2Number**

```go
func Str2Number(str string) (float64, error)
```

將字符串轉換為數字。

**示例：**
```go
// 轉換字符串為數字
value, err := util.Str2Number("123.456")
if err != nil {
    log.Printf("轉換失敗：%v", err)
} else {
    fmt.Printf("轉換結果：%.3f\n", value)
}
```

---

## 性能優化建議

### 記憶體管理

1. **使用合適的塊大小**
   ```go
   // 對於大文件，使用較大的塊大小
   handler := io.NewLargeFileHandler(1024*1024*1024, 10000)
   ```

2. **及時釋放資源**
   ```go
   defer file.Close()
   ```

3. **監控記憶體使用**
   ```go
   // 在處理大文件時監控記憶體
   runtime.GC()
   ```

### 並行處理

1. **使用 goroutine 進行並行計算**
   ```go
   // 並行處理多個通道
   var wg sync.WaitGroup
   for i, channel := range channels {
       wg.Add(1)
       go func(idx int, ch []float64) {
           defer wg.Done()
           // 處理通道數據
       }(i, channel)
   }
   wg.Wait()
   ```

### 錯誤處理

1. **使用結構化錯誤**
   ```go
   if err != nil {
       return &errors.AppError{
           Code:    errors.ErrProcessingFailed,
           Message: "處理失敗",
           Cause:   err,
       }
   }
   ```

2. **記錄詳細的錯誤信息**
   ```go
   logger.Error("操作失敗", err, map[string]interface{}{
       "operation": "calculate_max_mean",
       "file_path": filePath,
   })
   ```

---

## 常見問題

### Q: 如何處理大文件？
A: 使用 `LargeFileHandler` 進行流式處理：
```go
handler := io.NewLargeFileHandler(1024*1024*1024, 5000)
dataset, err := handler.ReadCSVStreaming(filePath, processChunk)
```

### Q: 如何自定義圖表樣式？
A: 使用 `ChartConfig` 或 `InteractiveChartConfig` 進行配置：
```go
config := chart.ChartConfig{
    Title:      "自定義標題",
    XAxisLabel: "時間",
    YAxisLabel: "值",
    Width:      vg.Points(1200),
    Height:     vg.Points(800),
}
```

### Q: 如何處理多語言支持？
A: 使用 `i18n` 模組：
```go
i18n.InitI18n()
i18n.SetLocale("zh-TW")
message := i18n.T("error.file_not_found")
```

---

## 更新日誌

### v1.0.0 (2025-07-16)
- 完整的 API 文檔
- 詳細的使用示例
- 參數說明和類型定義
- 錯誤處理指南
- 性能優化建議

---

本文檔將持續更新，如有問題請參考源代碼或聯繫開發團隊。