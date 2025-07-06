package main

import (
	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestFullWorkflow_MaxMeanCalculation 測試完整的最大平均值計算流程
func TestFullWorkflow_MaxMeanCalculation(t *testing.T) {
	// 準備測試配置
	cfg := &config.AppConfig{
		ScalingFactor: 10,
		Precision:     2,
		BOMEnabled:    false,
		OutputFormat:  "csv",
		PhaseLabels:   []string{"Phase1", "Phase2"},
	}

	// 創建模組實例
	csvHandler := io.NewCSVHandler(cfg)
	maxMeanCalc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)

	// 準備測試數據
	tempDir := t.TempDir()
	inputFile := filepath.Join(tempDir, "test_input.csv")

	// 寫入測試CSV文件
	testData := "Time,Ch1,Ch2\n0.1,100,50\n0.2,200,100\n0.3,150,75\n0.4,300,150\n"
	err := os.WriteFile(inputFile, []byte(testData), 0644)
	require.NoError(t, err)

	// 讀取CSV數據
	records, err := csvHandler.ReadCSV(inputFile)
	require.NoError(t, err)
	require.Len(t, records, 5) // 標題 + 4行數據

	// 執行最大平均值計算
	results, err := maxMeanCalc.CalculateFromRawData(records, 2)
	require.NoError(t, err)
	require.Len(t, results, 2) // 兩個通道

	// 轉換結果為CSV格式
	outputData := csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, 0.1, 0.4)
	require.Len(t, outputData, 6) // 標題 + 5行結果

	// 寫入輸出文件
	outputFile := filepath.Join(tempDir, "maxmean_result.csv")
	err = csvHandler.WriteCSV(outputFile, outputData)
	require.NoError(t, err)

	// 驗證輸出文件內容
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	require.Len(t, lines, 6)
	require.Equal(t, "Time,Ch1,Ch2", lines[0])
	require.Contains(t, lines[1], "開始範圍秒數")
	require.Contains(t, lines[2], "結束範圍秒數")
	require.Contains(t, lines[3], "開始計算秒數")
	require.Contains(t, lines[4], "結束計算秒數")
	require.Contains(t, lines[5], "最大平均值")
}

// TestFullWorkflow_DataNormalization 測試完整的數據標準化流程
func TestFullWorkflow_DataNormalization(t *testing.T) {
	// 準備測試配置
	cfg := &config.AppConfig{
		ScalingFactor: 10,
		Precision:     3,
		BOMEnabled:    false,
		OutputFormat:  "csv",
		PhaseLabels:   []string{"Phase1"},
	}

	// 創建模組實例
	csvHandler := io.NewCSVHandler(cfg)
	normalizer := calculator.NewNormalizer(cfg.ScalingFactor)

	// 準備測試數據
	tempDir := t.TempDir()

	// 主數據文件
	mainFile := filepath.Join(tempDir, "main_data.csv")
	mainData := "Time,Ch1,Ch2\n0.1,200,100\n0.2,400,200\n"
	err := os.WriteFile(mainFile, []byte(mainData), 0644)
	require.NoError(t, err)

	// 參考數據文件
	refFile := filepath.Join(tempDir, "ref_data.csv")
	refData := "Time,Ch1,Ch2\n0.1,100,50\n0.2,100,50\n"
	err = os.WriteFile(refFile, []byte(refData), 0644)
	require.NoError(t, err)

	// 讀取數據
	mainRecords, err := csvHandler.ReadCSV(mainFile)
	require.NoError(t, err)

	refRecords, err := csvHandler.ReadCSV(refFile)
	require.NoError(t, err)

	// 執行標準化
	result, err := normalizer.NormalizeFromRawData(mainRecords, refRecords)
	require.NoError(t, err)
	require.Len(t, result.Data, 2)

	// 驗證標準化結果
	require.Equal(t, 2.0, result.Data[0].Channels[0]) // 200/100 = 2.0
	require.Equal(t, 2.0, result.Data[0].Channels[1]) // 100/50 = 2.0
	require.Equal(t, 4.0, result.Data[1].Channels[0]) // 400/100 = 4.0
	require.Equal(t, 4.0, result.Data[1].Channels[1]) // 200/50 = 4.0

	// 轉換為CSV並輸出
	outputData := csvHandler.ConvertNormalizedDataToCSV(result)
	outputFile := filepath.Join(tempDir, "normalized_result.csv")
	err = csvHandler.WriteCSV(outputFile, outputData)
	require.NoError(t, err)

	// 驗證輸出文件
	content, err := os.ReadFile(outputFile)
	require.NoError(t, err)
	require.Contains(t, string(content), "Time,Ch1,Ch2")
	require.Contains(t, string(content), "2.000")
	require.Contains(t, string(content), "4.000")
}

