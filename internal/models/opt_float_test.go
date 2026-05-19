package models

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptFloat_MakeOpt(t *testing.T) {
	opt := MakeOpt(1.5)
	assert.True(t, opt.Set)
	assert.Equal(t, 1.5, opt.Value)

	// MakeOpt(0) 必須回 Set=true Value=0 — 這正是 Batch T 的核心：
	// 區分「真實 0」與「未提供」。
	zero := MakeOpt(0.0)
	assert.True(t, zero.Set, "MakeOpt(0) 必須是 Set=true，與 NoOpt 嚴格區分")
	assert.Equal(t, 0.0, zero.Value)
}

func TestOptFloat_NoOpt(t *testing.T) {
	opt := NoOpt()
	assert.False(t, opt.Set)
	assert.Equal(t, 0.0, opt.Value)
}

func TestOptFloat_Get(t *testing.T) {
	v, ok := MakeOpt(2.5).Get()
	assert.True(t, ok)
	assert.Equal(t, 2.5, v)

	v, ok = NoOpt().Get()
	assert.False(t, ok)
	assert.Equal(t, 0.0, v)

	// 關鍵：MakeOpt(0) 與 NoOpt() 在 Get() 回傳上必須不同。
	v, ok = MakeOpt(0.0).Get()
	assert.True(t, ok, "MakeOpt(0).Get() ok 必須是 true")
	assert.Equal(t, 0.0, v)
}

func TestOptFloat_OrElse(t *testing.T) {
	assert.Equal(t, 2.5, MakeOpt(2.5).OrElse(99.0))
	assert.Equal(t, 99.0, NoOpt().OrElse(99.0))
	// 關鍵：MakeOpt(0).OrElse(99) 必須回 0，不能誤認為「未設定走 default」。
	assert.Equal(t, 0.0, MakeOpt(0.0).OrElse(99.0))
}

func TestOptFloat_MarshalJSON_Set(t *testing.T) {
	opt := MakeOpt(1.5)
	b, err := json.Marshal(opt)
	require.NoError(t, err)
	assert.Equal(t, "1.5", string(b))
}

func TestOptFloat_MarshalJSON_Zero(t *testing.T) {
	// MakeOpt(0) 序列化為 bare number "0"（不是 null）— 與 NoOpt 區分。
	opt := MakeOpt(0.0)
	b, err := json.Marshal(opt)
	require.NoError(t, err)
	assert.Equal(t, "0", string(b))
}

func TestOptFloat_MarshalJSON_NoOpt(t *testing.T) {
	opt := NoOpt()
	b, err := json.Marshal(opt)
	require.NoError(t, err)
	assert.Equal(t, "null", string(b))
}

func TestOptFloat_UnmarshalJSON_Number(t *testing.T) {
	var opt OptFloat
	require.NoError(t, json.Unmarshal([]byte("1.5"), &opt))
	assert.True(t, opt.Set)
	assert.Equal(t, 1.5, opt.Value)
}

func TestOptFloat_UnmarshalJSON_Zero(t *testing.T) {
	// 既有 JSON manifest（"P0": 0.0）必須反序列化為 MakeOpt(0)，保留 wire 相容。
	var opt OptFloat
	require.NoError(t, json.Unmarshal([]byte("0"), &opt))
	assert.True(t, opt.Set, "JSON 中的 0 必須反序列化為 Set=true (Batch T 核心契約)")
	assert.Equal(t, 0.0, opt.Value)
}

func TestOptFloat_UnmarshalJSON_Null(t *testing.T) {
	var opt OptFloat
	require.NoError(t, json.Unmarshal([]byte("null"), &opt))
	assert.False(t, opt.Set)
}

func TestOptFloat_UnmarshalJSON_Invalid(t *testing.T) {
	var opt OptFloat
	err := json.Unmarshal([]byte(`"not a number"`), &opt)
	assert.Error(t, err)
}

func TestOptFloat_RoundTrip(t *testing.T) {
	// 確認 marshal → unmarshal 路徑保留 Set 旗標。
	cases := []OptFloat{
		MakeOpt(1.23),
		MakeOpt(0.0),
		MakeOpt(-5.5),
		NoOpt(),
	}
	for _, original := range cases {
		b, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded OptFloat
		require.NoError(t, json.Unmarshal(b, &decoded))
		assert.Equal(t, original, decoded, "round-trip 不變 (orig=%+v, json=%s, decoded=%+v)", original, b, decoded)
	}
}

