package main

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DocumentationGenerator 文檔生成器
type DocumentationGenerator struct {
	projectRoot string
	fset        *token.FileSet
	packages    map[string]*ast.Package
	docs        map[string]*doc.Package
}

// NewDocumentationGenerator 創建新的文檔生成器
func NewDocumentationGenerator(projectRoot string) *DocumentationGenerator {
	return &DocumentationGenerator{
		projectRoot: projectRoot,
		fset:        token.NewFileSet(),
		packages:    make(map[string]*ast.Package),
		docs:        make(map[string]*doc.Package),
	}
}

// parsePackages 解析所有包
func (dg *DocumentationGenerator) parsePackages() error {
	return filepath.Walk(dg.projectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 跳過 vendor、.git 等目錄
		if info.IsDir() && (strings.Contains(path, "vendor") ||
			strings.Contains(path, ".git") ||
			strings.Contains(path, "node_modules")) {
			return filepath.SkipDir
		}

		// 只處理 .go 文件，但跳過測試文件和生成的文件
		if !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") ||
			strings.Contains(path, "generated") {
			return nil
		}

		dir := filepath.Dir(path)
		relDir, err := filepath.Rel(dg.projectRoot, dir)
		if err != nil {
			return err
		}

		// 解析目錄中的包
		if _, exists := dg.packages[relDir]; !exists {
			pkgs, err := parser.ParseDir(dg.fset, dir, nil, parser.ParseComments)
			if err != nil {
				return nil // 跳過無法解析的文件
			}

			for _, pkg := range pkgs {
				if !strings.HasSuffix(pkg.Name, "_test") {
					dg.packages[relDir] = pkg
					dg.docs[relDir] = doc.New(pkg, "./", doc.AllDecls)
					break
				}
			}
		}

		return nil
	})
}

// generateMarkdownDocs 生成 Markdown 文檔
func (dg *DocumentationGenerator) generateMarkdownDocs() error {
	// 創建文檔目錄
	docsDir := filepath.Join(dg.projectRoot, "docs")
	if err := os.MkdirAll(docsDir, 0755); err != nil {
		return err
	}

	// 生成主文檔
	if err := dg.generateMainReadme(); err != nil {
		return err
	}

	// 生成 API 文檔
	if err := dg.generateAPIDocumentation(docsDir); err != nil {
		return err
	}

	// 生成架構文檔
	if err := dg.generateArchitectureDoc(docsDir); err != nil {
		return err
	}

	// 生成使用指南
	if err := dg.generateUsageGuide(docsDir); err != nil {
		return err
	}

	fmt.Println("📚 文檔生成完成！")
	fmt.Printf("   主文檔: %s\n", filepath.Join(dg.projectRoot, "README.md"))
	fmt.Printf("   API文檔: %s\n", filepath.Join(docsDir, "api.md"))
	fmt.Printf("   架構文檔: %s\n", filepath.Join(docsDir, "architecture.md"))
	fmt.Printf("   使用指南: %s\n", filepath.Join(docsDir, "usage.md"))

	return nil
}

