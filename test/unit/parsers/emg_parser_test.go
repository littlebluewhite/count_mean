package parsers

import (
	"os"
	"testing"

	"count_mean/internal/models"
	"count_mean/internal/parsers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEMGParser(t *testing.T) {
	parser := parsers.NewEMGParser()
	assert.NotNil(t, parser)
	assert.Equal(t, 0.001, parser.GetSampleInterval()) // 1000Hz = 0.001s interval
}

func TestEMGParser_ParseFile(t *testing.T) {
	tests := []struct {
		name       string
		csvContent string
		wantErr    bool
		checkData  func(*testing.T, *models.PhaseSyncEMGData)
	}{
		{
			name: "valid EMG file",
			csvContent: `Time,Ch1,Ch2,Ch3
0.000,100.5,200.3,150.8
0.001,101.2,199.7,151.2
0.002,99.8,201.1,149.5
0.003,102.1,198.9,152.0`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.PhaseSyncEMGData) {
				assert.Len(t, data.Time, 4)
				assert.Equal(t, 0.000, data.Time[0])
				assert.Equal(t, 0.003, data.Time[3])

				assert.Len(t, data.Headers, 3)
				assert.Equal(t, []string{"Ch1", "Ch2", "Ch3"}, data.Headers)

				assert.Len(t, data.Channels, 3)
				assert.Equal(t, 100.5, data.Channels["Ch1"][0])
				assert.Equal(t, 152.0, data.Channels["Ch3"][3])
			},
		},
		{
			name: "EMG file with missing data",
			csvContent: `Time,Ch1,Ch2
0.000,100.5,200.3
0.001,101.2
0.002,99.8,201.1,149.5
0.003,102.1,198.9`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.PhaseSyncEMGData) {
				assert.Len(t, data.Time, 3) // 跳過不完整的行
				assert.Equal(t, []float64{0.000, 0.002, 0.003}, data.Time)
				assert.Len(t, data.Channels["Ch1"], 3)
				assert.Len(t, data.Channels["Ch2"], 3)
			},
		},
		{
			name: "EMG file with invalid time values",
			csvContent: `Time,Ch1,Ch2
invalid_time,100.5,200.3
0.001,101.2,199.7
0.002,99.8,201.1
invalid_time_2,102.1,198.9`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.PhaseSyncEMGData) {
				assert.Len(t, data.Time, 2) // 只有兩行有效數據
				assert.Equal(t, []float64{0.001, 0.002}, data.Time)
			},
		},
		{
			name: "EMG file with invalid channel values",
			csvContent: `Time,Ch1,Ch2
0.000,100.5,invalid
0.001,invalid,199.7
0.002,99.8,201.1`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.PhaseSyncEMGData) {
				assert.Len(t, data.Time, 3)
				// 無效值應該被設為 0
				assert.Equal(t, 0.0, data.Channels["Ch2"][0])
				assert.Equal(t, 0.0, data.Channels["Ch1"][1])
				assert.Equal(t, 99.8, data.Channels["Ch1"][2])
			},
		},
		{
			name:       "empty EMG file",
			csvContent: "",
			wantErr:    true,
		},
		{
			name:       "EMG file without data rows",
			csvContent: `Time,Ch1,Ch2`,
			wantErr:    true,
		},
		{
			name: "EMG file with insufficient headers",
			csvContent: `Time
0.000`,
			wantErr: true,
		},
		{
			name: "EMG file with leading and trailing spaces",
			csvContent: `  Time  ,  Ch1  ,  Ch2  
  0.000  ,  100.5  ,  200.3  
  0.001  ,  101.2  ,  199.7  `,
			wantErr: false,
			checkData: func(t *testing.T, data *models.PhaseSyncEMGData) {
				assert.Len(t, data.Time, 2)
				assert.Equal(t, []string{"Ch1", "Ch2"}, data.Headers)
				assert.Equal(t, 100.5, data.Channels["Ch1"][0])
				assert.Equal(t, 199.7, data.Channels["Ch2"][1])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 創建臨時測試文件
			tmpFile, err := os.CreateTemp("", "test_emg_*.csv")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			// 測試解析
			parser := parsers.NewEMGParser()
			data, err := parser.ParseFile(tmpFile.Name())

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, data)

			if tt.checkData != nil {
				tt.checkData(t, data)
			}
		})
	}
}