// TestFullWorkflow_PhaseAnalysis 測試完整的階段分析流程
func TestFullWorkflow_PhaseAnalysis(t *testing.T) {
	// 準備測試配置
	cfg := &config.AppConfig{
		ScalingFactor: 10,
		Precision:     2,
		BOMEnabled:    false,
		OutputFormat:  "csv",
		PhaseLabels:   []string{"Phase1", "Phase2", "Phase3", "Phase4"},
	}

	// 創建模組實例
	csvHandler := io.NewCSVHandler(cfg)
	analyzer := calculator.NewPhaseAnalyzer(cfg.ScalingFactor, cfg.PhaseLabels)

	// 準備測試數據
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, "phase_data.csv")

	// 數據跨越兩個階段（0.5秒在第一階段，1.5秒在第二階段，2.5秒在第三階段）
	testData := "Time,Ch1,Ch2\n0.5,100,50\n1.5,200,100\n2.5,150,75\n"
	err := os.WriteFile(dataFile, []byte(testData), 0644)
	require.NoError(t, err)

	// 讀取數據
	records, err := csvHandler.ReadCSV(dataFile)
	require.NoError(t, err)

	// 定義階段時間點（5個時間點定義4個階段）
	phaseStrings := []string{"0.0", "1.0", "2.0", "3.0", "4.0"}

	// 執行階段分析
	result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
	require.NoError(t, err)
	require.Len(t, result.PhaseResults, 4)

	// 驗證第一階段（0.0-1.0秒，包含0.5秒的數據）
	phase1 := result.PhaseResults[0]
	require.Equal(t, "Phase1", phase1.PhaseName)
	require.Equal(t, 1e+12, phase1.MaxValues[0])  // Ch1最大值 (100 * 10^10)
	require.Equal(t, 5e+11, phase1.MaxValues[1])  // Ch2最大值 (50 * 10^10)
	require.Equal(t, 1e+12, phase1.MeanValues[0]) // Ch1平均值 (100 * 10^10)
	require.Equal(t, 5e+11, phase1.MeanValues[1]) // Ch2平均值 (50 * 10^10)

	// 驗證第二階段（1.0-2.0秒，包含1.5秒的數據）
	phase2 := result.PhaseResults[1]
	require.Equal(t, "Phase2", phase2.PhaseName)
	require.Equal(t, 2e+12, phase2.MaxValues[0]) // Ch1最大值 (200 * 10^10)
	require.Equal(t, 1e+12, phase2.MaxValues[1]) // Ch2最大值 (100 * 10^10)

	// 驗證第三階段（2.0-3.0秒，包含2.5秒的數據）
	phase3 := result.PhaseResults[2]
	require.Equal(t, "Phase3", phase3.PhaseName)
	require.Equal(t, 1.5e+12, phase3.MaxValues[0]) // Ch1最大值 (150 * 10^10)
	require.Equal(t, 7.5e+11, phase3.MaxValues[1]) // Ch2最大值 (75 * 10^10)

	// 第四階段應該沒有數據（在我們的測試數據範圍外）
	phase4 := result.PhaseResults[3]
	require.Equal(t, "Phase4", phase4.PhaseName)
	require.Len(t, phase4.MaxValues, 0) // 無數據

	// 生成每個階段的輸出文件
	for i, phaseResult := range result.PhaseResults {
		outputData := csvHandler.ConvertPhaseAnalysisToCSV(records[0], &phaseResult, result.MaxTimeIndex)
		outputFile := filepath.Join(tempDir, "phase_"+phaseResult.PhaseName+".csv")

		err := csvHandler.WriteCSV(outputFile, outputData)
		require.NoError(t, err)

		// 驗證輸出文件內容
		content, err := os.ReadFile(outputFile)
		require.NoError(t, err)
		require.Contains(t, string(content), phaseResult.PhaseName+" 最大值")
		require.Contains(t, string(content), phaseResult.PhaseName+" 平均值")

		t.Logf("階段 %d (%s) 分析完成", i+1, phaseResult.PhaseName)
	}
}