// generateMainReadme 生成主 README.md
func (dg *DocumentationGenerator) generateMainReadme() error {
	readmePath := filepath.Join(dg.projectRoot, "README.md")

	content := `# EMG 數據分析工具 🧠⚡

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
   ` + "`" + `bash
   git clone <repository-url>
   cd count_mean
   ` + "`" + `

2. **安裝依賴**
   ` + "`" + `bash
   go mod download
   ` + "`" + `

3. **運行 GUI 版本**
   ` + "`" + `bash
   go run main.go
   ` + "`" + `

4. **運行命令行版本**
   ` + "`" + `bash
   go run main.go -cli
   ` + "`" + `

### 配置設定

編輯 ` + "`" + `config.json` + "`" + ` 文件來自定義設定：

` + "`" + `json
{
  "scaling_factor": 10,
  "precision": 10,
  "language": "zh-TW",
  "log_level": "info"
}
` + "`" + `

## 📊 使用範例

### 基本 CSV 處理

` + "`" + `bash
# 處理單個文件
go run main.go -input data.csv -output result.csv

# 批量處理
go run main.go -batch -input ./input_dir -output ./output_dir
` + "`" + `

### 性能測試

` + "`" + `bash
# 運行性能基準測試
go run benchmark_test_main.go

# 只測試 CSV 處理性能
go run benchmark_test_main.go -csv-only

# 詳細輸出模式
go run benchmark_test_main.go -verbose
` + "`" + `

## 🏗️ 專案架構

` + "`" + `
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
` + "`" + `

## 🧪 測試與基準

### 運行測試

` + "`" + `bash
# 運行所有測試
go test ./...

# 運行特定測試
go test ./internal/calculator

# 包含基準測試
go test -bench=. ./...
` + "`" + `

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

在 ` + "`" + `config.json` + "`" + ` 中設定 ` + "`" + `language` + "`" + ` 參數即可切換語言。

## 🔧 開發說明

### 代碼風格
- 遵循 Go 官方代碼規範
- 使用 ` + "`" + `gofmt` + "`" + ` 格式化代碼
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
**最後更新**: ` + time.Now().Format("2006-01-02") + `  
**版本**: 1.0.0
`

	return os.WriteFile(readmePath, []byte(content), 0644)
}

// generateAPIDocumentation 生成 API 文檔
func (dg *DocumentationGenerator) generateAPIDocumentation(docsDir string) error {
	apiPath := filepath.Join(docsDir, "api.md")

	var content strings.Builder
	content.WriteString("# API 文檔\n\n")
	content.WriteString("## 概述\n\n")
	content.WriteString("EMG 數據分析工具的 API 文檔，包含所有公開的函數、結構體和接口。\n\n")

	// 按包名排序
	var sortedPkgs []string
	for pkgPath := range dg.docs {
		if pkgPath != "." { // 跳過根目錄
			sortedPkgs = append(sortedPkgs, pkgPath)
		}
	}
	sort.Strings(sortedPkgs)

	for _, pkgPath := range sortedPkgs {
		docPkg := dg.docs[pkgPath]
		if docPkg == nil {
			continue
		}

		content.WriteString(fmt.Sprintf("## 套件: %s\n\n", docPkg.Name))

		if docPkg.Doc != "" {
			content.WriteString(fmt.Sprintf("%s\n\n", docPkg.Doc))
		}

		// 類型文檔
		if len(docPkg.Types) > 0 {
			content.WriteString("### 類型定義\n\n")
			for _, typ := range docPkg.Types {
				content.WriteString(fmt.Sprintf("#### %s\n\n", typ.Name))
				if typ.Doc != "" {
					content.WriteString(fmt.Sprintf("%s\n\n", typ.Doc))
				}
				content.WriteString("```go\n")
				content.WriteString(fmt.Sprintf("type %s %s\n", typ.Name, typ.Decl))
				content.WriteString("```\n\n")

				// 方法文檔
				if len(typ.Methods) > 0 {
					content.WriteString("##### 方法\n\n")
					for _, method := range typ.Methods {
						content.WriteString(fmt.Sprintf("**%s**\n\n", method.Name))
						if method.Doc != "" {
							content.WriteString(fmt.Sprintf("%s\n\n", method.Doc))
						}
					}
				}
			}
		}

		// 函數文檔
		if len(docPkg.Funcs) > 0 {
			content.WriteString("### 函數\n\n")
			for _, fn := range docPkg.Funcs {
				content.WriteString(fmt.Sprintf("#### %s\n\n", fn.Name))
				if fn.Doc != "" {
					content.WriteString(fmt.Sprintf("%s\n\n", fn.Doc))
				}
			}
		}

		content.WriteString("---\n\n")
	}

	return os.WriteFile(apiPath, []byte(content.String()), 0644)
}

