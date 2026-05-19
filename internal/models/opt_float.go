package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// ErrOptFloatNaN 表示 UnmarshalJSON 收到 NaN 字面或數值,違反「OptFloat 只允許有限值」契約。
//
// JSON 來源若是「字面 NaN(JSON encoder 雖然不該寫,但實務上 Python/JS
// 等不同 encoder 對 NaN 行為各異)、Inf 字面、或 1e308 等過大數導致 IsInf」
// 任何不可運算的浮點,UnmarshalJSON 都應該 fail-fast。原本只擋語法錯誤,讓
// `1e308` 透過 json.Unmarshal 成功變成 +Inf,污染下游 phase 計算。
var ErrOptFloatNaN = errors.New("OptFloat 值為 NaN")

// ErrOptFloatInf 表示 UnmarshalJSON 收到 ±Inf 字面或溢位至 ±Inf 的大數。
// 與 ErrOptFloatNaN 拆開以利前端區分使用者輸入「N/A 字面」與「數值溢位」。
var ErrOptFloatInf = errors.New("OptFloat 值為 Inf")

// OptFloat 是「可選 float64」— 用 Set 旗標區分「已標定為 0」與「未提供 / NA / 空值」。
//
// 動機：PhasePoints 過去用 0 同時表達「未提供」與「真實時間為 0 秒」。下游
// `if v > 0` pattern 在 t=0 的合法 phase point 上會被誤判為「未提供」而跳過。
// 雖然 EMG phase points 實務上不會在 t=0（記錄器 warm-up + 動作觸發 ≥ 1s），
// 仍是型別安全層級的瑕疵 — 用 OptFloat 從根源消除歧義。
//
// JSON 線格式（backward-compat）：
//   - MarshalJSON：Set=true 輸出 bare number；Set=false 輸出 null
//   - UnmarshalJSON：bare number → MakeOpt(v)，null/缺欄位 → NoOpt()
//
// 既有 JSON manifest（含 {"P0": 0.13} 樣式）仍能正確反序列化為 MakeOpt(0.13)。
type OptFloat struct {
	Value float64
	Set   bool
}

// MakeOpt 建構「已標定」的 OptFloat。
func MakeOpt(v float64) OptFloat {
	return OptFloat{Value: v, Set: true}
}

// NoOpt 建構「未提供」的 OptFloat（零值即可達成，但顯式回傳更清楚）。
func NoOpt() OptFloat {
	return OptFloat{}
}

// Get 回傳 (value, ok) — ok=false 表示未提供。
func (o OptFloat) Get() (float64, bool) {
	return o.Value, o.Set
}

// OrElse 回傳 Value 或 default（未提供時走 default）。
func (o OptFloat) OrElse(def float64) float64 {
	if o.Set {
		return o.Value
	}
	return def
}

// MarshalJSON 將已標定值輸出為 bare number，未標定值輸出為 null。
// 與既有 JSON 線格式相容：手寫 JSON 用 `"P0": 0.13` 仍能正確 unmarshal。
//
// NaN / ±Inf 也輸出 null,與 UnmarshalJSON 的「null → NoOpt」契約對稱。
// 過去這條 path 走 stdlib `*float64` marshal 直接 error("json: unsupported value")
// — caller 拿到序列化失敗;但 UnmarshalJSON 已 accept null 為 NaN/Inf 的合法 wire
// 表達 (透過 ErrOptFloatNaN/Inf reject 文字 NaN/Inf 但放行 null),Marshal 應走
// 同一策略:不可運算浮點 → null,讓 round-trip 可預期 (NaN/Inf → null → NoOpt)。
// 對 producer 副作用:不可運算值不再 crash 整個 JSON 序列化流程。
func (o OptFloat) MarshalJSON() ([]byte, error) {
	if !o.Set {
		return []byte("null"), nil
	}
	if math.IsNaN(o.Value) || math.IsInf(o.Value, 0) {
		return []byte("null"), nil
	}
	b, err := json.Marshal(o.Value)
	if err != nil {
		return nil, fmt.Errorf("OptFloat marshal: %w", err)
	}
	return b, nil
}

// UnmarshalJSON 接受 bare number 或 null。null/空字串 → NoOpt()，數字 → MakeOpt(v)。
//
// NaN / ±Inf reject 對齊 parsers.parseFloat 行為 — 即使 JSON encoder
// (或手工編輯) 寫出 "1e308" 這類過大值被解析為 +Inf,仍要在邊界 fail-fast,
// 避免污染下游 phase 計算 / 標準化結果(會把整批變成 NaN/Inf)。
func (o *OptFloat) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		*o = NoOpt()
		return nil
	}

	var v float64
	if err := json.Unmarshal(trimmed, &v); err != nil {
		return fmt.Errorf("OptFloat unmarshal: %w", err)
	}

	if math.IsNaN(v) {
		return fmt.Errorf("%w: %s", ErrOptFloatNaN, string(trimmed))
	}

	if math.IsInf(v, 0) {
		// 包含: JSON 字面 "Inf"/"+Inf"/"-Inf" 與 1e308 等溢位
		return fmt.Errorf("%w: %s", ErrOptFloatInf, string(trimmed))
	}

	*o = MakeOpt(v)
	return nil
}