func TestEMGParser_ParseFile_FileNotFound(t *testing.T) {
	parser := parsers.NewEMGParser()
	_, err := parser.ParseFile("nonexistent_file.csv")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "無法開啟 EMG 檔案")
}

func TestEMGParser_GetDataInTimeRange(t *testing.T) {
	parser := parsers.NewEMGParser()

	// 創建測試數據
	testData := &models.PhaseSyncEMGData{
		Time:    []float64{0.0, 0.001, 0.002, 0.003, 0.004, 0.005},
		Headers: []string{"Ch1", "Ch2"},
		Channels: map[string][]float64{
			"Ch1": {100.0, 101.0, 102.0, 103.0, 104.0, 105.0},
			"Ch2": {200.0, 201.0, 202.0, 203.0, 204.0, 205.0},
		},
	}

	tests := []struct {
		name      string
		startTime float64
		endTime   float64
		wantErr   bool
		checkLen  int
	}{
		{
			name:      "valid time range",
			startTime: 0.001,
			endTime:   0.003,
			wantErr:   false,
			checkLen:  3, // indices 1, 2, 3
		},
		{
			name:      "start time greater than end time",
			startTime: 0.003,
			endTime:   0.001,
			wantErr:   true,
		},
		{
			name:      "time range outside data",
			startTime: 0.010,
			endTime:   0.020,
			wantErr:   true,
		},
		{
			name:      "exact boundary match",
			startTime: 0.000,
			endTime:   0.005,
			wantErr:   false,
			checkLen:  6, // all data
		},
		{
			name:      "partial range at beginning",
			startTime: 0.000,
			endTime:   0.002,
			wantErr:   false,
			checkLen:  3, // indices 0, 1, 2
		},
		{
			name:      "partial range at end",
			startTime: 0.003,
			endTime:   0.005,
			wantErr:   false,
			checkLen:  3, // indices 3, 4, 5
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rangeData, err := parser.GetDataInTimeRange(testData, tt.startTime, tt.endTime)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, rangeData)
			assert.Len(t, rangeData.Time, tt.checkLen)
			assert.Len(t, rangeData.Channels["Ch1"], tt.checkLen)
			assert.Len(t, rangeData.Channels["Ch2"], tt.checkLen)

			// 檢查時間範圍
			if tt.checkLen > 0 {
				assert.GreaterOrEqual(t, rangeData.Time[0], tt.startTime)
				assert.LessOrEqual(t, rangeData.Time[len(rangeData.Time)-1], tt.endTime)
			}

			// 檢查數據完整性
			for channelName := range rangeData.Channels {
				assert.Len(t, rangeData.Channels[channelName], len(rangeData.Time))
			}
		})
	}
}

