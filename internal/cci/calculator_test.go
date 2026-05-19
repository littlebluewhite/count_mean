package cci

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateCCIRudolph_NormalCase(t *testing.T) {
	// emg1=0.3, emg2=0.5 → s=0.3, l=0.5 → (0.3/0.5)*(0.3+0.5) = 0.6*0.8 = 0.48
	result := CalculateCCIRudolph(0.3, 0.5)
	assert.InDelta(t, 0.48, result, 1e-10)
}

func TestCalculateCCIRudolph_EqualValues(t *testing.T) {
	// emg1=emg2=0.4 → s=l=0.4 → (0.4/0.4)*(0.4+0.4) = 1.0*0.8 = 0.8
	result := CalculateCCIRudolph(0.4, 0.4)
	assert.InDelta(t, 0.8, result, 1e-10)
}

func TestCalculateCCIRudolph_ZeroDenominator(t *testing.T) {
	result := CalculateCCIRudolph(0, 0)
	assert.Equal(t, 0.0, result)
}

func TestCalculateCCIRudolph_OneZero(t *testing.T) {
	// emg1=0, emg2=0.5 → s=0, l=0.5 → (0/0.5)*(0+0.5) = 0
	result := CalculateCCIRudolph(0, 0.5)
	assert.Equal(t, 0.0, result)
}

func TestCalculateCCIRudolph_Symmetric(t *testing.T) {
	// Order shouldn't matter
	r1 := CalculateCCIRudolph(0.2, 0.8)
	r2 := CalculateCCIRudolph(0.8, 0.2)
	assert.Equal(t, r1, r2)
}

func TestCalculateCCITimeSeries(t *testing.T) {
	ch1 := []float64{0.3, 0.5, 0.1}
	ch2 := []float64{0.5, 0.3, 0.4}

	result, err := CalculateCCITimeSeries(context.Background(), ch1, ch2)
	require.NoError(t, err)
	require.Len(t, result, 3)

	assert.InDelta(t, 0.48, result[0], 1e-10)
	assert.InDelta(t, 0.48, result[1], 1e-10)
	// 0.1/0.4 * (0.1+0.4) = 0.25*0.5 = 0.125
	assert.InDelta(t, 0.125, result[2], 1e-10)
}

func TestCalculateCCITimeSeries_LengthMismatch(t *testing.T) {
	ch1 := []float64{0.3, 0.5}
	ch2 := []float64{0.5}

	_, err := CalculateCCITimeSeries(context.Background(), ch1, ch2)
	assert.Error(t, err)
}

