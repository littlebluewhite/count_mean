# API 文檔

## 概述

EMG 數據分析工具的 API 文檔，包含所有公開的函數、結構體和接口。

## 套件: gui

### 類型定義

#### App

App 表示GUI應用程式


```go
type App &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=21011) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400019dc40 <nil> 0 0x14000163b30 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**Run**

Run 啟動GUI應用程式


**checkAndHandleLargeFile**

checkAndHandleLargeFile 檢查並處理大文件


**executeBatchCalculation**

executeBatchCalculation 執行批量計算


**executeMaxMeanCalculation**

executeMaxMeanCalculation 執行最大平均值計算


**executeNormalization**

executeNormalization 執行資料標準化


**executePhaseAnalysis**

executePhaseAnalysis 執行階段分析


**executeSingleFileCalculation**

executeSingleFileCalculation 執行單檔案計算


**handleLargeFileError**

handleLargeFileError 處理大文件錯誤


**handleValidationError**

handleValidationError 處理驗證錯誤


**processBatchLargeFile**

processBatchLargeFile 批處理大文件


**processLargeFile**

processLargeFile 處理大文件


**resetToDefaults**

resetToDefaults 重置為默認配置


**saveConfiguration**

saveConfiguration 保存配置設定


**setupUI**

setupUI 設置用戶界面


**showConfigDialog**

showConfigDialog 顯示配置設定對話框


**showDirectorySelectDialog**

showDirectorySelectDialog 顯示目錄選擇對話框（用於配置設定）


**showError**

showError 顯示錯誤對話框


**showFileSelectDialog**

showFileSelectDialog 顯示文件選擇對話框


**showFolderSelectDialog**

showFolderSelectDialog 顯示資料夾選擇對話框


**showInfo**

showInfo 顯示信息對話框


**showMaxMeanCalculationDialog**

showMaxMeanCalculationDialog 顯示最大平均值計算對話框


**showNormalizationDialog**

showNormalizationDialog 顯示資料標準化對話框


**showPhaseAnalysisDialog**

showPhaseAnalysisDialog 顯示階段分析對話框


**updateStatus**

updateStatus 更新狀態顯示


---

## 套件: benchmark

### 類型定義

#### BenchmarkMetrics

BenchmarkMetrics 性能測試指標


```go
type BenchmarkMetrics &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=57356) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400024edc0 <nil> 0 0x1400024c4b0 <nil>})] %!s(token.Pos=0)}
```

#### BenchmarkResult

BenchmarkResult 基準測試結果


```go
type BenchmarkResult &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=58262) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400024f2c0 <nil> 0 0x1400024c600 <nil>})] %!s(token.Pos=0)}
```

#### BenchmarkSummary

BenchmarkSummary 測試摘要


```go
type BenchmarkSummary &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=59025) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400024f720 <nil> 0 0x1400024c9c0 <nil>})] %!s(token.Pos=0)}
```

#### Benchmarker

Benchmarker 性能測試器


```go
type Benchmarker &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=59825) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400024fba0 <nil> 0 0x1400024ca68 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**Benchmark**

Benchmark 執行性能測試


**BenchmarkOperations**

BenchmarkOperations 執行操作次數性能測試


**BenchmarkWithData**

BenchmarkWithData 執行帶數據量的性能測試


**GenerateReport**

GenerateReport 生成完整的測試報告


**GetResults**

GetResults 獲取測試結果


**PrintSummary**

PrintSummary 打印測試摘要


**Reset**

Reset 重置測試結果


**SaveReportToFile**

SaveReportToFile 保存報告到文件


**calculateSummary**

calculateSummary 計算測試摘要


**formatReportAsJSON**

formatReportAsJSON 格式化報告為 JSON


#### CSVBenchmarks

CSVBenchmarks CSV 相關性能測試


