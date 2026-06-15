package parsers

import (
	"errors"
	"math"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/models"
)

func TestNewEMGParser(t *testing.T) {
	parser := NewEMGParser()
	assert.NotNil(t, parser)
}

func TestEMGParser_Parse(t *testing.T) {
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
			tmpFile, err := os.CreateTemp(t.TempDir(), "test_emg_*.csv")
			require.NoError(t, err)

			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			// 測試解析 via open + Parse
			f, err := os.Open(tmpFile.Name())
			require.NoError(t, err)
			defer f.Close()

			parser := NewEMGParser()
			data, _, err := parser.Parse(f, tmpFile.Name())

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

// TestEMGParser_Parse_FromReader 釘住 reader-based 入口：feed io.Reader 給
// Parse 必須正確解析，包含 frequency 推算。
func TestEMGParser_Parse_FromReader(t *testing.T) {
	const csvContent = `Time,Ch1,Ch2,Ch3
0.000,100.5,200.3,150.8
0.001,101.2,199.7,151.2
0.002,99.8,201.1,149.5
0.003,102.1,198.9,152.0`

	parser := NewEMGParser()
	data, frequency, err := parser.Parse(strings.NewReader(csvContent), "reader.csv")
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Len(t, data.Time, 4)
	assert.Equal(t, 0.000, data.Time[0])
	assert.Equal(t, 0.003, data.Time[3])
	assert.Equal(t, []string{"Ch1", "Ch2", "Ch3"}, data.Headers)
	assert.Equal(t, 100.5, data.Channels["Ch1"][0])
	assert.Equal(t, 152.0, data.Channels["Ch3"][3])
	// 0.001s 間隔 → 1000Hz
	assert.InDelta(t, 1000.0, frequency, 1e-9)
}

// TestEMGParser_Parse_FromReaderWithBOM 確認 reader 入口仍會剝 BOM —
// BOM-prefixed CSV 的第一個 header (Time) 不應殘留 U+FEFF 而導致 header 解析錯亂。
func TestEMGParser_Parse_FromReaderWithBOM(t *testing.T) {
	const bom = "\xEF\xBB\xBF"
	content := bom + "Time,Ch1,Ch2\n0.000,1.0,2.0\n0.001,3.0,4.0\n"

	parser := NewEMGParser()
	data, _, err := parser.Parse(strings.NewReader(content), "bom.csv")
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, []string{"Ch1", "Ch2"}, data.Headers,
		"BOM must be stripped so first header is not polluted")
	assert.Len(t, data.Time, 2)
	assert.Equal(t, 1.0, data.Channels["Ch1"][0])
}

// TestEMGParser_Parse_ReaderError 釘住 reader-boundary 失敗面：當底層 io.Reader 在
// 串流途中報錯（iotest.ErrReader），Parse 必須把錯誤往上傳（含 name context），
// 不得 panic、不得回傳「err==nil 但 data 非 nil」的偽結果。
func TestEMGParser_Parse_ReaderError(t *testing.T) {
	data, freq, err := NewEMGParser().Parse(iotest.ErrReader(errors.New("boom")), "err.csv")

	require.Error(t, err)
	require.ErrorContains(t, err, "boom", "底層 reader 錯誤必須往上傳遞")
	assert.Contains(t, err.Error(), "err.csv", "錯誤需帶上 name context")
	assert.Nil(t, data, "reader 出錯時不得回傳偽造的非 nil data")
	assert.Zero(t, freq)
}