func TestMapHeaderToShortName(t *testing.T) {
	tests := []struct {
		header   string
		expected string
	}{
		{"R.RA: EMG 1 (from SF8_...)", "RA"},
		{"R.ES: EMG 2 (from SF8_...)", "ES"},
		{"R.IL: EMG 3 (from SF8_...)", "IL"},
		{"R.GMax: EMG 4 (from SF8_...)", "GMax"},
		{"R.RF: EMG 5 (from SF8_...)", "RF"},
		{"R.BF: EMG 6 (from SF8_...)", "BF"},
		{"R.TA&IO: EMG 7 (from SF8_...)", "TAIO"},
		{"R.MF: EMG 8 (from SF8_...)", "MF"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := MapHeaderToShortName(tt.header)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildChannelMap(t *testing.T) {
	headers := []string{
		"R.RA: EMG 1 (from SF8_Back_Tuck_Somersault_Rep_6.10) ->Filter->RMS []",
		"R.ES: EMG 2 (from SF8_Back_Tuck_Somersault_Rep_6.10) ->Filter->RMS []",
		"R.IL: EMG 3 (from SF8_Back_Tuck_Somersault_Rep_6.10) ->Filter->RMS []",
		"R.GMax: EMG 4 (from SF8_Back_Tuck_Somersault_Rep_6.10) ->Filter->RMS []",
		"R.RF: EMG 5 (from SF8_Back_Tuck_Somersault_Rep_6.10) ->Filter->RMS []",
		"R.BF: EMG 6 (from SF8_Back_Tuck_Somersault_Rep_6.10) ->Filter->RMS []",
		"R.TA&IO: EMG 7 (from SF8_Back_Tuck_Somersault_Rep_6.10) ->Filter->RMS []",
		"R.MF: EMG 8 (from SF8_Back_Tuck_Somersault_Rep_6.10) ->Filter->RMS []",
	}

	channelMap, err := BuildChannelMap(headers)
	require.NoError(t, err)

	assert.Equal(t, headers[0], channelMap["RA"])
	assert.Equal(t, headers[1], channelMap["ES"])
	assert.Equal(t, headers[6], channelMap["TAIO"])
	assert.Equal(t, headers[3], channelMap["GMax"])
}

func TestBuildChannelMap_MissingChannel(t *testing.T) {
	headers := []string{
		"R.RA: EMG 1",
		"R.ES: EMG 2",
		// Missing other channels
	}

	_, err := BuildChannelMap(headers)
	assert.Error(t, err)
}

// TestMapHeaderToShortName_LeftSideRejected 釘住 CCI 對齊 muscle_ratio 後，
// "L." 前綴的 header 一律回空字串、被 BuildChannelMap 跳過 — 不再像舊版會把 L.RA
// 也 strip 成 "RA" 與 R.RA 互覆蓋（last-wins by header order）。
func TestMapHeaderToShortName_LeftSideRejected(t *testing.T) {
	leftHeaders := []string{
		"L.RA: EMG 1 (left)",
		"L.ES: EMG 2 (left)",
		"L.GMax: EMG 4 (left)",
		"L.TA&IO: EMG 7 (left)",
	}
	for _, h := range leftHeaders {
		t.Run(h, func(t *testing.T) {
			assert.Empty(t, MapHeaderToShortName(h),
				"left-side header 應回空字串，實際=%q", MapHeaderToShortName(h))
		})
	}
}

// TestMapHeaderToShortName_NonRPrefixRejected 釘住非 "R." 前綴格式（缺冒號、無點前綴、
// 全名描述等）一律回空，避免 fallback 回 raw prefix 造成 silent miss-mapping。
func TestMapHeaderToShortName_NonRPrefixRejected(t *testing.T) {
	cases := []string{
		"EMG without colon",             // 無 ":"
		"R RECTUS ABDOMINIS: full name", // 無 "R." 點前綴
		"X.RA: unknown side prefix",     // 第二字母是 . 但非 R
		"R.UNKNOWN: not in shortNameMap",
	}
	for _, h := range cases {
		t.Run(h, func(t *testing.T) {
			assert.Empty(t, MapHeaderToShortName(h),
				"非 R.* 標準短名 header 應回空字串，實際=%q", MapHeaderToShortName(h))
		})
	}
}

// TestBuildChannelMap_LeftAndRightPriority 釘住 的核心契約：
// 同份 EMG 同時含左右 16 通道時，CCI 確定取右側、跳過左側，與 muscle_ratio 完全對稱。
// 修法前 last-wins by header order，channelMap["RA"] 可能指向 L.RA 或 R.RA 取決於排序。
func TestBuildChannelMap_LeftAndRightPriority(t *testing.T) {
	// 故意把 L.* 排在 R.* 之前 — 舊版 last-wins 邏輯下 L 會覆蓋 R
	headers := []string{
		"L.RA: EMG 1 (left)",
		"R.RA: EMG 1 (from SF8_...) ->Filter->RMS []",
		"L.ES: EMG 2 (left)",
		"R.ES: EMG 2 (from SF8_...) ->Filter->RMS []",
		"L.IL: EMG 3 (left)",
		"R.IL: EMG 3 (from SF8_...) ->Filter->RMS []",
		"L.GMax: EMG 4 (left)",
		"R.GMax: EMG 4 (from SF8_...) ->Filter->RMS []",
		"L.RF: EMG 5 (left)",
		"R.RF: EMG 5 (from SF8_...) ->Filter->RMS []",
		"L.BF: EMG 6 (left)",
		"R.BF: EMG 6 (from SF8_...) ->Filter->RMS []",
		"L.TA&IO: EMG 7 (left)",
		"R.TA&IO: EMG 7 (from SF8_...) ->Filter->RMS []",
		"L.MF: EMG 8 (left)",
		"R.MF: EMG 8 (from SF8_...) ->Filter->RMS []",
	}

	channelMap, err := BuildChannelMap(headers)
	require.NoError(t, err)

	// 每個短名必須對應 R.* header，絕不可指向 L.*
	for short, fullHeader := range channelMap {
		assert.True(t, strings.HasPrefix(fullHeader, "R."),
			"短名 %q 應對應 R.* header，實際=%q", short, fullHeader)
	}
}

// TestBuildChannelMap_DuplicateChannelFailFast 守護 
// 同份 EMG 出現兩個 R.RA (例如資料採集失誤、檔案合併出 bug) 必須 fail-fast,
// 不再 silent overwrite (last-wins by header order)。與 muscle_ratio 行為對稱,
// 兩個 caller 共用 musclemap.AssignShort helper。
func TestBuildChannelMap_DuplicateChannelFailFast(t *testing.T) {
	// 故意放兩個 R.RA — 同樣的短名映射,過去 last-wins,現在應 fail-fast
	headers := []string{
		"R.RA: EMG 1 (first session)",
		"R.RA: EMG 2 (second session)", // 重複
		"R.ES: EMG 3",
		"R.IL: EMG 4",
		"R.GMax: EMG 5",
		"R.RF: EMG 6",
		"R.BF: EMG 7",
		"R.TA&IO: EMG 8",
		"R.MF: EMG 9",
	}

	_, err := BuildChannelMap(headers)
	require.Error(t, err, "duplicate R.RA 必須 fail-fast,不能 silent overwrite")
	assert.Contains(t, err.Error(), "重複的肌肉通道", "error 必須說明重複根因")
	assert.Contains(t, err.Error(), "RA", "error 必須指名重複的 short name")
	// 兩個 header 都應該出現在 error 訊息中供使用者診斷
	assert.Contains(t, err.Error(), "first session")
	assert.Contains(t, err.Error(), "second session")
}

// TestBuildChannelMap_LeftOnlyFailFast 釘住「實驗只貼左側 → CCI 應 fail-fast 不 silent
// 用 L.* 算結果」。配合 muscle_ratio 行為對稱，避免使用者誤以為跑出的是右側結果。
func TestBuildChannelMap_LeftOnlyFailFast(t *testing.T) {
	headers := []string{
		"L.RA: EMG 1",
		"L.ES: EMG 2",
		"L.IL: EMG 3",
		"L.GMax: EMG 4",
		"L.RF: EMG 5",
		"L.BF: EMG 6",
		"L.TA&IO: EMG 7",
		"L.MF: EMG 8",
	}

	_, err := BuildChannelMap(headers)
	require.Error(t, err, "只有左側通道 → 缺右側必要肌肉，應 fail-fast")
	assert.Contains(t, err.Error(), "缺少必要的肌肉通道")
}

func TestDefaultMusclePairs(t *testing.T) {
	pairs := DefaultMusclePairs()
	assert.Len(t, pairs, 12)

	// Verify first pair
	assert.Equal(t, "RA/ES", pairs[0].Name)
	assert.Equal(t, "RA", pairs[0].Muscle1)
	assert.Equal(t, "ES", pairs[0].Muscle2)

	// Verify last pair
	assert.Equal(t, "TAIO/GMax", pairs[11].Name)
}

func TestCCIRudolph_MaxValue(t *testing.T) {
	// When both values are equal, CCI = (v/v)*(v+v) = 2v
	// So CCI can exceed 1 if EMG values are > 0.5
	result := CalculateCCIRudolph(0.5, 0.5)
	assert.InDelta(t, 1.0, result, 1e-10)

	// For equal values of 0.25: CCI = 1 * 0.5 = 0.5
	result2 := CalculateCCIRudolph(0.25, 0.25)
	assert.InDelta(t, 0.5, result2, 1e-10)
}

// TestCalculateCCIRudolph_InvalidInputs verifies that NaN/Inf/negative inputs
// return NaN so downstream consumers can detect and reject corrupted data.
// Rudolph 公式假設 rectified（非負）EMG；違反此前提時不該回傳似是而非的數值。
func TestCalculateCCIRudolph_InvalidInputs(t *testing.T) {
	cases := []struct {
		name string
		emg1 float64
		emg2 float64
	}{
		{"emg1_nan", math.NaN(), 0.5},
		{"emg2_nan", 0.5, math.NaN()},
		{"both_nan", math.NaN(), math.NaN()},
		{"emg1_pos_inf", math.Inf(1), 0.5},
		{"emg2_pos_inf", 0.5, math.Inf(1)},
		{"emg1_neg_inf", math.Inf(-1), 0.5},
		{"emg2_neg_inf", 0.5, math.Inf(-1)},
		{"emg1_negative", -0.1, 0.5},
		{"emg2_negative", 0.5, -0.1},
		{"both_negative", -0.3, -0.5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CalculateCCIRudolph(tc.emg1, tc.emg2)
			assert.True(t, math.IsNaN(got),
				"expected NaN for invalid input (emg1=%v, emg2=%v), got %v", tc.emg1, tc.emg2, got)
		})
	}
}
