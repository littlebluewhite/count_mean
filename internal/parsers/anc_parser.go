package parsers

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// ANC capacity bounds — Duration × PreciseRate 來自 attacker-controlled header；
// 沒有 clamp 時 1e308 級乘積 → int 溢位 / make() panic。
//
// maxANCSampleCapacity = 100M samples ≈ 800MB float64 per channel：real-world
// 最長練習 1hr × 10kHz = 36M，留 ~3× 緩衝；超過視為惡意 header，退回 default。
const (
	maxANCSampleCapacity = 100_000_000
	defaultANCCapacity   = 1024
)

// ANCParser ANC力板檔案解析器.
type ANCParser struct {
	frequency float64 // 採樣頻率 Hz
}

// NewANCParser 創建新的 ANC 解析器.
func NewANCParser() *ANCParser {
	return &ANCParser{
		frequency: 0,
	}
}

// ANCHeader ANC檔案頭信息.
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

// ParseFile 解析 ANC 檔案（支援 .anc 文字格式和 .xlsx Excel 格式）.
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

// initForceDataFromHeader initializes ForceData structure from ANC header.
func initForceDataFromHeader(header *ANCHeader) *models.ForceData {
	capacity := clampANCCapacity(header.Duration * header.PreciseRate)
	forceData := &models.ForceData{
		Time:    make([]float64, 0, capacity),
		Forces:  make(map[string][]float64),
		Headers: header.ChannelNames,
	}

	for _, channelName := range header.ChannelNames {
		forceData.Forces[channelName] = make([]float64, 0, capacity)
	}

	return forceData
}

// clampANCCapacity 把使用者 header 推算出的容量限制在合理範圍。
// NaN / Inf / 負值 / 超過 maxANCSampleCapacity 視為惡意 header，回退到 default
// 預配空間，後續 append 仍會動態擴展，不影響正確檔案的解析；只是擋掉 panic / OOM。
func clampANCCapacity(raw float64) int {
	if math.IsNaN(raw) || math.IsInf(raw, 0) || raw < 0 || raw > maxANCSampleCapacity {
		return defaultANCCapacity
	}

	return int(raw)
}

// parseANCDataLine parses a single data line and appends to forceData.
func parseANCDataLine(fields, channelNames []string, forceData *models.ForceData) bool {
	if len(fields) == 0 {
		return false
	}

	timeValue, ok := ParseFloatCell(fields[0])
	if !ok {
		return false
	}

	forceData.Time = append(forceData.Time, timeValue)

	for i, channelName := range channelNames {
		var value float64

		if i+1 < len(fields) {
			value, _ = ParseFloatCell(fields[i+1])
		}

		forceData.Forces[channelName] = append(forceData.Forces[channelName], value)
	}

	return true
}

// validateForceDataIntegrity validates that all channels have consistent data length.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func validateForceDataIntegrity(forceData *models.ForceData) error {
	dataLen := len(forceData.Time)
	for channelName, channelData := range forceData.Forces {
		if len(channelData) != dataLen {
			return fmt.Errorf("通道 %s 的數據長度不一致", channelName)
		}
	}

	return nil
}

// parseANCTextFile 解析文字格式的 ANC 檔案.
func (p *ANCParser) parseANCTextFile(filePath string) (*models.ForceData, error) {
	file, err := os.OpenFile(filePath, fsperm.ReadFlags, 0) //nolint:gosec // filePath validated by caller; fsperm.ReadFlags adds O_NOFOLLOW (symmetric with write-side)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 ANC 檔案 %s: %w", filePath, err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			// Log error but don't override original error
			_ = closeErr
		}
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, BufferInitKB*KilobyteMultiplier), BufferMaxBytes)

	header, err := p.parseHeader(scanner)
	if err != nil {
		return nil, fmt.Errorf("解析 ANC 頭部失敗: %w", err)
	}

	forceData := initForceDataFromHeader(header)
	minFieldCount := len(header.ChannelNames) + 1

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < minFieldCount {
			continue
		}

		parseANCDataLine(fields, header.ChannelNames, forceData)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("讀取 ANC 檔案時發生錯誤: %w", err)
	}

	if err := validateForceDataIntegrity(forceData); err != nil {
		return nil, err
	}

	if freq, freqErr := computeFrequencyFromTime(forceData.Time); freqErr == nil {
		p.frequency = freq
	}

	return forceData, nil
}