func TestEMGParser_GetDataInTimeRange(t *testing.T) {
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
			result, err := GetEMGDataInTimeRange(testData, tt.startTime, tt.endTime)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, result)
			assert.NotNil(t, result.Data)
			assert.Len(t, result.Data.Time, tt.checkLen)
			assert.Len(t, result.Data.Channels["Ch1"], tt.checkLen)
			assert.Len(t, result.Data.Channels["Ch2"], tt.checkLen)

			// 檢查實際時間範圍與數據一致
			if tt.checkLen > 0 {
				assert.Equal(t, result.Data.Time[0], result.ActualStartTime)
				assert.Equal(t, result.Data.Time[len(result.Data.Time)-1], result.ActualEndTime)
				assert.GreaterOrEqual(t, result.ActualStartTime, tt.startTime)
				assert.LessOrEqual(t, result.ActualEndTime, tt.endTime)
			}

			// 檢查數據完整性
			for channelName := range result.Data.Channels {
				assert.Len(t, result.Data.Channels[channelName], len(result.Data.Time))
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
			means, maxes := CalculateEMGStatistics(tt.data)

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

func TestEMGParser_DynamicFrequencyDetection(t *testing.T) {
	tests := []struct {
		name              string
		csvContent        string
		expectedFrequency float64
	}{
		{
			name:              "500Hz data",
			csvContent:        "Time,Ch1\n0.000,100.0\n0.002,101.0\n0.004,102.0\n",
			expectedFrequency: 500.0,
		},
		{
			name:              "1000Hz data",
			csvContent:        "Time,Ch1\n0.000,100.0\n0.001,101.0\n0.002,102.0\n",
			expectedFrequency: 1000.0,
		},
		{
			name:              "2000Hz data",
			csvContent:        "Time,Ch1\n0.000,100.0\n0.0005,101.0\n0.001,102.0\n",
			expectedFrequency: 2000.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewEMGParser()

			tmpFile, err := os.CreateTemp(t.TempDir(), "test_emg_freq_*.csv")
			require.NoError(t, err)
			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			f, err := os.Open(tmpFile.Name())
			require.NoError(t, err)
			defer f.Close()

			_, frequency, err := parser.Parse(f, tmpFile.Name())
			require.NoError(t, err)

			assert.InDelta(t, tt.expectedFrequency, frequency, 1e-9)
		})
	}
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
			err := ValidateEMGData(tt.data)

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
	parser := NewEMGParser()

	// header / dataRow 欄數一律對齊(真實 CSV 每欄都有對應 cell,空欄填空 cell);
	// expectedHeaders 為 public data.Headers(排除時間欄、不含 "" 佔位欄);
	// expectedValues 鎖定各通道首列取值,證明 record[j] 對齊到正確的具名通道。
	tests := []struct {
		name            string
		headerRow       string
		dataRow         string
		expectErr       bool
		expectedHeaders []string
		expectedValues  map[string]float64
	}{
		{
			name:            "normal headers",
			headerRow:       "Time,Ch1,Ch2,Ch3",
			dataRow:         "0.000,1,2,3",
			expectedHeaders: []string{"Ch1", "Ch2", "Ch3"},
			expectedValues:  map[string]float64{"Ch1": 1, "Ch2": 2, "Ch3": 3},
		},
		{
			name:            "headers with spaces",
			headerRow:       "  Time  ,  Ch1  ,  Ch2  ",
			dataRow:         "0.000,1,2",
			expectedHeaders: []string{"Ch1", "Ch2"},
			expectedValues:  map[string]float64{"Ch1": 1, "Ch2": 2},
		},
		{
			// 中段空欄:CSV 第 1、3 欄為空(佔位 spacer),Ch1 在第 2 欄、Ch2 在第 4 欄。
			// public Headers 不含 ""；資料值必須對齊到 record[2]/record[4] 而非位移。
			name:            "headers with empty strings",
			headerRow:       "Time,,Ch1,  ,Ch2",
			dataRow:         "0.000,,2,,4",
			expectedHeaders: []string{"Ch1", "Ch2"},
			expectedValues:  map[string]float64{"Ch1": 2, "Ch2": 4},
		},
		{
			name:      "all empty headers",
			headerRow: ",  ,   ",
			dataRow:   "0.000,1,2",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 由於 parseHeaders 是私有方法，我們通過創建一個簡單的 CSV 文件來間接測試
			csvContent := tt.headerRow + "\n" + tt.dataRow

			tmpFile, err := os.CreateTemp(t.TempDir(), "test_headers_*.csv")
			require.NoError(t, err)

			_, err = tmpFile.WriteString(csvContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			f, err := os.Open(tmpFile.Name())
			require.NoError(t, err)
			defer f.Close()

			data, _, err := parser.Parse(f, tmpFile.Name())

			if tt.expectErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			// public Headers 不含 "" 佔位欄
			assert.Equal(t, tt.expectedHeaders, data.Headers)

			// 資料值對齊到正確通道(中段空欄不可造成位移)
			for name, want := range tt.expectedValues {
				ch := data.Channels[name]
				require.Lenf(t, ch, 1, "通道 %q 應有 1 筆資料", name)
				assert.Equalf(t, want, ch[0], "通道 %q 首列取值錯位", name)
			}

			// 佔位空欄不應產生通道
			_, hasEmpty := data.Channels[""]
			assert.False(t, hasEmpty, `public Channels 不應含 "" 佔位欄`)
		})
	}
}

