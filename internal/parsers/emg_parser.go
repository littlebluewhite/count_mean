package parsers

import (
	"fmt"
	"strconv"
	"strings"

	"count_mean/internal/models"
)

// EMGParser EMG檔案解析器
type EMGParser struct {
	skipHeader bool
	frequency  float64 // 採樣頻率 Hz
}

// NewEMGParser 創建新的 EMG 解析器
func NewEMGParser() *EMGParser {
	return &EMGParser{
		skipHeader: true,
		frequency:  1000.0, // 1000Hz
	}
}

// ParseFile 解析 EMG CSV 檔案
func (p *EMGParser) ParseFile(filepath string) (*models.PhaseSyncEMGData, error) {
	// 使用直接讀取，不進行路徑驗證
	records, err := ReadCSVDirect(filepath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 EMG 檔案 %s: %w", filepath, err)
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("EMG 檔案為空")
	}

	// 解析標題行
	if !p.skipHeader || len(records) < 2 {
		return nil, fmt.Errorf("EMG 檔案格式錯誤：需要標題行和數據")
	}

	headers := p.parseHeaders(records[0])
	if len(headers) < 2 { // 至少需要時間列和一個數據列
		return nil, fmt.Errorf("EMG 檔案標題不足：至少需要時間列和一個數據列")
	}

	// 初始化數據結構
	emgData := &models.PhaseSyncEMGData{
		Time:     make([]float64, 0, len(records)-1),
		Channels: make(map[string][]float64),
		Headers:  headers[1:], // 排除時間列
	}

	// 為每個通道初始化切片
	for _, header := range emgData.Headers {
		emgData.Channels[header] = make([]float64, 0, len(records)-1)
	}

	// 解析數據行
	for i := 1; i < len(records); i++ {
		record := records[i]
		if len(record) < len(headers) {
			// 跳過不完整的行
			continue
		}

		// 解析時間
		timeValue, err := strconv.ParseFloat(strings.TrimSpace(record[0]), 64)
		if err != nil {
			// 跳過無效的時間值
			continue
		}
		emgData.Time = append(emgData.Time, timeValue)

		// 解析各通道數據
		for j := 1; j < len(headers) && j < len(record); j++ {
			channelName := headers[j]
			value, err := strconv.ParseFloat(strings.TrimSpace(record[j]), 64)
			if err != nil {
				// 無效值設為 0
				value = 0
			}
			emgData.Channels[channelName] = append(emgData.Channels[channelName], value)
		}
	}

	// 驗證數據完整性
	dataLen := len(emgData.Time)
	for channelName, channelData := range emgData.Channels {
		if len(channelData) != dataLen {
			return nil, fmt.Errorf("通道 %s 的數據長度不一致", channelName)
		}
	}

	return emgData, nil
}

// parseHeaders 解析標題行
func (p *EMGParser) parseHeaders(headerRow []string) []string {
	headers := make([]string, 0, len(headerRow))
	for _, h := range headerRow {
		trimmed := strings.TrimSpace(h)
		if trimmed != "" {
			headers = append(headers, trimmed)
		}
	}
	return headers
}

// GetDataInTimeRange 獲取指定時間範圍內的數據
func (p *EMGParser) GetDataInTimeRange(data *models.PhaseSyncEMGData, startTime, endTime float64) (*models.PhaseSyncEMGData, error) {
	if startTime > endTime {
		return nil, fmt.Errorf("開始時間 %.3f 不能大於結束時間 %.3f", startTime, endTime)
	}

	// 找到時間範圍的索引
	startIdx := -1
	endIdx := -1

	for i, t := range data.Time {
		if startIdx == -1 && t >= startTime {
			startIdx = i
		}
		if t <= endTime {
			endIdx = i
		} else if endIdx != -1 {
			break
		}
	}

	if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
		return nil, fmt.Errorf("找不到有效的時間範圍數據")
	}

	// 創建子集數據
	rangeData := &models.PhaseSyncEMGData{
		Time:     data.Time[startIdx : endIdx+1],
		Channels: make(map[string][]float64),
		Headers:  data.Headers,
	}

	// 複製通道數據
	for channelName, channelData := range data.Channels {
		rangeData.Channels[channelName] = channelData[startIdx : endIdx+1]
	}

	return rangeData, nil
}

// CalculateStatistics 計算統計數據
func CalculateEMGStatistics(data *models.PhaseSyncEMGData) (means map[string]float64, maxes map[string]float64) {
	means = make(map[string]float64)
	maxes = make(map[string]float64)

	for channelName, channelData := range data.Channels {
		if len(channelData) == 0 {
			means[channelName] = 0
			maxes[channelName] = 0
			continue
		}

		// 計算平均值
		sum := 0.0
		max := channelData[0]

		for _, value := range channelData {
			sum += value
			if value > max {
				max = value
			}
		}

		means[channelName] = sum / float64(len(channelData))
		maxes[channelName] = max
	}

	return means, maxes
}

// GetSampleInterval 獲取採樣間隔（秒）
func (p *EMGParser) GetSampleInterval() float64 {
	return 1.0 / p.frequency
}

// ValidateEMGData 驗證 EMG 數據
func ValidateEMGData(data *models.PhaseSyncEMGData) error {
	if data == nil {
		return fmt.Errorf("EMG 數據為空")
	}

	if len(data.Time) == 0 {
		return fmt.Errorf("EMG 時間序列為空")
	}

	if len(data.Channels) == 0 {
		return fmt.Errorf("EMG 沒有任何通道數據")
	}

	// 檢查時間序列是否遞增
	for i := 1; i < len(data.Time); i++ {
		if data.Time[i] <= data.Time[i-1] {
			return fmt.Errorf("EMG 時間序列在索引 %d 處不是遞增的", i)
		}
	}

	// 檢查所有通道數據長度一致
	expectedLen := len(data.Time)
	for channelName, channelData := range data.Channels {
		if len(channelData) != expectedLen {
			return fmt.Errorf("通道 %s 的數據長度 (%d) 與時間序列長度 (%d) 不符",
				channelName, len(channelData), expectedLen)
		}
	}

	return nil
}
