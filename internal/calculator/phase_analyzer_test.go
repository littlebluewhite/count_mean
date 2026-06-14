package calculator

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/models"
)

func TestPhaseAnalyzer_Analyze(t *testing.T) {
	phaseLabels := []string{"啟跳下蹲階段", "啟跳上升階段", "團身階段", "下降階段"}
	analyzer := NewPhaseAnalyzer(10, phaseLabels)

	t.Run("NilDataset", func(t *testing.T) {
		phases := []models.TimeRange{
			{Start: 0.0, End: 1.0},
		}
		result, err := analyzer.Analyze(nil, phases)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集為空")
		require.Nil(t, result)
	})

	t.Run("EmptyDataset", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    []models.EMGData{},
		}
		phases := []models.TimeRange{
			{Start: 0.0, End: 1.0},
		}
		result, err := analyzer.Analyze(dataset, phases)
		require.Error(t, err)
		require.Contains(t, err.Error(), "數據集為空")
		require.Nil(t, result)
	})

	t.Run("PhaseLabelMismatch", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 0.5, Channels: []float64{100.0}},
			},
		}
		phases := []models.TimeRange{
			{Start: 0.0, End: 1.0},
			{Start: 1.0, End: 2.0},
		}
		result, err := analyzer.Analyze(dataset, phases)
		require.Error(t, err)
		require.Contains(t, err.Error(), "階段數量與標籤數量不匹配")
		require.Nil(t, result)
	})

	t.Run("ValidAnalysis_SinglePhase", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 0.5, Channels: []float64{100.0}},
				{Time: 0.7, Channels: []float64{200.0}},
				{Time: 0.9, Channels: []float64{150.0}},
			},
		}
		phases := []models.TimeRange{
			{Start: 0.0, End: 1.0},
			{Start: 1.0, End: 2.0},
			{Start: 2.0, End: 3.0},
			{Start: 3.0, End: 4.0},
		}
		result, err := analyzer.Analyze(dataset, phases)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.PhaseResults, 4)

		// 檢查第一個階段（有數據）
		phase1 := result.PhaseResults[0]
		require.Equal(t, "啟跳下蹲階段", phase1.PhaseName)
		require.Equal(t, 200.0, phase1.MaxValues[0])  // 最大值
		require.Equal(t, 150.0, phase1.MeanValues[0]) // 平均值 (100+200+150)/3

		// 檢查其他階段（無數據）
		for i := 1; i < 4; i++ {
			phase := result.PhaseResults[i]
			require.Len(t, phase.MaxValues, 0)
			require.Len(t, phase.MeanValues, 0)
		}
	})

	t.Run("ValidAnalysis_MultipleChannels", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 0.5, Channels: []float64{100.0, 50.0}},
				{Time: 0.7, Channels: []float64{200.0, 100.0}},
				{Time: 1.5, Channels: []float64{300.0, 150.0}},
				{Time: 1.7, Channels: []float64{250.0, 125.0}},
			},
		}
		phases := []models.TimeRange{
			{Start: 0.0, End: 1.0},
			{Start: 1.0, End: 2.0},
			{Start: 2.0, End: 3.0},
			{Start: 3.0, End: 4.0},
		}
		result, err := analyzer.Analyze(dataset, phases)
		require.NoError(t, err)
		require.NotNil(t, result)

		// 檢查第一個階段
		phase1 := result.PhaseResults[0]
		require.Equal(t, 200.0, phase1.MaxValues[0])  // Ch1 最大值
		require.Equal(t, 100.0, phase1.MaxValues[1])  // Ch2 最大值
		require.Equal(t, 150.0, phase1.MeanValues[0]) // Ch1 平均值
		require.Equal(t, 75.0, phase1.MeanValues[1])  // Ch2 平均值

		// 檢查第二個階段
		phase2 := result.PhaseResults[1]
		require.Equal(t, 300.0, phase2.MaxValues[0])  // Ch1 最大值
		require.Equal(t, 150.0, phase2.MaxValues[1])  // Ch2 最大值
		require.Equal(t, 275.0, phase2.MeanValues[0]) // Ch1 平均值
		require.Equal(t, 137.5, phase2.MeanValues[1]) // Ch2 平均值
	})

	t.Run("BoundaryConditions_ExactBoundary", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 0.0, Channels: []float64{100.0}}, // 邊界值，不應包含
				{Time: 1.0, Channels: []float64{200.0}}, // 邊界值，不應包含
				{Time: 0.5, Channels: []float64{150.0}}, // 應包含在第一階段
			},
		}
		phases := []models.TimeRange{
			{Start: 0.0, End: 1.0},
			{Start: 1.0, End: 2.0},
			{Start: 2.0, End: 3.0},
			{Start: 3.0, End: 4.0},
		}
		result, err := analyzer.Analyze(dataset, phases)
		require.NoError(t, err)
		require.NotNil(t, result)

		// 第一階段只應包含 0.5 時間點的數據
		phase1 := result.PhaseResults[0]
		require.Equal(t, 150.0, phase1.MaxValues[0])
		require.Equal(t, 150.0, phase1.MeanValues[0])
	})

	t.Run("MaxTimeIndex_Calculation", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{100.0, 50.0}},
				{Time: 2.0, Channels: []float64{300.0, 200.0}}, // Ch1 最大值在此
				{Time: 3.0, Channels: []float64{150.0, 250.0}}, // Ch2 最大值在此
			},
		}
		phases := []models.TimeRange{
			{Start: 0.0, End: 4.0},
			{Start: 4.0, End: 5.0},
			{Start: 5.0, End: 6.0},
			{Start: 6.0, End: 7.0},
		}
		result, err := analyzer.Analyze(dataset, phases)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.MaxTimeIndex, 2)
		require.Equal(t, 2.0, result.MaxTimeIndex[0]) // Ch1 最大值在時間 2.0
		require.Equal(t, 3.0, result.MaxTimeIndex[1]) // Ch2 最大值在時間 3.0
	})

	t.Run("NoDataInAnyPhase", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data: []models.EMGData{
				{Time: 5.0, Channels: []float64{100.0}}, // 超出所有階段範圍
			},
		}
		phases := []models.TimeRange{
			{Start: 0.0, End: 1.0},
			{Start: 1.0, End: 2.0},
			{Start: 2.0, End: 3.0},
			{Start: 3.0, End: 4.0},
		}
		result, err := analyzer.Analyze(dataset, phases)
		require.NoError(t, err)
		require.NotNil(t, result)

		// 所有階段都應該沒有數據
		for _, phase := range result.PhaseResults {
			require.Len(t, phase.MaxValues, 0)
			require.Len(t, phase.MeanValues, 0)
		}
	})
}

