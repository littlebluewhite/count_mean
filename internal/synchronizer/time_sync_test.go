package synchronizer

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
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

// TestSliceEMG 釘住 SliceEMG 的毫秒取整規則(ADR-0043):start、end 與 sample 都
// math.Round 到整數毫秒後取含端點區間。
func TestSliceEMG(t *testing.T) {
	data := &models.PhaseSyncEMGData{
		Time:    []float64{0.0, 0.001, 0.002, 0.003, 0.004, 0.005},
		Headers: []string{"Ch1", "Ch2"},
		Channels: map[string][]float64{
			"Ch1": {100, 101, 102, 103, 104, 105},
			"Ch2": {200, 201, 202, 203, 204, 205},
		},
	}

	tests := []struct {
		name       string
		start, end float64
		wantTime   []float64
		wantCh1    []float64
	}{
		{name: "區間內", start: 0.001, end: 0.003,
			wantTime: []float64{0.001, 0.002, 0.003}, wantCh1: []float64{101, 102, 103}},
		{name: "恰為首末筆", start: 0.0, end: 0.005,
			wantTime: data.Time, wantCh1: data.Channels["Ch1"]},
		{name: "開頭一段", start: 0.0, end: 0.002,
			wantTime: []float64{0.0, 0.001, 0.002}, wantCh1: []float64{100, 101, 102}},
		{name: "結尾一段", start: 0.003, end: 0.005,
			wantTime: []float64{0.003, 0.004, 0.005}, wantCh1: []float64{103, 104, 105}},
		{name: "區間大於資料 → 兩端自然收在首末筆", start: -1, end: 10,
			wantTime: data.Time, wantCh1: data.Channels["Ch1"]},
		{name: "start == end 落在 sample 上", start: 0.002, end: 0.002,
			wantTime: []float64{0.002}, wantCh1: []float64{102}},
		{name: "端點與 sample 差 < 0.5ms → 取整到同一毫秒,該 sample 切入", start: 0.0014, end: 0.0026,
			wantTime: []float64{0.001, 0.002, 0.003}, wantCh1: []float64{101, 102, 103}},
		{name: "端點與 sample 差 > 0.5ms → 該 sample 不切入", start: 0.0016, end: 0.0024,
			wantTime: []float64{0.002}, wantCh1: []float64{102}},
		{name: "端點只差同步飄移(1e-7)", start: 0.001 + 1e-7, end: 0.003 - 1e-7,
			wantTime: []float64{0.001, 0.002, 0.003}, wantCh1: []float64{101, 102, 103}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SliceEMG(data, tt.start, tt.end)
			require.NoError(t, err)

			assert.Equal(t, tt.wantTime, got.Data.Time)
			assert.Equal(t, tt.wantCh1, got.Data.Channels["Ch1"])
			assert.Len(t, got.Data.Channels["Ch2"], len(tt.wantTime))
			assert.Equal(t, data.Headers, got.Data.Headers)
			assert.Equal(t, tt.wantTime[0], got.ActualStartTime)
			assert.Equal(t, tt.wantTime[len(tt.wantTime)-1], got.ActualEndTime)
		})
	}
}

// TestSliceEMG_Float32NoiseAtLargeTime 釘住 ADR-0043 選毫秒取整的理由:manifest 的
// 力板時間以 float32 匯出,雜訊隨時間變大(60 s 附近 float32 ULP ≈ 3.8e-6),會超過
// emgTimeEpsilon(1e-6)。毫秒取整下,雜訊端點仍與 sample 取整到同一毫秒,邊界
// sample 照樣切入;若改用 ±1e-6,下列每一列都會靜默少切 60.12 這一筆。
func TestSliceEMG_Float32NoiseAtLargeTime(t *testing.T) {
	data := &models.PhaseSyncEMGData{ // 100 Hz
		Time:     []float64{60.10, 60.11, 60.12, 60.13, 60.14},
		Headers:  []string{"Ch1"},
		Channels: map[string][]float64{"Ch1": {0, 1, 2, 3, 4}},
	}

	tests := []struct {
		name       string
		start, end float64
		wantTime   []float64
	}{
		{name: "終點 60.119997(−3e-6)保留 60.12", start: 60.10, end: 60.119997,
			wantTime: []float64{60.10, 60.11, 60.12}},
		{name: "終點 float32(60.12) = 60.1199989…(−1.07e-6)保留 60.12", start: 60.10, end: float64(float32(60.12)),
			wantTime: []float64{60.10, 60.11, 60.12}},
		{name: "起點 60.120003(+3e-6)保留 60.12", start: 60.120003, end: 60.14,
			wantTime: []float64{60.12, 60.13, 60.14}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SliceEMG(data, tt.start, tt.end)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTime, got.Data.Time)
		})
	}
}

