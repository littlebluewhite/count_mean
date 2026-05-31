package parsers

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
)

// ANC parser 缺欄位 sentinel errors，方便 caller 用 errors.Is 區分原因。
var (
	ErrANCIncompleteHeader      = errors.New("ANC 檔案頭部不完整")
	ErrANCMissingDuration       = errors.New("缺少 Duration 欄位")
	ErrANCMissingRate           = errors.New("缺少 PreciseRate 欄位")
	ErrANCMissingChannels       = errors.New("缺少通道名稱")
	ErrANCTotalCapacityExceeded = errors.New("ANC 預配總記憶體超出上限")
)

// ANC capacity bounds — Duration × PreciseRate 來自 attacker-controlled header；
// 沒有 clamp 時 1e308 級乘積 → int 溢位 / make() panic。
//
// maxANCSampleCapacity = 100M samples ≈ 800MB float64 per channel：real-world
// 最長練習 1hr × 10kHz = 36M，留 ~3× 緩衝；超過視為惡意 header，退回 default。
//
// maxANCTotalAllocBytes = 2 GiB：P1-A3-1 補完。clampANCCapacity 只擋每通道上限，
// 沒擋「通道數 × 每通道」乘積。64 ch × 100M sample/ch × 8 byte = 51 GB OOM。
// 2 GiB 上限可容納 64 ch × 1 hr × 1 kHz（典型 force plate + EMG 配置 < 20 MiB），
// 也可容納單一 100M sample/ch（=800 MiB）× 2 ch；超過視為 header 異常。
const (
	maxANCSampleCapacity  = 100_000_000
	defaultANCCapacity    = 1024
	maxANCTotalAllocBytes = 2 * 1024 * 1024 * 1024 // 2 GiB
	float64SizeBytes      = 8
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

// isXLSXName 依副檔名判斷是否為 xlsx（含複合副檔名如 .anc.xlsx）。
func isXLSXName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".xlsx")
}

// Parse 從 io.Reader 解析 ANC 資料，依 name 的副檔名分流 text / xlsx。
// name 同時提供 (a) 副檔名分流 與 (b) error context（reader 不知道自己的檔名）。
//
// xlsx 走 excelize.OpenReader 而非 excelize.OpenFile(path)，因此 caller 以
// fsperm.ReadFlags 開檔後傳入的 reader 仍享 O_NOFOLLOW 保護 —
// 補上 excelize.OpenFile 原本繞過 O_NOFOLLOW 的破口。
func (p *ANCParser) Parse(r io.Reader, name string) (*models.ForceData, error) {
	ext := strings.ToLower(filepath.Ext(name))

	switch ext {
	case ".xlsx":
		return p.parseXLSXReader(r, name)
	case ".anc":
		return p.parseANCTextReader(r, name)
	default:
		// 對於複合副檔名如 .anc.xlsx，檢查是否以 .xlsx 結尾
		if isXLSXName(name) {
			return p.parseXLSXReader(r, name)
		}
		// 默認使用文字解析（向後兼容）
		return p.parseANCTextReader(r, name)
	}
}

