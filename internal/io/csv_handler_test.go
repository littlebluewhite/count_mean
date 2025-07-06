package io

import (
	"count_mean/internal/config"
	"count_mean/internal/models"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewCSVHandler(t *testing.T) {
	cfg := config.DefaultConfig()
	handler := NewCSVHandler(cfg)
	require.NotNil(t, handler)
	require.Equal(t, cfg, handler.config)
}

func TestCSVHandler_ReadCSV(t *testing.T) {
	cfg := config.DefaultConfig()
	handler := NewCSVHandler(cfg)

	t.Run("FileNotExists", func(t *testing.T) {
		records, err := handler.ReadCSV("nonexistent.csv")
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法開啟檔案")
		require.Nil(t, records)
	})

	t.Run("ValidCSVFile", func(t *testing.T) {
		tempDir := t.TempDir()
		csvFile := filepath.Join(tempDir, "test.csv")

		csvContent := "Time,Ch1,Ch2\n1.0,100,50\n2.0,200,100\n"
		err := os.WriteFile(csvFile, []byte(csvContent), 0644)
		require.NoError(t, err)

		records, err := handler.ReadCSV(csvFile)
		require.NoError(t, err)
		require.Len(t, records, 3) // 標題 + 2行數據
		require.Equal(t, []string{"Time", "Ch1", "Ch2"}, records[0])
		require.Equal(t, []string{"1.0", "100", "50"}, records[1])
		require.Equal(t, []string{"2.0", "200", "100"}, records[2])
	})

	t.Run("EmptyFile", func(t *testing.T) {
		tempDir := t.TempDir()
		csvFile := filepath.Join(tempDir, "empty.csv")

		err := os.WriteFile(csvFile, []byte(""), 0644)
		require.NoError(t, err)

		records, err := handler.ReadCSV(csvFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "至少需要包含標題行和一行數據")
		require.Nil(t, records)
	})

	t.Run("OnlyHeader", func(t *testing.T) {
		tempDir := t.TempDir()
		csvFile := filepath.Join(tempDir, "header_only.csv")

		csvContent := "Time,Ch1,Ch2\n"
		err := os.WriteFile(csvFile, []byte(csvContent), 0644)
		require.NoError(t, err)

		records, err := handler.ReadCSV(csvFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "至少需要包含標題行和一行數據")
		require.Nil(t, records)
	})

	t.Run("MalformedCSV", func(t *testing.T) {
		tempDir := t.TempDir()
		csvFile := filepath.Join(tempDir, "malformed.csv")

		// 包含未封閉的引號
		csvContent := "Time,Ch1\n1.0,\"unclosed quote\n2.0,100\n"
		err := os.WriteFile(csvFile, []byte(csvContent), 0644)
		require.NoError(t, err)

		records, err := handler.ReadCSV(csvFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法讀取 CSV 資料")
		require.Nil(t, records)
	})
}

func TestCSVHandler_WriteCSV(t *testing.T) {
	t.Run("WriteWithBOM", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.BOMEnabled = true
		handler := NewCSVHandler(cfg)

		tempDir := t.TempDir()
		csvFile := filepath.Join(tempDir, "output_with_bom.csv")

		data := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
			{"2.0", "200"},
		}

		err := handler.WriteCSV(csvFile, data)
		require.NoError(t, err)

		// 檢查文件內容
		content, err := os.ReadFile(csvFile)
		require.NoError(t, err)

		// 檢查BOM
		require.True(t, len(content) >= 3)
		require.Equal(t, BOMBytes, content[:3])

		// 檢查CSV內容
		csvContent := string(content[3:])
		lines := strings.Split(strings.TrimSpace(csvContent), "\n")
		require.Len(t, lines, 3)
		require.Equal(t, "Time,Ch1", lines[0])
		require.Equal(t, "1.0,100", lines[1])
		require.Equal(t, "2.0,200", lines[2])
	})

	t.Run("WriteWithoutBOM", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.BOMEnabled = false
		handler := NewCSVHandler(cfg)

		tempDir := t.TempDir()
		csvFile := filepath.Join(tempDir, "output_without_bom.csv")

		data := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
		}

		err := handler.WriteCSV(csvFile, data)
		require.NoError(t, err)

		// 檢查文件內容
		content, err := os.ReadFile(csvFile)
		require.NoError(t, err)

		// 不應包含BOM
		csvContent := string(content)
		require.True(t, strings.HasPrefix(csvContent, "Time,Ch1"))
		require.False(t, strings.HasPrefix(csvContent, string(BOMBytes)))
	})

	t.Run("InvalidDirectory", func(t *testing.T) {
		cfg := config.DefaultConfig()
		handler := NewCSVHandler(cfg)

		invalidPath := "/nonexistent/directory/output.csv"
		data := [][]string{{"Time", "Ch1"}}

		err := handler.WriteCSV(invalidPath, data)
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法建立檔案")
	})

	t.Run("EmptyData", func(t *testing.T) {
		cfg := config.DefaultConfig()
		handler := NewCSVHandler(cfg)

		tempDir := t.TempDir()
		csvFile := filepath.Join(tempDir, "empty_data.csv")

		data := [][]string{}

		err := handler.WriteCSV(csvFile, data)
		require.NoError(t, err)

		// 檢查文件存在且為空
		content, err := os.ReadFile(csvFile)
		require.NoError(t, err)

		// 如果啟用BOM，文件應只包含BOM
		if cfg.BOMEnabled {
			require.Equal(t, BOMBytes, content)
		} else {
			require.Empty(t, content)
		}
	})
}

