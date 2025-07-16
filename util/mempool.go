package util

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

// MemoryPool 通用記憶體池管理器
type MemoryPool struct {
	float64Pool   *sync.Pool
	intPool       *sync.Pool
	stringPool    *sync.Pool
	byteSlicePool *sync.Pool
	stats         *PoolStats
	mu            sync.RWMutex
}

// PoolStats 記憶體池統計信息
type PoolStats struct {
	Float64Gets     int64
	Float64Puts     int64
	Float64Misses   int64
	IntGets         int64
	IntPuts         int64
	IntMisses       int64
	StringGets      int64
	StringPuts      int64
	StringMisses    int64
	ByteSliceGets   int64
	ByteSlicePuts   int64
	ByteSliceMisses int64
	TotalAllocated  int64
	TotalReused     int64
}

// GlobalMemoryPool 全局記憶體池實例
var GlobalMemoryPool = NewMemoryPool()

// NewMemoryPool 創建新的記憶體池
func NewMemoryPool() *MemoryPool {
	return &MemoryPool{
		float64Pool: &sync.Pool{
			New: func() interface{} {
				return make([]float64, 0, 1024) // 預設容量1024
			},
		},
		intPool: &sync.Pool{
			New: func() interface{} {
				return make([]int, 0, 1024)
			},
		},
		stringPool: &sync.Pool{
			New: func() interface{} {
				return make([]string, 0, 256)
			},
		},
		byteSlicePool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 4096) // 4KB初始容量
			},
		},
		stats: &PoolStats{},
	}
}

// GetFloat64Slice 從池中獲取float64切片
func (mp *MemoryPool) GetFloat64Slice(minCap int) []float64 {
	mp.mu.Lock()
	mp.stats.Float64Gets++
	mp.mu.Unlock()

	slice := mp.float64Pool.Get().([]float64)

	// 如果容量不足，重新分配
	if cap(slice) < minCap {
		mp.mu.Lock()
		mp.stats.Float64Misses++
		mp.stats.TotalAllocated += int64(minCap * 8) // 8 bytes per float64
		mp.mu.Unlock()

		return make([]float64, 0, minCap)
	}

	mp.mu.Lock()
	mp.stats.TotalReused += int64(cap(slice) * 8)
	mp.mu.Unlock()

	return slice[:0] // 重置長度但保留容量
}

// PutFloat64Slice 將float64切片放回池中
func (mp *MemoryPool) PutFloat64Slice(slice []float64) {
	if slice == nil || cap(slice) == 0 {
		return
	}

	mp.mu.Lock()
	mp.stats.Float64Puts++
	mp.mu.Unlock()

	// 清空切片但保留容量
	slice = slice[:0]
	mp.float64Pool.Put(slice)
}

// GetIntSlice 從池中獲取int切片
func (mp *MemoryPool) GetIntSlice(minCap int) []int {
	mp.mu.Lock()
	mp.stats.IntGets++
	mp.mu.Unlock()

	slice := mp.intPool.Get().([]int)

	if cap(slice) < minCap {
		mp.mu.Lock()
		mp.stats.IntMisses++
		mp.stats.TotalAllocated += int64(minCap * 8) // 8 bytes per int64
		mp.mu.Unlock()

		return make([]int, 0, minCap)
	}

	mp.mu.Lock()
	mp.stats.TotalReused += int64(cap(slice) * 8)
	mp.mu.Unlock()

	return slice[:0]
}

// PutIntSlice 將int切片放回池中
func (mp *MemoryPool) PutIntSlice(slice []int) {
	if slice == nil || cap(slice) == 0 {
		return
	}

	mp.mu.Lock()
	mp.stats.IntPuts++
	mp.mu.Unlock()

	slice = slice[:0]
	mp.intPool.Put(slice)
}

// GetStringSlice 從池中獲取string切片
func (mp *MemoryPool) GetStringSlice(minCap int) []string {
	mp.mu.Lock()
	mp.stats.StringGets++
	mp.mu.Unlock()

	slice := mp.stringPool.Get().([]string)

	if cap(slice) < minCap {
		mp.mu.Lock()
		mp.stats.StringMisses++
		mp.stats.TotalAllocated += int64(minCap * 16) // 16 bytes per string header
		mp.mu.Unlock()

		return make([]string, 0, minCap)
	}

	mp.mu.Lock()
	mp.stats.TotalReused += int64(cap(slice) * 16)
	mp.mu.Unlock()

	return slice[:0]
}

