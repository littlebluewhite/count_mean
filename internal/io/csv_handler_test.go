package io

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/csvutil"
	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

func TestNewCSVHandler(t *testing.T) {
	cfg := config.DefaultConfig()
	handler := NewCSVHandler(cfg)
	require.NotNil(t, handler)
}

func TestCSVHandler_ReadCSV(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	// Set allowed directories to include temp directory
	cfg.InputDir = tempDir
	cfg.OutputDir = tempDir
	cfg.OperateDir = tempDir
	handler := NewCSVHandler(cfg)

	t.Run("FileNotExists", func(t *testing.T) {
		// 讀 InputDir 內不存在的檔，通過 ReadCSV 的路徑驗證後落到
		// 「file-not-found」。
		records, err := handler.ReadCSV(filepath.Join(tempDir, "nonexistent.csv"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法獲取文件信息")
		require.Nil(t, records)
	})

	t.Run("ValidCSVFile", func(t *testing.T) {
		csvFile := filepath.Join(tempDir, "test.csv")

		csvContent := "Time,Ch1,Ch2\n1.0,100,50\n2.0,200,100\n"
		err := os.WriteFile(csvFile, []byte(csvContent), fsperm.FilePerm)
		require.NoError(t, err)

		records, err := handler.ReadCSV(csvFile)
		require.NoError(t, err)
		require.Len(t, records, 3) // 標題 + 2行數據
		require.Equal(t, []string{"Time", "Ch1", "Ch2"}, records[0])
		require.Equal(t, []string{"1.0", "100", "50"}, records[1])
		require.Equal(t, []string{"2.0", "200", "100"}, records[2])
	})

	t.Run("EmptyFile", func(t *testing.T) {
		csvFile := filepath.Join(tempDir, "empty.csv")

		err := os.WriteFile(csvFile, []byte(""), fsperm.FilePerm)
		require.NoError(t, err)

		records, err := handler.ReadCSV(csvFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "至少需要包含標題行和一行數據")
		require.Nil(t, records)
	})

	t.Run("OnlyHeader", func(t *testing.T) {
		csvFile := filepath.Join(tempDir, "header_only.csv")

		csvContent := "Time,Ch1,Ch2\n"
		err := os.WriteFile(csvFile, []byte(csvContent), fsperm.FilePerm)
		require.NoError(t, err)

		records, err := handler.ReadCSV(csvFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "至少需要包含標題行和一行數據")
		require.Nil(t, records)
	})

	t.Run("MalformedCSV", func(t *testing.T) {
		csvFile := filepath.Join(tempDir, "malformed.csv")

		// 包含未封閉的引號
		csvContent := "Time,Ch1\n1.0,\"unclosed quote\n2.0,100\n"
		err := os.WriteFile(csvFile, []byte(csvContent), fsperm.FilePerm)
		require.NoError(t, err)

		records, err := handler.ReadCSV(csvFile)
		require.Error(t, err)
		require.Contains(t, err.Error(), "無法讀取 CSV 資料")
		require.Nil(t, records)
	})
}

func TestCSVHandler_WriteFileCSV(t *testing.T) {
	t.Run("WriteWithBOM", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := config.DefaultConfig()
		cfg.BOMEnabled = true
		// Set allowed directories to include temp directory
		cfg.InputDir = tempDir
		cfg.OutputDir = tempDir
		cfg.OperateDir = tempDir
		handler := NewCSVHandler(cfg)

		data := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
			{"2.0", "200"},
		}

		csvFile, err := handler.WriteFileCSV(WriteRequest{Filename: "output_with_bom.csv"}, data)
		require.NoError(t, err)

		// 檢查文件內容
		content, err := os.ReadFile(csvFile)
		require.NoError(t, err)

		// 檢查BOM
		require.True(t, len(content) >= 3)
		require.Equal(t, csvutil.BOMBytes(), content[:3])

		// 檢查CSV內容
		csvContent := string(content[3:])
		lines := strings.Split(strings.TrimSpace(csvContent), "\n")
		require.Len(t, lines, 3)
		require.Equal(t, "Time,Ch1", lines[0])
		require.Equal(t, "1.0,100", lines[1])
		require.Equal(t, "2.0,200", lines[2])
	})

	t.Run("WriteWithoutBOM", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg := config.DefaultConfig()
		cfg.BOMEnabled = false
		// Set allowed directories to include temp directory
		cfg.InputDir = tempDir
		cfg.OutputDir = tempDir
		cfg.OperateDir = tempDir
		handler := NewCSVHandler(cfg)

		data := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
		}

		csvFile, err := handler.WriteFileCSV(WriteRequest{Filename: "output_without_bom.csv"}, data)
		require.NoError(t, err)

		// 檢查文件內容
		content, err := os.ReadFile(csvFile)
		require.NoError(t, err)

		// 不應包含BOM
		csvContent := string(content)
		require.True(t, strings.HasPrefix(csvContent, "Time,Ch1"))
		require.False(t, strings.HasPrefix(csvContent, string(csvutil.BOMBytes())))
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
			// Scaled result: 1500 divided by 10 to the power of 10 equals 0.15
			{ColumnIndex: 1, StartTime: 1.0, EndTime: 2.0, MaxMean: 1500.0},
			// Scaled result: 2000 divided by 10 to the power of 10 equals 0.20
			{ColumnIndex: 2, StartTime: 1.5, EndTime: 2.5, MaxMean: 2000.0},
		}

		data := handler.converter.ConvertMaxMeanResults(headers, results, 0.5, 3.0)
		require.Len(t, data, 6) // 標題 + 5行結果

		// 檢查標題
		require.Equal(t, headers, data[0])

		// 檢查開始範圍秒數行
		require.Equal(t, "開始範圍秒數", data[1][0])
		require.Equal(t, "0.50", data[1][1])
		require.Equal(t, "0.50", data[1][2])

		// 檢查結束範圍秒數行
		require.Equal(t, "結束範圍秒數", data[2][0])
		require.Equal(t, "3.00", data[2][1])
		require.Equal(t, "3.00", data[2][2])

		// 檢查開始計算秒數行
		require.Equal(t, "開始計算秒數", data[3][0])
		require.Equal(t, "0.00", data[3][1]) // 1.0/10^10 = 0.0000000001 with "%.2f" becomes "0.00"
		require.Equal(t, "0.00", data[3][2]) // 1.5/10^10 = 0.00000000015 with "%.2f" becomes "0.00"

		// 檢查結束計算秒數行
		require.Equal(t, "結束計算秒數", data[4][0])
		require.Equal(t, "0.00", data[4][1]) // 2.0/10^10 = 0.0000000002 with "%.2f" becomes "0.00"
		require.Equal(t, "0.00", data[4][2]) // 2.5/10^10 = 0.00000000025 with "%.2f" becomes "0.00"

		// 檢查最大平均值行（應用縮放因子）
		require.Equal(t, "最大平均值", data[5][0])
		require.Equal(t, "0.00", data[5][1]) // 1500/10^10 = 0.00000000015 用精度2格式化為 0.00
		require.Equal(t, "0.00", data[5][2]) // 2000/10^10 = 0.00000000020 用精度2格式化為 0.00
	})

	t.Run("EmptyResults", func(t *testing.T) {
		headers := []string{"Time", "Ch1"}
		results := []models.MaxMeanResult{}

		data := handler.converter.ConvertMaxMeanResults(headers, results, 0.0, 10.0)
		require.Len(t, data, 6) // 標題 + 5行（只有標籤）
		require.Equal(t, headers, data[0])
		require.Equal(t, []string{"開始範圍秒數"}, data[1])
		require.Equal(t, []string{"結束範圍秒數"}, data[2])
		require.Equal(t, []string{"開始計算秒數"}, data[3])
		require.Equal(t, []string{"結束計算秒數"}, data[4])
		require.Equal(t, []string{"最大平均值"}, data[5])
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

		data := handler.converter.ConvertMaxMeanResults(headers, results, 0.5, 4.0)
		require.Equal(t, "0.5000", data[1][1]) // 開始範圍 (precision 4)
		require.Equal(t, "4.0000", data[2][1]) // 結束範圍 (precision 4)
		require.Equal(t, "0.0000", data[3][1]) // 開始計算時間: 1.123456/10^6 = 0.000001123456 with precision 4 becomes "0.0000"
		require.Equal(t, "0.0000", data[4][1]) // 結束計算時間: 2.987654/10^6 = 0.000002987654 with precision 4 becomes "0.0000"
		require.Equal(t, "0.1235", data[5][1]) // 最大平均值: 123456.0/10^6 = 0.123456 with precision 4 becomes "0.1235"
	})
}

