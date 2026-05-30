package parsers

import (
	"fmt"
	"io"
	"strings"

	"count_mean/internal/models"
	"count_mean/util"
)

// EMGParser EMG 檔案解析器。Stateless — Parse 不寫 instance state，
// 同一 instance 可在多 goroutine 中安全共用（亦可 per-call 新建）。
type EMGParser struct {
	skipHeader bool
}

// NewEMGParser 創建新的 EMG 解析器。回傳值為 stateless parser，可長期持有或 per-call 新建。
func NewEMGParser() *EMGParser {
	return &EMGParser{skipHeader: true}
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

// Parse 從 io.Reader 解析 EMG CSV 資料。name 僅用於 error context（reader 不知道
// 自己的檔名）。回傳 (data, frequency, error)。
func (p *EMGParser) Parse(r io.Reader, name string) (*models.PhaseSyncEMGData, float64, error) {
	records, err := ReadCSVRecords(r)
	if err != nil {
		return nil, 0, fmt.Errorf("無法讀取 EMG 檔案 %s: %w", name, err)
	}

	return p.parseEMGRecords(records)
}

// parseEMGRecords 由 Parse 呼叫的 EMG record 解析核心。
func (p *EMGParser) parseEMGRecords(records [][]string) (*models.PhaseSyncEMGData, float64, error) {
	// acknowledgement：ReadCSVRecords 走 jagged-row 容忍模式
	// （FieldsPerRecord=-1, LazyQuotes=true），無法靠 csv.Reader 本身擋 formula
	// injection / script / SQL / command injection。理想是進入 EMG 語意層前用
	// ValidateCSVRow 對每筆 cell 過 cell-level injection 守門。
	//
	// 既定限制：目前 validation/patterns.go 對 CommandInjection 用 substring 比對，
	// "invalid_time" 之類的合法 EMG row（含子字串 "id"）會被誤判為 command injection
	// 而拒。`TestEMGParser_Parse/EMG_file_with_invalid_time_values` 即 pin 住「invalid
	// time 應被 skip 而非整檔 reject」的契約。在不犧牲此契約的前提下，EMG layer 仍仰賴
	// 下游 util.Str2Number（嚴格 strconv.ParseFloat）作為 numeric cell 的隱式守門：
	// formula `=cmd|/c calc!A1` 等惡意 cell 解析必失敗、被 skip。
	//
	// streaming 大檔路徑（large_file_handler.executeStreamingLoop）有實裝 cell-level
	// 守門 — 該路徑無「missing-data row tolerated」契約，文件規模也更大、injection 風險
	// 更高。EMG phase-sync / muscle ratio path 因 false-positive 包袱待 patterns
	// substring-match 收緊（後續 Wave）後再加上 ValidateCSVRow。

	headers, err := p.validateEMGRecords(records)
	if err != nil {
		return nil, 0, err
	}

	emgData := initEMGData(headers, len(records)-1)

	for i := 1; i < len(records); i++ {
		parseEMGDataRow(records[i], headers, emgData)
	}

	if err := validateEMGDataIntegrity(emgData); err != nil {
		return nil, 0, err
	}

	frequency, freqErr := computeFrequencyFromTime(emgData.Time)
	if freqErr != nil {
		frequency = 0
	}

	return emgData, frequency, nil
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

// GetEMGDataInTimeRange returns EMG data within the specified time range.
// Uses integer milliseconds for comparison to avoid floating point precision issues.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func GetEMGDataInTimeRange(
	data *models.PhaseSyncEMGData, startTime, endTime float64,
) (*EMGTimeRangeResult, error) {
	// validator path 已有此 guard，extractor path 需對稱保護避免 data.Time
	// 索引存取造成 nil-deref panic。空 Time slice 也視為空資料一併 reject。
	if data == nil || len(data.Time) == 0 {
		return nil, fmt.Errorf("EMG 數據為空: %w", ErrNilData)
	}

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
