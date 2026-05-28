# EMG 數據分析工具 API 文檔

## 概述

本文檔提供 EMG 數據分析工具的完整 API 參考，包含所有公開的函數、結構體和接口，以及詳細的使用示例和最佳實踐指南。

## 目錄

- [核心計算模組](#核心計算模組)
  - [最大平均值計算](#最大平均值計算)
  - [數據標準化](#數據標準化)
  - [階段分析](#階段分析)
  - [CCI 共同收縮分析](#cci-共同收縮分析)
- [資料解析](#資料解析)
  - [parsers.DataParser](#parsersdataparser)
- [I/O 操作](#io-操作)
  - [CSV 處理](#csv-處理)
  - [大文件處理](#大文件處理)
- [圖表生成](#圖表生成)
- [配置管理](#配置管理)
- [錯誤處理](#錯誤處理)
- [日誌記錄](#日誌記錄)
- [安全驗證](#安全驗證)
- [工具函數](#工具函數)

---

## 核心計算模組

### 最大平均值計算

#### MaxMeanCalculator

`MaxMeanCalculator` 提供滑動窗口最大平均值計算功能，是系統的核心計算元件。內部使用 goroutine worker pool + backpressure 控制；長時間執行的計算可透過 `context.Context` 取消。

`MaxMeanCalculator` 為**不透明結構體** — caller 只應透過下列 method 互動，不要依賴內部欄位佈局或型別（未來可能調整 worker pool / backpressure 實作而不影響 public API）。

##### 方法

**NewMaxMeanCalculator**

```go
func NewMaxMeanCalculator(scalingFactor int) *MaxMeanCalculator
```

創建新的最大平均值計算器實例。`scalingFactor` 控制時間軸的精度倍率（與 `AppConfig.ScalingFactor` 一致，用於將秒換算為微秒比較）。

**示例：**
```go
cfg := config.DefaultConfig()
calc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
```

**Calculate**

```go
func (c *MaxMeanCalculator) Calculate(
    ctx context.Context,
    dataset *models.EMGDataset,
    windowSize int,
) ([]models.MaxMeanResult, error)
```

計算指定窗口大小的最大平均值。

**參數：**
- `ctx` (context.Context): 取消計算用；GUI/CLI 應傳入可取消的 context，不確定來源時傳 `context.Background()`
- `dataset` (*models.EMGDataset): EMG 數據集
- `windowSize` (int): 滑動窗口大小，範圍：1-10000，建議值：50-200

**返回值：**
- `[]models.MaxMeanResult`: 各通道的最大平均值結果
- `error`: 錯誤信息；若 ctx 被取消會回傳 `context.Canceled`

**示例：**
```go
// 取得設定並建立 handler / calculator
cfg, _ := config.LoadConfig("./config.json")
csvHandler := io.NewCSVHandler(cfg)
calc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)

// 讀取原始 CSV 為 [][]string
records, err := csvHandler.ReadCSV("emg_data.csv")
if err != nil {
    log.Fatal(err)
}

// 透過 CalculateFromRawData（內部會用 DataParser 解析）
results, err := calc.CalculateFromRawData(context.Background(), records, 100)
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
func (c *MaxMeanCalculator) CalculateWithRange(
    ctx context.Context,
    dataset *models.EMGDataset,
    windowSize int,
    startRange, endRange float64,
) ([]models.MaxMeanResult, error)
```

計算指定時間範圍內的最大平均值。`startRange == 0 && endRange == 0` 表示使用整段資料。

**參數：**
- `ctx` (context.Context): 取消用 context
- `dataset` (*models.EMGDataset): EMG 數據集
- `windowSize` (int): 滑動窗口大小，範圍：1-10000
- `startRange` (float64): 開始時間（秒），範圍：≥0
- `endRange` (float64): 結束時間（秒），範圍：>startRange；0 表示不限

**示例：**
```go
// 計算特定時間範圍的最大平均值
results, err := calc.CalculateWithRange(context.Background(), dataset, 100, 2.0, 5.0)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("時間範圍 2.0-5.0 秒內的最大平均值：\n")
for _, result := range results {
    fmt.Printf("通道 %d: %.6f\n", result.ColumnIndex, result.MaxMean)
}
```

**CalculateFromRawData / CalculateFromRawDataWithRange**

```go
func (c *MaxMeanCalculator) CalculateFromRawData(
    ctx context.Context,
    records [][]string,
    windowSize int,
) ([]models.MaxMeanResult, error)

func (c *MaxMeanCalculator) CalculateFromRawDataWithRange(
    ctx context.Context,
    records [][]string,
    windowSize int,
    startRange, endRange float64,
) ([]models.MaxMeanResult, error)
```

從 CSV 原始字串資料計算最大平均值。內部會呼叫 `parsers.DataParser` 解析。

**參數：**
- `ctx` (context.Context): 取消用 context
- `records` ([][]string): 已解析的 CSV 列／欄陣列（第 0 列為標題列）
- `windowSize` (int): 滑動窗口大小

**示例：**
```go
records := [][]string{
    {"Time", "Channel1", "Channel2", "Channel3"},
    {"0.000", "0.001", "0.002", "0.003"},
    {"0.001", "0.002", "0.003", "0.004"},
    {"0.002", "0.003", "0.004", "0.005"},
}

results, err := calc.CalculateFromRawData(context.Background(), records, 2)
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
csvHandler := io.NewCSVHandler(cfg)
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

### CCI 共同收縮分析

`internal/cci` 提供 Rudolph 共同收縮指數（CCI）分析，計算 12 對肌肉的時間序列 CCI 並產出互動圖表。

**CalculateCCIRudolph**

```go
func CalculateCCIRudolph(emg1, emg2 float64) float64
```

單一時間點的 CCI Rudolph 公式：`CCI = (EMG_s / EMG_l) * (EMG_s + EMG_l)`，其中 `EMG_s` 為較小值、`EMG_l` 為較大值。**輸入消毒**：NaN / ±Inf / 負值會回傳 `math.NaN()`（公式假設 rectified EMG），下游 writer 可偵測並拒絕。

**Analyzer.Analyze**

```go
func (a *Analyzer) Analyze(
    emgData *models.PhaseSyncEMGData,
    pairs []MusclePair,
) (*CCIAnalysisResult, error)
```

接受 phase-sync 後的 EMG 資料與肌肉對清單，使用 errgroup 並行計算每對 CCI 時間序列，並產出步態週期內的 mean curve。

**Analyzer.ExportToCSV**

```go
func (a *Analyzer) ExportToCSV(result *CCIAnalysisResult, outputDir string) (string, error)
```

匯出時會檢查 `GaitEndTime > GaitStartTime`，否則回傳「無效的步態週期區間」錯誤，避免 NaN/Inf 寫入 `Gait Cycle (%)` 欄位。

**示例：**
```go
analyzer := cci.NewCCIAnalyzer()
result, err := analyzer.Analyze(phaseSyncData, cci.DefaultMusclePairs())
if err != nil {
    log.Fatal(err)
}

outputPath, err := analyzer.ExportToCSV(result, cfg.OutputDir)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("CCI 結果已輸出到 %s\n", outputPath)
```

---

## 資料解析

### parsers.DataParser

`internal/parsers.DataParser` 是 CSV / EMG / Motion / ANC 格式的統一解析入口，將原始 `[][]string` 或檔案內容轉換成 `*models.EMGDataset`。`MaxMeanCalculator.CalculateFromRawData` 內部即透過 DataParser 解析。

```go
func NewDataParser(scalingFactor int) *DataParser
func NewDataParserWithLogger(scalingFactor int, logger *logging.Logger) *DataParser

func (p *DataParser) ParseRawData(records [][]string) (*models.EMGDataset, error)
```

**參數：**
- `scalingFactor` (int): 與 `AppConfig.ScalingFactor` 一致，用於時間軸縮放
- `records` ([][]string): 標題列 + 資料列

**示例：**
```go
parser := parsers.NewDataParser(cfg.ScalingFactor)
dataset, err := parser.ParseRawData(records)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("解析成功：%d 筆 row, %d 個 channel\n",
    len(dataset.Data), len(dataset.Data[0].Channels))
```

`parsers` 套件另提供 `EMGParser`、`ANCParser`、`MotionParser`、`PhaseManifestParser` 等格式專用 reader；共用的工具（`ParseFloatCell`、`ValidateTimeSeries[T]`、`FindTimeRangeIndices`）位於 `parse_helpers.go`。

---

## I/O 操作

### CSV 處理

#### CSVHandler

`CSVHandler` 提供 CSV 文件讀寫功能，內建路徑驗證、BOM 支援與 symlink 拒絕（`O_NOFOLLOW`）。

```go
type CSVHandler struct {
    // 內部欄位：config、logger、pathValidator、largeFileHandler
}
```

**NewCSVHandler**

```go
func NewCSVHandler(cfg *config.AppConfig) *CSVHandler
```

建立 CSV handler。`cfg` 提供 InputDir / OutputDir / OperateDir 路徑邊界與 BOMEnabled 設定。

**ReadCSV**

```go
func (h *CSVHandler) ReadCSV(filename string) ([][]string, error)
```

讀取 CSV 文件並回傳原始列／欄陣列（**未解析**為 EMGDataset）。`filename` 會被 PathValidator 合併到 InputDir 之下，攻擊者無法用 `../` 跳出。要解析成 EMGDataset 請串接 `parsers.DataParser.ParseRawData`。

**參數：**
- `filename` (string): CSV 檔名（相對 InputDir）

**示例：**
```go
cfg, _ := config.LoadConfig("./config.json")
handler := io.NewCSVHandler(cfg)

// 讀取 CSV 原始資料
records, err := handler.ReadCSV("emg_data.csv")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("讀取成功：%d 筆 row\n", len(records))
```

**WriteCSV / WriteCSVToOutput**

```go
func (h *CSVHandler) WriteCSV(filename string, data [][]string) (err error)
func (h *CSVHandler) WriteCSVToOutput(filename string, data [][]string) error
```

`WriteCSV` 寫入指定路徑（已通過 PathValidator）；`WriteCSVToOutput` 自動寫入 `cfg.OutputDir`。兩者皆採 `O_NOFOLLOW` 拒絕 symlink-swap，並在 `BOMEnabled` 時補上 UTF-8 BOM。`WriteCSV` 採 named return，能傳播 `file.Close()` 錯誤（NFS 延遲寫入失敗才會出現）。

**示例：**
```go
// 寫入計算結果到 OutputDir
output := handler.ConvertMaxMeanResultsToCSV(records[0], results, 0, 0)
if err := handler.WriteCSVToOutput("max_mean_results.csv", output); err != nil {
    log.Fatal(err)
}
```

**ConvertMaxMeanResultsToCSV**

```go
func (h *CSVHandler) ConvertMaxMeanResultsToCSV(
    headers []string,
    results []models.MaxMeanResult,
    startRange, endRange float64,
) [][]string
```

將最大平均值結果合併原始 headers 轉成可寫入的 `[][]string`（每列：通道名、MaxMean、StartTime、EndTime；最後一列附範圍註記）。

---

### 大文件處理

#### LargeFileHandler

`LargeFileHandler` 專門處理大型 CSV 文件，提供串流式滑動窗口分塊計算。為**不透明結構體** — caller 只應透過下列 method 互動。

**NewLargeFileHandler**

```go
func NewLargeFileHandler(cfg *config.AppConfig) *LargeFileHandler
```

**參數：**
- `cfg` (*config.AppConfig): 應用配置。內部會根據 `cfg.InputDir`/`OutputDir`/`OperateDir` 建立路徑驗證白名單；記憶體上限與分塊大小由 handler 內部以工程經驗預設（記憶體限制 512 MB、`chunkSize=1000` 同時控制 progress 報告頻率），caller 無需指定。

**示例：**
```go
cfg := config.DefaultConfig()
handler := io.NewLargeFileHandler(cfg)
```

**ProcessLargeFileInChunks**

```go
func (h *LargeFileHandler) ProcessLargeFileInChunks(
    filename string,
    windowSize int,
    callback ProgressCallback,
) (*StreamingResult, error)
```

對大型 CSV 串流執行滑動窗口最大平均值計算 — 一次只在記憶體保留窗口大小的 ring buffer，配合 backpressure 在記憶體壓力下中止。`callback` 型別為 `ProgressCallback = func(processed, total int64, percentage float64)`，每 `chunkSize` 筆觸發一次（預設 1000；可傳 `nil` 跳過進度回報）。

`StreamingResult` 含完成統計（`ProcessedLines`、`Duration`、每通道 `Results`、`Headers`、`MemoryUsed` 等）；錯誤通常為 `errors.AppError` 包裝（路徑驗證失敗、記憶體爆量、CSV 格式錯誤等）。

**示例：**
```go
cfg := config.DefaultConfig()
handler := io.NewLargeFileHandler(cfg)

progressCallback := func(processed, total int64, percentage float64) {
    fmt.Printf("進度：%d / %d (%.2f%%)\n", processed, total, percentage)
}

result, err := handler.ProcessLargeFileInChunks("large_file.csv", 500, progressCallback)
if err != nil {
    log.Fatal(err)
}
fmt.Printf("處理 %d 筆，耗時 %s，產出 %d 通道結果\n",
    result.ProcessedLines, result.Duration, len(result.Results))
```

---

## 圖表生成

> **已移除：基於 gonum/plot 的 PNG 圖表生成器**
>
> `chart.ChartGenerator` / `GenerateLineChart` / `GenerateLineChartImage` 與
> 整個 `internal/chart/chart.go`（含 `gonum.org/v1/plot` 依賴）在 Wave 4 PR3
> (commit `f0ce17f`) 刪除 — 無 production caller，僅 test 自相依賴。
>

---

## 配置管理

### AppConfig

`AppConfig` 提供應用程序配置管理功能。

```go
type AppConfig struct {
    ScalingFactor   int      `json:"scalingFactor"`   // 縮放因子
    PhaseLabels     []string `json:"phaseLabels"`     // 階段標籤（中文）
    Precision       int      `json:"precision"`       // 數值精度 (0-15)
    OutputFormat    string   `json:"outputFormat"`    // 輸出格式："csv"
    BOMEnabled      bool     `json:"bomEnabled"`      // 寫入時是否前綴 UTF-8 BOM
    InputDir        string   `json:"inputDir"`        // 輸入目錄
    OutputDir       string   `json:"outputDir"`       // 輸出目錄
    OperateDir      string   `json:"operateDir"`      // 操作目錄
    LogLevel        string   `json:"logLevel"`        // debug, info, warn, error
    LogFormat       string   `json:"logFormat"`       // text, json
    LogDirectory    string   `json:"logDirectory"`    // 日誌目錄
    Language        string   `json:"language"`        // zh-TW, zh-CN, en-US, ja-JP
    TranslationsDir string   `json:"translationsDir"` // 翻譯文件目錄
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
fmt.Printf("輸入目錄：%s\n", config.InputDir)
fmt.Printf("輸出目錄：%s\n", config.OutputDir)
fmt.Printf("精度：%d\n", config.Precision)
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

**ErrorCode 常數（節錄，完整列表見 `internal/errors/errors.go`）：**
```go
const (
    ErrCodeFileNotFound    ErrorCode = "FILE_NOT_FOUND"
    ErrCodeFilePermission  ErrorCode = "FILE_PERMISSION"
    ErrCodeFileFormat      ErrorCode = "FILE_FORMAT"
    ErrCodePathValidation  ErrorCode = "PATH_VALIDATION"
    ErrCodeFileTooLarge    ErrorCode = "FILE_TOO_LARGE"
    ErrCodeDataParsing     ErrorCode = "DATA_PARSING"
    ErrCodeDataValidation  ErrorCode = "DATA_VALIDATION"
    ErrCodeCalculation     ErrorCode = "CALCULATION"
    ErrCodeConfigValidation ErrorCode = "CONFIG_VALIDATION"
)
```

**示例：**
```go
// 使用 constructor 而非直接 struct literal — Recoverable 旗標會根據 ErrCode 自動推斷。
err := errors.NewAppError(errors.ErrCodeFileNotFound, "找不到指定的 CSV 文件")

// 若需附加 context 細節，用 NewAppErrorWithDetails。
err = errors.NewAppErrorWithDetails(
    errors.ErrCodeFileNotFound,
    "找不到指定的 CSV 文件",
    fmt.Sprintf("file_path=%s operation=read_csv", filePath),
)

// 檢查錯誤類型
if appErr, ok := err.(*errors.AppError); ok {
    switch appErr.Code {
    case errors.ErrCodeFileNotFound:
        // 處理文件不存在錯誤
    case errors.ErrCodeFileFormat:
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

內部結構（`mu sync.RWMutex; allowedBasePaths []string`）受 RWMutex 保護，可在不同 goroutine 之間安全併用：呼叫者可在某 goroutine 透過 `SetAllowedBasePaths` 變更白名單，同時其他 goroutine 透過 `ValidateFilePath` 讀取（commit `1287572` 修補的 race condition）。

**NewPathValidator**

```go
func NewPathValidator(allowedBasePaths []string) *PathValidator
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
func (v *InputValidator) ValidateCSVData(records [][]string, filename string) error
```

驗證 CSV 原始記錄結構與檢查惡意內容（公式注入、binary smuggling 等）。傳入已解析的 `[][]string` 與來源檔名（後者用於錯誤訊息與 audit log）。

**示例：**
```go
validator := validation.NewInputValidator()

records, err := csvHandler.ReadCSV(filePath)
if err != nil { return err }

if err := validator.ValidateCSVData(records, filePath); err != nil {
    log.Printf("數據驗證失敗：%v", err)
    return err
}
```

**ValidateWindowSize**

```go
func (v *InputValidator) ValidateWindowSize(windowSizeStr string) (int, error)
```

驗證來自前端字串形式的視窗大小參數，回傳已解析的 `int` 與錯誤；接受字串是為了讓 Wails RPC 傳遞 raw user input 後在後端統一驗證/解析。

**示例：**
```go
// 從前端傳入字串
windowSize, err := validator.ValidateWindowSize(params.WindowSizeStr)
if err != nil {
    return fmt.Errorf("視窗大小無效：%w", err)
}
// 使用解析後的 int
results, err := calculator.Calculate(ctx, dataset, windowSize)
```

---

## 安全寫入工具

### internal/csvutil — BOM 與 formula-injection 防禦

`internal/csvutil` 提供 CSV 寫入端的兩類保護：UTF-8 BOM 統一處理，以及防止 spreadsheet（Excel / LibreOffice / Numbers）將 attacker-controlled cell 內容解釋為公式。

**SanitizeCellForWrite**

```go
func SanitizeCellForWrite(cell string) string
```

對單一 cell escape — 以 `'` 前綴中和開頭為 `=`、`@`、或非數值 `+/-` 開頭的 cell。對 `+1.5`、`-3` 等合法數值不動。Trim 後檢測，避免 attacker 用前置空白/tab 繞過。

**SanitizeHeaderRow**

```go
func SanitizeHeaderRow(row []string) []string
```

對單列每 cell 跑 `SanitizeCellForWrite`，回傳新 slice。Headers 是最高風險面（user 上傳的 CSV header 會 round-trip 進匯出檔）。

**SanitizeAllRows**

```go
func SanitizeAllRows(rows [][]string) [][]string
```

`csv_handler.WriteCSV` 內部用此函式作為單一 chokepoint，保證**所有 cell（header + body）**都會被 sanitize，堵住 `result.PhaseName` 等 user-controllable 標籤的 formula-injection 路徑。`SanitizeCellForWrite` 是 idempotent，已 escape 的 cell 不會被二次處理。

**PeekBOM / WriteBOM**

```go
func PeekBOM(r *bufio.Reader) (bool, error)
func WriteBOM(w io.Writer) error
```

`PeekBOM` 不消費 reader 即可探測是否有 UTF-8 BOM（讀端用）；`WriteBOM` 寫入 BOM 三 bytes（寫端用，與 `AppConfig.BOMEnabled` 配合）。

### internal/security/fsperm — 檔案權限與 O_NOFOLLOW

`internal/security/fsperm` 集中化檔案操作的權限與 flag 常數，是 single-source-of-truth：

```go
const FilePerm = 0o600         // 應用程式建立檔案的標準權限（owner-only）
const DirPerm  = 0o750         // 目錄權限（owner full + group read/exec）
var   WriteFlags  = os.O_WRONLY | os.O_CREATE | os.O_TRUNC | unix.O_NOFOLLOW
var   AppendFlags = os.O_WRONLY | os.O_CREATE | os.O_APPEND | unix.O_NOFOLLOW
var   ReadFlags   = os.O_RDONLY | unix.O_NOFOLLOW
```

`O_NOFOLLOW` 在 unix 由 OS 拒絕 symlink 開檔（symmetric 保護讀寫兩端）；Windows build tag 下 fallback 為純基本 flag（Windows ACL 不依 O_NOFOLLOW）。

**WriteFileNoFollow**

```go
func WriteFileNoFollow(path string, data []byte) error
```

一次性整檔寫入 helper（PNG 圖片、i18n JSON 等不需 streaming 的場景）。串流寫入（CSV）仍走 `os.OpenFile(path, fsperm.WriteFlags, fsperm.FilePerm)` pattern。

### 通用工具函數

#### 數學計算

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

1. **使用 `LargeFileHandler` 處理大檔**
   ```go
   // chunk size、memory limit 等內部以工程經驗預設，caller 只傳 cfg。
   cfg := config.DefaultConfig()
   handler := io.NewLargeFileHandler(cfg)
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

1. **使用結構化錯誤（建議用 constructor，Recoverable 自動由 ErrCode 推斷）**
   ```go
   if err != nil {
       return errors.WrapError(err, errors.ErrCodeCalculation, "處理失敗")
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
A: 透過 `LargeFileHandler.ProcessLargeFileInChunks` 進行流式滑動窗口計算：
```go
handler := io.NewLargeFileHandler(cfg)
result, err := handler.ProcessLargeFileInChunks(filePath, windowSize, progressCallback)
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