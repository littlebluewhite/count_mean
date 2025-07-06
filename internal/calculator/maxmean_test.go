package calculator

import (
	"count_mean/internal/models"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaxMeanCalculator_Calculate(t *testing.T) {
	calculator := NewMaxMeanCalculator(10)

	t.Run("EmptyDataset", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    []models.EMGData{},
		}
		results, err := calculator.Calculate(dataset, 5)
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
		results, err := calculator.Calculate(dataset, 0)
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
		results, err := calculator.Calculate(dataset, -1)
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
		results, err := calculator.Calculate(dataset, 5)
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
		results, err := calculator.Calculate(dataset, 2)
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
		results, err := calculator.Calculate(dataset, 2)
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
		results, err := calculator.Calculate(dataset, 1)
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
		results, err := calculator.Calculate(dataset, 2)
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, 100.0, results[0].MaxMean)
	})

	t.Run("NilDataset", func(t *testing.T) {
		results, err := calculator.Calculate(nil, 2)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集無效或窗口大小過大")
		require.Nil(t, results)
	})
}

func TestMaxMeanCalculator_CalculateFromRawData(t *testing.T) {
	calculator := NewMaxMeanCalculator(10)

	t.Run("ValidRawData", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1", "Ch2"},
			{"1.0", "100", "50"},
			{"2.0", "200", "100"},
			{"3.0", "150", "75"},
		}
		results, err := calculator.CalculateFromRawData(records, 2)
		require.NoError(t, err)
		require.Len(t, results, 2)
	})

	t.Run("InvalidRawData_NoHeader", func(t *testing.T) {
		records := [][]string{
			{"1.0", "100"},
		}
		results, err := calculator.CalculateFromRawData(records, 2)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據至少需要包含標題行和一行數據")
		require.Nil(t, results)
	})

	t.Run("InvalidRawData_BadNumber", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"invalid", "100"},
		}
		results, err := calculator.CalculateFromRawData(records, 1)
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
		results, err := calculator.CalculateFromRawData(records, 2)
		require.NoError(t, err)
		require.Len(t, results, 1)
	})
}

func TestMaxMeanCalculator_parseRawData(t *testing.T) {
	calculator := NewMaxMeanCalculator(10)

	t.Run("ScalingFactorApplication", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.5E-3", "2.5E-4"}, // Scientific notation
		}
		dataset, err := calculator.parseRawData(records)
		require.NoError(t, err)
		require.Len(t, dataset.Data, 1)
		require.Equal(t, 1.5e+07, dataset.Data[0].Time)        // 1.5E-3 * 10^10
		require.Equal(t, 2.5e+06, dataset.Data[0].Channels[0]) // 2.5E-4 * 10^10
	})
}
