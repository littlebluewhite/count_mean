package calculator

import "math"

// UnitScale 是 [[Scaled domain]] (縮放域) 換算的單一 owner。
//
// 時間與 EMG 值經 util.Str2Number 解析後已乘 10^scalingFactor (整數化以避開浮點誤差),
// 該域稱縮放域;使用者輸入 (秒) 與 CSV 輸出 (原單位) 則在原域。ToScaled 把原域值
// 轉入縮放域,FromScaled 轉回。scalingFactor 於 calculator ctor 時固定
// (ADR-0049;ADR-0005 Option D 的 per-call 注入仍維持拒絕)。
//
// 算術刻意與既有路徑 bit-identical:ToScaled 乘 10^sf、FromScaled「除以」10^sf
// (不是乘 10^-sf)。util.Str2Number 因 util 不能 import calculator 而自帶同一乘法,
// 由 TestUnitScale_MatchesStr2Number 釘住兩者一致。
type UnitScale struct{ m float64 }

// NewUnitScale 以 scalingFactor 建立換算器 (m = 10^sf)。
func NewUnitScale(sf int) UnitScale { return UnitScale{m: math.Pow10(sf)} }

// ToScaled 把原域值轉入縮放域 (v × 10^sf)。
func (u UnitScale) ToScaled(v float64) float64 { return v * u.m }

// FromScaled 把縮放域值轉回原域 (v ÷ 10^sf)。
func (u UnitScale) FromScaled(v float64) float64 { return v / u.m }
