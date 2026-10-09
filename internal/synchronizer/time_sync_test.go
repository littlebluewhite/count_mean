package synchronizer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTimeSynchronizer(t *testing.T) {
	ts := NewTimeSynchronizer()
	assert.NotNil(t, ts)
}

func TestResolveTimeIndex(t *testing.T) {
	times := []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}

	tests := []struct {
		name       string
		times      []float64
		targetTime float64
		expected   int
		inRange    bool
	}{
		{
			name:       "exact match at start",
			times:      times,
			targetTime: 0.0,
			expected:   0,
			inRange:    true,
		},
		{
			name:       "exact match at end",
			times:      times,
			targetTime: 1.0,
			expected:   10,
			inRange:    true,
		},
		{
			name:       "exact match in middle",
			times:      times,
			targetTime: 0.5,
			expected:   5,
			inRange:    true,
		},
		{
			name:       "target before start",
			times:      times,
			targetTime: -0.1,
			expected:   0,
			inRange:    false,
		},
		{
			name:       "target after end",
			times:      times,
			targetTime: 1.1,
			expected:   10,
			inRange:    false,
		},
		{
			name:       "target between values - closer to left",
			times:      times,
			targetTime: 0.12,
			expected:   1, // 0.12 is closer to 0.1 than 0.2
			inRange:    true,
		},
		{
			name:       "target between values - closer to right",
			times:      times,
			targetTime: 0.18,
			expected:   2, // 0.18 is closer to 0.2 than 0.1
			inRange:    true,
		},
		{
			name:       "target exactly between values",
			times:      times,
			targetTime: 0.15,
			expected:   1, // When exactly between, should return left index
			inRange:    true,
		},
		{
			name:       "empty array",
			times:      []float64{},
			targetTime: 0.5,
			expected:   -1,
			inRange:    false,
		},
		{
			name:       "single element - match",
			times:      []float64{0.5},
			targetTime: 0.5,
			expected:   0,
			inRange:    true,
		},
		{
			name:       "single element - no match",
			times:      []float64{0.5},
			targetTime: 0.3,
			expected:   0,
			inRange:    false,
		},
		{
			name:       "epsilon below start within tolerance",
			times:      times,
			targetTime: times[0] - 0.5e-6,
			expected:   0,
			inRange:    true,
		},
		{
			name:       "epsilon above end within tolerance",
			times:      times,
			targetTime: times[len(times)-1] + 0.5e-6,
			expected:   10,
			inRange:    true,
		},
		{
			name:       "below start beyond tolerance",
			times:      times,
			targetTime: times[0] - 2e-6,
			expected:   0,
			inRange:    false,
		},
		{
			name:       "above end beyond tolerance",
			times:      times,
			targetTime: times[len(times)-1] + 2e-6,
			expected:   10,
			inRange:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, inRange := ResolveTimeIndex(tt.times, tt.targetTime)
			assert.Equal(t, tt.expected, idx)
			assert.Equal(t, tt.inRange, inRange)
		})
	}
}

func BenchmarkResolveTimeIndex(b *testing.B) {
	times := make([]float64, 10000)
	for i := 0; i < 10000; i++ {
		times[i] = float64(i) * 0.001
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = ResolveTimeIndex(times, 5.0)
	}
}