func TestCSVHandler_ConvertNormalizedDataToCSV(t *testing.T) {
	cfg := &config.AppConfig{
		Precision: 3,
	}
	handler := NewCSVHandler(cfg)

	t.Run("ValidConversion", func(t *testing.T) {
		dataset := &models.EMGDataset{
			Headers:               []string{"Time", "Ch1", "Ch2"},
			OriginalTimePrecision: 2, // 設置時間精度為2位小數
			Data: []models.EMGData{
				{Time: 1.0, Channels: []float64{2.5, 3.75}},
				{Time: 2.0, Channels: []float64{1.25, 5.0}},
			},
		}

		data := handler.converter.ConvertNormalizedData(dataset)
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

		data := handler.converter.ConvertNormalizedData(dataset)
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

		data := handler.converter.ConvertPhaseAnalysis(headers, result, maxTimeIndex)
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
		require.Equal(t, "0.00", data[3][1]) // 1.25/10^10 = 0.000000000125 with "%.2f" becomes "0.00"
		require.Equal(t, "0.00", data[3][2]) // 2.75/10^10 = 0.000000000275 with "%.2f" becomes "0.00"
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

		data := handler.converter.ConvertPhaseAnalysis(headers, result, maxTimeIndex)
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
		require.Equal(t, "0.00", data[3][1]) // Ch1: 1.25/10^10 = 0.000000000125 with "%.2f" becomes "0.00"
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

		data := handler.converter.ConvertPhaseAnalysis(headers, result, maxTimeIndex)
		require.Len(t, data, 3) // 沒有時間行
		require.Equal(t, "測試階段 最大值", data[1][0])
		require.Equal(t, "測試階段 平均值", data[2][0])
	})
}

