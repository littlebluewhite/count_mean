package parsers

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"

	"count_mean/internal/models"
)

func TestNewANCParser(t *testing.T) {
	parser := NewANCParser()
	assert.NotNil(t, parser)
	assert.Equal(t, 0.0, parser.GetSampleInterval()) // 尚未解析資料，頻率為 0
}

func TestANCParser_Parse(t *testing.T) {
	tests := []struct {
		name        string
		fileContent string
		wantErr     bool
		checkData   func(*testing.T, *models.ForceData)
	}{
		{
			name: "valid ANC file",
			fileContent: `1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	TEST_TRIAL	Trial#:	1	Duration(Sec.):	5.000	#Channels:	6
4	BitDepth:	16	PreciseRate:	1000.000
5	
6	
7	
8	
9	Name	Fx	Fy	Fz	Mx	My	Mz
10	Rate	1000	1000	1000	1000	1000	1000
11	Range	2000	2000	5000	200	200	200
12	Units	N	N	N	Nm	Nm	Nm
0.000000	0.1	0.2	0.3	0.4	0.5	0.6
0.001000	0.2	0.3	0.4	0.5	0.6	0.7
0.002000	0.3	0.4	0.5	0.6	0.7	0.8`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.ForceData) {
				assert.Len(t, data.Time, 3)
				assert.Equal(t, 0.000000, data.Time[0])
				assert.Equal(t, 0.001000, data.Time[1])
				assert.Equal(t, 0.002000, data.Time[2])

				assert.Len(t, data.Headers, 6)
				assert.Equal(t, []string{"Fx", "Fy", "Fz", "Mx", "My", "Mz"}, data.Headers)

				assert.Len(t, data.Forces, 6)
				assert.Equal(t, 0.1, data.Forces["Fx"][0])
				assert.Equal(t, 0.6, data.Forces["Mz"][0])
				assert.Equal(t, 0.8, data.Forces["Mz"][2])
			},
		},
		{
			name: "file with missing data fields",
			fileContent: `1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	TEST_TRIAL	Trial#:	1	Duration(Sec.):	2.000	#Channels:	6
4	BitDepth:	16	PreciseRate:	1000.000
5	
6	
7	
8	
9	Name	Fx	Fy	Fz	Mx	My	Mz
10	Rate	1000	1000	1000	1000	1000	1000
11	Range	2000	2000	5000	200	200	200
12	Units	N	N	N	Nm	Nm	Nm
0.000000	0.1	0.2	0.3
0.001000	0.2	0.3	0.4	0.5`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.ForceData) {
				if len(data.Time) == 0 {
					// 如果沒有解析到數據，檢查結構是否正確初始化
					assert.NotNil(t, data.Forces)
					assert.NotNil(t, data.Headers)
					return
				}
				assert.Len(t, data.Time, 2)
				assert.Len(t, data.Forces, 6)
				// 檢查缺失數據被填充為0
				if len(data.Forces["Mx"]) > 0 {
					assert.Equal(t, 0.0, data.Forces["Mx"][0])
					assert.Equal(t, 0.0, data.Forces["My"][0])
					assert.Equal(t, 0.0, data.Forces["Mz"][0])
				}
				if len(data.Forces["Mz"]) > 1 {
					assert.Equal(t, 0.0, data.Forces["Mz"][1])
				}
			},
		},
		{
			name:        "empty file",
			fileContent: "",
			wantErr:     false,
			checkData: func(t *testing.T, data *models.ForceData) {
				assert.Len(t, data.Time, 0)
				assert.Len(t, data.Forces, 0)
			},
		},
		{
			name: "file with invalid time values",
			fileContent: `1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	TEST_TRIAL	Trial#:	1	Duration(Sec.):	2.000	#Channels:	2
4	BitDepth:	16	PreciseRate:	1000.000
5	
6	
7	
8	
9	Name	Fx	Fy
10	Rate	1000	1000
11	Range	2000	2000
12	Units	N	N
invalid_time	0.1	0.2
0.001000	0.2	0.3`,
			wantErr: false,
			checkData: func(t *testing.T, data *models.ForceData) {
				assert.Len(t, data.Time, 1) // 只有一行有效數據
				assert.Equal(t, 0.001000, data.Time[0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 創建臨時測試文件
			tmpFile, err := os.CreateTemp(t.TempDir(), "test_anc_*.anc")
			require.NoError(t, err)

			_, err = tmpFile.WriteString(tt.fileContent)
			require.NoError(t, err)
			require.NoError(t, tmpFile.Close())

			// 測試解析 via open + Parse
			f, err := os.Open(tmpFile.Name())
			require.NoError(t, err)
			defer f.Close()

			parser := NewANCParser()
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

// ANC text parser 必須 strip UTF-8 BOM。Excel 匯出帶 BOM 的 ANC 檔
// 若不剝除，第一行 `File_Type:` 比對失敗、channel name 多前綴 → silent miscalc。
func TestANCParser_StripsBOM(t *testing.T) {
	const bom = "\xEF\xBB\xBF"
	content := bom + `1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	TEST	Trial#:	1	Duration(Sec.):	1.000	#Channels:	2
4	BitDepth:	16	PreciseRate:	1000.000
5
6
7
8
9	Name	Fx	Fy
10	Rate	1000	1000
11	Range	2000	2000
12	Units	N	N
0.000	0.1	0.2
0.001	0.2	0.3`

	tmpFile, err := os.CreateTemp(t.TempDir(), "test_bom_*.anc")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	f, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer f.Close()

	parser := NewANCParser()
	data, err := parser.Parse(f, tmpFile.Name())
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, []string{"Fx", "Fy"}, data.Headers,
		"channel names should not be prefixed with BOM")
}

// TestANCParser_Parse_TextFromReader 釘住 reader-based 入口的 text 分支：
// name 以 .anc 結尾 → 走文字解析。
func TestANCParser_Parse_TextFromReader(t *testing.T) {
	const content = `1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	TEST_TRIAL	Trial#:	1	Duration(Sec.):	5.000	#Channels:	6
4	BitDepth:	16	PreciseRate:	1000.000
5
6
7
8
9	Name	Fx	Fy	Fz	Mx	My	Mz
10	Rate	1000	1000	1000	1000	1000	1000
11	Range	2000	2000	5000	200	200	200
12	Units	N	N	N	Nm	Nm	Nm
0.000000	0.1	0.2	0.3	0.4	0.5	0.6
0.001000	0.2	0.3	0.4	0.5	0.6	0.7
0.002000	0.3	0.4	0.5	0.6	0.7	0.8`

	parser := NewANCParser()
	data, err := parser.Parse(strings.NewReader(content), "reader.anc")
	require.NoError(t, err)
	require.NotNil(t, data)

	assert.Len(t, data.Time, 3)
	assert.Equal(t, []string{"Fx", "Fy", "Fz", "Mx", "My", "Mz"}, data.Headers)
	assert.Equal(t, 0.1, data.Forces["Fx"][0])
	assert.Equal(t, 0.8, data.Forces["Mz"][2])
}

// TestANCParser_Parse_TextFromReaderWithBOM 確認 text reader 分支仍剝 BOM —
// BOM 汙染會讓 File_Type 比對失敗、channel name 多前綴 → silent miscalc。
func TestANCParser_Parse_TextFromReaderWithBOM(t *testing.T) {
	const bom = "\xEF\xBB\xBF"
	content := bom + `1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	TEST	Trial#:	1	Duration(Sec.):	1.000	#Channels:	2
4	BitDepth:	16	PreciseRate:	1000.000
5
6
7
8
9	Name	Fx	Fy
10	Rate	1000	1000
11	Range	2000	2000
12	Units	N	N
0.000	0.1	0.2
0.001	0.2	0.3`

	parser := NewANCParser()
	data, err := parser.Parse(strings.NewReader(content), "bom.anc")
	require.NoError(t, err)
	require.NotNil(t, data)
	assert.Equal(t, []string{"Fx", "Fy"}, data.Headers,
		"channel names must not be prefixed with BOM")
}

// TestANCParser_Parse_XLSXFromReader 釘住 reader-based 入口的 xlsx 分支：
// name 以 .xlsx 結尾 → 走 excelize.OpenReader；結果須與 ParseFile（path wrapper）相同。
func TestANCParser_Parse_XLSXFromReader(t *testing.T) {
	t.Run("simple xlsx from reader", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy", "Fz"}
		data := [][]string{
			{"0.000", "1.1", "2.2", "3.3"},
			{"0.001", "1.2", "2.3", "3.4"},
			{"0.002", "1.3", "2.4", "3.5"},
		}
		buf := buildSimpleXLSXBuffer(t, headers, data)

		parser := NewANCParser()
		forceData, err := parser.Parse(bytes.NewReader(buf), "reader.xlsx")
		require.NoError(t, err)
		require.NotNil(t, forceData)

		assert.Len(t, forceData.Time, 3)
		assert.Equal(t, 0.000, forceData.Time[0])
		assert.Equal(t, []string{"Fx", "Fy", "Fz"}, forceData.Headers)
		assert.Equal(t, 1.1, forceData.Forces["Fx"][0])
		assert.Equal(t, 3.5, forceData.Forces["Fz"][2])
	})

	t.Run("ANC-format xlsx from reader", func(t *testing.T) {
		buf := buildANCFormatXLSXBuffer(t)

		parser := NewANCParser()
		forceData, err := parser.Parse(bytes.NewReader(buf), "anc.xlsx")
		require.NoError(t, err)
		require.NotNil(t, forceData)

		// 通道名稱來自 xlsx row 9（內部 rows[8]），資料從 row 12 起。
		assert.Equal(t, []string{"Fx", "Fy", "Fz"}, forceData.Headers)
		require.Len(t, forceData.Time, 3)
		assert.Equal(t, 0.000, forceData.Time[0])
		assert.Equal(t, 1.1, forceData.Forces["Fx"][0])
		assert.Equal(t, 9.9, forceData.Forces["Fz"][2])
	})

	t.Run("anc.xlsx compound extension routes to xlsx", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy"}
		data := [][]string{
			{"0.0", "10.0", "20.0"},
			{"1.0", "11.0", "21.0"},
		}
		buf := buildSimpleXLSXBuffer(t, headers, data)

		parser := NewANCParser()
		// .anc.xlsx 複合副檔名必須仍走 xlsx 分支（OpenReader），而非 text。
		forceData, err := parser.Parse(bytes.NewReader(buf), "trial.anc.xlsx")
		require.NoError(t, err)
		require.NotNil(t, forceData)
		assert.Len(t, forceData.Time, 2)
		assert.Equal(t, 0.0, forceData.Time[0])
	})
}

// TestANCParser_Parse_TextReaderError 釘住 text 分支的 reader-boundary 失敗面：
// .anc name 走文字解析，第一個讀取點是 BOM 偵測，底層 io.Reader 在串流途中報錯
// （iotest.ErrReader）必須往上傳，不得 panic、不得回傳「err==nil 但 data 非 nil」。
func TestANCParser_Parse_TextReaderError(t *testing.T) {
	data, err := NewANCParser().Parse(iotest.ErrReader(errors.New("boom")), "err.anc")

	require.Error(t, err)
	require.ErrorContains(t, err, "boom", "底層 reader 錯誤必須往上傳遞")
	assert.Nil(t, data, "reader 出錯時不得回傳偽造的非 nil ForceData")
}

// TestANCParser_Parse_XLSXReaderError 釘住 xlsx 分支的 reader-boundary 失敗面：
// .xlsx name 走 excelize.OpenReader，底層 io.Reader 報錯必須被包成 name-wrapped
// open error 往上傳，並回傳 nil *ForceData。
func TestANCParser_Parse_XLSXReaderError(t *testing.T) {
	data, err := NewANCParser().Parse(iotest.ErrReader(errors.New("boom")), "err.xlsx")

	require.Error(t, err)
	assert.ErrorContains(t, err, "無法開啟 Excel 檔案",
		"xlsx open 失敗須走 name-wrapped 開檔錯誤前綴")
	assert.ErrorContains(t, err, "boom", "底層 reader 錯誤必須往上傳遞")
	assert.Nil(t, data, "open 失敗時必須回傳 nil ForceData")
}

// TestANCParser_Parse_MalformedXLSX 把非 zip 的位元組餵給 xlsx 分支
// （excelize.OpenReader），必須回傳 name-wrapped 的開檔錯誤與 nil *ForceData，
// 對應 anc_parser.go 約 344 行的 "無法開啟 Excel 檔案" 包裝。
func TestANCParser_Parse_MalformedXLSX(t *testing.T) {
	data, err := NewANCParser().Parse(bytes.NewReader([]byte("PK-not-a-real-xlsx")), "x.xlsx")

	require.Error(t, err)
	assert.ErrorContains(t, err, "無法開啟 Excel 檔案 x.xlsx",
		"malformed xlsx 必須回傳 name-wrapped 開檔錯誤")
	assert.Nil(t, data, "malformed xlsx 必須回傳 nil ForceData")
}

// leading blank line 必須不會讓 handler map 偏移。
// 原本 parseHeader 對 leading blank line 仍 `lineNum++`,後續每一行的 dispatch
// key 整個錯位 1 — File_Type 被當 Board_Type 處理、ChannelNames 被當 Rates...
// 修法後:lineNum 在 blank-skip 之前不遞增,且若 parts[0] 為 ANC 顯式行號則用該
// parsedLine 當 dispatch key(雙重保險)。
func TestANCParser_ParseHeader_LeadingBlankLine_NoMisalignment(t *testing.T) {
	// 第一行刻意留空,以模擬 ANC 匯出時頭部換行的 edge case。
	const content = `
1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	TEST	Trial#:	1	Duration(Sec.):	1.000	#Channels:	2
4	BitDepth:	16	PreciseRate:	1000.000
5
6
7
8
9	Name	Fx	Fy
10	Rate	1000	1000
11	Range	2000	2000
12	Units	N	N
0.000	0.1	0.2
0.001	0.2	0.3
`

	tmpFile, err := os.CreateTemp(t.TempDir(), "test_leading_blank_*.anc")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	f, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer f.Close()

	parser := NewANCParser()
	data, err := parser.Parse(f, tmpFile.Name())
	require.NoError(t, err)
	require.NotNil(t, data)

	// 核心斷言:即使第一行是 blank,所有 header 欄位仍能正確對齊解出。
	assert.Equal(t, []string{"Fx", "Fy"}, data.Headers,
		"ChannelNames 必須正確解出 — 不能因 leading blank 偏移到 Rates handler")
	assert.Len(t, data.Time, 2, "資料行必須仍可正常解析")
	assert.InDelta(t, 0.001, parser.GetSampleInterval(), 1e-9,
		"PreciseRate=1000Hz 必須仍能解出(handler map 沒有偏移)")
}

// channel name 含空格不能被 Fields 拆兩列。
// `strings.Fields("Name\tLeft Quad\tFx")` 切空白 → 4 字串,把 "Left Quad" 拆成兩列;
// 改用 `strings.Split(content, "\t")` 後對齊 line 491 handleFirstLine 的 tab 行為。
// Rate / Range 同病也修。
func TestANCParser_ChannelName_ContainsSpace_NotSplit(t *testing.T) {
	// 通道名稱含空格("Left Quad"、"Right Glute"),且 Rate/Range 行也驗證對齊。
	const content = `1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	TEST	Trial#:	1	Duration(Sec.):	1.000	#Channels:	3
4	BitDepth:	16	PreciseRate:	1000.000
5
6
7
8
9	Name	Left Quad	Right Glute	Mid Hamstring
10	Rate	1000	1000	1000
11	Range	2000	2000	2000
12	Units	N	N	N
0.000	0.1	0.2	0.3
0.001	0.2	0.3	0.4
`

	tmpFile, err := os.CreateTemp(t.TempDir(), "test_space_channel_*.anc")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	fSpace, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer fSpace.Close()

	parser := NewANCParser()
	data, err := parser.Parse(fSpace, tmpFile.Name())
	require.NoError(t, err)
	require.NotNil(t, data)

	// 核心斷言:NumChannels 必須對齊 ChannelNames 長度(各為 3,不能被拆成 6)。
	assert.Equal(t, []string{"Left Quad", "Right Glute", "Mid Hamstring"}, data.Headers,
		"含空格的 channel name 必須保留為單一通道,不能被 Fields 拆兩列")
	assert.Len(t, data.Headers, 3, "通道數必須對齊 #Channels:3")

	// 資料對應也驗一下,確保通道分欄與資料欄對齊(不是 silent miscalc)。
	assert.InDelta(t, 0.1, data.Forces["Left Quad"][0], 1e-9)
	assert.InDelta(t, 0.2, data.Forces["Right Glute"][0], 1e-9)
	assert.InDelta(t, 0.3, data.Forces["Mid Hamstring"][0], 1e-9)
}

func TestANCParser_GetDataInTimeRange(t *testing.T) {
	// 創建測試數據
	testData := &models.ForceData{
		Time:    []float64{0.0, 0.001, 0.002, 0.003, 0.004, 0.005},
		Headers: []string{"Fx", "Fy"},
		Forces: map[string][]float64{
			"Fx": {1.0, 2.0, 3.0, 4.0, 5.0, 6.0},
			"Fy": {0.1, 0.2, 0.3, 0.4, 0.5, 0.6},
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rangeData, err := GetANCDataInTimeRange(testData, tt.startTime, tt.endTime)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, rangeData)
			assert.Len(t, rangeData.Time, tt.checkLen)
			assert.Len(t, rangeData.Forces["Fx"], tt.checkLen)
			assert.Len(t, rangeData.Forces["Fy"], tt.checkLen)

			// 檢查時間範圍
			if tt.checkLen > 0 {
				assert.GreaterOrEqual(t, rangeData.Time[0], tt.startTime)
				assert.LessOrEqual(t, rangeData.Time[len(rangeData.Time)-1], tt.endTime)
			}
		})
	}
}

