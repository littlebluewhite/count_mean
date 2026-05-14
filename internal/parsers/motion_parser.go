package parsers

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// MotionParser Motion檔案解析器.
type MotionParser struct {
	frequency   float64 // 採樣頻率 Hz
	categoryRow int     // 類別行（從0開始）- 如 "Trunk Angle"
	subcatRow   int     // 子類別行（從0開始）- 如 "Trunk Flexion / Extension..."
	headerRow   int     // 標題所在行（從0開始）- 如 "Series"
	dataRow     int     // 數據開始行（從0開始）
}

// NewMotionParser 創建新的 Motion 解析器.
func NewMotionParser() *MotionParser {
	return &MotionParser{
		frequency:   FrequencyMotion,
		categoryRow: MotionCategoryRow,
		subcatRow:   MotionSubcatRow,
		headerRow:   MotionHeaderRow,
		dataRow:     MotionDataRow,
	}
}

// readCSVRecords 讀取 CSV 檔案並返回記錄.
//
//nolint:revive // keep consistent API
func (p *MotionParser) readCSVRecords(filepath string) ([][]string, error) {
	file, err := os.OpenFile(filepath, fsperm.ReadFlags, 0) //nolint:gosec // filepath validated by caller; fsperm.ReadFlags adds O_NOFOLLOW (symmetric with write-side)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 Motion 檔案 %s: %w", filepath, err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	// Buffered single-pass 與 ReadCSVDirect 對稱：bufio + PeekBOM 取代過去
	// 直接 csv.NewReader(file) 無 BOM 偵測的不對稱 — Motion CSV 若由 Excel 匯出
	// 含 UTF-8 BOM (0xEF 0xBB 0xBF)，第一個欄位（如 Series header label）會被汙染。
	// reader.ReadAll() 仍會 materialize [][]string；非 row-by-row 流式（cross-compare review）。
	bufReader := bufio.NewReaderSize(file, csvReaderBufSize)
	if _, err := csvutil.PeekBOM(bufReader); err != nil {
		return nil, fmt.Errorf("讀取 Motion BOM 失敗: %w", err)
	}

	reader := csv.NewReader(bufReader)
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("讀取 Motion CSV 失敗: %w", err)
	}

	return records, nil
}

// validateRecordStructure 驗證記錄結構.
//
//nolint:err113 // dynamic errors with Chinese messages; Motion is proper noun
func (p *MotionParser) validateRecordStructure(records [][]string) error {
	if len(records) <= p.dataRow {
		return fmt.Errorf("Motion 檔案格式錯誤：數據行不足")
	}

	if p.headerRow >= len(records) {
		return fmt.Errorf("Motion 檔案格式錯誤：找不到標題行")
	}

	return nil
}

// extractHeaderRows 提取類別行和子類別行.
//
//nolint:nonamedreturns // named returns improve readability for multiple return values
func (p *MotionParser) extractHeaderRows(records [][]string) (categoryRow, subcatRow []string) {
	if p.categoryRow < len(records) {
		categoryRow = records[p.categoryRow]
	}

	if p.subcatRow < len(records) {
		subcatRow = records[p.subcatRow]
	}

	return categoryRow, subcatRow
}

// initializeMotionData 初始化 MotionData 結構.
//
//nolint:revive // unused-receiver: keep consistent API
func (p *MotionParser) initializeMotionData(headers []string, capacity int) *models.MotionData {
	motionData := &models.MotionData{
		Indices: make([]int, 0, capacity),
		Data:    make(map[string][]float64),
		Headers: headers[1:],
	}

	for _, header := range motionData.Headers {
		motionData.Data[header] = make([]float64, 0, capacity)
	}

	return motionData
}

// parseDataRecord 解析單一數據行.
//
//nolint:revive // unused-receiver: keep consistent API
func (p *MotionParser) parseDataRecord(record, headers []string, motionData *models.MotionData) bool {
	if len(record) == 0 || strings.TrimSpace(record[0]) == "" || len(record) < len(headers) {
		return false
	}

	indexValue, err := strconv.Atoi(strings.TrimSpace(record[0]))
	if err != nil {
		return false
	}

	motionData.Indices = append(motionData.Indices, indexValue)

	for j := 1; j < len(headers) && j < len(record); j++ {
		value, _ := ParseFloatCell(record[j])
		motionData.Data[headers[j]] = append(motionData.Data[headers[j]], value)
	}

	return true
}

// validateDataIntegrity 驗證數據完整性.
//
//nolint:revive,err113 // unused-receiver; dynamic errors with Chinese messages; Motion is proper noun
func (p *MotionParser) validateDataIntegrity(motionData *models.MotionData) error {
	dataLen := len(motionData.Indices)
	if dataLen == 0 {
		return fmt.Errorf("Motion 檔案沒有有效數據")
	}

	for columnName, columnData := range motionData.Data {
		if len(columnData) != dataLen {
			return fmt.Errorf("列 %s 的數據長度不一致", columnName)
		}
	}

	return nil
}

