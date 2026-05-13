package chart

import "math"

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
