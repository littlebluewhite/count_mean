// Package models provides data structures and business logic models
// for the EMG data analysis application.
package models

import (
	"context"
	"runtime"
	"sync"
	"time"

	"count_mean/internal/logging"
)

// backpressureLogger 是 NewBackpressureController 在偵測 zero-value fallback 時用的 logger。
// 為 testability 抽 var,測試可 override 把 log 寫到 buffer。
//
//nolint:gochecknoglobals // injection point for testing
var backpressureLogger = func() *logging.Logger {
	return logging.GetLogger("backpressure")
}

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
//
// 過去 normalize 只做了 CheckInterval 與 MaxMemoryMB,但 caller zero-value
// config 還可能踩到 MaxWorkers/MemoryThreshold/ThrottleThreshold = 0:
//   - MaxWorkers=0 → GetOptimalWorkerCount 回 0,worker pool 開不出來,job 全卡;
//   - MemoryThreshold=0 → isThrottled 永遠 true,GetMemoryUsageInfo 觀察值失真;
//   - ThrottleThreshold=0 → WaitForCapacity 第一句 `< 0` 直接 false,進入 ticker
//     loop 卡 100ms 才檢查一次,整體 throughput 大跌。
// 一併 sweep 並透過 backpressureLogger() 寫入 warn,讓使用者知道走 fallback。
// (註解過去說「log warn」但實作沒做,L17 修補)。
func NewBackpressureController(config *BackpressureConfig) *BackpressureController {
	if config == nil {
		config = DefaultBackpressureConfig()
	}

	logger := backpressureLogger()

	if config.CheckInterval <= 0 {
		logger.Warn("BackpressureConfig.CheckInterval <= 0,fallback 100ms 預設值", map[string]interface{}{
			"original": config.CheckInterval.String(),
		})
		config.CheckInterval = 100 * time.Millisecond
	}

	// MaxMemoryMB == 0 會讓 getMemoryUsageRatio 除以 0 變 +Inf,
	// WaitForCapacity 永遠 "超過記憶體上限" → 死鎖。退回 default 1GB 並 log warn。
	if config.MaxMemoryMB == 0 {
		logger.Warn("BackpressureConfig.MaxMemoryMB == 0,fallback default 1GB",
			map[string]interface{}{"fallback_mb": defaultMaxMemoryMB})
		config.MaxMemoryMB = defaultMaxMemoryMB
	}

	// sweep 其他 zero-value 欄位。
	if config.MaxWorkers <= 0 {
		fallback := runtime.NumCPU()
		logger.Warn("BackpressureConfig.MaxWorkers <= 0,fallback runtime.NumCPU()",
			map[string]interface{}{"original": config.MaxWorkers, "fallback": fallback})
		config.MaxWorkers = fallback
	}

	if config.MemoryThreshold <= 0 {
		logger.Warn("BackpressureConfig.MemoryThreshold <= 0,fallback default",
			map[string]interface{}{
				"original": config.MemoryThreshold,
				"fallback": defaultMemoryThreshold,
			})
		config.MemoryThreshold = defaultMemoryThreshold
	}

	if config.ThrottleThreshold <= 0 {
		logger.Warn("BackpressureConfig.ThrottleThreshold <= 0,fallback default",
			map[string]interface{}{
				"original": config.ThrottleThreshold,
				"fallback": defaultThrottleThreshold,
			})
		config.ThrottleThreshold = defaultThrottleThreshold
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
//
// # 命名語意警示
//
// **「WaitForCapacity」字面上像是「block 直到有可用容量再放行」，但實作只 gate
// 新任務的進入點 — 已經 in-flight 的 job 完全不受影響、不會被 pause 或 reset。
// 換句話說:**
//
//   - 本函式 == job admission throttle (gate-keeper):新 job 進入前先呼此函式
//     等到 memory ratio < ThrottleThreshold 再放行;期間若 ctx 取消即 return err。
//   - 本函式 != global memory pause:已開始執行的 worker (例如正在 ReadAll 一個
//     200MB CSV 的 sliding window worker) 不會因為這裡偵測到 memory 超標就被打斷。
//     它只是 "下一個 job 不能進來",不是 "整個 process pause"。
//
// 因此 caller 不能假設「呼 WaitForCapacity 成功 return 後 memory 已降下來」—
// 它只代表「memory 在 ratio < ThrottleThreshold 的瞬間,所以你的新 job 此刻可以
// 進場」。memory 何時實際降下來,取決於 in-flight job 各自完成的時間。
//
// 不改名為 TryReserveCapacity / AdmitNewJob 等更精確的名字,是因為:
//   (1) WaitForCapacity 是 public API,改名會讓既有 caller (cci / muscle_ratio /
//       calculator worker pool) 全部需要 migration
//   (2) 行為契約已透過此 docstring 釘住,reader 在 code review 時看到此處
//       即可理解;Rename 的 net benefit 不夠 cover blast radius。
//   (3) 既有測試 (backpressure_test.go) 已 lock 「ctx 取消立即回 err」「memory
//       降到 threshold 後 return nil」兩條主契約,行為不變。
//
// 若未來新增 caller 看到「WaitForCapacity」字面預期 stronger pause semantics,
// 此 docstring 是 single source of truth — 與 caller 對齊用此處說明。
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

// ActiveJobs 回傳目前未完成的任務數，供測試驗證
// RecordJobStart / RecordJobComplete 對稱性（reproducer 用）。
func (bc *BackpressureController) ActiveJobs() int {
	bc.mutex.RLock()
	defer bc.mutex.RUnlock()

	return bc.activeJobs
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