// generateArchitectureDoc 生成架構文檔
func (dg *DocumentationGenerator) generateArchitectureDoc(docsDir string) error {
	archPath := filepath.Join(docsDir, "architecture.md")

	content := `# 系統架構文檔

## 整體架構

EMG 數據分析工具採用模組化設計，將功能分離為獨立的套件，便於維護和擴展。

## 核心模組

### 1. 主程序 (main.go)
- 應用程序入口點
- 命令行參數處理
- GUI/CLI 模式選擇

### 2. GUI 模組 (gui/)
- **app.go**: Fyne 應用程序主體
- **components/**: UI 組件庫
- 負責用戶界面和交互邏輯

### 3. 計算引擎 (internal/calculator)
- **MaxMeanCalculator**: 核心計算類
- **算法實現**: 高效的數學運算
- **數據處理**: 支持各種數據格式

### 4. CSV 處理 (internal/csv)
- **CSVHandler**: CSV 文件讀寫
- **數據驗證**: 輸入數據格式檢查
- **錯誤處理**: 完善的異常處理機制

### 5. 配置管理 (internal/config)
- **AppConfig**: 應用程序配置結構
- **配置載入**: JSON 配置文件處理
- **默認設定**: 合理的默認值

### 6. 日誌系統 (internal/logging)
- **結構化日誌**: JSON 格式日誌輸出
- **多級別**: Debug, Info, Warn, Error
- **文件輪轉**: 自動日誌文件管理

### 7. 國際化 (internal/i18n)
- **多語言支持**: zh-TW, zh-CN, en-US
- **動態切換**: 運行時語言切換
- **本地化**: 日期、數字格式本地化

### 8. 輸入驗證 (internal/validator)
- **路徑驗證**: 防止目錄遍歷攻擊
- **數據清理**: 輸入數據淨化
- **安全檢查**: 多層安全驗證

### 9. 性能測試 (internal/benchmark)
- **Benchmarker**: 性能測試框架
- **CSVBenchmarks**: CSV 處理性能測試
- **報告生成**: 詳細的性能報告

## 數據流程

` + "`" + `
用戶輸入 -> 輸入驗證 -> CSV處理 -> 計算引擎 -> 結果輸出
    |          |           |          |           |
    v          v           v          v           v
  GUI/CLI -> Validator -> CSVHandler -> Calculator -> 文件/界面
` + "`" + `

## 安全機制

### 1. 輸入驗證
- 路徑規範化和驗證
- 文件類型檢查
- 數據格式驗證

### 2. 錯誤處理
- 分層錯誤處理
- 詳細錯誤日誌
- 優雅的錯誤恢復

### 3. 資源管理
- 自動資源清理
- 記憶體使用監控
- 文件句柄管理

## 性能優化

### 1. 算法優化
- 高效的數學運算
- 批量數據處理
- 記憶體友好的數據結構

### 2. 並發處理
- Goroutine 池管理
- Channel 通信
- 同步機制

### 3. 緩存策略
- 計算結果緩存
- 配置文件緩存
- 智能預載入

## 擴展性設計

### 1. 插件架構
- 介面導向設計
- 鬆耦合組件
- 動態載入機制

### 2. 配置驅動
- 外部配置文件
- 運行時參數調整
- 環境適應性

### 3. 模組化
- 獨立功能模組
- 清晰的依賴關係
- 易於測試和維護
`

	return os.WriteFile(archPath, []byte(content), 0644)
}

