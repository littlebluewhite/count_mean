package calculator_test

import (
	"count_mean/internal/calculator"
	"count_mean/internal/models"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaxMeanCalculator_Calculate(t *testing.T) {
	calc := calculator.NewMaxMeanCalculator(10)

	t.Run("EmptyDataset", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    []models.EMGData{},
		}
		results, err := calc.Calculate(dataset, 5)
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
		results, err := calc.Calculate(dataset, 0)
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
		results, err := calc.Calculate(dataset, -1)
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
		results, err := calc.Calculate(dataset, 5)
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
		results, err := calc.Calculate(dataset, 2)
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
		results, err := calc.Calculate(dataset, 2)
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
		results, err := calc.Calculate(dataset, 1)
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
		results, err := calc.Calculate(dataset, 2)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, 100.0, results[0].MaxMean)
	})

	t.Run("NilDataset", func(t *testing.T) {
		results, err := calc.Calculate(nil, 2)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集無效或窗口大小過大")
		require.Nil(t, results)
	})
}

func TestMaxMeanCalculator_CalculateFromRawData(t *testing.T) {
	calc := calculator.NewMaxMeanCalculator(10)

	t.Run("ValidRawData", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1", "Ch2"},
			{"1.0", "100", "50"},
			{"2.0", "200", "100"},
			{"3.0", "150", "75"},
		}
		results, err := calc.CalculateFromRawData(records, 2)
		require.NoError(t, err)
		require.Len(t, results, 2)
	})

	t.Run("InvalidRawData_NoHeader", func(t *testing.T) {
		records := [][]string{
			{"1.0", "100"},
		}
		results, err := calc.CalculateFromRawData(records, 2)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據至少需要包含標題行和一行數據")
		require.Nil(t, results)
	})

	t.Run("InvalidRawData_BadNumber", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"invalid", "100"},
		}
		results, err := calc.CalculateFromRawData(records, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析時間值失敗")
		require.Nil(t, results)
	})

	t.Run("SkipInvalidRows", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
			{"2.0"}, // 無效行，應被跳過
			{"3.0", "150"},
		}
		results, err := calc.CalculateFromRawData(records, 2)
		require.NoError(t, err)
		require.Len(t, results, 1)
	})
}

func TestMaxMeanCalculator_parseRawData(t *testing.T) {
	calc := calculator.NewMaxMeanCalculator(10)

	t.Run("ScalingFactorApplication", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.5E-3", "2.5E-4"}, // Scientific notation
		}
		dataset, err := calc.parseRawData(records)
		require.NoError(t, err)
		require.Len(t, dataset.Data, 1)
		require.Equal(t, 1.5e+07, dataset.Data[0].Time)        // 1.5E-3 * 10^10
		require.Equal(t, 2.5e+06, dataset.Data[0].Channels[0]) // 2.5E-4 * 10^10
	})
}

func TestMaxMeanCalculator_CalculateWithRange(t *testing.T) {
	calc := calculator.NewMaxMeanCalculator(10)

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
		results, err := calc.CalculateWithRange(dataset, 2, 1.5, 3.5)
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
		results, err := calc.CalculateWithRange(dataset, 2, 2.0, 3.0)
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

		results, err := calc.CalculateWithRange(dataset, 5, 1.0, 3.0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "指定時間範圍內的數據不足")
		require.Nil(t, results)
	})
}

func TestMaxMeanCalculator_CalculateFromRawDataWithRange(t *testing.T) {
	calc := calculator.NewMaxMeanCalculator(10)

	t.Run("ValidRawDataWithRange", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"0.1", "50"},
			{"0.2", "100"},
			{"0.3", "150"},
			{"0.4", "200"},
		}

		results, err := calc.CalculateFromRawDataWithRange(records, 2, 0.15, 0.35)
		require.NoError(t, err)
		require.Len(t, results, 1)

		// Should process data points at 0.2 and 0.3 seconds
		assert.Equal(t, 125.0, results[0].MaxMean) // (100+150)/2
	})

	t.Run("InvalidRawDataWithRange", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"invalid", "100"},
		}

		results, err := calc.CalculateFromRawDataWithRange(records, 1, 0.0, 1.0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析數據失敗")
		require.Nil(t, results)
	})
}

func TestMaxMeanCalculator_EdgeCases(t *testing.T) {
	calc := calculator.NewMaxMeanCalculator(1) // Different scaling factor

	t.Run("EmptyChannels", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{}},
				{Time: 2.0, Channels: []float64{}},
			},
		}

		results, err := calc.Calculate(dataset, 1)
		require.NoError(t, err)
		require.Len(t, results, 0) // No channels to process
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

		results, err := calc.Calculate(dataset, 2)
		require.NoError(t, err)
		require.Len(t, results, 2)

		// Ch1 should have max mean at time 1-2 (mean = 15.0)
		assert.Equal(t, 15.0, results[0].MaxMean)
		assert.Equal(t, 1.0, results[0].StartTime)

		// Ch2 should have max mean at time 2-3 (mean = 6.0)
		assert.Equal(t, 6.0, results[1].MaxMean)
		assert.Equal(t, 2.0, results[1].StartTime)
	})
}
