package util

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Str2Number parses a scientific notation string and converts it to a numeric type.
// The move parameter specifies an additional power of 10 to multiply by.
//
//nolint:ireturn // Generic function must return interface type T
func Str2Number[T Number, U ~int](input string, move U) (T, error) {
	parts := strings.Split(input, "E")
	// 去除空白
	mantissaStr := strings.ReplaceAll(parts[0], " ", "")

	mantissa, err := strconv.ParseFloat(mantissaStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse float from '%s': %w", input, err)
	}

	if len(parts) == 1 {
		multiplier := math.Pow10(int(move))
		return T(mantissa * multiplier), nil
	}

	exponent, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse exponent from '%s': %w", input, err)
	}

	multiplier := math.Pow10(int(int64(move) + exponent))
	result := mantissa * multiplier

	return T(result), nil
}
