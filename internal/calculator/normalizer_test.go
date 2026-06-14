package calculator

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/models"
)

func TestNormalizer_Normalize(t *testing.T) {
	normalizer := NewNormalizer(10)

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
	normalizer := NewNormalizer(10)

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
			{"Label", "invalid_value"}, // Invalid channel value (not a number)
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
	normalizer := NewNormalizer(6)

	t.Run("ScientificNotation", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.5E-3", "2.5E-4"},
		}
		// 參考行:Time=1.0E-3(縮放後不參與 ratio)、Ch1=1.0E-4(作分母)
		reference := [][]string{
			{"Time", "Ch1"},
			{"1.0E-3", "1.0E-4"},
		}
		dataset, err := normalizer.NormalizeFromRawData(records, reference)
		require.NoError(t, err)
		require.NotNil(t, dataset)

		// [17] 去假綠:原本只 require.NotNil,等於沒驗縮放。
		// 縮放在 Time 可見(直接複製 parsed EMG data.Time):
		//   sf=6 → 1.5E-3 × 10^6 = 1500.0
		// Normalized channel ratio 與 sf 無關(分子分母都 ×10^sf 抵消):
		//   Ch1 = 2.5E-4 / 1.0E-4 = 2.5(無論 sf 為何)
		require.Len(t, dataset.Data, 1)
		require.InDelta(t, 1500.0, dataset.Data[0].Time, 1e-6,
			"sf=6:Time=1.5E-3×10^6=1500.0;若縮放未生效此斷言會紅")
		require.InDelta(t, 2.5, dataset.Data[0].Channels[0], 1e-9,
			"normalized channel=2.5E-4/1.0E-4=2.5;ratio 與 sf 無關")

		// sf=0 對照:Time=原始未縮放值、Channel ratio 仍相同,
		// 確認縮放只在 Time 體現而非 ratio。
		normalizer0 := NewNormalizer(0)
		dataset0, err0 := normalizer0.NormalizeFromRawData(records, reference)
		require.NoError(t, err0)
		require.NotNil(t, dataset0)
		require.Len(t, dataset0.Data, 1)
		require.InDelta(t, 0.0015, dataset0.Data[0].Time, 1e-9,
			"sf=0:Time=1.5E-3 未縮放;與 sf=6 的 1500.0 不同")
		require.InDelta(t, 2.5, dataset0.Data[0].Channels[0], 1e-9,
			"normalized channel ratio 與 sf 無關,仍為 2.5")
		require.NotEqual(t, dataset0.Data[0].Time, dataset.Data[0].Time,
			"不同 sf → Time 不同,縮放確實生效")
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

// TestNormalizer_Normalize_NaNInfReference 驗證
// 參考值含 NaN/Inf 必須 fail-fast，不能進入後續 dataset / maxVal 運算，
// 否則整批 normalized data 都會被污染為 NaN/Inf。
func TestNormalizer_Normalize_NaNInfReference(t *testing.T) {
	normalizer := NewNormalizer(10)
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1", "Ch2"},
		Data: []models.EMGData{
			{Time: 1.0, Channels: []float64{100.0, 200.0}},
		},
	}

	t.Run("NaNReference", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{math.NaN(), 200.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "通道 1")
		require.Contains(t, err.Error(), "NaN")
		require.Nil(t, result)
	})

	t.Run("PositiveInfReference", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0, math.Inf(1)}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "通道 2")
		require.Contains(t, err.Error(), "Inf")
		require.Nil(t, result)
	})

	t.Run("NegativeInfReference", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0, math.Inf(-1)}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.Contains(t, err.Error(), "通道 2")
		require.Contains(t, err.Error(), "Inf")
		require.Nil(t, result)
	})
}

