package calculator

import (
	"count_mean/internal/models"
	"count_mean/util"
	"fmt"
)

// MaxMeanCalculator 處理最大平均值計算
type MaxMeanCalculator struct {
	scalingFactor int
}

// NewMaxMeanCalculator 創建新的最大平均值計算器
func NewMaxMeanCalculator(scalingFactor int) *MaxMeanCalculator {
	return &MaxMeanCalculator{
		scalingFactor: scalingFactor,
	}
}

// Calculate 計算指定窗口大小的最大平均值
func (c *MaxMeanCalculator) Calculate(dataset *models.EMGDataset, windowSize int) ([]models.MaxMeanResult, error) {
	if dataset == nil || len(dataset.Data) < windowSize {
		return nil, fmt.Errorf("數據集無效或窗口大小過大")
	}

	if windowSize < 1 {
		return nil, fmt.Errorf("窗口大小必須大於 0")
	}

	results := make([]models.MaxMeanResult, 0, len(dataset.Headers)-1)

	// 對每個通道計算最大平均值
	for channelIdx := 0; channelIdx < len(dataset.Data[0].Channels); channelIdx++ {
		maxMean := 0.0
		bestStartIdx := 0

		// 滑動窗口計算
		for startIdx := 0; startIdx <= len(dataset.Data)-windowSize; startIdx++ {
			values := make([]float64, 0, windowSize)

			for i := startIdx; i < startIdx+windowSize; i++ {
				if channelIdx < len(dataset.Data[i].Channels) {
					values = append(values, dataset.Data[i].Channels[channelIdx])
				}
			}

			if len(values) == windowSize {
				mean := util.ArrayMean(values)
				if mean > maxMean {
					maxMean = mean
					bestStartIdx = startIdx
				}
			}
		}

		result := models.MaxMeanResult{
			ColumnIndex: channelIdx + 1, // +1 因為第一列是時間
			StartTime:   dataset.Data[bestStartIdx].Time,
			EndTime:     dataset.Data[bestStartIdx+windowSize-1].Time,
			MaxMean:     maxMean,
		}

		results = append(results, result)
	}

	return results, nil
}

// CalculateFromRawData 從原始字符串數據計算
func (c *MaxMeanCalculator) CalculateFromRawData(records [][]string, windowSize int) ([]models.MaxMeanResult, error) {
	dataset, err := c.parseRawData(records)
	if err != nil {
		return nil, fmt.Errorf("解析數據失敗: %w", err)
	}

	return c.Calculate(dataset, windowSize)
}

// parseRawData 解析原始字符串數據
func (c *MaxMeanCalculator) parseRawData(records [][]string) (*models.EMGDataset, error) {
	if len(records) < 2 {
		return nil, fmt.Errorf("數據至少需要包含標題行和一行數據")
	}

	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}

	// 複製標題
	copy(dataset.Headers, records[0])

	// 解析數據
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue // 跳過無效行
		}

		// 解析時間
		timeVal, err := util.Str2Number[float64, int](row[0], c.scalingFactor)
		if err != nil {
			return nil, fmt.Errorf("解析時間值失敗在第 %d 行: %w", i+1, err)
		}

		// 解析通道數據
		channels := make([]float64, 0, len(row)-1)
		for j := 1; j < len(row); j++ {
			val, err := util.Str2Number[float64, int](row[j], c.scalingFactor)
			if err != nil {
				return nil, fmt.Errorf("解析數據失敗在第 %d 行第 %d 列: %w", i+1, j+1, err)
			}
			channels = append(channels, val)
		}

		data := models.EMGData{
			Time:     timeVal,
			Channels: channels,
		}

		dataset.Data = append(dataset.Data, data)
	}

	return dataset, nil
}