// initForceDataFromHeader initializes ForceData structure from ANC header.
//
// 除了每通道 capacity clamp（clampANCCapacity）之外，再加總量檢查
// （checkANCTotalAllocation）。攻擊者可放大 NumChannels 而非單通道 capacity
// 來繞過 per-channel ceiling — 例如 N ch × maxANCSampleCapacity = OOM。
func initForceDataFromHeader(header *ANCHeader) (*models.ForceData, error) {
	capacity := clampANCCapacity(header.Duration * header.PreciseRate)
	if err := checkANCTotalAllocation(capacity, len(header.ChannelNames)); err != nil {
		return nil, err
	}

	forceData := &models.ForceData{
		Time:    make([]float64, 0, capacity),
		Forces:  make(map[string][]float64),
		Headers: header.ChannelNames,
	}

	for _, channelName := range header.ChannelNames {
		forceData.Forces[channelName] = make([]float64, 0, capacity)
	}

	return forceData, nil
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

// checkANCTotalAllocation 確保「每通道 capacity × 通道數 × sizeof(float64)」
// 不超過 maxANCTotalAllocBytes。clampANCCapacity 只擋單通道上限，
// 但 NumChannels 也來自 attacker-controlled header — 64 ch × 100M = OOM。
//
// 設計取捨：reject 而非 silent truncate。Truncate 會讓 caller 拿到「不完整
// dataset 但 err == nil」，下游 sliding window / phase sync 把零值當有效資料 →
// silent miscalculation。Reject 至少讓使用者知道 header 不合理。
//
// 為什麼用 int64 算總量：int 在 32-bit 平台只有 ±2.1G，perChannel × numChannels
// 容易溢位反而變小、誤判通過。int64 ≥ ±9.2E18，2 GiB threshold 內絕對安全。
//
//nolint:err113 // dynamic error wraps sentinel + Chinese diagnostic
func checkANCTotalAllocation(perChannelCapacity, numChannels int) error {
	if perChannelCapacity < 0 || numChannels < 0 {
		return fmt.Errorf("%w: 通道容量或通道數為負（perChannel=%d, channels=%d）",
			ErrANCTotalCapacityExceeded, perChannelCapacity, numChannels)
	}

	// int64 avoids overflow when computing perCh × numCh × 8 on 32-bit ints.
	totalBytes := int64(perChannelCapacity) * int64(numChannels) * int64(float64SizeBytes)
	// L11:用 strict `>` 比較(不是 `≥`)。原本 error message 寫 `≥` 與 code 的 `>`
	// 不一致 — totalBytes == maxANCTotalAllocBytes 是「正好等於上限」合法情境,
	// 不該 reject;只有「超出上限」(strict >)才該 fail-fast。對齊 message 與 code。
	if totalBytes > int64(maxANCTotalAllocBytes) {
		return fmt.Errorf("%w: 估計總配置 %d bytes > %d bytes (perChannel=%d × channels=%d × 8)",
			ErrANCTotalCapacityExceeded, totalBytes, int64(maxANCTotalAllocBytes),
			perChannelCapacity, numChannels)
	}

	return nil
}

// parseANCDataLine parses a single data line and appends to forceData.
//
// # Field count contract
//
// 預期 `len(fields) == len(channelNames) + 1`(第 0 個是 time,後面對應每個 channel)。
// 對 fields 數量不一致時的行為:
//
//   - `len(fields) == 0`:reject(回 false),不 append。
//   - `len(fields) < len(channelNames)+1`:不足 channel 的位置 silently 補 0
//     (existing 行為 — caller 的上游 minFieldCount guard 已過濾此情境,留下的
//     都是 ParseFloatCell 失敗 cell)。
//   - `len(fields) > len(channelNames)+1`:**多出的 field 一律 truncate / 丟棄**
//     (P2-L:explicit by design)。原本因為迴圈 `range channelNames` 自動 bound 在
//     `len(channelNames)`,行為已是 truncate;此處明文釘住契約,避免未來 refactor
//     把迴圈改成 `for i, field := range fields` 之類造成 OOB 寫 or silently 漏資料。
//
// 為什麼選 truncate 而非 reject:
//   - ANC 檔案頭部 `#Channels:` 與 channel name row 是「資料 schema 宣告」,行尾
//     多餘的 column(例如某些 capture software 在 row 結尾加額外的 quality flag
//     column)算是「schema 外的補充資料」,reject 整檔過於激進,使用者無法 import。
//   - Header schema 已是 source of truth,truncate 是「沿用 declared schema」的
//     defensive default;若使用者真的需要那些 column,應該在 header 補上 channel
//     name(讓 schema 與 data 對齊)。
//   - 上游 `parseANCTextFile` 對「fields < minFieldCount」已 skip(continue),
//     對 fields > 也 skip 等於對齊;但 truncate 比 skip 損失更少資料(time + 已知
//     channel 仍進入 dataset)。
//
// 注意:若呼叫端的 channelNames slice 為空(空 ANC header),time 仍會被 append 進
// forceData.Time(對 caller 可作 EOF 偵測),但所有 channel 都會跳過。
func parseANCDataLine(fields, channelNames []string, forceData *models.ForceData) bool {
	if len(fields) == 0 {
		return false
	}

	timeValue, ok := ParseFloatCell(fields[0])
	if !ok {
		return false
	}

	forceData.Time = append(forceData.Time, timeValue)

	// 多餘 field 由 `range channelNames` 隱式 truncate(loop 不會超出
	// channelNames 邊界)。下方註解明示 design intent,避免未來 refactor 改成
	// `range fields` 形式而 silently 接受 schema-外資料。
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

// parseANCTextReader 解析文字格式的 ANC 資料（reader-based 核心）。
// name 僅用於 error context（reader 不知道自己的檔名）。
// 由 Parse 分流呼叫。
func (p *ANCParser) parseANCTextReader(r io.Reader, _ string) (*models.ForceData, error) {
	// BOM 偵測：與 phase_manifest_parser.go / csv_handler.go 對稱。Excel 匯出帶
	// UTF-8 BOM (0xEF 0xBB 0xBF) 的 ANC 文字檔，第一欄首字含 U+FEFF 會讓
	// `File_Type:` 比對失敗、channel name 多前綴 → 下游 silent miscalculation。
	bufReader := bufio.NewReaderSize(r, BufferInitKB*KilobyteMultiplier)
	if _, err := csvutil.PeekBOM(bufReader); err != nil {
		return nil, fmt.Errorf("BOM 偵測失敗: %w", err)
	}
	scanner := bufio.NewScanner(bufReader)
	scanner.Buffer(make([]byte, 0, BufferInitKB*KilobyteMultiplier), BufferMaxBytes)

	header, err := p.parseHeader(scanner)
	if err != nil {
		return nil, fmt.Errorf("解析 ANC 頭部失敗: %w", err)
	}

	forceData, err := initForceDataFromHeader(header)
	if err != nil {
		return nil, fmt.Errorf("ANC 預配記憶體檢查失敗: %w", err)
	}
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

// parseXLSXReader 解析 xlsx 格式的 ANC 資料（reader-based 核心）。
// name 僅用於 error context（reader 不知道自己的檔名）。
//
// 2. ANC 格式的 xlsx（有 11 行頭部資訊，第 9 行是通道名稱，第 12 行開始是數據）.
//
// 用 excelize.OpenReader 而非 excelize.OpenFile(path)：caller 已以
// fsperm.ReadFlags（O_NOFOLLOW）開檔，OpenReader 直接吃該 reader，避免 excelize
// 自行以 path 重開而繞過 O_NOFOLLOW symlink 保護。OpenReader 回傳的 *excelize.File
// 仍須 Close（持有解壓暫存資源），保留原本的 defer Close。
//
//nolint:err113 // dynamic errors with Chinese messages; Excel is proper noun
func (p *ANCParser) parseXLSXReader(r io.Reader, name string) (*models.ForceData, error) {
	// 從 reader 開啟 Excel；O_NOFOLLOW 保護由 caller 的 OpenFile 提供，此處不再以 path 重開。
	excelFile, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("無法開啟 Excel 檔案 %s: %w", name, err)
	}

	defer func() {
		if closeErr := excelFile.Close(); closeErr != nil {
			_ = closeErr // Ignore close error
		}
	}()

	// 獲取第一個工作表名稱
	sheetName := excelFile.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("Excel 檔案沒有工作表: %s", name)
	}

	// 讀取所有行
	rows, err := excelFile.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("讀取 Excel 工作表失敗: %w", err)
	}

	if len(rows) < 2 {
		return nil, fmt.Errorf("Excel 檔案數據不足（需要至少標題行和一行數據）: %s", name)
	}

	// 檢測檔案格式：ANC 格式的第一行通常以 "File_Type:" 開頭
	isANCFormat := false

	if len(rows) > 0 && len(rows[0]) > 0 {
		firstCell := strings.TrimSpace(rows[0][0])
		isANCFormat = strings.HasPrefix(firstCell, "File_Type:")
	}

	if isANCFormat {
		return p.parseANCFormatXLSX(rows, name)
	}

	return p.parseSimpleXLSX(rows, name)
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
//
// 與 修的 extractValue 同 class 的 sibling bug —
// 原本用 `strings.Split(part, ":")[1]` 抓 value，value 內若含 `:`(例如 timestamp
// `File_Type:08:30:45`)會被截到第一個 `:` 之前只剩 `"08"`。改用 SplitN(":", 2)
// 強制只切第一次,且驗證 len == 2 避免 value 為空時拿到單元素 slice 仍 index 1
// panic。修法行為與 extractValue 一致:value 內所有 `:` 都保留。
func handleFileTypeLine(_ *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "File_Type:") {
		return
	}

	fileParts := strings.Split(content, "\t")
	for _, part := range fileParts {
		if !strings.Contains(part, "File_Type:") {
			continue
		}

		// SplitN(":", 2):強制只切第一次,rest 整段保留 — value 內含 `:` 不被誤切。
		// len 驗證:若 part 不含 `:`（理論上 Contains 已過濾掉但保險起見），SplitN
		// 會回單元素 slice，跳過不寫 header。
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			return
		}
		header.FileType = strings.TrimSpace(kv[1])

		return
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

// splitTabCells 將 content 以 tab 切欄並 trim,過濾掉空欄位。
// 取代 strings.Fields:Fields 切所有 whitespace,把含空格的 channel name(例如
// "Left Quad")拆成兩列 → header.NumChannels 對不齊資料欄,silent miscalc。
// ANC header 欄位之間是 tab 而非 space,用 Split("\t") 正確保留 cell 邊界。
func splitTabCells(content string) []string {
	rawCells := strings.Split(content, "\t")
	cells := make([]string, 0, len(rawCells))
	for _, cell := range rawCells {
		trimmed := strings.TrimSpace(cell)
		if trimmed == "" {
			continue
		}
		cells = append(cells, trimmed)
	}
	return cells
}

// handleChannelNamesLine 處理通道名稱行.
//
// 改用 tab 切欄,不能用 strings.Fields。Fields 切所有 whitespace,
// channel name 含空格(例如 "Left Quad")會被拆兩列,downstream 通道映射整個錯位
// → silent miscalc。Tab 是 ANC header 的欄位分隔符,用 Split + Trim 才能保留
// cell 邊界。
func handleChannelNamesLine(_ *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "Name") {
		return
	}

	cells := splitTabCells(content)
	if len(cells) > 1 {
		header.ChannelNames = cells[1:] // 跳過 "Name"
	}
}

