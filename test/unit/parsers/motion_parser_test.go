package parsers

import (
	"fmt"
	"os"
	"testing"

	"count_mean/internal/models"
	"count_mean/internal/parsers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMotionParser(t *testing.T) {
	parser := parsers.NewMotionParser()
	assert.NotNil(t, parser)
	assert.Equal(t, 0.004, parser.GetSampleInterval()) // 250Hz = 0.004s interval
}

func TestMotionParser_ParseFile(t *testing.T) {
	tests := []struct {
		name       string
		csvContent string
		wantErr    bool
		checkData  func(*testing.T, *models.MotionData)
	}{
		{
			name: "valid Motion file",
			csvContent: `Line 1: Metadata
Line 2: More metadata
Line 3: Additional info
Index,X,Y,Z,RX,RY,RZ
1,10.5,20.3,30.8,1.2,2.1,3.5
2,11.2,21.7,31.2,1.5,2.4,3.8
3,9.8,19.1,29.5,0.9,1.8,3.2
4,12.1,22.9,32.0,1.7,2.6,4.1`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Indices, 4)
				assert.Equal(t, []int{1, 2, 3, 4}, data.Indices)

				assert.Len(t, data.Headers, 6)
				assert.Equal(t, []string{"X", "Y", "Z", "RX", "RY", "RZ"}, data.Headers)

				assert.Len(t, data.Data, 6)
				assert.Equal(t, 10.5, data.Data["X"][0])
				assert.Equal(t, 4.1, data.Data["RZ"][3])
			},
		},
		{
			name: "Motion file with missing data",
			csvContent: `Line 1
Line 2
Line 3
Index,X,Y,Z
1,10.5,20.3,30.8
2,11.2
3,9.8,19.1,29.5,extra
4,12.1,22.9,32.0`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Indices, 3) // 跳過不完整的行
				assert.Equal(t, []int{1, 3, 4}, data.Indices)
				assert.Len(t, data.Data["X"], 3)
				assert.Len(t, data.Data["Y"], 3)
				assert.Len(t, data.Data["Z"], 3)
			},
		},
		{
			name: "Motion file with invalid index values",
			csvContent: `Line 1
Line 2
Line 3
Index,X,Y
invalid_index,10.5,20.3
2,11.2,21.7
three,9.8,19.1
4,12.1,22.9`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Indices, 2) // 只有兩行有效數據
				assert.Equal(t, []int{2, 4}, data.Indices)
			},
		},
		{
			name: "Motion file with invalid data values",
			csvContent: `Line 1
Line 2
Line 3
Index,X,Y
1,10.5,invalid
2,invalid,21.7
3,9.8,19.1`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Indices, 3)
				// 無效值應該被設為 0
				assert.Equal(t, 0.0, data.Data["Y"][0])
				assert.Equal(t, 0.0, data.Data["X"][1])
				assert.Equal(t, 9.8, data.Data["X"][2])
			},
		},
		{
			name: "Motion file with empty lines",
			csvContent: `Line 1
Line 2
Line 3
Index,X,Y

1,10.5,20.3

2,11.2,21.7
`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Indices, 2) // 跳過空行
				assert.Equal(t, []int{1, 2}, data.Indices)
			},
		},
		{
			name: "Motion file with spaces",
			csvContent: `Line 1
Line 2
Line 3
  Index  ,  X  ,  Y  
  1  ,  10.5  ,  20.3  
  2  ,  11.2  ,  21.7  `,
			wantErr: false,
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Indices, 2)
				assert.Equal(t, []string{"X", "Y"}, data.Headers)
				assert.Equal(t, 10.5, data.Data["X"][0])
			},
		},
		{
			name:       "empty Motion file",
			csvContent: "",
			wantErr:    true,
		},
		{
			name: "Motion file without enough lines",
			csvContent: `Line 1
Line 2`,
			wantErr: true,
		},
		{
			name: "Motion file with insufficient headers",
			csvContent: `Line 1
Line 2
Line 3
Index
1`,
			wantErr: true,
		},
		{
			name: "Motion file without valid data",
			csvContent: `Line 1
Line 2
Line 3
Index,X,Y
invalid,invalid,invalid
text,text,text`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 創建臨時測試文件
			tmpFile, err := os.CreateTemp("", "test_motion_*.csv")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			// 測試解析
			parser := parsers.NewMotionParser()
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

func TestMotionParser_ParseFile_FileNotFound(t *testing.T) {
	parser := parsers.NewMotionParser()
	_, err := parser.ParseFile("nonexistent_file.csv")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "無法開啟 Motion 檔案")
}

