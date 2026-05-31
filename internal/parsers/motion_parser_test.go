package parsers

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/csvutil"
	"count_mean/internal/logging"
	"count_mean/internal/models"
)

func TestNewMotionParser(t *testing.T) {
	parser := NewMotionParser()
	assert.NotNil(t, parser)
	assert.Equal(t, 0.004, parser.GetSampleInterval()) // 250Hz = 0.004s interval
}

func TestMotionParser_Parse(t *testing.T) {
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
			tmpFile, err := os.CreateTemp(t.TempDir(), "test_motion_*.csv")
			require.NoError(t, err)

			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			// 測試解析 via open + Parse
			f, err := os.Open(tmpFile.Name())
			require.NoError(t, err)
			defer f.Close()

			parser := NewMotionParser()
			data, err := parser.Parse(f, tmpFile.Name())

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

// TestMotionParser_Parse_FromReader 釘住 reader-based 入口：feed io.Reader 給
// Parse 必須正確解析 Motion CSV 資料。
func TestMotionParser_Parse_FromReader(t *testing.T) {
	const csvContent = `Line 1: Metadata
Line 2: More metadata
Line 3: Additional info
Index,X,Y,Z,RX,RY,RZ
1,10.5,20.3,30.8,1.2,2.1,3.5
2,11.2,21.7,31.2,1.5,2.4,3.8
3,9.8,19.1,29.5,0.9,1.8,3.2
4,12.1,22.9,32.0,1.7,2.6,4.1`

	parser := NewMotionParser()
	data, err := parser.Parse(bytes.NewReader([]byte(csvContent)), "reader.csv")
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, []int{1, 2, 3, 4}, data.Indices)
	assert.Equal(t, []string{"X", "Y", "Z", "RX", "RY", "RZ"}, data.Headers)
	assert.Equal(t, 10.5, data.Data["X"][0])
	assert.Equal(t, 4.1, data.Data["RZ"][3])
}

// TestMotionParser_Parse_FromReaderWithBOM 確認 reader 入口仍剝 BOM —
// BOM-prefixed Motion CSV 解析結果（Headers / Data keys）不得殘留 BOM bytes。
func TestMotionParser_Parse_FromReaderWithBOM(t *testing.T) {
	csvBody := `Trunk Angle,X Cat,Y Cat,Z Cat
Subcat A,Subcat B,Subcat C,Subcat D
Additional metadata
Index,X,Y,Z
1,10.5,20.3,30.8
2,11.2,21.7,31.2
3,9.8,19.1,29.5
`
	payload := append([]byte{}, csvutil.BOMBytes()...)
	payload = append(payload, []byte(csvBody)...)

	data, err := NewMotionParser().Parse(bytes.NewReader(payload), "bom.csv")
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, []int{1, 2, 3}, data.Indices)
	for _, header := range data.Headers {
		assert.Falsef(t, bytes.Contains([]byte(header), csvutil.BOMBytes()),
			"header %q must not contain UTF-8 BOM", header)
	}
}

// TestMotionParser_Parse_ReaderError 釘住 reader-boundary 失敗面：當底層 io.Reader
// 在串流途中報錯（iotest.ErrReader），Parse 必須把錯誤往上傳（含 name context），
// 不得 panic、不得回傳「err==nil 但 data 非 nil」的偽結果。
func TestMotionParser_Parse_ReaderError(t *testing.T) {
	data, err := NewMotionParser().Parse(iotest.ErrReader(errors.New("boom")), "err.csv")

	require.Error(t, err)
	require.ErrorContains(t, err, "boom", "底層 reader 錯誤必須往上傳遞")
	assert.Contains(t, err.Error(), "err.csv", "錯誤需帶上 name context")
	assert.Nil(t, data, "reader 出錯時不得回傳偽造的非 nil data")
}

func TestMotionParser_IndexToTime(t *testing.T) {
	parser := NewMotionParser()

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
	parser := NewMotionParser()

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
			result, err := GetMotionDataAtIndex(testData, tt.targetIndex)

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
			rangeData, err := GetMotionDataInIndexRange(testData, tt.startIndex, tt.endIndex)

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
			errMsg:  "Motion 沒有任何列數據",
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
			errMsg:  "Motion index 序列在位置 2 處不是遞增的",
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
			err := ValidateMotionData(tt.data)

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
	parser := NewMotionParser()
	interval := parser.GetSampleInterval()
	assert.Equal(t, 0.004, interval) // 250Hz = 0.004s
}

