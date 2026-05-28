package chart

import (
	"math"
	"sort"
)

// triangleAreaFactor LTTB 三角形面積係數。
const triangleAreaFactor = 0.5

// LTTBDownsample 對 (xs, ys) 配對序列做 LTTB 降採樣，回傳 threshold 個保留點的索引。
// 用「索引」回傳讓 caller 能以同一組索引同步降採樣多個共享 X 的序列
// （例如 CCI 12-pair 共用 TimeValues），保證所有曲線 X 對齊。
//
// 退化條件：xs/ys 長度不一致 → panic（caller bug）；threshold >= len(xs)
// 或 <= 2 或 len(xs) <= 2 → 回傳所有索引。
func LTTBDownsample(xs, ys []float64, threshold int) []int {
	n := len(xs)
	if n != len(ys) {
		panic("LTTBDownsample: xs/ys 長度不一致")
	}

	if threshold >= n || threshold <= 2 || n <= 2 {
		indices := make([]int, n)
		for i := range indices {
			indices[i] = i
		}

		return indices
	}

	indices := make([]int, 0, threshold)
	indices = append(indices, 0)

	every := float64(n-2) / float64(threshold-2)
	lastSelected := 0

	for i := 0; i < threshold-2; i++ {
		bucketStart := int(math.Floor(float64(i)*every)) + 1
		bucketEnd := int(math.Floor(float64(i+1)*every)) + 1

		if bucketEnd > n-1 {
			bucketEnd = n - 1
		}

		nextStart := bucketEnd
		nextEnd := int(math.Floor(float64(i+2)*every)) + 1

		if nextEnd > n {
			nextEnd = n
		}

		avgX, avgY := nextBucketAverage(xs, ys, nextStart, nextEnd)

		maxIdx := findMaxAreaIndex(xs, ys, bucketStart, bucketEnd, lastSelected, avgX, avgY)

		indices = append(indices, maxIdx)
		lastSelected = maxIdx
	}

	indices = append(indices, n-1)

	return indices
}

// nextBucketAverage 計算下一桶 (nextStart..nextEnd) 的 (avgX, avgY)。
// 若 nextEnd <= nextStart（最後一桶情境），fallback 為最末點。
func nextBucketAverage(xs, ys []float64, nextStart, nextEnd int) (float64, float64) {
	count := nextEnd - nextStart
	if count <= 0 {
		last := len(xs) - 1

		return xs[last], ys[last]
	}

	var avgX, avgY float64

	for j := nextStart; j < nextEnd; j++ {
		avgX += xs[j]
		avgY += ys[j]
	}

	return avgX / float64(count), avgY / float64(count)
}

// findMaxAreaIndex 在 [bucketStart, bucketEnd) 範圍內尋找與
// (xs[lastSelected], ys[lastSelected]) 及 (avgX, avgY) 構成最大三角形面積的點。
func findMaxAreaIndex(
	xs, ys []float64, bucketStart, bucketEnd, lastSelected int, avgX, avgY float64,
) int {
	maxArea := -1.0
	maxIdx := bucketStart
	ax := xs[lastSelected]
	ay := ys[lastSelected]

	for j := bucketStart; j < bucketEnd; j++ {
		area := math.Abs(
			(ax-avgX)*(ys[j]-ay)-(ax-xs[j])*(avgY-ay),
		) * triangleAreaFactor
		if area > maxArea {
			maxArea = area
			maxIdx = j
		}
	}

	return maxIdx
}

// UnionLTTBIndices 對 series 中每條曲線各自跑 LTTBDownsample，再將所有保留索引取
// union、排序後回傳。為什麼用「索引 union」而不是「第一條曲線當 representative」：
//   - representative 策略在某條曲線平緩時，會讓其他曲線在同 bucket 的高 variance
//     窄峰被遺漏（峰值點不在 representative 曲線的 bucket 最大面積位置）。
//   - 每條曲線各自跑 LTTB 再 union，保留所有曲線自身的高 variance 點，
//     代價是索引總數可能稍超 threshold（最壞 len(series) × threshold 個點）。
//
// threshold*2 cap（由 capUnionIndices 實施）：
//   - 若 series 很多（例如 CCI 12 對），union 最壞可達 12 × threshold 個索引，
//     直接送 ECharts 會壓垮互動效能。
//   - 2× threshold 在保留 LTTB 高 variance 點的同時，限制前端最大渲染點數
//     （末筆保留使輸出至多 threshold*2 + 1 個索引）。
//
// 退化條件（回傳 nil）：
//   - len(time) <= threshold：點數不超過閾值，不需要降採樣。
//   - threshold <= 0：無效閾值。
//   - len(series) == 0：沒有曲線可處理。
//
// Caller invariant: 每個 series[i] 的長度必須等於 len(time)，否則 LTTBDownsample
// 會 panic（caller bug，與 LTTBDownsample 的合約一致；長度校驗屬於 adapter 層）。
func UnionLTTBIndices(time []float64, series [][]float64, threshold int) []int {
	if len(time) <= threshold || threshold <= 0 || len(series) == 0 {
		return nil
	}

	seen := make(map[int]struct{}, threshold)

	for _, s := range series {
		idx := LTTBDownsample(time, s, threshold)
		for _, i := range idx {
			seen[i] = struct{}{}
		}
	}

	indices := make([]int, 0, len(seen))
	for i := range seen {
		indices = append(indices, i)
	}

	sort.Ints(indices)

	return capUnionIndices(indices, threshold*2)
}

// capUnionIndices 在 indices 超出 limit 時用 stride decimation 壓回 limit 上限，
// 保留首尾索引以維持 zoom 範圍。indices 必須已遞增排序。
// 參數命名為 limit 避免與內建 cap() 混淆。
//
// 注意：stride 必須用 ceiling division — `len / limit` 的 floor 結果在
// `len % limit != 0` 時會給出太小的 stride。例：len=29999, limit=10000 用 floor
// 算出 stride=2，輸出 ≈ 15k 點仍超過 limit。ceiling `(len + limit - 1) / limit`
// 保證 `len / stride <= limit`，最後 append 末筆讓輸出至多 limit+1。
func capUnionIndices(indices []int, limit int) []int {
	if limit < 1 || len(indices) <= limit {
		return indices
	}

	stride := (len(indices) + limit - 1) / limit
	if stride < 2 {
		stride = 2
	}

	capped := make([]int, 0, limit+1)
	for i := 0; i < len(indices); i += stride {
		capped = append(capped, indices[i])
	}

	// 確保最後一筆保留，否則 zoom 末端會被截掉
	if last := indices[len(indices)-1]; len(capped) == 0 || capped[len(capped)-1] != last {
		capped = append(capped, last)
	}

	return capped
}
