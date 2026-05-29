package models

import "time"

// PhaseManifest 分期總檔案記錄。一筆 PhaseManifest 代表一個受試者試次（subject trial）。
//
// 欄位語意：
//   - Subject：受試者 ID，作為 output 檔名前綴；經 filename.Sanitize 處理路徑分隔符與特殊字元
//   - MotionFile / ForceFile / EMGFile：相對於 DataFolder 的檔名，路徑解析走 manifest.ResolveEMGFile（lenient）
//   - EMGMotionOffset：EMG 第一筆樣本對應的 Motion index；用於 motion-time ↔ EMG-time 轉換
//     （見 synchronizer.TimeSynchronizer.MotionIndexToEMGTime）
//   - PhasePoints：10 個分期點，混合 force-time（OptFloat 秒，可區分 t=0 與「未提供」）
//     與 motion-index（int，0 仍為「未提供」sentinel），由 PhasePoint.IsMotionIndex 區分
//   - MuscleRatioFile：V.14 manifest 第 16 欄；filename only、相對 DataFolder、可空。
//     V.10 / V.13 manifest（15 欄）解析時為 ""，仍走 happy path。
//
// "未提供" 契約（Batch T 重構後）：
//   - 力板時間欄位 (P0/P1/P2/S/C/T0/T/L)：型別為 OptFloat，Set=false 表示未提供。
//     Set=true && Value=0 是合法的「t=0 真實時間」，與「NA / 空白 / x / -」嚴格區分。
//   - motion-index 欄位 (D/O)：型別仍為 int，0 仍代表「未提供」sentinel（任務範圍外）。
//   - parsers.GetPhaseValue 把兩種欄位都統一回傳成 OptFloat — 未提供 → NoOpt()。
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
	MuscleRatioFile string      // V.14 第 16 欄；filename only、相對 DataFolder、可空（V.10/V.13 manifest 為 ""）
}

// PhasePoints 分期點定義。
//
// Batch T 重構：8 個力板時間欄位改為 OptFloat，用 Set 旗標區分「未提供」與「t=0」。
// D 與 O 仍為 int（motion-index sentinel），暫保留 0 即未提供契約。
type PhasePoints struct {
	P0 OptFloat // 力板時間
	P1 OptFloat // 力板時間
	P2 OptFloat // 力板時間
	S  OptFloat // 啟動瞬間-力板時間
	C  OptFloat // 下蹲轉換-力板時間
	D  int      // 下蹲結束-motion index（0 = 未提供 sentinel）
	T0 OptFloat // 正沖涼結束-力板時間
	T  OptFloat // 起跳瞬間-力板時間
	O  int      // 展體轉間-motion index（0 = 未提供 sentinel）
	L  OptFloat // 著地瞬間-力板時間
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

// PhaseSyncValidationError 驗證錯誤 — phase sync / manifest 解析期間欄位驗證失敗。
//
// 原名 ValidationError 與 internal/errors.ValidationError 同名，跨 package
// 引用易讀錯（且 IDE / golangci-lint 不會擋）。改名為 PhaseSyncValidationError
// 後語意更聚焦：本型別只在 internal/parsers/phase_manifest_parser.go 的
// ValidatePhaseManifest 內使用，回傳給 internal/phase_sync 與 internal/cci。
type PhaseSyncValidationError struct {
	Field   string
	Message string
}

func (e PhaseSyncValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// SyncTime 同步時間信息.
type SyncTime struct {
	EMGTime   float64
	ForceTime float64
	MotionIdx int
	ValidAt   time.Time
}