func TestMotionParser_DuplicateSeriesHeaders(t *testing.T) {
	// 測試重複 "Series" 欄位名稱的處理
	// 這是真實 Motion 檔案的格式
	tests := []struct {
		name            string
		csvContent      string
		wantErr         bool
		expectedHeaders []string
		checkData       func(*testing.T, *models.MotionData)
	}{
		{
			name: "duplicate Series with category row",
			csvContent: `[Data],,,
,Trunk Angle,Hip Angle,Knee Angle
,Trunk Flex (deg),Hip Flex (deg),Knee Flex (deg)
Index,Series,Series,Series
1,-10.040299,-4.25116,3.056281
2,-10.036259,-4.27678,3.095674
3,-9.987208,-4.447137,3.01922`,
			wantErr:         false,
			expectedHeaders: []string{"Trunk Angle", "Hip Angle", "Knee Angle"},
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Indices, 3)
				assert.Equal(t, []int{1, 2, 3}, data.Indices)

				// 檢查欄位名稱是否正確使用類別行
				assert.Len(t, data.Headers, 3)
				assert.Contains(t, data.Headers, "Trunk Angle")
				assert.Contains(t, data.Headers, "Hip Angle")
				assert.Contains(t, data.Headers, "Knee Angle")

				// 檢查數據
				assert.InDelta(t, -10.040299, data.Data["Trunk Angle"][0], 0.0001)
				assert.InDelta(t, -4.25116, data.Data["Hip Angle"][0], 0.0001)
				assert.InDelta(t, 3.056281, data.Data["Knee Angle"][0], 0.0001)
			},
		},
		{
			name: "duplicate Series with empty category row - use subcat row",
			csvContent: `[Data],,,
,,,
,Trunk Flexion,Hip Flexion,Knee Flexion
Index,Series,Series,Series
1,10.5,20.3,30.8
2,11.2,21.7,31.2`,
			wantErr:         false,
			expectedHeaders: []string{"Trunk Flexion", "Hip Flexion", "Knee Flexion"},
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Headers, 3)
				assert.Contains(t, data.Headers, "Trunk Flexion")
				assert.Contains(t, data.Headers, "Hip Flexion")
				assert.Contains(t, data.Headers, "Knee Flexion")
			},
		},
		{
			name: "duplicate Series with all empty rows - use indexed names",
			csvContent: `[Data],,,
,,,
,,,
Index,Series,Series,Series
1,10.5,20.3,30.8
2,11.2,21.7,31.2`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Headers, 3)
				// 第一個 Series 保持不變，之後的加上索引
				assert.Contains(t, data.Headers, "Series")
				assert.Contains(t, data.Headers, "Series_2")
				assert.Contains(t, data.Headers, "Series_3")
			},
		},
		{
			name: "mixed duplicate headers",
			csvContent: `[Data],,,,
,Angle A,Angle B,Angle A,Angle C
,Detail 1,Detail 2,Detail 3,Detail 4
Index,Series,Series,Series,Series
1,10.5,20.3,30.8,40.1
2,11.2,21.7,31.2,41.5`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Headers, 4)
				// 檢查重複的 "Angle A" 是否被處理
				assert.Contains(t, data.Headers, "Angle A")
				assert.Contains(t, data.Headers, "Angle B")
				assert.Contains(t, data.Headers, "Angle A_2") // 第二個 Angle A
				assert.Contains(t, data.Headers, "Angle C")
			},
		},
		{
			name: "subcat row with parentheses - extract short name",
			csvContent: `[Data],,,
,,,
,Trunk Flexion / Extension (deg),Hip Flexion / Extension (deg),Knee Flexion / Extension (deg)
Index,Series,Series,Series
1,10.5,20.3,30.8`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.MotionData) {
				assert.Len(t, data.Headers, 3)
				// 括號前的部分應該被提取
				assert.Contains(t, data.Headers, "Trunk Flexion / Extension")
				assert.Contains(t, data.Headers, "Hip Flexion / Extension")
				assert.Contains(t, data.Headers, "Knee Flexion / Extension")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 創建臨時測試文件
			tmpFile, err := os.CreateTemp(t.TempDir(), "test_motion_duplicate_*.csv")
			require.NoError(t, err)

			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			f, err := os.Open(tmpFile.Name())
			require.NoError(t, err)
			defer f.Close()

			// 測試解析
			parser := NewMotionParser()
			data, err := parser.Parse(f, tmpFile.Name())

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, data)

			if tt.expectedHeaders != nil {
				assert.Equal(t, tt.expectedHeaders, data.Headers)
			}

			if tt.checkData != nil {
				tt.checkData(t, data)
			}
		})
	}
}

