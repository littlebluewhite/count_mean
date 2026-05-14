# 測試目錄結構說明

## 目錄組織

### 📁 test/
測試相關文件的統一管理目錄

> **單元測試已搬回 `internal/<pkg>/` 同目錄（Go idiomatic same-package placement）**。本目錄下只保留跨 package 的測試（integration / benchmark / phase_sync_test / property / race）與輔助資源。

#### 📂 integration/
**整合測試** - 測試多個模組之間的協作
- 完整流程測試
- 跨模組功能驗證
- 實際場景模擬

#### 📂 benchmark/
**性能基準測試** - 性能測試和基準比較
- 性能測試程序
- 基準測試報告
- 性能監控工具

#### 📂 demo/
**演示程序** - 功能演示和教學範例
- 功能演示程序
- 使用範例
- 教學範例代碼

#### 📂 testdata/
**測試數據** - 測試用的數據文件
- CSV 測試文件
- 配置文件
- 預期結果文件

## 測試運行

### 運行所有測試
```bash
go test ./...
```

### 運行單元測試
```bash
go test ./internal/... ./gui/... ./util/...
```

### 運行整合測試
```bash
go test ./test/integration/...
```

### 運行性能測試
```bash
go test -bench=. ./test/benchmark/...
```

### 運行演示程序
```bash
go run ./test/demo/<demo_name>.go
```

## 測試原則

1. **分離關注點**: 不同類型的測試分別組織
2. **模組化**: 按功能模組組織測試
3. **可重複**: 測試結果可重複且可靠
4. **文檔化**: 每個測試都有清晰的說明
5. **數據分離**: 測試數據與測試邏輯分離