func TestBOMBytes(t *testing.T) {
	require.Equal(t, []byte{0xEF, 0xBB, 0xBF}, csvutil.BOMBytes())
	require.Len(t, csvutil.BOMBytes(), 3)
}

// TestCSVHandler_ReadCSV_StripsLeadingBOM 釘住 cross-compare review 真正 bug:
// readAndParseCSV 過去直接 csv.NewReader 沒處理 UTF-8 BOM，導致 Excel 匯出的
// CSV 開頭 0xEF 0xBB 0xBF 會污染 records[0][0]，下游 caller 把帶 BOM 的 header
// 寫進輸出 CSV / 用於 channel 比對都會 broken。
// parsers/csv_reader.go 已在 PR-E 用 PeekBOM 修，這條 io 路徑漏修，testdata
// 過去全部無 BOM 沒能 cover。
func TestCSVHandler_ReadCSV_StripsLeadingBOM(t *testing.T) {
	tempDir := t.TempDir()
	csvPath := filepath.Join(tempDir, "bom.csv")

	// 用 csvutil.BOMBytes() 而非字面值避免 source 含 illegal BOM
	content := append([]byte{}, csvutil.BOMBytes()...)
	content = append(content, []byte("Time,Ch1,Ch2\n0.001,100,200\n")...)
	require.NoError(t, os.WriteFile(csvPath, content, fsperm.FilePerm))

	cfg := config.DefaultConfig()
	cfg.InputDir = tempDir
	cfg.OutputDir = tempDir
	cfg.OperateDir = tempDir
	handler := NewCSVHandler(cfg)

	records, err := handler.ReadCSV(csvPath)
	require.NoError(t, err)
	require.Len(t, records, 2)

	// headers 必須是純字串不含 BOM (regression: 過去回傳 U+FEFF + "Time")
	// 用 csvutil.BOMBytes() 動態比對而非字面 BOM 字元，避免 Go source 含 illegal BOM
	require.Equal(t, "Time", records[0][0])
	require.False(t, bytes.HasPrefix([]byte(records[0][0]), csvutil.BOMBytes()),
		"records[0][0] still has BOM prefix: %q", records[0][0])

	// 資料列不該被誤剝 (BOM 只剝檔頭一次)
	require.Equal(t, "0.001", records[1][0])
}