// 2. ANC 格式的 xlsx（有 11 行頭部資訊，第 9 行是通道名稱，第 12 行開始是數據）.
//
//nolint:err113 // dynamic errors with Chinese messages; Excel is proper noun
func (p *ANCParser) parseXLSXFile(filePath string) (*models.ForceData, error) {
	// 開啟 Excel 檔案
	excelFile, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 Excel 檔案 %s: %w", filePath, err)
	}

	defer func() {
		if closeErr := excelFile.Close(); closeErr != nil {
			_ = closeErr // Ignore close error
		}
	}()

	// 獲取第一個工作表名稱
	sheetName := excelFile.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("Excel 檔案沒有工作表: %s", filePath)
	}

	// 讀取所有行
	rows, err := excelFile.GetRows(sheetName)
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

// parseANCFormatXLSX 解析 ANC 格式的 xlsx 檔案（有頭部資訊）.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func (p *ANCParser) parseANCFormatXLSX(rows [][]string, filePath string) (*models.ForceData, error) {
	// ANC 格式結構：
	// Row 1-8: 頭部資訊
	// Row 9: 通道名稱（Name, F1X, F1Y, ...）
	// Row 10: 採樣率
	// Row 11: 範圍
	// Row 12+: 數據（時間, 數值...）
	if len(rows) < ANCDataStartLine {
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
		Time:    make([]float64, 0, len(rows)-ANCHeaderRows),
		Forces:  make(map[string][]float64),
		Headers: channelNames,
	}

	// 為每個通道初始化切片
	for _, channelName := range channelNames {
		forceData.Forces[channelName] = make([]float64, 0, len(rows)-ANCHeaderRows)
	}

	// 從第 12 行開始解析數據（索引 11）
	parseDataRows(rows, ANCHeaderRows, channelNames, forceData)

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

	if freq, freqErr := computeFrequencyFromTime(forceData.Time); freqErr == nil {
		p.frequency = freq
	}

	return forceData, nil
}

// parseSimpleXLSX 解析純數據表格式的 xlsx 檔案.
//
//nolint:err113 // dynamic errors with Chinese messages; Excel is proper noun
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
	parseDataRows(rows, 1, channelNames, forceData)

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

	if freq, freqErr := computeFrequencyFromTime(forceData.Time); freqErr == nil {
		p.frequency = freq
	}

	return forceData, nil
}

// parseDataRow parses a single data row and appends values to forceData.
// Returns false if the row should be skipped.
func parseDataRow(row, channelNames []string, forceData *models.ForceData) bool {
	if len(row) == 0 {
		return false
	}

	timeValue, ok := ParseFloatCell(row[0])
	if !ok {
		return false
	}

	forceData.Time = append(forceData.Time, timeValue)

	for i, channelName := range channelNames {
		var value float64

		if i+1 < len(row) {
			value, _ = ParseFloatCell(row[i+1])
		}

		forceData.Forces[channelName] = append(forceData.Forces[channelName], value)
	}

	return true
}

// parseDataRows 解析數據行，從指定的起始行索引開始.
// 這是一個共用的輔助函數，用於解析時間序列和通道數據.
func parseDataRows(rows [][]string, startRowIdx int, channelNames []string, forceData *models.ForceData) {
	for rowIdx := startRowIdx; rowIdx < len(rows); rowIdx++ {
		parseDataRow(rows[rowIdx], channelNames, forceData)
	}
}

// lineHandler 定義行處理函數類型.
type lineHandler func(p *ANCParser, header *ANCHeader, content string)

// lineHandlers 行處理器映射表.
//
//nolint:gochecknoglobals // immutable configuration mapping
var lineHandlers = map[int]lineHandler{
	1:  handleFileTypeLine,
	2:  handleBoardTypeLine,
	3:  handleTrialInfoLine,
	4:  handleBitDepthLine,
	9:  handleChannelNamesLine,
	10: handleChannelRatesLine,
	11: handleChannelRangesLine,
}

// handleFileTypeLine 處理 File_Type 行.
func handleFileTypeLine(_ *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "File_Type:") {
		return
	}

	fileParts := strings.Split(content, "\t")
	for _, part := range fileParts {
		if strings.Contains(part, "File_Type:") {
			header.FileType = strings.TrimSpace(strings.Split(part, ":")[1])

			return
		}
	}
}

// handleBoardTypeLine 處理 Board_Type 行.
func handleBoardTypeLine(p *ANCParser, header *ANCHeader, content string) {
	if strings.Contains(content, "Board_Type:") {
		header.BoardType = p.extractValue(content, "Board_Type:")
	}
}

// handleTrialInfoLine 處理 Trial 信息行.
func handleTrialInfoLine(p *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "Trial_Name:") {
		return
	}

	header.TrialName = p.extractValue(content, "Trial_Name:")

	if val := p.extractValue(content, "Trial#:"); val != "" {
		header.TrialNumber, _ = strconv.Atoi(val) //nolint:errcheck // optional header field
	}

	if val := p.extractValue(content, "Duration(Sec.):"); val != "" {
		header.Duration, _ = strconv.ParseFloat(val, 64) //nolint:errcheck // optional header field
	}

	if val := p.extractValue(content, "#Channels:"); val != "" {
		header.NumChannels, _ = strconv.Atoi(strings.TrimSpace(val)) //nolint:errcheck // optional header field
	}
}

