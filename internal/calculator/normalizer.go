package calculator

import (
	"count_mean/internal/models"
	"count_mean/util"
	"fmt"
)

// Normalizer 處理數據標準化
type Normalizer struct {
	scalingFactor int
}

// NewNormalizer 創建新的標準化器
func NewNormalizer(scalingFactor int) *Normalizer {
	return &Normalizer{
		scalingFactor: scalingFactor,
	}
}

// Normalize 標準化數據集（每個值除以參考值）
func (n *Normalizer) Normalize(dataset *models.EMGDataset, reference *models.EMGDataset) (*models.EMGDataset, error) {
	if dataset == nil || reference == nil {
		return nil, fmt.Errorf("數據集或參考數據集為空")
	}

	if len(dataset.Data) == 0 || len(reference.Data) == 0 {
		return nil, fmt.Errorf("數據集或參考數據集為空")
	}

	// 檢查通道數是否匹配
	if len(dataset.Data[0].Channels) != len(reference.Data[0].Channels) {
		return nil, fmt.Errorf("數據集和參考數據集的通道數不匹配")
	}

	result := &models.EMGDataset{
		Headers: make([]string, len(dataset.Headers)),
		Data:    make([]models.EMGData, 0, len(dataset.Data)),
	}

	// 複製標題
	copy(result.Headers, dataset.Headers)

	// 獲取參考值（使用第一行數據）
	refValues := reference.Data[0].Channels

	// 檢查是否有除以零的情況
	for i, refVal := range refValues {
		if refVal == 0 {
			return nil, fmt.Errorf("參考值在通道 %d 為零，無法進行標準化", i+1)
		}
	}

	// 標準化每一行數據
	for _, data := range dataset.Data {
		normalizedChannels := make([]float64, len(data.Channels))

		for i, val := range data.Channels {
			normalizedChannels[i] = val / refValues[i]
		}

		normalizedData := models.EMGData{
			Time:     data.Time,
			Channels: normalizedChannels,
		}

		result.Data = append(result.Data, normalizedData)
	}

	return result, nil
}

// NormalizeFromRawData 從原始字符串數據進行標準化
func (n *Normalizer) NormalizeFromRawData(records [][]string, reference [][]string) (*models.EMGDataset, error) {
	dataset, err := n.parseRawData(records)
	if err != nil {
		return nil, fmt.Errorf("解析主數據失敗: %w", err)
	}

	refDataset, err := n.parseRawData(reference)
	if err != nil {
		return nil, fmt.Errorf("解析參考數據失敗: %w", err)
	}

	return n.Normalize(dataset, refDataset)
}

// parseRawData 解析原始字符串數據
func (n *Normalizer) parseRawData(records [][]string) (*models.EMGDataset, error) {
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
		timeVal, err := util.Str2Number[float64, int](row[0], n.scalingFactor)
		if err != nil {
			return nil, fmt.Errorf("解析時間值失敗在第 %d 行: %w", i+1, err)
		}

		// 解析通道數據
		channels := make([]float64, 0, len(row)-1)
		for j := 1; j < len(row); j++ {
			val, err := util.Str2Number[float64, int](row[j], n.scalingFactor)
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