func TestCalculateEMGStatistics(t *testing.T) {
	tests := []struct {
		name         string
		data         *models.PhaseSyncEMGData
		expectedMean map[string]float64
		expectedMax  map[string]float64
	}{
		{
			name: "normal data",
			data: &models.PhaseSyncEMGData{
				Time:    []float64{0.0, 0.001, 0.002},
				Headers: []string{"Ch1", "Ch2"},
				Channels: map[string][]float64{
					"Ch1": {100.0, 200.0, 300.0}, // mean = 200.0, max = 300.0
					"Ch2": {50.0, 100.0, 150.0},  // mean = 100.0, max = 150.0
				},
			},
			expectedMean: map[string]float64{
				"Ch1": 200.0,
				"Ch2": 100.0,
			},
			expectedMax: map[string]float64{
				"Ch1": 300.0,
				"Ch2": 150.0,
			},
		},
		{
			name: "single value channels",
			data: &models.PhaseSyncEMGData{
				Time:    []float64{0.0},
				Headers: []string{"Ch1"},
				Channels: map[string][]float64{
					"Ch1": {123.5},
				},
			},
			expectedMean: map[string]float64{
				"Ch1": 123.5,
			},
			expectedMax: map[string]float64{
				"Ch1": 123.5,
			},
		},
		{
			name: "empty channels",
			data: &models.PhaseSyncEMGData{
				Time:    []float64{},
				Headers: []string{"Ch1"},
				Channels: map[string][]float64{
					"Ch1": {},
				},
			},
			expectedMean: map[string]float64{
				"Ch1": 0.0,
			},
			expectedMax: map[string]float64{
				"Ch1": 0.0,
			},
		},
		{
			name: "negative values",
			data: &models.PhaseSyncEMGData{
				Time:    []float64{0.0, 0.001, 0.002},
				Headers: []string{"Ch1"},
				Channels: map[string][]float64{
					"Ch1": {-100.0, 50.0, -200.0}, // mean = -83.33, max = 50.0
				},
			},
			expectedMean: map[string]float64{
				"Ch1": -83.33333333333333,
			},
			expectedMax: map[string]float64{
				"Ch1": 50.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			means, maxes := parsers.CalculateEMGStatistics(tt.data)

			assert.Equal(t, len(tt.expectedMean), len(means))
			assert.Equal(t, len(tt.expectedMax), len(maxes))

			for channel, expectedMean := range tt.expectedMean {
				assert.InDelta(t, expectedMean, means[channel], 0.0001, "Mean for channel %s", channel)
			}

			for channel, expectedMax := range tt.expectedMax {
				assert.Equal(t, expectedMax, maxes[channel], "Max for channel %s", channel)
			}
		})
	}
}

func TestEMGParser_GetSampleInterval(t *testing.T) {
	parser := parsers.NewEMGParser()
	interval := parser.GetSampleInterval()
	assert.Equal(t, 0.001, interval) // 1000Hz = 0.001s
}

