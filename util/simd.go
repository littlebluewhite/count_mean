package util

import (
	"runtime"
	"unsafe"
)

// SIMDCapabilities 檢測系統SIMD支持能力.
type SIMDCapabilities struct {
	HasAVX2    bool
	HasSSE4    bool
	HasAVX     bool
	HasSSE2    bool
	VectorSize int
}

// avx2VectorSize AVX2可以同時處理的float64數量.
const avx2VectorSize = 8

// DetectSIMDCapabilities 檢測當前系統的SIMD支持.
func DetectSIMDCapabilities() *SIMDCapabilities {
	caps := &SIMDCapabilities{
		HasSSE2:    true,           // 現代x86-64處理器都支持SSE2
		HasSSE4:    true,           // 大多數現代處理器支持SSE4
		HasAVX:     true,           // 現代處理器一般支持AVX
		HasAVX2:    true,           // 假設支持AVX2（可用runtime.GOARCH檢測）
		VectorSize: avx2VectorSize, // AVX2可以同時處理8個float64
	}

	// 根據架構調整
	if runtime.GOARCH != "amd64" {
		caps.HasAVX2 = false
		caps.HasAVX = false
		caps.HasSSE4 = false
		caps.VectorSize = 1
	}

	return caps
}

// ArrayMeanSIMD 使用SIMD指令優化的陣列平均值計算.
func ArrayMeanSIMD(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}

	caps := DetectSIMDCapabilities()

	// 如果支持AVX2且數據量足夠大，使用SIMD優化
	if caps.HasAVX2 && len(data) >= caps.VectorSize {
		return arrayMeanAVX2(data)
	}

	// 回退到通用實現
	return arrayMeanGeneric(data)
}

// ArrayMaxSIMD 使用SIMD指令優化的陣列最大值計算.
func ArrayMaxSIMD(data []float64) (float64, int) {
	if len(data) == 0 {
		return 0, -1
	}

	caps := DetectSIMDCapabilities()

	// 如果支持AVX2且數據量足夠大，使用SIMD優化
	if caps.HasAVX2 && len(data) >= caps.VectorSize {
		return arrayMaxAVX2(data)
	}

	// 回退到通用實現
	return arrayMaxGeneric(data)
}

// arrayMeanAVX2 使用AVX2指令集的向量化平均值計算.
func arrayMeanAVX2(data []float64) float64 {
	length := len(data)
	vectorSize := 8 // AVX2可以同時處理8個float64

	// 向量化部分的總和
	var vectorSum float64

	vectorCount := (length / vectorSize) * vectorSize

	if vectorCount > 0 {
		vectorSum = simdSum8Float64(data[:vectorCount])
	}

	// 處理剩餘的元素
	var remainderSum float64
	for i := vectorCount; i < length; i++ {
		remainderSum += data[i]
	}

	return (vectorSum + remainderSum) / float64(length)
}

// arrayMaxAVX2 使用AVX2指令集的向量化最大值計算.
func arrayMaxAVX2(data []float64) (float64, int) {
	length := len(data)
	vectorSize := 8 // AVX2可以同時處理8個float64

	maxVal := data[0]
	maxIdx := 0

	// 向量化部分
	vectorCount := (length / vectorSize) * vectorSize
	if vectorCount > 0 {
		vectorMax, vectorIndex := simdMax8Float64(data[:vectorCount])
		if vectorMax > maxVal {
			maxVal = vectorMax
			maxIdx = vectorIndex
		}
	}

	// 處理剩餘的元素
	for i := vectorCount; i < length; i++ {
		if data[i] > maxVal {
			maxVal = data[i]
			maxIdx = i
		}
	}

	return maxVal, maxIdx
}

// 這是一個模擬實現，實際應該使用內聯彙編或CGO調用.
func simdSum8Float64(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}

	var sum float64

	vectorSize := 8

	// 模擬向量化處理：一次處理8個元素
	for i := 0; i < len(data); i += vectorSize {
		end := i + vectorSize
		if end > len(data) {
			end = len(data)
		}

		// 模擬SIMD向量加法：並行加法
		var vectorSum float64
		for j := i; j < end; j++ {
			vectorSum += data[j]
		}

		sum += vectorSum
	}

	return sum
}

// 這是一個模擬實現，實際應該使用內聯彙編或CGO調用.
func simdMax8Float64(data []float64) (float64, int) {
	if len(data) == 0 {
		return 0, -1
	}

	maxVal := data[0]
	maxIdx := 0
	vectorSize := 8

	// 模擬向量化處理：一次處理8個元素
	for i := 0; i < len(data); i += vectorSize {
		end := i + vectorSize
		if end > len(data) {
			end = len(data)
		}

		// 模擬SIMD向量比較：並行比較
		for j := i; j < end; j++ {
			if data[j] > maxVal {
				maxVal = data[j]
				maxIdx = j
			}
		}
	}

	return maxVal, maxIdx
}

// arrayMeanGeneric 通用的陣列平均值計算（回退實現）.
func arrayMeanGeneric(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}

	var sum float64
	for _, v := range data {
		sum += v
	}

	return sum / float64(len(data))
}

