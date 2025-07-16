package gui

import (
	"context"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"sync"
	"time"
)

// ProgressManager 管理進度報告
type ProgressManager struct {
	logger       *logging.Logger
	mutex        sync.RWMutex
	currentInfo  *models.ProgressInfo
	subscribers  []chan models.ProgressInfo
	isActive     bool
	ctx          context.Context
	cancelFunc   context.CancelFunc
	lastUpdateAt time.Time
	updateBuffer time.Duration
}

// NewProgressManager 創建新的進度管理器
func NewProgressManager() *ProgressManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &ProgressManager{
		logger:       logging.GetLogger("progress_manager"),
		subscribers:  make([]chan models.ProgressInfo, 0),
		ctx:          ctx,
		cancelFunc:   cancel,
		updateBuffer: 100 * time.Millisecond, // 默認100ms更新間隔
	}
}

// SetUpdateBuffer 設置更新間隔緩衝
func (pm *ProgressManager) SetUpdateBuffer(duration time.Duration) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	pm.updateBuffer = duration
}

// Subscribe 訂閱進度更新
func (pm *ProgressManager) Subscribe() <-chan models.ProgressInfo {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// 創建帶緩衝的通道，避免阻塞
	ch := make(chan models.ProgressInfo, 10)
	pm.subscribers = append(pm.subscribers, ch)

	// 如果有當前進度信息，立即發送
	if pm.currentInfo != nil {
		select {
		case ch <- *pm.currentInfo:
		default:
			// 通道滿了，跳過這次更新
		}
	}

	pm.logger.Debug("新的進度訂閱者", map[string]interface{}{
		"subscriber_count": len(pm.subscribers),
	})

	return ch
}

// Unsubscribe 取消訂閱進度更新
func (pm *ProgressManager) Unsubscribe(ch <-chan models.ProgressInfo) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// 從訂閱者列表中移除
	for i, subscriber := range pm.subscribers {
		if subscriber == ch {
			// 關閉通道
			close(subscriber)

			// 從切片中移除
			pm.subscribers = append(pm.subscribers[:i], pm.subscribers[i+1:]...)
			break
		}
	}

	pm.logger.Debug("取消進度訂閱", map[string]interface{}{
		"subscriber_count": len(pm.subscribers),
	})
}

// UpdateProgress 更新進度（實現 ProgressCallback 接口）
func (pm *ProgressManager) UpdateProgress(info models.ProgressInfo) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// 檢查是否需要更新（基於時間間隔）
	now := time.Now()
	if pm.lastUpdateAt.Add(pm.updateBuffer).After(now) && info.Percentage < 100 {
		return
	}

	pm.lastUpdateAt = now
	pm.currentInfo = &info

	// 向所有訂閱者發送進度更新
	for i := len(pm.subscribers) - 1; i >= 0; i-- {
		subscriber := pm.subscribers[i]
		select {
		case subscriber <- info:
			// 成功發送
		default:
			// 通道滿了或已關閉，移除此訂閱者
			close(subscriber)
			pm.subscribers = append(pm.subscribers[:i], pm.subscribers[i+1:]...)
			pm.logger.Warn("移除無響應的進度訂閱者", map[string]interface{}{
				"remaining_subscribers": len(pm.subscribers),
			})
		}
	}

	pm.logger.Debug("進度更新", map[string]interface{}{
		"percentage":       info.Percentage,
		"status":           info.Status,
		"channel_index":    info.ChannelIndex,
		"subscriber_count": len(pm.subscribers),
	})
}

// GetCurrentProgress 獲取當前進度
func (pm *ProgressManager) GetCurrentProgress() *models.ProgressInfo {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	if pm.currentInfo != nil {
		// 返回副本，避免併發修改
		info := *pm.currentInfo
		return &info
	}

	return nil
}

// Start 開始進度追蹤
func (pm *ProgressManager) Start() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.isActive = true
	pm.logger.Info("進度管理器已啟動")
}

// Stop 停止進度追蹤
func (pm *ProgressManager) Stop() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.isActive = false

	// 關閉所有訂閱者通道
	for _, subscriber := range pm.subscribers {
		close(subscriber)
	}
	pm.subscribers = nil

	// 取消上下文
	pm.cancelFunc()

	pm.logger.Info("進度管理器已停止")
}

// IsActive 檢查進度管理器是否活躍
func (pm *ProgressManager) IsActive() bool {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	return pm.isActive
}

// GetSubscriberCount 獲取訂閱者數量
func (pm *ProgressManager) GetSubscriberCount() int {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()
	return len(pm.subscribers)
}

// CreateProgressCallback 創建進度回調函數
func (pm *ProgressManager) CreateProgressCallback() models.ProgressCallback {
	return pm.UpdateProgress
}

// SendCompletionNotification 發送完成通知
func (pm *ProgressManager) SendCompletionNotification(status string, totalTime time.Duration) {
	info := models.ProgressInfo{
		CurrentStep:   100,
		TotalSteps:    100,
		Percentage:    100,
		Status:        status,
		ChannelIndex:  0,
		ChannelName:   "",
		ElapsedTime:   totalTime.Round(time.Second).String(),
		EstimatedTime: "完成",
	}

	pm.UpdateProgress(info)
}

// SendErrorNotification 發送錯誤通知
func (pm *ProgressManager) SendErrorNotification(errorMsg string) {
	pm.mutex.RLock()
	currentInfo := pm.currentInfo
	pm.mutex.RUnlock()

	info := models.ProgressInfo{
		Status:        "錯誤: " + errorMsg,
		ChannelIndex:  0,
		ChannelName:   "",
		EstimatedTime: "已停止",
	}

	// 如果有當前進度，保持其他信息
	if currentInfo != nil {
		info.CurrentStep = currentInfo.CurrentStep
		info.TotalSteps = currentInfo.TotalSteps
		info.Percentage = currentInfo.Percentage
		info.ElapsedTime = currentInfo.ElapsedTime
	}

	pm.UpdateProgress(info)
}
