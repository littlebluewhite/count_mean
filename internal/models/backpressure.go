// Package models provides data structures and business logic models
// for the EMG data analysis application.
package models

import (
	"context"
	"runtime"
	"sync"
	"time"
)

// Backpressure controller constants.
const (
	defaultMaxMemoryMB           = 1024 // 1GB
	defaultMemoryThreshold       = 0.8  // 80%
	defaultThrottleThreshold     = 0.9  // 90%
	defaultWorkerReductionFactor = 0.5  // 減少50%工作協程
	defaultGCIntervalSeconds     = 5
)

// BackpressureConfig 代表背壓控制配置.
type BackpressureConfig struct {
	MaxMemoryMB           uint64        `json:"max_memory_mb"`           // 最大記憶體使用量(MB)
	MaxWorkers            int           `json:"max_workers"`             // 最大工作協程數
	MemoryThreshold       float64       `json:"memory_threshold"`        // 記憶體使用率閾值 (0.0-1.0)
	ThrottleThreshold     float64       `json:"throttle_threshold"`      // 開始限流的記憶體使用率
	CheckInterval         time.Duration `json:"check_interval"`          // 檢查間隔
	WorkerReductionFactor float64       `json:"worker_reduction_factor"` // 工作協程減少因子
	GCInterval            time.Duration `json:"gc_interval"`             // 垃圾回收間隔
}

// DefaultBackpressureConfig 返回默認的背壓控制配置.
func DefaultBackpressureConfig() *BackpressureConfig {
	return &BackpressureConfig{
		MaxMemoryMB:           defaultMaxMemoryMB,
		MaxWorkers:            runtime.NumCPU(),
		MemoryThreshold:       defaultMemoryThreshold,
		ThrottleThreshold:     defaultThrottleThreshold,
		CheckInterval:         100 * time.Millisecond,
		WorkerReductionFactor: defaultWorkerReductionFactor,
		GCInterval:            defaultGCIntervalSeconds * time.Second,
	}
}

// BackpressureStats 代表背壓控制統計信息.
//
// 監控生命週期（Start/Stop/monitor/adjustWorkers/maybeRunGC）移除後，
// PeakMemoryUsage / AverageWorkers / ThrottleEvents / GCTriggers 四個欄位
// 失去生產者，對外永遠回 0，徒增 telemetry 噪音與認知負擔。
// Wave 6 review 後一併刪除（frontend grep 確認無消費者）。
type BackpressureStats struct {
	TotalProcessingTime  time.Duration `json:"total_processing_time"`   // 總處理時間
	ThroughputJobsPerSec float64       `json:"throughput_jobs_per_sec"` // 吞吐量(任務/秒)
}

// BackpressureController 代表背壓控制器.
// 移除監控生命週期後 isActive / stopChan / lastGCTime / workerSumTotal /
// checkCount 都無 reader，一併刪除欄位。
type BackpressureController struct {
	config      *BackpressureConfig
	stats       BackpressureStats
	mutex       sync.RWMutex
	activeJobs  int
	startTime   time.Time
	workerCount int
	totalJobs   int
}

// NewBackpressureController 創建新的背壓控制器.
//
// CheckInterval <= 0 會被 normalize 為預設值：WaitForCapacity 用 time.NewTicker，
// 對 non-positive interval 會 panic（"non-positive interval for NewTicker"）；
// caller 傳 zero-value config（例如自行 new BackpressureConfig 但沒設這欄）時
// 應拿到 sane default 而非 crash（codex Wave 6 second-pass P3）。
func NewBackpressureController(config *BackpressureConfig) *BackpressureController {
	if config == nil {
		config = DefaultBackpressureConfig()
	}

	if config.CheckInterval <= 0 {
		config.CheckInterval = 100 * time.Millisecond
	}

	return &BackpressureController{
		config:      config,
		workerCount: config.MaxWorkers,
		startTime:   time.Now(),
	}
}

