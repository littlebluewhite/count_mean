package parsers

import (
	"errors"
	"fmt"

	apperrors "count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/util"
)

// DataParser 私有錯誤。
//
// ErrInsufficientData 定義於 errors.go（alias 到 apperrors.ErrInsufficientData）;
// validateRecords 用 inline wrap 提供中文訊息（maxmean_test 斷言該子字串），
// 同時不破壞 errors.Is 跨套件對稱性。
var (
	ErrNilInput     = errors.New("輸入數據不能為 nil")
	ErrEmptyDataset = errors.New("解析後數據集為空，所有行都被跳過")
	ErrSkipRow      = errors.New("skip row")
)

// DataParser 處理原始字符串數據的解析，統一提供給 MaxMeanCalculator、Normalizer 和 PhaseAnalyzer 使用.
type DataParser struct {
	scalingFactor int
	logger        *logging.Logger
}

// NewDataParser 創建新的數據解析器.
func NewDataParser(scalingFactor int) *DataParser {
	return &DataParser{
		scalingFactor: scalingFactor,
		logger:        logging.GetLogger("data_parser"),
	}
}

// NewDataParserWithLogger 創建帶自訂 logger 的數據解析器.
// 如果 logger 為 nil，則使用默認 logger.
func NewDataParserWithLogger(scalingFactor int, logger *logging.Logger) *DataParser {
	if logger == nil {
		logger = logging.GetLogger("data_parser")
	}

	return &DataParser{
		scalingFactor: scalingFactor,
		logger:        logger,
	}
}

// ParseRawData 解析原始字符串數據為 EMGDataset.
func (p *DataParser) ParseRawData(records [][]string) (*models.EMGDataset, error) {
	if records == nil {
		return nil, ErrNilInput
	}

	return p.ParseRawDataWithOptions(records, ParseOptions{
		DetectTimePrecision: false,
		LogVerbose:          true,
	})
}

// ParseOptions 解析選項.
type ParseOptions struct {
	// DetectTimePrecision 是否檢測時間精度並設置到 OriginalTimePrecision
	DetectTimePrecision bool
	// LogVerbose 是否輸出詳細日誌
	LogVerbose bool
}

// validateRecords 驗證輸入記錄.
func (p *DataParser) validateRecords(records [][]string) error {
	if records == nil {
		return ErrNilInput
	}

	if len(records) < 2 {
		err := fmt.Errorf("數據至少需要包含標題行和一行數據: %w", apperrors.ErrInsufficientData)
		p.logger.Error("原始數據結構驗證失敗", err, map[string]any{"record_count": len(records)})

		return err
	}

	return nil
}

// initializeDataset 初始化數據集.
func (p *DataParser) initializeDataset(records [][]string, opts ParseOptions) *models.EMGDataset {
	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}

	copy(dataset.Headers, records[0])

	if opts.DetectTimePrecision {
		dataset.OriginalTimePrecision = util.DetectTimePrecision(records)
		p.logger.Debug("檢測到時間精度", map[string]any{"detected_precision": dataset.OriginalTimePrecision})
	}

	return dataset
}

// parseChannels 解析通道數據.
func (p *DataParser) parseChannels(row []string, rowNumber int) ([]float64, error) {
	channels := make([]float64, 0, len(row)-1)

	for j := 1; j < len(row); j++ {
		val, err := util.Str2Number[float64, int](row[j], p.scalingFactor)
		if err != nil {
			p.logger.Error("通道數據解析失敗", err, map[string]any{
				"row_number": rowNumber, "column_number": j + 1, "value": row[j],
			})

			return nil, fmt.Errorf("解析數據失敗在第 %d 行第 %d 列: %w", rowNumber, j+1, err)
		}

		channels = append(channels, val)
	}

	return channels, nil
}

// parseDataRow 解析單一數據行，返回 EMGData.
// 如果應該跳過該行，返回 ErrSkipRow 錯誤.
func (p *DataParser) parseDataRow(row []string, rowNumber int, opts ParseOptions) (*models.EMGData, error) {
	// 先排除空 row 再讀 row[0]，避免在非 encoding/csv 來源傳入 []string{} 時 panic。
	if len(row) == 0 {
		return nil, ErrSkipRow
	}

	// 經過上面 guard，len(row) 至少是 1。只剩兩條 skip 條件：單欄列（無 channel 值）或空白時間。
	if len(row) == 1 || row[0] == "" {
		if opts.LogVerbose && row[0] == "" {
			p.logger.Debug("跳過空白時間行", map[string]any{"row_number": rowNumber})
		}

		return nil, ErrSkipRow
	}

	timeVal, err := util.Str2Number[float64, int](row[0], p.scalingFactor)
	if err != nil {
		if opts.LogVerbose {
			p.logger.Warn("時間值解析失敗，跳過此行", map[string]any{
				"row_number": rowNumber, "time_value": row[0], "error": err.Error(),
			})
		}

		return nil, ErrSkipRow
	}

	channels, err := p.parseChannels(row, rowNumber)
	if err != nil {
		return nil, err
	}

	return &models.EMGData{Time: timeVal, Channels: channels}, nil
}

// ParseRawDataWithOptions 使用指定選項解析原始字符串數據.
func (p *DataParser) ParseRawDataWithOptions(records [][]string, opts ParseOptions) (*models.EMGDataset, error) {
	if err := p.validateRecords(records); err != nil {
		return nil, err
	}

	if opts.LogVerbose {
		p.logger.Debug("開始解析原始數據", map[string]any{
			"record_count": len(records), "scaling_factor": p.scalingFactor,
		})
	}

	dataset := p.initializeDataset(records, opts)

	for i := 1; i < len(records); i++ {
		data, err := p.parseDataRow(records[i], i+1, opts)
		if err != nil {
			if errors.Is(err, ErrSkipRow) {
				continue
			}

			return nil, err
		}

		dataset.Data = append(dataset.Data, *data)
	}

	if len(dataset.Data) == 0 {
		p.logger.Error("原始數據解析失敗", ErrEmptyDataset, map[string]any{"header_count": len(dataset.Headers)})

		return nil, ErrEmptyDataset
	}

	if opts.LogVerbose {
		p.logger.Info("原始數據解析完成", map[string]any{
			"parsed_records": len(dataset.Data),
			"channel_count":  len(dataset.Data[0].Channels),
			"header_count":   len(dataset.Headers),
		})
	}

	return dataset, nil
}

// GetScalingFactor 獲取縮放因子.
func (p *DataParser) GetScalingFactor() int {
	return p.scalingFactor
}
