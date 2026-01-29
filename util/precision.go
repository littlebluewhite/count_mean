package util

import (
	"strings"
)

// DetectTimePrecision checks the first 10 rows of data to determine time precision.
// Returns default precision of 2 if not detected.
func DetectTimePrecision(records [][]string) int {
	if len(records) < 2 {
		return 2 // 預設精度
	}

	maxPrecision := 0
	// 檢查前幾行數據來確定時間精度
	for i := 1; i < len(records) && i <= 10; i++ {
		if len(records[i]) > 0 {
			timeStr := records[i][0]

			precision := GetDecimalPrecision(timeStr)
			if precision > maxPrecision {
				maxPrecision = precision
			}
		}
	}

	// 如果檢測不到小數位數，預設為 2
	if maxPrecision == 0 {
		maxPrecision = 2
	}

	return maxPrecision
}

// GetDecimalPrecision 獲取字串中小數點後的位數.
func GetDecimalPrecision(numStr string) int {
	// 移除空白字元
	numStr = strings.TrimSpace(numStr)

	// 找到小數點位置
	dotIndex := strings.Index(numStr, ".")
	if dotIndex == -1 {
		return 0 // 沒有小數點
	}

	// 計算小數點後的位數
	return len(numStr) - dotIndex - 1
}
