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

創建新的最大平均值計算器實例。`scalingFactor` 控制時間軸的精度倍率（與 `AppConfig.ScalingFactor` 一致，用於縮放域換算：時間值經 `Str2Number` 乘 10^scalingFactor，使用者輸入的秒由 `calculator.UnitScale` 轉入同一域後再比較，見 ADR-0049）。

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

**CalculateInRange**

```go
type MaxMeanRange struct{ Start, End *float64 } // 原域秒；nil = 未給

type MaxMeanRun struct {
    Results    []models.MaxMeanResult
    Start, End float64 // 實際採用的範圍（原域秒）
}

func (c *MaxMeanCalculator) CalculateInRange(
    ctx context.Context,
    dataset *models.EMGDataset,
    windowSize int,
    r MaxMeanRange,
) (*MaxMeanRun, error)
```

計算時間範圍 `r` 內的最大平均值。範圍兩端各自可省略（ADR-0050）：

- `Start == nil`：從資料首樣本起算；`End == nil`：算到資料末樣本；兩端皆 nil = 整段資料。
- `0` 是合法邊界值（時間軸可從負值開始），不代表「未給」。兩端都給 `0` 就是 `[0, 0]`，資料不足時回 `ErrInvalidTimeRange`。
- `MaxMeanRun.Start/End`：有給的那端是使用者的值；沒給的那端是實際首 / 末樣本的時間（`UnitScale.FromScaled` 轉回原域秒）。CSV 的「開始/結束範圍秒數」列印的就是這兩個值。

**參數：**
- `ctx` (context.Context): 取消用 context
- `dataset` (*models.EMGDataset): EMG 數據集
- `windowSize` (int): 滑動窗口大小，範圍：1-10000
- `r` (MaxMeanRange): 時間範圍（秒），兩端各自可為 nil

**示例：**
```go
// 只給起點：2.0 秒到資料末端
start := 2.0
run, err := calc.CalculateInRange(context.Background(), dataset, 100,
    calculator.MaxMeanRange{Start: &start})
if err != nil {
    log.Fatal(err)
}

fmt.Printf("時間範圍 %.1f-%.1f 秒內的最大平均值：\n", run.Start, run.End)
for _, result := range run.Results {
    fmt.Printf("通道 %d: %.6f\n", result.ColumnIndex, result.MaxMean)
}
```

**CalculateFromRawData / CalculateFromRawDataInRange**

```go
func (c *MaxMeanCalculator) CalculateFromRawData(
    ctx context.Context,
    records [][]string,
    windowSize int,
) ([]models.MaxMeanResult, error)

func (c *MaxMeanCalculator) CalculateFromRawDataInRange(
    ctx context.Context,
    records [][]string,
    windowSize int,
    r MaxMeanRange,
) (*MaxMeanRun, error)
```

從 CSV 原始字串資料計算最大平均值。內部會呼叫 `parsers.DataParser` 解析；`CalculateFromRawDataInRange` 的範圍語意同 `CalculateInRange`。

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

// 保存標準化結果（寫到 OutputDir，回傳實際寫入的路徑）
csvHandler := io.NewCSVHandler(cfg)
outPath, err := csvHandler.WriteNormalized(io.WriteRequest{Filename: "normalized_data.csv"}, normalizedData)
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

接受 phase-sync 後的 EMG 資料與肌肉對清單，使用 errgroup 並行計算每對 CCI 時間序列，並產出 `CCIAnalysisResult`（每對的 CCI 時間序列，與步態週期 gait 區間）。

**CCIAnalysisResult.Validate / GaitPercent**（ADR-0053）

```go
func (r *CCIAnalysisResult) Validate() error
func (r *CCIAnalysisResult) GaitPercent(t float64) float64
```

`Validate` 檢查結果不變式：非 nil（`ErrNilResult`）、`GaitStartTime` / `GaitEndTime` 為有限值且 duration > 0（`ErrInvalidGaitCycle`）、每個 `PairResults[i].Values` 長度等於 `len(TimeValues)`（`ErrPairLengthMismatch`）。`computeCCI` 回傳前、`io.WriteCCIResult`、`GenerateCCIInteractiveChart` 入口都呼叫它。`GaitPercent` 把 EMG 時間換算成 gait 百分比，不 clamp。

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