func TestPhaseAnalyzer_AnalyzeFromRawData(t *testing.T) {
	phaseLabels := []string{"啟跳下蹲階段", "啟跳上升階段", "團身階段", "下降階段"}
	analyzer := NewPhaseAnalyzer(10, phaseLabels)

	t.Run("ValidRawData", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"0.5", "100"},
			{"1.5", "200"},
		}
		phaseStrings := []string{"0.0", "1.0", "2.0", "3.0", "4.0"}
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.PhaseResults, 4)
	})

	t.Run("InvalidRawData", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.0", "invalid"},
		}
		phaseStrings := []string{"0.0", "1.0", "2.0", "3.0", "4.0"}
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析數據失敗")
		require.Nil(t, result)
	})

	t.Run("InvalidPhaseStrings", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"0.5", "100"},
		}
		phaseStrings := []string{"0.0", "invalid", "2.0"}
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析階段失敗")
		require.Nil(t, result)
	})

	t.Run("InsufficientPhaseStrings", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"0.5", "100"},
		}
		phaseStrings := []string{"0.0", "1.0"} // 少於5個時間點
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.Error(t, err)
		require.Contains(t, err.Error(), "需要至少 5 個時間點來定義 4 個階段")
		require.Nil(t, result)
	})
}

func TestPhaseAnalyzer_parsePhases(t *testing.T) {
	phaseLabels := []string{"階段1", "階段2", "階段3"}
	analyzer := NewPhaseAnalyzer(10, phaseLabels)

	t.Run("ValidPhases", func(t *testing.T) {
		// 由於 parsePhases 是私有方法，我們通過 AnalyzeFromRawData 來驗證階段解析
		records := [][]string{
			{"Time", "Ch1"},
			{"0.5", "100"},
			{"1.5", "200"},
			{"2.5", "150"},
		}
		phaseStrings := []string{"0.0", "1.0", "2.0", "3.0", "4.0"} // 需要5個時間點來定義4個階段
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.NoError(t, err)
		require.NotNil(t, result)
		// 驗證階段分析結果包含預期的階段數量
		require.Len(t, result.PhaseResults, 3)
	})

	t.Run("ScientificNotation", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.5E-3", "100"},
			{"2.5E-3", "200"},
			{"3.5E-3", "150"},
		}
		phaseStrings := []string{"1.0E-3", "2.0E-3", "3.0E-3", "4.0E-3", "5.0E-3"} // 需要5個時間點
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.NoError(t, err)
		require.NotNil(t, result)
		// 驗證科學記法正確處理
		require.Len(t, result.PhaseResults, 3)
	})

	t.Run("InsufficientTimePoints", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"0.5", "100"},
		}
		phaseStrings := []string{"0.0", "1.0"} // 只有2個時間點
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.Error(t, err)
		require.Contains(t, err.Error(), "階段")
		require.Nil(t, result)
	})

	t.Run("InvalidTimePoint", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"0.5", "100"},
		}
		phaseStrings := []string{"0.0", "invalid", "2.0", "3.0", "4.0"} // 5個時間點，但有無效值
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析")
		require.Nil(t, result)
	})
}

