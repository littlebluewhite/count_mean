package calculator

import (
	"errors"
	"math"
	"testing"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/models"

	"github.com/stretchr/testify/require"
)

// TestCalculateStatistics_NaNChannel_FailFast 驗證生產唯一咽喉點:
// CalculateStatistics → ValidateEMGData 必須在 NaN 通道進入 util.ArrayMean/ArrayMax
// 之前 fail-fast,並讓 errors.Is 穿透 "EMG 數據驗證失敗: %w" 包裝取到 sentinel。
//
// 這一個 calculator 級測試即同時覆蓋:
//   - 常規 phase_sync handler(呼 CalculateStatistics)
//   - normalized phase_sync handler(同樣經 CalculateStatistics)
//
// 不需另寫 GUI handler 測試(setup 過重且 handler 把 err 轉 (failedResult, nil))。
func TestCalculateStatistics_NaNChannel_FailFast(t *testing.T) {
	calc := NewEMGStatisticsCalculator()
	data := &models.PhaseSyncEMGData{
		Time:    []float64{0.0, 0.001, 0.002},
		Headers: []string{"Ch1"},
		Channels: map[string][]float64{
			"Ch1": {1.0, math.NaN(), 3.0},
		},
	}
	params := StatisticsParams{
		Subject:    "test-subject",
		StartPhase: models.PhaseP0,
		StartTime:  0.0,
		EndPhase:   models.PhaseL,
		EndTime:    0.002,
	}

	_, err := calc.CalculateStatistics(data, params)
	require.Error(t, err, "NaN 通道必須 fail-fast,不可 silently 寫進統計輸出")
	require.True(t, errors.Is(err, calcerrors.ErrNaNInChannel),
		"errors.Is 必須穿透 '%%w' 包裝取到 ErrNaNInChannel,實際: %v", err)
}

// TestFormatStatisticsReport:PhaseSync 與 NormalizedPhaseSync 共用的統計報告
// (原經 phase_sync.GenerateAnalysisReport 轉呼叫測試,該別名已刪,ADR-0047)。
func TestFormatStatisticsReport(t *testing.T) {
	stats := &models.EMGStatistics{
		Subject:      "TestSubject",
		StartPhase:   "P0",
		EndPhase:     "P2",
		StartTime:    0.0,
		EndTime:      2.0,
		ChannelNames: []string{"Ch1", "Ch2"},
		ChannelMeans: map[string]float64{
			"Ch1": 100.5,
			"Ch2": 200.3,
		},
		ChannelMaxes: map[string]float64{
			"Ch1": 150.0,
			"Ch2": 250.0,
		},
	}

	report := FormatStatisticsReport(stats)
	require.NotEmpty(t, report)
	require.Contains(t, report, "TestSubject")
	require.Contains(t, report, "P0")
	require.Contains(t, report, "P2")
}