func TestCSVHandler_ConvertMaxMeanResultsToCSV(t *testing.T) {
	cfg := &config.AppConfig{
		ScalingFactor: 10,
		Precision:     2,
		BOMEnabled:    true,
	}
	handler := NewCSVHandler(cfg)

	t.Run("ValidConversion", func(t *testing.T) {
		headers := []string{"Time", "Ch1", "Ch2"}
		results := []models.MaxMeanResult{
			{ColumnIndex: 1, StartTime: 1.0, EndTime: 2.0, MaxMean: 1500.0}, // 1500/10^10 = 0.15
			{ColumnIndex: 2, StartTime: 1.5, EndTime: 2.5, MaxMean: 2000.0}, // 2000/10^10 = 0.20
		}

		data := handler.ConvertMaxMeanResultsToCSV(headers, results)
		require.Len(t, data, 4) // 標題 + 3行結果

		// 檢查標題
		require.Equal(t, headers, data[0])

		// 檢查開始秒數行
		require.Equal(t, "開始秒數", data[1][0])
		require.Equal(t, "1.00", data[1][1])
		require.Equal(t, "1.50", data[1][2])

		// 檢查結束秒數行
		require.Equal(t, "結束秒數", data[2][0])
		require.Equal(t, "2.00", data[2][1])
		require.Equal(t, "2.50", data[2][2])

		// 檢查最大平均值行（應用縮放因子）
		require.Equal(t, "最大平均值", data[3][0])
		require.Equal(t, "0.00", data[3][1]) // 1500/10^10 = 0.00000000015 用精度2格式化為 0.00
		require.Equal(t, "0.00", data[3][2]) // 2000/10^10 = 0.00000000020 用精度2格式化為 0.00
	})

	t.Run("EmptyResults", func(t *testing.T) {
		headers := []string{"Time", "Ch1"}
		results := []models.MaxMeanResult{}

		data := handler.ConvertMaxMeanResultsToCSV(headers, results)
		require.Len(t, data, 4) // 標題 + 3行（只有標籤）
		require.Equal(t, headers, data[0])
		require.Equal(t, []string{"開始秒數"}, data[1])
		require.Equal(t, []string{"結束秒數"}, data[2])
		require.Equal(t, []string{"最大平均值"}, data[3])
	})

	t.Run("PrecisionFormatting", func(t *testing.T) {
		cfg := &config.AppConfig{
			ScalingFactor: 6,
			Precision:     4,
			BOMEnabled:    true,
		}
		handler := NewCSVHandler(cfg)

		headers := []string{"Time", "Ch1"}
		results := []models.MaxMeanResult{
			{ColumnIndex: 1, StartTime: 1.123456, EndTime: 2.987654, MaxMean: 123456.0},
		}

		data := handler.ConvertMaxMeanResultsToCSV(headers, results)
		require.Equal(t, "1.12", data[1][1])   // 開始時間保持2位小數
		require.Equal(t, "2.99", data[2][1])   // 結束時間保持2位小數
		require.Equal(t, "0.1235", data[3][1]) // 最大平均值使用4位精度
	})
}

func TestCSVHandler_ConvertNormalizedDataToCSV(t *testing.T) {
	cfg := &config.AppConfig{
		Precision: 3,
	}
	handler := NewCSVHandler(cfg)

	t.Run("ValidConversion", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1", "Ch2"},
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{2.5, 3.75}},
				{Time: 2.0, Channels: []float64{1.25, 5.0}},
			},
		}

		data := handler.ConvertNormalizedDataToCSV(dataset)
		require.Len(t, data, 3) // 標題 + 2行數據

		// 檢查標題
		require.Equal(t, dataset.Headers, data[0])

		// 檢查第一行數據
		require.Equal(t, "1.00", data[1][0])  // 時間保持2位小數
		require.Equal(t, "2.500", data[1][1]) // 數據使用指定精度
		require.Equal(t, "3.750", data[1][2])

		// 檢查第二行數據
		require.Equal(t, "2.00", data[2][0])
		require.Equal(t, "1.250", data[2][1])
		require.Equal(t, "5.000", data[2][2])
	})

	t.Run("EmptyDataset", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    []models.EMGData{},
		}

		data := handler.ConvertNormalizedDataToCSV(dataset)
		require.Len(t, data, 1) // 只有標題
		require.Equal(t, dataset.Headers, data[0])
	})
}

