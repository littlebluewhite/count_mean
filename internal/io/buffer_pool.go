package io

import (
	"sync"

	"count_mean/internal/models"
)

// Buffer pool capacity constants.
const (
	stringArrayPoolCapacity = 1000 // 字符串陣列池預分配容量
	emgDataPoolCapacity     = 2000 // EMG數據池預分配容量
	float64PoolCapacity     = 100  // float64切片池預分配容量
)

// BufferPool 緩衝區池管理.
type BufferPool struct {
	stringArrayPool *sync.Pool // 字符串陣列池
	emgDataPool     *sync.Pool // EMG數據池
	float64Pool     *sync.Pool // float64 切片池
	mutex           sync.RWMutex
	stats           BufferPoolStats
}

// BufferPoolStats 緩衝區池統計.
type BufferPoolStats struct {
	StringArrayGets int64   `json:"string_array_gets"`
	StringArrayPuts int64   `json:"string_array_puts"`
	EMGDataGets     int64   `json:"emg_data_gets"`
	EMGDataPuts     int64   `json:"emg_data_puts"`
	Float64Gets     int64   `json:"float64_gets"`
	Float64Puts     int64   `json:"float64_puts"`
	ReuseRatio      float64 `json:"reuse_ratio"`
}

// NewBufferPool 創建緩衝區池.
func NewBufferPool() *BufferPool {
	return &BufferPool{
		stringArrayPool: &sync.Pool{
			New: func() interface{} {
				return make([][]string, 0, stringArrayPoolCapacity)
			},
		},
		emgDataPool: &sync.Pool{
			New: func() interface{} {
				return make([]models.EMGData, 0, emgDataPoolCapacity)
			},
		},
		float64Pool: &sync.Pool{
			New: func() interface{} {
				return make([]float64, 0, float64PoolCapacity)
			},
		},
	}
}

// GetStringArray 獲取字符串陣列.
func (bp *BufferPool) GetStringArray() [][]string {
	bp.mutex.Lock()
	bp.stats.StringArrayGets++
	bp.mutex.Unlock()

	poolVal := bp.stringArrayPool.Get()

	arr, ok := poolVal.([][]string)
	if !ok {
		// Fallback: create new slice if type assertion fails
		return make([][]string, 0, stringArrayPoolCapacity)
	}

	return arr[:0] // 重置長度但保留容量
}

// PutStringArray 歸還字符串陣列.
func (bp *BufferPool) PutStringArray(arr [][]string) {
	if cap(arr) > 0 {
		bp.mutex.Lock()
		bp.stats.StringArrayPuts++
		bp.mutex.Unlock()

		// 清空引用以避免記憶體洩漏
		for i := range arr {
			arr[i] = nil
		}

		//nolint:staticcheck // SA6002: slice pooling tradeoff - boxing overhead acceptable for memory reuse
		bp.stringArrayPool.Put(arr[:0])
	}
}

// GetEMGDataSlice 獲取EMG數據切片.
func (bp *BufferPool) GetEMGDataSlice() []models.EMGData {
	bp.mutex.Lock()
	bp.stats.EMGDataGets++
	bp.mutex.Unlock()

	poolVal := bp.emgDataPool.Get()

	slice, ok := poolVal.([]models.EMGData)
	if !ok {
		// Fallback: create new slice if type assertion fails
		return make([]models.EMGData, 0, emgDataPoolCapacity)
	}

	return slice[:0] // 重置長度但保留容量
}

// PutEMGDataSlice 歸還EMG數據切片.
func (bp *BufferPool) PutEMGDataSlice(slice []models.EMGData) {
	if cap(slice) > 0 {
		bp.mutex.Lock()
		bp.stats.EMGDataPuts++
		bp.mutex.Unlock()

		// 清空數據以避免記憶體洩漏
		for i := range slice {
			slice[i] = models.EMGData{}
		}

		//nolint:staticcheck // SA6002: slice pooling tradeoff - boxing overhead acceptable for memory reuse
		bp.emgDataPool.Put(slice[:0])
	}
}

// GetFloat64Slice 獲取float64切片.
func (bp *BufferPool) GetFloat64Slice() []float64 {
	bp.mutex.Lock()
	bp.stats.Float64Gets++
	bp.mutex.Unlock()

	poolVal := bp.float64Pool.Get()

	slice, ok := poolVal.([]float64)
	if !ok {
		// Fallback: create new slice if type assertion fails
		return make([]float64, 0, float64PoolCapacity)
	}

	return slice[:0] // 重置長度但保留容量
}

// PutFloat64Slice 歸還float64切片.
func (bp *BufferPool) PutFloat64Slice(slice []float64) {
	if cap(slice) > 0 {
		bp.mutex.Lock()
		bp.stats.Float64Puts++
		bp.mutex.Unlock()

		//nolint:staticcheck // SA6002: slice pooling tradeoff - boxing overhead acceptable for memory reuse
		bp.float64Pool.Put(slice[:0])
	}
}

// GetStats 獲取緩衝區池統計.
func (bp *BufferPool) GetStats() BufferPoolStats {
	bp.mutex.RLock()
	defer bp.mutex.RUnlock()

	stats := bp.stats

	// 計算重用率
	totalGets := stats.StringArrayGets + stats.EMGDataGets + stats.Float64Gets
	totalPuts := stats.StringArrayPuts + stats.EMGDataPuts + stats.Float64Puts

	if totalGets > 0 {
		stats.ReuseRatio = float64(totalPuts) / float64(totalGets)
	}

	return stats
}