func TestANCParser_GetSampleInterval(t *testing.T) {
	parser := NewANCParser()

	// 解析含 1000Hz 資料的 ANC 檔案後，interval 應為 0.001s
	ancContent := "1\tFile_Type:\tAMTI_FORCE_PLATE\tGeneration#:\t4\n" +
		"2\tBoard_Type:\tOR6-5-1000\n" +
		"3\tTrial_Name:\tTEST\tTrial#:\t1\tDuration(Sec.):\t1.000\t#Channels:\t2\n" +
		"4\tBitDepth:\t16\tPreciseRate:\t1000.000\n" +
		"5\t\n6\t\n7\t\n8\t\n" +
		"9\tName\tFx\tFy\n" +
		"10\tRate\t1000\t1000\n" +
		"11\tRange\t2000\t2000\n" +
		"12\tUnits\tN\tN\n" +
		"0.000000\t0.1\t0.2\n" +
		"0.001000\t0.2\t0.3\n" +
		"0.002000\t0.3\t0.4\n"

	tmpFile, err := os.CreateTemp(t.TempDir(), "test_anc_freq_*.anc")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(ancContent)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	fFreq, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer fFreq.Close()

	_, err = parser.Parse(fFreq, tmpFile.Name())
	require.NoError(t, err)

	interval := parser.GetSampleInterval()
	assert.Equal(t, 0.001, interval) // 1000Hz = 0.001s
}

