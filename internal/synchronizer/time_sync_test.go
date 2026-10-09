package synchronizer

import (
	"math"
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

func TestOutsideEMG(t *testing.T) {
	times := []float64{0.2, 0.7, 1.2}

	tests := []struct {
		name   string
		times  []float64
		t      float64
		before bool
		after  bool
	}{
		{name: "首筆", times: times, t: 0.2},
		{name: "末筆", times: times, t: 1.2},
		{name: "中間(非 sample)", times: times, t: 0.45},
		{name: "低於首筆、容差內", times: times, t: 0.2 - 0.5e-6},
		{name: "高於末筆、容差內", times: times, t: 1.2 + 0.5e-6},
		{name: "低於首筆、超出容差", times: times, t: 0.2 - 2e-6, before: true},
		{name: "高於末筆、超出容差", times: times, t: 1.2 + 2e-6, after: true},
		{name: "遠低於首筆", times: times, t: -1, before: true},
		{name: "遠高於末筆", times: times, t: 5, after: true},
		{name: "-Inf", times: times, t: math.Inf(-1), before: true},
		{name: "+Inf", times: times, t: math.Inf(1), after: true},
		{name: "NaN 無從比較 → 兩側皆越界", times: times, t: math.NaN(), before: true, after: true},
		{name: "空時間軸 → 兩側皆越界", times: []float64{}, t: 0, before: true, after: true},
		{name: "nil 時間軸 → 兩側皆越界", times: nil, t: 0, before: true, after: true},
		{name: "單筆、容差內", times: []float64{0.5}, t: 0.5 + 0.5e-6},
		{name: "單筆、低於", times: []float64{0.5}, t: 0.4, before: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before, after := OutsideEMG(tt.times, tt.t)
			assert.Equal(t, tt.before, before, "before")
			assert.Equal(t, tt.after, after, "after")

			// 與 ResolveTimeIndex 同一條規則:inRange == !before && !after。
			_, inRange := ResolveTimeIndex(tt.times, tt.t)
			assert.Equal(t, !before && !after, inRange, "ResolveTimeIndex inRange 須與 OutsideEMG 一致")
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
