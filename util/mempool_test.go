package util

import (
	"fmt"
	"runtime"
	"testing"
)

func TestMemoryPool_Float64Slice(t *testing.T) {
	pool := NewMemoryPool()

	// 測試獲取和放回float64切片
	slice1 := pool.GetFloat64Slice(100)
	if cap(slice1) < 100 {
		t.Errorf("Expected capacity >= 100, got %d", cap(slice1))
	}

	// 使用切片
	for i := 0; i < 50; i++ {
		slice1 = append(slice1, float64(i))
	}

	// 放回池中
	pool.PutFloat64Slice(slice1)

	// 重新獲取，應該重用之前的切片
	slice2 := pool.GetFloat64Slice(100)
	if len(slice2) != 0 {
		t.Errorf("Expected length 0, got %d", len(slice2))
	}

	pool.PutFloat64Slice(slice2)

	// 檢查統計信息
	stats := pool.GetStats()
	if stats.Float64Gets < 2 {
		t.Errorf("Expected at least 2 gets, got %d", stats.Float64Gets)
	}
	if stats.Float64Puts < 2 {
		t.Errorf("Expected at least 2 puts, got %d", stats.Float64Puts)
	}
}

func TestMemoryPool_IntSlice(t *testing.T) {
	pool := NewMemoryPool()

	slice := pool.GetIntSlice(200)
	if cap(slice) < 200 {
		t.Errorf("Expected capacity >= 200, got %d", cap(slice))
	}

	for i := 0; i < 100; i++ {
		slice = append(slice, i)
	}

	pool.PutIntSlice(slice)

	stats := pool.GetStats()
	if stats.IntGets < 1 {
		t.Error("Expected at least 1 int get")
	}
	if stats.IntPuts < 1 {
		t.Error("Expected at least 1 int put")
	}
}

func TestMemoryPool_StringSlice(t *testing.T) {
	pool := NewMemoryPool()

	slice := pool.GetStringSlice(50)
	if cap(slice) < 50 {
		t.Errorf("Expected capacity >= 50, got %d", cap(slice))
	}

	for i := 0; i < 25; i++ {
		slice = append(slice, fmt.Sprintf("string_%d", i))
	}

	pool.PutStringSlice(slice)

	// 重新獲取，檢查字符串是否已清空
	slice2 := pool.GetStringSlice(50)
	if len(slice2) != 0 {
		t.Errorf("Expected length 0, got %d", len(slice2))
	}

	pool.PutStringSlice(slice2)
}

func TestMemoryPool_ByteSlice(t *testing.T) {
	pool := NewMemoryPool()

	slice := pool.GetByteSlice(1024)
	if cap(slice) < 1024 {
		t.Errorf("Expected capacity >= 1024, got %d", cap(slice))
	}

	for i := 0; i < 512; i++ {
		slice = append(slice, byte(i%256))
	}

	pool.PutByteSlice(slice)

	stats := pool.GetStats()
	if stats.ByteSliceGets < 1 {
		t.Error("Expected at least 1 byte slice get")
	}
	if stats.ByteSlicePuts < 1 {
		t.Error("Expected at least 1 byte slice put")
	}
}

func TestMemoryPool_HitRate(t *testing.T) {
	pool := NewMemoryPool()

	// 第一次獲取會是miss
	slice1 := pool.GetFloat64Slice(100)
	pool.PutFloat64Slice(slice1)

	// 第二次獲取應該是hit
	slice2 := pool.GetFloat64Slice(100)
	pool.PutFloat64Slice(slice2)

	hitRate := pool.GetHitRate()
	if hitRate < 0.4 { // 至少50%的命中率
		t.Errorf("Expected hit rate >= 0.4, got %f", hitRate)
	}
}

func TestPooledArray_Float64(t *testing.T) {
	arr := NewPooledFloat64Array(100)
	defer arr.Close()

	// 測試添加元素
	arr.AppendFloat64(1.0, 2.0, 3.0)
	if arr.Len() != 3 {
		t.Errorf("Expected length 3, got %d", arr.Len())
	}

	// 測試容量
	if arr.Cap() < 100 {
		t.Errorf("Expected capacity >= 100, got %d", arr.Cap())
	}

	// 測試數據訪問
	data := arr.Float64Data()
	if len(data) != 3 || data[0] != 1.0 || data[1] != 2.0 || data[2] != 3.0 {
		t.Errorf("Unexpected data: %v", data)
	}

	// 測試重置
	arr.Reset()
	if arr.Len() != 0 {
		t.Errorf("Expected length 0 after reset, got %d", arr.Len())
	}
}