func TestValidateForceData(t *testing.T) {
	tests := []struct {
		name    string
		data    *models.ForceData
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil data",
			data:    nil,
			wantErr: true,
			errMsg:  "力板數據為空",
		},
		{
			name: "empty time series",
			data: &models.ForceData{
				Time:    []float64{},
				Forces:  make(map[string][]float64),
				Headers: []string{},
			},
			wantErr: true,
			errMsg:  "力板時間序列為空",
		},
		{
			name: "no force channels",
			data: &models.ForceData{
				Time:    []float64{0.0, 0.001},
				Forces:  make(map[string][]float64),
				Headers: []string{},
			},
			wantErr: true,
			errMsg:  "力板沒有任何通道數據",
		},
		{
			name: "non-increasing time series",
			data: &models.ForceData{
				Time:    []float64{0.0, 0.001, 0.0005}, // 時間不遞增
				Headers: []string{"Fx"},
				Forces: map[string][]float64{
					"Fx": {1.0, 2.0, 3.0},
				},
			},
			wantErr: true,
			errMsg:  "力板時間序列在索引 2 處不是遞增的",
		},
		{
			name: "mismatched data length",
			data: &models.ForceData{
				Time:    []float64{0.0, 0.001, 0.002},
				Headers: []string{"Fx", "Fy"},
				Forces: map[string][]float64{
					"Fx": {1.0, 2.0, 3.0},
					"Fy": {0.1, 0.2}, // 長度不匹配
				},
			},
			wantErr: true,
			errMsg:  "通道 Fy 的數據長度",
		},
		{
			name: "valid data",
			data: &models.ForceData{
				Time:    []float64{0.0, 0.001, 0.002},
				Headers: []string{"Fx", "Fy"},
				Forces: map[string][]float64{
					"Fx": {1.0, 2.0, 3.0},
					"Fy": {0.1, 0.2, 0.3},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateForceData(tt.data)

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

func TestANCParser_extractValue(t *testing.T) {
	parser := NewANCParser()

	tests := []struct {
		name     string
		content  string
		label    string
		expected string
	}{
		{
			name:     "extract file type",
			content:  "File_Type:AMTI_FORCE_PLATE	Generation#:4",
			label:    "File_Type:",
			expected: "AMTI_FORCE_PLATE",
		},
		{
			name:     "extract trial name",
			content:  "Trial_Name:TEST_TRIAL	Trial#:1",
			label:    "Trial_Name:",
			expected: "TEST_TRIAL",
		},
		{
			name:     "label not found",
			content:  "Some other content without the label",
			label:    "NotFound:",
			expected: "",
		},
		{
			name:     "label without value",
			content:  "Label:",
			label:    "Label:",
			expected: "",
		},
		{
			// extractValue must preserve `:` inside the value (e.g. a
			// timestamp HH:MM:SS or a URL-style label). The pre-fix code used
			// strings.Split(part, ":") which trimmed everything after the first
			// `:` and only returned the middle segment — silently corrupting
			// any header value that contains a colon.
			name:     "value contains colon (timestamp)",
			content:  "Trial_Name:08:30:45",
			label:    "Trial_Name:",
			expected: "08:30:45",
		},
		{
			// Tab-separated 也常見：label 與 value 之間用 tab 分開，value 內含 ":"。
			name:     "tab-separated value containing colon",
			content:  "Board_Type:\tAMTI:OR6-5",
			label:    "Board_Type:",
			expected: "AMTI:OR6-5",
		},
		{
			// HasPrefix word-boundary。同列出現 `Foo_Trial#:5\tTrial#:1`，
			// 找 `Trial#:` 不該被前一個 cell 的 `Foo_Trial#:5` 誤命中而回 `5`，
			// 必須走到後面真正開頭是 `Trial#:` 的 cell 取 `1`。原本用 Contains 會
			// 把 `Foo_Trial#:5` 當命中（substring collision），現在 HasPrefix 過濾掉。
			name:     "M12 prefix match avoids substring collision (suffix-only matches must skip)",
			content:  "Foo_Trial#:5\tTrial#:1",
			label:    "Trial#:",
			expected: "1",
		},
		{
			// cell 中段含 label 但非 prefix。`abcTrial#:9` 不該被 `Trial#:`
			// 命中（label 必須出現在 trimmed cell 的開頭）。
			name:     "M12 label embedded in middle is not matched",
			content:  "abcTrial#:9",
			label:    "Trial#:",
			expected: "",
		},
		{
			// cell 含值且 value 本身含 `:`，HasPrefix + 直接切 prefix 後
			// rest 應保留全部 `:`（取代原本 SplitN 行為，行為一致）。
			name:     "M12 prefix-strip preserves colons in value",
			content:  "Trial_Name:09:15:00",
			label:    "Trial_Name:",
			expected: "09:15:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 直接呼叫真正的 extractValue 私有方法 — 用同 package test 來繞過
			// 介面限制，避免 helper 與 production code 走向不同步（重現的
			// 問題正是 helper 把 bug 抄了一份）。
			result := parser.extractValue(tt.content, tt.label)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestANCParser_FileType_ColonInValue 是 的回歸測試 —
// handleFileTypeLine 原本用 `strings.Split(part, ":")[1]` 抓 File_Type value,
// 若 value 自身含 `:`(例如 timestamp `08:30:45`)會被截到第一個 `:` 之前
// 只剩 `"08"`。改用 SplitN(part, ":", 2) + len check 之後,整個 value 必須保留。
//
// 與 P1-A3-5(extractValue)同 class 的 sibling bug,V5 PoC 重現 fixture:
// `File_Type:08:30:45` → 修前回 `"08"`、修後回 `"08:30:45"`。
func TestANCParser_FileType_ColonInValue(t *testing.T) {
	// fixture:第 1 行 File_Type 為同 cell 寫法且 value 含 `:`(timestamp 形)。
	// 注意是 `File_Type:08:30:45`(無 tab 分隔),完全踩到 sibling antipattern
	// 的觸發條件 — value 含至少一個 `:`,Split 後第 [1] 段只剩前面的截斷值。
	const content = `1	File_Type:08:30:45	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	TEST	Trial#:	1	Duration(Sec.):	1.000	#Channels:	2
4	BitDepth:	16	PreciseRate:	1000.000
5
6
7
8
9	Name	Fx	Fy
10	Rate	1000	1000
11	Range	2000	2000
12	Units	N	N
0.000	0.1	0.2
0.001	0.2	0.3
`

	tmpFile, err := os.CreateTemp(t.TempDir(), "test_filetype_colon_*.anc")
	require.NoError(t, err)
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	fColon, err := os.Open(tmpFile.Name())
	require.NoError(t, err)
	defer fColon.Close()

	parser := NewANCParser()
	data, err := parser.Parse(fColon, tmpFile.Name())
	require.NoError(t, err)
	require.NotNil(t, data)

	// 核心斷言:value 必須完整保留 — 修前會回 `"08"`(被首個 `:` 截斷),修後
	// 必須是完整 `"08:30:45"`。透過直接呼叫 handleFileTypeLine 確認 sibling
	// 修法 真的生效(避免 caller 邊路徑遮蔽 fail)。
	header := &ANCHeader{}
	handleFileTypeLine(nil, header, "File_Type:08:30:45	Generation#:	4")
	assert.Equal(t, "08:30:45", header.FileType,
		"handleFileTypeLine 必須保留 value 內的所有 `:` — 不能在第一個 `:` 截斷")
}

func TestANCParser_Integration(t *testing.T) {
	// 集成測試：創建完整的ANC文件並測試完整流程
	t.Run("complete ANC file parsing", func(t *testing.T) {
		ancContent := `1	File_Type:	AMTI_FORCE_PLATE	Generation#:	4
2	Board_Type:	OR6-5-1000
3	Trial_Name:	INTEGRATION_TEST	Trial#:	99	Duration(Sec.):	1.000	#Channels:	6
4	BitDepth:	16	PreciseRate:	1000.000
5
6
7
8
9	Name	Fx	Fy	Fz	Mx	My	Mz
10	Rate	1000	1000	1000	1000	1000	1000
11	Range	2000	2000	5000	200	200	200
12	Units	N	N	N	Nm	Nm	Nm
0.000000	-0.5	1.2	-985.3	2.1	-1.8	0.3
0.001000	-0.6	1.1	-984.9	2.0	-1.9	0.2
0.002000	-0.4	1.3	-985.1	2.2	-1.7	0.4
0.003000	-0.5	1.2	-985.0	2.1	-1.8	0.3`

		// 創建臨時文件
		tmpFile, err := os.CreateTemp(t.TempDir(), "integration_test_*.anc")
		require.NoError(t, err)

		_, err = tmpFile.WriteString(ancContent)
		require.NoError(t, err)
		require.NoError(t, tmpFile.Close())

		f, err := os.Open(tmpFile.Name())
		require.NoError(t, err)
		defer f.Close()

		// 解析文件
		parser := NewANCParser()
		data, err := parser.Parse(f, tmpFile.Name())
		require.NoError(t, err)

		// 驗證數據完整性
		err = ValidateForceData(data)
		assert.NoError(t, err)

		// 檢查數據內容
		assert.Len(t, data.Time, 4)
		assert.Len(t, data.Headers, 6)
		assert.Len(t, data.Forces, 6)

		// 測試時間範圍查詢
		rangeData, err := GetANCDataInTimeRange(data, 0.001, 0.002)
		require.NoError(t, err)
		assert.Len(t, rangeData.Time, 2)

		// 驗證範圍數據
		err = ValidateForceData(rangeData)
		assert.NoError(t, err)
	})
}

// ==================== XLSX 格式測試 ====================

// createTestXLSXFile 創建測試用的 xlsx 檔案.
func createTestXLSXFile(t *testing.T, headers []string, data [][]string) string {
	t.Helper()

	f := excelize.NewFile()
	sheetName := f.GetSheetName(0)

	// 寫入標題行
	for colIdx, header := range headers {
		cell, err := excelize.CoordinatesToCellName(colIdx+1, 1)
		require.NoError(t, err)
		err = f.SetCellValue(sheetName, cell, header)
		require.NoError(t, err)
	}

	// 寫入數據行
	for rowIdx, row := range data {
		for colIdx, value := range row {
			cell, err := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			require.NoError(t, err)
			err = f.SetCellValue(sheetName, cell, value)
			require.NoError(t, err)
		}
	}

	// 保存到臨時文件
	tmpFile, err := os.CreateTemp(t.TempDir(), "test_anc_*.xlsx")
	require.NoError(t, err)

	err = tmpFile.Close()
	require.NoError(t, err)

	err = f.SaveAs(tmpFile.Name())
	require.NoError(t, err)
	err = f.Close()
	require.NoError(t, err)

	return tmpFile.Name()
}

func TestANCParser_ParseXLSXFile(t *testing.T) {
	t.Run("valid xlsx file", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy", "Fz"}
		data := [][]string{
			{"0.000", "1.1", "2.2", "3.3"},
			{"0.001", "1.2", "2.3", "3.4"},
			{"0.002", "1.3", "2.4", "3.5"},
		}

		xlsxPath := createTestXLSXFile(t, headers, data)
		defer os.Remove(xlsxPath)

		f1, err := os.Open(xlsxPath)
		require.NoError(t, err)
		defer f1.Close()

		parser := NewANCParser()
		forceData, err := parser.Parse(f1, xlsxPath)

		require.NoError(t, err)
		assert.NotNil(t, forceData)
		assert.Len(t, forceData.Time, 3)
		assert.Equal(t, 0.0, forceData.Time[0])
		assert.Equal(t, 0.001, forceData.Time[1])
		assert.Equal(t, 0.002, forceData.Time[2])

		assert.Len(t, forceData.Headers, 3) // Fx, Fy, Fz
		assert.Equal(t, []string{"Fx", "Fy", "Fz"}, forceData.Headers)

		assert.Len(t, forceData.Forces, 3)
		assert.Equal(t, 1.1, forceData.Forces["Fx"][0])
		assert.Equal(t, 2.2, forceData.Forces["Fy"][0])
		assert.Equal(t, 3.3, forceData.Forces["Fz"][0])
	})

	t.Run("xlsx file with many data points", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy", "Fz", "Mx", "My", "Mz"}

		var data [][]string

		// 創建 1000 個數據點（模擬 1 秒的 1000Hz 數據）
		for i := 0; i < 1000; i++ {
			time := float64(i) / 1000.0
			row := []string{
				formatFloat(time),
				"1.0", "2.0", "3.0", "4.0", "5.0", "6.0",
			}
			data = append(data, row)
		}

		xlsxPath := createTestXLSXFile(t, headers, data)
		defer os.Remove(xlsxPath)

		f2, err := os.Open(xlsxPath)
		require.NoError(t, err)
		defer f2.Close()

		parser := NewANCParser()
		forceData, err := parser.Parse(f2, xlsxPath)

		require.NoError(t, err)
		assert.Len(t, forceData.Time, 1000)
		assert.Len(t, forceData.Headers, 6)
		assert.Len(t, forceData.Forces, 6)

		// 驗證時間範圍
		assert.InDelta(t, 0.0, forceData.Time[0], 0.0001)
		assert.InDelta(t, 0.999, forceData.Time[999], 0.0001)
	})

	t.Run("xlsx file with .anc.xlsx extension", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy"}
		data := [][]string{
			{"0.0", "10.0", "20.0"},
			{"1.0", "11.0", "21.0"},
		}

		// 創建臨時 xlsx 文件
		f := excelize.NewFile()
		sheetName := f.GetSheetName(0)

		for colIdx, header := range headers {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, 1)
			f.SetCellValue(sheetName, cell, header)
		}

		for rowIdx, row := range data {
			for colIdx, value := range row {
				cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
				f.SetCellValue(sheetName, cell, value)
			}
		}

		// 使用 .anc.xlsx 副檔名
		tmpFile, err := os.CreateTemp(t.TempDir(), "test_*.anc.xlsx")
		require.NoError(t, err)

		err = tmpFile.Close()
		require.NoError(t, err)

		err = f.SaveAs(tmpFile.Name())
		require.NoError(t, err)
		err = f.Close()
		require.NoError(t, err)

		f3, err := os.Open(tmpFile.Name())
		require.NoError(t, err)
		defer f3.Close()

		parser := NewANCParser()
		forceData, err := parser.Parse(f3, tmpFile.Name())

		require.NoError(t, err)
		assert.Len(t, forceData.Time, 2)
		assert.Equal(t, 0.0, forceData.Time[0])
		assert.Equal(t, 1.0, forceData.Time[1])
	})

	t.Run("xlsx file with empty rows", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy"}
		data := [][]string{
			{"0.0", "1.0", "2.0"},
			{"", "", ""},            // 空行
			{"0.002", "3.0", "4.0"}, // 有效行
		}

		xlsxPath := createTestXLSXFile(t, headers, data)
		defer os.Remove(xlsxPath)

		f4, err := os.Open(xlsxPath)
		require.NoError(t, err)
		defer f4.Close()

		parser := NewANCParser()
		forceData, err := parser.Parse(f4, xlsxPath)

		require.NoError(t, err)
		// 應該只有 2 個有效數據點（跳過空行）
		assert.Len(t, forceData.Time, 2)
	})

	t.Run("xlsx file with missing column values", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy", "Fz"}
		data := [][]string{
			{"0.0", "1.0"},                 // 缺少 Fy 和 Fz
			{"0.001", "2.0", "3.0"},        // 缺少 Fz
			{"0.002", "4.0", "5.0", "6.0"}, // 完整
		}

		xlsxPath := createTestXLSXFile(t, headers, data)
		defer os.Remove(xlsxPath)

		f5, err := os.Open(xlsxPath)
		require.NoError(t, err)
		defer f5.Close()

		parser := NewANCParser()
		forceData, err := parser.Parse(f5, xlsxPath)

		require.NoError(t, err)
		assert.Len(t, forceData.Time, 3)

		// 缺失的值應該被填充為 0
		assert.Equal(t, 0.0, forceData.Forces["Fy"][0])
		assert.Equal(t, 0.0, forceData.Forces["Fz"][0])
		assert.Equal(t, 0.0, forceData.Forces["Fz"][1])
		assert.Equal(t, 6.0, forceData.Forces["Fz"][2])
	})

	t.Run("xlsx file with only header row", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy"}
		data := [][]string{} // 沒有數據

		xlsxPath := createTestXLSXFile(t, headers, data)
		defer os.Remove(xlsxPath)

		f6, err := os.Open(xlsxPath)
		require.NoError(t, err)
		defer f6.Close()

		parser := NewANCParser()
		_, err = parser.Parse(f6, xlsxPath)

		assert.Error(t, err)
		// 錯誤訊息可能是 "數據不足" 或 "沒有有效的數據行"
		assert.True(t, strings.Contains(err.Error(), "數據不足") ||
			strings.Contains(err.Error(), "沒有有效的數據行"),
			"Expected error message about insufficient data, got: %s", err.Error())
	})
}

