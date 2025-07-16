package models

import (
	"runtime"
	"sync"
	"time"
)

// BackpressureConfig 代表背壓控制配置
type BackpressureConfig struct {
	MaxMemoryMB           uint64        `json:"max_memory_mb"`           // 最大記憶體使用量(MB)
	MaxWorkers            int           `json:"max_workers"`             // 最大工作協程數
	MemoryThreshold       float64       `json:"memory_threshold"`        // 記憶體使用率閾值 (0.0-1.0)
	ThrottleThreshold     float64       `json:"throttle_threshold"`      // 開始限流的記憶體使用率
	CheckInterval         time.Duration `json:"check_interval"`          // 檢查間隔
	WorkerReductionFactor float64       `json:"worker_reduction_factor"` // 工作協程減少因子
	GCInterval            time.Duration `json:"gc_interval"`             // 垃圾回收間隔
}

// DefaultBackpressureConfig 返回默認的背壓控制配置
func DefaultBackpressureConfig() *BackpressureConfig {
	return &BackpressureConfig{
		MaxMemoryMB:           1024, // 1GB
		MaxWorkers:            runtime.NumCPU(),
		MemoryThreshold:       0.8, // 80%
		ThrottleThreshold:     0.9, // 90%
		CheckInterval:         100 * time.Millisecond,
		WorkerReductionFactor: 0.5, // 減少50%工作協程
		GCInterval:            5 * time.Second,
	}
}

// BackpressureStats 代表背壓控制統計信息
type BackpressureStats struct {
	PeakMemoryUsage      uint64        `json:"peak_memory_usage"`       // 峰值記憶體使用量(bytes)
	AverageWorkers       float64       `json:"average_workers"`         // 平均工作協程數
	ThrottleEvents       int           `json:"throttle_events"`         // 限流事件次數
	TotalProcessingTime  time.Duration `json:"total_processing_time"`   // 總處理時間
	ThroughputJobsPerSec float64       `json:"throughput_jobs_per_sec"` // 吞吐量(任務/秒)
	GCTriggers           int           `json:"gc_triggers"`             // GC觸發次數
}

// BackpressureController 代表背壓控制器
type BackpressureController struct {
	config         *BackpressureConfig
	stats          BackpressureStats
	mutex          sync.RWMutex
	activeJobs     int
	startTime      time.Time
	lastGCTime     time.Time
	isActive       bool
	stopChan       chan struct{}
	workerCount    int
	totalJobs      int
	workerSumTotal float64 // 用於計算平均工作協程數
	checkCount     int     // 檢查次數，用於計算平均值
}

// NewBackpressureController 創建新的背壓控制器
func NewBackpressureController(config *BackpressureConfig) *BackpressureController {
	if config == nil {
		config = DefaultBackpressureConfig()
	}

	return &BackpressureController{
		config:      config,
		stopChan:    make(chan struct{}),
		workerCount: config.MaxWorkers,
		startTime:   time.Now(),
		lastGCTime:  time.Now(),
	}
}

// Start 啟動背壓控制器
func (bc *BackpressureController) Start() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	if bc.isActive {
		return
	}

	bc.isActive = true
	bc.startTime = time.Now()
	bc.lastGCTime = time.Now()

	// 啟動監控協程
	go bc.monitor()
}

// Stop 停止背壓控制器
func (bc *BackpressureController) Stop() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	if !bc.isActive {
		return
	}

	bc.isActive = false
	close(bc.stopChan)
}

// Reset 重置統計信息
func (bc *BackpressureController) Reset() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	bc.stats = BackpressureStats{}
	bc.activeJobs = 0
	bc.totalJobs = 0
	bc.workerSumTotal = 0
	bc.checkCount = 0
	bc.startTime = time.Now()
	bc.lastGCTime = time.Now()
	bc.workerCount = bc.config.MaxWorkers
}

// monitor 監控記憶體使用並調整工作協程數
func (bc *BackpressureController) monitor() {
	ticker := time.NewTicker(bc.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-bc.stopChan:
			return
		case <-ticker.C:
			bc.adjustWorkers()
			bc.maybeRunGC()
		}
	}
}