// TestFullWorkflow_ConfigurationManagement 測試完整的配置管理流程
func TestFullWorkflow_ConfigurationManagement(t *testing.T) {
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "test_config.json")

	// 測試配置保存
	originalConfig := &config.AppConfig{
		ScalingFactor: 8,
		Precision:     4,
		BOMEnabled:    true,
		OutputFormat:  "json",
		PhaseLabels:   []string{"測試階段1", "測試階段2", "測試階段3"},
	}

	err := originalConfig.SaveConfig(configFile)
	require.NoError(t, err)

	// 測試配置載入
	loadedConfig, err := config.LoadConfig(configFile)
	require.NoError(t, err)

	// 驗證載入的配置
	require.Equal(t, originalConfig.ScalingFactor, loadedConfig.ScalingFactor)
	require.Equal(t, originalConfig.Precision, loadedConfig.Precision)
	require.Equal(t, originalConfig.BOMEnabled, loadedConfig.BOMEnabled)
	require.Equal(t, originalConfig.OutputFormat, loadedConfig.OutputFormat)
	require.Equal(t, originalConfig.PhaseLabels, loadedConfig.PhaseLabels)

	// 測試配置驗證
	err = loadedConfig.Validate()
	require.NoError(t, err)

	// 測試使用載入的配置創建模組
	csvHandler := io.NewCSVHandler(loadedConfig)
	require.NotNil(t, csvHandler)

	maxMeanCalc := calculator.NewMaxMeanCalculator(loadedConfig.ScalingFactor)
	require.NotNil(t, maxMeanCalc)

	normalizer := calculator.NewNormalizer(loadedConfig.ScalingFactor)
	require.NotNil(t, normalizer)

	analyzer := calculator.NewPhaseAnalyzer(loadedConfig.ScalingFactor, loadedConfig.PhaseLabels)
	require.NotNil(t, analyzer)
}

// TestFullWorkflow_ErrorHandling 測試完整流程的錯誤處理
func TestFullWorkflow_ErrorHandling(t *testing.T) {
	cfg := config.DefaultConfig()
	csvHandler := io.NewCSVHandler(cfg)
	maxMeanCalc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)

	t.Run("InvalidCSVFile", func(t *testing.T) {
		// 測試讀取不存在的文件
		_, err := csvHandler.ReadCSV("nonexistent.csv")
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法開啟檔案")
	})

	t.Run("InvalidInputData", func(t *testing.T) {
		// 測試無效的CSV數據
		invalidRecords := [][]string{
			{"Time", "Ch1"},
			{"invalid_time", "100"},
		}
		_, err := maxMeanCalc.CalculateFromRawData(invalidRecords, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "解析時間值失敗")
	})

	t.Run("InvalidWindowSize", func(t *testing.T) {
		// 測試無效的窗口大小
		validRecords := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
		}
		_, err := maxMeanCalc.CalculateFromRawData(validRecords, 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "窗口大小必須大於 0")
	})
}

// TestFullWorkflow_Performance 測試大數據集的性能
func TestFullWorkflow_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("跳過性能測試")
	}

	cfg := config.DefaultConfig()
	csvHandler := io.NewCSVHandler(cfg)
	maxMeanCalc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)

	// 生成大量測試數據
	tempDir := t.TempDir()
	largeFile := filepath.Join(tempDir, "large_data.csv")

	// 創建包含1000行數據的文件
	var builder strings.Builder
	builder.WriteString("Time,Ch1,Ch2,Ch3\n")
	for i := 0; i < 1000; i++ {
		builder.WriteString(fmt.Sprintf("%d.0,%d,%d,%d\n", i, i*10, i*20, i*30))
	}

	err := os.WriteFile(largeFile, []byte(builder.String()), 0644)
	require.NoError(t, err)

	// 測試讀取大文件
	records, err := csvHandler.ReadCSV(largeFile)
	require.NoError(t, err)
	require.Len(t, records, 1001) // 標題 + 1000行數據

	// 測試大數據集的計算性能
	results, err := maxMeanCalc.CalculateFromRawData(records, 10)
	require.NoError(t, err)
	require.Len(t, results, 3) // 3個通道

	// 驗證計算結果
	for _, result := range results {
		require.Greater(t, result.MaxMean, 0.0)
		require.GreaterOrEqual(t, result.EndTime, result.StartTime)
	}

	t.Log("大數據集性能測試完成")
}