func TestValidateEMGData(t *testing.T) {
	tests := []struct {
		name    string
		data    *models.PhaseSyncEMGData
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil data",
			data:    nil,
			wantErr: true,
			errMsg:  "EMG 數據為空",
		},
		{
			name: "empty time series",
			data: &models.PhaseSyncEMGData{
				Time:     []float64{},
				Channels: make(map[string][]float64),
				Headers:  []string{},
			},
			wantErr: true,
			errMsg:  "EMG 時間序列為空",
		},
		{
			name: "no channels",
			data: &models.PhaseSyncEMGData{
				Time:     []float64{0.0, 0.001},
				Channels: make(map[string][]float64),
				Headers:  []string{},
			},
			wantErr: true,
			errMsg:  "EMG 沒有任何通道數據",
		},
		{
			name: "non-increasing time series",
			data: &models.PhaseSyncEMGData{
				Time:    []float64{0.0, 0.001, 0.0005}, // 時間不遞增
				Headers: []string{"Ch1"},
				Channels: map[string][]float64{
					"Ch1": {1.0, 2.0, 3.0},
				},
			},
			wantErr: true,
			errMsg:  "EMG 時間序列在索引 2 處不是遞增的",
		},
		{
			name: "mismatched data length",
			data: &models.PhaseSyncEMGData{
				Time:    []float64{0.0, 0.001, 0.002},
				Headers: []string{"Ch1", "Ch2"},
				Channels: map[string][]float64{
					"Ch1": {1.0, 2.0, 3.0},
					"Ch2": {1.0, 2.0}, // 長度不匹配
				},
			},
			wantErr: true,
			errMsg:  "通道 Ch2 的數據長度",
		},
		{
			name: "valid data",
			data: &models.PhaseSyncEMGData{
				Time:    []float64{0.0, 0.001, 0.002},
				Headers: []string{"Ch1", "Ch2"},
				Channels: map[string][]float64{
					"Ch1": {1.0, 2.0, 3.0},
					"Ch2": {0.1, 0.2, 0.3},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parsers.ValidateEMGData(tt.data)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestEMGParser_parseHeaders(t *testing.T) {
	parser := parsers.NewEMGParser()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "normal headers",
			input:    []string{"Time", "Ch1", "Ch2", "Ch3"},
			expected: []string{"Time", "Ch1", "Ch2", "Ch3"},
		},
		{
			name:     "headers with spaces",
			input:    []string{"  Time  ", "  Ch1  ", "  Ch2  "},
			expected: []string{"Time", "Ch1", "Ch2"},
		},
		{
			name:     "headers with empty strings",
			input:    []string{"Time", "", "Ch1", "  ", "Ch2"},
			expected: []string{"Time", "Ch1", "Ch2"},
		},
		{
			name:     "all empty headers",
			input:    []string{"", "  ", "   "},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 由於 parseHeaders 是私有方法，我們通過創建一個簡單的 CSV 文件來間接測試
			csvContent := ""
			for i, header := range tt.input {
				if i > 0 {
					csvContent += ","
				}
				csvContent += header
			}
			csvContent += "\n0.000,1,2,3"

			tmpFile, err := os.CreateTemp("", "test_headers_*.csv")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			data, err := parser.ParseFile(tmpFile.Name())

			if len(tt.expected) <= 1 { // 如果只有時間列或沒有列，應該出錯
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected[1:], data.Headers) // 排除時間列
			}
		})
	}
}

func TestEMGParser_Integration(t *testing.T) {
	// 集成測試：創建完整的 EMG 文件並測試完整流程
	t.Run("complete EMG file parsing and validation", func(t *testing.T) {
		emgContent := `Time,Ch1,Ch2,Ch3,Ch4
0.000000,145.2,123.8,167.3,189.4
0.001000,146.1,124.5,166.9,190.2
0.002000,144.8,123.1,168.1,188.7
0.003000,147.3,125.2,165.8,191.1
0.004000,145.9,124.0,167.6,189.8`

		// 創建臨時文件
		tmpFile, err := os.CreateTemp("", "integration_test_*.csv")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString(emgContent)
		require.NoError(t, err)
		tmpFile.Close()

		// 解析文件
		parser := parsers.NewEMGParser()
		data, err := parser.ParseFile(tmpFile.Name())
		require.NoError(t, err)

		// 驗證數據完整性
		err = parsers.ValidateEMGData(data)
		assert.NoError(t, err)

		// 檢查數據內容
		assert.Len(t, data.Time, 5)
		assert.Len(t, data.Headers, 4)
		assert.Len(t, data.Channels, 4)

		// 測試時間範圍查詢
		rangeData, err := parser.GetDataInTimeRange(data, 0.001, 0.003)
		require.NoError(t, err)
		assert.Len(t, rangeData.Time, 3)

		// 驗證範圍數據
		err = parsers.ValidateEMGData(rangeData)
		assert.NoError(t, err)

		// 測試統計計算
		means, maxes := parsers.CalculateEMGStatistics(data)
		assert.Len(t, means, 4)
		assert.Len(t, maxes, 4)

		// 檢查統計結果的合理性
		for channel := range data.Channels {
			assert.Greater(t, means[channel], 0.0, "Mean should be positive for channel %s", channel)
			assert.Greater(t, maxes[channel], 0.0, "Max should be positive for channel %s", channel)
			assert.GreaterOrEqual(t, maxes[channel], means[channel], "Max should be >= mean for channel %s", channel)
		}
	})
}
