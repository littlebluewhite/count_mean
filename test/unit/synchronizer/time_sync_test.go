package synchronizer

import (
	"testing"

	"count_mean/internal/synchronizer"

	"github.com/stretchr/testify/assert"
)

func TestNewTimeSynchronizer(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()
	assert.NotNil(t, ts)
}

func TestTimeSynchronizer_MotionIndexToTime(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name     string
		index    int
		expected float64
	}{
		{
			name:     "index 1 (first index)",
			index:    1,
			expected: 0.0, // (1-1)/250 = 0
		},
		{
			name:     "index 2",
			index:    2,
			expected: 0.004, // (2-1)/250 = 0.004
		},
		{
			name:     "index 250",
			index:    250,
			expected: 0.996, // (250-1)/250 = 0.996
		},
		{
			name:     "index 251",
			index:    251,
			expected: 1.0, // (251-1)/250 = 1.0
		},
		{
			name:     "index 0 (invalid)",
			index:    0,
			expected: 0.0, // Should return 0 for invalid index
		},
		{
			name:     "negative index",
			index:    -1,
			expected: 0.0, // Should return 0 for negative index
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.MotionIndexToTime(tt.index)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestTimeSynchronizer_TimeToMotionIndex(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name     string
		time     float64
		expected int
	}{
		{
			name:     "time 0.0",
			time:     0.0,
			expected: 1, // 0*250 + 1 = 1
		},
		{
			name:     "time 0.004",
			time:     0.004,
			expected: 2, // round(0.004*250) + 1 = 2
		},
		{
			name:     "time 0.996",
			time:     0.996,
			expected: 250, // round(0.996*250) + 1 = 250
		},
		{
			name:     "time 1.0",
			time:     1.0,
			expected: 251, // round(1.0*250) + 1 = 251
		},
		{
			name:     "time 0.002 (rounding test)",
			time:     0.002,
			expected: 2, // round(0.002*250) + 1 = round(0.5) + 1 = 2
		},
		{
			name:     "time 0.006 (rounding test)",
			time:     0.006,
			expected: 3, // round(0.006*250) + 1 = round(1.5) + 1 = 3
		},
		{
			name:     "negative time",
			time:     -1.0,
			expected: 1, // Should return 1 for negative time
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.TimeToMotionIndex(tt.time)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTimeSynchronizer_MotionIndexToEMGTime(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name            string
		motionIndex     int
		emgMotionOffset int
		expected        float64
	}{
		{
			name:            "motion index 100, offset 100",
			motionIndex:     100,
			emgMotionOffset: 100,
			expected:        0.0, // (100-100)/250 = 0
		},
		{
			name:            "motion index 150, offset 100",
			motionIndex:     150,
			emgMotionOffset: 100,
			expected:        0.2, // (150-100)/250 = 0.2
		},
		{
			name:            "motion index 350, offset 100",
			motionIndex:     350,
			emgMotionOffset: 100,
			expected:        1.0, // (350-100)/250 = 1.0
		},
		{
			name:            "motion index 50, offset 100",
			motionIndex:     50,
			emgMotionOffset: 100,
			expected:        -0.2, // (50-100)/250 = -0.2
		},
		{
			name:            "motion index 250, offset 0",
			motionIndex:     250,
			emgMotionOffset: 0,
			expected:        1.0, // (250-0)/250 = 1.0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.MotionIndexToEMGTime(tt.motionIndex, tt.emgMotionOffset)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestTimeSynchronizer_EMGTimeToMotionIndex(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name            string
		emgTime         float64
		emgMotionOffset int
		expected        int
	}{
		{
			name:            "EMG time 0.0, offset 100",
			emgTime:         0.0,
			emgMotionOffset: 100,
			expected:        100, // round(0.0*250) + 100 = 100
		},
		{
			name:            "EMG time 0.2, offset 100",
			emgTime:         0.2,
			emgMotionOffset: 100,
			expected:        150, // round(0.2*250) + 100 = 150
		},
		{
			name:            "EMG time 1.0, offset 100",
			emgTime:         1.0,
			emgMotionOffset: 100,
			expected:        350, // round(1.0*250) + 100 = 350
		},
		{
			name:            "EMG time -0.2, offset 100",
			emgTime:         -0.2,
			emgMotionOffset: 100,
			expected:        50, // round(-0.2*250) + 100 = 50
		},
		{
			name:            "EMG time 1.0, offset 0",
			emgTime:         1.0,
			emgMotionOffset: 0,
			expected:        250, // round(1.0*250) + 0 = 250
		},
		{
			name:            "result less than 1",
			emgTime:         -1.0,
			emgMotionOffset: 100,
			expected:        1, // Should return 1 for results < 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.EMGTimeToMotionIndex(tt.emgTime, tt.emgMotionOffset)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTimeSynchronizer_ForceTimeToMotionIndex(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name      string
		forceTime float64
		expected  int
	}{
		{
			name:      "force time 0.0",
			forceTime: 0.0,
			expected:  1,
		},
		{
			name:      "force time 0.004",
			forceTime: 0.004,
			expected:  2,
		},
		{
			name:      "force time 1.0",
			forceTime: 1.0,
			expected:  251,
		},
		{
			name:      "negative force time",
			forceTime: -1.0,
			expected:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.ForceTimeToMotionIndex(tt.forceTime)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTimeSynchronizer_MotionIndexToForceTime(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name        string
		motionIndex int
		expected    float64
	}{
		{
			name:        "motion index 1",
			motionIndex: 1,
			expected:    0.0,
		},
		{
			name:        "motion index 2",
			motionIndex: 2,
			expected:    0.004,
		},
		{
			name:        "motion index 251",
			motionIndex: 251,
			expected:    1.0,
		},
		{
			name:        "motion index 0",
			motionIndex: 0,
			expected:    0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.MotionIndexToForceTime(tt.motionIndex)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestTimeSynchronizer_ForceTimeToEMGTime(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name            string
		forceTime       float64
		emgMotionOffset int
		expected        float64
	}{
		{
			name:            "force time 0.0, offset 100",
			forceTime:       0.0,
			emgMotionOffset: 100,
			expected:        -0.396, // ((0*250+1)-100)/250 = -0.396
		},
		{
			name:            "force time 0.4, offset 100",
			forceTime:       0.4,
			emgMotionOffset: 100,
			expected:        0.004, // ((0.4*250+1)-100)/250 = 0.004
		},
		{
			name:            "force time 1.0, offset 100",
			forceTime:       1.0,
			emgMotionOffset: 100,
			expected:        0.604, // ((1.0*250+1)-100)/250 = 0.604
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.ForceTimeToEMGTime(tt.forceTime, tt.emgMotionOffset)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

func TestTimeSynchronizer_EMGTimeToForceTime(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name            string
		emgTime         float64
		emgMotionOffset int
		expected        float64
	}{
		{
			name:            "EMG time 0.0, offset 100",
			emgTime:         0.0,
			emgMotionOffset: 100,
			expected:        0.396, // (100-1)/250 = 0.396
		},
		{
			name:            "EMG time 0.2, offset 100",
			emgTime:         0.2,
			emgMotionOffset: 100,
			expected:        0.596, // (150-1)/250 = 0.596
		},
		{
			name:            "EMG time 1.0, offset 100",
			emgTime:         1.0,
			emgMotionOffset: 100,
			expected:        1.396, // (350-1)/250 = 1.396
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ts.EMGTimeToForceTime(tt.emgTime, tt.emgMotionOffset)
			assert.InDelta(t, tt.expected, result, 0.001)
		})
	}
}

func TestTimeSynchronizer_GetSyncedTimeRange(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name               string
		startValue         float64
		startIsMotionIndex bool
		endValue           float64
		endIsMotionIndex   bool
		emgMotionOffset    int
		expectErr          bool
		errorMsg           string
		checkResult        func(*testing.T, *synchronizer.SyncedTimeRange)
	}{
		{
			name:               "force to force",
			startValue:         1.0,
			startIsMotionIndex: false,
			endValue:           2.0,
			endIsMotionIndex:   false,
			emgMotionOffset:    100,
			expectErr:          false,
			checkResult: func(t *testing.T, result *synchronizer.SyncedTimeRange) {
				assert.Equal(t, 1.0, result.StartForceTime)
				assert.Equal(t, 2.0, result.EndForceTime)
				assert.Greater(t, result.EndEMGTime, result.StartEMGTime)
			},
		},
		{
			name:               "motion to motion",
			startValue:         250,
			startIsMotionIndex: true,
			endValue:           350,
			endIsMotionIndex:   true,
			emgMotionOffset:    100,
			expectErr:          false,
			checkResult: func(t *testing.T, result *synchronizer.SyncedTimeRange) {
				assert.Equal(t, 250, result.StartMotionIndex)
				assert.Equal(t, 350, result.EndMotionIndex)
				assert.Greater(t, result.EndEMGTime, result.StartEMGTime)
			},
		},
		{
			name:               "force to motion",
			startValue:         1.0,
			startIsMotionIndex: false,
			endValue:           350,
			endIsMotionIndex:   true,
			emgMotionOffset:    100,
			expectErr:          false,
			checkResult: func(t *testing.T, result *synchronizer.SyncedTimeRange) {
				assert.Equal(t, 1.0, result.StartForceTime)
				assert.Equal(t, 350, result.EndMotionIndex)
				assert.Greater(t, result.EndEMGTime, result.StartEMGTime)
			},
		},
		{
			name:               "motion to force",
			startValue:         250,
			startIsMotionIndex: true,
			endValue:           2.0,
			endIsMotionIndex:   false,
			emgMotionOffset:    100,
			expectErr:          false,
			checkResult: func(t *testing.T, result *synchronizer.SyncedTimeRange) {
				assert.Equal(t, 250, result.StartMotionIndex)
				assert.Equal(t, 2.0, result.EndForceTime)
				assert.Greater(t, result.EndEMGTime, result.StartEMGTime)
			},
		},
		{
			name:               "invalid time range (start > end)",
			startValue:         500,
			startIsMotionIndex: true,
			endValue:           250,
			endIsMotionIndex:   true,
			emgMotionOffset:    100,
			expectErr:          true,
			errorMsg:           "開始時間",
		},
		{
			name:               "zero offset",
			startValue:         1.0,
			startIsMotionIndex: false,
			endValue:           2.0,
			endIsMotionIndex:   false,
			emgMotionOffset:    0,
			expectErr:          false,
			checkResult: func(t *testing.T, result *synchronizer.SyncedTimeRange) {
				assert.Equal(t, 1.0, result.StartForceTime)
				assert.Equal(t, 2.0, result.EndForceTime)
				assert.Greater(t, result.EndEMGTime, result.StartEMGTime)
			},
		},
		{
			name:               "negative offset",
			startValue:         1.0,
			startIsMotionIndex: false,
			endValue:           2.0,
			endIsMotionIndex:   false,
			emgMotionOffset:    -50,
			expectErr:          false,
			checkResult: func(t *testing.T, result *synchronizer.SyncedTimeRange) {
				assert.Equal(t, 1.0, result.StartForceTime)
				assert.Equal(t, 2.0, result.EndForceTime)
				assert.Greater(t, result.EndEMGTime, result.StartEMGTime)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ts.GetSyncedTimeRange(
				tt.startValue, tt.startIsMotionIndex,
				tt.endValue, tt.endIsMotionIndex,
				tt.emgMotionOffset,
			)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, result)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)

				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestFindNearestTimeIndex(t *testing.T) {
	times := []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}

	tests := []struct {
		name       string
		times      []float64
		targetTime float64
		expected   int
	}{
		{
			name:       "exact match at start",
			times:      times,
			targetTime: 0.0,
			expected:   0,
		},
		{
			name:       "exact match at end",
			times:      times,
			targetTime: 1.0,
			expected:   10,
		},
		{
			name:       "exact match in middle",
			times:      times,
			targetTime: 0.5,
			expected:   5,
		},
		{
			name:       "target before start",
			times:      times,
			targetTime: -0.1,
			expected:   0,
		},
		{
			name:       "target after end",
			times:      times,
			targetTime: 1.1,
			expected:   10,
		},
		{
			name:       "target between values - closer to left",
			times:      times,
			targetTime: 0.12,
			expected:   1, // 0.12 is closer to 0.1 than 0.2
		},
		{
			name:       "target between values - closer to right",
			times:      times,
			targetTime: 0.18,
			expected:   2, // 0.18 is closer to 0.2 than 0.1
		},
		{
			name:       "target exactly between values",
			times:      times,
			targetTime: 0.15,
			expected:   1, // When exactly between, should return left index
		},
		{
			name:       "empty array",
			times:      []float64{},
			targetTime: 0.5,
			expected:   -1,
		},
		{
			name:       "single element - match",
			times:      []float64{0.5},
			targetTime: 0.5,
			expected:   0,
		},
		{
			name:       "single element - no match",
			times:      []float64{0.5},
			targetTime: 0.3,
			expected:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := synchronizer.FindNearestTimeIndex(tt.times, tt.targetTime)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateTimeSync(t *testing.T) {
	tests := []struct {
		name             string
		emgMotionOffset  int
		emgDataLength    int
		motionDataLength int
		expectErr        bool
		errorMsg         string
	}{
		{
			name:             "valid parameters",
			emgMotionOffset:  100,
			emgDataLength:    1000,
			motionDataLength: 500,
			expectErr:        false,
		},
		{
			name:             "zero offset",
			emgMotionOffset:  0,
			emgDataLength:    1000,
			motionDataLength: 500,
			expectErr:        false,
		},
		{
			name:             "negative offset",
			emgMotionOffset:  -1,
			emgDataLength:    1000,
			motionDataLength: 500,
			expectErr:        true,
			errorMsg:         "EMG Motion Offset 不能為負數",
		},
		{
			name:             "offset exceeds motion length",
			emgMotionOffset:  600,
			emgDataLength:    1000,
			motionDataLength: 500,
			expectErr:        true,
			errorMsg:         "EMG Motion Offset (600) 超過 Motion 數據長度 (500)",
		},
		{
			name:             "offset equals motion length",
			emgMotionOffset:  500,
			emgDataLength:    1000,
			motionDataLength: 500,
			expectErr:        false,
		},
		{
			name:             "edge case - all zeros",
			emgMotionOffset:  0,
			emgDataLength:    0,
			motionDataLength: 0,
			expectErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := synchronizer.ValidateTimeSync(
				tt.emgMotionOffset,
				tt.emgDataLength,
				tt.motionDataLength,
			)

			if tt.expectErr {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test round-trip conversions
func TestTimeSynchronizer_RoundTripConversions(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	tests := []struct {
		name            string
		initialValue    float64
		emgMotionOffset int
	}{
		{
			name:            "time 0.0",
			initialValue:    0.0,
			emgMotionOffset: 100,
		},
		{
			name:            "time 1.0",
			initialValue:    1.0,
			emgMotionOffset: 100,
		},
		{
			name:            "time 0.5",
			initialValue:    0.5,
			emgMotionOffset: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Round-trip: Time -> MotionIndex -> Time
			motionIndex := ts.TimeToMotionIndex(tt.initialValue)
			backToTime := ts.MotionIndexToTime(motionIndex)
			assert.InDelta(t, tt.initialValue, backToTime, 0.01) // Allow small rounding error

			// Round-trip: EMGTime -> MotionIndex -> EMGTime
			motionIndex2 := ts.EMGTimeToMotionIndex(tt.initialValue, tt.emgMotionOffset)
			backToEMGTime := ts.MotionIndexToEMGTime(motionIndex2, tt.emgMotionOffset)
			assert.InDelta(t, tt.initialValue, backToEMGTime, 0.01)
		})
	}
}

// Benchmark tests
func BenchmarkTimeSynchronizer_MotionIndexToTime(b *testing.B) {
	ts := synchronizer.NewTimeSynchronizer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ts.MotionIndexToTime(250)
	}
}

func BenchmarkTimeSynchronizer_TimeToMotionIndex(b *testing.B) {
	ts := synchronizer.NewTimeSynchronizer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ts.TimeToMotionIndex(1.0)
	}
}

func BenchmarkTimeSynchronizer_GetSyncedTimeRange(b *testing.B) {
	ts := synchronizer.NewTimeSynchronizer()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ts.GetSyncedTimeRange(1.0, false, 2.0, false, 100)
	}
}

func BenchmarkFindNearestTimeIndex(b *testing.B) {
	times := make([]float64, 10000)
	for i := 0; i < 10000; i++ {
		times[i] = float64(i) * 0.001
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = synchronizer.FindNearestTimeIndex(times, 5.0)
	}
}

// Test precision and edge cases
func TestTimeSynchronizer_PrecisionAndEdgeCases(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	// Test precision with very small values
	t.Run("small values", func(t *testing.T) {
		smallTime := 0.001
		index := ts.TimeToMotionIndex(smallTime)
		backToTime := ts.MotionIndexToTime(index)
		assert.InDelta(t, smallTime, backToTime, 0.01)
	})

	// Test precision with large values
	t.Run("large values", func(t *testing.T) {
		largeTime := 100.0
		index := ts.TimeToMotionIndex(largeTime)
		backToTime := ts.MotionIndexToTime(index)
		assert.InDelta(t, largeTime, backToTime, 0.01)
	})

	// Test rounding behavior
	t.Run("rounding behavior", func(t *testing.T) {
		// Test exact half-values
		halfTime := 0.002 // Exactly 0.5 index units
		index := ts.TimeToMotionIndex(halfTime)
		// Should round to nearest even or follow math.Round behavior
		assert.True(t, index == 1 || index == 2)
	})
}

// Test concurrent access
func TestTimeSynchronizer_ConcurrentAccess(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	// Run multiple goroutines simultaneously
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				// Test various conversions
				_ = ts.MotionIndexToTime(250)
				_ = ts.TimeToMotionIndex(1.0)
				_ = ts.MotionIndexToEMGTime(250, 100)
				_ = ts.EMGTimeToMotionIndex(1.0, 100)
				_, _ = ts.GetSyncedTimeRange(1.0, false, 2.0, false, 100)
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

// Test with extreme values
func TestTimeSynchronizer_ExtremeValues(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	// Test with very large motion index
	largeIndex := 1000000
	time := ts.MotionIndexToTime(largeIndex)
	assert.Greater(t, time, 0.0)

	// Test with very large time
	largeTime := 1000.0
	index := ts.TimeToMotionIndex(largeTime)
	assert.Greater(t, index, 0)

	// Test with very large EMG offset
	largeOffset := 1000000
	emgTime := ts.MotionIndexToEMGTime(1000100, largeOffset)
	assert.InDelta(t, 0.4, emgTime, 0.01)
}

// Test mathematical relationships
func TestTimeSynchronizer_MathematicalRelationships(t *testing.T) {
	ts := synchronizer.NewTimeSynchronizer()

	// Test frequency relationships
	t.Run("frequency consistency", func(t *testing.T) {
		// 250Hz means 250 samples per second
		// So 1 second should correspond to 250 indices
		oneSecond := 1.0
		index := ts.TimeToMotionIndex(oneSecond)
		expectedIndex := 251 // 250 + 1 (1-based indexing)
		assert.Equal(t, expectedIndex, index)
	})

	// Test symmetry
	t.Run("conversion symmetry", func(t *testing.T) {
		originalTime := 2.5
		index := ts.TimeToMotionIndex(originalTime)
		backToTime := ts.MotionIndexToTime(index)

		// Should be very close due to quantization
		assert.InDelta(t, originalTime, backToTime, 0.01)
	})

	// Test EMG-Motion relationship
	t.Run("EMG-Motion relationship", func(t *testing.T) {
		emgTime := 1.0
		offset := 100
		motionIndex := ts.EMGTimeToMotionIndex(emgTime, offset)
		backToEMGTime := ts.MotionIndexToEMGTime(motionIndex, offset)

		assert.InDelta(t, emgTime, backToEMGTime, 0.01)
	})
}
