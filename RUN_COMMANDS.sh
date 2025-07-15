#!/bin/bash

# EMG 資料分析工具 - 執行腳本

echo "========================================="
echo "EMG 資料分析工具 - Wails 應用程式"
echo "========================================="

# 設定 Go 路徑
export PATH=$HOME/sdk/go1.24.4/bin:$PATH

# 檢查 Go 是否安裝
if ! command -v go &> /dev/null; then
    echo "錯誤: Go 未找到。請確認 Go 安裝路徑："
    echo "當前查找路徑: $HOME/sdk/go1.24.4/bin"
    exit 1
fi

# 檢查 Wails 是否安裝
if ! command -v wails &> /dev/null; then
    echo "Wails 未安裝。正在安裝..."
    go install github.com/wailsapp/wails/v2/cmd/wails@v2.10.2
    
    # 確保 GOPATH/bin 在 PATH 中
    export PATH=$PATH:$(go env GOPATH)/bin
fi

# 選擇執行模式
echo ""
echo "請選擇執行模式："
echo "1) 開發模式 (支援熱重載)"
echo "2) 構建並執行"
echo "3) 直接用 Go 執行"
echo "4) 清理並重新下載依賴"
echo ""
read -p "請輸入選項 (1-4): " choice

case $choice in
    1)
        echo "啟動開發模式..."
        wails dev
        ;;
    2)
        echo "構建應用程式..."
        wails build
        echo "執行應用程式..."
        ./build/bin/EMG資料分析工具
        ;;
    3)
        echo "使用 Go 直接執行..."
        go run .
        ;;
    4)
        echo "清理模組快取..."
        go clean -modcache
        echo "重新下載依賴..."
        go mod download
        echo "完成！"
        ;;
    *)
        echo "無效選項"
        exit 1
        ;;
esac