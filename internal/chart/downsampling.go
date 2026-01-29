package chart

import (
	"math"

	"count_mean/internal/models"
)

// Downsampling constants.
const (
	triangleAreaFactor = 0.5 // 三角形面積計算係數
)

// lttbBucketRange 表示 LTTB 桶的範圍.
type lttbBucketRange struct {
	start  int
	end    int
	length int
}

// lttbDownsample LTTB 降採樣算法實現.
func lttbDownsample(dataset *models.EMGDataset, threshold int) *models.EMGDataset {
	dataLength := len(dataset.Data)
	if threshold >= dataLength || threshold == 0 {
		return dataset
	}

	sampled := &models.EMGDataset{
		Headers: dataset.Headers,
		Data:    make([]models.EMGData, 0, threshold),
	}

	// 始終包含第一個和最後一個點
	sampled.Data = append(sampled.Data, dataset.Data[0])

	// 計算每個桶的大小
	every := float64(dataLength-2) / float64(threshold-2)

	for i := 0; i < threshold-2; i++ {
		bucketRange := calculateBucketRange(i, every, dataLength)
		avgX, avgY := calculateBucketAverage(dataset, bucketRange)
		maxAreaPoint := findMaxAreaPoint(dataset, sampled, bucketRange, avgX, avgY)

		sampled.Data = append(sampled.Data, dataset.Data[maxAreaPoint])
	}

	// 添加最後一個點
	sampled.Data = append(sampled.Data, dataset.Data[dataLength-1])

	return sampled
}

// calculateBucketRange 計算桶的範圍.
func calculateBucketRange(bucketIdx int, every float64, dataLength int) lttbBucketRange {
	start := int(math.Floor(float64(bucketIdx)*every)) + 1
	end := int(math.Floor(float64(bucketIdx+1)*every)) + 1

	if end >= dataLength {
		end = dataLength
	}

	return lttbBucketRange{
		start:  start,
		end:    end,
		length: end - start,
	}
}

// calculateBucketAverage 計算桶內數據的平均值.
func calculateBucketAverage(dataset *models.EMGDataset, bucket lttbBucketRange) (float64, []float64) {
	avgX := 0.0
	channelCount := len(dataset.Data[0].Channels)
	avgY := make([]float64, channelCount)

	for j := 0; j < bucket.length; j++ {
		idx := bucket.start + j
		avgX += dataset.Data[idx].Time

		for k := 0; k < channelCount && k < len(dataset.Data[idx].Channels); k++ {
			avgY[k] += dataset.Data[idx].Channels[k]
		}
	}

	avgX /= float64(bucket.length)

	for k := range avgY {
		avgY[k] /= float64(bucket.length)
	}

	return avgX, avgY
}

// findMaxAreaPoint 在桶內尋找最大三角形面積的點.
func findMaxAreaPoint(dataset, sampled *models.EMGDataset, bucket lttbBucketRange, avgX float64, avgY []float64) int {
	maxArea := -1.0
	maxAreaPoint := bucket.start

	lastPoint := sampled.Data[len(sampled.Data)-1]

	for j := 0; j < bucket.length; j++ {
		idx := bucket.start + j
		area := calculateTriangleArea(dataset.Data[idx], lastPoint, avgX, avgY)

		if area > maxArea {
			maxArea = area
			maxAreaPoint = idx
		}
	}

	return maxAreaPoint
}

// calculateTriangleArea 計算三角形面積.
func calculateTriangleArea(current, last models.EMGData, avgX float64, avgY []float64) float64 {
	if len(current.Channels) == 0 || len(last.Channels) == 0 || len(avgY) == 0 {
		return 0.0
	}

	// 使用第一個通道的值計算面積
	return math.Abs(
		(last.Time-avgX)*(current.Channels[0]-last.Channels[0])-
			(last.Time-current.Time)*(avgY[0]-last.Channels[0]),
	) * triangleAreaFactor
}