// TestNormalizer_Normalize_MultiRowReferenceRejected 驗證
// reference EMGDataset 只允許含一行 MVC/MAX 參考值（domain semantics —
// 一條肌肉一個 peak）。多行 reference 過去會被 Normalize 取 Data[0] 後
// 靜默忽略其餘行，導致使用者誤以為「reference 全行有效」但結果只用第一行。
// 修法：Normalize 進入時對 len(reference.Data) != 1 fail-fast。
func TestNormalizer_Normalize_MultiRowReferenceRejected(t *testing.T) {
	normalizer := NewNormalizer(10)
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data: []models.EMGData{
			{Time: 1.0, Channels: []float64{100.0}},
		},
	}

	t.Run("TwoRowReference", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0}},
				{Time: 0.1, Channels: []float64{200.0}}, // 過去被靜默忽略
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrReferenceMultipleRows)
		require.Nil(t, result)
	})

	t.Run("ManyRowReference", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0}},
				{Time: 0.1, Channels: []float64{150.0}},
				{Time: 0.2, Channels: []float64{200.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrReferenceMultipleRows)
		require.Nil(t, result)
	})
}

// TestNormalizer_NormalizeFromRawData_MultiRowReferenceRejected 驗證：
// 多行 reference 透過 NormalizeFromRawData → parseReferenceData → Normalize
// 仍會 fail-fast 對齊 ErrReferenceMultipleRows。
func TestNormalizer_NormalizeFromRawData_MultiRowReferenceRejected(t *testing.T) {
	normalizer := NewNormalizer(10)
	records := [][]string{
		{"Time", "Ch1"},
		{"1.0", "200"},
	}

	t.Run("TwoRowReference", func(t *testing.T) {
		reference := [][]string{
			{"Time", "Ch1"},
			{"Label1", "100"},
			{"Label2", "200"}, // 過去被靜默忽略
		}
		result, err := normalizer.NormalizeFromRawData(records, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrReferenceMultipleRows)
		require.Nil(t, result)
	})
}

// TestNormalize_RaggedRow_Rejected 驗證
// dataset.ChannelCount() 只看 Data[0]，row 0 之外的 ragged row（更寬會 panic、
// 更窄會 silent 錯誤輸出）必須在進 hot loop 前 fail-fast。
//
// V4 PoC 重現：row 0 [1,2,3] / row 1 [10,20,30,40,50] → `refValues[j]`
// 在 j=3 時 index-out-of-range panic；narrower row [4,5] 則 silent emit
// 一個 width=2 的 row，下游 CSV/chart 取到 ragged dataset 才會出狀況、
// 離 root cause 已經很遠。
func TestNormalize_RaggedRow_Rejected(t *testing.T) {
	normalizer := NewNormalizer(10)
	reference := &models.EMGDataset{
		Headers: []string{"Time", "Ch1", "Ch2", "Ch3"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{1.0, 2.0, 3.0}},
		},
	}

	t.Run("WiderRowPanicsBecomesError", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2", "Ch3"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{1.0, 2.0, 3.0}},      // 與 reference 同寬，gate 放行
				{Time: 2.0, Channels: []float64{10, 20, 30, 40, 50}}, // 寬度 5，refValues[3] panic
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrChannelMismatch)
		require.Nil(t, result)
	})

	t.Run("NarrowerRowFailsFast", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2", "Ch3"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{1.0, 2.0, 3.0}},
				{Time: 2.0, Channels: []float64{4.0, 5.0}}, // 寬度 2，過去被 silent 接受
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrChannelMismatch)
		require.Nil(t, result)
	})

	t.Run("UniformRowsStillWork", func(t *testing.T) {
		// 不要把 happy path 也廢掉
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2", "Ch3"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{2.0, 4.0, 6.0}},
				{Time: 2.0, Channels: []float64{4.0, 8.0, 12.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Data, 2)
		require.Equal(t, []float64{2.0, 2.0, 2.0}, result.Data[0].Channels)
		require.Equal(t, []float64{4.0, 4.0, 4.0}, result.Data[1].Channels)
	})
}