// generateUsageGuide 生成使用指南
func (dg *DocumentationGenerator) generateUsageGuide(docsDir string) error {
	usagePath := filepath.Join(docsDir, "usage.md")

	content := `# 使用指南

## 快速開始

### 1. 安裝需求

確保您的系統已安裝：
- Go 1.24 或更高版本
- Git (用於克隆專案)

### 2. 下載和安裝

` + "`" + `bash
# 克隆專案
git clone <repository-url>
cd count_mean

# 下載依賴
go mod download

# 編譯程序
go build -o emg_tool main.go
` + "`" + `

## GUI 模式使用

### 啟動 GUI

` + "`" + `bash
# 直接運行
go run main.go

# 或使用編譯後的程序
./emg_tool
` + "`" + `

### GUI 操作步驟

1. **選擇輸入文件**：點擊「選擇文件」按鈕
2. **設定參數**：調整縮放因子和精度
3. **開始分析**：點擊「開始分析」按鈕
4. **查看結果**：在結果區域查看計算結果
5. **保存結果**：點擊「保存結果」按鈕

## 命令行模式使用

### 基本語法

` + "`" + `bash
go run main.go -cli [選項]
` + "`" + `

### 常用選項

` + "`" + `
-input    <文件路徑>    指定輸入 CSV 文件
-output   <文件路徑>    指定輸出文件 (可選)
-scaling  <數值>       設定縮放因子 (默認: 10)
-precision <數值>      設定精度 (默認: 10)
-config   <配置文件>   指定配置文件路徑
-verbose              詳細輸出模式
-help                 顯示幫助信息
` + "`" + `

### 使用範例

` + "`" + `bash
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
` + "`" + `

## 配置文件

### 配置結構

創建 ` + "`" + `config.json` + "`" + ` 文件：

` + "`" + `json
{
  "scaling_factor": 10,
  "precision": 10,
  "language": "zh-TW",
  "log_level": "info",
  "max_file_size": 104857600,
  "concurrent_workers": 4,
  "cache_enabled": true
}
` + "`" + `

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

` + "`" + `bash
# 完整性能測試套件
go run benchmark_test_main.go

# 只測試 CSV 處理
go run benchmark_test_main.go -csv-only

# 詳細輸出模式
go run benchmark_test_main.go -verbose

# 自定義報告目錄
go run benchmark_test_main.go -report-dir ./my_reports
` + "`" + `

### 查看測試報告

測試完成後，報告文件會保存在指定目錄中：

` + "`" + `
benchmark_reports/
├── csv_benchmark_report_20240101_120000.json
├── system_benchmark_report_20240101_120100.json
├── memory_benchmark_report_20240101_120200.json
└── concurrency_benchmark_report_20240101_120300.json
` + "`" + `

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

日誌文件位於 ` + "`" + `logs/` + "`" + ` 目錄：

` + "`" + `bash
# 查看最新日誌
tail -f logs/app.log

# 查看錯誤日誌
grep "ERROR" logs/app.log
` + "`" + `

## 高級功能

### 批量處理

` + "`" + `bash
# 處理目錄中所有 CSV 文件
go run main.go -cli -batch -input ./data_dir -output ./results_dir
` + "`" + `

### 自定義算法

實現 ` + "`" + `Calculator` + "`" + ` 接口來添加自定義算法：

` + "`" + `go
type Calculator interface {
    Calculate(data [][]float64) (float64, float64, error)
    CalculateWithWindow(data [][]float64, windowSize int) ([]float64, []float64, error)
}
` + "`" + `

### 擴展 GUI

在 ` + "`" + `gui/components/` + "`" + ` 目錄中添加新的 UI 組件：

` + "`" + `go
type CustomWidget struct {
    widget.BaseWidget
    // 自定義屬性
}
` + "`" + `

## 最佳實踐

1. **數據準備**：確保 CSV 數據格式正確
2. **參數調優**：根據數據特性調整參數
3. **性能監控**：定期運行基準測試
4. **日誌管理**：定期清理舊日誌文件
5. **配置備份**：備份重要配置文件
`

	return os.WriteFile(usagePath, []byte(content), 0644)
}

// main 函數
func main() {
	projectRoot, err := os.Getwd()
	if err != nil {
		fmt.Printf("❌ 無法獲取當前目錄: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("🚀 開始生成項目文檔...")

	// 創建文檔生成器
	dg := NewDocumentationGenerator(projectRoot)

	// 解析所有套件
	fmt.Print("📖 解析代碼套件... ")
	if err := dg.parsePackages(); err != nil {
		fmt.Printf("❌ 失敗: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 完成")

	// 生成文檔
	fmt.Print("📝 生成 Markdown 文檔... ")
	if err := dg.generateMarkdownDocs(); err != nil {
		fmt.Printf("❌ 失敗: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 完成")

	fmt.Println("\n🎉 任務 9 - 代碼文檔完善 已完成！")
	fmt.Println("\n生成的文檔包括:")
	fmt.Println("  📄 README.md - 項目主文檔")
	fmt.Println("  📁 docs/api.md - API 參考文檔")
	fmt.Println("  🏗️ docs/architecture.md - 系統架構文檔")
	fmt.Println("  📖 docs/usage.md - 詳細使用指南")
}
