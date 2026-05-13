package parsers

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"count_mean/util"
)

// ParseFloatCell trims whitespace and parses a CSV cell as float64.
// Returns (0, false) when the cell is empty, whitespace-only, or unparseable —
// callers that want "best-effort zero fallback" can ignore ok and just use the value.
//
// **行為設計**：刻意在解析前 `strings.TrimSpace` 是為了容納科學儀器（ANC 力板、
// EMG sensor）輸出的文字檔常見的 trailing tab / leading space。此前 ANC parser
// 用 `strconv.ParseFloat(fields[0], 64)` 對 ` 0.001 \t1.23` 這類 row 會回 error
// 整行被 skip — Wave 4 cleanup 統一改走本函式後，這類 row 會被正確接受。
// 若你需要嚴格 round-trip 解析（拒絕任何前後空白），請改用 `strconv.ParseFloat`。
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

// ParseTimeAndChannels parses record[0] as time and record[channelStartIdx:] as channel values.
// Returns (0, nil, false) when the time cell is empty or unparseable — signal to skip the row.
// Channel cells that fail to parse silently fall back to 0 (preserves prior parser semantics).
func ParseTimeAndChannels(record []string, channelStartIdx int) (float64, []float64, bool) {
	if len(record) == 0 {
		return 0, nil, false
	}

	timeVal, ok := ParseFloatCell(record[0])
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
