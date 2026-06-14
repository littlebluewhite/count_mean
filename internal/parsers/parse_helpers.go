package parsers

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"count_mean/util"
)

// MaxReasonableMotionIndex 是 motion-index(D / O)欄位的合理上限。
//
// 1e9 frame ≈ 250 Hz 取樣率下 46 天連續錄製 — 涵蓋任何合理的動作 capture
// session,超過此值視為惡意 / 損毀輸入。
//
// **攻擊向量(V5):** `D=1e15` + `O=0` sentinel 組合下,validateMotionIndexOrder
// 把 O=0 當「未提供」跳過,只有 D 一個 motion-index 點。單元素 list monotonicity
// 永遠 pass,下游 phase_sync sliding window / chart axis 拿到 1e15 frame index
// 直接 OOM / hang(DoS)。parseInt 在 motion-index path 加 cap 攔住此向量。
const MaxReasonableMotionIndex = 1_000_000_000

// ParseFloatCell trims whitespace and parses a CSV cell as float64.
// Returns (0, false) when the cell is empty, whitespace-only, or unparseable —
// callers that want "best-effort zero fallback" can ignore ok and just use the value.
//
// **NaN/Inf 行為**：strconv.ParseFloat 對 "NaN" / "Inf" 字面回 (value, nil)，
// 本函式也照樣 return (NaN/Inf, true)。這是**刻意設計** — EMG sensor 對於
// missing-data cell 會輸出字面 "NaN"，下游 muscle_ratio / phase_sync 看到
// NaN 會把該 row 輸出為空白 cell（見 muscle_ratio/analyzer_test.go 的 NaN-handling test）。
// 若 caller 需要拒絕 NaN/Inf（例如 phase manifest 時間欄位），請在 caller 端加
// math.IsNaN / math.IsInf 檢查；phase_manifest_parser.go 的 parseFloat 已對稱實作。
//
// **行為設計**：刻意在解析前 `strings.TrimSpace` 是為了容納科學儀器（ANC 力板、
// EMG sensor）輸出的文字檔常見的 trailing tab / leading space。此前 ANC parser
// 用 `strconv.ParseFloat(fields[0], 64)` 對 ` 0.001 \t1.23` 這類 row 會回 error
// 整行被 skip
// 若你需要嚴格 round-trip 解析（拒絕任何前後空白），請改用 `strconv.ParseFloat`。
//
// **缺值 vs 字面 NaN**：本函式 **無法** 區分「empty cell（missing-data）」與
// 「字面 0」(兩者都回 `(0, false)` / `(0, true)`)；也無法區分「字面 NaN」與
// 「missing」(後者回 `(0, false)`，前者回 `(NaN, true)`)。需要嚴格 round-trip
// 區分 missing/NaN/Inf 的 caller 改用
// `ParseFloatCellWithMissing` + `FormatFloatCell`。
func ParseFloatCell(s string) (float64, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return 0, false
	}

	val, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, false
	}

	return val, true
}

// ParseTimeCell 解析時間欄位 — 在 ParseFloatCell 基礎上額外拒絕 NaN/Inf。
//
// 時間軸的 NaN/Inf 會 silently poison 下游：sliding-window 對齊與 monotonicity 的
// `<` 比較對 NaN 永遠 false，會 bypass 範圍檢查，最終 phase_sync 除以 NaN 造成
// NaN-poisoning。故時間欄位一律 fail-fast 回 ok=false（對齊 phase_manifest_parser.go
// parseFloat 對時間點的 NaN/Inf 拒絕）。
//
// 注意：**通道值列仍用 ParseFloatCell** — EMG sensor 對 missing-data 輸出字面 "NaN"，
// 下游刻意把該 cell 輸出為空白（見 ParseFloatCell doc）。只有時間欄位走本函式。
func ParseTimeCell(s string) (float64, bool) {
	val, ok := ParseFloatCell(s)
	if !ok {
		return 0, false
	}
	if math.IsNaN(val) || math.IsInf(val, 0) {
		return 0, false
	}
	return val, true
}

