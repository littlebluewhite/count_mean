package parsers

import (
	"fmt"
	"io"
	"math"
	"strings"

	calcerrors "count_mean/internal/errors"
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

// validateUniqueChannelNames 檢查具名通道(排除時間欄與 "" 佔位欄)是否有重複名稱。
//
// 重複名稱會在 Channels map 折疊成同一 key、每行 append 兩次 → 該通道長度變兩倍,
// 在 validateEMGDataIntegrity 表現為語意不清的「長度不一致」。此處先以位置 headers
// (仍保有各自獨立的重複項)偵測,回傳明確訊息。不 rename 通道,以免破壞下游
// BuildChannelMap 按生理名取值。
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func validateUniqueChannelNames(headers []string) error {
	seen := make(map[string]struct{}, len(headers))

	for _, h := range headers[1:] {
		if h == "" {
			continue
		}

		if _, dup := seen[h]; dup {
			return fmt.Errorf("EMG 檔案標題含重複的通道名稱：%q", h)
		}

		seen[h] = struct{}{}
	}

	return nil
}

// initEMGData initializes EMGData structure with channel slices.
//
// headers[0] 是時間欄一律排除;其餘可能含位置佔位用的 "" 空欄(見 parseHeaders)——
// 過濾掉,只有具名欄成為 publish 的 Headers/Channels。位置 headers 仍由 parseEMGDataRow
// 持有以維持 record[j] 對齊(對齊 motion_parser.go initializeMotionData 範式)。
func initEMGData(headers []string, capacity int) *models.PhaseSyncEMGData {
	dataHeaders := make([]string, 0, len(headers))

	for _, h := range headers[1:] {
		if h == "" {
			continue
		}

		dataHeaders = append(dataHeaders, h)
	}

	emgData := &models.PhaseSyncEMGData{
		Time:     make([]float64, 0, capacity),
		Channels: make(map[string][]float64),
		Headers:  dataHeaders,
	}

	for _, header := range dataHeaders {
		emgData.Channels[header] = make([]float64, 0, capacity)
	}

	return emgData
}

// parseEMGDataRow parses a single EMG data row.
// Returns false if the row should be skipped.
//
// **三 parser(EMG / Motion / ANC)空欄策略契約**(Unit D 對齊後文件化,此處不改行為):
//   - 時間 / index 欄:走 ParseTimeCell(額外拒 NaN/Inf)。失敗即「整列 skip」—
//     時間軸的空 / NaN 會 poison 下游 sliding-window 與 monotonicity 比較,故 fail-row。
//   - 通道值欄:走 ParseFloatCell 且**刻意忽略 ok**(`value, _`),空 / 不可解析 cell
//     fallback 成 0、列仍保留。理由:科學儀器輸出常有 trailing tab / 個別 sensor
//     dropout,不該因單一通道空值丟整列;字面 "NaN" 則照 strconv 解析為 NaN value,
//     由下游(muscle_ratio / phase_sync)決定輸出空白 cell(見 ParseFloatCell doc)。
//
// 分歧由來:三 parser 早期各自手寫 strconv.ParseFloat,對 trailing 空白 / 空欄
// 的容忍度不一(ANC 曾因 `strconv.ParseFloat(" 0.001 \t..")` error 整列被 skip)。
// Unit D 統一收斂到 parse_helpers 的 ParseTimeCell / ParseFloatCell 兩 primitive,
// 三 parser 此後共用同一空欄語意;需嚴格區分 missing 與字面 NaN 的新 caller 改用
// ParseFloatCellWithMissing + FormatFloatCell(見 parse_helpers.go)。
func parseEMGDataRow(record, headers []string, emgData *models.PhaseSyncEMGData) bool {
	if len(record) < len(headers) {
		return false
	}

	timeValue, ok := ParseTimeCell(record[0])
	if !ok {
		return false
	}

	emgData.Time = append(emgData.Time, timeValue)

	for j := 1; j < len(headers) && j < len(record); j++ {
		if headers[j] == "" {
			// 佔位空欄(spacer):位置仍前進以維持 record[j] 對齊,但不收進任何通道。
			continue
		}

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

	if err := validateUniqueChannelNames(headers); err != nil {
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
//
// **位置對齊不可破壞**(對齊 motion_parser.go buildUniqueHeaders 範式):parseEMGDataRow
// 按 record[j] 位置餵值,headers[j] 必須對應 CSV 第 j 欄。若某欄為空(中間的 spacer 欄),
// 不可 compact 抽掉 —— 否則後面的具名欄會位移、配到 spacer 欄的值 → 靜默資料錯位。
// 改以 "" 佔位保留位置,由 initEMGData / parseEMGDataRow 跳過不收進通道。尾端純佔位欄
// 則裁掉(後面沒有具名欄,不需保留位置;留著會撐大 len(headers) 害 parseEMGDataRow 的
// len(record) < len(headers) 保護把正常 ragged data row 誤判為欄位不足而整列跳過)。
func (p *EMGParser) parseHeaders(headerRow []string) []string { //nolint:revive // keep consistent API
	headers := make([]string, 0, len(headerRow))

	for _, h := range headerRow {
		headers = append(headers, strings.TrimSpace(h))
	}

	for len(headers) > 0 && headers[len(headers)-1] == "" {
		headers = headers[:len(headers)-1]
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
// **前置條件**:caller 必須先過 ValidateEMGData。本函式用 util.ArrayMean/ArrayMax,
// 對 NaN/Inf 不安全(會 silently poison 整個通道、把字面 NaN 寫進輸出)。非有限值的
// fail-fast 校驗在 ValidateEMGData;生產唯一 caller(calculator.CalculateStatistics)
// 已先驗。直接呼叫者務必自行先過 ValidateEMGData。
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

	if err := ValidateTimeSeries(data.Time, data.Channels, TimeSeriesLabels{
		DataName:     "EMG",
		SeriesName:   "時間序列",
		SeriesPos:    "索引",
		ChannelLabel: "通道",
	}); err != nil {
		return err
	}

	return validateEMGChannelValues(data.Channels)
}

// validateEMGChannelValues 掃描 EMG 通道值,對任何 NaN/Inf fail-fast。
//
// 為什麼在此 fail-fast 而非沿用「通道 NaN→輸出空白」:phase_sync 統計路徑
// (CalculateStatistics → CalculateEMGStatistics)用 util.ArrayMean/ArrayMax,
// 對 NaN 不安全 — NaN 會 poison 整個通道的 mean/max,把字面 NaN 寫進 Output2
// (range_normalizer.go 已文檔化此 util 限制,不可改 util 語義)。此 validator
// 僅供 stats 路徑(唯一生產 caller = calculator.CalculateStatistics);muscle_ratio
// 等刻意容忍 NaN 的路徑不經此 gate,不受影響。
//
// 鏡像 calculator.validateChannelValues 的單遍掃描;reuse 同一組 sentinel
// 讓上層 errors.Is 跨 maxmean 與 stats 路徑一致。
func validateEMGChannelValues(channels map[string][]float64) error {
	for name, samples := range channels {
		for i, v := range samples {
			if math.IsNaN(v) {
				return fmt.Errorf("通道 %q 第 %d 個取樣含 NaN: %w", name, i+1, calcerrors.ErrNaNInChannel)
			}

			if math.IsInf(v, 0) {
				return fmt.Errorf("通道 %q 第 %d 個取樣含 Inf: %w", name, i+1, calcerrors.ErrInfInChannel)
			}
		}
	}

	return nil
}