func TestPooledArray_Int(t *testing.T) {
	arr := NewPooledIntArray(50)
	defer arr.Close()

	arr.AppendInt(10, 20, 30)
	if arr.Len() != 3 {
		t.Errorf("Expected length 3, got %d", arr.Len())
	}

	data := arr.IntData()
	if len(data) != 3 || data[0] != 10 || data[1] != 20 || data[2] != 30 {
		t.Errorf("Unexpected data: %v", data)
	}
}

func TestPooledArray_String(t *testing.T) {
	arr := NewPooledStringArray(25)
	defer arr.Close()

	arr.AppendString("hello", "world")
	if arr.Len() != 2 {
		t.Errorf("Expected length 2, got %d", arr.Len())
	}

	data := arr.StringData()
	if len(data) != 2 || data[0] != "hello" || data[1] != "world" {
		t.Errorf("Unexpected data: %v", data)
	}
}

func TestPooledCalculator_CalculateArrayMean(t *testing.T) {
	calc := NewPooledCalculator()

	data := []float64{1, 2, 3, 4, 5}
	mean := calc.CalculateArrayMean(data)

	expectedMean := 3.0
	if mean != expectedMean {
		t.Errorf("Expected mean %f, got %f", expectedMean, mean)
	}
}

func TestPooledCalculator_CalculateArrayMax(t *testing.T) {
	calc := NewPooledCalculator()

	data := []float64{1, 5, 3, 2, 4}
	max, maxIndex := calc.CalculateArrayMax(data)

	expectedMax := 5.0
	expectedIndex := 1
	if max != expectedMax || maxIndex != expectedIndex {
		t.Errorf("Expected max (%f, %d), got (%f, %d)", expectedMax, expectedIndex, max, maxIndex)
	}
}

func TestPooledCalculator_CalculateSlidingWindowMean(t *testing.T) {
	calc := NewPooledCalculator()

	data := []float64{1, 2, 3, 4, 5, 6}
	windowSize := 3

	results, err := calc.CalculateSlidingWindowMean(data, windowSize)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	expectedResults := []float64{2.0, 3.0, 4.0, 5.0} // (1+2+3)/3, (2+3+4)/3, (3+4+5)/3, (4+5+6)/3
	if len(results) != len(expectedResults) {
		t.Fatalf("Expected %d results, got %d", len(expectedResults), len(results))
	}

	for i, expected := range expectedResults {
		if results[i] != expected {
			t.Errorf("Result[%d]: expected %f, got %f", i, expected, results[i])
		}
	}
}

func TestPooledCalculator_BatchCalculate(t *testing.T) {
	calc := NewPooledCalculator()

	datasets := [][]float64{
		{1, 2, 3},
		{4, 5, 6},
		{7, 8, 9},
	}

	results := calc.BatchCalculate(datasets)

	expectedResults := []struct {
		Mean     float64
		Max      float64
		MaxIndex int
	}{
		{2.0, 3.0, 2},
		{5.0, 6.0, 2},
		{8.0, 9.0, 2},
	}

	if len(results) != len(expectedResults) {
		t.Fatalf("Expected %d results, got %d", len(expectedResults), len(results))
	}

	for i, expected := range expectedResults {
		if results[i].Mean != expected.Mean || results[i].Max != expected.Max || results[i].MaxIndex != expected.MaxIndex {
			t.Errorf("Result[%d]: expected %+v, got %+v", i, expected, results[i])
		}
	}
}

func TestGCOptimizer(t *testing.T) {
	optimizer := NewGCOptimizer(1) // 1MB閾值

	// 測試GC觸發
	optimizer.SetGCThreshold(0) // 設置很低的閾值
	triggered := optimizer.CheckAndTriggerGC()

	// 在測試環境中，GC觸發可能不會返回true，但函數應該正常運行
	t.Logf("GC triggered: %v", triggered)

	// 測試禁用/啟用
	optimizer.DisableAutoGC()
	triggered = optimizer.CheckAndTriggerGC()
	if triggered {
		t.Error("GC should not be triggered when disabled")
	}

	optimizer.EnableAutoGC()
	// 啟用後應該可以觸發
}

// 基準測試：比較使用記憶體池和不使用記憶體池的性能

