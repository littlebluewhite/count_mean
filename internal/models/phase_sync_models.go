package models

import "time"

// PhaseManifest 分期總檔案記錄
type PhaseManifest struct {
	Subject         string      // 主題名稱
	MotionFile      string      // Motion檔案名
	ForceFile       string      // 力板檔案名
	EMGFile         string      // EMG檔案名
	EMGMotionOffset int         // EMG第一筆對應Motion的index
	PhasePoints     PhasePoints // 分期點數據
}

// PhasePoints 分期點定義
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

// AnalysisParams 分析參數
type AnalysisParams struct {
	ManifestFile string // 分期總檔案路徑
	DataFolder   string // 數據資料夾路徑
	StartPhase   string // 開始分期點
	EndPhase     string // 結束分期點
	SubjectIndex int    // 選擇的主題索引（從0開始）
}

// EMGStatistics EMG 統計結果
type EMGStatistics struct {
	Subject      string             // 主題名稱
	StartPhase   string             // 開始分期點
	StartTime    float64            // 開始時間（EMG時間）
	EndPhase     string             // 結束分期點
	EndTime      float64            // 結束時間（EMG時間）
	ChannelNames []string           // 通道名稱列表
	ChannelMeans map[string]float64 // 各通道平均值
	ChannelMaxes map[string]float64 // 各通道最大值
}

// PhaseSyncEMGData EMG數據結構（用於分期同步分析）
type PhaseSyncEMGData struct {
	Time     []float64            // 時間序列
	Channels map[string][]float64 // 通道名稱 -> 數據序列
	Headers  []string             // 通道順序
}

// MotionData Motion數據結構
type MotionData struct {
	Indices []int                // Index序列
	Data    map[string][]float64 // 數據列
	Headers []string             // 標題
}

// ForceData 力板數據結構
type ForceData struct {
	Time    []float64            // 時間序列
	Forces  map[string][]float64 // 力值數據
	Headers []string             // 標題
}

// PhaseTimeRange 分期時間範圍
type PhaseTimeRange struct {
	StartTime float64
	EndTime   float64
	StartType string // "force" or "motion"
	EndType   string // "force" or "motion"
}

// ValidationError 驗證錯誤
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// SyncTime 同步時間信息
type SyncTime struct {
	EMGTime   float64
	ForceTime float64
	MotionIdx int
	ValidAt   time.Time
}