// TestCSVHandler_ReadCSV_StrictCSVDefenses 釘住 csv.NewReader 在 readAndParseCSV
// 必須跑 strict defaults — FieldsPerRecord 自動鎖定 header 欄位數、LazyQuotes=false
// 拒絕未配對引號、ReuseRecord=false 不複用 backing array。
//
// 此 test 透過三種 attacker-controlled 變形確認 fail-fast：
//  1. 未配對引號（quote-injection / DoS）：應在 ReadAll 階段直接 error
//  2. 欄位數不一致（jagged rows）：FieldsPerRecord=0 由 reader 鎖定第一筆，後續錯誤
//  3. 正常 CSV：strict defaults 不應影響合法輸入
func TestCSVHandler_ReadCSV_StrictCSVDefenses(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = tempDir
	cfg.OutputDir = tempDir
	cfg.OperateDir = tempDir
	handler := NewCSVHandler(cfg)

	t.Run("RejectsUnpairedQuote", func(t *testing.T) {
		// LazyQuotes=false：未配對引號 fail-fast。
		path := filepath.Join(tempDir, "unpaired_quote.csv")
		// 第一個 data field 開頭 " 但永遠不關閉，跨多行直到 EOF
		csvContent := "Time,Ch1\n1.0,\"never closed\n2.0,123\n3.0,456"
		require.NoError(t, os.WriteFile(path, []byte(csvContent), fsperm.FilePerm))

		_, err := handler.ReadCSV(path)
		require.Error(t, err, "未配對引號必須 fail-fast (LazyQuotes=false)")
	})

	t.Run("RejectsJaggedRows", func(t *testing.T) {
		// FieldsPerRecord=0：header 鎖 N 欄，data row 欄位數不符直接 error。
		path := filepath.Join(tempDir, "jagged.csv")
		csvContent := "Time,Ch1,Ch2\n1.0,100,200\n2.0,300\n" // 第三行只有 2 欄
		require.NoError(t, os.WriteFile(path, []byte(csvContent), fsperm.FilePerm))

		_, err := handler.ReadCSV(path)
		require.Error(t, err, "欄位數不一致必須被擋 (FieldsPerRecord=0)")
	})

	t.Run("AcceptsCleanCSV", func(t *testing.T) {
		// Strict defaults 不應影響合法輸入。
		path := filepath.Join(tempDir, "clean.csv")
		csvContent := "Time,Ch1,Ch2\n1.0,100,200\n2.0,300,400\n"
		require.NoError(t, os.WriteFile(path, []byte(csvContent), fsperm.FilePerm))

		records, err := handler.ReadCSV(path)
		require.NoError(t, err, "乾淨 CSV 必須通過 strict defenses")
		require.Len(t, records, 3)
	})
}
