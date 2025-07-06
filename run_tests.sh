#!/bin/bash

# 測試運行腳本
# 用於執行重組後的測試文件

# 設置 Go 路徑
export PATH=~/sdk/go1.24.4/bin:$PATH

echo "🧪 開始執行測試文件重組後的測試"
echo "====================================="

# 檢查 Go 是否可用
if ! command -v go &> /dev/null; then
    echo "❌ Go 未找到，請確保 Go 已正確安裝"
    exit 1
fi

echo "✅ Go 版本: $(go version)"
echo ""

# 測試函數
run_test_category() {
    local category=$1
    local path=$2
    
    echo "📂 測試類別: $category"
    echo "📍 路徑: $path"
    
    if [ -d "$path" ]; then
        cd "$path" || return 1
        
        # 列出測試文件
        test_files=$(find . -name "*_test.go" | wc -l)
        echo "📄 找到 $test_files 個測試文件"
        
        # 嘗試編譯測試
        echo "🔨 編譯測試..."
        if go test -c -o /dev/null ./... 2>/dev/null; then
            echo "✅ 編譯成功"
            
            # 運行測試（如果編譯成功）
            echo "🏃 運行測試..."
            if go test -v ./...; then
                echo "✅ 測試通過"
            else
                echo "⚠️  測試運行有問題，但編譯成功"
            fi
        else
            echo "❌ 編譯失敗，嘗試列出錯誤..."
            go test -c ./... 2>&1 | head -20
        fi
        
        cd - > /dev/null
    else
        echo "❌ 路徑不存在: $path"
    fi
    
    echo ""
}

# 回到項目根目錄
cd /Users/wilson/IdeaProjects/count_mean

# 測試各個類別
echo "開始測試重組後的測試結構..."
echo ""

# 單元測試
run_test_category "單元測試 - 配置" "./test/unit/config"
run_test_category "單元測試 - 計算器" "./test/unit/calculator" 
run_test_category "單元測試 - CSV處理" "./test/unit/csv"
run_test_category "單元測試 - 錯誤處理" "./test/unit/errors"
run_test_category "單元測試 - 國際化" "./test/unit/i18n"
run_test_category "單元測試 - 日誌" "./test/unit/logging"
run_test_category "單元測試 - 安全" "./test/unit/security"
run_test_category "單元測試 - 工具" "./test/unit/util"
run_test_category "單元測試 - 驗證" "./test/unit/validation"

# 集成測試
run_test_category "集成測試" "./test/integration"

# 性能測試
run_test_category "性能基準測試" "./test/benchmark"

# 示例程序
if [ -d "./test/demo" ]; then
    echo "📂 測試類別: 示例程序"
    echo "📍 路徑: ./test/demo"
    demo_files=$(find ./test/demo -name "*.go" | wc -l)
    echo "📄 找到 $demo_files 個示例文件"
    
    for demo in ./test/demo/*.go; do
        if [ -f "$demo" ]; then
            echo "🔨 編譯示例: $(basename "$demo")"
            if go build -o /tmp/demo_test "$demo"; then
                echo "✅ 編譯成功: $(basename "$demo")"
            else
                echo "❌ 編譯失敗: $(basename "$demo")"
            fi
        fi
    done
    echo ""
fi

echo "====================================="
echo "🏁 測試結構驗證完成"
echo ""
echo "📋 測試組織結構總結:"
echo "   📂 test/"
echo "   ├── 📂 unit/          # 單元測試"
echo "   │   ├── 📂 calculator/ # 計算模組測試"
echo "   │   ├── 📂 config/     # 配置模組測試"
echo "   │   ├── 📂 csv/        # CSV處理測試"
echo "   │   ├── 📂 errors/     # 錯誤處理測試"
echo "   │   ├── 📂 i18n/       # 國際化測試"
echo "   │   ├── 📂 logging/    # 日誌模組測試"
echo "   │   ├── 📂 security/   # 安全模組測試"
echo "   │   ├── 📂 util/       # 工具函數測試"
echo "   │   └── 📂 validation/ # 驗證模組測試"
echo "   ├── 📂 integration/    # 集成測試"
echo "   ├── 📂 benchmark/      # 性能基準測試"
echo "   ├── 📂 demo/           # 示例程序"
echo "   └── 📂 testdata/       # 測試數據"
echo ""
echo "💡 使用方法:"
echo "   - 運行所有測試: go test ./test/..."
echo "   - 運行單元測試: go test ./test/unit/..."
echo "   - 運行特定模組: go test ./test/unit/config/"
echo "   - 運行性能測試: go test ./test/benchmark/"