// PutStringSlice 將string切片放回池中
func (mp *MemoryPool) PutStringSlice(slice []string) {
	if slice == nil || cap(slice) == 0 {
		return
	}

	mp.mu.Lock()
	mp.stats.StringPuts++
	mp.mu.Unlock()

	// 清空字符串引用以避免記憶體洩漏
	for i := range slice {
		slice[i] = ""
	}
	slice = slice[:0]
	mp.stringPool.Put(slice)
}

// GetByteSlice 從池中獲取byte切片
func (mp *MemoryPool) GetByteSlice(minCap int) []byte {
	mp.mu.Lock()
	mp.stats.ByteSliceGets++
	mp.mu.Unlock()

	slice := mp.byteSlicePool.Get().([]byte)

	if cap(slice) < minCap {
		mp.mu.Lock()
		mp.stats.ByteSliceMisses++
		mp.stats.TotalAllocated += int64(minCap)
		mp.mu.Unlock()

		return make([]byte, 0, minCap)
	}

	mp.mu.Lock()
	mp.stats.TotalReused += int64(cap(slice))
	mp.mu.Unlock()

	return slice[:0]
}

// PutByteSlice 將byte切片放回池中
func (mp *MemoryPool) PutByteSlice(slice []byte) {
	if slice == nil || cap(slice) == 0 {
		return
	}

	mp.mu.Lock()
	mp.stats.ByteSlicePuts++
	mp.mu.Unlock()

	slice = slice[:0]
	mp.byteSlicePool.Put(slice)
}

// GetStats 獲取記憶體池統計信息
func (mp *MemoryPool) GetStats() PoolStats {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return *mp.stats
}

// ResetStats 重置統計信息
func (mp *MemoryPool) ResetStats() {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.stats = &PoolStats{}
}

// GetHitRate 獲取命中率
func (mp *MemoryPool) GetHitRate() float64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	totalGets := mp.stats.Float64Gets + mp.stats.IntGets + mp.stats.StringGets + mp.stats.ByteSliceGets
	totalMisses := mp.stats.Float64Misses + mp.stats.IntMisses + mp.stats.StringMisses + mp.stats.ByteSliceMisses

	if totalGets == 0 {
		return 0
	}

	return float64(totalGets-totalMisses) / float64(totalGets)
}

// GetMemoryReduction 獲取記憶體減少量
func (mp *MemoryPool) GetMemoryReduction() float64 {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	totalMemory := mp.stats.TotalAllocated + mp.stats.TotalReused
	if totalMemory == 0 {
		return 0
	}

	return float64(mp.stats.TotalReused) / float64(totalMemory)
}

// PooledArray 使用記憶體池的陣列包裝器
type PooledArray struct {
	float64Data []float64
	intData     []int
	stringData  []string
	byteData    []byte
	pool        *MemoryPool
	dataType    string
}

// NewPooledFloat64Array 創建新的pooled float64陣列
func NewPooledFloat64Array(capacity int) *PooledArray {
	return &PooledArray{
		float64Data: GlobalMemoryPool.GetFloat64Slice(capacity),
		pool:        GlobalMemoryPool,
		dataType:    "float64",
	}
}

// NewPooledIntArray 創建新的pooled int陣列
func NewPooledIntArray(capacity int) *PooledArray {
	return &PooledArray{
		intData:  GlobalMemoryPool.GetIntSlice(capacity),
		pool:     GlobalMemoryPool,
		dataType: "int",
	}
}

// NewPooledStringArray 創建新的pooled string陣列
func NewPooledStringArray(capacity int) *PooledArray {
	return &PooledArray{
		stringData: GlobalMemoryPool.GetStringSlice(capacity),
		pool:       GlobalMemoryPool,
		dataType:   "string",
	}
}

