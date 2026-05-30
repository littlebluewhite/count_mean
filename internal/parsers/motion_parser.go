package parsers

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// MotionParser Motion檔案解析器.
type MotionParser struct {
	frequency   float64         // 採樣頻率 Hz
	categoryRow int             // 類別行（從0開始）- 如 "Trunk Angle"
	subcatRow   int             // 子類別行（從0開始）- 如 "Trunk Flexion / Extension..."
	headerRow   int             // 標題所在行（從0開始）- 如 "Series"
	dataRow     int             // 數據開始行（從0開始）
	logger      *logging.Logger // 用於記錄 skip 警告（含 row number / cell 值）
}

// NewMotionParser 創建新的 Motion 解析器.
func NewMotionParser() *MotionParser {
	return &MotionParser{
		frequency:   FrequencyMotion,
		categoryRow: MotionCategoryRow,
		subcatRow:   MotionSubcatRow,
		headerRow:   MotionHeaderRow,
		dataRow:     MotionDataRow,
		logger:      logging.GetLogger("motion_parser"),
	}
}

// NewMotionParserWithLogger 創建帶自訂 logger 的 Motion 解析器。
// 若 logger 為 nil 則 fallback 到 default logger。
//
// 主要用途：測試時注入 bytes.Buffer-backed logger 捕捉 row-skip 警告，
// 或上層 caller 想要把 motion parsing 警告路由到自家 module logger。
func NewMotionParserWithLogger(logger *logging.Logger) *MotionParser {
	if logger == nil {
		logger = logging.GetLogger("motion_parser")
	}

	return &MotionParser{
		frequency:   FrequencyMotion,
		categoryRow: MotionCategoryRow,
		subcatRow:   MotionSubcatRow,
		headerRow:   MotionHeaderRow,
		dataRow:     MotionDataRow,
		logger:      logger,
	}
}

// readCSVRecords 讀取 CSV 檔案並返回記錄.
//
// Thin wrapper: opens with fsperm.ReadFlags (O_NOFOLLOW) then delegates to the
// shared BOM-aware ReadCSVRecords core. BOM 偵測對 Motion CSV 是 load-bearing —
// Excel 匯出含 UTF-8 BOM (0xEF 0xBB 0xBF) 時第一欄（Series header label）會被汙染。
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

	records, err := ReadCSVRecords(file)
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
	// headers[0] 是 Index 欄一律排除;其餘可能含位置佔位用的 "" 空欄
	//(見 buildUniqueHeaders)— 過濾掉,只有具名欄成為 series。
	dataHeaders := make([]string, 0, len(headers))
	for _, h := range headers[1:] {
		if h == "" {
			continue
		}
		dataHeaders = append(dataHeaders, h)
	}

	motionData := &models.MotionData{
		Indices: make([]int, 0, capacity),
		Data:    make(map[string][]float64),
		Headers: dataHeaders,
	}

	for _, header := range dataHeaders {
		motionData.Data[header] = make([]float64, 0, capacity)
	}

	return motionData
}