// TestParseEMGDataRow_TimeCellRouting 驗證路由正確性：
//   - 時間列為 "NaN" → 整行跳過（parseEMGDataRow 回 false）
//   - 時間列合法、通道列為 "NaN" → 整行接受（回 true），且對應通道值為 NaN
//
// 「通道 NaN → 接受」是有意設計：EMG sensor missing-data 輸出字面 "NaN"，
// 下游 muscle_ratio / phase_sync 把該 cell 輸出為空白。
func TestParseEMGDataRow_TimeCellRouting(t *testing.T) {
	t.Parallel()

	headers := []string{"Time", "Ch1", "Ch2"}

	t.Run("時間列_NaN_整行跳過", func(t *testing.T) {
		t.Parallel()

		emgData := initEMGData(headers, 0)
		record := []string{"NaN", "1.0", "2.0"}
		accepted := parseEMGDataRow(record, headers, emgData)

		if accepted {
			t.Error("parseEMGDataRow: 時間列為 NaN 時應回 false（整行跳過），但回了 true")
		}
		if len(emgData.Time) != 0 {
			t.Errorf("時間列為 NaN 時不應 append 任何時間點，got len=%d", len(emgData.Time))
		}
	})

	t.Run("通道列_NaN_整行接受且通道值為NaN", func(t *testing.T) {
		t.Parallel()

		emgData := initEMGData(headers, 0)
		record := []string{"0.001", "NaN", "2.0"}
		accepted := parseEMGDataRow(record, headers, emgData)

		if !accepted {
			t.Error("parseEMGDataRow: 時間列合法、通道列含 NaN 時應回 true，但回了 false")
		}
		if len(emgData.Time) != 1 || emgData.Time[0] != 0.001 {
			t.Errorf("時間點應為 0.001，got %v", emgData.Time)
		}
		ch1 := emgData.Channels["Ch1"]
		if len(ch1) != 1 || !math.IsNaN(ch1[0]) {
			t.Errorf("Ch1 通道值應為 NaN（sensor missing-data），got %v", ch1)
		}
		ch2 := emgData.Channels["Ch2"]
		if len(ch2) != 1 || ch2[0] != 2.0 {
			t.Errorf("Ch2 通道值應為 2.0，got %v", ch2)
		}
	})
}

// TestEMGParser_MidColumnEmptyHeaderAlignment 鎖定「header 中段有空欄 + 對應空資料欄」
// 的對齊不變式(對齊 motion_parser.go「不可 compact」範式):
//   - 非空通道拿到 record[j] 正確位置的值(不因中段空欄而位移)
//   - 空欄被跳過、不產生 "" 通道
//   - Time 欄不受影響、資料行不被誤跳(非空 Time 過 validateEMGDataIntegrity)
//
// 修正前 parseHeaders 會 compact 抽掉空欄,使具名通道位移配到 spacer 欄的值 → 靜默錯位。
func TestEMGParser_MidColumnEmptyHeaderAlignment(t *testing.T) {
	parser := NewEMGParser()

	// 第 1、4 欄為空 spacer;Ch1=第2欄、Ch2=第3欄、Ch3=第5欄。
	// 資料每列各欄對齊填值,空欄填空 cell。
	emgContent := "Time,,Ch1,Ch2,,Ch3\n" +
		"0.000,,11,12,,13\n" +
		"0.001,,21,22,,23"

	tmpFile, err := os.CreateTemp(t.TempDir(), "midempty_*.csv")
	require.NoError(t, err)

	_, err = tmpFile.WriteString(emgContent)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	f, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer f.Close()

	data, _, err := parser.Parse(f, tmpFile.Name())
	require.NoError(t, err)

	// public Headers 不含 "" 佔位欄,且保持 CSV 出現順序
	assert.Equal(t, []string{"Ch1", "Ch2", "Ch3"}, data.Headers)

	// Time 不受中段空欄影響
	assert.Equal(t, []float64{0.000, 0.001}, data.Time)

	// 各通道對齊到正確的 record[j] 位置(非位移)
	assert.Equal(t, []float64{11, 21}, data.Channels["Ch1"])
	assert.Equal(t, []float64{12, 22}, data.Channels["Ch2"])
	assert.Equal(t, []float64{13, 23}, data.Channels["Ch3"])

	// 佔位空欄不產生通道
	_, hasEmpty := data.Channels[""]
	assert.False(t, hasEmpty, `public Channels 不應含 "" 佔位欄`)

	// 整檔通過完整性校驗(非空 Time、各通道等長)
	assert.NoError(t, validateEMGDataIntegrity(data))
}