```go
type CSVBenchmarks &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=68841) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400027e120 <nil> 0 0x140002765b8 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**BenchmarkCSVReading**

BenchmarkCSVReading 測試 CSV 讀取性能


**BenchmarkConcurrentProcessing**

BenchmarkConcurrentProcessing 測試並發處理性能


**BenchmarkLargeFileProcessing**

BenchmarkLargeFileProcessing 測試大文件處理性能


**BenchmarkMaxMeanCalculation**

BenchmarkMaxMeanCalculation 測試最大均值計算性能


**BenchmarkMemoryUsage**

BenchmarkMemoryUsage 測試記憶體使用性能


**BenchmarkNormalization**

BenchmarkNormalization 測試數據正規化性能


**Cleanup**

Cleanup 清理臨時文件


**GetBenchmarker**

GetBenchmarker 獲取基準測試器


**RunAllBenchmarks**

RunAllBenchmarks 執行所有 CSV 相關的性能測試


**generateTestCSV**

generateTestCSV 生成測試用的 CSV 文件


#### SystemInfo

SystemInfo 系統資訊


```go
type SystemInfo &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=58676) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400024f500 <nil> 0 0x1400024c738 <nil>})] %!s(token.Pos=0)}
```

---

## 套件: calculator

### 類型定義

#### AnalyzeResult

AnalyzeResult 階段分析結果


```go
type AnalyzeResult &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=95450) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140002e9580 <nil> 0 0x140002e29c0 <nil>})] %!s(token.Pos=0)}
```

#### MaxMeanCalculator

MaxMeanCalculator 處理最大平均值計算


```go
type MaxMeanCalculator &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=78578) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400029c480 <nil> 0 0x1400029a168 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**Calculate**

Calculate 計算指定窗口大小的最大平均值


**CalculateFromRawData**

CalculateFromRawData 從原始字符串數據計算


**CalculateFromRawDataWithRange**

CalculateFromRawDataWithRange 從原始字符串數據計算指定時間範圍內的最大平均值


**CalculateWithRange**

CalculateWithRange 計算指定時間範圍內的最大平均值


**parseRawData**

parseRawData 解析原始字符串數據


#### Normalizer

Normalizer 處理數據標準化


```go
type Normalizer &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=88983) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140002d2360 <nil> 0 0x1400029b7b8 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**Normalize**

Normalize 標準化數據集（每個值除以參考值）


**NormalizeFromRawData**

NormalizeFromRawData 從原始字符串數據進行標準化


**parseRawData**

parseRawData 解析原始字符串數據


#### PhaseAnalyzer

PhaseAnalyzer 處理階段分析


```go
type PhaseAnalyzer &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=95035) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140002e91c0 <nil> 0 0x140002e28b8 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**Analyze**

Analyze 分析不同階段的數據


**AnalyzeFromRawData**

AnalyzeFromRawData 從原始字符串數據進行階段分析


**parsePhases**

parsePhases 解析階段字符串為時間範圍


**parseRawData**

parseRawData 解析原始字符串數據


---

## 套件: config

### 類型定義

#### AppConfig

AppConfig 應用程式配置


```go
type AppConfig &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=101896) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x14000308e80 <nil> 0 0x140002e3938 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**EnsureDirectories**

EnsureDirectories 確保配置中的目錄存在


**ProcessingOptions**

ProcessingOptions 獲取處理選項


**SaveConfig**

SaveConfig 保存配置到檔案


**ToAnalysisConfig**

ToAnalysisConfig 轉換為分析配置


**Validate**

Validate 驗證配置


---

## 套件: errors

### 類型定義

#### AppError

AppError represents a structured application error


```go
type AppError &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=107022) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003243c0 <nil> 0 0x1400031c3a8 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**Error**

Error implements the error interface


**Is**

Is checks if the error matches the target error code


**Unwrap**

Unwrap returns the underlying cause error


**WithContext**

WithContext adds context information to the error


#### ErrorCode

ErrorCode represents different types of errors


```go
type ErrorCode &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=106029) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x14000319ca0 <nil> 0 0x14000319cc0 <nil>})] %!s(token.Pos=0)}
```

