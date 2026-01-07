package parsers

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"count_mean/internal/models"

	"github.com/xuri/excelize/v2"
)

// ANCParser ANC力板檔案解析器
type ANCParser struct {
	frequency float64 // 採樣頻率 Hz
}

// NewANCParser 創建新的 ANC 解析器
func NewANCParser() *ANCParser {
	return &ANCParser{
		frequency: 1000.0, // 1000Hz
	}
}

// ANCHeader ANC檔案頭信息
type ANCHeader struct {
	FileType      string
	BoardType     string
	TrialName     string
	TrialNumber   int
	Duration      float64
	NumChannels   int
	BitDepth      int
	PreciseRate   float64
	ChannelNames  []string
	ChannelRates  []int
	ChannelRanges []int
}

// ParseFile 解析 ANC 檔案（支援 .anc 文字格式和 .xlsx Excel 格式）
func (p *ANCParser) ParseFile(filePath string) (*models.ForceData, error) {
	// 根據副檔名選擇解析方式
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".xlsx":
		return p.parseXLSXFile(filePath)
	case ".anc":
		return p.parseANCTextFile(filePath)
	default:
		// 對於複合副檔名如 .anc.xlsx，檢查是否以 .xlsx 結尾
		if strings.HasSuffix(strings.ToLower(filePath), ".xlsx") {
			return p.parseXLSXFile(filePath)
		}
		// 默認使用文字解析（向後兼容）
		return p.parseANCTextFile(filePath)
	}
}

// parseANCTextFile 解析文字格式的 ANC 檔案
func (p *ANCParser) parseANCTextFile(filePath string) (*models.ForceData, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 ANC 檔案 %s: %w", filePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 增加緩衝區大小處理長行

	// 解析頭部信息
	header, err := p.parseHeader(scanner)
	if err != nil {
		return nil, fmt.Errorf("解析 ANC 頭部失敗: %w", err)
	}

	// 初始化數據結構
	forceData := &models.ForceData{
		Time:    make([]float64, 0, int(header.Duration*header.PreciseRate)),
		Forces:  make(map[string][]float64),
		Headers: header.ChannelNames,
	}

	// 為每個通道初始化切片
	for _, channelName := range header.ChannelNames {
		forceData.Forces[channelName] = make([]float64, 0, int(header.Duration*header.PreciseRate))
	}

	// 解析數據行
	lineNum := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		// 跳過空行
		if strings.TrimSpace(line) == "" {
			continue
		}

		// 解析數據行
		fields := strings.Fields(line)
		if len(fields) < len(header.ChannelNames)+1 { // +1 for time column
			continue
		}

		// 解析時間
		timeValue, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		forceData.Time = append(forceData.Time, timeValue)

		// 解析各通道數據
		for i, channelName := range header.ChannelNames {
			if i+1 < len(fields) {
				value, err := strconv.ParseFloat(fields[i+1], 64)
				if err != nil {
					value = 0
				}
				forceData.Forces[channelName] = append(forceData.Forces[channelName], value)
			} else {
				forceData.Forces[channelName] = append(forceData.Forces[channelName], 0)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("讀取 ANC 檔案時發生錯誤: %w", err)
	}

	// 驗證數據完整性
	dataLen := len(forceData.Time)
	for channelName, channelData := range forceData.Forces {
		if len(channelData) != dataLen {
			return nil, fmt.Errorf("通道 %s 的數據長度不一致", channelName)
		}
	}

	return forceData, nil
}

// parseXLSXFile 解析 Excel xlsx 格式的 ANC 檔案
// 支援兩種格式：
// 1. 純數據表格式（第一行是標題，後續是數據）
// 2. ANC 格式的 xlsx（有 11 行頭部資訊，第 9 行是通道名稱，第 12 行開始是數據）
func (p *ANCParser) parseXLSXFile(filePath string) (*models.ForceData, error) {
	// 開啟 Excel 檔案
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 Excel 檔案 %s: %w", filePath, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			// 忽略關閉錯誤
		}
	}()

	// 獲取第一個工作表名稱
	sheetName := f.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("Excel 檔案沒有工作表: %s", filePath)
	}

	// 讀取所有行
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("讀取 Excel 工作表失敗: %w", err)
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("Excel 檔案數據不足（需要至少標題行和一行數據）: %s", filePath)
	}

	// 檢測檔案格式：ANC 格式的第一行通常以 "File_Type:" 開頭
	isANCFormat := false
	if len(rows) > 0 && len(rows[0]) > 0 {
		firstCell := strings.TrimSpace(rows[0][0])
		isANCFormat = strings.HasPrefix(firstCell, "File_Type:")
	}

	if isANCFormat {
		return p.parseANCFormatXLSX(rows, filePath)
	}
	return p.parseSimpleXLSX(rows, filePath)
}

