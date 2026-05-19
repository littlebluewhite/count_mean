package util

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ErrNaNInput indicates the input string parsed to NaN (e.g. literal "NaN").
//
// 雖然多數 caller (data_parser.parseChannels / large_file_handler /
// normalizer / phase_analyzer) 都在拿到 val 後對 NaN/Inf 做進一步驗證,但
// 中央 entry 應 fail-fast,避免「caller 忘檢查 → NaN 進 sliding window →
// silent miscompute」這條長尾。errors.Is 可辨識 → caller 可選擇性放寬
// (例:emg_writer round-trip path 已 documented 容忍 NaN sentinel 表示 missing,
// 該路徑不走 Str2Number)。
var ErrNaNInput = errors.New("str2number: input parsed to NaN")

// ErrInfInput indicates the input string parsed to ±Inf (e.g. "Inf" / "+Inf" /
// "-Inf" 或極端 ParseFloat exponent overflow)。Same rationale as ErrNaNInput.
var ErrInfInput = errors.New("str2number: input parsed to Inf")

// Str2Number parses a decimal or scientific-notation string and converts it
// to a numeric type, multiplied by 10^move.
//
// Implementation uses strconv.ParseFloat throughout (P1-A2-5 / P1-A16-3):
//   - locale-independent in Go (always "." as decimal separator), and
//   - natively handles both uppercase "E" and lowercase "e" exponents.
//
// The previous implementation manually split on "E" and recombined mantissa ×
// 10^(move+exp). That worked for the EMG-tool's well-formed inputs but mixed
// concerns (parsing + arithmetic) and silently fell back to a no-split path
// on lowercase "e" — fragile semantics that this rewrite removes.
//
// Whitespace handling: the previous implementation stripped spaces from the
// mantissa via ReplaceAll. To preserve backward-compatible tolerance for
// CSV / phase-string inputs that include surrounding whitespace, we
// strings.TrimSpace the input before parsing. Internal whitespace (e.g.
// "1. 5") is no longer accepted — that was never a documented contract.
//
// NaN / ±Inf reject。strconv.ParseFloat 對 "NaN" / "+Inf" / "-Inf"
// (大小寫 insensitive) 與 exponent overflow 回 finite-but-NaN / ±Inf 數值
// 且 err == nil;此時 caller (例:sliding window 增量加減) 拿到 NaN 後 windowSum
// 永久污染、`NaN > maxMean` 恆為 false,silent miscompute。Str2Number 在中央
// 入口攔下,回 ErrNaNInput / ErrInfInput sentinel (errors.Is 可辨識),
// caller 不需要再每處檢查 — 對齊 sliding window/ normalizer的
// fail-fast 模型。
//
//nolint:ireturn // Generic function must return interface type T
func Str2Number[T Number, U ~int](input string, move U) (T, error) {
	trimmed := strings.TrimSpace(input)

	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse float from '%s': %w", input, err)
	}

	if math.IsNaN(parsed) {
		return 0, fmt.Errorf("%w: input=%q", ErrNaNInput, input)
	}

	if math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%w: input=%q", ErrInfInput, input)
	}

	return T(parsed * math.Pow10(int(move))), nil
}
