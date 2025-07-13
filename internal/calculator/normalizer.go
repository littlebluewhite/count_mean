package calculator

import (
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/util"
	"fmt"
	"strings"
	"time"
)

// Normalizer 處理數據標準化
type Normalizer struct {
	scalingFactor int
	logger        *logging.Logger
}

// NewNormalizer 創建新的標準化器
func NewNormalizer(scalingFactor int) *Normalizer {
	return &Normalizer{
		scalingFactor: scalingFactor,
		logger:        logging.GetLogger("normalizer"),
	}
}

// Normalize 標準化數據集（每個值除以參考值）
func (n *Normalizer) Normalize(dataset *models.EMGDataset, reference *models.EMGDataset) (*models.EMGDataset, error) {
	startTime := time.Now()

	if dataset == nil || reference == nil {
		err := fmt.Errorf("數據集或參考數據集為空")
		n.logger.Error("標準化輸入驗證失敗", err, map[string]interface{}{
			"dataset_nil":   dataset == nil,
			"reference_nil": reference == nil,
		})
		return nil, err
	}

	n.logger.Info("開始數據標準化", map[string]interface{}{
		"dataset_points":   len(dataset.Data),
		"reference_points": len(reference.Data),
		"scaling_factor":   n.scalingFactor,
	})

	if len(dataset.Data) == 0 || len(reference.Data) == 0 {
		err := fmt.Errorf("數據集或參考數據集為空")
		n.logger.Error("標準化數據為空", err, map[string]interface{}{
			"dataset_length":   len(dataset.Data),
			"reference_length": len(reference.Data),
		})
		return nil, err
	}

	// 檢查通道數是否匹配
	if len(dataset.Data[0].Channels) != len(reference.Data[0].Channels) {
		err := fmt.Errorf("數據集和參考數據集的通道數不匹配")
		n.logger.Error("通道數不匹配", err, map[string]interface{}{
			"dataset_channels":   len(dataset.Data[0].Channels),
			"reference_channels": len(reference.Data[0].Channels),
		})
		return nil, err
	}

	result := &models.EMGDataset{
		Headers:               make([]string, len(dataset.Headers)),
		Data:                  make([]models.EMGData, 0, len(dataset.Data)),
		OriginalTimePrecision: dataset.OriginalTimePrecision,
	}

	// 複製標題
	copy(result.Headers, dataset.Headers)

	// 獲取參考值（使用第一行數據）
	refValues := reference.Data[0].Channels
	n.logger.Debug("使用參考值", map[string]interface{}{
		"reference_values": refValues,
	})

	// 檢查是否有除以零的情況
	for i, refVal := range refValues {
		if refVal == 0 {
			err := fmt.Errorf("參考值在通道 %d 為零，無法進行標準化", i+1)
			n.logger.Error("參考值為零", err, map[string]interface{}{
				"channel_index":   i + 1,
				"reference_value": refVal,
			})
			return nil, err
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

	duration := time.Since(startTime)
	n.logger.Info("數據標準化完成", map[string]interface{}{
		"duration_ms":      duration.Milliseconds(),
		"processed_points": len(result.Data),
		"channel_count":    len(result.Data[0].Channels),
	})

	return result, nil
}

// NormalizeFromRawData 從原始字符串數據進行標準化
func (n *Normalizer) NormalizeFromRawData(records [][]string, reference [][]string) (*models.EMGDataset, error) {
	n.logger.Info("開始從原始數據進行標準化", map[string]interface{}{
		"main_records":      len(records),
		"reference_records": len(reference),
	})

	dataset, err := n.parseRawData(records)
	if err != nil {
		n.logger.Error("主數據解析失敗", err)
		return nil, fmt.Errorf("解析主數據失敗: %w", err)
	}

	refDataset, err := n.parseReferenceData(reference)
	if err != nil {
		n.logger.Error("參考數據解析失敗", err)
		return nil, fmt.Errorf("解析參考數據失敗: %w", err)
	}

	return n.Normalize(dataset, refDataset)
}

// parseRawData 解析原始字符串數據
func (n *Normalizer) parseRawData(records [][]string) (*models.EMGDataset, error) {
	n.logger.Debug("開始解析標準化原始數據", map[string]interface{}{
		"record_count":   len(records),
		"scaling_factor": n.scalingFactor,
	})

	if len(records) < 2 {
		err := fmt.Errorf("數據至少需要包含標題行和一行數據")
		n.logger.Error("標準化原始數據結構驗證失敗", err, map[string]interface{}{
			"record_count": len(records),
		})
		return nil, err
	}

	dataset := &models.EMGDataset{
		Headers:               make([]string, len(records[0])),
		Data:                  make([]models.EMGData, 0, len(records)-1),
		OriginalTimePrecision: n.detectTimePrecision(records),
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
		if row[0] == "" {
			n.logger.Debug("跳過空白時間行", map[string]interface{}{
				"row_number": i + 1,
			})
			continue // 跳過空白時間值的行
		}

		timeVal, err := util.Str2Number[float64, int](row[0], n.scalingFactor)
		if err != nil {
			n.logger.Warn("時間值解析失敗，跳過此行", map[string]interface{}{
				"row_number": i + 1,
				"time_value": row[0],
				"error":      err.Error(),
			})
			continue // 跳過無法解析的行
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

	// 檢查是否有有效數據
	if len(dataset.Data) == 0 {
		return nil, fmt.Errorf("解析後數據集為空，所有行都被跳過")
	}

	n.logger.Debug("標準化原始數據解析完成", map[string]interface{}{
		"parsed_records": len(dataset.Data),
		"channel_count":  len(dataset.Data[0].Channels),
		"header_count":   len(dataset.Headers),
	})

	return dataset, nil
}

// parseReferenceData 解析參考數據（特殊格式，第一列是標籤而非時間）
func (n *Normalizer) parseReferenceData(records [][]string) (*models.EMGDataset, error) {
	n.logger.Debug("開始解析參考數據", map[string]interface{}{
		"record_count": len(records),
	})

	if len(records) < 2 {
		err := fmt.Errorf("參考數據至少需要包含標題行和一行數據")
		n.logger.Error("參考數據結構驗證失敗", err, map[string]interface{}{
			"record_count": len(records),
		})
		return nil, err
	}

	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}

	// 複製標題
	copy(dataset.Headers, records[0])

	// 解析數據行（跳過標題）
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue // 跳過無效行
		}

		// 參考數據不需要時間值，使用索引作為時間
		data := models.EMGData{
			Time:     float64(i - 1), // 使用行索引作為時間
			Channels: make([]float64, 0, len(row)-1),
		}

		// 解析通道數據（從第二列開始，跳過標籤列）
		for j := 1; j < len(row); j++ {
			val, err := util.Str2Number[float64, int](row[j], n.scalingFactor)
			if err != nil {
				n.logger.Error("參考數據通道值解析失敗", err, map[string]interface{}{
					"row_number":    i + 1,
					"column_number": j + 1,
					"value":         row[j],
				})
				return nil, fmt.Errorf("解析參考數據失敗在行 %d 列 %d: %w", i+1, j+1, err)
			}
			data.Channels = append(data.Channels, val)
		}

		dataset.Data = append(dataset.Data, data)
	}

	// 檢查是否有有效數據
	if len(dataset.Data) == 0 {
		return nil, fmt.Errorf("參考數據解析後為空")
	}

	n.logger.Debug("參考數據解析完成", map[string]interface{}{
		"parsed_records": len(dataset.Data),
		"channel_count":  len(dataset.Data[0].Channels),
		"header_count":   len(dataset.Headers),
	})

	return dataset, nil
}

// detectTimePrecision 檢測時間欄位的小數位數
func (n *Normalizer) detectTimePrecision(records [][]string) int {
	if len(records) < 2 {
		return 2 // 預設精度
	}

	maxPrecision := 0
	// 檢查前幾行數據來確定時間精度
	for i := 1; i < len(records) && i <= 10; i++ {
		if len(records[i]) > 0 {
			timeStr := records[i][0]
			precision := n.getDecimalPrecision(timeStr)
			if precision > maxPrecision {
				maxPrecision = precision
			}
		}
	}

	// 如果檢測不到小數位數，預設為 2
	if maxPrecision == 0 {
		maxPrecision = 2
	}

	n.logger.Debug("檢測到時間精度", map[string]interface{}{
		"detected_precision": maxPrecision,
	})

	return maxPrecision
}

// getDecimalPrecision 獲取字串中小數點後的位數
func (n *Normalizer) getDecimalPrecision(numStr string) int {
	// 移除空白字元
	numStr = strings.TrimSpace(numStr)

	// 找到小數點位置
	dotIndex := strings.Index(numStr, ".")
	if dotIndex == -1 {
		return 0 // 沒有小數點
	}

	// 計算小數點後的位數
	return len(numStr) - dotIndex - 1
}