func TestMotionParser_IndexToTime(t *testing.T) {
	parser := parsers.NewMotionParser()

	tests := []struct {
		index        int
		expectedTime float64
	}{
		{1, 0.000},   // index 1 -> time 0
		{2, 0.004},   // index 2 -> time 0.004
		{3, 0.008},   // index 3 -> time 0.008
		{250, 0.996}, // index 250 -> time 0.996
		{251, 1.000}, // index 251 -> time 1.000
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("index_%d", tt.index), func(t *testing.T) {
			time := parser.IndexToTime(tt.index)
			assert.InDelta(t, tt.expectedTime, time, 0.0001)
		})
	}
}

func TestMotionParser_TimeToIndex(t *testing.T) {
	parser := parsers.NewMotionParser()

	tests := []struct {
		time          float64
		expectedIndex int
	}{
		{0.000, 1},   // time 0 -> index 1
		{0.004, 2},   // time 0.004 -> index 2
		{0.002, 2},   // time 0.002 -> index 2 (rounded)
		{0.006, 3},   // time 0.006 -> index 3 (rounded)
		{0.008, 3},   // time 0.008 -> index 3
		{1.000, 251}, // time 1.000 -> index 251
		{-0.001, 1},  // negative time -> index 1 (minimum)
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("time_%.3f", tt.time), func(t *testing.T) {
			index := parser.TimeToIndex(tt.time)
			assert.Equal(t, tt.expectedIndex, index)
		})
	}
}

func TestMotionParser_GetDataAtIndex(t *testing.T) {
	parser := parsers.NewMotionParser()

	// 創建測試數據
	testData := &models.MotionData{
		Indices: []int{1, 2, 3, 5, 7},
		Headers: []string{"X", "Y"},
		Data: map[string][]float64{
			"X": {10.0, 20.0, 30.0, 50.0, 70.0},
			"Y": {100.0, 200.0, 300.0, 500.0, 700.0},
		},
	}

	tests := []struct {
		name         string
		targetIndex  int
		wantErr      bool
		expectedData map[string]float64
	}{
		{
			name:        "existing index",
			targetIndex: 3,
			wantErr:     false,
			expectedData: map[string]float64{
				"X": 30.0,
				"Y": 300.0,
			},
		},
		{
			name:        "first index",
			targetIndex: 1,
			wantErr:     false,
			expectedData: map[string]float64{
				"X": 10.0,
				"Y": 100.0,
			},
		},
		{
			name:        "last index",
			targetIndex: 7,
			wantErr:     false,
			expectedData: map[string]float64{
				"X": 70.0,
				"Y": 700.0,
			},
		},
		{
			name:        "non-existing index",
			targetIndex: 4,
			wantErr:     true,
		},
		{
			name:        "index too small",
			targetIndex: 0,
			wantErr:     true,
		},
		{
			name:        "index too large",
			targetIndex: 10,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parser.GetDataAtIndex(testData, tt.targetIndex)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedData, result)
		})
	}
}

func TestMotionParser_GetDataInIndexRange(t *testing.T) {
	parser := parsers.NewMotionParser()

	// 創建測試數據
	testData := &models.MotionData{
		Indices: []int{1, 2, 3, 5, 7, 10},
		Headers: []string{"X", "Y"},
		Data: map[string][]float64{
			"X": {10.0, 20.0, 30.0, 50.0, 70.0, 100.0},
			"Y": {100.0, 200.0, 300.0, 500.0, 700.0, 1000.0},
		},
	}

	tests := []struct {
		name         string
		startIndex   int
		endIndex     int
		wantErr      bool
		expectedLen  int
		checkIndices []int
	}{
		{
			name:         "valid range",
			startIndex:   2,
			endIndex:     5,
			wantErr:      false,
			expectedLen:  3, // indices 2, 3, 5
			checkIndices: []int{2, 3, 5},
		},
		{
			name:       "start greater than end",
			startIndex: 5,
			endIndex:   2,
			wantErr:    true,
		},
		{
			name:       "range outside data",
			startIndex: 15,
			endIndex:   20,
			wantErr:    true,
		},
		{
			name:         "exact boundary match",
			startIndex:   1,
			endIndex:     10,
			wantErr:      false,
			expectedLen:  6, // all data
			checkIndices: []int{1, 2, 3, 5, 7, 10},
		},
		{
			name:         "partial range with gaps",
			startIndex:   3,
			endIndex:     8,
			wantErr:      false,
			expectedLen:  3, // indices 3, 5, 7
			checkIndices: []int{3, 5, 7},
		},
		{
			name:         "single index range",
			startIndex:   5,
			endIndex:     5,
			wantErr:      false,
			expectedLen:  1, // only index 5
			checkIndices: []int{5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rangeData, err := parser.GetDataInIndexRange(testData, tt.startIndex, tt.endIndex)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, rangeData)
			assert.Len(t, rangeData.Indices, tt.expectedLen)
			assert.Equal(t, tt.checkIndices, rangeData.Indices)

			// 檢查數據完整性
			for columnName := range rangeData.Data {
				assert.Len(t, rangeData.Data[columnName], len(rangeData.Indices))
			}

			// 檢查標題保持一致
			assert.Equal(t, testData.Headers, rangeData.Headers)
		})
	}
}