// handleChannelRatesLine 處理採樣率行.
//
// 與 handleChannelNamesLine 對稱,以 tab 切欄。Rate 行雖然 cell 通常是
// 整數,但與 ChannelNames 共用同一 tab 對齊,用 Fields 可能因 channel 名稱含空格
// 造成 ChannelNames / Rates 長度不一致;統一改 tab-split 後不再因「上一行欄數」
// 而前後不對齊。
func handleChannelRatesLine(_ *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "Rate") {
		return
	}

	cells := splitTabCells(content)
	if len(cells) > 1 {
		header.ChannelRates = make([]int, 0, len(cells)-1)

		for _, f := range cells[1:] {
			rate, _ := strconv.Atoi(f) //nolint:errcheck // optional header field
			header.ChannelRates = append(header.ChannelRates, rate)
		}
	}
}

// handleChannelRangesLine 處理範圍行.
//
// 與 handleChannelNamesLine / handleChannelRatesLine 對稱,以 tab 切欄。
func handleChannelRangesLine(_ *ANCParser, header *ANCHeader, content string) {
	if !strings.Contains(content, "Range") {
		return
	}

	cells := splitTabCells(content)
	if len(cells) > 1 {
		header.ChannelRanges = make([]int, 0, len(cells)-1)

		for _, f := range cells[1:] {
			rang, _ := strconv.Atoi(f) //nolint:errcheck // optional header field
			header.ChannelRanges = append(header.ChannelRanges, rang)
		}
	}
}