`parsers` 套件另提供 `EMGParser`、`ANCParser`、`MotionParser`、`PhaseManifestParser` 等格式專用 reader；共用的工具（`ParseFloatCell`、`ValidateTimeSeries[T]`）位於 `parse_helpers.go`。EMG 時間軸的區間切片（`SliceEMG`）與越界判斷（`OutsideEMG`）在 `synchronizer` 套件（ADR-0043）。

---

## I/O 操作

### CSV 處理

#### CSVHandler

`CSVHandler` 提供 CSV 文件讀寫功能，內建路徑驗證、BOM 支援與 symlink 拒絕（`O_NOFOLLOW`）。

```go
type CSVHandler struct {
    // 內部欄位：config、pathValidator、csvValidator、filenameValidator、logger、converter
}
```

**NewCSVHandler**

```go
func NewCSVHandler(cfg *config.AppConfig) *CSVHandler
```

建立 CSV handler。`cfg` 提供 InputDir / OutputDir / OperateDir 路徑邊界與 BOMEnabled 設定。

**ReadCSV**

```go
func (h *CSVHandler) ReadCSV(path string) ([][]string, error)
```

使用者選取 CSV 的**唯一讀取入口**，回傳原始列／欄陣列（**未解析**為 EMGDataset；要解析請串接 `parsers.DataParser.ParseRawData`）。`path` 可為任意絕對路徑（GUI 檔案對話框回傳的路徑），不再限定於 InputDir 之下。流程：

1. `ValidateFilename(Base(path))`：只擋檔案系統非法的檔名（ADR-0041），並要求 `.csv` 副檔名（不分大小寫；ADR-0054）。
2. `PathValidator.ValidateExternalPath(path)`：擋 `..` traversal 與系統敏感位置（ADR-0038 / ADR-0039）。路徑不做 URL-decode，`%`、`+` 皆為字面字元。
3. `fsperm.OpenRegularLimited(path, fsperm.MaxReadBytes)`：open 前先 `stat`，非 regular file（目錄、FIFO 等）直接拒絕；以 `fsperm.ReadFlags`（含 `O_NOFOLLOW`）開檔，再對 fd `fstat`：必須是 regular file 且 ≤ 100 MB，超過回 `error.io.file_too_large`；讀取中檔案變大，讀到第 100 MB + 1 byte 時同樣回 `error.io.file_too_large`（ADR-0056）。
4. 單次解析，再做讀取側 sanity check（儲存格 ≤ 32 KB、CSV 結構、user-picked 檔案的 UTF-8）；讀取側不做 injection 偵測（寫入側由 `csvutil` 負責逸出）。

錯誤是帶 key 的 `i18n.Error`（`error.io.*`；內層為 `error.filename.*` / `error.csv.*`），caller 層以 `i18n.Localize` 渲染（ADR-0048）。非一般檔案與過大不帶路徑（過大只帶 MB 數）；CSV 驗證錯誤只帶行號 / 欄號。開檔錯誤以原 error 為 Cause，`errors.Is(err, os.ErrNotExist)` 照常成立。

**參數：**
- `path` (string): CSV 檔的絕對路徑

**示例：**
```go
cfg, _ := config.LoadConfig("./config.json")
handler := io.NewCSVHandler(cfg)

// 讀取使用者選取的 CSV 原始資料
records, err := handler.ReadCSV("/Users/me/data/emg_data.csv")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("讀取成功：%d 筆 row\n", len(records))
```

**WriteRequest 與公開 writer**

