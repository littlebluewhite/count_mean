package calculator

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security/fsperm"
)

// Report formatting constants.
const reportSeparatorWidth = 52

// Statistics validation errors.
var (
	// ErrNegativeStartTime indicates a negative start time.
	ErrNegativeStartTime = errors.New("start time cannot be negative")
	// ErrNegativeEndTime indicates a negative end time.
	ErrNegativeEndTime = errors.New("end time cannot be negative")
	// ErrStartTimeNotBeforeEnd indicates start time is not before end time.
	ErrStartTimeNotBeforeEnd = errors.New("start time must be less than end time")
	// ErrEmptySubject indicates an empty subject name.
	ErrEmptySubject = errors.New("subject name cannot be empty")
)

// EMGStatisticsCalculator EMG 統計計算器.
type EMGStatisticsCalculator struct {
	precision int // 小數位數精度
}

// NewEMGStatisticsCalculator 創建新的 EMG 統計計算器.
func NewEMGStatisticsCalculator(precision int) *EMGStatisticsCalculator {
	if precision <= 0 {
		precision = 6
	}

	return &EMGStatisticsCalculator{
		precision: precision,
	}
}

// CalculateStatistics 計算 EMG 數據的統計信息.
func (*EMGStatisticsCalculator) CalculateStatistics(
	emgData *models.PhaseSyncEMGData,
	params StatisticsParams,
) (*models.EMGStatistics, error) {
	// 驗證輸入數據
	if err := parsers.ValidateEMGData(emgData); err != nil {
		return nil, fmt.Errorf("EMG 數據驗證失敗: %w", err)
	}

	// 計算平均值和最大值
	means, maxes := parsers.CalculateEMGStatistics(emgData)

	// 創建統計結果
	stats := &models.EMGStatistics{
		Subject:      params.Subject,
		StartPhase:   params.StartPhase,
		StartTime:    params.StartTime,
		EndPhase:     params.EndPhase,
		EndTime:      params.EndTime,
		ChannelNames: emgData.Headers,
		ChannelMeans: means,
		ChannelMaxes: maxes,
	}

	return stats, nil
}

// buildUniformRow creates a row with the same value repeated for all channels.
func buildUniformRow(label, value string, channelCount int) []string {
	row := make([]string, 0, channelCount+1)
	row = append(row, label)

	for i := 0; i < channelCount; i++ {
		row = append(row, value)
	}

	return row
}

// buildChannelValueRow creates a row with values from a channel map.
func buildChannelValueRow(
	label string,
	channelNames []string,
	values map[string]float64,
	calc *EMGStatisticsCalculator,
) []string {
	row := make([]string, 0, len(channelNames)+1)
	row = append(row, label)

	for _, name := range channelNames {
		row = append(row, calc.formatFloat(values[name]))
	}

	return row
}

// writeCSVWithBOM creates a file with UTF-8 BOM and returns a CSV writer.
//
// fsperm.WriteFlags 含 O_NOFOLLOW (unix) 拒絕 symlink；攻擊者無法在 OutputDir
// 植入 symlink 把寫入導向 /etc/passwd 等敏感檔。
func writeCSVWithBOM(outputPath string) (*os.File, *csv.Writer, error) {
	//nolint:gosec // G304: outputPath is validated by caller
	file, err := os.OpenFile(outputPath, fsperm.WriteFlags, fsperm.FilePerm)
	if err != nil {
		return nil, nil, fmt.Errorf("無法創建輸出檔案 %s: %w", outputPath, err)
	}

	if err := csvutil.WriteBOM(file); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}

		return nil, nil, fmt.Errorf("無法寫入 BOM: %w", err)
	}

	return file, csv.NewWriter(file), nil
}