// parseHeader 解析 ANC 檔案頭部。
//
// 解析完成後驗證必要欄位（Duration、PreciseRate、ChannelNames）— 缺一即 fail-fast，
// 避免下游用 zero value 算出 silently wrong 的 sample rate / channel mapping。
//
// 為什麼這些欄位是 "必要"：
//   - PreciseRate：用於 sliding window / time alignment，0 會造成 div-by-zero
//   - ChannelNames：沒名稱就無法 map data column → silently 全部資料丟給 0 號 channel
//   - Duration：clampANCCapacity 用它預估 slice 容量，0 退到 default 是 best-effort
//     但搭配 PreciseRate 異常時暗示整個 header 壞掉，一併拒絕較安全
//
//nolint:unparam // error return kept for future extensibility and API consistency
func (p *ANCParser) parseHeader(scanner *bufio.Scanner) (*ANCHeader, error) {
	header := &ANCHeader{}
	// fallbackLineNum 是「first-content-relative 1-based line number」,只在「無顯式行號」
	// 的 ANC 變體用作 dispatch key。第一個 content row 視為 line 1,之後每一行(包括
	// 該 row 之後的 blank lines)都遞增 — ANC 標準佈局中第 5-8 行是 blank,但 line 9
	// 必為 ChannelNames,fallback 必須對齊這個結構。
	//
	// `lineNum++` 在 blank-skip 之前推進,leading blank line 會偏移 1
	// → File_Type 被當 Board_Type 處理,handler map 整體錯位 silent miscalc。
	// 修法雙重保險:
	//   (1) Leading blank lines(first content 之前)→ 不推進 fallbackLineNum
	//   (2) 若 parts[0] 為 ANC 顯式行號 → 用 parsedLine 當 dispatch(最可靠,直接跳過
	//       iteration counter,即使中間有任意 blank 也不會偏移)
	//   (3) 否則用 fallbackLineNum(用於無行號變體,保留「blank 行算進結構」契約)
	fallbackLineNum := 0
	sawContent := false

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, "\t")
		isBlank := strings.TrimSpace(line) == ""

		// Leading blank(尚未見任何 content)→ skip 不推進,避免 偏移。
		// 後續 blank(已見 content)→ 仍要推進,維持 ANC 標準佈局中 line 5-8 blank
		// 不影響 line 9 = ChannelNames 的 fallback 對齊。
		if isBlank && !sawContent {
			continue
		}
		if isBlank {
			fallbackLineNum++
			continue
		}
		sawContent = true
		fallbackLineNum++

		// 決定 dispatch line number 與 content payload。
		// 拆 tab 後判斷 parts[0] 是否為 line-number prefix:有顯式行號 → 用 parsedLine
		// 當 dispatch key,完全免疫 iteration counter 偏移;無 prefix → 整行視為 content
		// 並 fallback 到 first-content-relative 計數。
		var (
			dispatchLine int
			content      string
		)
		if parsedLine, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && len(parts) >= 2 {
			dispatchLine = parsedLine
			content = strings.Join(parts[1:], "\t")
		} else {
			dispatchLine = fallbackLineNum
			content = line
		}

		// 超出頭部範圍(第 12 行起為資料),停止 header 解析。
		if dispatchLine >= ANCDataStartLine {
			break
		}

		// 使用處理器映射表處理對應行
		if handler, exists := lineHandlers[dispatchLine]; exists {
			handler(p, header, content)
		}
	}

	// Empty file / whitespace-only：graceful 空 header，與既有 caller 契約一致
	// （test fixture "empty file" 依賴此行為，後續 data scan 自然得到空 dataset）。
	if !sawContent {
		return header, nil
	}

	if err := validateANCHeader(header); err != nil {
		return nil, err
	}

	return header, nil
}

