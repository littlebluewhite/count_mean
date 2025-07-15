# Golang 和 Wails 應用程式執行指南

## 前置需求

1. **安裝 Go** (版本 1.24.4 或更高)
   ```bash
   # macOS 使用 Homebrew
   brew install go
   
   # 或從官網下載
   # https://golang.org/dl/
   ```

2. **安裝 Wails CLI**
   ```bash
   export PATH=$HOME/sdk/go1.24.4/bin:$PATH
   go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
   ```

## 執行方式

### 直接運行構建好的應用程式

```bash
# 運行已構建的應用程式
./build/bin/count_mean.app/Contents/MacOS/EMG資料分析工具
```

### 使用執行腳本（推薦）

```bash
# 使用自動化腳本
./RUN_COMMANDS.sh
```

### 開發模式

1. **開發模式執行**
   ```bash
   # 設定環境變數
   export PATH=$HOME/sdk/go1.24.4/bin:$HOME/go/bin:$PATH
   
   # 開發模式
   wails dev
   ```

### 構建應用程式

1. **構建生產版本**
   ```bash
   # 設定環境變數
   export PATH=$HOME/sdk/go1.24.4/bin:$HOME/go/bin:$PATH
   
   # 構建 macOS 應用程式
   wails build
   ```

2. **構建輸出位置**
   - 構建的應用程式會在 `build/bin/count_mean.app` 目錄下

### 注意事項

- **重要**: 不要直接使用 `go run .` 運行 Wails 應用程式，必須使用 `wails build` 或 `wails dev`
- 應用程式需要正確的構建標籤才能運行

## 常見問題解決

### 1. "command not found: wails"
確保 Wails 已正確安裝並在 PATH 中：
```bash
export PATH=$HOME/sdk/go1.24.4/bin:$HOME/go/bin:$PATH
```

### 2. "main redeclared in this block"
確保沒有多個 main 函數衝突：
```bash
# 如果有衝突，備份不需要的文件
mv main_old.go main_old.go.bak
mv generate_docs.go generate_docs.go.bak
```

### 3. "Wails applications will not build without the correct build tags"
必須使用 Wails 命令構建，不能直接用 `go run`：
```bash
# 正確方式
wails build
# 或
wails dev
```

### 4. 模組相依性問題
```bash
# 清理並重新下載相依套件
go clean -modcache
go mod download
```

## 已完成的修復

✅ 修復了 `new_gui/app.go` 與 `main.go` 的整合問題
✅ 修復了 `internal/data/processor.go` 中的類型不匹配問題
✅ 修復了 CSV 讀寫函數的參數問題
✅ 修復了多個 main 函數衝突問題
✅ 成功構建並運行應用程式

## 應用程式功能

執行後，您可以使用以下功能：

1. **最大平均值計算** - 計算 EMG 數據的最大平均值
2. **資料標準化** - 使用參考值標準化數據
3. **資料做圖** - 使用 go-echarts 生成視覺化圖表
4. **階段分析** - 分析不同運動階段的數據
5. **系統配置** - 設定預設輸入/輸出/參考路徑

## 開發建議

1. 使用 `wails dev` 進行開發，支援熱重載
2. 修改前端代碼在 `frontend/src` 目錄
3. 修改後端邏輯在 `new_gui/app.go`
4. 配置文件為 `config.json`