package models_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

func TestPhasePoint_Constants_HaveExpectedWireValues(t *testing.T) {
	// Wire identifiers — 與既有 CSV / JSON / 前端硬編碼相同,變動會破壞相容性。
	cases := map[models.PhasePoint]string{
		models.PhaseP0: "P0",
		models.PhaseP1: "P1",
		models.PhaseP2: "P2",
		models.PhaseS:  "S",
		models.PhaseC:  "C",
		models.PhaseD:  "D",
		models.PhaseT0: "T0",
		models.PhaseT:  "T",
		models.PhaseO:  "O",
		models.PhaseL:  "L",
	}
	for phase, expected := range cases {
		assert.Equal(t, expected, string(phase), "phase %q wire value drifted", expected)
	}
}

func TestAllPhases_ReturnsTenInCanonicalOrder(t *testing.T) {
	expected := []models.PhasePoint{
		models.PhaseP0, models.PhaseP1, models.PhaseP2, models.PhaseS, models.PhaseC,
		models.PhaseD, models.PhaseT0, models.PhaseT, models.PhaseO, models.PhaseL,
	}
	require.Equal(t, expected, models.AllPhases())
}

func TestAllPhases_IsDefensiveCopy(t *testing.T) {
	first := models.AllPhases()
	first[0] = "MUTATED"
	second := models.AllPhases()
	assert.Equal(t, models.PhaseP0, second[0], "AllPhases must return a copy; mutating caller's slice leaked into internal storage")
}

func TestPhasePoint_IsValid(t *testing.T) {
	for _, phase := range models.AllPhases() {
		assert.True(t, phase.IsValid(), "%q should be valid", phase)
	}
	invalid := []models.PhasePoint{"", "X", "P3", "p0", " P0", "Z9"}
	for _, phase := range invalid {
		assert.False(t, phase.IsValid(), "%q should be invalid", phase)
	}
}

// TestParsePhasePoint 是 cross-compare review P2 finding 的 regression test。
// PhasePoint 是 defined string type — 直接 cast PhasePoint("invalid") 不會觸發
// IsValid 檢查。ParsePhasePoint 是「字串入口應走的 sanctioned constructor」，
// 讓非法輸入在邊界立即被 caller 看見而非延後到下游 IsValid 才發現。
func TestParsePhasePoint(t *testing.T) {
	t.Run("valid_canonical", func(t *testing.T) {
		for _, raw := range []string{"P0", "P1", "P2", "S", "C", "D", "T0", "T", "O", "L"} {
			got, err := models.ParsePhasePoint(raw)
			require.NoError(t, err, "valid %q should parse", raw)
			assert.Equal(t, models.PhasePoint(raw), got)
		}
	})

	t.Run("invalid_rejected", func(t *testing.T) {
		invalid := []string{"", "X", "P3", "p0", " P0", "Z9", "P1 ", "0P"}
		for _, raw := range invalid {
			got, err := models.ParsePhasePoint(raw)
			assert.Error(t, err, "invalid %q should be rejected", raw)
			assert.Equal(t, models.PhasePoint(""), got, "invalid input should return zero value")
		}
	})
}

func TestPhasePoint_IsMotionIndex(t *testing.T) {
	// 唯一 motion index 是 D 與 O。
	motionPhases := []models.PhasePoint{models.PhaseD, models.PhaseO}
	for _, phase := range motionPhases {
		assert.True(t, phase.IsMotionIndex(), "%q should be motion index", phase)
	}

	for _, phase := range models.AllPhases() {
		if phase == models.PhaseD || phase == models.PhaseO {
			continue
		}
		assert.False(t, phase.IsMotionIndex(), "%q should NOT be motion index", phase)
	}

	// invalid phase 也不算 motion index
	assert.False(t, models.PhasePoint("X").IsMotionIndex(), "invalid phase should not be motion index")
}

func TestPhasePoint_Order(t *testing.T) {
	for i, phase := range models.AllPhases() {
		assert.Equal(t, i, phase.Order(), "%q order mismatch", phase)
	}
	assert.Equal(t, -1, models.PhasePoint("X").Order(), "invalid phase order must be -1")
	assert.Equal(t, -1, models.PhasePoint("").Order(), "empty phase order must be -1")
}