func TestPhaseAnalyzer_parseRawData(t *testing.T) {
	phaseLabels := []string{"階段1"}
	analyzer := NewPhaseAnalyzer(6, phaseLabels)

	t.Run("ScalingFactorApplication", func(t *testing.T) {
		// Time=1.5E-3,Ch1=2.5E-4
		// sf=6:Time 縮放後 = 1500.0,Ch1 縮放後 = 250.0
		// phaseStrings 同樣經 sf 縮放:
		//   1.0E-3 → 1000.0、2.0E-3 → 2000.0(其餘 3 點供 MinTimePointsForPhases=5 用)
		// 1 個 phase label → phases[0] = {Start:1000.0, End:2000.0}
		// 1500.0 在 (1000.0, 2000.0) 內 → MaxValues[0] 必須存在且為 250.0
		records := [][]string{
			{"Time", "Ch1"},
			{"1.5E-3", "2.5E-4"},
		}
		// MinTimePointsForPhases = 5;1 個 phase label 只用前 2 點定義 phases[0]
		phaseStrings := []string{"1.0E-3", "2.0E-3", "3.0E-3", "4.0E-3", "5.0E-3"}
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Len(t, result.PhaseResults, 1) // 1 個 phase label = 1 個 phase

		// [8] 去守衛:強制斷言 MaxValues[0] 存在且值正確,去掉舊的 if-exists 守衛。
		// 若 guard 仍在、data 沒落入 phase,斷言會被跳過 → 假綠。
		val, exists := result.PhaseResults[0].MaxValues[0]
		require.True(t, exists, "scaled data 應落入 phase,MaxValues[0] 必須存在")
		require.Equal(t, 250.0, val) // 2.5E-4 × 10^6 (scalingFactor=6)

		// 負對照:sf=3 對照組驗證縮放因子確實生效(不同 sf → 不同通道值)。
		// sf=3:2.5E-4 × 10^3 = 0.25,與 sf=6 的 250.0 不相等。
		// phaseStrings 縮放後 phase 範圍 = (1.0, 2.0);
		// data.Time = 1.5E-3 × 10^3 = 1.5 落在 (1.0, 2.0) 內 → exists3 = true。
		require.NotEqual(t, 2.5e-4, val, "原始未縮放值不應與縮放後相等")
		analyzer3 := NewPhaseAnalyzer(3, phaseLabels)
		result3, err3 := analyzer3.AnalyzeFromRawData(records, phaseStrings)
		require.NoError(t, err3)
		require.NotNil(t, result3)
		val3, exists3 := result3.PhaseResults[0].MaxValues[0]
		require.True(t, exists3, "sf=3 的 scaled data 同樣應落入 phase")
		require.InDelta(t, 0.25, val3, 1e-9, "sf=3:2.5E-4 × 10^3 = 0.25")
		require.NotEqual(t, val, val3, "sf 不同 → 通道值不同,縮放確實生效")
	})

	t.Run("SkipInvalidRows", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
			{"2.0"}, // 無效行，應被跳過
			{"3.0", "200"},
		}
		phaseStrings := []string{"0.5", "1.5", "2.5", "3.5", "4.5"} // 5個時間點
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.NoError(t, err)
		require.Len(t, result.PhaseResults, 1) // 1 phase label = 1 phase
	})

	t.Run("ErrorInDataParsing", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
		}
		phaseStrings := []string{"0.5", "1.5", "2.5", "3.5"} // 仍然只有4個時間點
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.Error(t, err)
		require.Contains(t, err.Error(), "需要至少 5 個時間點")
		require.Nil(t, result)
	})

	t.Run("ErrorInChannelParsing", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.0", "invalid"},
		}
		phaseStrings := []string{"0.5", "1.5", "2.5", "3.5", "4.5"} // 5個時間點
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析數據失敗在第 2 行第 2 列")
		require.Nil(t, result)
	})
}