#### ProcessingError

ProcessingError represents errors that occur during data processing


```go
type ProcessingError &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=110433) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400032a520 <nil> 0 0x1400031ca68 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**Error**

Error returns a formatted error message for ProcessingError


#### ValidationError

ValidationError represents input validation errors


```go
type ValidationError &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=111773) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x1400032b6c0 <nil> 0 0x1400031cd50 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**Error**

Error returns a formatted error message for ValidationError


### 函數

#### IsRecoverable

IsRecoverable determines if an error type is recoverable


---

## 套件: i18n

### 類型定義

#### I18n

I18n manages internationalization


```go
type I18n &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=115284) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x14000333840 <nil> 0 0x1400031d3b0 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**DetectSystemLocale**

DetectSystemLocale attempts to detect the system locale


**GetLocale**

GetLocale returns the current locale


**GetLocaleName**

GetLocaleName returns the display name of a locale


**GetSupportedLocales**

GetSupportedLocales returns list of supported locales


**LoadTranslations**

LoadTranslations loads translation files from a directory


**SaveTranslations**

SaveTranslations saves current translations to files


**SetLocale**

SetLocale sets the current locale


**T**

T translates a message key


**getBuiltinTranslations**

getBuiltinTranslations returns built-in translations for a locale


**parseLocale**

parseLocale parses locale string and returns supported locale


#### Locale

Locale represents a supported locale


```go
type Locale &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=115000) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x14000333620 <nil> 0 0x14000333640 <nil>})] %!s(token.Pos=0)}
```

### 函數

#### GetLocaleName

#### InitI18n

InitI18n initializes the global i18n instance


#### SetLocale

#### T

Global functions for convenience


---

## 套件: io

### 類型定義

#### CSVHandler

CSVHandler 處理 CSV 檔案讀寫


```go
type CSVHandler &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=134190) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x14000368120 <nil> 0 0x1400034aac8 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**ConvertMaxMeanResultsToCSV**

ConvertMaxMeanResultsToCSV 將最大平均值結果轉換為 CSV 格式


**ConvertNormalizedDataToCSV**

ConvertNormalizedDataToCSV 將標準化數據轉換為 CSV 格式


**ConvertPhaseAnalysisToCSV**

ConvertPhaseAnalysisToCSV 將階段分析結果轉換為 CSV 格式


**GetFileInfo**

GetFileInfo 獲取文件信息


**ListCSVFilesInDirectory**

ListCSVFilesInDirectory 列出指定目錄中的CSV文件


**ListInputDirectories**

ListInputDirectories 列出輸入目錄中的子目錄


**ListInputFiles**

ListInputFiles 列出輸入目錄中的CSV文件


**ProcessLargeFile**

ProcessLargeFile 處理大文件


**ReadCSV**

ReadCSV 讀取 CSV 檔案（自動檢測大文件並使用相應處理方式）


**ReadCSVFromDirectory**

ReadCSVFromDirectory 從指定目錄讀取CSV檔案


**ReadCSVFromInput**

ReadCSVFromInput 從輸入目錄讀取CSV檔案


**ReadCSVFromPrompt**

ReadCSVFromPrompt 從使用者輸入讀取 CSV 檔案


**ReadCSVFromPromptWithName**

ReadCSVFromPromptWithName 從使用者輸入讀取 CSV 檔案並返回檔名


**ReadLargeCSVStreaming**

ReadLargeCSVStreaming 流式讀取大 CSV 文件


**WriteCSV**

WriteCSV 寫入 CSV 檔案


**WriteCSVToOutput**

WriteCSVToOutput 寫入CSV文件到輸出目錄


**WriteCSVToOutputDirectory**

WriteCSVToOutputDirectory 寫入CSV文件到輸出目錄的子目錄


**WriteLargeCSVStreaming**

WriteLargeCSVStreaming 流式寫入大 CSV 文件


#### FileInfo

FileInfo 文件信息