```go
type WriteRequest struct {
    Filename string // File-based writer 的輸出檔名（須為 .csv）
    SubDir   string // OutputDir 之下的子目錄，空字串 = OutputDir 根（MaxMean batch 用）
}

// File-based（檔名由 caller 以 WriteRequest 傳入）
func (h *CSVHandler) WriteMaxMean(req WriteRequest, headers []string, results []models.MaxMeanResult, startRange, endRange float64) (string, error)
func (h *CSVHandler) WriteNormalized(req WriteRequest, dataset *models.EMGDataset) (string, error)
func (h *CSVHandler) WritePhaseAnalysis(req WriteRequest, headers []string, result *calculator.AnalyzeResult /* ... */) (string, error)
func (h *CSVHandler) WriteFileCSV(req WriteRequest, data [][]string) (string, error) // raw：data[0] 為 header

// Subject-based（檔名由 filename.SubjectOutputName 推導，一律寫在 OutputDir 根，不收 WriteRequest）
func (h *CSVHandler) WritePhaseSyncResult(stats *models.EMGStatistics) (string, error)
func (h *CSVHandler) WriteCCIResult(ctx context.Context, result *cci.CCIAnalysisResult) (string, error)
func (h *CSVHandler) WriteSubjectPNG(subject, suffix string, png []byte) (string, error) // 只收已解碼並驗證過的 PNG 位元組
// 另有 WriteNormalizedPhaseSyncResult / WriteNormalizedPhaseSyncEMG / WriteCCIPhasesResult /
// WriteMuscleRatioOutputAll / WriteMuscleRatioOutputPhases，見 internal/io/csv_handler.go
```

所有 writer 皆回傳**實際寫入的路徑**，並經同一道落檔門（GLOSSARY「Output placement」，ADR-0055）：containment → 檔名驗證（CSV 要 `.csv`，PNG 用 `ValidateName`）→ 敏感位置 → 建目錄 → atomic 寫入（tmp → fsync → rename）。row layout、scaling、precision、formula-injection 逸出由 writer 內部持有；caller 不再自行組裝 `[][]string`。BOM：全部 CSV 都照 `BOMEnabled`（關掉時 Subject-based CSV 也不帶 BOM）；PNG 位元組原樣寫出。目標檔名若是 symlink，rename 取代連結本身、不跟到目標。輸出一律落在 `cfg.OutputDir[/SubDir]` 之內，`SubDir` 含 `..` 逸出時回「輸出路徑無效」。

**示例：**
```go
// 寫入計算結果到 OutputDir
outPath, err := handler.WriteMaxMean(
    io.WriteRequest{Filename: "max_mean_results.csv"},
    records[0], results, 0, 0,
)
if err != nil {
    log.Fatal(err)
}
fmt.Println("已寫入：", outPath)
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

**SaveConfigAtomic**

```go
func (c *AppConfig) SaveConfigAtomic(configPath string) error
```

以 atomic tmp+rename 保存配置到文件，parent 目錄不存在時自動建立。

**示例：**
```go
// 修改配置
config.WindowSize = 100
config.ScalingFactor = 1000

// 保存配置
err := config.SaveConfigAtomic("config.json")
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

使用者會看到的錯誤是帶 catalog key 的 `i18n.Error`（`internal/i18n/error.go`，ADR-0048）；計算層另有 sentinel 與 `CalculatorError`（`internal/errors`）。舊的 `AppError` / `ValidationError` / `ErrorCode` / `IsRecoverable` 已刪除（ADR-0058）。

#### i18n.Error

```go
type Error struct {
    Key   string // catalog key，例如 i18n.KeyErrorIOFileTooLarge
    Args  []any  // 以 Key 的 catalog 文字格式化
    Cause error  // 非 nil 時訊息接「: 」+ Cause 的文字
}

func NewError(key string, args ...any) error              // 只有訊息
func WrapError(cause error, key string, args ...any) error // 「訊息: cause」
func (e *Error) Error() string  // 固定以內建 zh-TW catalog 渲染（log 跨 locale 一致）
func (e *Error) Unwrap() error  // 回 Cause
func Localize(err error) string // 依目前 locale 渲染給使用者看
```

**示例：**
```go
// 產生：帶 key；路徑只進 log，不放進訊息
if sizeMB > 100 {
    return i18n.NewError(i18n.KeyErrorIOFileTooLarge, sizeMB)
}
if err != nil {
    // Cause 保留原 error，errors.Is(err, os.ErrNotExist) 照常成立
    return i18n.WrapError(err, i18n.KeyErrorIOOpenFailed)
}

// 分類：以 errors.As 取 Key，不比對錯誤文字
var keyed *i18n.Error
if errors.As(err, &keyed) {
    switch keyed.Key {
    case i18n.KeyErrorIOFileTooLarge:
        // 處理檔案過大
    case i18n.KeyErrorIOFilenameInvalid:
        // 處理檔名不合法
    }
}

// 顯示：依目前 locale 渲染（內建 zh-TW catalog 下與 err.Error() 相同）
msg := i18n.Localize(err)
```

