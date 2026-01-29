package calculator

import (
	"count_mean/internal/models"
)

// DataProvider 提供滑動窗口計算所需的數據訪問接口.
type DataProvider interface {
	// GetValue 獲取指定數據索引和通道索引的值.
	GetValue(dataIdx, channelIdx int) float64
	// Length 返回數據長度.
	Length() int
}

// SlidingWindowResult 滑動窗口計算結果.
type SlidingWindowResult struct {
	MaxMean      float64 // 最大平均值
	BestStartIdx int     // 最佳窗口起始索引
}

// SlidingWindowCalculator 滑動窗口計算器.
type SlidingWindowCalculator struct{}

// NewSlidingWindowCalculator 創建新的滑動窗口計算器.
func NewSlidingWindowCalculator() *SlidingWindowCalculator {
	return &SlidingWindowCalculator{}
}

// CalculateMaxMean 計算指定範圍內的最大平均值（使用增量滑動窗口優化）。
// provider: 數據提供者，channelIdx: 通道索引，windowSize: 窗口大小，
// startIdx: 起始索引（包含），endIdx: 結束索引（包含）。
func (*SlidingWindowCalculator) CalculateMaxMean(
	provider DataProvider,
	channelIdx, windowSize, startIdx, endIdx int,
) SlidingWindowResult {
	bestStartIdx := startIdx

	// 計算第一個窗口的初始和
	windowSum := 0.0
	for i := startIdx; i < startIdx+windowSize; i++ {
		windowSum += provider.GetValue(i, channelIdx)
	}

	// 計算第一個窗口的平均值
	maxMean := windowSum / float64(windowSize)

	// 使用增量滑動窗口計算後續窗口
	for winStartIdx := startIdx + 1; winStartIdx <= endIdx-windowSize+1; winStartIdx++ {
		// 移除窗口左側的值
		windowSum -= provider.GetValue(winStartIdx-1, channelIdx)

		// 添加窗口右側的新值
		rightIdx := winStartIdx + windowSize - 1
		windowSum += provider.GetValue(rightIdx, channelIdx)

		// 計算當前窗口的平均值
		currentMean := windowSum / float64(windowSize)
		if currentMean > maxMean {
			maxMean = currentMean
			bestStartIdx = winStartIdx
		}
	}

	return SlidingWindowResult{
		MaxMean:      maxMean,
		BestStartIdx: bestStartIdx,
	}
}

// EMGDatasetProvider 實現 DataProvider 接口，用於 EMGDataset.
type EMGDatasetProvider struct {
	dataset *models.EMGDataset
}

// NewEMGDatasetProvider 創建 EMGDataset 的數據提供者.
func NewEMGDatasetProvider(dataset *models.EMGDataset) *EMGDatasetProvider {
	return &EMGDatasetProvider{dataset: dataset}
}

// GetValue 獲取指定數據索引和通道索引的值.
func (p *EMGDatasetProvider) GetValue(dataIdx, channelIdx int) float64 {
	if dataIdx >= len(p.dataset.Data) {
		return 0
	}

	if channelIdx >= len(p.dataset.Data[dataIdx].Channels) {
		return 0
	}

	return p.dataset.Data[dataIdx].Channels[channelIdx]
}

// Length 返回數據長度.
func (p *EMGDatasetProvider) Length() int {
	return len(p.dataset.Data)
}

// GetTime 獲取指定索引的時間值.
func (p *EMGDatasetProvider) GetTime(dataIdx int) float64 {
	if dataIdx >= len(p.dataset.Data) {
		return 0
	}

	return p.dataset.Data[dataIdx].Time
}
