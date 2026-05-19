package io

import (
	"os"
	"strings"
	"sync"
	"testing"

	"count_mean/internal/config"
	"count_mean/internal/models"
)

// TestBufferPool_GetStringArray_RoundTrip 驗證 GetStringArray/PutStringArray 基本契約：
// Get 回傳的 slice 長度為 0；Put 後 stats.Puts 累加。
func TestBufferPool_GetStringArray_RoundTrip(t *testing.T) {
	bp := NewBufferPool()

	arr := bp.GetStringArray()
	if len(arr) != 0 {
		t.Errorf("Get 回傳的 slice 長度應為 0, got %d", len(arr))
	}

	arr = append(arr, []string{"a", "b"})
	bp.PutStringArray(arr)

	stats := bp.GetStats()
	if stats.StringArrayGets != 1 {
		t.Errorf("StringArrayGets 應為 1, got %d", stats.StringArrayGets)
	}

	if stats.StringArrayPuts != 1 {
		t.Errorf("StringArrayPuts 應為 1, got %d", stats.StringArrayPuts)
	}
}

// TestBufferPool_GetEMGDataSlice_RoundTrip 驗證 EMGData slice round-trip。
func TestBufferPool_GetEMGDataSlice_RoundTrip(t *testing.T) {
	bp := NewBufferPool()

	slice := bp.GetEMGDataSlice()
	if len(slice) != 0 {
		t.Errorf("Get 回傳的 slice 長度應為 0, got %d", len(slice))
	}

	slice = append(slice, models.EMGData{Time: 1.0})
	bp.PutEMGDataSlice(slice)

	stats := bp.GetStats()
	if stats.EMGDataGets != 1 || stats.EMGDataPuts != 1 {
		t.Errorf("EMGData stats 不對: Gets=%d, Puts=%d", stats.EMGDataGets, stats.EMGDataPuts)
	}
}

// TestBufferPool_GetFloat64Slice_RoundTrip 驗證 float64 slice round-trip。
func TestBufferPool_GetFloat64Slice_RoundTrip(t *testing.T) {
	bp := NewBufferPool()

	slice := bp.GetFloat64Slice()
	if len(slice) != 0 {
		t.Errorf("Get 回傳的 slice 長度應為 0, got %d", len(slice))
	}

	slice = append(slice, 1.0, 2.0, 3.0)
	bp.PutFloat64Slice(slice)

	stats := bp.GetStats()
	if stats.Float64Gets != 1 || stats.Float64Puts != 1 {
		t.Errorf("Float64 stats 不對: Gets=%d, Puts=%d", stats.Float64Gets, stats.Float64Puts)
	}
}

// TestBufferPool_PutNilOrEmpty 驗證 Put 空 slice 不會 panic 也不會累加 stats。
func TestBufferPool_PutNilOrEmpty(t *testing.T) {
	bp := NewBufferPool()

	bp.PutStringArray(nil)
	bp.PutEMGDataSlice(nil)
	bp.PutFloat64Slice(nil)

	stats := bp.GetStats()
	if stats.StringArrayPuts != 0 || stats.EMGDataPuts != 0 || stats.Float64Puts != 0 {
		t.Errorf("Put nil 不應累加 stats, got %+v", stats)
	}
}

// TestBufferPool_ReuseRatio 驗證 ReuseRatio 在 GetStats 中正確計算。
func TestBufferPool_ReuseRatio(t *testing.T) {
	bp := NewBufferPool()

	// 3 gets, 2 puts → ratio = 2/3
	for range 3 {
		arr := bp.GetStringArray()
		_ = arr
	}

	bp.PutStringArray(make([][]string, 0, 10))
	bp.PutStringArray(make([][]string, 0, 10))

	stats := bp.GetStats()
	expectedRatio := 2.0 / 3.0
	if stats.ReuseRatio < expectedRatio-0.01 || stats.ReuseRatio > expectedRatio+0.01 {
		t.Errorf("ReuseRatio 應為 %.3f, got %.3f", expectedRatio, stats.ReuseRatio)
	}
}