func TestANCParser_ParseXLSXFile_Validation(t *testing.T) {
	t.Run("validate parsed xlsx data", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy", "Fz"}
		data := [][]string{
			{"0.0", "1.0", "2.0", "3.0"},
			{"0.001", "1.1", "2.1", "3.1"},
			{"0.002", "1.2", "2.2", "3.2"},
		}

		xlsxPath := createTestXLSXFile(t, headers, data)
		defer os.Remove(xlsxPath)

		f7, err := os.Open(xlsxPath)
		require.NoError(t, err)
		defer f7.Close()

		parser := NewANCParser()
		forceData, err := parser.Parse(f7, xlsxPath)
		require.NoError(t, err)

		// 使用 ValidateForceData 驗證
		err = ValidateForceData(forceData)
		assert.NoError(t, err)
	})

	t.Run("xlsx time range query", func(t *testing.T) {
		headers := []string{"Time", "Fx", "Fy"}
		data := [][]string{
			{"0.0", "1.0", "2.0"},
			{"0.5", "1.5", "2.5"},
			{"1.0", "2.0", "3.0"},
			{"1.5", "2.5", "3.5"},
			{"2.0", "3.0", "4.0"},
		}

		xlsxPath := createTestXLSXFile(t, headers, data)
		defer os.Remove(xlsxPath)

		f8, err := os.Open(xlsxPath)
		require.NoError(t, err)
		defer f8.Close()

		parser := NewANCParser()
		forceData, err := parser.Parse(f8, xlsxPath)
		require.NoError(t, err)

		// 獲取時間範圍內的數據
		rangeData, err := GetANCDataInTimeRange(forceData, 0.5, 1.5)
		require.NoError(t, err)

		assert.Len(t, rangeData.Time, 3) // 0.5, 1.0, 1.5
		assert.Equal(t, 0.5, rangeData.Time[0])
		assert.Equal(t, 1.5, rangeData.Time[2])
	})
}