// ExportToCSV 將統計結果導出為 CSV 檔案.
// Flush 與 Close 的錯誤都會被回傳：磁碟滿等狀況下 csv.Writer 把錯誤延後到 Flush 才丟，
// 若僅在 defer 中忽略，caller 會看到 nil 但檔案內容不完整。
func (calc *EMGStatisticsCalculator) ExportToCSV(
	stats *models.EMGStatistics,
	outputPath string,
) (err error) {
	file, writer, err := writeCSVWithBOM(outputPath)
	if err != nil {
		return err
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("關閉檔案失敗: %w", closeErr)
		}
	}()

	channelCount := len(stats.ChannelNames)

	rows := []struct {
		row      []string
		errorMsg string
	}{
		{csvutil.SanitizeHeaderRow(append([]string{""}, stats.ChannelNames...)), "寫入標題行失敗"},
		{buildUniformRow("開始分期點", string(stats.StartPhase), channelCount), "寫入開始分期點失敗"},
		{buildUniformRow("開始時間", calc.formatFloat(stats.StartTime), channelCount), "寫入開始時間失敗"},
		{buildUniformRow("結束分期點", string(stats.EndPhase), channelCount), "寫入結束分期點失敗"},
		{buildUniformRow("結束時間", calc.formatFloat(stats.EndTime), channelCount), "寫入結束時間失敗"},
		{buildUniformRow("時間差值", calc.formatFloat(stats.EndTime-stats.StartTime), channelCount), "寫入時間差值失敗"},
		{buildChannelValueRow("平均值", stats.ChannelNames, stats.ChannelMeans, calc), "寫入平均值失敗"},
		{buildChannelValueRow("最大值", stats.ChannelNames, stats.ChannelMaxes, calc), "寫入最大值失敗"},
	}

	for _, r := range rows {
		if err := writer.Write(r.row); err != nil {
			return fmt.Errorf("%s: %w", r.errorMsg, err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("CSV flush 失敗: %w", err)
	}

	return nil
}

// formatFloat 格式化浮點數.
func (calc *EMGStatisticsCalculator) formatFloat(value float64) string {
	format := fmt.Sprintf("%%.%df", calc.precision)
	return fmt.Sprintf(format, value)
}

// GenerateOutputFileName 生成輸出檔案名.
func GenerateOutputFileName(subject string, startPhase, endPhase models.PhasePoint) string {
	safeSubject := SanitizeFileName(subject)

	return fmt.Sprintf("%s_%s-%s_statistics.csv", safeSubject, startPhase, endPhase)
}

// SanitizeFileName 把可能造成路徑穿越或檔名衝突的字元替換為底線。
//
// 補強：除了原本字元替換外，加：
//   - 空字串 fallback 為 "untitled"
//   - 超長 (> 200 chars，預留 prefix/suffix 空間) 截斷
//   - Windows reserved name (CON / PRN / COMx / LPTx) 加 "_safe" 後綴
//
// 補強：在路徑分隔符替換之外，**移除**下列 rune 以防止視覺欺騙 /
// 終端機 escape / 檔系統 reject：
//   - ASCII control chars (除 `\t` 仍 strip — filename 不應含 tab)
//   - Unicode `Cc` (control, 含 U+0000-U+001F / U+007F-U+009F)
//   - Unicode `Cf` (format, 含 RTL override U+202E、bidi controls U+2066-U+2069、
//     ZWSP/ZWJ U+200B-U+200D、BOM U+FEFF 等)
//   - Unicode `Cs` (surrogate; UTF-8 內若出現代表 mis-encoded UTF-16)
//
// 直接 drop 而非替換為底線，因為這些 rune 沒有合理的「看得到的等價字元」，
// 替換成 `_` 會在檔名中製造無意義底線；drop 才是預期 UX。
func SanitizeFileName(name string) string {
	replacements := map[rune]rune{
		'/':  '_',
		'\\': '_',
		':':  '_',
		'*':  '_',
		'?':  '_',
		'"':  '_',
		'<':  '_',
		'>':  '_',
		'|':  '_',
		' ':  '_',
	}

	result := make([]rune, 0, len(name))

	for _, ch := range name {
		if isUnsafeFilenameRune(ch) {
			// drop 控制 / format / surrogate / NUL — 不留底線佔位
			continue
		}
		if replacement, ok := replacements[ch]; ok {
			result = append(result, replacement)
		} else {
			result = append(result, ch)
		}
	}

	cleaned := string(result)
	if cleaned == "" {
		return "untitled"
	}

	// Windows reserved base name → 加 _safe 後綴避開。
	//
	// Windows reserved name 規則是「stem (first dot 前的 segment)」匹配,
	// 不是「整個 filename」匹配。"CON.csv" / "CON.tar.gz" / "PRN.log" 在 Windows
	// 上仍會被當成 reserved device file 而 reject (或更糟,寫入打開實體裝置)。
	// 過去用 `strings.ToUpper(cleaned)` 比對整段,只擋住 "CON" alone,
	// 把 "CON.csv" 放過 — 在 macOS/Linux 開發跑 PASS 但 Windows 部署炸開。
	// 改成 strings.SplitN 取 stem,涵蓋多副檔名 (.tar.gz) 情境。
	//
	// leading-dot bypass 加固。past stem 取法 `SplitN(".CON.csv", ".", 2)[0]`
	// 會回 "" — 空 stem 跑進 switch 永遠不命中任何 reserved name,讓 ".CON.csv" /
	// ".NUL.txt" 之類 leading-dot 變形直接逃過 reserved-name 守門。Windows NT
	// path parser 在比對 reserved name table 之前會 strip leading dot,所以
	// ".CON.csv" 在 Windows 上仍會被當成 device。修法:用 strings.TrimLeft 把
	// leading dot 先剝掉再取 stem 比對,保留原 cleaned 作為 splice 基準以保留
	// dot prefix 在輸出 (".CON.csv" → ".CON_safe.csv")。
	const reservedSuffix = "_safe"
	dotPrefixLen := len(cleaned) - len(strings.TrimLeft(cleaned, "."))
	afterDots := cleaned[dotPrefixLen:]
	stemAfterDots := strings.SplitN(afterDots, ".", 2)[0]
	stemUpper := strings.ToUpper(stemAfterDots)
	switch stemUpper {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		// 在 stem 末尾插入 _safe,保留原本副檔名 (含多副檔名) 與 leading dot:
		// "CON.csv"    → "CON_safe.csv"
		// "CON.tar.gz" → "CON_safe.tar.gz"
		// ".CON.csv"   → ".CON_safe.csv" (P2-C:保留 dot prefix,僅插入 _safe)
		// "..NUL.txt"  → "..NUL_safe.txt"
		stemEnd := dotPrefixLen + len(stemAfterDots)
		if rest := cleaned[stemEnd:]; rest != "" {
			cleaned = cleaned[:stemEnd] + reservedSuffix + rest
		} else {
			cleaned += reservedSuffix
		}
	}

	// 截斷過長 filename — 保留前 200 個 **byte**（預留 prefix / extension / index 空間）。
	//
	// 用 byte truncation 可能切到 multi-byte UTF-8 rune 中間,產生
	// invalid UTF-8 sequence (例如 0xE4 0xB8 0x80 = "一",切在 byte 1 後得到
	// 0xE4 lone 是 invalid)。Windows/macOS file API 對 invalid UTF-8 行為各異
	// (FAT32 直接 reject、APFS 容忍但 ls 顯示亂碼)。改成 rune-aware:
	// scan 到 maxLen 邊界時退回到最後一個 valid rune boundary,確保截斷結果仍是
	// 完整 UTF-8 sequence。
	//
	// 為什麼仍用 byte 限額 (200) 而非 rune 數: filesystem 規格 (例 ext4 NAME_MAX=255)
	// 是 byte 上限,不是 rune 數。中文 filename 每 rune 3 bytes,200 bytes ≈ 66 中文字,
	// 對使用者命名足夠。
	const maxLen = 200
	if len(cleaned) > maxLen {
		cleaned = truncateAtRuneBoundary(cleaned, maxLen)
	}

	return cleaned
}

// truncateAtRuneBoundary 把 s 截斷到 maxBytes 以內,且保證不會切到 UTF-8 multi-byte
// rune 中間。回傳的 string 長度 <= maxBytes 且必為 valid UTF-8。
//
// 演算法: 從 maxBytes 位置往前掃,找到第一個 utf8.RuneStart byte (高位 byte 結構為
// 0xxxxxxx (ASCII)、110xxxxx (2-byte leader)、1110xxxx (3-byte leader)、
// 11110xxx (4-byte leader);continuation byte 為 10xxxxxx 不會被 RuneStart 認可)。
// 用 DecodeRuneInString 邊掃邊累加 byte 長度,直到下一個 rune 會超過 maxBytes 即停。
func truncateAtRuneBoundary(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}

	// 遍歷 s 的 rune,累積 byte 長度。下一個 rune 加進來會超過 maxBytes 即停。
	totalBytes := 0
	for _, r := range s {
		runeBytes := utf8.RuneLen(r)
		// utf8.RuneLen 對 invalid rune 回 -1,把它當 1 byte 處理 (與 byte truncation 行為對齊)。
		if runeBytes < 0 {
			runeBytes = 1
		}
		if totalBytes+runeBytes > maxBytes {
			break
		}
		totalBytes += runeBytes
	}
	return s[:totalBytes]
}

