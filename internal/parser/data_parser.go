package parser

import (
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/util"
	"fmt"
)

// DataParser 處理原始字符串數據的解析，統一提供給 MaxMeanCalculator、Normalizer 和 PhaseAnalyzer 使用
type DataParser struct {
	scalingFactor int
	logger        *logging.Logger
}

// NewDataParser 創建新的數據解析器
func NewDataParser(scalingFactor int) *DataParser {
	return &DataParser{
		scalingFactor: scalingFactor,
		logger:        logging.GetLogger("data_parser"),
	}
}

// NewDataParserWithLogger 創建帶有自定義 logger 的數據解析器
// 如果 logger 為 nil，則使用默認 logger
func NewDataParserWithLogger(scalingFactor int, logger *logging.Logger) *DataParser {
	if logger == nil {
		logger = logging.GetLogger("data_parser")
	}
	return &DataParser{
		scalingFactor: scalingFactor,
		logger:        logger,
	}
}

// ParseRawData 解析原始字符串數據為 EMGDataset
func (p *DataParser) ParseRawData(records [][]string) (*models.EMGDataset, error) {
	if records == nil {
		return nil, fmt.Errorf("輸入數據不能為 nil")
	}
	return p.ParseRawDataWithOptions(records, ParseOptions{
		DetectTimePrecision: false,
		LogVerbose:          true,
	})
}

// ParseOptions 解析選項
type ParseOptions struct {
	// DetectTimePrecision 是否檢測時間精度並設置到 OriginalTimePrecision
	DetectTimePrecision bool
	// LogVerbose 是否輸出詳細日誌
	LogVerbose bool
}

// ParseRawDataWithOptions 使用指定選項解析原始字符串數據
func (p *DataParser) ParseRawDataWithOptions(records [][]string, opts ParseOptions) (*models.EMGDataset, error) {
	if records == nil {
		return nil, fmt.Errorf("輸入數據不能為 nil")
	}
	if opts.LogVerbose {
		p.logger.Debug("開始解析原始數據", map[string]interface{}{
			"record_count":   len(records),
			"scaling_factor": p.scalingFactor,
		})
	}

	if len(records) < 2 {
		err := fmt.Errorf("數據至少需要包含標題行和一行數據")
		p.logger.Error("原始數據結構驗證失敗", err, map[string]interface{}{
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

	// 可選：檢測時間精度
	if opts.DetectTimePrecision {
		dataset.OriginalTimePrecision = util.DetectTimePrecision(records)
		p.logger.Debug("檢測到時間精度", map[string]interface{}{
			"detected_precision": dataset.OriginalTimePrecision,
		})
	}

	// 解析數據
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue // 跳過無效行
		}

		// 解析時間
		if row[0] == "" {
			if opts.LogVerbose {
				p.logger.Debug("跳過空白時間行", map[string]interface{}{
					"row_number": i + 1,
				})
			}
			continue // 跳過空白時間值的行
		}

		timeVal, err := util.Str2Number[float64, int](row[0], p.scalingFactor)
		if err != nil {
			if opts.LogVerbose {
				p.logger.Warn("時間值解析失敗，跳過此行", map[string]interface{}{
					"row_number": i + 1,
					"time_value": row[0],
					"error":      err.Error(),
				})
			}
			continue // 跳過無法解析的行
		}

		// 解析通道數據
		channels := make([]float64, 0, len(row)-1)
		for j := 1; j < len(row); j++ {
			val, err := util.Str2Number[float64, int](row[j], p.scalingFactor)
			if err != nil {
				p.logger.Error("通道數據解析失敗", err, map[string]interface{}{
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
		p.logger.Error("原始數據解析失敗", err, map[string]interface{}{
			"header_count": len(dataset.Headers),
		})
		return nil, err
	}

	if opts.LogVerbose {
		p.logger.Info("原始數據解析完成", map[string]interface{}{
			"parsed_records": len(dataset.Data),
			"channel_count":  len(dataset.Data[0].Channels),
			"header_count":   len(dataset.Headers),
		})
	}

	return dataset, nil
}

// GetScalingFactor 獲取縮放因子
func (p *DataParser) GetScalingFactor() int {
	return p.scalingFactor
}