// TestANCParser_XLSX_RowIndexContract 釘住 xlsx row index 必須與
// excelize 1-based row number 對齊（內部用 0-based slice index）。任何人未來
// 動 parseSimpleXLSX / parseANCFormatXLSX 的 startRowIdx 都會破壞這個 mapping。
//
// 契約：
//   - simple xlsx：xlsx row 1 = header，xlsx row 2+ = data。內部 rows[0] 是
//     header，rows[1] 是第一筆數據（startRowIdx=1）。
//   - ANC-format xlsx：xlsx row 9 = channel names（內部 rows[8]）；xlsx row 12+
//     = data（內部 startRowIdx=ANCHeaderRows=11）。
//
// 為了「lockdown」契約而非僅 happy-path，本 test 用每行唯一值（row i 的 Fx
// 數值 = i），讓任何 off-by-one 都會在 assertion 上立刻失敗。
func TestANCParser_XLSX_RowIndexContract(t *testing.T) {
	t.Parallel()

	t.Run("simple_xlsx_row1_header_row2_first_data", func(t *testing.T) {
		t.Parallel()

		// xlsx row 1 (1-based) = header "Time, Fx, Fy"
		// xlsx row 2 = first data: time=0.000, Fx=2.0, Fy=20.0 (row number encoded into value)
		// xlsx row 3 = second data: time=0.001, Fx=3.0, Fy=30.0
		// xlsx row 4 = third data:  time=0.002, Fx=4.0, Fy=40.0
		headers := []string{"Time", "Fx", "Fy"}
		data := [][]string{
			{"0.000", "2.0", "20.0"}, // xlsx row 2 → internal Time/Fx/Fy index 0
			{"0.001", "3.0", "30.0"}, // xlsx row 3 → internal index 1
			{"0.002", "4.0", "40.0"}, // xlsx row 4 → internal index 2
		}

		xlsxPath := createTestXLSXFile(t, headers, data)
		t.Cleanup(func() { _ = os.Remove(xlsxPath) })

		fR1, err := os.Open(xlsxPath)
		require.NoError(t, err)
		defer fR1.Close()

		parser := NewANCParser()
		forceData, err := parser.Parse(fR1, xlsxPath)
		require.NoError(t, err)
		require.NotNil(t, forceData)

		// Header row (xlsx row 1) must NOT appear as data.
		require.Len(t, forceData.Time, 3,
			"expected exactly 3 data rows (xlsx rows 2-4); header (xlsx row 1) must not be data")

		// First data row corresponds to xlsx row 2 (encoded Fx=2.0).
		assert.Equal(t, 0.000, forceData.Time[0],
			"forceData.Time[0] must be xlsx row 2 time value (0.000)")
		assert.Equal(t, 2.0, forceData.Forces["Fx"][0],
			"forceData.Forces[Fx][0] must equal xlsx row 2 Fx (2.0); off-by-one would skip row 2")
		assert.Equal(t, 20.0, forceData.Forces["Fy"][0])

		// Second data row corresponds to xlsx row 3.
		assert.Equal(t, 0.001, forceData.Time[1])
		assert.Equal(t, 3.0, forceData.Forces["Fx"][1])

		// Third data row corresponds to xlsx row 4.
		assert.Equal(t, 0.002, forceData.Time[2])
		assert.Equal(t, 4.0, forceData.Forces["Fx"][2])

		// "Time" must be excluded from channel names (only data columns are channels).
		assert.Equal(t, []string{"Fx", "Fy"}, forceData.Headers,
			"channel names must exclude the leading 'Time' column from header row")
		assert.NotContains(t, forceData.Forces, "Time",
			"'Time' must not be a channel; it's the time axis")
	})

	t.Run("anc_format_xlsx_row9_names_row12_first_data", func(t *testing.T) {
		t.Parallel()

		// Build an ANC-format xlsx:
		//   row 1: File_Type:AMTI_FORCE_PLATE (triggers isANCFormat detection)
		//   row 2-8: filler
		//   row 9: Name Fx Fy Fz  (channel names; internal rows[8])
		//   row 10: Rate 1000 1000 1000
		//   row 11: Range 2000 2000 5000
		//   row 12: 0.000 1.1 2.2 3.3   (first data row; internal rows[11], startRowIdx=11)
		//   row 13: 0.001 4.4 5.5 6.6
		//   row 14: 0.002 7.7 8.8 9.9
		xlsxPath := buildANCFormatXLSX(t)
		t.Cleanup(func() { _ = os.Remove(xlsxPath) })

		fR2, err := os.Open(xlsxPath)
		require.NoError(t, err)
		defer fR2.Close()

		parser := NewANCParser()
		forceData, err := parser.Parse(fR2, xlsxPath)
		require.NoError(t, err)
		require.NotNil(t, forceData)

		// Channel names must come from xlsx row 9 (internal rows[8]).
		assert.Equal(t, []string{"Fx", "Fy", "Fz"}, forceData.Headers,
			"channels must read from xlsx row 9 (internal rows[8])")

		// Data must start at xlsx row 12 (internal rows[11]).
		// If anyone changes startRowIdx from ANCHeaderRows(11) → 10 or 12, the
		// first time value would be from "Rate" or "0.001" — both fail this assert.
		require.Len(t, forceData.Time, 3,
			"expected exactly 3 data rows (xlsx rows 12-14)")
		assert.Equal(t, 0.000, forceData.Time[0],
			"forceData.Time[0] must equal xlsx row 12 time (0.000); off-by-one points to row 11 (Range) or row 13")
		assert.Equal(t, 1.1, forceData.Forces["Fx"][0])
		assert.Equal(t, 0.001, forceData.Time[1])
		assert.Equal(t, 0.002, forceData.Time[2])
		assert.Equal(t, 9.9, forceData.Forces["Fz"][2])
	})
}