// adjustWorkers 根據記憶體使用情況調整工作協程數
func (bc *BackpressureController) adjustWorkers() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	memUsageRatio := bc.getMemoryUsageRatio()

	// 更新峰值記憶體使用量
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	if memStats.Alloc > bc.stats.PeakMemoryUsage {
		bc.stats.PeakMemoryUsage = memStats.Alloc
	}

	// 根據記憶體使用率調整工作協程數
	var newWorkerCount int
	if memUsageRatio >= bc.config.ThrottleThreshold {
		// 記憶體使用率超過90%，大幅減少工作協程
		newWorkerCount = max(1, int(float64(bc.config.MaxWorkers)*0.25))
		bc.stats.ThrottleEvents++
	} else if memUsageRatio >= bc.config.MemoryThreshold {
		// 記憶體使用率在80%-90%之間，適度減少工作協程
		newWorkerCount = max(1, int(float64(bc.config.MaxWorkers)*0.5))
	} else {
		// 記憶體使用率正常，使用完整工作協程數
		newWorkerCount = bc.config.MaxWorkers
	}

	bc.workerCount = newWorkerCount

	// 更新統計信息
	bc.workerSumTotal += float64(bc.workerCount)
	bc.checkCount++
	bc.stats.AverageWorkers = bc.workerSumTotal / float64(bc.checkCount)
}

// maybeRunGC 在需要時觸發垃圾回收
func (bc *BackpressureController) maybeRunGC() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()

	if time.Since(bc.lastGCTime) >= bc.config.GCInterval {
		runtime.GC()
		bc.lastGCTime = time.Now()
		bc.stats.GCTriggers++
	}
}

// getMemoryUsageRatio 獲取記憶體使用率
func (bc *BackpressureController) getMemoryUsageRatio() float64 {
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	maxMemoryBytes := bc.config.MaxMemoryMB * 1024 * 1024
	return float64(memStats.Alloc) / float64(maxMemoryBytes)
}

// GetMemoryUsageRatio 公開的記憶體使用率獲取方法
func (bc *BackpressureController) GetMemoryUsageRatio() float64 {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()
	return bc.getMemoryUsageRatio()
}

// IsThrottled 檢查是否正在限流
func (bc *BackpressureController) IsThrottled() bool {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()
	return bc.getMemoryUsageRatio() >= bc.config.MemoryThreshold
}

// GetOptimalWorkerCount 獲取當前最佳工作協程數
func (bc *BackpressureController) GetOptimalWorkerCount() int {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()
	return bc.workerCount
}

// WaitForCapacity 等待有足夠的容量處理新任務
func (bc *BackpressureController) WaitForCapacity() {
	for bc.getMemoryUsageRatio() >= bc.config.ThrottleThreshold {
		time.Sleep(bc.config.CheckInterval)
		runtime.Gosched() // 讓出CPU時間片
	}
}

// RecordJobStart 記錄任務開始
func (bc *BackpressureController) RecordJobStart() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()
	bc.activeJobs++
	bc.totalJobs++
}

// RecordJobComplete 記錄任務完成
func (bc *BackpressureController) RecordJobComplete() {
	bc.mutex.Lock()
	defer bc.mutex.Unlock()
	if bc.activeJobs > 0 {
		bc.activeJobs--
	}
}

// GetStats 獲取統計信息
func (bc *BackpressureController) GetStats() BackpressureStats {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	// 更新處理時間和吞吐量
	bc.stats.TotalProcessingTime = time.Since(bc.startTime)
	if bc.stats.TotalProcessingTime.Seconds() > 0 {
		bc.stats.ThroughputJobsPerSec = float64(bc.totalJobs) / bc.stats.TotalProcessingTime.Seconds()
	}

	return bc.stats
}

// GetActiveJobs 獲取當前活躍任務數
func (bc *BackpressureController) GetActiveJobs() int {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()
	return bc.activeJobs
}

// max 輔助函數，返回兩個整數中的較大值
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