// TestEMGParser_DuplicateChannelName 驗證重複通道名稱回傳明確訊息,
// 取代修正前語意不清的「長度不一致」(P2-6;僅 UX、無行為變更)。
func TestEMGParser_DuplicateChannelName(t *testing.T) {
	parser := NewEMGParser()

	// Ch1 出現兩次 → 折疊成同一 map key、長度變兩倍。
	emgContent := "Time,Ch1,Ch2,Ch1\n" +
		"0.000,1,2,3\n" +
		"0.001,4,5,6"

	tmpFile, err := os.CreateTemp(t.TempDir(), "dupchan_*.csv")
	require.NoError(t, err)

	_, err = tmpFile.WriteString(emgContent)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	f, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer f.Close()

	_, _, err = parser.Parse(f, tmpFile.Name())
	require.Error(t, err)

	// 明確指出「重複的通道名稱」與重複的名字,而非「長度不一致」
	assert.Contains(t, err.Error(), "重複的通道名稱")
	assert.Contains(t, err.Error(), "Ch1")
	assert.NotContains(t, err.Error(), "長度不一致")
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
		tmpFile, err := os.CreateTemp(t.TempDir(), "integration_test_*.csv")
		require.NoError(t, err)

		_, err = tmpFile.WriteString(emgContent)
		require.NoError(t, err)
		require.NoError(t, tmpFile.Close())

		// 解析文件
		f, err := os.Open(tmpFile.Name())
		require.NoError(t, err)
		defer f.Close()

		parser := NewEMGParser()
		data, _, err := parser.Parse(f, tmpFile.Name())
		require.NoError(t, err)

		// 驗證數據完整性
		err = ValidateEMGData(data)
		assert.NoError(t, err)

		// 檢查數據內容
		assert.Len(t, data.Time, 5)
		assert.Len(t, data.Headers, 4)
		assert.Len(t, data.Channels, 4)

		// 測試時間範圍查詢
		rangeResult, err := GetEMGDataInTimeRange(data, 0.001, 0.003)
		require.NoError(t, err)
		assert.Len(t, rangeResult.Data.Time, 3)

		// 驗證實際時間範圍與數據一致
		assert.Equal(t, rangeResult.Data.Time[0], rangeResult.ActualStartTime)
		assert.Equal(t, rangeResult.Data.Time[len(rangeResult.Data.Time)-1], rangeResult.ActualEndTime)

		// 驗證範圍數據
		err = ValidateEMGData(rangeResult.Data)
		assert.NoError(t, err)

		// 測試統計計算
		means, maxes := CalculateEMGStatistics(data)
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

// TestValidateEMGData_NonFiniteChannelValues 驗證 validateEMGChannelValues 的
// fail-fast 行為:NaN 或 Inf 的通道取樣必須回傳對應 sentinel,不可 silently 寫進輸出。
//
// 覆蓋範圍:
//   - NaN 通道 → errors.Is(err, calcerrors.ErrNaNInChannel)
//   - +Inf 通道 → errors.Is(err, calcerrors.ErrInfInChannel)
//   - -Inf 通道 → errors.Is(err, calcerrors.ErrInfInChannel)
//   - 全有限值   → nil(確認正常路徑不受影響)
func TestValidateEMGData_NonFiniteChannelValues(t *testing.T) {
	// validBase 建構一筆合法的 PhaseSyncEMGData,caller 可注入問題值。
	validBase := func(channelValues map[string][]float64) *models.PhaseSyncEMGData {
		return &models.PhaseSyncEMGData{
			Time:     []float64{0.0, 0.001, 0.002},
			Headers:  []string{"Ch1"},
			Channels: channelValues,
		}
	}

	t.Run("NaN 通道 → ErrNaNInChannel", func(t *testing.T) {
		data := validBase(map[string][]float64{
			"Ch1": {1.0, math.NaN(), 3.0},
		})
		err := ValidateEMGData(data)
		require.Error(t, err)
		require.True(t, errors.Is(err, calcerrors.ErrNaNInChannel),
			"期望 ErrNaNInChannel sentinel,實際: %v", err)
	})

	t.Run("+Inf 通道 → ErrInfInChannel", func(t *testing.T) {
		data := validBase(map[string][]float64{
			"Ch1": {1.0, math.Inf(1), 3.0},
		})
		err := ValidateEMGData(data)
		require.Error(t, err)
		require.True(t, errors.Is(err, calcerrors.ErrInfInChannel),
			"期望 ErrInfInChannel sentinel,實際: %v", err)
	})

	t.Run("-Inf 通道 → ErrInfInChannel", func(t *testing.T) {
		data := validBase(map[string][]float64{
			"Ch1": {1.0, math.Inf(-1), 3.0},
		})
		err := ValidateEMGData(data)
		require.Error(t, err)
		require.True(t, errors.Is(err, calcerrors.ErrInfInChannel),
			"期望 ErrInfInChannel sentinel,實際: %v", err)
	})

	t.Run("全有限值 → nil", func(t *testing.T) {
		data := validBase(map[string][]float64{
			"Ch1": {1.0, 2.0, 3.0},
		})
		require.NoError(t, ValidateEMGData(data))
	})
}
