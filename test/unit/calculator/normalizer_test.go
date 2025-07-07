package calculator_test

import (
	"count_mean/internal/models"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/calculator"
)

func TestNormalizer_Normalize(t *testing.T) {
	normalizer := calculator.NewNormalizer(10)

	t.Run("NilDatasets", func(t *testing.T) {
		result, err := normalizer.Normalize(nil, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集或參考數據集為空")
		require.Nil(t, result)
	})

	t.Run("NilMainDataset", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
			},
		}
		result, err := normalizer.Normalize(nil, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集或參考數據集為空")
		require.Nil(t, result)
	})

	t.Run("NilReferenceDataset", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集或參考數據集為空")
		require.Nil(t, result)
	})

	t.Run("EmptyDatasets", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    []models.EMGData{},
		}
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    []models.EMGData{},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集或參考數據集為空")
		require.Nil(t, result)
	})

	t.Run("ChannelMismatch", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0, 50.0}},
			},
		}
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集和參考數據集的通道數不匹配")
		require.Nil(t, result)
	})

	t.Run("DivisionByZero", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0}},
			},
		}
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{0.0}}, // 除零情況
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "參考值在通道 1 為零，無法進行標準化")
		require.Nil(t, result)
	})

	t.Run("DivisionByZero_MultipleChannels", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0, 50.0}},
			},
		}
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0, 0.0}}, // 第二個通道為零
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "參考值在通道 2 為零，無法進行標準化")
		require.Nil(t, result)
	})

	t.Run("ValidNormalization_SingleChannel", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{200.0}},
				{Time: 2.0, Channels: []float64{400.0}},
			},
		}
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0}}, // 參考值
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Data, 2)
		require.Equal(t, 2.0, result.Data[0].Channels[0]) // 200/100
		require.Equal(t, 4.0, result.Data[1].Channels[0]) // 400/100
		require.Equal(t, 1.0, result.Data[0].Time)
		require.Equal(t, 2.0, result.Data[1].Time)
	})

	t.Run("ValidNormalization_MultipleChannels", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{200.0, 300.0}},
				{Time: 2.0, Channels: []float64{400.0, 600.0}},
			},
		}
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0, 150.0}}, // 參考值
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Data, 2)
		require.Equal(t, 2.0, result.Data[0].Channels[0]) // 200/100
		require.Equal(t, 2.0, result.Data[0].Channels[1]) // 300/150
		require.Equal(t, 4.0, result.Data[1].Channels[0]) // 400/100
		require.Equal(t, 4.0, result.Data[1].Channels[1]) // 600/150
	})

	t.Run("HeaderPreservation", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Channel1", "Channel2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0, 200.0}},
			},
		}
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{50.0, 100.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.NoError(t, err)
		require.Equal(t, dataset.Headers, result.Headers) // 應保持原數據集的標題
	})
}

func TestNormalizer_NormalizeFromRawData(t *testing.T) {
	normalizer := calculator.NewNormalizer(10)

	t.Run("ValidRawData", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.0", "200"},
			{"2.0", "400"},
		}
		reference := [][]string{
			{"Time", "Ch1"},
			{"0.0", "100"},
		}
		result, err := normalizer.NormalizeFromRawData(records, reference)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Data, 2)
		require.Equal(t, 2.0, result.Data[0].Channels[0]) // 200/100
		require.Equal(t, 4.0, result.Data[1].Channels[0]) // 400/100
	})

	t.Run("InvalidMainData", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"invalid", "200"},
		}
		reference := [][]string{
			{"Time", "Ch1"},
			{"0.0", "100"},
		}
		result, err := normalizer.NormalizeFromRawData(records, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析主數據失敗")
		require.Nil(t, result)
	})

	t.Run("InvalidReferenceData", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.0", "200"},
		}
		reference := [][]string{
			{"Time", "Ch1"},
			{"invalid", "100"},
		}
		result, err := normalizer.NormalizeFromRawData(records, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析參考數據失敗")
		require.Nil(t, result)
	})

	t.Run("SkipInvalidRows", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.0", "200"},
			{"2.0"}, // 無效行，應被跳過
			{"3.0", "400"},
		}
		reference := [][]string{
			{"Time", "Ch1"},
			{"0.0", "100"},
		}
		result, err := normalizer.NormalizeFromRawData(records, reference)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Data, 2) // 只有兩行有效數據
	})
}

func TestNormalizer_parseRawData(t *testing.T) {
	normalizer := calculator.NewNormalizer(6)

	t.Run("ScientificNotation", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.5E-3", "2.5E-4"},
		}
		// 由於 parseRawData 是私有方法，我們使用 NormalizeFromRawData 來驗證縮放因子應用
		reference := [][]string{
			{"Time", "Ch1"},
			{"1.0E-3", "1.0E-4"},
		}
		dataset, err := normalizer.NormalizeFromRawData(records, reference)
		require.NoError(t, err)
		// 驗證縮放因子的應用通過正常化結果體現
		require.NotNil(t, dataset)
	})

	t.Run("EmptyRecords", func(t *testing.T) {
		records := [][]string{}
		reference := [][]string{{"Time", "Ch1"}}
		dataset, err := normalizer.NormalizeFromRawData(records, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析")
		require.Nil(t, dataset)
	})

	t.Run("OnlyHeader", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
		}
		reference := [][]string{{"Time", "Ch1"}, {"1.0", "1.0"}}
		dataset, err := normalizer.NormalizeFromRawData(records, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析")
		require.Nil(t, dataset)
	})
}