// TestSliceEMG_Errors 釘住三種失敗的 sentinel 與錯誤全文(沿用舊
// parsers.GetEMGDataInTimeRange / FindTimeRangeIndices 的 user-facing 字樣)。
func TestSliceEMG_Errors(t *testing.T) {
	data := &models.PhaseSyncEMGData{
		Time:     []float64{0.0, 0.001, 0.002},
		Headers:  []string{"Ch1"},
		Channels: map[string][]float64{"Ch1": {1, 2, 3}},
	}
	sparse := &models.PhaseSyncEMGData{ // 100 Hz
		Time:     []float64{0.0, 0.01, 0.02},
		Headers:  []string{"Ch1"},
		Channels: map[string][]float64{"Ch1": {1, 2, 3}},
	}

	tests := []struct {
		name       string
		data       *models.PhaseSyncEMGData
		start, end float64
		wantErr    error // nil = 不檢查 sentinel
		wantText   string
	}{
		{name: "nil data", data: nil, start: 0, end: 1,
			wantErr: parsers.ErrNilData, wantText: "EMG 數據為空: data is nil"},
		{name: "空 Time", data: &models.PhaseSyncEMGData{Time: []float64{}}, start: 0, end: 1,
			wantErr: parsers.ErrNilData, wantText: "EMG 數據為空: data is nil"},
		{name: "start > end", data: data, start: 0.002, end: 0.001,
			wantText: "開始時間 0.002 不能大於結束時間 0.001"},
		{name: "區間整段在資料之後", data: data, start: 0.010, end: 0.020,
			wantErr: ErrTimeRangeNotFound, wantText: "找不到有效的時間範圍數據: no data found in time range"},
		{name: "區間整段在資料之前", data: data, start: -2, end: -1,
			wantErr: ErrTimeRangeNotFound, wantText: "找不到有效的時間範圍數據: no data found in time range"},
		{name: "區間夾在兩筆 sample 之間(取整後也碰不到)", data: sparse, start: 0.003, end: 0.007,
			wantErr: ErrTimeRangeNotFound, wantText: "找不到有效的時間範圍數據: no data found in time range"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SliceEMG(tt.data, tt.start, tt.end)
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Equal(t, tt.wantText, err.Error())
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

// TestSliceEMG_SubMillisecondEndBoundary 記錄毫秒取整規則在 sample interval < 1 ms
// 時的已知行為(ADR-0043 的限制):end 與 sample 都取整到毫秒,所以 end 之後不到
// 0.5 ms、且取整到同一毫秒的 sample 也會切入。以 2.5 kHz 時間軸為例:end = 1.0008 與
// 1.0012 都取整到 1001 ms,1.0012 一併切入;start 側對稱,1.0008 會隨 start = 1.0012
// 切入。
func TestSliceEMG_SubMillisecondEndBoundary(t *testing.T) {
	data := &models.PhaseSyncEMGData{
		Time:     []float64{1.0, 1.0004, 1.0008, 1.0012, 1.0016},
		Headers:  []string{"Ch1"},
		Channels: map[string][]float64{"Ch1": {0, 1, 2, 3, 4}},
	}

	t.Run("end 之後 0.4ms、同一毫秒的 sample 切入", func(t *testing.T) {
		got, err := SliceEMG(data, 1.0, 1.0008)
		require.NoError(t, err)
		assert.Equal(t, []float64{1.0, 1.0004, 1.0008, 1.0012}, got.Data.Time)
		assert.Equal(t, 1.0012, got.ActualEndTime)
	})

	t.Run("start 之前 0.4ms、同一毫秒的 sample 切入", func(t *testing.T) {
		got, err := SliceEMG(data, 1.0012, 1.0016)
		require.NoError(t, err)
		assert.Equal(t, []float64{1.0008, 1.0012, 1.0016}, got.Data.Time)
		assert.Equal(t, 1.0008, got.ActualStartTime)
	})

	t.Run("end 之後 0.4ms、但取整到下一毫秒的 sample 不切入", func(t *testing.T) {
		got, err := SliceEMG(data, 1.0, 1.0004)
		require.NoError(t, err)
		assert.Equal(t, []float64{1.0, 1.0004}, got.Data.Time)
	})
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
