package cci

import (
	"math"
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

	result, err := CalculateCCITimeSeries(ch1, ch2)
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

	_, err := CalculateCCITimeSeries(ch1, ch2)
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