func TestOptFloat_NaNInf_Set(t *testing.T) {
	// MakeOpt 仍允許存 NaN — 結構本身不檢,reject 集中在 wire boundary (UnmarshalJSON)
	// 與 caller (parseFloat / ValidatePhaseManifest)。這個 test 守護:結構層 setter
	// 不主動篩選,避免下游想要 NaN sentinel (例如 "未填寫" 比 0/Inf 更明確) 時被擋。
	nan := MakeOpt(math.NaN())
	assert.True(t, nan.Set)
	assert.True(t, math.IsNaN(nan.Value))
}

// TestOptFloat_UnmarshalJSON_NaN 守護 JSON 字面 NaN 必須 reject。
// Go stdlib json.Unmarshal 對字面 "NaN" 本身就回錯(parse error),所以這條 path
// 必失敗於語法 error layer。仍保留 test 釘住「不能 silent 接受 NaN sentinel」契約 —
// 未來若改用 encoding/json/v2 / json-iterator/go 等接受 NaN 字面的 encoder,
// 我們仍要透過 ErrOptFloatNaN 攔住,而不是讓 NaN 流入下游。
func TestOptFloat_UnmarshalJSON_NaN(t *testing.T) {
	var opt OptFloat
	err := json.Unmarshal([]byte("NaN"), &opt)
	require.Error(t, err, "JSON 字面 NaN 必須失敗 — 無論走 parse error 或 ErrOptFloatNaN")
}

// TestOptFloat_UnmarshalJSON_Inf 守護 JSON 字面 Inf / -Inf 必須 reject。
// 同上,stdlib 對這些字面回 parse error;若未來換 encoder 我們仍透過 ErrOptFloatInf 攔下。
func TestOptFloat_UnmarshalJSON_Inf(t *testing.T) {
	cases := []string{"Infinity", "-Infinity", "Inf", "-Inf", "+Inf"}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			var opt OptFloat
			err := json.Unmarshal([]byte(c), &opt)
			require.Error(t, err, "JSON 字面 %q 必須被 reject", c)
		})
	}
}

// TestOptFloat_MarshalJSON_NaNInfEmitsNull 守護 OptFloat.MarshalJSON 對
// NaN / ±Inf 必須 emit `null`,而非走 stdlib float64 path 拋 "json: unsupported
// value" error。理由是與 UnmarshalJSON 對稱:UnmarshalJSON 已 accept `null` 為
// NaN/Inf sentinel,MarshalJSON 也應走同一個語意 (NaN/Inf → null),否則 round-trip
// 不一致 (Marshal 拋 error / Unmarshal 接受 null)。
//
// 修法後:Set=true + Value 為 NaN/+Inf/-Inf,均 emit "null"。caller 後續
// json.Unmarshal 回 OptFloat 會得到 NoOpt() (即 Set=false),這是合理 round-trip:
// 不可運算的浮點 (NaN/Inf) 不應在 wire 上有獨立表達,合併到 "未提供" sentinel。
//
// 對齊 UnmarshalJSON 的「null → NoOpt」契約;TestOptFloat_UnmarshalJSON_Null 仍守 reverse direction。
func TestOptFloat_MarshalJSON_NaNInfEmitsNull(t *testing.T) {
	cases := []struct {
		name string
		v    float64
	}{
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opt := OptFloat{Value: tc.v, Set: true}
			b, err := opt.MarshalJSON()
			require.NoError(t, err, "MarshalJSON(%s) 必須不 error", tc.name)
			assert.Equal(t, []byte("null"), b,
				"MarshalJSON(%s) 必須 emit `null` 與 UnmarshalJSON null 契約對稱", tc.name)
		})
	}
}

// TestOptFloat_MarshalJSON_RoundTrip_NaNInfBecomesNoOpt 守護 副契約:
// 透過 json.Marshal → json.Unmarshal 一個 NaN/Inf OptFloat,結果應為 NoOpt()
// (Set=false)。這證明 wire 上 NaN/Inf → null → NoOpt 的單向蛻變是 self-consistent。
//
// 為什麼合理:wire format 不該帶不可運算的浮點;若 producer 端有 NaN/Inf,
// consumer 端應該視同「未提供」處理,而不是拒絕整個 JSON。對齊 UnmarshalJSON
// 的 null → NoOpt 契約。
func TestOptFloat_MarshalJSON_RoundTrip_NaNInfBecomesNoOpt(t *testing.T) {
	cases := []float64{math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, v := range cases {
		t.Run(fmt.Sprintf("v=%v", v), func(t *testing.T) {
			original := OptFloat{Value: v, Set: true}
			b, err := json.Marshal(original)
			require.NoError(t, err)
			assert.Equal(t, "null", string(b), "wire 應為 null")

			var decoded OptFloat
			require.NoError(t, json.Unmarshal(b, &decoded))
			assert.False(t, decoded.Set, "NaN/Inf round-trip 後應為 NoOpt (Set=false)")
		})
	}
}
