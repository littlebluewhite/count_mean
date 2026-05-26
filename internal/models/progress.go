package models

import (
	"time"
)

// ProgressInfo 代表處理進度信息.
type ProgressInfo struct {
	CurrentStep   int     `json:"current_step"`   // 當前步驟
	TotalSteps    int     `json:"total_steps"`    // 總步驟數
	Percentage    float64 `json:"percentage"`     // 完成百分比
	Status        string  `json:"status"`         // 當前狀態描述
	ChannelIndex  int     `json:"channel_index"`  // 當前處理的通道索引
	ChannelName   string  `json:"channel_name"`   // 當前處理的通道名稱
	ElapsedTime   string  `json:"elapsed_time"`   // 已用時間
	EstimatedTime string  `json:"estimated_time"` // 預估剩餘時間
}

// ProgressCallback 代表進度回調函數類型.
//
// 並行契約：callback 由 ProgressTracker 同步呼叫，且 ProgressTracker
// 自身要求 single-goroutine 使用（見 ProgressTracker doc）。callback 內部
// 若需把進度推播到其他 goroutine（例如 GUI），由 callback 實作者自行同步
// （例如 gui.ProgressManager 用 atomic 存最新進度）。
type ProgressCallback func(progress ProgressInfo)

// ProgressTracker 代表進度追蹤器。
//
// 並行契約（P1-A9-2）：本 struct 故意不加 mutex 或 atomic。所有 method
// 呼叫（包含 read-only getter GetElapsedTime / GetProgress / IsCompleted）
// 都必須在同一個 goroutine 內進行，避免在熱路徑（每筆 EMG channel 結果都會
// 呼叫 UpdateProgress）付出 lock 成本。原因：UpdateProgress 會 mutate
// currentStep / lastUpdateAt，這些 field 沒被 atomic 包裝，從其他 goroutine
// 並行讀會被 race detector 抓到（即使只是讀 int / time.Time 等 word-sized
// 型別在 ARM64 / weak memory model 仍可能 torn read）。
//
// 現有 caller 都滿足此契約：
//   - calculator.orchestrator：Start / UpdateProgress / Complete / GetElapsedTime
//     都在 execute / collectResults / finalize 同一個 goroutine 內。worker pool
//     透過 channel 把結果送回單一 collector，worker 本身從不 touch tracker。
//
// 若未來新增 caller 需要從多 goroutine 呼叫 ProgressTracker，必須改用 mutex
// 包裝（或將 currentStep 改為 atomic.Int64 + 對 lastUpdateAt/callback dispatch
// 加 mutex）。新增 caller 時請先回頭檢視這段契約。
type ProgressTracker struct {
	startTime    time.Time
	callback     ProgressCallback
	totalSteps   int
	currentStep  int
	lastUpdateAt time.Time
	updateBuffer time.Duration // 更新間隔緩衝，避免過於頻繁的回調
}

// NewProgressTracker 創建新的進度追蹤器.
//
// `lastUpdateAt` 保持 zero value（time.Time{}）— 舊版 `time.Now()`
// 初始化會與 `Start()` 立刻 `UpdateProgress(0, ...)` 形成「首筆被自身節流抑制」
// 的 bug（now.Sub(lastUpdateAt) < updateBuffer 立即成立）。zero value 讓
// `now.Sub(lastUpdateAt)` 永遠遠大於任何 updateBuffer,首筆必過。
func NewProgressTracker(totalSteps int, callback ProgressCallback) *ProgressTracker {
	return &ProgressTracker{
		startTime:    time.Now(),
		callback:     callback,
		totalSteps:   totalSteps,
		currentStep:  0,
		lastUpdateAt: time.Time{},            // 不能初始為 time.Now(),否則首筆被抑制
		updateBuffer: 100 * time.Millisecond, // 默認100ms更新間隔
	}
}

// SetUpdateBuffer 設置更新間隔緩衝.
func (pt *ProgressTracker) SetUpdateBuffer(duration time.Duration) {
	pt.updateBuffer = duration
}