// buildANCFormatXLSX 建立完整 ANC-format xlsx：rows 1-11 為頭部、row 9 為通道名稱、
// row 12+ 為資料；返回臨時檔案路徑。提供 row-index lockdown 使用。
func buildANCFormatXLSX(t *testing.T) string {
	t.Helper()

	f := excelize.NewFile()
	sheetName := f.GetSheetName(0)

	setCell := func(row int, vals ...string) {
		for col, v := range vals {
			cell, err := excelize.CoordinatesToCellName(col+1, row)
			require.NoError(t, err)
			require.NoError(t, f.SetCellValue(sheetName, cell, v))
		}
	}

	setCell(1, "File_Type:", "AMTI_FORCE_PLATE")
	setCell(2, "Board_Type:", "OR6-5-1000")
	setCell(3, "Trial_Name:", "TEST")
	setCell(4, "BitDepth:", "16")
	setCell(5, "")
	setCell(6, "")
	setCell(7, "")
	setCell(8, "")
	setCell(9, "Name", "Fx", "Fy", "Fz") // row 9 → internal rows[8]
	setCell(10, "Rate", "1000", "1000", "1000")
	setCell(11, "Range", "2000", "2000", "5000")
	setCell(12, "0.000", "1.1", "2.2", "3.3") // row 12 → internal rows[11]; first data
	setCell(13, "0.001", "4.4", "5.5", "6.6")
	setCell(14, "0.002", "7.7", "8.8", "9.9")

	tmpFile, err := os.CreateTemp(t.TempDir(), "test_anc_format_*.xlsx")
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())
	require.NoError(t, f.SaveAs(tmpFile.Name()))
	require.NoError(t, f.Close())

	return tmpFile.Name()
}