// TestValidateReference_Sentinels 驗證
// validateReferenceValues 必須三路 dispatch 不同 sentinel —
// NaN / Inf / Zero 各自走 ErrNaNReference / ErrInfReference / ErrZeroReference。
// 對齊 range_normalizer 的 ErrChannelMaxNaN / ErrChannelMaxInf 拆分模式，
// callers 才能 programmatically 分辨「資料品質問題 retry」vs「真實 0 拒絕執行」。
func TestValidateReference_Sentinels(t *testing.T) {
	normalizer := NewNormalizer(10)
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1", "Ch2"},
		Data: []models.EMGData{
			{Time: 1.0, Channels: []float64{100.0, 200.0}},
		},
	}

	t.Run("NaNReferenceErrorIsNaNSentinel", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{math.NaN(), 200.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrNaNReference)
		require.NotErrorIs(t, err, calcerrors.ErrZeroReference)
		require.NotErrorIs(t, err, calcerrors.ErrInfReference)
		require.Nil(t, result)
	})

	t.Run("PositiveInfReferenceErrorIsInfSentinel", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0, math.Inf(1)}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrInfReference)
		require.NotErrorIs(t, err, calcerrors.ErrZeroReference)
		require.NotErrorIs(t, err, calcerrors.ErrNaNReference)
		require.Nil(t, result)
	})

	t.Run("NegativeInfReferenceErrorIsInfSentinel", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0, math.Inf(-1)}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrInfReference)
		require.NotErrorIs(t, err, calcerrors.ErrZeroReference)
		require.NotErrorIs(t, err, calcerrors.ErrNaNReference)
		require.Nil(t, result)
	})

	t.Run("ZeroReferenceErrorIsZeroSentinel", func(t *testing.T) {
		reference := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0, 0.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrZeroReference)
		require.NotErrorIs(t, err, calcerrors.ErrNaNReference)
		require.NotErrorIs(t, err, calcerrors.ErrInfReference)
		require.Nil(t, result)
	})
}

// TestNormalize_AssertsHeaderChannelMismatch 釘住 (b):
// EMGDataset 規格契約 — Headers[0] 為時間欄、Headers[1..] 為各通道名,所以
// len(Headers) 必須恰好 == ChannelCount() + 1。過去若 caller 構造的 dataset
// Headers 與 Data[0].Channels 寬度不一致 (parser bug / 測試 fixture 失誤),
// Normalize 不會擋,直接把錯誤 Headers 複製到輸出,下游 CSV writer 寫出
// 「欄位數與資料行不一致」的損壞 CSV。
//
// 此 test 守住 boundary fail-fast — Headers 過寬 / 過窄都應觸發
// ErrChannelMismatch 並回 nil result。
func TestNormalize_AssertsHeaderChannelMismatch(t *testing.T) {
	normalizer := NewNormalizer(10)
	reference := &models.EMGDataset{
		Headers: []string{"Time", "Ch1", "Ch2"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{1.0, 2.0}},
		},
	}

	t.Run("TooManyHeaders", func(t *testing.T) {
		// dataset.ChannelCount() = 2 (from Data[0]),但 Headers 寬度為 4 (期望 3)
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2", "Ch3_phantom"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{1.0, 2.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrChannelMismatch)
		require.Nil(t, result)
	})

	t.Run("TooFewHeaders", func(t *testing.T) {
		// dataset.ChannelCount() = 2,但 Headers 只有 2 個 (期望 3:Time + Ch1 + Ch2)
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{1.0, 2.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.Error(t, err)
		require.ErrorIs(t, err, calcerrors.ErrChannelMismatch)
		require.Nil(t, result)
	})

	t.Run("HeadersMatchChannelCountPasses", func(t *testing.T) {
		// happy path:Headers = ["Time", "Ch1", "Ch2"] (寬度 3),ChannelCount = 2,3 == 2+1 通過
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{2.0, 4.0}},
			},
		}
		result, err := normalizer.Normalize(dataset, reference)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.Headers, 3)
	})
}
