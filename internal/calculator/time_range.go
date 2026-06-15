package calculator

import "count_mean/internal/parsers"

// ResolveTimeRange determines the actual time range from records.
// 當 startTime 與 endTime 均為零時,從 records 第一列與最後一列的時間欄位推導出範圍;
// 否則直接回傳呼叫端指定的範圍。
func ResolveTimeRange(records [][]string, startTime, endTime float64) (float64, float64) {
	if startTime != 0 || endTime != 0 {
		return startTime, endTime
	}

	var start, end float64

	if len(records) > 1 && len(records[1]) > 0 {
		start, _ = parsers.ParseFloatCell(records[1][0])
	}

	if len(records) > 1 && len(records[len(records)-1]) > 0 {
		end, _ = parsers.ParseFloatCell(records[len(records)-1][0])
	}

	return start, end
}