**送進前端**：GUI handler 不自行渲染 err。bound method 回的 err 由 `recoverHandlerPanic` 出口統一送出 `redact.Paths(i18n.Localize(err))`（先在地化、後脫敏；文字沒變時回原 err）；result 的 `Message` 經 `gui/envelope.go` 的 `failMessage` / `inputMessage` / `redactText`，同樣先在地化、後脫敏。兩條通道見 GLOSSARY.md 的 **Webview envelope**。

#### 計算層 sentinel 與 CalculatorError

`internal/errors` 只放計算層錯誤：`ErrEmptyDataset`、`ErrWindowTooLarge`、`ErrInsufficientData` 等 sentinel（以 `errors.Is` 比對），以及包住 sentinel、帶情境訊息的 `CalculatorError`。

```go
err := apperrors.NewCalculatorError(apperrors.ErrWindowTooLarge, "窗口大小超過資料長度")

if errors.Is(err, apperrors.ErrWindowTooLarge) {
    // 處理窗口過大
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

內部結構（`allowedBasePaths []string`）於建構後不可變（`SetAllowedBasePaths` 與 RWMutex 已移除，見 ADR-0032），可在不同 goroutine 之間安全併用。

**NewPathValidator**

```go
func NewPathValidator(allowedBasePaths []string) *PathValidator
```

allow-list 只經 `GetAllowedBasePaths()` 交給 `fsperm.AtomicWriteFile` 當 dirfd 錨點（Output placement）；`ValidateExternalPath` / `ValidateExternalDir` 不看它。原本限定 allow-list 的 `ValidateFilePath` 已刪除（ADR-0055）。

**示例：**
```go
// 創建路徑驗證器
validator := security.NewPathValidator([]string{
    "/app/input",
    "/app/output",
    "/app/config",
})
```

**ValidateExternalPath**

```go
func (v *PathValidator) ValidateExternalPath(path string) error
```

驗證使用者外部選取的**檔案**路徑（GUI 檔案對話框、CSV 讀寫目標），**不**要求落在 allow-list 內。擋 `..` traversal、系統敏感位置（`/etc`、`~/.ssh` …）與過長路徑；並把路徑經 symlink 解析後再檢查一次（`ln -s /etc /tmp/foo` 不能繞過）。路徑不做 URL-decode。`CSVHandler.ReadCSV` 與 Output placement（全部輸出檔）都經此驗證（ADR-0038 / ADR-0039 / ADR-0055）。**不檢查檔名**（Windows 保留名、檔名長度）：檔名規則由 `filename.ValidateName` 單一裁判，檔案 caller 自己呼叫（ADR-0054）。

**ValidateExternalDir**

```go
func (v *PathValidator) ValidateExternalDir(dir string) error
```

驗證使用者外部選取的**目錄**（output / data folder / config 目錄）。與 `ValidateExternalPath` 擋同一組敏感位置，但以目錄語意判定：比對前補結尾分隔符，使目錄根本身（`/etc`、`~/.ssh`）也命中。取代舊的「附 dummy child 再驗檔案」手法（ADR-0038）。

**示例：**
```go
if err := validator.ValidateExternalDir(dataFolder); err != nil {
    return fmt.Errorf("資料夾不安全: %w", err)
}
if err := validator.ValidateExternalPath(csvPath); err != nil {
    return fmt.Errorf("檔案路徑不安全: %w", err)
}
```

### 輸入驗證

輸入驗證由子驗證器直接提供：`validation/csv`（`csv.NewValidator()`）負責 CSV 結構與內容驗證，`validation/filename`（`filename.NewValidator()`）負責檔名安全性驗證。

`validation/filename` 是檔名規則的**單一裁判**（ADR-0054）：

```go
func ValidateName(name string) error                       // 不綁副檔名
func (v *Validator) ValidateFilename(filename string) error // = ValidateName + .csv
```

`ValidateName` 擋空白、tab / 控制字元、Unicode Cf / Cs、路徑分隔符、Windows 磁碟代號前綴、`<>:"|?*`、Windows 保留名（**每個點分段**比對，`CON.txt.csv`、`a.NUL.png` 都擋）與長度 > 255。`ValidateFilename` 另要求副檔名為 `.csv`（TrimSpace 後取、不分大小寫；尾點與無副檔名拒絕）。`manifest.LoadManifests` 開檔前對 `Base(path)` 呼叫 `ValidateName`，`manifest.OpenDataFile` 對資料檔名（`\` 正規化成 `/` 後取 `path.Base`，各 OS 結果相同）呼叫 `ValidateName`（ADR-0056）；`CSVHandler.ReadCSV` 與 CSV 輸出（Output placement 的 CSV payload）呼叫 `ValidateFilename`，PNG 輸出呼叫 `ValidateName`。`filename.Sanitize` 用同一條逐段保留名規則（`a.NUL.b` → `a.NUL_safe.b`），所以 `SubjectOutputName(subject, suffix) + ext` 必過 `ValidateName`（property 測試釘住）。`SubjectOutputKey(subject) string` = NFC(ToLower(Sanitize(subject)))，是 Subject 在 macOS / Windows 上的輸出檔名身分：兩個 Subject 的 key 相同即寫到同一檔，`manifest.LoadManifests` 以它拒絕撞名的 manifest（ADR-0056）。

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