// parseANCFormatXLSX 解析 ANC 格式的 xlsx 檔案（有頭部資訊）
func (p *ANCParser) parseANCFormatXLSX(rows [][]string, filePath string) (*models.ForceData, error) {
	// ANC 格式結構：
	// Row 1-8: 頭部資訊
	// Row 9: 通道名稱（Name, F1X, F1Y, ...）
	// Row 10: 採樣率
	// Row 11: 範圍
	// Row 12+: 數據（時間, 數值...）

	if len(rows) < 12 {
		return nil, fmt.Errorf("ANC xlsx 檔案格式不完整（需要至少 12 行）: %s", filePath)
	}

	// 解析第 9 行的通道名稱（索引 8）
	nameRow := rows[8]
	if len(nameRow) < 2 {
		return nil, fmt.Errorf("ANC xlsx 通道名稱行格式錯誤: %s", filePath)
	}

	// 第一欄應該是 "Name"，後續是通道名稱
	channelNames := make([]string, 0, len(nameRow)-1)
	for i := 1; i < len(nameRow); i++ {
		name := strings.TrimSpace(nameRow[i])
		if name != "" {
			channelNames = append(channelNames, name)
		}
	}

	if len(channelNames) == 0 {
		return nil, fmt.Errorf("ANC xlsx 檔案沒有找到有效的通道名稱: %s", filePath)
	}

	// 初始化數據結構
	forceData := &models.ForceData{
		Time:    make([]float64, 0, len(rows)-11),
		Forces:  make(map[string][]float64),
		Headers: channelNames,
	}

	// 為每個通道初始化切片
	for _, channelName := range channelNames {
		forceData.Forces[channelName] = make([]float64, 0, len(rows)-11)
	}

	// 從第 12 行開始解析數據（索引 11）
	for rowIdx := 11; rowIdx < len(rows); rowIdx++ {
		row := rows[rowIdx]

		// 跳過空行
		if len(row) == 0 {
			continue
		}

		// 解析時間（第一欄）
		timeStr := strings.TrimSpace(row[0])
		if timeStr == "" {
			continue
		}

		timeValue, err := strconv.ParseFloat(timeStr, 64)
		if err != nil {
			continue // 跳過無效的時間值
		}
		forceData.Time = append(forceData.Time, timeValue)

		// 解析各通道數據
		for i, channelName := range channelNames {
			colIdx := i + 1
			var value float64 = 0
			if colIdx < len(row) {
				valueStr := strings.TrimSpace(row[colIdx])
				if valueStr != "" {
					parsedValue, err := strconv.ParseFloat(valueStr, 64)
					if err == nil {
						value = parsedValue
					}
				}
			}
			forceData.Forces[channelName] = append(forceData.Forces[channelName], value)
		}
	}

	// 驗證數據完整性
	if len(forceData.Time) == 0 {
		return nil, fmt.Errorf("ANC xlsx 檔案沒有有效的數據行: %s", filePath)
	}

	dataLen := len(forceData.Time)
	for channelName, channelData := range forceData.Forces {
		if len(channelData) != dataLen {
			return nil, fmt.Errorf("通道 %s 的數據長度不一致", channelName)
		}
	}

	return forceData, nil
}