// calculateEstimatedTime 計算預估剩餘時間.
//
// totalSteps == 0 直接視為「計算中...」 — 不能說「完成」(語意誤導,實際是
// caller 給 0 totalSteps 的退化情形,沒任何工作可做)。原本 step >= totalSteps
// 在 totalSteps == 0 時也會 trivially true,變成回「完成」誤導 GUI。
func (pt *ProgressTracker) calculateEstimatedTime(step int, elapsed time.Duration) string {
	if pt.totalSteps <= 0 {
		return "計算中..."
	}

	if step > 0 && step < pt.totalSteps {
		avgTimePerStep := elapsed / time.Duration(step)
		remainingSteps := pt.totalSteps - step
		estimatedRemaining := avgTimePerStep * time.Duration(remainingSteps)

		return estimatedRemaining.Round(time.Second).String()
	}

	if step >= pt.totalSteps {
		return "完成"
	}

	return "計算中..."
}

// buildProgressInfo 構建進度信息.
//
// 加 totalSteps == 0 guard — 過去 percentage = float64(step) / float64(0) * 100
// 會產出 +Inf / NaN (step==0 時) 被 encoding/json marshal 失敗 (Inf 不是 valid JSON
// number),導致 GUI progress callback 整條 payload 拋出。0 totalSteps 多半是 caller
// 邏輯 bug (e.g. 空 manifest 即啟動 tracker),這裡 special-case Percentage=0 並把
// EstimatedTime 標 "計算中..." 維持 fail-soft — caller 仍能拿到 valid info 結構繼續流。
func (pt *ProgressTracker) buildProgressInfo(
	step int, status string, channelIndex int, channelName string, elapsed time.Duration,
) ProgressInfo {
	var percentage float64
	if pt.totalSteps > 0 {
		percentage = float64(step) / float64(pt.totalSteps) * 100
		if percentage > 100 {
			percentage = 100
		}
	}

	return ProgressInfo{
		CurrentStep:   step,
		TotalSteps:    pt.totalSteps,
		Percentage:    percentage,
		Status:        status,
		ChannelIndex:  channelIndex,
		ChannelName:   channelName,
		ElapsedTime:   elapsed.Round(time.Second).String(),
		EstimatedTime: pt.calculateEstimatedTime(step, elapsed),
	}
}

// UpdateProgress 更新進度.
//
// throttle 行為：
//   - lastUpdateAt 是 zero value（Tracker 剛 New 出來,尚未 emit 過）→ 必過。
//   - step == 0(initial / Start 觸發的首筆) → 必過,讓「分析開始」這個訊號
//     可靠送到 callback。
//   - step >= totalSteps（完成）→ 必過,避免「99% 卡住」UX 退化。
//   - 其餘情況才按 updateBuffer 節流。
func (pt *ProgressTracker) UpdateProgress(step int, status string, channelIndex int, channelName string) {
	// 檢查是否需要更新（基於時間間隔）
	now := time.Now()
	isInitial := pt.lastUpdateAt.IsZero() || step == 0
	isFinal := step >= pt.totalSteps
	if !isInitial && !isFinal && now.Sub(pt.lastUpdateAt) < pt.updateBuffer {
		return
	}

	pt.currentStep = step
	pt.lastUpdateAt = now

	if pt.callback == nil {
		return
	}

	elapsed := now.Sub(pt.startTime)
	info := pt.buildProgressInfo(step, status, channelIndex, channelName, elapsed)
	pt.callback(info)
}

// Start 開始追蹤.
//
// Start 一定要 emit 一筆「進度從 0 開始」訊號讓 callback / GUI 知道
// 分析已啟動。UpdateProgress 內 step==0 與 lastUpdateAt.IsZero() 都是 bypass
// throttle 的條件,因此這裡直接呼叫即可,不需要額外旁路。
func (pt *ProgressTracker) Start(initialStatus string) {
	pt.UpdateProgress(0, initialStatus, 0, "")
}

// Complete 完成追蹤.
func (pt *ProgressTracker) Complete(finalStatus string) {
	pt.UpdateProgress(pt.totalSteps, finalStatus, 0, "")
}

// GetElapsedTime 獲取已用時間.
func (pt *ProgressTracker) GetElapsedTime() time.Duration {
	return time.Since(pt.startTime)
}

// GetProgress 獲取當前進度百分比.
func (pt *ProgressTracker) GetProgress() float64 {
	if pt.totalSteps == 0 {
		return 0
	}

	return float64(pt.currentStep) / float64(pt.totalSteps) * 100
}

// IsCompleted 檢查是否已完成.
func (pt *ProgressTracker) IsCompleted() bool {
	return pt.currentStep >= pt.totalSteps
}