// Reset 重置統計信息。
// 完整監控生命週期（Start/Stop/monitor/adjustWorkers/maybeRunGC）已移除：
// 整套從未在 production code path 被呼叫；adjustWorkers 動態調整 worker 的邏輯
// 是「實作了但沒接上」的死碼，maybeRunGC 內的 runtime.GC() 也屬於 hot path
// latency spike 來源。保留 Reset 與 GetOptimalWorkerCount 作為仍有用的工具。
func (bc *BackpressureController) Reset() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	bc.stats = BackpressureStats{}
	bc.activeJobs = 0
	bc.totalJobs = 0
	bc.startTime = time.Now()
	bc.workerCount = bc.config.MaxWorkers
}

// getMemoryUsageRatio 獲取記憶體使用率.
func (bc *BackpressureController) getMemoryUsageRatio() float64 {
	var memStats runtime.MemStats

	runtime.ReadMemStats(&memStats)

	maxMemoryBytes := bc.config.MaxMemoryMB * 1024 * 1024

	return float64(memStats.Alloc) / float64(maxMemoryBytes)
}

// memoryUsageRatio RLock-protected 記憶體使用率獲取方法（GetMemoryUsageInfo 內部用）.
func (bc *BackpressureController) memoryUsageRatio() float64 {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	return bc.getMemoryUsageRatio()
}

// isThrottled RLock-protected 檢查是否正在限流（GetMemoryUsageInfo 內部用）.
func (bc *BackpressureController) isThrottled() bool {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	return bc.getMemoryUsageRatio() >= bc.config.MemoryThreshold
}

// GetOptimalWorkerCount 獲取當前最佳工作協程數.
func (bc *BackpressureController) GetOptimalWorkerCount() int {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	return bc.workerCount
}

// WaitForCapacity 等待有足夠的容量處理新任務。
// 若 ctx 在等待期間被取消，立即回傳 ctx.Err()；正常結束回傳 nil。
//
// 用 Ticker（而非 time.After）避免 ctx 取消時 leak 未觸發的 timer：
// time.After 直到觸發前都留在 runtime time wheel，若 CheckInterval 較長且
// ctx 反覆取消重建，會堆積 timer goroutine（Wave 6 review P2）。
func (bc *BackpressureController) WaitForCapacity(ctx context.Context) error {
	if bc.getMemoryUsageRatio() < bc.config.ThrottleThreshold {
		return nil
	}

	ticker := time.NewTicker(bc.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if bc.getMemoryUsageRatio() < bc.config.ThrottleThreshold {
				return nil
			}
			runtime.Gosched() // 讓出CPU時間片
		}
	}
}

// RecordJobStart 記錄任務開始.
func (bc *BackpressureController) RecordJobStart() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	bc.activeJobs++
	bc.totalJobs++
}

// RecordJobComplete 記錄任務完成.
func (bc *BackpressureController) RecordJobComplete() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	if bc.activeJobs > 0 {
		bc.activeJobs--
	}
}

// GetStats 獲取統計信息.
// 衍生欄位（TotalProcessingTime、ThroughputJobsPerSec）只計算到回傳值中，
// 不寫回 bc.stats，避免在 RLock 下污染共用狀態（多 reader 會 race）。
func (bc *BackpressureController) GetStats() BackpressureStats {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	snapshot := bc.stats
	snapshot.TotalProcessingTime = time.Since(bc.startTime)
	if snapshot.TotalProcessingTime.Seconds() > 0 {
		snapshot.ThroughputJobsPerSec = float64(bc.totalJobs) / snapshot.TotalProcessingTime.Seconds()
	}

	return snapshot
}

// GetMemoryUsageInfo 獲取記憶體使用信息（用於日誌記錄）.
func (bc *BackpressureController) GetMemoryUsageInfo() map[string]interface{} {
	var memStats runtime.MemStats

	runtime.ReadMemStats(&memStats)

	info := map[string]interface{}{
		"alloc_mb":       memStats.Alloc / 1024 / 1024,
		"total_alloc_mb": memStats.TotalAlloc / 1024 / 1024,
		"sys_mb":         memStats.Sys / 1024 / 1024,
		"num_gc":         memStats.NumGC,
		"usage_ratio":    bc.memoryUsageRatio(),
		"is_throttled":   bc.isThrottled(),
	}

	return info
}
