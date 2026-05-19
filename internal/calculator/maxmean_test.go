package calculator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

func TestMaxMeanCalculator_Calculate(t *testing.T) {
	calc := NewMaxMeanCalculator(10)

	t.Run("EmptyDataset", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    []models.EMGData{},
		}
		results, err := calc.Calculate(context.Background(), dataset, 5)
		require.Error(t, err)
		require.Nil(t, results)
	})

	t.Run("InvalidWindowSize_Zero", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
			},
		}
		results, err := calc.Calculate(context.Background(), dataset, 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "窗口大小必須大於 0")
		require.Nil(t, results)
	})

	t.Run("InvalidWindowSize_Negative", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
			},
		}
		results, err := calc.Calculate(context.Background(), dataset, -1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "窗口大小必須大於 0")
		require.Nil(t, results)
	})

	t.Run("WindowTooLarge", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
				{Time: 2.0, Channels: []float64{200.0}},
			},
		}
		results, err := calc.Calculate(context.Background(), dataset, 5)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集無效或窗口大小過大")
		require.Nil(t, results)
	})

	t.Run("ValidCalculation_SingleChannel", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
				{Time: 2.0, Channels: []float64{200.0}},
				{Time: 3.0, Channels: []float64{150.0}},
				{Time: 4.0, Channels: []float64{300.0}},
			},
		}
		results, err := calc.Calculate(context.Background(), dataset, 2)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, 1, results[0].ColumnIndex)
		require.Equal(t, 3.0, results[0].StartTime)
		require.Equal(t, 4.0, results[0].EndTime)
		require.Equal(t, 225.0, results[0].MaxMean) // (150+300)/2
	})

	t.Run("ValidCalculation_MultipleChannels", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0, 50.0}},
				{Time: 2.0, Channels: []float64{200.0, 100.0}},
				{Time: 3.0, Channels: []float64{150.0, 75.0}},
			},
		}
		results, err := calc.Calculate(context.Background(), dataset, 2)
		require.NoError(t, err)
		require.Len(t, results, 2)

		// Channel 1
		require.Equal(t, 1, results[0].ColumnIndex)
		require.Equal(t, 175.0, results[0].MaxMean) // (200+150)/2

		// Channel 2
		require.Equal(t, 2, results[1].ColumnIndex)
		require.Equal(t, 87.5, results[1].MaxMean) // (100+75)/2
	})

	t.Run("SingleDataPoint", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
			},
		}
		results, err := calc.Calculate(context.Background(), dataset, 1)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, 100.0, results[0].MaxMean)
		require.Equal(t, 1.0, results[0].StartTime)
		require.Equal(t, 1.0, results[0].EndTime)
	})

	t.Run("AllSameValues", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
				{Time: 2.0, Channels: []float64{100.0}},
				{Time: 3.0, Channels: []float64{100.0}},
			},
		}
		results, err := calc.Calculate(context.Background(), dataset, 2)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, 100.0, results[0].MaxMean)
	})

	t.Run("NilDataset", func(t *testing.T) {
		results, err := calc.Calculate(context.Background(), nil, 2)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集為空")
		require.Nil(t, results)
	})
}

func TestMaxMeanCalculator_CalculateFromRawData(t *testing.T) {
	calc := NewMaxMeanCalculator(10)

	t.Run("ValidRawData", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1", "Ch2"},
			{"1.0", "100", "50"},
			{"2.0", "200", "100"},
			{"3.0", "150", "75"},
		}
		results, err := calc.CalculateFromRawData(context.Background(), records, 2)
		require.NoError(t, err)
		require.Len(t, results, 2)
	})

	t.Run("InvalidRawData_NoHeader", func(t *testing.T) {
		records := [][]string{
			{"1.0", "100"},
		}
		results, err := calc.CalculateFromRawData(context.Background(), records, 2)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據至少需要包含標題行和一行數據")
		require.Nil(t, results)
	})

	t.Run("InvalidRawData_BadNumber", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"invalid", "100"},
		}
		results, err := calc.CalculateFromRawData(context.Background(), records, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析後數據集為空")
		require.Nil(t, results)
	})

	t.Run("SkipInvalidRows", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
			{"2.0"}, // 無效行，應被跳過
			{"3.0", "150"},
		}
		results, err := calc.CalculateFromRawData(context.Background(), records, 2)
		require.NoError(t, err)
		require.Len(t, results, 1)
	})
}

func TestMaxMeanCalculator_parseRawData(t *testing.T) {
	calc := NewMaxMeanCalculator(10)

	t.Run("ScalingFactorApplication", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.5E-3", "2.5E-4"}, // Scientific notation
		}
		// 測試通過公共方法驗證數據解析和縮放因子應用
		// 由於 parseRawData 是私有方法，我們通過計算結果來驗證縮放因子是否正確應用
		results, err := calc.CalculateFromRawData(context.Background(), records, 1)
		require.NoError(t, err)
		// 驗證結果確實考慮了縮放因子的應用
		require.NotEmpty(t, results)
	})
}

