package util

import (
	"strconv"
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

// GetDecimalPrecision 取得數字以「定點表示」時小數點後的有效位數。
//
// 正確處理科學記號:有效定點小數位 = mantissa 小數位 − 指數值(下限 0)。
// 指數段不可單純丟棄——負指數會「增加」定點小數位,正指數會「減少」。
//
//	"1.5e-3"  = 0.0015      → 1 − (−3) = 4
//	"2.250E10"= 2.25e10(整數)→ max(0, 3 − 10) = 0
//	"5e-7"    = 0.0000005   → 0 − (−7) = 7
//	"1.234"   (無指數)      → 3
func GetDecimalPrecision(numStr string) int {
	// 移除空白字元
	numStr = strings.TrimSpace(numStr)

	// 拆出指數段(若有):指數影響定點小數位數,須計入而非丟棄。
	// 指數無法解析(如 "1.5e")時退化為 0(視同無指數)。
	exponent := 0
	if expIdx := strings.IndexAny(numStr, "eE"); expIdx != -1 {
		if e, err := strconv.Atoi(numStr[expIdx+1:]); err == nil {
			exponent = e
		}
		numStr = numStr[:expIdx]
	}

	// mantissa 小數點後位數
	mantissaDecimals := 0
	if dotIndex := strings.Index(numStr, "."); dotIndex != -1 {
		mantissaDecimals = len(numStr) - dotIndex - 1
	}

	// 定點有效小數位 = mantissa 小數位 − 指數,下限 0。
	if precision := mantissaDecimals - exponent; precision > 0 {
		return precision
	}

	return 0
}