// ParseFloatCellWithMissing parses a CSV cell with explicit "missing" semantics —
// the round-trip-safe counterpart to `FormatFloatCell`.
//
// Return semantics (v, isMissing):
//   - ""（含 whitespace-only）→ `(NaN, true)`  — 「missing-data」訊號
//   - "NaN"（不分大小寫）       → `(NaN, false)` — 字面 NaN 值（非 missing）
//   - "+Inf" / "-Inf" / "Inf"    → `(±Inf, false)`
//   - 合法數字                   → `(v, false)`
//   - 不可解析                   → `(NaN, false)` — 視為「壞 NaN-ish 值」交 caller 決定
//
// **與 ParseFloatCell 差異**：ParseFloatCell 把 "" 與 "NaN" / "Inf" 不可分地壓
// 在 ok=true/false 同一個維度;此函式拆成 `isMissing` 與 `v` 兩個維度,讓 writer
// 端可以用 FormatFloatCell 還原 "" / "NaN" / "+Inf" 三種不同字面,跑 round-trip
// 不損失。新 caller 若關心 missing 與 NaN value 的區別,應該選此函式。
//
// 為什麼 unparseable 回 `(NaN, false)` 而非 `(NaN, true)`：parse 失敗代表 cell
// 有 garbage（可能是 corrupted sensor output 或惡意輸入）;`isMissing=true` 是
// 「sensor 主動告知此 sample 沒被測到」的 semantic 訊號,不該被混入 garbage
// fallback。caller 想 fail-fast 可以直接 `if math.IsNaN(v) && !isMissing` 判定。
func ParseFloatCellWithMissing(s string) (float64, bool) {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return math.NaN(), true
	}

	val, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return math.NaN(), false
	}

	return val, false
}

// FormatFloatCell formats a float64 for CSV output with explicit missing-cell support —
// the round-trip-safe counterpart to `ParseFloatCellWithMissing`.
//
// Output semantics:
//   - isMissing=true             → ""        （空 cell — 表示 missing-data，無論 v 為何）
//   - math.IsNaN(v) && !isMissing → "NaN"    （字面 NaN — 表示真實 NaN 值）
//   - math.IsInf(v, +1)           → "+Inf"
//   - math.IsInf(v, -1)           → "-Inf"
//   - 其他                        → strconv.FormatFloat(v, 'f', precision, 64)
//
// **與 internal/io.formatNormalizedEMGCell 差異**：formatNormalizedEMGCell 對 NaN / Inf 一律寫空字串,
// 這是 muscle_ratio 對 NaN-as-missing 的 legacy 契約(見 analyzer_test.go
// TestAnalyze_NaNCellWrittenAsEmpty / TestAnalyze_EMGUpstreamNaN_OutputCellEmpty)。
// FormatFloatCell 提供 **嚴格區分** 的新語意,讓未來真正需要 round-trip 保留
// NaN 字面 / Inf 字面 的 writer(例如 manifest export)可以採用,不破壞既有契約。
//
// precision <= 0 視為 6（與 internal/io.phaseSyncPrecision 對齊）。
func FormatFloatCell(v float64, isMissing bool, precision int) string {
	if isMissing {
		return ""
	}

	if math.IsNaN(v) {
		return "NaN"
	}

	if math.IsInf(v, 1) {
		return "+Inf"
	}

	if math.IsInf(v, -1) {
		return "-Inf"
	}

	if precision <= 0 {
		precision = defaultFloatCellPrecision
	}

	return strconv.FormatFloat(v, 'f', precision, 64)
}

// defaultFloatCellPrecision 為 FormatFloatCell 預設小數位數，與 internal/io.phaseSyncPrecision 對齊。
const defaultFloatCellPrecision = 6

// ParseTimeAndChannels parses record[0] as time and record[channelStartIdx:] as channel values.
// Returns (0, nil, false) when the time cell is empty or unparseable — signal to skip the row.
// Channel cells that fail to parse silently fall back to 0 (preserves prior parser semantics).
func ParseTimeAndChannels(record []string, channelStartIdx int) (float64, []float64, bool) {
	if len(record) == 0 {
		return 0, nil, false
	}

	timeVal, ok := ParseTimeCell(record[0])
	if !ok {
		return 0, nil, false
	}

	channelCount := len(record) - channelStartIdx
	if channelCount < 0 {
		channelCount = 0
	}

	channels := make([]float64, channelCount)

	for i := 0; i < channelCount; i++ {
		if val, ok := ParseFloatCell(record[channelStartIdx+i]); ok {
			channels[i] = val
		}
	}

	return timeVal, channels, true
}

// FindTimeRangeIndices returns the first/last indices in times whose ms-rounded values
// fall within [startTime, endTime]. Rounding to integer milliseconds matches the
// project-wide convention for time comparison and avoids float precision drift.
// Returns a wrapped ErrTimeRangeNotFound when no samples qualify.
func FindTimeRangeIndices(times []float64, startTime, endTime float64) (int, int, error) {
	startMs := int64(math.Round(startTime * 1000))
	endMs := int64(math.Round(endTime * 1000))

	startIdx := -1
	endIdx := -1

	for i, t := range times {
		tMs := int64(math.Round(t * 1000))
		if startIdx == -1 && tMs >= startMs {
			startIdx = i
		}

		if tMs <= endMs {
			endIdx = i
		} else if endIdx != -1 {
			break
		}
	}

	if startIdx == -1 || endIdx == -1 || startIdx > endIdx {
		return -1, -1, fmt.Errorf("找不到有效的時間範圍數據: %w", ErrTimeRangeNotFound)
	}

	return startIdx, endIdx, nil
}