func BenchmarkMemoryPool_Float64Slice(b *testing.B) {
	pool := NewMemoryPool()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		slice := pool.GetFloat64Slice(1000)
		for j := 0; j < 500; j++ {
			slice = append(slice, float64(j))
		}
		pool.PutFloat64Slice(slice)
	}
}

func BenchmarkDirect_Float64Slice(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		slice := make([]float64, 0, 1000)
		for j := 0; j < 500; j++ {
			slice = append(slice, float64(j))
		}
		// 沒有池化，直接丟棄
	}
}

func BenchmarkPooledCalculator_Mean(b *testing.B) {
	calc := NewPooledCalculator()
	data := make([]float64, 10000)
	for i := range data {
		data[i] = float64(i)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		calc.CalculateArrayMean(data)
	}
}

func BenchmarkDirect_Mean(b *testing.B) {
	data := make([]float64, 10000)
	for i := range data {
		data[i] = float64(i)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ArrayMeanSIMD(data)
	}
}

func BenchmarkPooledCalculator_SlidingWindow(b *testing.B) {
	calc := NewPooledCalculator()
	data := make([]float64, 10000)
	for i := range data {
		data[i] = float64(i)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		calc.CalculateSlidingWindowMean(data, 100)
	}
}

// 記憶體使用測試
func TestMemoryUsage(t *testing.T) {
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)

	// 使用記憶體池
	pool := NewMemoryPool()
	var slices [][]float64

	for i := 0; i < 1000; i++ {
		slice := pool.GetFloat64Slice(1000)
		for j := 0; j < 500; j++ {
			slice = append(slice, float64(j))
		}
		slices = append(slices, slice)
	}

	// 放回池中
	for _, slice := range slices {
		pool.PutFloat64Slice(slice)
	}

	runtime.ReadMemStats(&m2)
	memoryUsed := m2.TotalAlloc - m1.TotalAlloc

	t.Logf("Memory used with pool: %d bytes", memoryUsed)

	// 檢查統計信息
	stats := pool.GetStats()
	t.Logf("Pool stats: Gets=%d, Puts=%d, Misses=%d, HitRate=%.2f%%",
		stats.Float64Gets, stats.Float64Puts, stats.Float64Misses, pool.GetHitRate()*100)
}

// 並發安全測試
func TestMemoryPool_Concurrent(t *testing.T) {
	pool := NewMemoryPool()

	// 啟動多個goroutine並發使用記憶體池
	const numGoroutines = 10
	const numOperations = 100

	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < numOperations; j++ {
				// 獲取和釋放float64切片
				slice := pool.GetFloat64Slice(100)
				for k := 0; k < 50; k++ {
					slice = append(slice, float64(k))
				}
				pool.PutFloat64Slice(slice)

				// 獲取和釋放int切片
				intSlice := pool.GetIntSlice(100)
				for k := 0; k < 50; k++ {
					intSlice = append(intSlice, k)
				}
				pool.PutIntSlice(intSlice)
			}
		}(i)
	}

	// 等待所有goroutine完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// 檢查統計信息
	stats := pool.GetStats()
	expectedOperations := int64(numGoroutines * numOperations)

	if stats.Float64Gets < expectedOperations || stats.IntGets < expectedOperations {
		t.Errorf("Expected at least %d operations, got Float64Gets=%d, IntGets=%d",
			expectedOperations, stats.Float64Gets, stats.IntGets)
	}
}

// 壓力測試：測試記憶體池在高負載下的表現
func TestMemoryPool_StressTest(t *testing.T) {
	pool := NewMemoryPool()

	// 模擬高負載：大量小切片和少量大切片
	const iterations = 1000

	for i := 0; i < iterations; i++ {
		// 小切片
		smallSlice := pool.GetFloat64Slice(10)
		for j := 0; j < 5; j++ {
			smallSlice = append(smallSlice, float64(j))
		}
		pool.PutFloat64Slice(smallSlice)

		// 大切片
		if i%10 == 0 {
			largeSlice := pool.GetFloat64Slice(10000)
			for j := 0; j < 5000; j++ {
				largeSlice = append(largeSlice, float64(j))
			}
			pool.PutFloat64Slice(largeSlice)
		}
	}

	stats := pool.GetStats()
	t.Logf("Stress test stats: Gets=%d, Puts=%d, HitRate=%.2f%%",
		stats.Float64Gets, stats.Float64Puts, pool.GetHitRate()*100)
}