// handleBitDepthLine 處理 BitDepth 和 PreciseRate 行.
func handleBitDepthLine(p *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "BitDepth:") {
		return
	}

	if val := p.extractValue(content, "BitDepth:"); val != "" {
		header.BitDepth, _ = strconv.Atoi(val) //nolint:errcheck // optional header field
	}

	if val := p.extractValue(content, "PreciseRate:"); val != "" {
		header.PreciseRate, _ = strconv.ParseFloat(val, 64) //nolint:errcheck // optional header field
	}
}

// handleChannelNamesLine 處理通道名稱行.
func handleChannelNamesLine(_ *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "Name") {
		return
	}

	fields := strings.Fields(content)
	if len(fields) > 1 {
		header.ChannelNames = fields[1:] // 跳過 "Name"
	}
}

// handleChannelRatesLine 處理採樣率行.
func handleChannelRatesLine(_ *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "Rate") {
		return
	}

	fields := strings.Fields(content)
	if len(fields) > 1 {
		header.ChannelRates = make([]int, 0, len(fields)-1)

		for _, f := range fields[1:] {
			rate, _ := strconv.Atoi(f) //nolint:errcheck // optional header field
			header.ChannelRates = append(header.ChannelRates, rate)
		}
	}
}

// handleChannelRangesLine 處理範圍行.
func handleChannelRangesLine(_ *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "Range") {
		return
	}

	fields := strings.Fields(content)
	if len(fields) > 1 {
		header.ChannelRanges = make([]int, 0, len(fields)-1)

		for _, f := range fields[1:] {
			rang, _ := strconv.Atoi(f) //nolint:errcheck // optional header field
			header.ChannelRanges = append(header.ChannelRanges, rang)
		}
	}
}

// parseHeader 解析 ANC 檔案頭部.
//
//nolint:unparam // error return kept for future extensibility and API consistency
func (p *ANCParser) parseHeader(scanner *bufio.Scanner) (*ANCHeader, error) {
	header := &ANCHeader{}
	lineNum := 0

	for scanner.Scan() && lineNum < ANCDataStartLine { // 通常頭部在前12行內
		line := scanner.Text()
		lineNum++

		// 移除行號和製表符
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}

		// 獲取實際內容（跳過行號）
		content := strings.Join(parts[1:], "\t")

		// 第12行是數據開始
		if lineNum == ANCDataStartLine {
			return header, nil
		}

		// 使用處理器映射表處理對應行
		if handler, exists := lineHandlers[lineNum]; exists {
			handler(p, header, content)
		}
	}

	return header, nil
}

// extractValue 從字符串中提取指定標籤的值.
//
//nolint:revive // unused-receiver: keep consistent API
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

// GetANCDataInTimeRange returns force data within the specified time range.
// Uses integer milliseconds for comparison to avoid floating point precision issues.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func GetANCDataInTimeRange(data *models.ForceData, startTime, endTime float64) (*models.ForceData, error) {
	if startTime > endTime {
		return nil, fmt.Errorf("開始時間 %.3f 不能大於結束時間 %.3f", startTime, endTime)
	}

	startIdx, endIdx, err := FindTimeRangeIndices(data.Time, startTime, endTime)
	if err != nil {
		return nil, err
	}

	rangeData := &models.ForceData{
		Time:    data.Time[startIdx : endIdx+1],
		Forces:  make(map[string][]float64),
		Headers: data.Headers,
	}

	for channelName, forceData := range data.Forces {
		rangeData.Forces[channelName] = forceData[startIdx : endIdx+1]
	}

	return rangeData, nil
}

// GetSampleInterval 獲取採樣間隔（秒）.
func (p *ANCParser) GetSampleInterval() float64 {
	if p.frequency == 0 {
		return 0
	}

	return 1.0 / p.frequency
}

// ValidateForceData 驗證力板數據.
//
//nolint:err113 // dynamic error message with Chinese for user-facing output
func ValidateForceData(data *models.ForceData) error {
	if data == nil {
		return fmt.Errorf("力板數據為空: %w", ErrNilData)
	}

	return ValidateTimeSeries(data.Time, data.Forces, TimeSeriesLabels{
		DataName:     "力板",
		SeriesName:   "時間序列",
		SeriesPos:    "索引",
		ChannelLabel: "通道",
	})
}
