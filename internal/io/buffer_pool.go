package io

import (
	"sync"
	"sync/atomic"

	"count_mean/internal/models"
)

// Buffer pool capacity constants.
const (
	stringArrayPoolCapacity = 1000 // 字符串陣列池預分配容量
	emgDataPoolCapacity     = 2000 // EMG數據池預分配容量
	float64PoolCapacity     = 100  // float64切片池預分配容量
)

// Buffer pool 最大保留容量:Put 進來的 slice 若 cap 超過此上限就丟掉,
// 不放回 pool。一次性大 alloc(例如一筆異常 row 觸發 buffer 暴漲)若被 pool
// 長期持有,常駐記憶體會永遠維持在 worst-case 高水位,做不到 GC。上限取
// 預設容量的 8x — 對正常工作集足夠寬鬆但能擋下單筆 outlier 的長尾駐留。
//
// 與 Go runtime sync.Pool 的關係:sync.Pool 會在 GC 時清空,但只要 hot path
// 仍頻繁 Get/Put,被 retain 的 buffer 永遠來不及 GC。max cap guard 把
// outlier 直接丟掉(讓 caller 下次 Get 拿到 New 出的小 buf),抹平記憶體尖峰。
//
// audit (unchanged):earlier review flagged 可能的「雜湊驗證效能」overhead,
// 但 audit 確認 buffer_pool 沒有 hash 驗證 — cap retain check 純粹是 integer
// 比較 (`cap(arr) > maxStringArrayRetainCap`),已是 O(1) 不可優化形式。
// BenchmarkBufferPool_StringArray ~138 ns/op (1 alloc/op = wrapper boxing,
// 已是穩態最低值);EMGData / Float64 均 0 alloc/op。改用 `bytes.Equal` 或
// `length+first-bytes check` 等替代方案 N/A — 沒有 hash 路徑可改。Followup
// 留作 monitoring:若未來新增 content-based dedup / checksum 路徑,再重評。
const (
	maxStringArrayRetainCap = stringArrayPoolCapacity * 8
	maxEMGDataRetainCap     = emgDataPoolCapacity * 8
	maxFloat64RetainCap     = float64PoolCapacity * 8
)

// slice wrapper structs
// 直接把 slice 丟進 sync.Pool 會觸發 SA6002（slice header 是 value，
// 必須 box 到 interface{} 才能存入 Pool，每次 Put 都會 alloc 24B
// 的 boxed value）。改用 pointer-to-struct 包裝：
//   - Pool 內部只存 *wrapper（一根指針），interface{} 存 pointer 不需 boxing；
//   - wrapper 本身也是 pool 化的，從 Get 拿出的 wrapper 在 Put 時被歸還，
//     達成 steady-state alloc/op = 0。
//
// 詳見：https://staticcheck.dev/docs/checks#SA6002
type stringArrayWrapper struct {
	buf [][]string
}

type emgDataWrapper struct {
	buf []models.EMGData
}

type float64Wrapper struct {
	buf []float64
}

// BufferPool 緩衝區池管理.
//
// 設計（Wave 3 Batch M, P1-A5-4/A5-5）:
//   - 每種 slice 兩個 pool：一個存 *wrapper{buf}，一個存空 wrapper（讓 Put 重用 wrapper）；
//   - stats 用 sync/atomic 計數，徹底拿掉 RWMutex 序列化 hot-path；
//   - Get/Put 都是 lock-free（除了 sync.Pool 本身的 P-local cache fast-path）。
type BufferPool struct {
	// slice pools — 存 *wrapper{buf: <非 nil slice>}
	stringArrayPool sync.Pool
	emgDataPool     sync.Pool
	float64Pool     sync.Pool

	// wrapper pools — 存 *wrapper{buf: nil}，讓 Put 重用 wrapper 結構本身，
	// 避免每次 Put 都 alloc 24B wrapper（達成 steady-state allocs/op = 0）。
	stringArrayWrapperPool sync.Pool
	emgDataWrapperPool     sync.Pool
	float64WrapperPool     sync.Pool

	// Stats — atomic counters.
	stringArrayGets atomic.Int64
	stringArrayPuts atomic.Int64
	emgDataGets     atomic.Int64
	emgDataPuts     atomic.Int64
	float64Gets     atomic.Int64
	float64Puts     atomic.Int64
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
	bp := &BufferPool{}

	// slice pools — New 預配置 wrapper + 初始容量 buf。
	bp.stringArrayPool.New = func() any {
		return &stringArrayWrapper{buf: make([][]string, 0, stringArrayPoolCapacity)}
	}
	bp.emgDataPool.New = func() any {
		return &emgDataWrapper{buf: make([]models.EMGData, 0, emgDataPoolCapacity)}
	}
	bp.float64Pool.New = func() any {
		return &float64Wrapper{buf: make([]float64, 0, float64PoolCapacity)}
	}

	// wrapper pools — New 配置空 wrapper（buf=nil），Put 時填入 caller 歸還的 slice。
	bp.stringArrayWrapperPool.New = func() any { return &stringArrayWrapper{} }
	bp.emgDataWrapperPool.New = func() any { return &emgDataWrapper{} }
	bp.float64WrapperPool.New = func() any { return &float64Wrapper{} }

	return bp
}