// TestBufferPool_PutStringArray_ExceedsMaxCap_NotRetained釘住:
// cap 超過 maxStringArrayRetainCap 的 slice Put 進來不放回 pool — 後續 Get 拿到
// 的 slice cap 不該等於剛丟進去的 oversized cap(否則代表 outlier 被 retain 了)。
//
// Verification 方法:Put 一顆超大 slice → Get 多次 → 拿到的 cap 都應該是 New 出的
// 預設值(stringArrayPoolCapacity),不會是 oversized。
func TestBufferPool_PutStringArray_ExceedsMaxCap_NotRetained(t *testing.T) {
	bp := NewBufferPool()

	const oversized = maxStringArrayRetainCap + 1
	bigBuf := make([][]string, 0, oversized)
	bp.PutStringArray(bigBuf)

	// 至少撈 16 次都不該拿到 cap == oversized 的 slice。
	for range 16 {
		s := bp.GetStringArray()
		if cap(s) == oversized {
			t.Fatalf("oversized cap %d 被 retain — M16 max-cap guard 失效", oversized)
		}
		bp.PutStringArray(s)
	}
}

// TestBufferPool_PutEMGDataSlice_ExceedsMaxCap_NotRetained同上,EMGData。
func TestBufferPool_PutEMGDataSlice_ExceedsMaxCap_NotRetained(t *testing.T) {
	bp := NewBufferPool()

	const oversized = maxEMGDataRetainCap + 1
	bigBuf := make([]models.EMGData, 0, oversized)
	bp.PutEMGDataSlice(bigBuf)

	for range 16 {
		s := bp.GetEMGDataSlice()
		if cap(s) == oversized {
			t.Fatalf("oversized cap %d 被 retain — M16 max-cap guard 失效", oversized)
		}
		bp.PutEMGDataSlice(s)
	}
}

// TestBufferPool_PutFloat64Slice_ExceedsMaxCap_NotRetained同上,float64。
// 此 pool 是 streaming hot path,outlier retain 的影響最大。
func TestBufferPool_PutFloat64Slice_ExceedsMaxCap_NotRetained(t *testing.T) {
	bp := NewBufferPool()

	const oversized = maxFloat64RetainCap + 1
	bigBuf := make([]float64, 0, oversized)
	bp.PutFloat64Slice(bigBuf)

	for range 16 {
		s := bp.GetFloat64Slice()
		if cap(s) == oversized {
			t.Fatalf("oversized cap %d 被 retain — M16 max-cap guard 失效", oversized)
		}
		bp.PutFloat64Slice(s)
	}
}

// TestBufferPool_PutFloat64Slice_AtMaxCap_StillRetained邊界:
// cap **正好等於** max retain cap 必須仍被 retain(guard 條件是 > 不是 >=)。
// 避免 max boundary 被誤殺,影響正常工作集 reuse ratio。
//
// 不依賴 sync.Pool 一定撈回剛 Put 的物件(sync.Pool 在 GC pressure 下會清空,
// 整 package race 模式特別容易 trigger),改用 source-level 守 guard 條件本身。
func TestBufferPool_PutFloat64Slice_AtMaxCap_StillRetained(t *testing.T) {
	bp := NewBufferPool()

	// 行為層面:cap == max 的 slice Put 不應 panic、stats 計入 Puts。
	boundary := make([]float64, 0, maxFloat64RetainCap)
	preCount := bp.float64Puts.Load()
	bp.PutFloat64Slice(boundary)
	if got := bp.float64Puts.Load(); got != preCount+1 {
		t.Fatalf("float64Puts 應 +1, got %d (pre=%d)", got, preCount)
	}

	// 結構層面:guard 寫法是 `cap > max` 而非 `>=`,以 source-level 字面守住,
	// 未來 refactor 若不慎收緊成 >= 此 test 立即 fire。
	src, err := os.ReadFile("buffer_pool.go")
	if err != nil {
		t.Fatalf("read buffer_pool.go: %v", err)
	}
	if !strings.Contains(string(src), "cap(slice) > maxFloat64RetainCap") {
		t.Fatalf("buffer_pool.go 缺 `cap(slice) > maxFloat64RetainCap` 邊界守衛,M16 guard 條件被改動")
	}
}

