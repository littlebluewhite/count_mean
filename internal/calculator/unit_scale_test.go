package calculator

import (
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/util"
)

// TestUnitScale_MatchesStr2Number 釘住 ToScaled 與 util.Str2Number 的縮放算術 bit-for-bit 一致
// (util 不能 import calculator,故兩處各自實作、由本測試鎖同值)。
func TestUnitScale_MatchesStr2Number(t *testing.T) {
	inputs := []string{"0", "0.001", "1.5", "12.3456789", "922.5", "1005", "-3.25"}
	for _, sf := range []int{0, 1, 3, 6, 10} {
		u := NewUnitScale(sf)
		for _, in := range inputs {
			want, err := util.Str2Number[float64, int](in, sf)
			require.NoError(t, err)

			v, err := strconv.ParseFloat(in, 64)
			require.NoError(t, err)
			assert.Equal(t, math.Float64bits(want), math.Float64bits(u.ToScaled(v)),
				"sf=%d input=%s", sf, in)
		}
	}
}

// TestUnitScale_Direction 鎖住方向:ToScaled 乘 10^sf、FromScaled 除 10^sf;
// 兩者若接反 (seconds-unit phase range 回歸、CSV 反縮放) 會在此失敗。
func TestUnitScale_Direction(t *testing.T) {
	u := NewUnitScale(3)
	assert.InDelta(t, 2500.0, u.ToScaled(2.5), 0)
	assert.InDelta(t, 2.5, u.FromScaled(2500.0), 0)

	// 除法而非乘 10^-sf:兩者在 float64 下可能差 1 ulp,輸出 bytes 需維持除法。
	const v = 123456789.0
	u10 := NewUnitScale(10)
	assert.Equal(t, math.Float64bits(v/1e10), math.Float64bits(u10.FromScaled(v)))
}

func TestUnitScale_RoundTrip(t *testing.T) {
	for _, sf := range []int{0, 1, 3, 6} {
		u := NewUnitScale(sf)
		for _, v := range []float64{0, 0.5, 1, 12.25, 1000, -7.5} {
			assert.InDelta(t, v, u.FromScaled(u.ToScaled(v)), 1e-9*math.Max(1, math.Abs(v)),
				"sf=%d v=%v", sf, v)
		}
	}
}

// TestUnitScale_MonotonicBeyondInt64Microseconds 僅記錄:SF=10 下 >922 秒的時間
// (舊 ×1e6 轉 int64 會 clamp 的範圍) ToScaled 仍是保序的 float64 乘法。
// 812ebac 的真正釘子是 TestMaxMean_ScaledTimeOverflow_WindowMiscompute
// (maxmean_invariants_test.go),不是本測試。
func TestUnitScale_MonotonicBeyondInt64Microseconds(t *testing.T) {
	u := NewUnitScale(10)
	a, b := u.ToScaled(1000.0), u.ToScaled(1001.0)
	assert.Less(t, a, b)
	assert.InDelta(t, 1e13, a, 0)
}