// validateANCHeader 確保 parseHeader 解出的關鍵欄位合法。
//
// 嚴格度設計（不過度嚴格以免破壞既有 fixture）：
//   - ChannelNames 必須非空 — 這是 silent miscalculation 的核心，沒名稱
//     就無法 map data column，下游會把所有資料丟給 0 號 channel
//   - Duration / PreciseRate = 0 視為「未提供」OK（既有 extractValue 對
//     tab-separated 格式抓不到 value，所有現存 fixture 都是 0；強制要求
//     會破壞 backward compat，已列 P1 修 extractValue）
//   - 但若 Duration/PreciseRate 為**負值或 NaN/Inf**，則一律 reject —
//     這是真正的「data poisoning」 攻擊面，不會誤殺正常 fixture
func validateANCHeader(header *ANCHeader) error {
	if len(header.ChannelNames) == 0 {
		return fmt.Errorf("%w: %w", ErrANCIncompleteHeader, ErrANCMissingChannels)
	}
	if header.PreciseRate < 0 || math.IsNaN(header.PreciseRate) || math.IsInf(header.PreciseRate, 0) {
		return fmt.Errorf("%w: %w (got %v)",
			ErrANCIncompleteHeader, ErrANCMissingRate, header.PreciseRate)
	}
	if header.Duration < 0 || math.IsNaN(header.Duration) || math.IsInf(header.Duration, 0) {
		return fmt.Errorf("%w: %w (got %v)",
			ErrANCIncompleteHeader, ErrANCMissingDuration, header.Duration)
	}
	return nil
}

