# EMG 數據分析工具 🧠⚡

[![Go Version](https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-blue?style=for-the-badge)](LICENSE)
[![Platform](https://img.shields.io/badge/Platform-Cross--Platform-brightgreen?style=for-the-badge)](https://github.com/)

> 一個高性能的肌電圖 (EMG) 數據分析工具，支持 GUI 和命令行操作，專門用於處理和分析生物電信號數據。

## ✨ 主要特色

### 🎯 核心功能
- **多格式支持**: CSV 文件處理和分析
- **實時計算**: 高效的最大值和平均值計算
- **雙界面**: GUI 圖形界面和 CLI 命令行工具
- **多語言**: 完整的國際化支持 (中文/英文)
- **高性能**: 內建基準測試和性能監控

### 🏗️ 技術特色
- **模組化設計**: 清晰的代碼架構和組件分離
- **安全防護**: 路徑驗證、輸入清理、錯誤處理
- **結構化日誌**: 詳細的操作記錄和調試信息
- **配置靈活**: JSON 配置文件支持
- **測試完整**: 豐富的單元測試和基準測試

## 🛠️ 安裝與使用

### 快速開始

1. **克隆專案**
   `bash
   git clone <repository-url>
   cd count_mean
   `

2. **安裝依賴**
   `bash
   go mod download
   `

3. **運行 GUI 版本**
   `bash
   go run main.go
   `

4. **運行命令行版本**
   `bash
   go run main.go -cli
   `

### 配置設定

編輯 `config.json` 文件來自定義設定：

`json
{
  "scaling_factor": 10,
  "precision": 10,
  "language": "zh-TW",
  "log_level": "info"
}
`

## 📊 使用範例

### 基本 CSV 處理

`bash
# 處理單個文件
go run main.go -input data.csv -output result.csv

# 批量處理
go run main.go -batch -input ./input_dir -output ./output_dir
`

### 性能測試

`bash
# 運行性能基準測試
go run benchmark_test_main.go

# 只測試 CSV 處理性能
go run benchmark_test_main.go -csv-only

# 詳細輸出模式
go run benchmark_test_main.go -verbose
`

## 🏗️ 專案架構

`
count_mean/
├── main.go                    # 主程序入口
├── gui/                       # GUI 界面組件
│   ├── app.go                # Fyne 應用主體
│   └── components/           # UI 組件
├── internal/                  # 內部套件
│   ├── calculator/           # 計算引擎
│   ├── config/              # 配置管理
│   ├── csv/                 # CSV 處理
│   ├── logging/             # 日誌系統
│   ├── i18n/                # 國際化
│   ├── validator/           # 輸入驗證
│   └── benchmark/           # 性能測試
├── docs/                     # 文檔目錄
├── benchmark_*.go            # 性能測試工具
└── README.md                # 專案說明
`

## 🧪 測試與基準

### 運行測試

`bash
# 運行所有測試
go test ./...

# 運行特定測試
go test ./internal/calculator

# 包含基準測試
go test -bench=. ./...
`

### 性能基準測試

專案包含完整的性能測試套件：

- **CSV 處理測試**: 不同文件大小的讀取和處理性能
- **數學計算測試**: 計算引擎的執行效率
- **記憶體測試**: 記憶體使用和垃圾回收影響
- **併發測試**: 多 goroutine 並發處理能力

## 📖 API 文檔

詳細的 API 文檔請參考：
- [API 參考](docs/api.md)
- [架構說明](docs/architecture.md)
- [使用指南](docs/usage.md)

## 🌍 國際化支持

支持多語言界面：
- 繁體中文 (zh-TW)
- 簡體中文 (zh-CN)
- 英文 (en-US)

在 `config.json` 中設定 `language` 參數即可切換語言。

## 🔧 開發說明

### 代碼風格
- 遵循 Go 官方代碼規範
- 使用 `gofmt` 格式化代碼
- 包含完整的文檔註釋

### 貢獻指南
1. Fork 專案
2. 創建功能分支
3. 提交變更
4. 發起 Pull Request

## 📊 性能特色

- **高效算法**: 優化的數學計算引擎
- **記憶體管理**: 智能緩存和垃圾回收
- **並發處理**: 支持多核心並行計算
- **大文件支持**: 流式處理大型 CSV 文件

## 🛡️ 安全特色

- **路徑驗證**: 防止目錄遍歷攻擊
- **輸入清理**: 嚴格的數據驗證
- **錯誤處理**: 完善的異常捕捉機制
- **日誌審計**: 詳細的操作記錄

## 📜 授權條款

本專案使用 MIT 授權條款。詳情請參閱 [LICENSE](LICENSE) 文件。

## 🤝 貢獻與支持

歡迎提交 Issue 和 Pull Request！

---

**開發者**: [Your Name]  
**最後更新**: 2025-07-07  
**版本**: 1.0.0
