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

func TestNormalizeToGaitCycle(t *testing.T) {
	// Simple case: 5 points → 101 points
	data := []float64{0, 0.25, 0.5, 0.75, 1.0}
	result := NormalizeToGaitCycle(data, 101)

	require.Len(t, result, 101)
	assert.InDelta(t, 0.0, result[0], 1e-10)
	assert.InDelta(t, 1.0, result[100], 1e-10)
	assert.InDelta(t, 0.5, result[50], 1e-10)
}

func TestNormalizeToGaitCycle_Empty(t *testing.T) {
	result := NormalizeToGaitCycle(nil, 101)
	assert.Nil(t, result)
}

func TestNormalizeToGaitCycle_SinglePoint(t *testing.T) {
	data := []float64{0.5}
	result := NormalizeToGaitCycle(data, 5)

	require.Len(t, result, 5)
	for _, v := range result {
		assert.Equal(t, 0.5, v)
	}
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

func TestComputeMeanAndSD(t *testing.T) {
	curves := [][]float64{
		{1.0, 2.0, 3.0},
		{3.0, 4.0, 5.0},
		{2.0, 3.0, 4.0},
	}

	mean, sd := ComputeMeanAndSD(curves)

	require.Len(t, mean, 3)
	require.Len(t, sd, 3)

	assert.InDelta(t, 2.0, mean[0], 1e-10)
	assert.InDelta(t, 3.0, mean[1], 1e-10)
	assert.InDelta(t, 4.0, mean[2], 1e-10)

	// SD of [1,3,2] = 1.0
	assert.InDelta(t, 1.0, sd[0], 1e-10)
	assert.InDelta(t, 1.0, sd[1], 1e-10)
	assert.InDelta(t, 1.0, sd[2], 1e-10)
}

func TestComputeMeanAndSD_SingleCurve(t *testing.T) {
	curves := [][]float64{{1.0, 2.0, 3.0}}

	mean, sd := ComputeMeanAndSD(curves)

	assert.InDelta(t, 1.0, mean[0], 1e-10)
	assert.InDelta(t, 2.0, mean[1], 1e-10)

	// Single curve: SD should be 0
	for _, v := range sd {
		assert.Equal(t, 0.0, v)
	}
}

func TestComputeMeanAndSD_Empty(t *testing.T) {
	mean, sd := ComputeMeanAndSD(nil)
	assert.Nil(t, mean)
	assert.Nil(t, sd)
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

func TestNormalizeToGaitCycle_Interpolation(t *testing.T) {
	// 3 points: [0, 1, 0], normalize to 5 points
	data := []float64{0, 1, 0}
	result := NormalizeToGaitCycle(data, 5)

	require.Len(t, result, 5)
	assert.InDelta(t, 0.0, result[0], 1e-10)  // 0%
	assert.InDelta(t, 0.5, result[1], 1e-10)  // 25%
	assert.InDelta(t, 1.0, result[2], 1e-10)  // 50%
	assert.InDelta(t, 0.5, result[3], 1e-10)  // 75%
	assert.InDelta(t, 0.0, result[4], 1e-10)  // 100%

	_ = math.Abs(0) // ensure math import is used
}
