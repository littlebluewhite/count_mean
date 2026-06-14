package calculator

import (
	"errors"
	"math"
	"testing"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/models"

	"github.com/stretchr/testify/assert"
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

// TestGenerateOutputFileName_LiteralCharacterization 逐字釘住自動檔名,作為
// ADR-0019 把本函數改呼 filename.SubjectOutputName 前的 byte-identity gate。
// 不可用 GenerateOutputFileName 自身算 expected(套套邏輯,bug 會兩邊同變)。
func TestGenerateOutputFileName_LiteralCharacterization(t *testing.T) {
	// PhasePoint 是 type PhasePoint string(PhaseP0="P0"、PhaseL="L"、PhaseS="S"、PhaseT="T")。
	assert.Equal(t,
		"subject_01_P0-L_statistics.csv",
		GenerateOutputFileName("subject_01", models.PhaseP0, models.PhaseL),
	)
	// subject 帶分隔符 → Sanitize 收斂單一 segment(安全不變量逐字)。
	assert.Equal(t,
		"a_b_S-T_statistics.csv",
		GenerateOutputFileName("a/b", models.PhaseS, models.PhaseT),
	)
}