func TestValidateMotionData(t *testing.T) {
	tests := []struct {
		name    string
		data    *models.MotionData
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil data",
			data:    nil,
			wantErr: true,
			errMsg:  "Motion 數據為空",
		},
		{
			name: "empty indices",
			data: &models.MotionData{
				Indices: []int{},
				Data:    make(map[string][]float64),
				Headers: []string{},
			},
			wantErr: true,
			errMsg:  "Motion index 序列為空",
		},
		{
			name: "no data columns",
			data: &models.MotionData{
				Indices: []int{1, 2},
				Data:    make(map[string][]float64),
				Headers: []string{},
			},
			wantErr: true,
			errMsg:  "Motion 沒有任何數據列",
		},
		{
			name: "non-increasing indices",
			data: &models.MotionData{
				Indices: []int{1, 3, 2}, // 不遞增
				Headers: []string{"X"},
				Data: map[string][]float64{
					"X": {1.0, 3.0, 2.0},
				},
			},
			wantErr: true,
			errMsg:  "Motion index 在位置 2 處不是遞增的",
		},
		{
			name: "mismatched data length",
			data: &models.MotionData{
				Indices: []int{1, 2, 3},
				Headers: []string{"X", "Y"},
				Data: map[string][]float64{
					"X": {1.0, 2.0, 3.0},
					"Y": {1.0, 2.0}, // 長度不匹配
				},
			},
			wantErr: true,
			errMsg:  "列 Y 的數據長度",
		},
		{
			name: "valid data",
			data: &models.MotionData{
				Indices: []int{1, 2, 5, 10},
				Headers: []string{"X", "Y"},
				Data: map[string][]float64{
					"X": {1.0, 2.0, 5.0, 10.0},
					"Y": {0.1, 0.2, 0.5, 1.0},
				},
			},
			wantErr: false,
		},
		{
			name: "valid data with gaps in indices",
			data: &models.MotionData{
				Indices: []int{1, 3, 7, 15},
				Headers: []string{"X"},
				Data: map[string][]float64{
					"X": {1.0, 3.0, 7.0, 15.0},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parsers.ValidateMotionData(tt.data)

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

func TestMotionParser_GetSampleInterval(t *testing.T) {
	parser := parsers.NewMotionParser()
	interval := parser.GetSampleInterval()
	assert.Equal(t, 0.004, interval) // 250Hz = 0.004s
}

func TestMotionParser_Integration(t *testing.T) {
	// 集成測試：創建完整的 Motion 文件並測試完整流程
	t.Run("complete Motion file parsing and validation", func(t *testing.T) {
		motionContent := `Metadata Line 1
Metadata Line 2
Metadata Line 3
Index,X,Y,Z,RX,RY,RZ
1,12.5,23.8,45.3,1.2,2.1,3.5
2,13.1,24.5,46.9,1.5,2.4,3.8
3,11.8,22.1,44.1,0.9,1.8,3.2
5,14.3,25.2,47.8,1.7,2.6,4.1
7,12.9,23.0,45.6,1.3,2.2,3.6`

		// 創建臨時文件
		tmpFile, err := os.CreateTemp("", "integration_test_*.csv")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.WriteString(motionContent)
		require.NoError(t, err)
		tmpFile.Close()

		// 解析文件
		parser := parsers.NewMotionParser()
		data, err := parser.ParseFile(tmpFile.Name())
		require.NoError(t, err)

		// 驗證數據完整性
		err = parsers.ValidateMotionData(data)
		assert.NoError(t, err)

		// 檢查數據內容
		assert.Len(t, data.Indices, 5)
		assert.Equal(t, []int{1, 2, 3, 5, 7}, data.Indices)
		assert.Len(t, data.Headers, 6)
		assert.Len(t, data.Data, 6)

		// 測試 index 範圍查詢
		rangeData, err := parser.GetDataInIndexRange(data, 2, 5)
		require.NoError(t, err)
		assert.Len(t, rangeData.Indices, 3) // indices 2, 3, 5

		// 驗證範圍數據
		err = parsers.ValidateMotionData(rangeData)
		assert.NoError(t, err)

		// 測試特定 index 數據獲取
		dataAtIndex3, err := parser.GetDataAtIndex(data, 3)
		require.NoError(t, err)
		assert.Equal(t, 11.8, dataAtIndex3["X"])
		assert.Equal(t, 3.2, dataAtIndex3["RZ"])

		// 測試時間轉換
		time := parser.IndexToTime(3)
		assert.InDelta(t, 0.008, time, 0.0001) // index 3 -> time 0.008

		index := parser.TimeToIndex(0.008)
		assert.Equal(t, 3, index) // time 0.008 -> index 3
	})
}