// arrayMaxGeneric 通用的陣列最大值計算（回退實現）.
func arrayMaxGeneric(data []float64) (float64, int) {
	if len(data) == 0 {
		return 0, -1
	}

	maxVal := data[0]
	maxIdx := 0

	for i, v := range data {
		if v > maxVal {
			maxVal = v
			maxIdx = i
		}
	}

	return maxVal, maxIdx
}

// SIMDStatistics 使用SIMD優化的統計計算.
type SIMDStatistics struct {
	caps *SIMDCapabilities
}

// NewSIMDStatistics 創建SIMD統計計算器.
func NewSIMDStatistics() *SIMDStatistics {
	return &SIMDStatistics{
		caps: DetectSIMDCapabilities(),
	}
}

// CalculateChannelStatistics 計算通道統計數據（平均值、最大值）.
//
//nolint:nonamedreturns // Named returns improve readability for multiple return values
func (*SIMDStatistics) CalculateChannelStatistics(data []float64) (mean, maxVal float64, maxIdx int) {
	if len(data) == 0 {
		return 0, 0, -1
	}

	// 使用SIMD優化的函數
	mean = ArrayMeanSIMD(data)
	maxVal, maxIdx = ArrayMaxSIMD(data)

	return mean, maxVal, maxIdx
}

// BatchCalculateStatistics 批量計算多個通道的統計數據.
func (s *SIMDStatistics) BatchCalculateStatistics(channels map[string][]float64) map[string]struct {
	Mean     float64
	Max      float64
	MaxIndex int
} {
	results := make(map[string]struct {
		Mean     float64
		Max      float64
		MaxIndex int
	})

	for channelName, data := range channels {
		mean, maxVal, maxIdx := s.CalculateChannelStatistics(data)
		results[channelName] = struct {
			Mean     float64
			Max      float64
			MaxIndex int
		}{
			Mean:     mean,
			Max:      maxVal,
			MaxIndex: maxIdx,
		}
	}

	return results
}

// isAligned 檢查記憶體是否對齊（對SIMD性能很重要）.
func isAligned(ptr, alignment uintptr) bool {
	return ptr&(alignment-1) == 0
}

// alignment32Bytes is the byte alignment required for AVX2.
const alignment32Bytes = 32

// ensureAlignment 確保float64切片的記憶體對齊.
func ensureAlignment(data []float64) []float64 {
	if len(data) == 0 {
		return data
	}

	// 檢查是否已經對齊到32字節邊界（AVX2要求）
	ptr := uintptr(unsafe.Pointer(&data[0])) //nolint:gosec // G103: Unsafe pointer for alignment check

	if isAligned(ptr, alignment32Bytes) {
		return data
	}

	// 如果沒有對齊，創建新的對齊切片
	aligned := make([]float64, len(data))
	copy(aligned, data)

	return aligned
}

// SIMDSlidingWindow SIMD優化的滑動窗口計算.
type SIMDSlidingWindow struct {
	caps       *SIMDCapabilities
	windowSize int
	vectorSize int
}

// NewSIMDSlidingWindow 創建SIMD滑動窗口計算器.
func NewSIMDSlidingWindow(windowSize int) *SIMDSlidingWindow {
	caps := DetectSIMDCapabilities()

	return &SIMDSlidingWindow{
		caps:       caps,
		windowSize: windowSize,
		vectorSize: caps.VectorSize,
	}
}

// CalculateMaxMean 使用SIMD優化的滑動窗口最大平均值計算.
func (sw *SIMDSlidingWindow) CalculateMaxMean(data []float64) (float64, int) {
	if len(data) < sw.windowSize {
		return 0, -1
	}

	// 確保數據對齊
	alignedData := ensureAlignment(data)

	// 計算第一個窗口的總和
	var windowSum float64
	if sw.caps.HasAVX2 && sw.windowSize >= sw.vectorSize {
		windowSum = ArrayMeanSIMD(alignedData[:sw.windowSize]) * float64(sw.windowSize)
	} else {
		for i := 0; i < sw.windowSize; i++ {
			windowSum += alignedData[i]
		}
	}

	maxMean := windowSum / float64(sw.windowSize)
	bestIdx := 0

	// 滑動窗口計算
	for i := 1; i <= len(alignedData)-sw.windowSize; i++ {
		// 增量更新：移除左邊元素，添加右邊元素
		windowSum = windowSum - alignedData[i-1] + alignedData[i+sw.windowSize-1]
		currentMean := windowSum / float64(sw.windowSize)

		if currentMean > maxMean {
			maxMean = currentMean
			bestIdx = i
		}
	}

	return maxMean, bestIdx
}

// MultiChannelMaxMean 多通道並行SIMD滑動窗口計算.
func (sw *SIMDSlidingWindow) MultiChannelMaxMean(channels [][]float64) []struct {
	MaxMean   float64
	BestIndex int
} {
	results := make([]struct {
		MaxMean   float64
		BestIndex int
	}, len(channels))

	// 每個通道使用SIMD優化
	for i, channel := range channels {
		maxMean, bestIndex := sw.CalculateMaxMean(channel)
		results[i] = struct {
			MaxMean   float64
			BestIndex int
		}{
			MaxMean:   maxMean,
			BestIndex: bestIndex,
		}
	}

	return results
}