// NewPooledByteArray 創建新的pooled byte陣列
func NewPooledByteArray(capacity int) *PooledArray {
	return &PooledArray{
		byteData: GlobalMemoryPool.GetByteSlice(capacity),
		pool:     GlobalMemoryPool,
		dataType: "byte",
	}
}

// Float64Data 獲取float64數據
func (pa *PooledArray) Float64Data() []float64 {
	return pa.float64Data
}

// IntData 獲取int數據
func (pa *PooledArray) IntData() []int {
	return pa.intData
}

// StringData 獲取string數據
func (pa *PooledArray) StringData() []string {
	return pa.stringData
}

// ByteData 獲取byte數據
func (pa *PooledArray) ByteData() []byte {
	return pa.byteData
}

// Append 向陣列添加元素
func (pa *PooledArray) AppendFloat64(values ...float64) {
	if pa.dataType == "float64" {
		pa.float64Data = append(pa.float64Data, values...)
	}
}

func (pa *PooledArray) AppendInt(values ...int) {
	if pa.dataType == "int" {
		pa.intData = append(pa.intData, values...)
	}
}

func (pa *PooledArray) AppendString(values ...string) {
	if pa.dataType == "string" {
		pa.stringData = append(pa.stringData, values...)
	}
}

func (pa *PooledArray) AppendByte(values ...byte) {
	if pa.dataType == "byte" {
		pa.byteData = append(pa.byteData, values...)
	}
}

// Len 獲取陣列長度
func (pa *PooledArray) Len() int {
	switch pa.dataType {
	case "float64":
		return len(pa.float64Data)
	case "int":
		return len(pa.intData)
	case "string":
		return len(pa.stringData)
	case "byte":
		return len(pa.byteData)
	default:
		return 0
	}
}

// Cap 獲取陣列容量
func (pa *PooledArray) Cap() int {
	switch pa.dataType {
	case "float64":
		return cap(pa.float64Data)
	case "int":
		return cap(pa.intData)
	case "string":
		return cap(pa.stringData)
	case "byte":
		return cap(pa.byteData)
	default:
		return 0
	}
}

// Reset 重置陣列長度
func (pa *PooledArray) Reset() {
	switch pa.dataType {
	case "float64":
		pa.float64Data = pa.float64Data[:0]
	case "int":
		pa.intData = pa.intData[:0]
	case "string":
		// 清空字符串引用
		for i := range pa.stringData {
			pa.stringData[i] = ""
		}
		pa.stringData = pa.stringData[:0]
	case "byte":
		pa.byteData = pa.byteData[:0]
	}
}

// Close 釋放陣列資源回池中
func (pa *PooledArray) Close() {
	if pa.pool == nil {
		return
	}

	switch pa.dataType {
	case "float64":
		pa.pool.PutFloat64Slice(pa.float64Data)
	case "int":
		pa.pool.PutIntSlice(pa.intData)
	case "string":
		pa.pool.PutStringSlice(pa.stringData)
	case "byte":
		pa.pool.PutByteSlice(pa.byteData)
	}

	pa.pool = nil
}

// PooledCalculator 使用記憶體池的計算器
type PooledCalculator struct {
	pool *MemoryPool
}

// NewPooledCalculator 創建新的池化計算器
func NewPooledCalculator() *PooledCalculator {
	return &PooledCalculator{
		pool: GlobalMemoryPool,
	}
}

// CalculateArrayMean 使用記憶體池的陣列平均值計算
func (pc *PooledCalculator) CalculateArrayMean(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}

	// 使用記憶體池獲取臨時切片
	tempSlice := pc.pool.GetFloat64Slice(len(data))
	defer pc.pool.PutFloat64Slice(tempSlice)

	// 複製數據到臨時切片進行處理
	tempSlice = append(tempSlice, data...)

	// 使用SIMD優化的計算
	return ArrayMeanSIMD(tempSlice)
}

// CalculateArrayMax 使用記憶體池的陣列最大值計算
func (pc *PooledCalculator) CalculateArrayMax(data []float64) (float64, int) {
	if len(data) == 0 {
		return 0, -1
	}

	// 使用記憶體池獲取臨時切片
	tempSlice := pc.pool.GetFloat64Slice(len(data))
	defer pc.pool.PutFloat64Slice(tempSlice)

	// 複製數據到臨時切片進行處理
	tempSlice = append(tempSlice, data...)

	// 使用SIMD優化的計算
	return ArrayMaxSIMD(tempSlice)
}