// GetStringArray 獲取字符串陣列.
func (bp *BufferPool) GetStringArray() [][]string {
	bp.stringArrayGets.Add(1)

	w, ok := bp.stringArrayPool.Get().(*stringArrayWrapper)
	if !ok || w == nil {
		// Pool.New 永遠回 *stringArrayWrapper；這條 fallback 是 defensive，
		// 避免 nil/型別錯誤連鎖崩潰。
		return make([][]string, 0, stringArrayPoolCapacity)
	}

	buf := w.buf[:0] // 重置長度但保留容量

	// 把空 wrapper 歸還 wrapper pool 供 Put 重用，避免 wrapper alloc 累積。
	w.buf = nil
	bp.stringArrayWrapperPool.Put(w)

	return buf
}

// PutStringArray 歸還字符串陣列.
//
// 對 cap 超過 maxStringArrayRetainCap 的 slice 直接丟棄不放回 pool,避免
// 單筆 outlier alloc 長期駐留拉高常駐記憶體 watermark。stats 仍計入 Puts —
// caller 視為「歸還已完成」,只是 pool 內部選擇不保留這顆超大 buf。
func (bp *BufferPool) PutStringArray(arr [][]string) {
	if cap(arr) == 0 {
		return
	}

	bp.stringArrayPuts.Add(1)

	if cap(arr) > maxStringArrayRetainCap {
		// outlier:不放回 pool。讓 caller 的 reference 退出 scope 後正常 GC。
		return
	}

	// 清空引用以避免記憶體洩漏（外層 slot 還持有 []string 的指針）。
	for i := range arr {
		arr[i] = nil
	}

	// 從 wrapper pool 拿一個空 wrapper 填入 arr，避免每次 Put 都 alloc。
	w, ok := bp.stringArrayWrapperPool.Get().(*stringArrayWrapper)
	if !ok || w == nil {
		w = &stringArrayWrapper{}
	}

	w.buf = arr[:0]
	bp.stringArrayPool.Put(w)
}

// GetEMGDataSlice 獲取EMG數據切片.
func (bp *BufferPool) GetEMGDataSlice() []models.EMGData {
	bp.emgDataGets.Add(1)

	w, ok := bp.emgDataPool.Get().(*emgDataWrapper)
	if !ok || w == nil {
		return make([]models.EMGData, 0, emgDataPoolCapacity)
	}

	buf := w.buf[:0]

	w.buf = nil
	bp.emgDataWrapperPool.Put(w)

	return buf
}

// PutEMGDataSlice 歸還EMG數據切片.
//
// 同 PutStringArray,對 cap 超過 maxEMGDataRetainCap 的 slice 直接丟棄。
func (bp *BufferPool) PutEMGDataSlice(slice []models.EMGData) {
	if cap(slice) == 0 {
		return
	}

	bp.emgDataPuts.Add(1)

	if cap(slice) > maxEMGDataRetainCap {
		return
	}

	// 清空 EMGData 內部引用（Channels 是 slice，可能還持有 float64Pool 出借的 buf）。
	for i := range slice {
		slice[i] = models.EMGData{}
	}

	w, ok := bp.emgDataWrapperPool.Get().(*emgDataWrapper)
	if !ok || w == nil {
		w = &emgDataWrapper{}
	}

	w.buf = slice[:0]
	bp.emgDataPool.Put(w)
}

// GetFloat64Slice 獲取float64切片.
func (bp *BufferPool) GetFloat64Slice() []float64 {
	bp.float64Gets.Add(1)

	w, ok := bp.float64Pool.Get().(*float64Wrapper)
	if !ok || w == nil {
		return make([]float64, 0, float64PoolCapacity)
	}

	buf := w.buf[:0]

	w.buf = nil
	bp.float64WrapperPool.Put(w)

	return buf
}

// PutFloat64Slice 歸還float64切片.
//
// 同 PutStringArray,對 cap 超過 maxFloat64RetainCap 的 slice 直接丟棄。
// 此 pool 是 streaming hot path(parseDataRow 每筆 row 都 Get/Put),最容易因
// 異常超寬 row 累積 outlier。
func (bp *BufferPool) PutFloat64Slice(slice []float64) {
	if cap(slice) == 0 {
		return
	}

	bp.float64Puts.Add(1)

	if cap(slice) > maxFloat64RetainCap {
		return
	}

	w, ok := bp.float64WrapperPool.Get().(*float64Wrapper)
	if !ok || w == nil {
		w = &float64Wrapper{}
	}

	w.buf = slice[:0]
	bp.float64Pool.Put(w)
}

// GetStats 獲取緩衝區池統計.
//
// 各計數獨立 atomic load，snapshot 之間可能跨 nanosecond 邊界；
// ReuseRatio 是純監控指標，這點窗口偏差可接受（不影響正確性）。
func (bp *BufferPool) GetStats() BufferPoolStats {
	stats := BufferPoolStats{
		StringArrayGets: bp.stringArrayGets.Load(),
		StringArrayPuts: bp.stringArrayPuts.Load(),
		EMGDataGets:     bp.emgDataGets.Load(),
		EMGDataPuts:     bp.emgDataPuts.Load(),
		Float64Gets:     bp.float64Gets.Load(),
		Float64Puts:     bp.float64Puts.Load(),
	}

	totalGets := stats.StringArrayGets + stats.EMGDataGets + stats.Float64Gets
	totalPuts := stats.StringArrayPuts + stats.EMGDataPuts + stats.Float64Puts

	if totalGets > 0 {
		stats.ReuseRatio = float64(totalPuts) / float64(totalGets)
	}

	return stats
}