// TestMotionParser_SparseSeriesRow 重現 NSF11_BTS_5_ok_OK_20Hz.csv 的 bug:
// 真實 Motion 檔的 header(Series)列是稀疏的 —— `Index,Series,,,Series`,只有
// 第 1、4 數據欄標了 "Series",中間兩欄留空。舊版 buildUniqueHeaders 以 header
// 列為「有幾欄」的真相來源,遇空 cell 即 continue,導致:
//  1. R.Hip Angle / R.Knee Angle 兩欄被整欄丟棄(少畫兩條線);
//  2. 更糟:headers 被 compact 後位置對齊斷裂,parseDataRecord 仍按 record[j]
//     位置餵值,使僅存的 "Ankle Angle" 線吃到 record[2](R.Hip 的數值),
//     真正的 Ankle 資料(record[4])完全沒被讀 —— 資料錯位。
//
// 正確契約:欄名應取自 category 列(四欄都有名),四條 series 與資料欄位位置
// 對齊。本 test 用 NSF11 前三筆真實資料,並特意斷言 Ankle Angle 的值以抓錯位。
func TestMotionParser_SparseSeriesRow(t *testing.T) {
	csvContent := `[Data],,,,
,Trunk Angle,R.Hip Angle,R.Knee Angle,Ankle Angle
,Trunk Angle,R.Hip Angle,R.Knee Angle,Ankle Angle
Index,Series,,,Series
1,-16.036152,3.376008,-5.568622,-6.885391
2,-16.119648,3.360193,-5.545166,-6.874992
3,-16.113022,3.498265,-5.623739,-6.930542`

	tmpFile, err := os.CreateTemp(t.TempDir(), "test_motion_sparse_*.csv")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(csvContent)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	f, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer f.Close()

	data, err := NewMotionParser().Parse(f, tmpFile.Name())
	require.NoError(t, err)
	require.NotNil(t, data)

	// 四個數據欄都該保留,順序對齊 category 列。
	assert.Equal(t, []string{"Trunk Angle", "R.Hip Angle", "R.Knee Angle", "Ankle Angle"},
		data.Headers, "稀疏 Series 列不該丟棄 category 列有名的欄位")

	assert.Equal(t, []int{1, 2, 3}, data.Indices)

	// 每欄的值必須對齊正確的資料欄 —— 特別是 Ankle Angle 不能吃到 R.Hip 的值。
	assert.InDelta(t, -16.036152, data.Data["Trunk Angle"][0], 0.0001)
	assert.InDelta(t, 3.376008, data.Data["R.Hip Angle"][0], 0.0001)
	assert.InDelta(t, -5.568622, data.Data["R.Knee Angle"][0], 0.0001)
	assert.InDelta(t, -6.885391, data.Data["Ankle Angle"][0], 0.0001,
		"Ankle Angle 必須讀 record[4],而非錯位讀到 R.Hip 的 record[2]")
}