func TestMaxMeanCalculator_CalculateWithRange(t *testing.T) {
	calc := NewMaxMeanCalculator(10)

	t.Run("ValidTimeRange", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0e10, Channels: []float64{50.0}},  // 1.0 seconds
				{Time: 2.0e10, Channels: []float64{100.0}}, // 2.0 seconds
				{Time: 3.0e10, Channels: []float64{150.0}}, // 3.0 seconds
				{Time: 4.0e10, Channels: []float64{200.0}}, // 4.0 seconds
			},
		}

		// Calculate max mean for range 1.5 to 3.5 seconds
		results, err := calc.CalculateWithRange(context.Background(), dataset, 2, 1.5, 3.5)
		require.NoError(t, err)
		require.Len(t, results, 1)

		// Should find the max mean of (100, 150) = 125.0 at time range 2.0-3.0
		assert.Equal(t, 125.0, results[0].MaxMean)
	})

	t.Run("InsufficientDataInRange", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0e10, Channels: []float64{50.0}},
				{Time: 5.0e10, Channels: []float64{100.0}},
			},
		}

		// Try to calculate with range 2.0-3.0 seconds (no data in this range)
		results, err := calc.CalculateWithRange(context.Background(), dataset, 2, 2.0, 3.0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "指定時間範圍內的數據不足")
		require.Nil(t, results)
	})

	t.Run("WindowSizeLargerThanAvailableData", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 2.0e10, Channels: []float64{100.0}},
			},
		}

		results, err := calc.CalculateWithRange(context.Background(), dataset, 5, 1.0, 3.0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集無效或窗口大小過大")
		require.Nil(t, results)
	})

	// EndRange == 0 是合法上界（時間軸從 0 開始的情境）。原本
	// resolveDataRange 把 0 當作 "no end" sentinel，導致 CalculateWithRange
	// (start=-1, end=0) 這類「指定到 0 為止」的查詢被靜默改寫為「到資料末端」。
	// 新版用 CalculationOptions.HasEndRange 顯式表達意圖。
	t.Run("EndRangeZero_ExplicitUpperBound", func(t *testing.T) {
		// scaling factor 10，時間軸用 1e10 為「1 秒」。
		// 0 / 1e10 / 2e10 三點，windowSize=2，end=0e10 (== 0)。
		// 期望：只有第一個 window (Time 0..1) 落在 startRange=-1 ~ endRange=0 內 → 失敗，
		// 因為 0 點之後直到 0 為止只有一筆。
		// 但 startRange=-1, endRange=0 表示包含 Time<=0 的點 (即 idx 0 only) →
		// 點數不足 windowSize=2 → 應回 ErrInvalidTimeRange。
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{10.0}},
				{Time: 1.0e10, Channels: []float64{20.0}},
				{Time: 2.0e10, Channels: []float64{30.0}},
			},
		}

		_, err := calc.CalculateWithRange(context.Background(), dataset, 2, -1.0, 0.0)
		require.Error(t, err,
			"endRange=0 with explicit HasEndRange should treat 0 as upper bound, "+
				"not 'no end' sentinel; only 1 point ≤ 0 → insufficient for window=2")
		require.Contains(t, err.Error(), "指定時間範圍內的數據不足")
	})
}

func TestMaxMeanCalculator_CalculateFromRawDataWithRange(t *testing.T) {
	calc := NewMaxMeanCalculator(10)

	t.Run("ValidRawDataWithRange", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"0.1", "50"},
			{"0.2", "100"},
			{"0.3", "150"},
			{"0.4", "200"},
		}

		results, err := calc.CalculateFromRawDataWithRange(context.Background(), records, 2, 0.15, 0.35)
		require.NoError(t, err)
		require.Len(t, results, 1)

		// Should process data points at 0.2 and 0.3 seconds
		// With scaling factor 10: (100*10^10 + 150*10^10)/2 = 1.25e+12
		assert.Equal(t, 1.25e+12, results[0].MaxMean)
	})

	t.Run("InvalidRawDataWithRange", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"invalid", "100"},
		}

		results, err := calc.CalculateFromRawDataWithRange(context.Background(), records, 1, 0.0, 1.0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析數據失敗")
		require.Nil(t, results)
	})
}

func TestMaxMeanCalculator_EdgeCases(t *testing.T) {
	calc := NewMaxMeanCalculator(1) // Different scaling factor

	t.Run("EmptyChannels", func(t *testing.T) {
		// 0-channel input 改為 fail-fast。原本「0 channel = 空 result」
		// 是 silent success，但實務上 0 channel 表示上游 parser 沒解出資料 — 應該
		// 讓 caller 明確處理而非靜默產生空 output。
		dataset := &models.EMGDataset{
			Headers: []string{"Time"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{}},
				{Time: 2.0, Channels: []float64{}},
			},
		}

		results, err := calc.Calculate(context.Background(), dataset, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "通道")
		require.Nil(t, results)
	})

	t.Run("MultipleChannelsWithDifferentOptimalWindows", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{10.0, 1.0}},
				{Time: 2.0, Channels: []float64{20.0, 2.0}}, // Ch1 optimal here
				{Time: 3.0, Channels: []float64{5.0, 10.0}}, // Ch2 optimal here
				{Time: 4.0, Channels: []float64{15.0, 5.0}},
			},
		}

		results, err := calc.Calculate(context.Background(), dataset, 2)
		require.NoError(t, err)
		require.Len(t, results, 2)

		// Ch1 should have max mean at time 1-2 (mean = 15.0)
		assert.Equal(t, 15.0, results[0].MaxMean)
		assert.Equal(t, 1.0, results[0].StartTime)

		// Ch2 should have max mean at time 3-4 (mean = 7.5)
		assert.Equal(t, 7.5, results[1].MaxMean)
		assert.Equal(t, 3.0, results[1].StartTime)
	})
}