// ParseFile 解析 Motion CSV 檔案.
//
//nolint:err113 // dynamic errors with Chinese messages; Motion is proper noun
func (p *MotionParser) ParseFile(filepath string) (*models.MotionData, error) {
	records, err := p.readCSVRecords(filepath)
	if err != nil {
		return nil, err
	}

	if err := p.validateRecordStructure(records); err != nil {
		return nil, err
	}

	categoryRow, subcatRow := p.extractHeaderRows(records)
	headers := p.buildUniqueHeaders(records[p.headerRow], categoryRow, subcatRow)

	if len(headers) < 2 {
		return nil, fmt.Errorf("Motion 檔案標題不足：至少需要 index 列和一個數據列")
	}

	motionData := p.initializeMotionData(headers, len(records)-p.dataRow)

	for i := p.dataRow; i < len(records); i++ {
		p.parseDataRecord(records[i], headers, motionData)
	}

	if err := p.validateDataIntegrity(motionData); err != nil {
		return nil, err
	}

	return motionData, nil
}

// getNameFromRow 從指定行中獲取名稱.
func getNameFromRow(row []string, index int) string {
	if index < len(row) {
		return strings.TrimSpace(row[index])
	}

	return ""
}

// extractSubcatName 從子類別字串中提取名稱（括號前的部分）.
func extractSubcatName(subcat string) string {
	if subcat == "" {
		return ""
	}

	if idx := strings.Index(subcat, "("); idx > 0 {
		return strings.TrimSpace(subcat[:idx])
	}

	return subcat
}

// resolveColumnName 解析列名稱（優先使用 category，其次 subcat，最後 header）.
func resolveColumnName(index int, headerName string, categoryRow, subcatRow []string) string {
	if name := getNameFromRow(categoryRow, index); name != "" {
		return name
	}

	if name := extractSubcatName(getNameFromRow(subcatRow, index)); name != "" {
		return name
	}

	return headerName
}

// ensureUniqueName 確保名稱唯一，如有重複則加上索引.
func ensureUniqueName(name string, usedNames map[string]int) string {
	if count, exists := usedNames[name]; exists {
		usedNames[name] = count + 1
		return fmt.Sprintf("%s_%d", name, count+1)
	}

	usedNames[name] = 1

	return name
}

// buildUniqueHeaders 構建唯一的標題列表.
// 優先使用 categoryRow，如果為空則使用 subcatRow，最後加上索引確保唯一性.
//
//nolint:revive // unused-receiver: keep consistent API
func (p *MotionParser) buildUniqueHeaders(headerRow, categoryRow, subcatRow []string) []string {
	headers := make([]string, 0, len(headerRow))
	usedNames := make(map[string]int)

	for i, h := range headerRow {
		trimmed := strings.TrimSpace(h)
		if trimmed == "" {
			continue
		}

		if i == 0 {
			headers = append(headers, trimmed)
			continue
		}

		uniqueName := resolveColumnName(i, trimmed, categoryRow, subcatRow)
		uniqueName = ensureUniqueName(uniqueName, usedNames)
		headers = append(headers, uniqueName)
	}

	return headers
}

// GetSampleInterval 獲取採樣間隔（秒）.
func (p *MotionParser) GetSampleInterval() float64 {
	return 1.0 / p.frequency
}

// IndexToTime 將 Motion index 轉換為時間（秒）.
func (p *MotionParser) IndexToTime(index int) float64 {
	// Motion index 從 1 開始，時間從 0 開始
	return float64(index-1) * p.GetSampleInterval()
}

// TimeToIndex 將時間（秒）轉換為最接近的 Motion index.
func (p *MotionParser) TimeToIndex(time float64) int {
	// 四捨五入到最接近的 index
	index := int(time/p.GetSampleInterval()+RoundingOffset) + 1
	if index < 1 {
		index = 1
	}

	return index
}

// GetMotionDataAtIndex 獲取指定 index 的數據.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func GetMotionDataAtIndex(data *models.MotionData, targetIndex int) (map[string]float64, error) {
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

// GetMotionDataInIndexRange 獲取指定 index 範圍內的數據.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func GetMotionDataInIndexRange(
	data *models.MotionData, startIndex, endIndex int,
) (*models.MotionData, error) {
	if startIndex > endIndex {
		return nil, fmt.Errorf("開始 index %d 不能大於結束 index %d", startIndex, endIndex)
	}

	startPos, endPos, err := FindIndexRangeIndices(data.Indices, startIndex, endIndex)
	if err != nil {
		return nil, err
	}

	rangeData := &models.MotionData{
		Indices: data.Indices[startPos : endPos+1],
		Data:    make(map[string][]float64),
		Headers: data.Headers,
	}

	for columnName, columnData := range data.Data {
		rangeData.Data[columnName] = columnData[startPos : endPos+1]
	}

	return rangeData, nil
}

// ValidateMotionData 驗證 Motion 數據.
//
//nolint:err113 // dynamic Chinese error message; Motion is proper noun
func ValidateMotionData(data *models.MotionData) error {
	if data == nil {
		return fmt.Errorf("Motion 數據為空: %w", ErrNilData)
	}

	return ValidateTimeSeries(data.Indices, data.Data, TimeSeriesLabels{
		DataName:     "Motion",
		SeriesName:   "index 序列",
		SeriesPos:    "位置",
		ChannelLabel: "列",
	})
}