// TestMotionParser_BlankSpacerColumnPreservesAlignment 鎖定 codex review P2:
// 當 Motion CSV 中間有一個「三列(header/category/subcat)皆空」的 spacer 欄,且其後
// 還有具名欄時,不可把 spacer 欄 compact 抽掉 —— parseDataRecord 按 record[j] 位置
// 餵值,抽掉中間欄會讓後面的具名欄整個位移、配到 spacer 欄的值(靜默錯位)。
//
// 此 fixture 第 2 欄(0-based,即 record[2])是 spacer:category/subcat/header 三列在該
// 欄皆空,但資料列在該位置有值(99.x)。正確行為:spacer 不成為 series,且 Knee/Ankle
// 必須讀到 record[3]/record[4] 而非位移後的 record[2]/record[3]。
func TestMotionParser_BlankSpacerColumnPreservesAlignment(t *testing.T) {
	csvContent := `[Data],,,,
,Trunk Angle,,Knee Angle,Ankle Angle
,,,,
Index,Series,,Series,Series
1,-16.0,99.9,-5.5,-6.8
2,-16.1,99.8,-5.4,-6.7`

	tmpFile, err := os.CreateTemp(t.TempDir(), "test_motion_spacer_*.csv")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(csvContent)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	f2, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer f2.Close()

	data, err := NewMotionParser().Parse(f2, tmpFile.Name())
	require.NoError(t, err)
	require.NotNil(t, data)

	// spacer 欄不該成為 series;三個具名欄依 CSV 位置保留。
	assert.Equal(t, []string{"Trunk Angle", "Knee Angle", "Ankle Angle"}, data.Headers,
		"中間 spacer 欄不該被當成 series,具名欄順序須維持")
	assert.Equal(t, []int{1, 2}, data.Indices)

	// 關鍵:Knee/Ankle 讀的是 record[3]/record[4],不是位移後的 spacer record[2]。
	assert.InDelta(t, -16.0, data.Data["Trunk Angle"][0], 0.0001)
	assert.InDelta(t, -5.5, data.Data["Knee Angle"][0], 0.0001,
		"Knee Angle 必須讀 record[3],而非錯位讀到 spacer 的 record[2]=99.9")
	assert.InDelta(t, -6.8, data.Data["Ankle Angle"][0], 0.0001,
		"Ankle Angle 必須讀 record[4],而非位移讀到 record[3]")
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
		tmpFile, err := os.CreateTemp(t.TempDir(), "integration_test_*.csv")
		require.NoError(t, err)

		_, err = tmpFile.WriteString(motionContent)
		require.NoError(t, err)
		require.NoError(t, tmpFile.Close())

		f, err := os.Open(tmpFile.Name())
		require.NoError(t, err)
		defer f.Close()

		// 解析文件
		parser := NewMotionParser()
		data, err := parser.Parse(f, tmpFile.Name())
		require.NoError(t, err)

		// 驗證數據完整性
		err = ValidateMotionData(data)
		assert.NoError(t, err)

		// 檢查數據內容
		assert.Len(t, data.Indices, 5)
		assert.Equal(t, []int{1, 2, 3, 5, 7}, data.Indices)
		assert.Len(t, data.Headers, 6)
		assert.Len(t, data.Data, 6)

		// 測試 index 範圍查詢
		rangeData, err := GetMotionDataInIndexRange(data, 2, 5)
		require.NoError(t, err)
		assert.Len(t, rangeData.Indices, 3) // indices 2, 3, 5

		// 驗證範圍數據
		err = ValidateMotionData(rangeData)
		assert.NoError(t, err)

		// 測試特定 index 數據獲取
		dataAtIndex3, err := GetMotionDataAtIndex(data, 3)
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

// TestMotionParser_StripsBOMFromFirstField 鎖定 Wave 4 PR-E 補上的
// readCSVRecords BOM 處理。EMG 之前在 csv_reader.go 已用 csvutil.TrimBOM 剝
// BOM，Motion 路徑曾完全沒處理 — Excel 匯出的 Motion CSV 開頭 0xEF 0xBB 0xBF
// 會汙染 records[0][0]。
//
// 對稱修復後的契約：
//  1. BOM-prefixed Motion CSV 能順利 Parse，不報錯。
//  2. 解析結果（Headers、Data keys）不含 BOM bytes。
//
// 若未來 Motion structure 改成「第一列當 header 用」，此 test 會立刻發現
// BOM 汙染回流。
func TestMotionParser_StripsBOMFromFirstField(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "motion_bom.csv")

	csvBody := `Trunk Angle,X Cat,Y Cat,Z Cat
Subcat A,Subcat B,Subcat C,Subcat D
Additional metadata
Index,X,Y,Z
1,10.5,20.3,30.8
2,11.2,21.7,31.2
3,9.8,19.1,29.5
`

	f, err := os.Create(path) //nolint:gosec // tmpDir is t.TempDir()
	require.NoError(t, err)
	_, err = f.Write(csvutil.BOMBytes())
	require.NoError(t, err)
	_, err = f.WriteString(csvBody)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	fRead, err := os.Open(path)
	require.NoError(t, err)
	defer fRead.Close()

	data, err := NewMotionParser().Parse(fRead, path)
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Equal(t, []int{1, 2, 3}, data.Indices)

	for _, header := range data.Headers {
		assert.Falsef(t, bytes.Contains([]byte(header), csvutil.BOMBytes()),
			"header %q must not contain UTF-8 BOM", header)
	}
	for key := range data.Data {
		assert.Falsef(t, bytes.Contains([]byte(key), csvutil.BOMBytes()),
			"data key %q must not contain UTF-8 BOM", key)
	}
}