**CSVPayload**

```go
func CSVPayload(opts SafeWriteOptions) func(io.Writer) error
```

CSV 輸出的位元組由它持有，是 sanitize 的單一 chokepoint：UTF-8 BOM（`OmitBOM` 為 false 時；io 依 `BOMEnabled` 設定）、header 經 `SanitizeHeaderRow`、每列 body 經 `SanitizeBodyRow`，保證**所有 cell（header + body）**都會被 sanitize，堵住 `result.PhaseName` 等 user-controllable 標籤的 formula-injection 路徑；每 1024 列 Flush 一次探測 underlying 寫入錯誤。落檔交給 `fsperm.AtomicWriteFile`（經 io 的 Output placement，ADR-0055）。`SanitizeCellForWrite` 是 idempotent，已 escape 的 cell 不會被二次處理。

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

**OpenRegularLimited / CheckRegularSize**（ADR-0056）

```go
const MaxReadBytes = 100 * 1024 * 1024
var ErrNotRegular, ErrTooLarge error             // 一般 error(fsperm 不 import i18n)
type TooLargeError struct{ Size, Limit int64 } // Unwrap() == ErrTooLarge

func OpenRegularLimited(path string, limit int64) (*LimitedFile, error)
func CheckRegularSize(f *os.File, limit int64) error
```

整檔讀進記憶體的輸入檔的讀檔門。`OpenRegularLimited`：pre-stat（非 regular 回 `ErrNotRegular`，FIFO 不必開）→ `ReadFlags` 開檔（錯誤原樣回 `*os.PathError`）→ `CheckRegularSize`（fstat：regular 且 ≤ limit，否則 `ErrNotRegular` / `*TooLargeError`）→ `*LimitedFile`（`Read` / `Close`），讀到第 limit+1 byte 時回 `*TooLargeError{Size: limit+1}`。caller：`CSVHandler.ReadCSV`、`manifest.LoadManifests`（分期總檔案）。`CheckRegularSize` 給已開好 fd 的 `manifest.OpenDataFile` 用（資料檔只在開檔時檢查）。user-facing 訊息由 caller 轉換。

**WriteFileNoFollow**

```go
func WriteFileNoFollow(path string, data []byte) error
```

一次性整檔寫入 helper（目前用於 i18n JSON）。寫進 OutputDir 的輸出檔（CSV、PNG）不走它，而是經 io 的 Output placement 走 `fsperm.AtomicWriteFile`（ADR-0055）。

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

1. **及時釋放資源**
   ```go
   defer file.Close()
   ```

2. **監控記憶體使用**
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

1. **使用者會看到的錯誤帶 i18n key（caller 以 `errors.As(...).Key` 分類、以 `i18n.Localize` 顯示）**
   ```go
   if err != nil {
       return i18n.WrapError(err, i18n.KeyErrorHandlerCalculateFailed)
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
A: 超過 100 MB 的 CSV 會被 `CSVHandler.ReadCSV` 以 `error.io.file_too_large` 拒絕（串流路徑已刪除，見 ADR-0033），請先分割檔案。

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