func TestPhasePoint_JSON_RoundTrip(t *testing.T) {
	// 用 struct 包裝模擬 AnalysisParams.StartPhase 場景。
	type wrapper struct {
		Phase models.PhasePoint `json:"phase"`
	}

	for _, original := range models.AllPhases() {
		data, err := json.Marshal(wrapper{Phase: original})
		require.NoError(t, err, "marshal %q", original)
		assert.JSONEq(t, `{"phase":"`+string(original)+`"}`, string(data),
			"JSON should serialize as raw wire string for %q", original)

		var decoded wrapper
		require.NoError(t, json.Unmarshal(data, &decoded), "unmarshal %q", original)
		assert.Equal(t, original, decoded.Phase, "round-trip mismatch for %q", original)
		assert.True(t, decoded.Phase.IsValid(), "round-tripped %q should remain valid", original)
	}
}

// TestPhasePoint_UnmarshalJSON_RejectsInvalid 釘住 Wave 6 PR4 fail-fast 改動:
// 過去 PhasePoint 沒自訂 UnmarshalJSON,前端傳 "Z9" / "p0" / "" 會被 Go 預設
// unmarshal 行為直接 cast 成 PhasePoint("Z9") 而非 error,所有保護都靠下游
// AnalyzePhaseSync 的 IsValid() 兜底。加上 UnmarshalJSON delegate 到 ParsePhasePoint
// 後,JSON 解析邊界就會拒絕非法字串並回 error。
func TestPhasePoint_UnmarshalJSON_RejectsInvalid(t *testing.T) {
	type wrapper struct {
		Phase models.PhasePoint `json:"phase"`
	}

	invalid := []string{"", "X", "P3", "p0", " P0", "Z9", "P1 ", "0P"}
	for _, raw := range invalid {
		jsonStr := `{"phase":"` + raw + `"}`
		var decoded wrapper
		err := json.Unmarshal([]byte(jsonStr), &decoded)
		require.Error(t, err, "invalid PhasePoint %q must fail unmarshal (was: %s)", raw, jsonStr)
		// 失敗時 decoded.Phase 應保持 zero-value,不應被部分污染
		assert.Equal(t, models.PhasePoint(""), decoded.Phase,
			"unmarshal 失敗時 PhasePoint 應保持 zero-value 而非部分接受 %q", raw)
	}
}

// TestPhasePoint_UnmarshalJSON_AcceptsAllCanonical 反向確認:10 個合法 PhasePoint
// 在 UnmarshalJSON 後不應被誤判為錯誤。
func TestPhasePoint_UnmarshalJSON_AcceptsAllCanonical(t *testing.T) {
	type wrapper struct {
		Phase models.PhasePoint `json:"phase"`
	}

	for _, valid := range models.AllPhases() {
		jsonStr := `{"phase":"` + string(valid) + `"}`
		var decoded wrapper
		require.NoError(t, json.Unmarshal([]byte(jsonStr), &decoded),
			"canonical PhasePoint %q must unmarshal without error", valid)
		assert.Equal(t, valid, decoded.Phase, "unmarshal value mismatch for %q", valid)
		assert.True(t, decoded.Phase.IsValid(), "%q 解析後 IsValid 必須為 true", valid)
	}
}

// TestPhasePoint_UnmarshalJSON_RejectsNonStringJSON 釘住:JSON 中 phase 不能是
// 數字 / null / object,必須是字串。這也是 Wave 6 PR4 預期的安全性收益。
func TestPhasePoint_UnmarshalJSON_RejectsNonStringJSON(t *testing.T) {
	type wrapper struct {
		Phase models.PhasePoint `json:"phase"`
	}

	cases := []string{
		`{"phase":42}`,         // 數字
		`{"phase":true}`,       // bool
		`{"phase":null}`,       // null
		`{"phase":["P0"]}`,     // array
		`{"phase":{"x":"P0"}}`, // object
	}
	for _, jsonStr := range cases {
		var decoded wrapper
		err := json.Unmarshal([]byte(jsonStr), &decoded)
		require.Error(t, err, "non-string JSON 必須讓 unmarshal 失敗: %s", jsonStr)
	}
}
