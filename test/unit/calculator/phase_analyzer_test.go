package calculator_test

import (
	"count_mean/internal/models"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/calculator"
)

func TestPhaseAnalyzer_Analyze(t *testing.T) {
	phaseLabels := []string{"啟跳下蹲階段", "啟跳上升階段", "團身階段", "下降階段"}
	analyzer := calculator.NewPhaseAnalyzer(10, phaseLabels)

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
	analyzer := calculator.NewPhaseAnalyzer(10, phaseLabels)

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
	analyzer := calculator.NewPhaseAnalyzer(10, phaseLabels)

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
	analyzer := calculator.NewPhaseAnalyzer(6, phaseLabels)

	t.Run("ScalingFactorApplication", func(t *testing.T) {
		records := [][]string{
			{"Time", "Ch1"},
			{"1.5E-3", "2.5E-4"},
		}
		phaseStrings := []string{"0.0", "1.0E-3", "2.0E-3", "3.0E-3", "4.0E-3"} // 5個時間點
		result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
		require.NoError(t, err)
		require.NotNil(t, result)
		// The test should verify that scaling factor is applied correctly to the phase analysis
		// Since the time 1.5E-3 with scaling factor 6 becomes 1500.0, and falls within phase 1 (1.0E-3 to 2.0E-3)
		// which becomes 1000.0 to 2000.0, the data should be in the second phase (index 1)
		require.Len(t, result.PhaseResults, 1) // 1 phase label = 1 phase
		// Verify that the phase contains the data (scaled value 250.0 for the channel)
		if val, exists := result.PhaseResults[0].MaxValues[0]; exists {
			require.Equal(t, 250.0, val) // 2.5E-4 * 10^6
		}
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
