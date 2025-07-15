package parsers

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"

	"count_mean/internal/models"
)

// MotionParser Motion檔案解析器
type MotionParser struct {
	frequency float64 // 採樣頻率 Hz
	headerRow int     // 標題所在行（從0開始）
	dataRow   int     // 數據開始行（從0開始）
}

// NewMotionParser 創建新的 Motion 解析器
func NewMotionParser() *MotionParser {
	return &MotionParser{
		frequency: 250.0, // 250Hz
		headerRow: 3,     // 第4行是標題
		dataRow:   4,     // 第5行開始是數據
	}
}

// ParseFile 解析 Motion CSV 檔案
func (p *MotionParser) ParseFile(filepath string) (*models.MotionData, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 Motion 檔案 %s: %w", filepath, err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // 允許不同行有不同的欄位數

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("讀取 Motion CSV 失敗: %w", err)
	}

	if len(records) <= p.dataRow {
		return nil, fmt.Errorf("Motion 檔案格式錯誤：數據行不足")
	}

	// 解析標題行
	if p.headerRow >= len(records) {
		return nil, fmt.Errorf("Motion 檔案格式錯誤：找不到標題行")
	}

	headers := p.parseHeaders(records[p.headerRow])
	if len(headers) < 2 { // 至少需要 index 列和一個數據列
		return nil, fmt.Errorf("Motion 檔案標題不足：至少需要 index 列和一個數據列")
	}

	// 初始化數據結構
	motionData := &models.MotionData{
		Indices: make([]int, 0, len(records)-p.dataRow),
		Data:    make(map[string][]float64),
		Headers: headers[1:], // 排除 index 列
	}

	// 為每個數據列初始化切片
	for _, header := range motionData.Headers {
		motionData.Data[header] = make([]float64, 0, len(records)-p.dataRow)
	}

	// 解析數據行
	for i := p.dataRow; i < len(records); i++ {
		record := records[i]
		if len(record) == 0 || strings.TrimSpace(record[0]) == "" {
			// 跳過空行
			continue
		}

		if len(record) < len(headers) {
			// 跳過不完整的行
			continue
		}

		// 解析 index
		indexValue, err := strconv.Atoi(strings.TrimSpace(record[0]))
		if err != nil {
			// 跳過無效的 index 值
			continue
		}
		motionData.Indices = append(motionData.Indices, indexValue)

		// 解析各列數據
		for j := 1; j < len(headers) && j < len(record); j++ {
			columnName := headers[j]
			value, err := strconv.ParseFloat(strings.TrimSpace(record[j]), 64)
			if err != nil {
				// 無效值設為 0
				value = 0
			}
			motionData.Data[columnName] = append(motionData.Data[columnName], value)
		}
	}

	// 驗證數據完整性
	dataLen := len(motionData.Indices)
	if dataLen == 0 {
		return nil, fmt.Errorf("Motion 檔案沒有有效數據")
	}

	for columnName, columnData := range motionData.Data {
		if len(columnData) != dataLen {
			return nil, fmt.Errorf("列 %s 的數據長度不一致", columnName)
		}
	}

	return motionData, nil
}

// parseHeaders 解析標題行
func (p *MotionParser) parseHeaders(headerRow []string) []string {
	headers := make([]string, 0, len(headerRow))
	for _, h := range headerRow {
		trimmed := strings.TrimSpace(h)
		if trimmed != "" {
			headers = append(headers, trimmed)
		}
	}
	return headers
}

// GetSampleInterval 獲取採樣間隔（秒）
func (p *MotionParser) GetSampleInterval() float64 {
	return 1.0 / p.frequency
}

// IndexToTime 將 Motion index 轉換為時間（秒）
func (p *MotionParser) IndexToTime(index int) float64 {
	// Motion index 從 1 開始，時間從 0 開始
	return float64(index-1) * p.GetSampleInterval()
}

// TimeToIndex 將時間（秒）轉換為最接近的 Motion index
func (p *MotionParser) TimeToIndex(time float64) int {
	// 四捨五入到最接近的 index
	index := int(time/p.GetSampleInterval()+0.5) + 1
	if index < 1 {
		index = 1
	}
	return index
}

// GetDataAtIndex 獲取指定 index 的數據
func (p *MotionParser) GetDataAtIndex(data *models.MotionData, targetIndex int) (map[string]float64, error) {
	// 查找 index
	idx := -1
	for i, index := range data.Indices {
		if index == targetIndex {
			idx = i
			break
		}
	}

	if idx == -1 {
		return nil, fmt.Errorf("找不到 index %d 的數據", targetIndex)
	}

	// 提取該 index 的所有數據
	result := make(map[string]float64)
	for columnName, columnData := range data.Data {
		result[columnName] = columnData[idx]
	}

	return result, nil
}

// GetDataInIndexRange 獲取指定 index 範圍內的數據
func (p *MotionParser) GetDataInIndexRange(data *models.MotionData, startIndex, endIndex int) (*models.MotionData, error) {
	if startIndex > endIndex {
		return nil, fmt.Errorf("開始 index %d 不能大於結束 index %d", startIndex, endIndex)
	}

	// 找到 index 範圍的位置
	startPos := -1
	endPos := -1

	for i, idx := range data.Indices {
		if startPos == -1 && idx >= startIndex {
			startPos = i
		}
		if idx <= endIndex {
			endPos = i
		} else if endPos != -1 {
			break
		}
	}

	if startPos == -1 || endPos == -1 || startPos > endPos {
		return nil, fmt.Errorf("找不到有效的 index 範圍數據")
	}

	// 創建子集數據
	rangeData := &models.MotionData{
		Indices: data.Indices[startPos : endPos+1],
		Data:    make(map[string][]float64),
		Headers: data.Headers,
	}

	// 複製數據
	for columnName, columnData := range data.Data {
		rangeData.Data[columnName] = columnData[startPos : endPos+1]
	}

	return rangeData, nil
}

// ValidateMotionData 驗證 Motion 數據
func ValidateMotionData(data *models.MotionData) error {
	if data == nil {
		return fmt.Errorf("Motion 數據為空")
	}

	if len(data.Indices) == 0 {
		return fmt.Errorf("Motion index 序列為空")
	}

	if len(data.Data) == 0 {
		return fmt.Errorf("Motion 沒有任何數據列")
	}

	// 檢查 index 序列是否遞增
	for i := 1; i < len(data.Indices); i++ {
		if data.Indices[i] <= data.Indices[i-1] {
			return fmt.Errorf("Motion index 在位置 %d 處不是遞增的", i)
		}
	}

	// 檢查所有數據列長度一致
	expectedLen := len(data.Indices)
	for columnName, columnData := range data.Data {
		if len(columnData) != expectedLen {
			return fmt.Errorf("列 %s 的數據長度 (%d) 與 index 序列長度 (%d) 不符",
				columnName, len(columnData), expectedLen)
		}
	}

	return nil
}