// parseSimpleXLSX 解析純數據表格式的 xlsx 檔案
func (p *ANCParser) parseSimpleXLSX(rows [][]string, filePath string) (*models.ForceData, error) {
	// 解析標題行（第一行應該包含 Time 和各通道名稱）
	headerRow := rows[0]
	if len(headerRow) < 2 {
		return nil, fmt.Errorf("Excel 標題行欄位不足: %s", filePath)
	}

	// 第一欄應該是 Time，後續欄位是通道名稱
	channelNames := make([]string, 0, len(headerRow)-1)
	for i := 1; i < len(headerRow); i++ {
		name := strings.TrimSpace(headerRow[i])
		if name != "" {
			channelNames = append(channelNames, name)
		}
	}

	if len(channelNames) == 0 {
		return nil, fmt.Errorf("Excel 檔案沒有找到有效的通道名稱: %s", filePath)
	}

	// 初始化數據結構
	forceData := &models.ForceData{
		Time:    make([]float64, 0, len(rows)-1),
		Forces:  make(map[string][]float64),
		Headers: channelNames,
	}

	// 為每個通道初始化切片
	for _, channelName := range channelNames {
		forceData.Forces[channelName] = make([]float64, 0, len(rows)-1)
	}

	// 解析數據行（從第二行開始）
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		row := rows[rowIdx]

		// 跳過空行
		if len(row) == 0 {
			continue
		}

		// 解析時間（第一欄）
		timeStr := strings.TrimSpace(row[0])
		if timeStr == "" {
			continue
		}

		timeValue, err := strconv.ParseFloat(timeStr, 64)
		if err != nil {
			continue // 跳過無效的時間值
		}
		forceData.Time = append(forceData.Time, timeValue)

		// 解析各通道數據
		for i, channelName := range channelNames {
			colIdx := i + 1
			var value float64 = 0
			if colIdx < len(row) {
				valueStr := strings.TrimSpace(row[colIdx])
				if valueStr != "" {
					parsedValue, err := strconv.ParseFloat(valueStr, 64)
					if err == nil {
						value = parsedValue
					}
				}
			}
			forceData.Forces[channelName] = append(forceData.Forces[channelName], value)
		}
	}

	// 驗證數據完整性
	if len(forceData.Time) == 0 {
		return nil, fmt.Errorf("Excel 檔案沒有有效的數據行: %s", filePath)
	}

	dataLen := len(forceData.Time)
	for channelName, channelData := range forceData.Forces {
		if len(channelData) != dataLen {
			return nil, fmt.Errorf("通道 %s 的數據長度不一致", channelName)
		}
	}

	return forceData, nil
}

// parseHeader 解析 ANC 檔案頭部
func (p *ANCParser) parseHeader(scanner *bufio.Scanner) (*ANCHeader, error) {
	header := &ANCHeader{}
	lineNum := 0

	for scanner.Scan() && lineNum < 12 { // 通常頭部在前12行內
		line := scanner.Text()
		lineNum++

		// 移除行號和製表符
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}

		// 獲取實際內容（跳過行號）
		content := strings.Join(parts[1:], "\t")

		switch lineNum {
		case 1: // File_Type 行
			if strings.Contains(content, "File_Type:") {
				fileParts := strings.Split(content, "\t")
				for i, part := range fileParts {
					if strings.Contains(part, "File_Type:") {
						header.FileType = strings.TrimSpace(strings.Split(part, ":")[1])
					} else if strings.Contains(part, "Generation#:") && i > 0 {
						// Generation number 通常在 File_Type 之後
					}
				}
			}

		case 2: // Board_Type 行
			if strings.Contains(content, "Board_Type:") {
				header.BoardType = p.extractValue(content, "Board_Type:")
			}

		case 3: // Trial 信息行
			if strings.Contains(content, "Trial_Name:") {
				header.TrialName = p.extractValue(content, "Trial_Name:")

				// 解析 Trial#
				if val := p.extractValue(content, "Trial#:"); val != "" {
					header.TrialNumber, _ = strconv.Atoi(val)
				}

				// 解析 Duration
				if val := p.extractValue(content, "Duration(Sec.):"); val != "" {
					header.Duration, _ = strconv.ParseFloat(val, 64)
				}

				// 解析 #Channels
				if val := p.extractValue(content, "#Channels:"); val != "" {
					header.NumChannels, _ = strconv.Atoi(strings.TrimSpace(val))
				}
			}

		case 4: // BitDepth 和 PreciseRate 行
			if strings.Contains(content, "BitDepth:") {
				if val := p.extractValue(content, "BitDepth:"); val != "" {
					header.BitDepth, _ = strconv.Atoi(val)
				}

				if val := p.extractValue(content, "PreciseRate:"); val != "" {
					header.PreciseRate, _ = strconv.ParseFloat(val, 64)
					p.frequency = header.PreciseRate
				}
			}

		case 9: // 通道名稱行
			if strings.Contains(content, "Name") {
				fields := strings.Fields(content)
				if len(fields) > 1 {
					header.ChannelNames = fields[1:] // 跳過 "Name"
				}
			}

		case 10: // 採樣率行
			if strings.Contains(content, "Rate") {
				fields := strings.Fields(content)
				if len(fields) > 1 {
					header.ChannelRates = make([]int, 0, len(fields)-1)
					for _, f := range fields[1:] {
						rate, _ := strconv.Atoi(f)
						header.ChannelRates = append(header.ChannelRates, rate)
					}
				}
			}

		case 11: // 範圍行
			if strings.Contains(content, "Range") {
				fields := strings.Fields(content)
				if len(fields) > 1 {
					header.ChannelRanges = make([]int, 0, len(fields)-1)
					for _, f := range fields[1:] {
						rang, _ := strconv.Atoi(f)
						header.ChannelRanges = append(header.ChannelRanges, rang)
					}
				}
			}

		case 12: // 數據開始
			// 這是第一行數據，需要回退
			return header, nil
		}
	}

	return header, nil
}