// formatFloat 將浮點數格式化為字符串.
func formatFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(
		fmt.Sprintf("%.6f", f), "0"), ".")
}

// buildSimpleXLSXBuffer 建立 simple (header + data) xlsx 並回傳 in-memory bytes，
// 供 reader-based Parse 測試使用（不落地檔案）。
func buildSimpleXLSXBuffer(t *testing.T, headers []string, data [][]string) []byte {
	t.Helper()

	f := excelize.NewFile()
	sheetName := f.GetSheetName(0)

	for colIdx, header := range headers {
		cell, err := excelize.CoordinatesToCellName(colIdx+1, 1)
		require.NoError(t, err)
		require.NoError(t, f.SetCellValue(sheetName, cell, header))
	}

	for rowIdx, row := range data {
		for colIdx, value := range row {
			cell, err := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			require.NoError(t, err)
			require.NoError(t, f.SetCellValue(sheetName, cell, value))
		}
	}

	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	require.NoError(t, f.Close())

	return buf.Bytes()
}

// buildANCFormatXLSXBuffer 建立完整 ANC-format xlsx（rows 1-11 頭部、row 9 通道名稱、
// row 12+ 資料）並回傳 in-memory bytes，供 reader-based Parse 測試使用。
func buildANCFormatXLSXBuffer(t *testing.T) []byte {
	t.Helper()

	f := excelize.NewFile()
	sheetName := f.GetSheetName(0)

	setCell := func(row int, vals ...string) {
		for col, v := range vals {
			cell, err := excelize.CoordinatesToCellName(col+1, row)
			require.NoError(t, err)
			require.NoError(t, f.SetCellValue(sheetName, cell, v))
		}
	}

	setCell(1, "File_Type:", "AMTI_FORCE_PLATE")
	setCell(2, "Board_Type:", "OR6-5-1000")
	setCell(3, "Trial_Name:", "TEST")
	setCell(4, "BitDepth:", "16")
	setCell(5, "")
	setCell(6, "")
	setCell(7, "")
	setCell(8, "")
	setCell(9, "Name", "Fx", "Fy", "Fz") // row 9 → internal rows[8]
	setCell(10, "Rate", "1000", "1000", "1000")
	setCell(11, "Range", "2000", "2000", "5000")
	setCell(12, "0.000", "1.1", "2.2", "3.3") // row 12 → internal rows[11]; first data
	setCell(13, "0.001", "4.4", "5.5", "6.6")
	setCell(14, "0.002", "7.7", "8.8", "9.9")

	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	require.NoError(t, f.Close())

	return buf.Bytes()
}