// TestPhaseAnalyzer_Analyze_RaggedDataset 釘住 Finding [2]:
// validateAnalyzeInput 對 ragged dataset(各列通道數不一致)必須 fail-fast 回
// ErrChannelMismatch。舊行為:collectPhaseData 按位置讀 Channels,中段列通道數少
// 時補 phantom 0,通道數多時靜默截斷 → silent 資料錯位/污染。
func TestPhaseAnalyzer_Analyze_RaggedDataset(t *testing.T) {
	phaseLabels := []string{"階段一"}
	analyzer := NewPhaseAnalyzer(10, phaseLabels)

	cases := []struct {
		name             string
		data             []models.EMGData
		wantRowIndex     int
		wantRowChannels  int
		wantExpectedChan int
	}{
		{
			name: "row1 通道數少於 row0",
			data: []models.EMGData{
				{Time: 0.5, Channels: []float64{1.0, 2.0}}, // row0: 2 通道
				{Time: 1.5, Channels: []float64{3.0}},      // row1: 1 通道 → 不一致
			},
			wantRowIndex:     2,
			wantRowChannels:  1,
			wantExpectedChan: 2,
		},
		{
			name: "row1 通道數多於 row0",
			data: []models.EMGData{
				{Time: 0.5, Channels: []float64{1.0}},           // row0: 1 通道
				{Time: 1.5, Channels: []float64{2.0, 3.0, 4.0}}, // row1: 3 通道 → 不一致
			},
			wantRowIndex:     2,
			wantRowChannels:  3,
			wantExpectedChan: 1,
		},
		{
			name: "row0 有 1 通道、row1 有 0 通道",
			data: []models.EMGData{
				{Time: 0.5, Channels: []float64{1.0}}, // row0: 1 通道
				{Time: 1.5, Channels: []float64{}},    // row1: 0 通道
			},
			wantRowIndex:     2,
			wantRowChannels:  0,
			wantExpectedChan: 1,
		},
	}

	phases := []models.TimeRange{
		{Start: 0.0, End: 2.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataset := &models.EMGDataset{
				Headers: []string{"Time", "Ch1"},
				Data:    tc.data,
			}

			result, err := analyzer.Analyze(dataset, phases)
			require.Error(t, err, "ragged dataset 必須 fail-fast")
			require.Nil(t, result, "ragged dataset 不得回傳 result")
			require.True(t, errors.Is(err, calcerrors.ErrChannelMismatch),
				"error 必須包裝 ErrChannelMismatch;實際 err=%v", err)

			// 驗證 context 欄位(取出 *CalculatorError 後查 Context map)。
			var calcErr *calcerrors.CalculatorError
			require.True(t, errors.As(err, &calcErr),
				"error 必須是 *CalculatorError;實際 err=%v", err)
			ctx := calcErr.Context
			require.Equal(t, tc.wantRowIndex, ctx["row_index"],
				"row_index 欄位不符;ctx=%v", ctx)
			require.Equal(t, tc.wantRowChannels, ctx["row_channels"],
				"row_channels 欄位不符;ctx=%v", ctx)
			require.Equal(t, tc.wantExpectedChan, ctx["expected_channels"],
				"expected_channels 欄位不符;ctx=%v", ctx)
		})
	}
}
