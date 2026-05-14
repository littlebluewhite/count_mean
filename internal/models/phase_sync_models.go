package models

import "time"

// PhaseManifest 分期總檔案記錄。一筆 PhaseManifest 代表一個受試者試次（subject trial）。
//
// 欄位語意：
//   - Subject：受試者 ID，作為 output 檔名前綴；經 calculator.SanitizeFileName 處理路徑分隔符與特殊字元
//   - MotionFile / ForceFile / EMGFile：相對於 DataFolder 的檔名，路徑解析走 manifest.ResolveEMGFile（lenient）
//   - EMGMotionOffset：EMG 第一筆樣本對應的 Motion index；用於 motion-time ↔ EMG-time 轉換
//     （見 synchronizer.TimeSynchronizer.MotionIndexToEMGTime）
//   - PhasePoints：10 個分期點，混合 force-time（float64 秒）與 motion-index（int），由 PhasePoint.IsMotionIndex 區分
//
// 0 即空契約：
//   - PhasePoints 內任何欄位為 0（int=0 或 float64=0.0）視為「該分期點未標定，應跳過」
//   - 0 是合法的「空值」標記，不應誤判為「時間/index 為 0」的有效值
//   - parsers.GetPhaseValue 與 cci.getPhasePointDefs 都依此契約跳過 0 值
//
// EMGMotionOffset 定義：
//   - 正值：EMG 起點晚於 Motion 起點 N 個 motion frame
//   - 負值：EMG 起點早於 Motion 起點
//   - 0：EMG 與 Motion 對齊（罕見，多為 manifest 未填）
//
// 目前 consumer：
//   - internal/cci：CCI Rudolph 分析（fail-fast，per-subject）
//   - internal/muscle_ratio：肌肉比值批次分析（per-subject batch，partial-success）
//   - internal/phase_sync：分期同步分析
//   - internal/calculator (normalized phase sync)：標準化分期同步分析
//
// 改動本 struct 欄位或契約時，必須同步檢視 4 個 consumer。
type PhaseManifest struct {
	Subject         string      // 主題名稱
	MotionFile      string      // Motion檔案名
	ForceFile       string      // 力板檔案名
	EMGFile         string      // EMG檔案名
	EMGMotionOffset int         // EMG第一筆對應Motion的index
	PhasePoints     PhasePoints // 分期點數據
}

// PhasePoints 分期點定義.
type PhasePoints struct {
	P0 float64 // 力板時間
	P1 float64 // 力板時間
	P2 float64 // 力板時間
	S  float64 // 啟動瞬間-力板時間
	C  float64 // 下蹲轉換-力板時間
	D  int     // 下蹲結束-motion index
	T0 float64 // 正沖涼結束-力板時間
	T  float64 // 起跳瞬間-力板時間
	O  int     // 展體轉間-motion index
	L  float64 // 著地瞬間-力板時間
}

// AnalysisParams 分析參數.
type AnalysisParams struct {
	ManifestFile string     // 分期總檔案路徑
	DataFolder   string     // 數據資料夾路徑
	StartPhase   PhasePoint // 開始分期點
	EndPhase     PhasePoint // 結束分期點
	SubjectIndex int        // 選擇的主題索引（從0開始）
}

// EMGStatistics EMG 統計結果.
type EMGStatistics struct {
	Subject      string             // 主題名稱
	StartPhase   PhasePoint         // 開始分期點
	StartTime    float64            // 開始時間（EMG時間）
	EndPhase     PhasePoint         // 結束分期點
	EndTime      float64            // 結束時間（EMG時間）
	ChannelNames []string           // 通道名稱列表
	ChannelMeans map[string]float64 // 各通道平均值
	ChannelMaxes map[string]float64 // 各通道最大值
}

// PhaseSyncEMGData EMG數據結構（用於分期同步分析）.
type PhaseSyncEMGData struct {
	Time     []float64            // 時間序列
	Channels map[string][]float64 // 通道名稱 -> 數據序列
	Headers  []string             // 通道順序
}

// MotionData Motion數據結構.
type MotionData struct {
	Indices []int                // Index序列
	Data    map[string][]float64 // 數據列
	Headers []string             // 標題
}

// ForceData 力板數據結構.
type ForceData struct {
	Time    []float64            // 時間序列
	Forces  map[string][]float64 // 力值數據
	Headers []string             // 標題
}

// PhaseTimeRange 分期時間範圍.
type PhaseTimeRange struct {
	StartTime float64
	EndTime   float64
	StartType string // "force" or "motion"
	EndType   string // "force" or "motion"
}

// ValidationError 驗證錯誤.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// SyncTime 同步時間信息.
type SyncTime struct {
	EMGTime   float64
	ForceTime float64
	MotionIdx int
	ValidAt   time.Time
}