// isUnsafeFilenameRune reports whether r should be dropped from a sanitized
// filename. 涵蓋 ASCII control / Unicode Cc(control) / Cf(format) / Cs(surrogate)。
//
// 為什麼用 unicode.In(r, Cf, Cs) 而非逐項列表：Cf 已涵蓋 U+202E (RTL OVERRIDE)、
// U+200B-U+200D (ZWSP/ZWJ)、U+2060 (WJ)、U+2066-U+2069 (bidi isolation)、
// U+FEFF (BOM) 等所有「視覺欺騙」vector，毋須維護白名單。Cc 涵蓋 U+0000-U+001F
// 以及 U+007F-U+009F，後者在歷史終端機上會被 interpret 為 escape，filename 不應
// 出現。Cs 為 UTF-16 surrogate，UTF-8 內出現即為 mis-encoded，全 drop。
func isUnsafeFilenameRune(r rune) bool {
	if r == 0 {
		return true
	}
	// unicode.IsControl 涵蓋 \p{Cc}（C0/C1 控制碼，含 \r \n \t \x07 NUL 等）。
	// 與 csvutil sanitize 不同的是，filename 也不容許 \t — tab 在多數檔系統呈現
	// 為灰色塊或空白，會干擾 listing；統一 drop。
	if unicode.IsControl(r) {
		return true
	}
	// \p{Cf}（format）：RTL override / ZWSP / BOM / bidi controls。
	// \p{Cs}（surrogate）：mis-encoded UTF-16。
	return unicode.In(r, unicode.Cf, unicode.Cs)
}