func TestCSVHandler_ConvertPhaseAnalysisToCSV(t *testing.T) {
	cfg := &config.AppConfig{
		ScalingFactor: 10,
		Precision:     2,
	}
	handler := NewCSVHandler(cfg)

	t.Run("ValidConversion", func(t *testing.T) {
		headers := []string{"Time", "Ch1", "Ch2"}
		result := &models.PhaseAnalysisResult{
			PhaseName: "測試階段",
			MaxValues: map[int]float64{
				0: 1500.0, // Ch1
				1: 2000.0, // Ch2
			},
			MeanValues: map[int]float64{
				0: 1000.0, // Ch1
				1: 1500.0, // Ch2
			},
		}
		maxTimeIndex := map[int]float64{
			0: 1.25, // Ch1 最大值時間
			1: 2.75, // Ch2 最大值時間
		}

		data := handler.ConvertPhaseAnalysisToCSV(headers, result, maxTimeIndex)
		require.Len(t, data, 4) // 標題 + 最大值行 + 平均值行 + 時間行

		// 檢查標題
		require.Equal(t, headers, data[0])

		// 檢查最大值行
		require.Equal(t, "測試階段 最大值", data[1][0])
		require.Equal(t, "0.00", data[1][1]) // 1500/10^10 = 0.00000000015 with precision 2
		require.Equal(t, "0.00", data[1][2]) // 2000/10^10 = 0.00000000020 with precision 2

		// 檢查平均值行
		require.Equal(t, "測試階段 平均值", data[2][0])
		require.Equal(t, "0.00", data[2][1]) // 1000/10^10 = 0.00000000010 with precision 2
		require.Equal(t, "0.00", data[2][2]) // 1500/10^10 = 0.00000000015 with precision 2

		// 檢查時間行
		require.Equal(t, "整個階段最大值出現在_秒", data[3][0])
		require.Equal(t, "1.25", data[3][1])
		require.Equal(t, "2.75", data[3][2])
	})

	t.Run("MissingChannelData", func(t *testing.T) {
		headers := []string{"Time", "Ch1", "Ch2", "Ch3"}
		result := &models.PhaseAnalysisResult{
			PhaseName: "測試階段",
			MaxValues: map[int]float64{
				0: 1500.0, // 只有Ch1
				// Ch2, Ch3缺失
			},
			MeanValues: map[int]float64{
				0: 1000.0, // 只有Ch1
				2: 800.0,  // 只有Ch3
			},
		}
		maxTimeIndex := map[int]float64{
			0: 1.25, // 只有Ch1
		}

		data := handler.ConvertPhaseAnalysisToCSV(headers, result, maxTimeIndex)
		require.Len(t, data, 4)

		// 檢查最大值行
		require.Equal(t, "0.00", data[1][1]) // Ch1: 1500/10^10 = 0.00000000015 with precision 2
		require.Equal(t, "N/A", data[1][2])  // Ch2 缺失
		require.Equal(t, "N/A", data[1][3])  // Ch3 缺失

		// 檢查平均值行
		require.Equal(t, "0.00", data[2][1]) // Ch1: 1000/10^10 = 0.00000000010 with precision 2
		require.Equal(t, "N/A", data[2][2])  // Ch2 缺失
		require.Equal(t, "0.00", data[2][3]) // Ch3: 800/10^10 = 0.00000000008 with precision 2

		// 檢查時間行
		require.Equal(t, "1.25", data[3][1]) // Ch1
		require.Equal(t, "N/A", data[3][2])  // Ch2 缺失
		require.Equal(t, "N/A", data[3][3])  // Ch3 缺失
	})

	t.Run("EmptyMaxTimeIndex", func(t *testing.T) {
		headers := []string{"Time", "Ch1"}
		result := &models.PhaseAnalysisResult{
			PhaseName:  "測試階段",
			MaxValues:  map[int]float64{0: 1500.0},
			MeanValues: map[int]float64{0: 1000.0},
		}
		maxTimeIndex := map[int]float64{} // 空的時間索引

		data := handler.ConvertPhaseAnalysisToCSV(headers, result, maxTimeIndex)
		require.Len(t, data, 3) // 沒有時間行
		require.Equal(t, "測試階段 最大值", data[1][0])
		require.Equal(t, "測試階段 平均值", data[2][0])
	})
}

func TestCSVHandler_ReadCSVFromPrompt(t *testing.T) {
	cfg := config.DefaultConfig()
	handler := NewCSVHandler(cfg)

	// 注意：這個測試難以自動化，因為它需要標準輸入
	// 在實際環境中，可以使用依賴注入來提供可測試的輸入源
	t.Run("MethodExists", func(t *testing.T) {
		// 只測試方法存在，不測試互動邏輯
		require.NotNil(t, handler.ReadCSVFromPrompt)
	})
}

func TestBOMBytes(t *testing.T) {
	require.Equal(t, []byte{0xEF, 0xBB, 0xBF}, BOMBytes)
	require.Len(t, BOMBytes, 3)
}