// extractValue 從字符串中提取指定標籤的值
func (p *ANCParser) extractValue(content, label string) string {
	parts := strings.Split(content, "\t")
	for _, part := range parts {
		if strings.Contains(part, label) {
			valueParts := strings.Split(part, ":")
			if len(valueParts) >= 2 {
				return strings.TrimSpace(valueParts[1])
			}
		}
	}
	return ""
}

// GetDataInTimeRange 獲取指定時間範圍內的數據
// 使用整數毫秒進行比較，避免浮點數精度問題
func (p *ANCParser) GetDataInTimeRange(data *models.ForceData, startTime, endTime float64) (*models.ForceData, error) {
	if startTime > endTime {
		return nil, fmt.Errorf("開始時間 %.3f 不能大於結束時間 %.3f", startTime, endTime)
	}

	// 將時間轉換為整數毫秒進行比較，避免浮點數精度問題
	startTimeMs := int64(math.Round(startTime * 1000))
	endTimeMs := int64(math.Round(endTime * 1000))

	// 找到時間範圍的索引
	startIdx := -1
	endIdx := -1

	for i, t := range data.Time {
		tMs := int64(math.Round(t * 1000))
		if startIdx == -1 && tMs >= startTimeMs {
			startIdx = i
		}
		if tMs <= endTimeMs {
			endIdx = i
		} else if endIdx != -1 {
			break
		}
	}

	if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
		return nil, fmt.Errorf("找不到有效的時間範圍數據")
	}

	// 創建子集數據
	rangeData := &models.ForceData{
		Time:    data.Time[startIdx : endIdx+1],
		Forces:  make(map[string][]float64),
		Headers: data.Headers,
	}

	// 複製力值數據
	for channelName, forceData := range data.Forces {
		rangeData.Forces[channelName] = forceData[startIdx : endIdx+1]
	}

	return rangeData, nil
}

// GetSampleInterval 獲取採樣間隔（秒）
func (p *ANCParser) GetSampleInterval() float64 {
	return 1.0 / p.frequency
}

// ValidateForceData 驗證力板數據
func ValidateForceData(data *models.ForceData) error {
	if data == nil {
		return fmt.Errorf("力板數據為空")
	}

	if len(data.Time) == 0 {
		return fmt.Errorf("力板時間序列為空")
	}

	if len(data.Forces) == 0 {
		return fmt.Errorf("力板沒有任何通道數據")
	}

	// 檢查時間序列是否遞增
	for i := 1; i < len(data.Time); i++ {
		if data.Time[i] <= data.Time[i-1] {
			return fmt.Errorf("力板時間序列在索引 %d 處不是遞增的", i)
		}
	}

	// 檢查所有通道數據長度一致
	expectedLen := len(data.Time)
	for channelName, forceData := range data.Forces {
		if len(forceData) != expectedLen {
			return fmt.Errorf("通道 %s 的數據長度 (%d) 與時間序列長度 (%d) 不符",
				channelName, len(forceData), expectedLen)
		}
	}

	return nil
}
