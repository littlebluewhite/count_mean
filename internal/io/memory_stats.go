package io

import (
	stderrors "errors"
	"fmt"
	"math"
	"runtime"

	"count_mean/internal/logging"
)

// Memory threshold constants.
const (
	memoryWarningThreshold  = 0.9  // 90% threshold for warning
	memoryCriticalThreshold = 0.95 // 95% threshold for critical
	heapEfficiencyThreshold = 0.6  // 60% heap efficiency threshold
	pauseNsBufferSize       = 256  // Size of PauseNs ring buffer
	pauseNsIndexMask        = 255  // Mask for circular buffer index (256-1)
)

// Static errors for memory checks.
// 注意：先前的 errMemoryUsageHigh（95-100% 使用率）已移除 — 詳見
// evaluateMemoryThresholds 的 cross-compare review 修法說明。
var errMemoryOverLimit = stderrors.New("記憶體使用超過限制")

// MemoryStats 記憶體統計信息.
type MemoryStats struct {
	Alloc         uint64  `json:"alloc"`           // 當前分配的記憶體
	TotalAlloc    uint64  `json:"total_alloc"`     // 總分配記憶體
	Sys           uint64  `json:"sys"`             // 系統記憶體
	Lookups       uint64  `json:"lookups"`         // 查找次數
	Mallocs       uint64  `json:"mallocs"`         // 分配次數
	Frees         uint64  `json:"frees"`           // 釋放次數
	HeapAlloc     uint64  `json:"heap_alloc"`      // 堆分配
	HeapSys       uint64  `json:"heap_sys"`        // 堆系統記憶體
	HeapIdle      uint64  `json:"heap_idle"`       // 堆空閒記憶體
	HeapInuse     uint64  `json:"heap_inuse"`      // 堆使用記憶體
	HeapReleased  uint64  `json:"heap_released"`   // 堆釋放記憶體
	HeapObjects   uint64  `json:"heap_objects"`    // 堆對象數
	StackInuse    uint64  `json:"stack_inuse"`     // 棧使用記憶體
	StackSys      uint64  `json:"stack_sys"`       // 棧系統記憶體
	MSpanInuse    uint64  `json:"mspan_inuse"`     // MSpan使用記憶體
	MSpanSys      uint64  `json:"mspan_sys"`       // MSpan系統記憶體
	MCacheInuse   uint64  `json:"mcache_inuse"`    // MCache使用記憶體
	MCacheSys     uint64  `json:"mcache_sys"`      // MCache系統記憶體
	BuckHashSys   uint64  `json:"buckhash_sys"`    // BuckHash系統記憶體
	GCSys         uint64  `json:"gc_sys"`          // GC系統記憶體
	OtherSys      uint64  `json:"other_sys"`       // 其他系統記憶體
	NextGC        uint64  `json:"next_gc"`         // 下次GC閾值
	LastGC        uint64  `json:"last_gc"`         // 上次GC時間
	PauseTotalNs  uint64  `json:"pause_total"`     // GC暫停總時間
	PauseNs       uint64  `json:"pause_ns"`        // 最近GC暫停時間
	NumGC         uint32  `json:"num_gc"`          // GC次數
	NumForcedGC   uint32  `json:"num_forced_gc"`   // 強制GC次數
	GCCPUFraction float64 `json:"gc_cpu_fraction"` // GC CPU比例
	UsageRatio    float64 `json:"usage_ratio"`     // 使用率
	IsOverLimit   bool    `json:"is_over_limit"`   // 是否超過限制
}

// getDetailedMemoryStats 獲取詳細記憶體統計信息.
func (h *LargeFileHandler) getDetailedMemoryStats() *MemoryStats {
	var memStats runtime.MemStats

	runtime.ReadMemStats(&memStats)

	stats := &MemoryStats{
		Alloc:         memStats.Alloc,
		TotalAlloc:    memStats.TotalAlloc,
		Sys:           memStats.Sys,
		Lookups:       memStats.Lookups,
		Mallocs:       memStats.Mallocs,
		Frees:         memStats.Frees,
		HeapAlloc:     memStats.HeapAlloc,
		HeapSys:       memStats.HeapSys,
		HeapIdle:      memStats.HeapIdle,
		HeapInuse:     memStats.HeapInuse,
		HeapReleased:  memStats.HeapReleased,
		HeapObjects:   memStats.HeapObjects,
		StackInuse:    memStats.StackInuse,
		StackSys:      memStats.StackSys,
		MSpanInuse:    memStats.MSpanInuse,
		MSpanSys:      memStats.MSpanSys,
		MCacheInuse:   memStats.MCacheInuse,
		MCacheSys:     memStats.MCacheSys,
		BuckHashSys:   memStats.BuckHashSys,
		GCSys:         memStats.GCSys,
		OtherSys:      memStats.OtherSys,
		NextGC:        memStats.NextGC,
		LastGC:        memStats.LastGC,
		PauseTotalNs:  memStats.PauseTotalNs,
		NumGC:         memStats.NumGC,
		NumForcedGC:   memStats.NumForcedGC,
		GCCPUFraction: memStats.GCCPUFraction,
		UsageRatio:    float64(memStats.Alloc) / float64(h.memoryLimit),
		IsOverLimit:   safeUint64ToInt64(memStats.Alloc) > h.memoryLimit,
	}

	// 計算最近的GC暫停時間
	if memStats.NumGC > 0 {
		stats.PauseNs = memStats.PauseNs[(memStats.NumGC+pauseNsIndexMask)%pauseNsBufferSize]
	}

	return stats
}