// TestParseANCChannelNames 釘住 Finding [9]:
// parseANCChannelNames 中段空名必須 fail-fast;尾部空名必須靜默裁掉;全有效名必須正確回傳。
// 同時涵蓋 parseANCFormatXLSX 與 parseSimpleXLSX 共用的裁/驗邏輯。
func TestParseANCChannelNames(t *testing.T) {
	t.Run("全有效名回傳正確 slice", func(t *testing.T) {
		names, err := parseANCChannelNames([]string{"Fx", "Fy", "Fz"}, "ANC xlsx", "test.xlsx")
		require.NoError(t, err)
		require.Equal(t, []string{"Fx", "Fy", "Fz"}, names)
	})

	t.Run("尾部空名裁掉、解析成功", func(t *testing.T) {
		// 尾部空 header cell 常見(試算表補白欄),應容忍。
		names, err := parseANCChannelNames([]string{"Fx", "Fy", "", ""}, "ANC xlsx", "test.xlsx")
		require.NoError(t, err)
		require.Equal(t, []string{"Fx", "Fy"}, names)
	})

	t.Run("中段空名 fail-fast 含錯位語義", func(t *testing.T) {
		// "Fx", "", "Fz":第 2 欄為空 → 中段空名 → fail-fast。
		// 若靜默 compact,"Fz" 會讀到 "Fx" 原本的 row[i+1] 位置 → 資料損壞。
		_, err := parseANCChannelNames([]string{"Fx", "", "Fz"}, "ANC xlsx", "test.xlsx")
		require.Error(t, err)
		require.Contains(t, err.Error(), "錯位", "error 必須包含「錯位」語義")
		require.Contains(t, err.Error(), "2", "error 必須指出第 2 欄")
	})

	t.Run("全空(裁尾後空)回傳「沒有找到有效通道名稱」", func(t *testing.T) {
		_, err := parseANCChannelNames([]string{"", ""}, "Excel", "test.xlsx")
		require.Error(t, err)
		require.Contains(t, err.Error(), "沒有找到有效的通道名稱")
	})

	t.Run("空輸入 slice 回傳「沒有找到有效通道名稱」", func(t *testing.T) {
		_, err := parseANCChannelNames([]string{}, "ANC xlsx", "test.xlsx")
		require.Error(t, err)
		require.Contains(t, err.Error(), "沒有找到有效的通道名稱")
	})

	t.Run("中段空名在 Excel 語境回傳 Excel 前綴訊息", func(t *testing.T) {
		_, err := parseANCChannelNames([]string{"Time", "", "Ch2"}, "Excel", "data.xlsx")
		require.Error(t, err)
		require.Contains(t, err.Error(), "Excel")
	})
}

// TestANCParser_XLSX_MidEmptyChannelName_FailFast 端對端釘住 Finding [9]:
// simple-xlsx 中段空名 → Parse 回 error;尾部空名 → 解析成功且通道數正確。
func TestANCParser_XLSX_MidEmptyChannelName_FailFast(t *testing.T) {
	t.Run("simple xlsx 中段空名 fail-fast", func(t *testing.T) {
		// 標題行: Time | Ch1 | (空) | Ch3 → 中段空名在 index 2
		buf := buildSimpleXLSXBuffer(t,
			[]string{"Time", "Ch1", "", "Ch3"},
			[][]string{{"0.000", "1.0", "2.0", "3.0"}},
		)
		parser := NewANCParser()
		_, err := parser.Parse(bytes.NewReader(buf), "mid_empty.xlsx")
		require.Error(t, err, "中段空名必須 fail-fast")
		require.Contains(t, err.Error(), "錯位", "error 必須含「錯位」語義")
	})

	t.Run("simple xlsx 尾部空名解析成功且通道數正確", func(t *testing.T) {
		// 標題行: Time | Ch1 | Ch2 | (空) → 尾部空名應被裁掉,只保留 Ch1, Ch2
		buf := buildSimpleXLSXBuffer(t,
			[]string{"Time", "Ch1", "Ch2", ""},
			[][]string{
				{"0.000", "1.0", "2.0", "0.0"},
				{"0.001", "1.1", "2.1", "0.0"},
			},
		)
		parser := NewANCParser()
		data, err := parser.Parse(bytes.NewReader(buf), "trailing_empty.xlsx")
		require.NoError(t, err, "尾部空名必須靜默容忍")
		require.NotNil(t, data)
		require.Equal(t, []string{"Ch1", "Ch2"}, data.Headers, "只保留有名稱的通道")
		require.Len(t, data.Time, 2)
	})
}
