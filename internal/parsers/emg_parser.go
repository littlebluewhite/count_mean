package parsers

import (
	"fmt"
	"strings"

	"count_mean/internal/models"
	"count_mean/util"
)

// EMGParser EMG檔案解析器.
type EMGParser struct {
	skipHeader bool
	frequency  float64 // 採樣頻率 Hz
}

// NewEMGParser 創建新的 EMG 解析器.
func NewEMGParser() *EMGParser {
	return &EMGParser{
		skipHeader: true,
		frequency:  0,
	}
}

// validateEMGRecords validates basic requirements for EMG records.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func (p *EMGParser) validateEMGRecords(records [][]string) ([]string, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("EMG 檔案為空")
	}

	if !p.skipHeader || len(records) < 2 {
		return nil, fmt.Errorf("EMG 檔案格式錯誤：需要標題行和數據")
	}

	headers := p.parseHeaders(records[0])
	if len(headers) < 2 {
		return nil, fmt.Errorf("EMG 檔案標題不足：至少需要時間列和一個數據列")
	}

	return headers, nil
}

// initEMGData initializes EMGData structure with channel slices.
func initEMGData(headers []string, capacity int) *models.PhaseSyncEMGData {
	emgData := &models.PhaseSyncEMGData{
		Time:     make([]float64, 0, capacity),
		Channels: make(map[string][]float64),
		Headers:  headers[1:],
	}

	for _, header := range emgData.Headers {
		emgData.Channels[header] = make([]float64, 0, capacity)
	}

	return emgData
}

// parseEMGDataRow parses a single EMG data row.
// Returns false if the row should be skipped.
func parseEMGDataRow(record, headers []string, emgData *models.PhaseSyncEMGData) bool {
	if len(record) < len(headers) {
		return false
	}

	timeValue, ok := ParseFloatCell(record[0])
	if !ok {
		return false
	}

	emgData.Time = append(emgData.Time, timeValue)

	for j := 1; j < len(headers) && j < len(record); j++ {
		value, _ := ParseFloatCell(record[j])
		emgData.Channels[headers[j]] = append(emgData.Channels[headers[j]], value)
	}

	return true
}

// validateEMGDataIntegrity validates EMG data channel consistency.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func validateEMGDataIntegrity(emgData *models.PhaseSyncEMGData) error {
	dataLen := len(emgData.Time)
	for channelName, channelData := range emgData.Channels {
		if len(channelData) != dataLen {
			return fmt.Errorf("通道 %s 的數據長度不一致", channelName)
		}
	}

	return nil
}

// ParseFile 解析 EMG CSV 檔案.
func (p *EMGParser) ParseFile(filepath string) (*models.PhaseSyncEMGData, error) {
	records, err := ReadCSVDirect(filepath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 EMG 檔案 %s: %w", filepath, err)
	}

	headers, err := p.validateEMGRecords(records)
	if err != nil {
		return nil, err
	}

	emgData := initEMGData(headers, len(records)-1)

	for i := 1; i < len(records); i++ {
		parseEMGDataRow(records[i], headers, emgData)
	}

	if err := validateEMGDataIntegrity(emgData); err != nil {
		return nil, err
	}

	if freq, freqErr := computeFrequencyFromTime(emgData.Time); freqErr == nil {
		p.frequency = freq
	}

	return emgData, nil
}

// parseHeaders 解析標題行.
func (p *EMGParser) parseHeaders(headerRow []string) []string { //nolint:revive // keep consistent API
	headers := make([]string, 0, len(headerRow))

	for _, h := range headerRow {
		trimmed := strings.TrimSpace(h)
		if trimmed != "" {
			headers = append(headers, trimmed)
		}
	}

	return headers
}

// EMGTimeRangeResult 時間範圍提取結果，包含實際選取的時間範圍.
type EMGTimeRangeResult struct {
	Data            *models.PhaseSyncEMGData
	ActualStartTime float64 // 實際選取的第一個數據點時間
	ActualEndTime   float64 // 實際選取的最後一個數據點時間
}

// GetDataInTimeRange returns EMG data within the specified time range.
// Uses integer milliseconds for comparison to avoid floating point precision issues.
//
//nolint:revive,err113 // unused-receiver: keep consistent API; dynamic errors with Chinese messages
func (p *EMGParser) GetDataInTimeRange(
	data *models.PhaseSyncEMGData, startTime, endTime float64,
) (*EMGTimeRangeResult, error) {
	if startTime > endTime {
		return nil, fmt.Errorf("開始時間 %.3f 不能大於結束時間 %.3f", startTime, endTime)
	}

	startIdx, endIdx, err := FindTimeRangeIndices(data.Time, startTime, endTime)
	if err != nil {
		return nil, err
	}

	rangeData := &models.PhaseSyncEMGData{
		Time:     data.Time[startIdx : endIdx+1],
		Channels: make(map[string][]float64),
		Headers:  data.Headers,
	}

	for channelName, channelData := range data.Channels {
		rangeData.Channels[channelName] = channelData[startIdx : endIdx+1]
	}

	return &EMGTimeRangeResult{
		Data:            rangeData,
		ActualStartTime: data.Time[startIdx],
		ActualEndTime:   data.Time[endIdx],
	}, nil
}

// CalculateEMGStatistics 計算統計數據.
//
//nolint:nonamedreturns // named returns improve readability for multiple return values
func CalculateEMGStatistics(data *models.PhaseSyncEMGData) (means, maxes map[string]float64) {
	means = make(map[string]float64)
	maxes = make(map[string]float64)

	for channelName, channelData := range data.Channels {
		if len(channelData) == 0 {
			means[channelName] = 0
			maxes[channelName] = 0

			continue
		}

		means[channelName] = util.ArrayMean(channelData)
		maxVal, _ := util.ArrayMax(channelData)
		maxes[channelName] = maxVal
	}

	return means, maxes
}

// GetSampleInterval 獲取採樣間隔（秒）.
func (p *EMGParser) GetSampleInterval() float64 {
	if p.frequency == 0 {
		return 0
	}

	return 1.0 / p.frequency
}

// ValidateEMGData 驗證 EMG 數據.
//
//nolint:err113 // dynamic error message with Chinese for user-facing output
func ValidateEMGData(data *models.PhaseSyncEMGData) error {
	if data == nil {
		return fmt.Errorf("EMG 數據為空: %w", ErrNilData)
	}

	return ValidateTimeSeries(data.Time, data.Channels, TimeSeriesLabels{
		DataName:     "EMG",
		SeriesName:   "時間序列",
		SeriesPos:    "索引",
		ChannelLabel: "通道",
	})
}