// ValidateStatisticsParams 驗證統計參數.
func ValidateStatisticsParams(params *StatisticsParams) error {
	if params.StartTime < 0 {
		return fmt.Errorf("開始時間不能為負數 (%.3f): %w", params.StartTime, ErrNegativeStartTime)
	}

	if params.EndTime < 0 {
		return fmt.Errorf("結束時間不能為負數 (%.3f): %w", params.EndTime, ErrNegativeEndTime)
	}

	if params.StartTime >= params.EndTime {
		return fmt.Errorf("開始時間 (%.3f) 必須小於結束時間 (%.3f): %w",
			params.StartTime, params.EndTime, ErrStartTimeNotBeforeEnd)
	}

	if params.Subject == "" {
		return ErrEmptySubject
	}

	return nil
}

// StatisticsParams 統計參數.
type StatisticsParams struct {
	Subject    string
	StartPhase models.PhasePoint
	StartTime  float64
	EndPhase   models.PhasePoint
	EndTime    float64
}

// FormatStatisticsReport 格式化統計報告.
// 使用 strings.Builder 取代 `report += fmt.Sprintf(...)`，避免在迴圈內每次 reallocate。
func FormatStatisticsReport(stats *models.EMGStatistics) string {
	var sb strings.Builder
	sb.Grow(256 + len(stats.ChannelNames)*64)

	sb.WriteString("EMG 統計分析報告\n")
	sb.WriteString("================\n")
	fmt.Fprintf(&sb, "主題: %s\n", stats.Subject)
	fmt.Fprintf(&sb, "分析區間: %s (%.3fs) → %s (%.3fs)\n",
		stats.StartPhase, stats.StartTime,
		stats.EndPhase, stats.EndTime)
	fmt.Fprintf(&sb, "持續時間: %.3f 秒\n", stats.EndTime-stats.StartTime)
	fmt.Fprintf(&sb, "通道數量: %d\n\n", len(stats.ChannelNames))

	sb.WriteString("各通道統計結果:\n")
	fmt.Fprintf(&sb, "%-20s %15s %15s\n", "通道名稱", "平均值", "最大值")
	sb.WriteString(strings.Repeat("-", reportSeparatorWidth))
	sb.WriteByte('\n')

	for _, channelName := range stats.ChannelNames {
		mean := stats.ChannelMeans[channelName]
		maxVal := stats.ChannelMaxes[channelName]
		fmt.Fprintf(&sb, "%-20s %15.6f %15.6f\n", channelName, mean, maxVal)
	}

	return sb.String()
}