// CalculateSlidingWindowMean 使用記憶體池的滑動窗口平均值計算
func (pc *PooledCalculator) CalculateSlidingWindowMean(data []float64, windowSize int) ([]float64, error) {
	if len(data) < windowSize {
		return nil, fmt.Errorf("data length %d is less than window size %d", len(data), windowSize)
	}

	resultCount := len(data) - windowSize + 1

	// 使用記憶體池獲取結果切片
	results := pc.pool.GetFloat64Slice(resultCount)
	defer pc.pool.PutFloat64Slice(results)

	// 計算每個窗口的平均值
	for i := 0; i < resultCount; i++ {
		windowData := data[i : i+windowSize]
		mean := ArrayMeanSIMD(windowData)
		results = append(results, mean)
	}

	// 創建返回結果的副本
	resultCopy := make([]float64, len(results))
	copy(resultCopy, results)

	return resultCopy, nil
}

// BatchCalculate 批量計算多個陣列的統計信息
func (pc *PooledCalculator) BatchCalculate(datasets [][]float64) []struct {
	Mean     float64
	Max      float64
	MaxIndex int
} {
	results := make([]struct {
		Mean     float64
		Max      float64
		MaxIndex int
	}, len(datasets))

	for i, data := range datasets {
		mean := pc.CalculateArrayMean(data)
		max, maxIndex := pc.CalculateArrayMax(data)

		results[i] = struct {
			Mean     float64
			Max      float64
			MaxIndex int
		}{
			Mean:     mean,
			Max:      max,
			MaxIndex: maxIndex,
		}
	}

	return results
}

// GetMemoryUsage 獲取記憶體使用情況
func (pc *PooledCalculator) GetMemoryUsage() (allocated, reused int64, hitRate float64) {
	stats := pc.pool.GetStats()
	return stats.TotalAllocated, stats.TotalReused, pc.pool.GetHitRate()
}

// GCOptimizer GC優化器
type GCOptimizer struct {
	pool           *MemoryPool
	gcThreshold    int64 // 觸發GC的記憶體閾值
	lastGCTime     int64
	gcInterval     int64 // GC間隔時間（毫秒）
	forceGCEnabled bool
}

// NewGCOptimizer 創建GC優化器
func NewGCOptimizer(gcThresholdMB int64) *GCOptimizer {
	return &GCOptimizer{
		pool:           GlobalMemoryPool,
		gcThreshold:    gcThresholdMB * 1024 * 1024, // 轉換為bytes
		gcInterval:     5000,                        // 5秒
		forceGCEnabled: true,
	}
}

// CheckAndTriggerGC 檢查並觸發GC
func (gco *GCOptimizer) CheckAndTriggerGC() bool {
	if !gco.forceGCEnabled {
		return false
	}

	stats := gco.pool.GetStats()
	currentMemory := stats.TotalAllocated

	// 檢查記憶體閾值
	if currentMemory > gco.gcThreshold {
		gco.triggerGC()
		return true
	}

	// 檢查時間間隔
	currentTime := time.Now().UnixMilli()
	if currentTime-gco.lastGCTime > gco.gcInterval {
		gco.triggerGC()
		return true
	}

	return false
}

// triggerGC 觸發垃圾回收
func (gco *GCOptimizer) triggerGC() {
	runtime.GC()
	gco.lastGCTime = time.Now().UnixMilli()
}

// SetGCThreshold 設置GC閾值
func (gco *GCOptimizer) SetGCThreshold(thresholdMB int64) {
	gco.gcThreshold = thresholdMB * 1024 * 1024
}

// SetGCInterval 設置GC間隔
func (gco *GCOptimizer) SetGCInterval(intervalMs int64) {
	gco.gcInterval = intervalMs
}

// EnableAutoGC 啟用自動GC
func (gco *GCOptimizer) EnableAutoGC() {
	gco.forceGCEnabled = true
}

// DisableAutoGC 禁用自動GC
func (gco *GCOptimizer) DisableAutoGC() {
	gco.forceGCEnabled = false
}
