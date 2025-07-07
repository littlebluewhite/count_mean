package calculator

import (
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/util"
	"fmt"
	"math"
	"time"
)

// MaxMeanCalculator 處理最大平均值計算
type MaxMeanCalculator struct {
	scalingFactor int
	logger        *logging.Logger
}

// NewMaxMeanCalculator 創建新的最大平均值計算器
func NewMaxMeanCalculator(scalingFactor int) *MaxMeanCalculator {
	return &MaxMeanCalculator{
		scalingFactor: scalingFactor,
		logger:        logging.GetLogger("max_mean_calculator"),
	}
}

// Calculate 計算指定窗口大小的最大平均值
func (c *MaxMeanCalculator) Calculate(dataset *models.EMGDataset, windowSize int) ([]models.MaxMeanResult, error) {
	startTime := time.Now()
	
	if dataset == nil || len(dataset.Data) == 0 {
		err := fmt.Errorf("數據集為空")
		dataLength := 0
		if dataset != nil {
			dataLength = len(dataset.Data)
		}
		c.logger.Error("計算參數驗證失敗", err, map[string]interface{}{
			"dataset_nil": dataset == nil,
			"data_length": dataLength,
		})
		return nil, err
	}
	
	c.logger.Info("開始最大平均值計算", map[string]interface{}{
		"window_size":   windowSize,
		"data_points":   len(dataset.Data),
		"channel_count": len(dataset.Data[0].Channels),
	})

	if len(dataset.Data) < windowSize {
		err := fmt.Errorf("數據集無效或窗口大小過大")
		c.logger.Error("窗口大小驗證失敗", err, map[string]interface{}{
			"data_length": len(dataset.Data),
			"window_size": windowSize,
		})
		return nil, err
	}

	if windowSize < 1 {
		err := fmt.Errorf("窗口大小必須大於 0")
		c.logger.Error("窗口大小驗證失敗", err, map[string]interface{}{
			"window_size": windowSize,
		})
		return nil, err
	}

	results := make([]models.MaxMeanResult, 0, len(dataset.Headers)-1)

	// 對每個通道計算最大平均值
	for channelIdx := 0; channelIdx < len(dataset.Data[0].Channels); channelIdx++ {
		c.logger.Debug("計算通道最大平均值", map[string]interface{}{
			"channel_index": channelIdx + 1,
		})
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

	duration := time.Since(startTime)
	c.logger.Info("最大平均值計算完成", map[string]interface{}{
		"duration_ms":   duration.Milliseconds(),
		"channel_count": len(results),
		"window_size":   windowSize,
	})

	return results, nil
}

// CalculateFromRawData 從原始字符串數據計算
func (c *MaxMeanCalculator) CalculateFromRawData(records [][]string, windowSize int) ([]models.MaxMeanResult, error) {
	c.logger.Info("開始從原始數據計算最大平均值", map[string]interface{}{
		"record_count": len(records),
		"window_size":  windowSize,
	})

	dataset, err := c.parseRawData(records)
	if err != nil {
		c.logger.Error("原始數據解析失敗", err)
		return nil, fmt.Errorf("解析數據失敗: %w", err)
	}

	return c.Calculate(dataset, windowSize)
}

// CalculateFromRawDataWithRange 從原始字符串數據計算指定時間範圍內的最大平均值
func (c *MaxMeanCalculator) CalculateFromRawDataWithRange(records [][]string, windowSize int, startRange, endRange float64) ([]models.MaxMeanResult, error) {
	c.logger.Info("開始從原始數據計算指定範圍內的最大平均值", map[string]interface{}{
		"record_count": len(records),
		"window_size":  windowSize,
		"start_range":  startRange,
		"end_range":    endRange,
	})

	dataset, err := c.parseRawData(records)
	if err != nil {
		c.logger.Error("原始數據解析失敗", err)
		return nil, fmt.Errorf("解析數據失敗: %w", err)
	}

	return c.CalculateWithRange(dataset, windowSize, startRange, endRange)
}

// CalculateWithRange 計算指定時間範圍內的最大平均值
func (c *MaxMeanCalculator) CalculateWithRange(dataset *models.EMGDataset, windowSize int, startRange, endRange float64) ([]models.MaxMeanResult, error) {
	startTime := time.Now()
	
	if dataset == nil || len(dataset.Data) < windowSize {
		err := fmt.Errorf("數據集無效或窗口大小過大")
		dataLength := 0
		if dataset != nil {
			dataLength = len(dataset.Data)
		}
		c.logger.Error("範圍計算參數驗證失敗", err, map[string]interface{}{
			"dataset_nil": dataset == nil,
			"data_length": dataLength,
			"window_size": windowSize,
		})
		return nil, err
	}
	
	c.logger.Info("開始指定範圍內的最大平均值計算", map[string]interface{}{
		"window_size":   windowSize,
		"start_range":   startRange,
		"end_range":     endRange,
		"data_points":   len(dataset.Data),
		"channel_count": len(dataset.Data[0].Channels),
	})

	if windowSize < 1 {
		err := fmt.Errorf("窗口大小必須大於 0")
		c.logger.Error("窗口大小驗證失敗", err, map[string]interface{}{
			"window_size": windowSize,
		})
		return nil, err
	}

	// 轉換時間範圍為縮放後的值
	scaledStartRange := startRange * math.Pow10(c.scalingFactor)
	scaledEndRange := endRange * math.Pow10(c.scalingFactor)

	// 找到時間範圍內的數據索引
	startIdx := -1
	endIdx := -1

	for i, data := range dataset.Data {
		if startIdx == -1 && data.Time >= scaledStartRange {
			startIdx = i
		}
		if data.Time <= scaledEndRange {
			endIdx = i
		}
	}

	if startIdx == -1 || endIdx == -1 || endIdx-startIdx+1 < windowSize {
		err := fmt.Errorf("指定時間範圍內的數據不足以進行窗口分析")
		c.logger.Error("時間範圍內數據不足", err, map[string]interface{}{
			"start_idx":        startIdx,
			"end_idx":          endIdx,
			"available_points": endIdx - startIdx + 1,
			"required_points":  windowSize,
			"start_range":      startRange,
			"end_range":        endRange,
		})
		return nil, err
	}

	c.logger.Debug("時間範圍分析完成", map[string]interface{}{
		"start_idx":        startIdx,
		"end_idx":          endIdx,
		"available_points": endIdx - startIdx + 1,
		"scaled_start":     scaledStartRange,
		"scaled_end":       scaledEndRange,
	})

	results := make([]models.MaxMeanResult, 0, len(dataset.Headers)-1)

	// 對每個通道在指定範圍內計算最大平均值
	for channelIdx := 0; channelIdx < len(dataset.Data[0].Channels); channelIdx++ {
		maxMean := 0.0
		bestStartIdx := startIdx

		// 在指定範圍內滑動窗口計算
		for winStartIdx := startIdx; winStartIdx <= endIdx-windowSize+1; winStartIdx++ {
			values := make([]float64, 0, windowSize)

			for i := winStartIdx; i < winStartIdx+windowSize; i++ {
				if channelIdx < len(dataset.Data[i].Channels) {
					values = append(values, dataset.Data[i].Channels[channelIdx])
				}
			}

			if len(values) == windowSize {
				mean := util.ArrayMean(values)
				if mean > maxMean {
					maxMean = mean
					bestStartIdx = winStartIdx
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

	duration := time.Since(startTime)
	c.logger.Info("指定範圍內最大平均值計算完成", map[string]interface{}{
		"duration_ms":      duration.Milliseconds(),
		"channel_count":    len(results),
		"window_size":      windowSize,
		"start_range":      startRange,
		"end_range":        endRange,
		"processed_points": endIdx - startIdx + 1,
	})

	return results, nil
}

// parseRawData 解析原始字符串數據
func (c *MaxMeanCalculator) parseRawData(records [][]string) (*models.EMGDataset, error) {
	c.logger.Debug("開始解析原始數據", map[string]interface{}{
		"record_count":   len(records),
		"scaling_factor": c.scalingFactor,
	})

	if len(records) < 2 {
		err := fmt.Errorf("數據至少需要包含標題行和一行數據")
		c.logger.Error("原始數據結構驗證失敗", err, map[string]interface{}{
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

	// 解析數據
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue // 跳過無效行
		}

		// 解析時間
		if row[0] == "" {
			c.logger.Debug("跳過空白時間行", map[string]interface{}{
				"row_number": i + 1,
			})
			continue // 跳過空白時間值的行
		}
		
		timeVal, err := util.Str2Number[float64, int](row[0], c.scalingFactor)
		if err != nil {
			c.logger.Warn("時間值解析失敗，跳過此行", map[string]interface{}{
				"row_number": i + 1,
				"time_value": row[0],
				"error":      err.Error(),
			})
			continue // 跳過無法解析的行
		}

		// 解析通道數據
		channels := make([]float64, 0, len(row)-1)
		for j := 1; j < len(row); j++ {
			val, err := util.Str2Number[float64, int](row[j], c.scalingFactor)
			if err != nil {
				c.logger.Error("通道數據解析失敗", err, map[string]interface{}{
					"row_number":    i + 1,
					"column_number": j + 1,
					"value":         row[j],
				})
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

	if len(dataset.Data) == 0 {
		err := fmt.Errorf("解析後數據集為空，所有行都被跳過")
		c.logger.Error("原始數據解析失敗", err, map[string]interface{}{
			"header_count": len(dataset.Headers),
		})
		return nil, err
	}

	c.logger.Info("原始數據解析完成", map[string]interface{}{
		"parsed_records": len(dataset.Data),
		"channel_count":  len(dataset.Data[0].Channels),
		"header_count":   len(dataset.Headers),
	})

	return dataset, nil
}