// safeUint64ToInt64 safely converts uint64 to int64, capping at max int64.
func safeUint64ToInt64(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}

	return int64(value)
}

// evaluateMemoryThresholds 把純粹的「依 stats 決定要不要 return error + 記錄分級警告」
// 邏輯抽出，方便用 fake MemoryStats 在 unit test 裡驗證 threshold 決策。
//
// 修法歷史：cross-compare review（Claude vs codex）抓到 95-100% 區間會 return
// errMemoryUsageHigh，但這時 stats.IsOverLimit 仍為 false（< memoryLimit）。
// 上游 executeStreamingLoop 把任何 error 當 fatal abort，於是 user 在記憶體
// 仍低於 limit 時就被 abort — 與舊版 handleMemoryPressure 只 warn 的行為矛盾。
// 修法：只在 stats.IsOverLimit==true 時 return error；95% 改純 log，不中斷。
func evaluateMemoryThresholds(stats *MemoryStats, limit int64, logger *logging.Logger) error {
	if stats.IsOverLimit {
		logger.Warn("記憶體使用超過限制", map[string]interface{}{
			"current_mb":  stats.Alloc / 1024 / 1024,
			"limit_mb":    limit / 1024 / 1024,
			"usage_ratio": stats.UsageRatio,
		})

		return fmt.Errorf(
			"%w: %d MB > %d MB (%.2f%%)",
			errMemoryOverLimit,
			stats.Alloc/1024/1024, limit/1024/1024, stats.UsageRatio*fullProgress,
		)
	}

	if stats.UsageRatio > memoryWarningThreshold {
		logger.Warn("記憶體使用接近限制", map[string]interface{}{
			"current_mb":  stats.Alloc / 1024 / 1024,
			"limit_mb":    limit / 1024 / 1024,
			"usage_ratio": stats.UsageRatio,
		})

		if stats.UsageRatio > memoryCriticalThreshold {
			logger.Info("記憶體使用率超過95%，距離 limit 仍在容忍範圍 — streaming 繼續但建議調整 chunk size 或 memoryLimit")
		}
	}

	if stats.HeapSys > 0 {
		heapEfficiency := float64(stats.HeapInuse) / float64(stats.HeapSys)
		if heapEfficiency < heapEfficiencyThreshold {
			logger.Warn("堆記憶體效率低", map[string]interface{}{
				"heap_efficiency": heapEfficiency,
				"heap_inuse_mb":   stats.HeapInuse / 1024 / 1024,
				"heap_sys_mb":     stats.HeapSys / 1024 / 1024,
				"heap_idle_mb":    stats.HeapIdle / 1024 / 1024,
			})
		}
	}

	return nil
}

// checkMemoryUsage 檢查記憶體使用.
func (h *LargeFileHandler) checkMemoryUsage() error {
	stats := h.getDetailedMemoryStats()

	// 記錄詳細的記憶體信息
	h.logger.Debug("記憶體使用統計", map[string]interface{}{
		"alloc_mb":        stats.Alloc / 1024 / 1024,
		"total_alloc_mb":  stats.TotalAlloc / 1024 / 1024,
		"sys_mb":          stats.Sys / 1024 / 1024,
		"heap_alloc_mb":   stats.HeapAlloc / 1024 / 1024,
		"heap_sys_mb":     stats.HeapSys / 1024 / 1024,
		"heap_idle_mb":    stats.HeapIdle / 1024 / 1024,
		"heap_inuse_mb":   stats.HeapInuse / 1024 / 1024,
		"heap_objects":    stats.HeapObjects,
		"stack_inuse_mb":  stats.StackInuse / 1024 / 1024,
		"usage_ratio":     stats.UsageRatio,
		"num_gc":          stats.NumGC,
		"num_forced_gc":   stats.NumForcedGC,
		"gc_cpu_fraction": stats.GCCPUFraction,
		"next_gc_mb":      stats.NextGC / 1024 / 1024,
		"limit_mb":        h.memoryLimit / 1024 / 1024,
	})

	return evaluateMemoryThresholds(stats, h.memoryLimit, h.logger)
}

// getMemoryUsage 獲取當前記憶體使用.
func (*LargeFileHandler) getMemoryUsage() int64 {
	var memStats runtime.MemStats

	runtime.ReadMemStats(&memStats)

	return safeUint64ToInt64(memStats.Alloc)
}