```go
type FileInfo &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=151768) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x14000393580 <nil> 0 0x1400037daa0 <nil>})] %!s(token.Pos=0)}
```

#### LargeFileHandler

LargeFileHandler 處理大文件的結構


```go
type LargeFileHandler &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=150654) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x14000392cc0 <nil> 0 0x1400037d848 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**GetFileInfo**

GetFileInfo 獲取文件基本信息


**ProcessLargeFileInChunks**

ProcessLargeFileInChunks 分塊處理大文件


**ReadCSVStreaming**

ReadCSVStreaming 流式讀取大 CSV 文件


**WriteCSVStreaming**

WriteCSVStreaming 流式寫入 CSV 文件


**calculateSlidingWindow**

calculateSlidingWindow 計算滑動視窗


**checkMemoryUsage**

checkMemoryUsage 檢查記憶體使用


**getMemoryUsage**

getMemoryUsage 獲取當前記憶體使用


**parseDataRow**

parseDataRow 解析數據行


**scanFileStructure**

scanFileStructure 快速掃描文件結構


#### ProgressCallback

ProgressCallback 進度回調函數類型


```go
type ProgressCallback &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=150531) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x14000392b80 <nil> 0 0x14000392c80 <nil>})] %!s(token.Pos=0)}
```

#### StreamingResult

StreamingResult 流式處理結果


```go
type StreamingResult &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=152020) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003937a0 <nil> 0 0x1400037db48 <nil>})] %!s(token.Pos=0)}
```

---

## 套件: logging

### 類型定義

#### LogEntry

LogEntry represents a structured log entry


```go
type LogEntry &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=166267) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003d04e0 <nil> 0 0x140003a5f50 <nil>})] %!s(token.Pos=0)}
```

#### LogLevel

LogLevel represents the severity level of a log entry


```go
type LogLevel &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=165802) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003d0060 <nil> 0 0x140003d0080 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**String**

String returns the string representation of the log level


#### Logger

Logger provides structured logging functionality


```go
type Logger &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=166856) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003d08a0 <nil> 0 0x140003d4048 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**Debug**

Debug logs a debug message


**Error**

Error logs an error message


**Fatal**

Fatal logs a fatal message and exits


**Info**

Info logs an info message


**Warn**

Warn logs a warning message


**WithContext**

WithContext adds context data to the logger


**WithError**

WithError adds error context to the logger


**WithModule**

WithModule returns a logger with a specific module context


**log**

log writes a log entry


**writeJSON**

writeJSON writes the log entry in JSON format


**writeText**

writeText writes the log entry in human-readable text format


### 函數

#### Debug

Convenience functions using the default logger


#### Error

#### Fatal

#### Info

#### InitLogger

InitLogger initializes the default logger


#### Warn

---

## 套件: models

### 類型定義

#### AnalysisConfig

AnalysisConfig 代表分析配置


```go
type AnalysisConfig &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=174528) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003e8cc0 <nil> 0 0x140003d58f0 <nil>})] %!s(token.Pos=0)}
```

#### EMGData

EMGData 代表 EMG 數據的結構


```go
type EMGData &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=173779) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003e8760 <nil> 0 0x140003d57b8 <nil>})] %!s(token.Pos=0)}
```

#### EMGDataset

EMGDataset 代表完整的 EMG 數據集


```go
type EMGDataset &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=173920) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003e8860 <nil> 0 0x140003d5800 <nil>})] %!s(token.Pos=0)}
```

#### MaxMeanResult

MaxMeanResult 代表最大平均值計算結果


```go
type MaxMeanResult &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=174068) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003e8960 <nil> 0 0x140003d5848 <nil>})] %!s(token.Pos=0)}
```

#### PhaseAnalysisResult

PhaseAnalysisResult 代表階段分析結果


```go
type PhaseAnalysisResult &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=174309) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003e8b20 <nil> 0 0x140003d5890 <nil>})] %!s(token.Pos=0)}
```

#### ProcessingOptions

ProcessingOptions 代表處理選項


```go
type ProcessingOptions &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=174955) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003e8fe0 <nil> 0 0x140003d5980 <nil>})] %!s(token.Pos=0)}
```

#### TimeRange

TimeRange 代表時間範圍


```go
type TimeRange &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=174830) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003e8f00 <nil> 0 0x140003d5938 <nil>})] %!s(token.Pos=0)}
```

---

## 套件: security

### 類型定義

#### PathValidator

PathValidator provides secure path validation functionality


```go
type PathValidator &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=175250) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140003e9220 <nil> 0 0x140003d59c8 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**GetSafePath**