// FindIndexRangeIndices returns the first/last positions in indices whose values
// fall within [startIndex, endIndex]. Mirrors FindTimeRangeIndices but for the
// int-typed sample indices that Motion data uses instead of seconds.
// Returns a wrapped ErrIndexRangeNotFound when no samples qualify.
func FindIndexRangeIndices(indices []int, startIndex, endIndex int) (int, int, error) {
	startPos := -1
	endPos := -1

	for i, idx := range indices {
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
		return -1, -1, fmt.Errorf("找不到有效的 index 範圍數據: %w", ErrIndexRangeNotFound)
	}

	return startPos, endPos, nil
}

// TimeSeriesLabels carries the dataset-specific words that ValidateTimeSeries
// substitutes into its error messages so EMG / Force / Motion validation can
// share one generic implementation while keeping their user-facing wording.
type TimeSeriesLabels struct {
	DataName     string // "EMG" / "力板" / "Motion"
	SeriesName   string // "時間序列" / "時間序列" / "index 序列"
	SeriesPos    string // "索引" / "索引" / "位置"
	ChannelLabel string // "通道" / "通道" / "列"
}

// joinDataName 把 dataName 與後接文字接起來，ASCII 字尾自動插入空白避免「EMG時間序列」
// 連寫，CJK 字尾則直接相接（中文不需要空格）。
// 修法理由（cross-compare review P3）：原先 ValidateTimeSeries 用 "%s%s" 直接拼接，
// 要求 caller 自行把 "EMG " 帶尾空白傳進來。一旦未來 caller 漏掉 trailing space，
// 錯誤訊息會悄悄變成 "EMG時間序列為空"，靜默 lint。集中到 helper 後 caller 只傳乾淨
// dataName，格式不再仰賴傳入字串的細節。
//
// **限制**：只看 dataName 末 byte 是否為 ASCII alphanumeric。對於以 ASCII 標點
// 結尾（如 "EMG-"）或多 byte 字尾末 byte 恰巧落在 A-Z/a-z/0-9 範圍的罕見輸入，
// 判斷結果可能不準。目前 3 個 caller (EMG / Motion / 力板) 結尾皆為「ASCII 字母 +
// 中文字」標準型態，故安全。未來新增 dataName 若不符此型態，請更新此函式或在
// caller 端自行決定間隔。
func joinDataName(dataName, rest string) string {
	if dataName == "" {
		return rest
	}
	last := dataName[len(dataName)-1]
	if (last >= 'A' && last <= 'Z') || (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9') {
		return dataName + " " + rest
	}
	return dataName + rest
}

// ValidateTimeSeries checks the canonical "time series + channel map" invariants:
// non-empty series, at least one channel, strictly increasing series, and all
// channels matching the series length. EMG / Force / Motion share this shape
// except for the typed series (float64 vs int) and the user-facing wording.
// Callers handle the nil-pointer guard themselves because the outer data type differs.
//
// TimeSeriesLabels.DataName 不需要帶 trailing space — joinDataName 會視字尾自動處理。
func ValidateTimeSeries[T util.Number](
	series []T,
	channels map[string][]float64,
	labels TimeSeriesLabels,
) error {
	if len(series) == 0 {
		return fmt.Errorf("%s為空: %w",
			joinDataName(labels.DataName, labels.SeriesName), ErrTimeSequenceEmpty)
	}

	if len(channels) == 0 {
		return fmt.Errorf("%s沒有任何%s數據: %w",
			joinDataName(labels.DataName, ""), labels.ChannelLabel, ErrNoChannels)
	}

	for i := 1; i < len(series); i++ {
		if series[i] <= series[i-1] {
			return fmt.Errorf("%s在%s %d 處不是遞增的: %w",
				joinDataName(labels.DataName, labels.SeriesName),
				labels.SeriesPos, i, ErrTimeNotIncreasing)
		}
	}

	expectedLen := len(series)
	for name, channelData := range channels {
		if len(channelData) != expectedLen {
			return fmt.Errorf("%s %s 的數據長度 (%d) 與%s長度 (%d) 不符: %w",
				labels.ChannelLabel, name, len(channelData),
				joinDataName(labels.DataName, labels.SeriesName),
				expectedLen, ErrInconsistentLength)
		}
	}

	return nil
}
