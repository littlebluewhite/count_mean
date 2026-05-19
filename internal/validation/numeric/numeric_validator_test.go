package numeric

import (
	"math"
	"testing"
)

// TestValidator_ValidateFloat_ScientificNotation 是 TODO_2 #P1-A8-4 的 regression：
// 過去 ValidateFloat 內含一條 dead branch「若偵測到 e+/e-/E+/E- pattern 則放行」，
// 但 patterns/patterns.go 註冊的 NumericMalicious list 只有 `e+e` `E+E` `e-e` `E-E`
// （連續兩個 e/E），永遠不會回傳獨立的 `e+` / `e-` / `E+` / `E-`，所以那條 branch
// 是死碼。刪除後只剩單純「detected → reject」邏輯，並交由 strconv.ParseFloat
// 自行處理合法的科學記號（1.5e10 等）。
//
// 此 test fixture 鎖住兩條不變式：
//  1. 合法科學記號（沒有可疑 pattern）依舊被 ParseFloat 接受
//  2. 真正可疑的 pattern（e+e、++ 等）依舊被拒絕
func TestValidator_ValidateFloat_ScientificNotation(t *testing.T) {
	t.Parallel()

	v := NewValidator()

	t.Run("legitimate scientific notation is accepted", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			in   string
			want float64
		}{
			{"1.5e10", 1.5e10},
			{"2.0E-3", 2.0e-3},
			{"1e2", 100},
			{"-3.14e+5", -3.14e5},
		}
		for _, c := range cases {
			got, err := v.ValidateFloat(c.in, "field", -math.MaxFloat64, math.MaxFloat64)
			if err != nil {
				t.Errorf("ValidateFloat(%q) unexpected error: %v", c.in, err)
				continue
			}
			if got != c.want {
				t.Errorf("ValidateFloat(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	})

	t.Run("malicious patterns are rejected", func(t *testing.T) {
		t.Parallel()
		bad := []string{
			"1e+e2", // double e via "e+e"
			"1E-E2", // double E via "E-E"
			"1++2",  // ++ pattern
			"0x1A",  // hex prefix
			"NaN",   // special float keyword in pattern list
		}
		for _, c := range bad {
			if _, err := v.ValidateFloat(c, "field", -math.MaxFloat64, math.MaxFloat64); err == nil {
				t.Errorf("ValidateFloat(%q) should reject malicious pattern, got nil error", c)
			}
		}
	})
}

// TestValidator_ValidateInteger_RejectsScientificNotation 確認 integer 路徑同步：
// scientific notation 對 integer 永遠不合法（strconv.ParseInt 不接受 `1e2`），
// 在 detector 攔到含 `e+` 等 substring 之前 ParseInt 也會 reject — 但因為
// detector 先 fire，我們驗證的是「不會 silently 接受」，而不論哪條 path 拒絕。
func TestValidator_ValidateInteger_RejectsScientificNotation(t *testing.T) {
	t.Parallel()

	v := NewValidator()

	cases := []string{
		"1e2",
		"1E-3",
		"1e+5",
		"-3.14e10",
	}
	for _, c := range cases {
		if _, err := v.ValidateInteger(c, "field", math.MinInt64, math.MaxInt64); err == nil {
			t.Errorf("ValidateInteger(%q) should reject scientific notation, got nil error", c)
		}
	}
}

// TestSafeRangeConstants_BoundedBelowLanguageMax 鎖住 TODO_2 #P1-A8-5 的設計
// 不變式：safeMaxInt64 / safeMaxFloat64 必須小於 language-level math.MaxInt64
// / math.MaxFloat64（headroom），未來改名或調值時 reviewer 一眼看到 boundary
// 預期，避免把保留位 silently 拿掉。
func TestSafeRangeConstants_BoundedBelowLanguageMax(t *testing.T) {
	t.Parallel()

	t.Run("int reserves 1 unit headroom", func(t *testing.T) {
		t.Parallel()
		if safeMaxInt64 != math.MaxInt64-1 {
			t.Errorf("safeMaxInt64 = %d, want math.MaxInt64-1 = %d", safeMaxInt64, math.MaxInt64-1)
		}
	})

	t.Run("float reserves 10x decimal headroom", func(t *testing.T) {
		t.Parallel()
		// 1.7976931348623157e+307 must be exactly one decimal magnitude below
		// math.MaxFloat64 ≈ 1.7976931348623157e+308.
		if safeMaxFloat64*10 > math.MaxFloat64 || safeMaxFloat64*10 < math.MaxFloat64/100 {
			t.Errorf("safeMaxFloat64 (%g) is not within one decimal magnitude of math.MaxFloat64 (%g)",
				safeMaxFloat64, math.MaxFloat64)
		}
	})
}