// parseDataRecord 解析單一數據行。
//
// rowNumber 為 1-based 絕對 CSV 行號（含 metadata / header rows），用於 skip 警告。
// 任何 silent skip 情境都會發 Warn level log（含 row_number + offending cell），
// 方便操作員之後在原始 CSV 找到問題行（cross-compare 避免 silent miscount）。
//
// L10:空字串 / 空 record 的 skip 改 Info-level log。trailing newline 等正常結尾
// 情境也會走這裡(實務上常見:Excel 匯出末尾有空行),嘈雜的 Warn 會掩蓋真正的
// malformed row;改 Info 既保留 audit trail 又不會在正常檔案干擾預設 log level。
func (p *MotionParser) parseDataRecord(
	record, headers []string, motionData *models.MotionData, rowNumber int,
) bool {
	if len(record) == 0 || strings.TrimSpace(record[0]) == "" {
		// L10:Info-level 而非 Warn,因為 trailing blank line 常見且無害。
		// 仍記 row_number 讓操作員若需追查 silent miscount 時能對齊 CSV。
		p.logger.Info("Motion CSV 空行已跳過", map[string]any{
			"row_number": rowNumber,
			"reason":     emptyRowSkipReason(record),
		})
		return false
	}

	if len(record) < len(headers) {
		p.logger.Warn("Motion CSV 行欄位數不足，已跳過", map[string]any{
			"row_number":  rowNumber,
			"got_fields":  len(record),
			"want_fields": len(headers),
			"first_cell":  record[0],
		})
		return false
	}

	indexValue, err := strconv.Atoi(strings.TrimSpace(record[0]))
	if err != nil {
		p.logger.Warn("Motion CSV 第 1 欄非整數，已跳過", map[string]any{
			"row_number": rowNumber,
			"first_cell": record[0],
			"error":      err.Error(),
		})
		return false
	}

	motionData.Indices = append(motionData.Indices, indexValue)

	for j := 1; j < len(headers) && j < len(record); j++ {
		if headers[j] == "" {
			// 佔位空欄(spacer):位置仍前進以維持 record[j] 對齊,但不收進任何 series。
			continue
		}
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
// Thin wrapper: readCSVRecords opens with fsperm.ReadFlags (O_NOFOLLOW) then
// delegates to the shared parseMotionRecords core. The reader-based Parse lets
// the Phase-D validated-open door hand an already-open *os.File straight in.
func (p *MotionParser) ParseFile(filepath string) (*models.MotionData, error) {
	records, err := p.readCSVRecords(filepath)
	if err != nil {
		return nil, err
	}

	return p.parseMotionRecords(records)
}

// Parse 從 io.Reader 解析 Motion CSV 資料。name 僅用於 error context（reader 不知道
// 自己的檔名）。語意與 ParseFile 相同。
//
//nolint:err113 // dynamic errors with Chinese messages; Motion is proper noun
func (p *MotionParser) Parse(r io.Reader, name string) (*models.MotionData, error) {
	records, err := ReadCSVRecords(r)
	if err != nil {
		return nil, fmt.Errorf("讀取 Motion 檔案 %s 失敗: %w", name, err)
	}

	return p.parseMotionRecords(records)
}

// parseMotionRecords 由 ParseFile / Parse 共用的 Motion record 解析核心。
//
//nolint:err113 // dynamic errors with Chinese messages; Motion is proper noun
func (p *MotionParser) parseMotionRecords(records [][]string) (*models.MotionData, error) {
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
		// rowNumber 採 1-based 絕對 CSV 行號（i 為 0-based）讓警告與使用者眼中的
		// "第幾行" 對齊。
		p.parseDataRecord(records[i], headers, motionData, i+1)
	}

	if err := p.validateDataIntegrity(motionData); err != nil {
		return nil, err
	}

	return motionData, nil
}

// emptyRowSkipReason (L10) 用於 parseDataRecord 空行 skip 的 log,把「為什麼這行
// 被認定為空」明確標出來,方便對照原始 CSV 推斷 trailing newline / leading 空
// cell / mid-file blank。
func emptyRowSkipReason(record []string) string {
	if len(record) == 0 {
		return "empty record (no fields)"
	}
	return "first cell is blank after trim"
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
// 欄位數的真相來源不是單一列:真實 Motion CSV 的 header(Series)列可能稀疏 ——
// 例如 NSF11_BTS_5_ok_OK_20Hz.csv 的 `Index,Series,,,Series` 只在有 marker 的欄
// 填字,真正每一欄的名字落在 category / subcat 列。因此取三列最寬者逐「欄位位置」
// 解析,確保 category/subcat 有名的欄不被漏掉。
//
// **位置對齊不可破壞**:parseDataRecord 按 record[j] 位置餵值,headers[j] 必須對應
// CSV 第 j 欄。若某欄三列皆空(中間的 spacer 欄),不可 compact 抽掉 —— 否則後面的
// 具名欄會位移、配到 spacer 欄的值 → 靜默資料錯位。改以 "" 佔位保留位置,由
// initializeMotionData / parseDataRecord 跳過不產生 series。回傳的 headers 因此與
// CSV 欄位位置 1:1 對齊(尾端純佔位欄除外,見下方 trim)。
//
//nolint:revive // unused-receiver: keep consistent API
func (p *MotionParser) buildUniqueHeaders(headerRow, categoryRow, subcatRow []string) []string {
	width := max(len(headerRow), len(categoryRow), len(subcatRow))

	headers := make([]string, 0, width)
	usedNames := make(map[string]int)

	for i := range width {
		if i == 0 {
			// 第 0 欄為 Index 欄,名稱取自 header 列(category/subcat 此欄通常為空);
			// 此 slot 之後會被 initializeMotionData 的 headers[1:] 剝掉,僅作位置佔位。
			headers = append(headers, getNameFromRow(headerRow, 0))
			continue
		}

		uniqueName := resolveColumnName(i, getNameFromRow(headerRow, i), categoryRow, subcatRow)
		if uniqueName == "" {
			// 三列在此欄皆空 → 真正的空欄。append "" 佔位保留位置對齊(見上方 doc)。
			headers = append(headers, "")
			continue
		}

		headers = append(headers, ensureUniqueName(uniqueName, usedNames))
	}

	// 移除 trailing 佔位欄:它們後面沒有具名欄,不需保留位置。留著會撐大 len(headers),
	// 害 parseDataRecord 的 `len(record) < len(headers)` 保護把正常 ragged data row
	// (Excel 末欄省略 comma)誤判為「欄位數不足」而整列跳過。中間的佔位欄必須留。
	for len(headers) > 0 && headers[len(headers)-1] == "" {
		headers = headers[:len(headers)-1]
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
