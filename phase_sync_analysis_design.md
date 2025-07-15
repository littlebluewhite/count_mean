# 分期同步分析功能設計文檔

## 功能概述
此功能用於同步分析三種不同採樣頻率的生物力學數據（Motion、EMG、力板），並根據用戶選擇的分期點計算 EMG 訊號的平均值和最大值。

## 系統架構

### 1. 模組劃分

```
internal/
├── phase_sync/                    # 分期同步分析主模組
│   ├── analyzer.go               # 主分析器
│   ├── config.go                 # 配置定義
│   └── models.go                 # 數據模型
├── parsers/                      # 檔案解析器模組
│   ├── phase_manifest_parser.go  # 分期總檔案解析器
│   ├── motion_parser.go          # Motion CSV 解析器
│   ├── emg_parser.go            # EMG CSV 解析器
│   └── anc_parser.go            # ANC 力板檔案解析器
├── synchronizer/                 # 數據同步模組
│   ├── time_sync.go             # 時間同步邏輯
│   └── phase_calculator.go      # 分期點計算
└── calculator/                   # 計算模組
    └── emg_statistics.go        # EMG 統計計算
```

### 2. 數據模型

```go
// 分期總檔案記錄
type PhaseManifest struct {
    Subject           string    // 主題名稱
    MotionFile       string    // Motion檔案名
    ForceFile        string    // 力板檔案名
    EMGFile          string    // EMG檔案名
    EMGMotionOffset  int       // EMG第一筆對應Motion的index
    PhasePoints      PhasePoints
}

// 分期點定義
type PhasePoints struct {
    P0  float64  // 力板時間
    P1  float64  // 力板時間
    P2  float64  // 力板時間
    S   float64  // 啟動瞬間-力板時間
    C   float64  // 下蹲轉換-力板時間
    D   int      // 下蹲結束-motion index
    T0  float64  // 正沖涼結束-力板時間
    T   float64  // 起跳瞬間-力板時間
    O   int      // 展體轉間-motion index
    L   float64  // 著地瞬間-力板時間
}

// 分析參數
type AnalysisParams struct {
    ManifestFile    string   // 分期總檔案路徑
    DataFolder      string   // 數據資料夾路徑
    StartPhase      string   // 開始分期點
    EndPhase        string   // 結束分期點
    SubjectIndex    int      // 選擇的主題索引
}

// EMG 統計結果
type EMGStatistics struct {
    StartPhase     string
    StartTime      float64
    EndPhase       string
    EndTime        float64
    ChannelMeans   map[string]float64  // 各通道平均值
    ChannelMaxes   map[string]float64  // 各通道最大值
}
```

### 3. 核心功能流程

#### 3.1 檔案解析流程
1. 解析分期總檔案，獲取檔案映射關係
2. 根據選擇的主題，載入對應的三個數據檔案
3. 解析 Motion CSV（250Hz）
4. 解析 EMG CSV（1000Hz）
5. 解析 ANC 力板檔案（1000Hz）

#### 3.2 時間同步流程
1. 確定基準時間點（力板和 Motion 同步開始）
2. 使用 EMGMotionOffset 計算 EMG 時間偏移
3. 建立三個數據源的時間映射關係

#### 3.3 分期點轉換
- 力板時間 → EMG 時間
- Motion index → EMG 時間
- 考慮不同採樣頻率的轉換

#### 3.4 統計計算
1. 根據開始和結束分期點確定時間範圍
2. 提取 EMG 數據段
3. 計算各通道的平均值和最大值
4. 輸出結果到 CSV

### 4. API 設計

```go
// 主要 API 函數
func AnalyzePhaseSyncData(params AnalysisParams) (*EMGStatistics, error)
func LoadPhaseManifest(filepath string) ([]PhaseManifest, error)
func FindDataFiles(folder, pattern string) ([]string, error)
func ExportStatistics(stats *EMGStatistics, outputPath string) error
```

### 5. 前端 UI 設計

新增按鈕：「分期同步分析」

UI 面板包含：
1. 分期總檔案選擇器（拖放區域）
2. 數據資料夾選擇器
3. 主題選擇下拉選單（根據分期總檔案內容動態生成）
4. 開始分期點選擇器（P0, P1, P2, S, C, D, T0, T, O, L）
5. 結束分期點選擇器（P1, P2, S, C, D, T0, T, O, L）
6. 執行分析按鈕
7. 結果預覽區域
8. 匯出 CSV 按鈕

### 6. 技術要點

1. **採樣頻率處理**
   - Motion: 250Hz (0.004s/sample)
   - EMG: 1000Hz (0.001s/sample)
   - 力板: 1000Hz (0.001s/sample)

2. **時間同步公式**
   ```
   EMG時間 = (Motion_index - 1) * 0.004 + EMG偏移時間
   力板時間 = (Motion_index - 1) * 0.004
   ```

3. **ANC 檔案解析**
   - 需要特殊的二進制解析器
   - 提取時間戳和力值數據

4. **錯誤處理**
   - 檔案不存在
   - 格式錯誤
   - 時間範圍無效
   - 分期點選擇邏輯錯誤

### 7. 輸出格式

CSV 輸出格式：
```
,Channel1,Channel2,Channel3,...
開始分期點,P0
開始時間,3.012
結束分期點,P2
結束時間,3.774
平均值,0.123,0.456,0.789,...
最大值,0.234,0.567,0.890,...
```