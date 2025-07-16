package util

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// 生成測試數據
func generateTestData(size int) []float64 {
	data := make([]float64, size)
	for i := range data {
		data[i] = rand.Float64() * 1000 // 0-1000之間的隨機數
	}
	return data
}

// 測試SIMD平均值計算的正確性
func TestArrayMeanSIMD(t *testing.T) {
	testCases := []struct {
		name string
		data []float64
		want float64
	}{
		{
			name: "empty array",
			data: []float64{},
			want: 0,
		},
		{
			name: "single element",
			data: []float64{5.0},
			want: 5.0,
		},
		{
			name: "small array",
			data: []float64{1, 2, 3, 4, 5},
			want: 3.0,
		},
		{
			name: "large array",
			data: []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
			want: 55.0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ArrayMeanSIMD(tc.data)
			if got != tc.want {
				t.Errorf("ArrayMeanSIMD(%v) = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

// 測試SIMD最大值計算的正確性
func TestArrayMaxSIMD(t *testing.T) {
	testCases := []struct {
		name      string
		data      []float64
		wantMax   float64
		wantIndex int
	}{
		{
			name:      "empty array",
			data:      []float64{},
			wantMax:   0,
			wantIndex: -1,
		},
		{
			name:      "single element",
			data:      []float64{5.0},
			wantMax:   5.0,
			wantIndex: 0,
		},
		{
			name:      "small array",
			data:      []float64{1, 5, 3, 2, 4},
			wantMax:   5.0,
			wantIndex: 1,
		},
		{
			name:      "large array",
			data:      []float64{10, 20, 100, 40, 50, 60, 70, 80, 90, 30},
			wantMax:   100.0,
			wantIndex: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotMax, gotIndex := ArrayMaxSIMD(tc.data)
			if gotMax != tc.wantMax || gotIndex != tc.wantIndex {
				t.Errorf("ArrayMaxSIMD(%v) = (%v, %v), want (%v, %v)",
					tc.data, gotMax, gotIndex, tc.wantMax, tc.wantIndex)
			}
		})
	}
}

// 測試SIMD統計計算器
func TestSIMDStatistics_CalculateChannelStatistics(t *testing.T) {
	stats := NewSIMDStatistics()

	data := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	expectedMean := 5.5
	expectedMax := 10.0
	expectedMaxIndex := 9

	mean, max, maxIndex := stats.CalculateChannelStatistics(data)

	if mean != expectedMean {
		t.Errorf("Expected mean %v, got %v", expectedMean, mean)
	}
	if max != expectedMax {
		t.Errorf("Expected max %v, got %v", expectedMax, max)
	}
	if maxIndex != expectedMaxIndex {
		t.Errorf("Expected maxIndex %v, got %v", expectedMaxIndex, maxIndex)
	}
}

// 測試SIMD滑動窗口計算
func TestSIMDSlidingWindow_CalculateMaxMean(t *testing.T) {
	sw := NewSIMDSlidingWindow(3)

	// 測試數據：窗口大小為3，最大平均值應該是(8+9+10)/3 = 9
	data := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	maxMean, bestIndex := sw.CalculateMaxMean(data)

	expectedMaxMean := 9.0
	expectedBestIndex := 7 // 窗口起始位置

	if maxMean != expectedMaxMean {
		t.Errorf("Expected maxMean %v, got %v", expectedMaxMean, maxMean)
	}
	if bestIndex != expectedBestIndex {
		t.Errorf("Expected bestIndex %v, got %v", expectedBestIndex, bestIndex)
	}
}

// 基準測試：比較SIMD和通用實現的性能

func BenchmarkArrayMeanSIMD_Small(b *testing.B) {
	data := generateTestData(100)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ArrayMeanSIMD(data)
	}
}

func BenchmarkArrayMeanGeneric_Small(b *testing.B) {
	data := generateTestData(100)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		arrayMeanGeneric(data)
	}
}

func BenchmarkArrayMeanSIMD_Large(b *testing.B) {
	data := generateTestData(10000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ArrayMeanSIMD(data)
	}
}

func BenchmarkArrayMeanGeneric_Large(b *testing.B) {
	data := generateTestData(10000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		arrayMeanGeneric(data)
	}
}

func BenchmarkArrayMaxSIMD_Small(b *testing.B) {
	data := generateTestData(100)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ArrayMaxSIMD(data)
	}
}

func BenchmarkArrayMaxGeneric_Small(b *testing.B) {
	data := generateTestData(100)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		arrayMaxGeneric(data)
	}
}