// extractValue 從字符串中提取指定標籤的值。
//
// 支援兩種 ANC header 排版：
//  1. 同 cell 內：`Label:value`（label 與 value 之間沒有 tab）。
//  2. 跨 cell：`Label:` `\t` `value`（label 結尾即冒號，value 在下個 tab-cell）。
//
// 必須保留 value 中的 `:` 字元（例如 timestamp "08:30:45" 或
// "AMTI:OR6-5" 板型名稱）。原本用 `strings.Split(part, ":")` 取 valueParts[1]
// 會把 value 內第一個 `:` 之後的內容截斷 — 例如 "Trial_Name:08:30:45" 只回
// "08"，silently 把 30:45 丟掉。改用 SplitN(":", 2) 強制只切第一次，rest 保留。
//
// label 比對改 `strings.HasPrefix` 取代 `strings.Contains`，加 word-boundary。
// 原本 substring match 在 cell 開頭含 label 時能正常抓 value，但若同一 cell 出現
// `"Foo_Trial#:5"` 而 caller 找 `"Trial#:"`，Contains 會誤中該 cell 把 `5` 當值
// 回傳。Prefix match 強制 label 必須出現在 trimmed cell 的開頭，避免 substring
// collision。跨 cell 變體（同一 cell 只含 `"Label:"`、value 在下個 tab cell）
// 仍走原本 i+1 < len(parts) 分支，行為不變。同 cell 帶 value 改直接切 prefix
// 取 rest，不再走 SplitN(":", 2)，value 內含 `:` 同樣不會被誤切。
//
//nolint:revive // unused-receiver: keep consistent API
func (p *ANCParser) extractValue(content, label string) string {
	parts := strings.Split(content, "\t")
	for i, part := range parts {
		trimmedPart := strings.TrimSpace(part)
		if !strings.HasPrefix(trimmedPart, label) {
			continue
		}

		// label 已是 prefix，後面就是 value（可能為空，代表跨 cell）。直接切掉
		// 前綴保留 value 全部內容，value 內若含 `:` 不會被誤切。
		rest := strings.TrimSpace(trimmedPart[len(label):])
		if rest != "" {
			return rest
		}

		// 跨 cell：value 在下個 tab-cell。注意 ANC header 一行有多個
		// "Label: \t value" pair，下個 part 是 value 而非另一個 label，
		// 因此 i+1 < len(parts) 就直接拿。
		if i+1 < len(parts) {
			return strings.TrimSpace(parts[i+1])
		}
	}

	return ""
}

// GetANCDataInTimeRange returns force data within the specified time range.
// Uses integer milliseconds for comparison to avoid floating point precision issues.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func GetANCDataInTimeRange(data *models.ForceData, startTime, endTime float64) (*models.ForceData, error) {
	// validator path 已有此 guard，extractor path 需對稱保護避免 data.Time
	// 索引存取造成 nil-deref panic。空 Time slice 也視為空資料一併 reject。
	if data == nil || len(data.Time) == 0 {
		return nil, fmt.Errorf("力板數據為空: %w", ErrNilData)
	}

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
