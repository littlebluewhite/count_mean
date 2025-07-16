package models

import (
	"time"
)

// ProgressInfo 代表處理進度信息
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

// ProgressCallback 代表進度回調函數類型
type ProgressCallback func(progress ProgressInfo)

// ProgressTracker 代表進度追蹤器
type ProgressTracker struct {
	startTime    time.Time
	callback     ProgressCallback
	totalSteps   int
	currentStep  int
	lastUpdateAt time.Time
	updateBuffer time.Duration // 更新間隔緩衝，避免過於頻繁的回調
}

// NewProgressTracker 創建新的進度追蹤器
func NewProgressTracker(totalSteps int, callback ProgressCallback) *ProgressTracker {
	return &ProgressTracker{
		startTime:    time.Now(),
		callback:     callback,
		totalSteps:   totalSteps,
		currentStep:  0,
		lastUpdateAt: time.Now(),
		updateBuffer: 100 * time.Millisecond, // 默認100ms更新間隔
	}
}

// SetUpdateBuffer 設置更新間隔緩衝
func (pt *ProgressTracker) SetUpdateBuffer(duration time.Duration) {
	pt.updateBuffer = duration
}

// UpdateProgress 更新進度
func (pt *ProgressTracker) UpdateProgress(step int, status string, channelIndex int, channelName string) {
	// 檢查是否需要更新（基於時間間隔）
	now := time.Now()
	if step < pt.totalSteps && now.Sub(pt.lastUpdateAt) < pt.updateBuffer {
		return
	}

	pt.currentStep = step
	pt.lastUpdateAt = now

	if pt.callback != nil {
		percentage := float64(step) / float64(pt.totalSteps) * 100
		if percentage > 100 {
			percentage = 100
		}

		elapsed := now.Sub(pt.startTime)
		var estimated string
		if step > 0 && step < pt.totalSteps {
			avgTimePerStep := elapsed / time.Duration(step)
			remainingSteps := pt.totalSteps - step
			estimatedRemaining := avgTimePerStep * time.Duration(remainingSteps)
			estimated = estimatedRemaining.Round(time.Second).String()
		} else if step >= pt.totalSteps {
			estimated = "完成"
		} else {
			estimated = "計算中..."
		}

		info := ProgressInfo{
			CurrentStep:   step,
			TotalSteps:    pt.totalSteps,
			Percentage:    percentage,
			Status:        status,
			ChannelIndex:  channelIndex,
			ChannelName:   channelName,
			ElapsedTime:   elapsed.Round(time.Second).String(),
			EstimatedTime: estimated,
		}

		pt.callback(info)
	}
}

// Start 開始追蹤
func (pt *ProgressTracker) Start(initialStatus string) {
	pt.UpdateProgress(0, initialStatus, 0, "")
}

// Complete 完成追蹤
func (pt *ProgressTracker) Complete(finalStatus string) {
	pt.UpdateProgress(pt.totalSteps, finalStatus, 0, "")
}

// GetElapsedTime 獲取已用時間
func (pt *ProgressTracker) GetElapsedTime() time.Duration {
	return time.Since(pt.startTime)
}

// GetProgress 獲取當前進度百分比
func (pt *ProgressTracker) GetProgress() float64 {
	if pt.totalSteps == 0 {
		return 0
	}
	return float64(pt.currentStep) / float64(pt.totalSteps) * 100
}

// IsCompleted 檢查是否已完成
func (pt *ProgressTracker) IsCompleted() bool {
	return pt.currentStep >= pt.totalSteps
}
