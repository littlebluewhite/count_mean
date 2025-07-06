# 使用指南

## 快速開始

### 1. 安裝需求

確保您的系統已安裝：
- Go 1.24 或更高版本
- Git (用於克隆專案)

### 2. 下載和安裝

`bash
# 克隆專案
git clone <repository-url>
cd count_mean

# 下載依賴
go mod download

# 編譯程序
go build -o emg_tool main.go
`

## GUI 模式使用

### 啟動 GUI

`bash
# 直接運行
go run main.go

# 或使用編譯後的程序
./emg_tool
`

### GUI 操作步驟

1. **選擇輸入文件**：點擊「選擇文件」按鈕
2. **設定參數**：調整縮放因子和精度
3. **開始分析**：點擊「開始分析」按鈕
4. **查看結果**：在結果區域查看計算結果
5. **保存結果**：點擊「保存結果」按鈕

## 命令行模式使用

### 基本語法

`bash
go run main.go -cli [選項]
`

### 常用選項

`
-input    <文件路徑>    指定輸入 CSV 文件
-output   <文件路徑>    指定輸出文件 (可選)
-scaling  <數值>       設定縮放因子 (默認: 10)
-precision <數值>      設定精度 (默認: 10)
-config   <配置文件>   指定配置文件路徑
-verbose              詳細輸出模式
-help                 顯示幫助信息
`

### 使用範例

`bash
# 基本使用
go run main.go -cli -input data.csv

# 指定輸出文件
go run main.go -cli -input data.csv -output result.csv

# 自定義參數
go run main.go -cli -input data.csv -scaling 5 -precision 8

# 使用配置文件
go run main.go -cli -config custom_config.json

# 詳細輸出
go run main.go -cli -input data.csv -verbose
`

## 配置文件

### 配置結構

創建 `config.json` 文件：

`json
{
  "scaling_factor": 10,
  "precision": 10,
  "language": "zh-TW",
  "log_level": "info",
  "max_file_size": 104857600,
  "concurrent_workers": 4,
  "cache_enabled": true
}
`

### 配置說明

- **scaling_factor**: 數據縮放因子
- **precision**: 計算精度
- **language**: 界面語言 (zh-TW, zh-CN, en-US)
- **log_level**: 日誌級別 (debug, info, warn, error)
- **max_file_size**: 最大文件大小 (字節)
- **concurrent_workers**: 並發處理工作者數量
- **cache_enabled**: 是否啟用緩存

## 性能測試

### 運行基準測試

`bash
# 完整性能測試套件
go run benchmark_test_main.go

# 只測試 CSV 處理
go run benchmark_test_main.go -csv-only

# 詳細輸出模式
go run benchmark_test_main.go -verbose

# 自定義報告目錄
go run benchmark_test_main.go -report-dir ./my_reports
`

### 查看測試報告

測試完成後，報告文件會保存在指定目錄中：

`
benchmark_reports/
├── csv_benchmark_report_20240101_120000.json
├── system_benchmark_report_20240101_120100.json
├── memory_benchmark_report_20240101_120200.json
└── concurrency_benchmark_report_20240101_120300.json
`

## 故障排除

### 常見問題

1. **無法讀取 CSV 文件**
   - 檢查文件路徑是否正確
   - 確認文件權限
   - 驗證 CSV 格式

2. **記憶體不足**
   - 減少 concurrent_workers 數量
   - 增加系統虛擬記憶體
   - 分批處理大文件

3. **計算結果異常**
   - 檢查輸入數據格式
   - 驗證數值範圍
   - 調整精度設定

### 日誌查看

日誌文件位於 `logs/` 目錄：

`bash
# 查看最新日誌
tail -f logs/app.log

# 查看錯誤日誌
grep "ERROR" logs/app.log
`

## 高級功能

### 批量處理

`bash
# 處理目錄中所有 CSV 文件
go run main.go -cli -batch -input ./data_dir -output ./results_dir
`

### 自定義算法

實現 `Calculator` 接口來添加自定義算法：

`go
type Calculator interface {
    Calculate(data [][]float64) (float64, float64, error)
    CalculateWithWindow(data [][]float64, windowSize int) ([]float64, []float64, error)
}
`

### 擴展 GUI

在 `gui/components/` 目錄中添加新的 UI 組件：

`go
type CustomWidget struct {
    widget.BaseWidget
    // 自定義屬性
}
`

## 最佳實踐

1. **數據準備**：確保 CSV 數據格式正確
2. **參數調優**：根據數據特性調整參數
3. **性能監控**：定期運行基準測試
4. **日誌管理**：定期清理舊日誌文件
5. **配置備份**：備份重要配置文件