GetSafePath returns a safe path within the allowed directories


**IsCSVFile**

IsCSVFile checks if the file has a .csv extension


**SanitizePath**

SanitizePath sanitizes a file path by removing dangerous characters


**ValidateDirectoryPath**

ValidateDirectoryPath validates that a directory path is within allowed directories


**ValidateFilePath**

ValidateFilePath validates that a file path is within allowed directories


---

## 套件: validation

### 類型定義

#### InputValidator

InputValidator provides comprehensive input validation functionality


```go
type InputValidator &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=178430) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x140004031a0 <nil> 0 0x140004042a0 <nil>})] %!s(token.Pos=0)}
```

##### 方法

**SanitizeString**

SanitizeString removes dangerous characters from string input


**ValidateCSVData**

ValidateCSVData validates CSV data structure


**ValidateDirectoryPath**

ValidateDirectoryPath validates directory path input


**ValidateEmail**

ValidateEmail validates email address format


**ValidateFilename**

ValidateFilename validates a filename for safety and correctness


**ValidateOutputFormat**

ValidateOutputFormat validates output format selection


**ValidatePhaseLabels**

ValidatePhaseLabels validates phase label input


**ValidatePrecision**

ValidatePrecision validates the precision parameter


**ValidateScalingFactor**

ValidateScalingFactor validates the scaling factor parameter


**ValidateTimeRange**

ValidateTimeRange validates time range parameters


**ValidateWindowSize**

ValidateWindowSize validates the window size parameter


**WithAllowedExtensions**

WithAllowedExtensions sets the allowed file extensions


**WithMaxFileSize**

WithMaxFileSize sets the maximum allowed file size


**validateSinglePhaseLabel**

validateSinglePhaseLabel validates a single phase label


---

## 套件: main

### 函數

#### createLargeTestFile

createLargeTestFile 創建大型測試文件


#### main

示範結構化日誌使用


---

## 套件: integration

### 函數

#### TestBenchmarkIntegration

性能基準測試整合測試


#### TestFullWorkflow_ConfigurationManagement

TestFullWorkflow_ConfigurationManagement 測試完整的配置管理流程


#### TestFullWorkflow_DataNormalization

TestFullWorkflow_DataNormalization 測試完整的數據標準化流程


#### TestFullWorkflow_ErrorHandling

TestFullWorkflow_ErrorHandling 測試完整流程的錯誤處理


#### TestFullWorkflow_MaxMeanCalculation

TestFullWorkflow_MaxMeanCalculation 測試完整的最大平均值計算流程


#### TestFullWorkflow_Performance

TestFullWorkflow_Performance 測試大數據集的性能


#### TestFullWorkflow_PhaseAnalysis

TestFullWorkflow_PhaseAnalysis 測試完整的階段分析流程


#### TestI18nIntegration

國際化集成測試


#### TestIntegration_ConcurrentOperations

#### TestIntegration_ErrorRecovery

#### TestIntegration_FullWorkflow

---

## 套件: util

### 類型定義

#### Number

```go
type Number &{%!s(*ast.CommentGroup=<nil>) %!s(token.Pos=285634) type %!s(token.Pos=0) [%!s(*ast.TypeSpec=&{<nil> 0x14000304f60 <nil> 0 0x14000276a98 <nil>})] %!s(token.Pos=0)}
```

### 函數

#### ArrayMax

#### ArrayMean

#### Str2Number

---