// TestMotionParser_LogsInfoOnEmptyRowSkip (L10) 鎖定:當 record[0] 為空字串
// (例如 leading-comma row `",X,Y"`)被 silent skip 時必須 emit Info-level log,
// 含 row_number 以便追查 silent miscount。
//
// 注意:Go encoding/csv 已自動 collapse 純空行(整列只有 newline),這些不會
// 抵達 parseDataRecord;此 test 涵蓋的是 csv reader 看得到、但 first-cell-empty
// 的 row(leading comma / 連串 whitespace 之類)。
//
// 行為契約:
//   - 解析仍成功(不該因為 first-cell-empty fail);
//   - 跳過的 row 數 = leading-comma 行數;
//   - 對每個 skip 都 emit 一筆 Info log,訊息含 "Motion CSV 空行已跳過" 與 row_number。
func TestMotionParser_LogsInfoOnEmptyRowSkip(t *testing.T) {
	// CSV 行 layout (1-based):
	//   1: Line 1
	//   2: Line 2
	//   3: Line 3
	//   4: Index,X,Y
	//   5: 1,10.5,20.3
	//   6: ,2.0,3.0    <-- record[0]="" → skip + log row 6
	//   7: 2,11.2,21.7
	//   8: ,4.0,5.0    <-- record[0]="" → skip + log row 8
	//   9: 3,9.8,19.1
	csvContent := `Line 1
Line 2
Line 3
Index,X,Y
1,10.5,20.3
,2.0,3.0
2,11.2,21.7
,4.0,5.0
3,9.8,19.1
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "motion_blank.csv")
	require.NoError(t, os.WriteFile(path, []byte(csvContent), 0o600)) //nolint:gosec // t.TempDir owned

	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelInfo, &buf, true) // JSON 方便穩定 assert
	parser := NewMotionParserWithLogger(logger)

	fBlank, err := os.Open(path)
	require.NoError(t, err)
	defer fBlank.Close()

	data, err := parser.Parse(fBlank, path)
	require.NoError(t, err)
	require.NotNil(t, data)

	// 兩個 first-cell-empty 行被 skip,合法資料維持 3 筆。
	assert.Equal(t, []int{1, 2, 3}, data.Indices,
		"first-cell-empty 行 skip 後 indices 仍為 1/2/3")

	out := buf.String()
	assert.Contains(t, out, "Motion CSV 空行已跳過",
		"L10:空行 skip 必須 emit Info log 訊息")
	assert.Contains(t, out, `"row_number":"6"`, "L10:空行 log 必須帶 row_number=6")
	assert.Contains(t, out, `"row_number":"8"`, "L10:空行 log 必須帶 row_number=8")
}

// TestMotionParser_WarnsOnNonIntFirstColumn pins when parseDataRecord
// silently skips a row because record[0] is not parseable as int, we must emit
// a warning carrying the absolute row number so operators investigating
// "missing samples" complaints can locate the offending row in their input.
// Without this, the parser quietly drops rows and downstream calculators see
// fewer samples than the user expected — silent miscount.
func TestMotionParser_WarnsOnNonIntFirstColumn(t *testing.T) {
	csvContent := `Line 1
Line 2
Line 3
Index,X,Y
1,10.5,20.3
not_an_int,11.2,21.7
3,9.8,19.1
`

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "motion_warn.csv")
	require.NoError(t, os.WriteFile(path, []byte(csvContent), 0o600)) //nolint:gosec // tmpDir is t.TempDir()

	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelWarn, &buf, true) // JSON for stable assertion
	parser := NewMotionParserWithLogger(logger)

	fWarn, err := os.Open(path)
	require.NoError(t, err)
	defer fWarn.Close()

	data, err := parser.Parse(fWarn, path)
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, []int{1, 3}, data.Indices, "malformed row must still be skipped")

	out := buf.String()
	assert.Contains(t, out, "not_an_int",
		"warning must include the offending cell value for diagnosability")
	// 必須帶 row number (1-based 絕對 CSV 行索引)，方便比對原始 CSV 行。
	// CSV 行佈局 (1-based):
	//   1: Line 1   2: Line 2   3: Line 3
	//   4: Index,X,Y
	//   5: 1,10.5,20.3
	//   6: not_an_int,11.2,21.7  ← 預期 row number
	//   7: 3,9.8,19.1
	// JSON 序列化經 sanitizeContextValue 走 fmt.Sprintf("%v") 把 int 變字串。
	assert.Contains(t, out, `"row_number":"6"`,
		"warning must include absolute row number (1-based) so operators can locate it")
}