// TestBufferPool_ConcurrentGetPut 在 -race 下驗證 sync.Pool + atomic stats 的線程安全。
func TestBufferPool_ConcurrentGetPut(t *testing.T) {
	bp := NewBufferPool()

	const (
		goroutines = 16
		iterations = 1000
	)

	var wg sync.WaitGroup

	for range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range iterations {
				s := bp.GetStringArray()
				s = append(s, []string{"x"})
				bp.PutStringArray(s)

				e := bp.GetEMGDataSlice()
				e = append(e, models.EMGData{Time: 1})
				bp.PutEMGDataSlice(e)

				f := bp.GetFloat64Slice()
				f = append(f, 1.0)
				bp.PutFloat64Slice(f)
			}
		}()
	}

	wg.Wait()

	stats := bp.GetStats()
	expected := int64(goroutines * iterations)

	if stats.StringArrayGets != expected || stats.StringArrayPuts != expected {
		t.Errorf("StringArray 並發 stats 不對: Gets=%d, Puts=%d, expected=%d",
			stats.StringArrayGets, stats.StringArrayPuts, expected)
	}

	if stats.EMGDataGets != expected || stats.EMGDataPuts != expected {
		t.Errorf("EMGData 並發 stats 不對: Gets=%d, Puts=%d, expected=%d",
			stats.EMGDataGets, stats.EMGDataPuts, expected)
	}

	if stats.Float64Gets != expected || stats.Float64Puts != expected {
		t.Errorf("Float64 並發 stats 不對: Gets=%d, Puts=%d, expected=%d",
			stats.Float64Gets, stats.Float64Puts, expected)
	}
}

// BenchmarkBufferPool_StringArray 並發測量 Get/Put hot-path 的 allocs/op。
// 預期 wrapper struct + sync.Pool 後 allocs/op 維持低水位（穩態下應接近 0）。
func BenchmarkBufferPool_StringArray(b *testing.B) {
	bp := NewBufferPool()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s := bp.GetStringArray()
			s = append(s, []string{"x"})
			bp.PutStringArray(s)
		}
	})
}

// BenchmarkBufferPool_EMGData 並發測量 EMGData slice hot-path。
func BenchmarkBufferPool_EMGData(b *testing.B) {
	bp := NewBufferPool()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s := bp.GetEMGDataSlice()
			s = append(s, models.EMGData{Time: 1.0})
			bp.PutEMGDataSlice(s)
		}
	})
}

// BenchmarkBufferPool_Float64 並發測量 float64 slice hot-path（主要 streaming 路徑）。
func BenchmarkBufferPool_Float64(b *testing.B) {
	bp := NewBufferPool()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s := bp.GetFloat64Slice()
			s = append(s, 1.0, 2.0, 3.0)
			bp.PutFloat64Slice(s)
		}
	})
}

// TestBufferPool_ConcurrentResetAndUse釘住 LargeFileHandler.bufferPool 的
// atomic.Pointer 改寫:N goroutine 同時呼叫 ResetBufferPool (Store) 與 reader
// (GetBufferPoolStats → 內部 Load)— 不改寫前 field 是裸 *BufferPool,reader/writer
// 並發訪問同一個 word 是 Go memory model 下的 DATA RACE,`go test -race` 必然 detect。
// 改成 atomic.Pointer 後 Store/Load 提供 happens-before 序列化,race detector 不應再有
// finding。
//
// 設計刻意只用 public API:GetBufferPoolStats 內部 read `h.bufferPool`,
// ResetBufferPool 內部 write `h.bufferPool` — 涵蓋 read/write 兩條路徑而不需依賴
// internal field 表示法(可同時 compile against unfixed 與 fixed code)。
//
// 注意:本 test 不檢查業務正確性(Reset 期間的 Get/Put 可能落在新 pool 也可能落在
// 舊 pool — 兩者都是 well-defined 行為),只負責讓 race detector 在 unfixed 版本
// 上 fire,fixed 版本上靜默通過。
func TestBufferPool_ConcurrentResetAndUse(t *testing.T) {
	cfg := config.DefaultConfig()
	handler := NewLargeFileHandler(cfg)

	const (
		writers    = 4
		readers    = 4
		iterations = 500
	)

	var wg sync.WaitGroup

	// writers: 撞 ResetBufferPool (寫 h.bufferPool)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				handler.ResetBufferPool()
			}
		}()
	}

	// readers: 撞 GetBufferPoolStats (讀 h.bufferPool)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = handler.GetBufferPoolStats()
			}
		}()
	}

	wg.Wait()
}

// BenchmarkBufferPool_Mixed 模擬 streaming 路徑：每 iter 取 3 種 slice。
func BenchmarkBufferPool_Mixed(b *testing.B) {
	bp := NewBufferPool()

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			f := bp.GetFloat64Slice()
			f = append(f, 1.0)
			bp.PutFloat64Slice(f)

			e := bp.GetEMGDataSlice()
			e = append(e, models.EMGData{Time: 1, Channels: f})
			bp.PutEMGDataSlice(e)
		}
	})
}
