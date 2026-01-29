package calculator

import (
	"fmt"
	"time"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/parser"
	"count_mean/util"
)

// Normalizer 處理數據標準化.
type Normalizer struct {
	scalingFactor int
	logger        *logging.Logger
	dataParser    *parser.DataParser
}

// NewNormalizer 創建新的標準化器.
func NewNormalizer(scalingFactor int) *Normalizer {
	logger := logging.GetLogger("normalizer")

	return &Normalizer{
		scalingFactor: scalingFactor,
		logger:        logger,
		dataParser:    parser.NewDataParserWithLogger(scalingFactor, logger),
	}
}

// Normalize 標準化數據集（每個值除以參考值）.
func (n *Normalizer) Normalize(dataset, reference *models.EMGDataset) (*models.EMGDataset, error) {
	if n == nil {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "標準化器為空")
	}

	startTime := time.Now()

	// 使用 IsEmpty() 統一檢查 nil 和空數據
	if dataset.IsEmpty() || reference.IsEmpty() {
		err := calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "數據集或參考數據集為空")
		n.logger.Error("標準化輸入驗證失敗", err, map[string]interface{}{
			"dataset_empty":   dataset.IsEmpty(),
			"reference_empty": reference.IsEmpty(),
		})

		return nil, err
	}

	n.logger.Info("開始數據標準化", map[string]interface{}{
		"dataset_points":   dataset.DataPointCount(),
		"reference_points": reference.DataPointCount(),
		"scaling_factor":   n.scalingFactor,
	})

	// 檢查通道數是否匹配
	if dataset.ChannelCount() != reference.ChannelCount() {
		err := calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrChannelMismatch,
			"數據集和參考數據集的通道數不匹配",
			map[string]interface{}{
				"dataset_channels":   dataset.ChannelCount(),
				"reference_channels": reference.ChannelCount(),
			},
		)
		n.logger.Error("通道數不匹配", err, map[string]interface{}{
			"dataset_channels":   dataset.ChannelCount(),
			"reference_channels": reference.ChannelCount(),
		})

		return nil, err
	}

	result := &models.EMGDataset{
		Headers:               make([]string, len(dataset.Headers)),
		Data:                  make([]models.EMGData, 0, dataset.DataPointCount()),
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
			err := calcerrors.NewCalculatorErrorWithContext(
				calcerrors.ErrZeroReference,
				fmt.Sprintf("參考值在通道 %d 為零，無法進行標準化", i+1),
				map[string]interface{}{
					"channel_index":   i + 1,
					"reference_value": refVal,
				},
			)
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
		"processed_points": result.DataPointCount(),
		"channel_count":    result.ChannelCount(),
	})

	return result, nil
}

// NormalizeFromRawData 從原始字符串數據進行標準化.
func (n *Normalizer) NormalizeFromRawData(records, reference [][]string) (*models.EMGDataset, error) {
	if n == nil {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "標準化器為空")
	}

	n.logger.Info("開始從原始數據進行標準化", map[string]interface{}{
		"main_records":      len(records),
		"reference_records": len(reference),
	})

	dataset, err := n.dataParser.ParseRawDataWithOptions(records, parser.ParseOptions{
		DetectTimePrecision: true,
		LogVerbose:          true,
	})
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

// parseReferenceData 解析參考數據（特殊格式，第一列是標籤而非時間）.
func (n *Normalizer) parseReferenceData(records [][]string) (*models.EMGDataset, error) {
	n.logger.Debug("開始解析參考數據", map[string]interface{}{
		"record_count": len(records),
	})

	if len(records) < 2 {
		err := calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrInvalidReferenceData,
			"參考數據至少需要包含標題行和一行數據",
			map[string]interface{}{
				"record_count": len(records),
			},
		)
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
	if dataset.IsEmpty() {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrInvalidReferenceData, "參考數據解析後為空")
	}

	n.logger.Debug("參考數據解析完成", map[string]interface{}{
		"parsed_records": dataset.DataPointCount(),
		"channel_count":  dataset.ChannelCount(),
		"header_count":   len(dataset.Headers),
	})

	return dataset, nil
}