func BenchmarkArrayMaxSIMD_Large(b *testing.B) {
	data := generateTestData(10000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ArrayMaxSIMD(data)
	}
}

func BenchmarkArrayMaxGeneric_Large(b *testing.B) {
	data := generateTestData(10000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		arrayMaxGeneric(data)
	}
}

// 滑動窗口基準測試
func BenchmarkSIMDSlidingWindow_Small(b *testing.B) {
	sw := NewSIMDSlidingWindow(10)
	data := generateTestData(1000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sw.CalculateMaxMean(data)
	}
}

func BenchmarkSIMDSlidingWindow_Large(b *testing.B) {
	sw := NewSIMDSlidingWindow(100)
	data := generateTestData(100000)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sw.CalculateMaxMean(data)
	}
}

// 多通道統計計算基準測試
func BenchmarkBatchCalculateStatistics_SIMD(b *testing.B) {
	stats := NewSIMDStatistics()

	channels := make(map[string][]float64)
	for i := 0; i < 10; i++ {
		channels[fmt.Sprintf("channel_%d", i)] = generateTestData(1000)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		stats.BatchCalculateStatistics(channels)
	}
}

// 記憶體對齊測試
func TestMemoryAlignment(t *testing.T) {
	data := generateTestData(1000)
	aligned := ensureAlignment(data)

	// 測試對齊後的數據是否正確
	for i, v := range data {
		if aligned[i] != v {
			t.Errorf("Alignment changed data: original[%d]=%v, aligned[%d]=%v", i, v, i, aligned[i])
		}
	}
}

// 測試SIMD能力檢測
func TestDetectSIMDCapabilities(t *testing.T) {
	caps := DetectSIMDCapabilities()

	// 基本檢查
	if caps.VectorSize <= 0 {
		t.Error("VectorSize should be positive")
	}

	// 在x86-64架構上，應該至少支持SSE2
	if caps.HasSSE2 == false {
		t.Log("Warning: SSE2 support not detected")
	}

	t.Logf("SIMD Capabilities: AVX2=%v, AVX=%v, SSE4=%v, SSE2=%v, VectorSize=%d",
		caps.HasAVX2, caps.HasAVX, caps.HasSSE4, caps.HasSSE2, caps.VectorSize)
}

// 壓力測試：確保SIMD實現在大數據集上的穩定性
func TestSIMDStressTest(t *testing.T) {
	// 測試各種大小的數據集
	sizes := []int{1, 7, 8, 9, 15, 16, 17, 31, 32, 33, 100, 1000, 10000}

	for _, size := range sizes {
		data := generateTestData(size)

		// 比較SIMD和通用實現的結果
		simdMean := ArrayMeanSIMD(data)
		genericMean := arrayMeanGeneric(data)

		// 允許小的浮點誤差
		if abs(simdMean-genericMean) > 1e-10 {
			t.Errorf("Mean mismatch for size %d: SIMD=%v, Generic=%v", size, simdMean, genericMean)
		}

		simdMax, simdIndex := ArrayMaxSIMD(data)
		genericMax, genericIndex := arrayMaxGeneric(data)

		if simdMax != genericMax || simdIndex != genericIndex {
			t.Errorf("Max mismatch for size %d: SIMD=(%v,%v), Generic=(%v,%v)",
				size, simdMax, simdIndex, genericMax, genericIndex)
		}
	}
}

// 輔助函數：計算絕對值
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// 性能比較測試
func TestPerformanceComparison(t *testing.T) {
	sizes := []int{100, 1000, 10000, 100000}

	for _, size := range sizes {
		data := generateTestData(size)

		t.Run(fmt.Sprintf("size_%d", size), func(t *testing.T) {
			// 測試SIMD性能
			start := time.Now()
			for i := 0; i < 1000; i++ {
				ArrayMeanSIMD(data)
			}
			simdTime := time.Since(start)

			// 測試通用實現性能
			start = time.Now()
			for i := 0; i < 1000; i++ {
				arrayMeanGeneric(data)
			}
			genericTime := time.Since(start)

			speedup := float64(genericTime) / float64(simdTime)
			t.Logf("Size %d: SIMD=%v, Generic=%v, Speedup=%.2fx",
				size, simdTime, genericTime, speedup)
		})
	}
}
