package parsers

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/csvutil"
	"count_mean/internal/security/fsperm"
	"count_mean/test/testutil"
)

func TestReadCSVDirect(t *testing.T) {
	tests := []struct {
		name        string
		csvContent  string
		wantErr     bool
		expectedLen int
		checkData   func(*testing.T, [][]string)
	}{
		{
			name:        "valid CSV file",
			csvContent:  "Time,Ch1,Ch2\n1.0,100,50\n2.0,200,100\n3.0,300,150",
			wantErr:     false,
			expectedLen: 4,
			checkData: func(t *testing.T, data [][]string) {
				assert.Equal(t, []string{"Time", "Ch1", "Ch2"}, data[0])
				assert.Equal(t, []string{"1.0", "100", "50"}, data[1])
				assert.Equal(t, []string{"2.0", "200", "100"}, data[2])
				assert.Equal(t, []string{"3.0", "300", "150"}, data[3])
			},
		},
		{
			name: "CSV with quotes",
			csvContent: "Name,Description,Value\n" +
				"\"Test Name\",\"A test, with comma\",123\n" +
				"\"Another Name\",\"Simple desc\",456",
			wantErr:     false,
			expectedLen: 3,
			checkData: func(t *testing.T, data [][]string) {
				assert.Equal(t, []string{"Name", "Description", "Value"}, data[0])
				assert.Equal(t, []string{"Test Name", "A test, with comma", "123"}, data[1])
				assert.Equal(t, []string{"Another Name", "Simple desc", "456"}, data[2])
			},
		},
		{
			name:        "CSV with different field counts",
			csvContent:  "A,B,C\n1,2\n3,4,5,6\n7,8,9",
			wantErr:     false,
			expectedLen: 4,
			checkData: func(t *testing.T, data [][]string) {
				assert.Equal(t, []string{"A", "B", "C"}, data[0])
				assert.Equal(t, []string{"1", "2"}, data[1])           // 少欄位
				assert.Equal(t, []string{"3", "4", "5", "6"}, data[2]) // 多欄位
				assert.Equal(t, []string{"7", "8", "9"}, data[3])      // 正常欄位
			},
		},
		{
			name:        "CSV with leading spaces",
			csvContent:  "  A,  B,  C\n  1,  2,  3\n  4,  5,  6",
			wantErr:     false,
			expectedLen: 3,
			checkData: func(t *testing.T, data [][]string) {
				// TrimLeadingSpace = true 應該移除前導空格
				assert.Equal(t, []string{"A", "B", "C"}, data[0])
				assert.Equal(t, []string{"1", "2", "3"}, data[1])
				assert.Equal(t, []string{"4", "5", "6"}, data[2])
			},
		},
		{
			name:        "CSV with lazy quotes",
			csvContent:  "A,B,C\n1,text\"with\"quotes,4\n5,6,7",
			wantErr:     false,
			expectedLen: 3,
			checkData: func(t *testing.T, data [][]string) {
				assert.Equal(t, []string{"A", "B", "C"}, data[0])
				// LazyQuotes = true 允許不規範的引號使用
				assert.Len(t, data[1], 3) // 確保有三個欄位
				assert.Equal(t, "1", data[1][0])
				assert.Contains(t, data[1][1], "text") // 中間欄位包含引號
				assert.Equal(t, "4", data[1][2])
				assert.Equal(t, []string{"5", "6", "7"}, data[2])
			},
		},
		{
			name:        "empty CSV file",
			csvContent:  "",
			wantErr:     false,
			expectedLen: 0,
			checkData: func(t *testing.T, data [][]string) {
				assert.Len(t, data, 0)
			},
		},
		{
			name:        "single line CSV",
			csvContent:  "Header1,Header2,Header3",
			wantErr:     false,
			expectedLen: 1,
			checkData: func(t *testing.T, data [][]string) {
				assert.Equal(t, []string{"Header1", "Header2", "Header3"}, data[0])
			},
		},
		{
			name:        "CSV with empty lines",
			csvContent:  "A,B,C\n\n1,2,3\n\n4,5,6\n",
			wantErr:     false,
			expectedLen: 3, // 標準 CSV 解析器會跳過純空行，只保留有數據的行
			checkData: func(t *testing.T, data [][]string) {
				assert.Equal(t, []string{"A", "B", "C"}, data[0])
				assert.Equal(t, []string{"1", "2", "3"}, data[1])
				assert.Equal(t, []string{"4", "5", "6"}, data[2])
			},
		},
		{
			name:        "CSV with special characters",
			csvContent:  "Name,Data\n測試,123\n特殊字符,αβγ\n符號,!@#$%",
			wantErr:     false,
			expectedLen: 4,
			checkData: func(t *testing.T, data [][]string) {
				assert.Equal(t, []string{"Name", "Data"}, data[0])
				assert.Equal(t, []string{"測試", "123"}, data[1])
				assert.Equal(t, []string{"特殊字符", "αβγ"}, data[2])
				assert.Equal(t, []string{"符號", "!@#$%"}, data[3])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 創建臨時測試文件
			tmpDir := t.TempDir()
			tmpFilePath := filepath.Join(tmpDir, "test_csv.csv")

			err := os.WriteFile(tmpFilePath, []byte(tt.csvContent), fsperm.FilePerm)
			require.NoError(t, err)

			// 測試 ReadCSVDirect
			data, err := ReadCSVDirect(tmpFilePath)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Len(t, data, tt.expectedLen)

			if tt.checkData != nil {
				tt.checkData(t, data)
			}
		})
	}
}

func TestReadCSVDirect_FileNotFound(t *testing.T) {
	_, err := ReadCSVDirect("nonexistent_file.csv")
	// 用 errors.Is 比對 fs.ErrNotExist 避免依賴 OS-specific 訊息文字
	// （Unix: "no such file or directory"，Windows: "The system cannot find the file specified."）
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrNotExist)
}

func TestReadCSVDirect_FilePermissionDenied(t *testing.T) {
	testutil.SkipIfChmodIneffective(t)

	// 創建測試文件
	tmpDir := t.TempDir()
	tmpFilePath := filepath.Join(tmpDir, "test_permission.csv")

	err := os.WriteFile(tmpFilePath, []byte("test,data\n1,2"), fsperm.FilePerm)
	require.NoError(t, err)

	// 移除讀取權限
	err = os.Chmod(tmpFilePath, 0o000)
	require.NoError(t, err)

	defer func() {
		_ = os.Chmod(tmpFilePath, fsperm.FilePerm) // 恢復權限以便清理
	}()

	// require.Error 在 err == nil 時直接 FailNow，根本性消除「err.Error() 對 nil 呼叫」
	// 的 panic 機制；errors.Is 比對 fs.ErrPermission 跨平台等價。
	_, err = ReadCSVDirect(tmpFilePath)
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrPermission)
}

func TestReadCSVDirect_InvalidDirectory(t *testing.T) {
	// 測試嘗試讀取目錄而不是文件
	tmpDir := t.TempDir()
	_, err := ReadCSVDirect(tmpDir)
	assert.Error(t, err)
}

func TestReadCSVDirect_BinaryFile(t *testing.T) {
	// 創建二進制文件
	tmpDir := t.TempDir()
	tmpFilePath := filepath.Join(tmpDir, "test_binary.csv")

	// 寫入二進制數據
	binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
	err := os.WriteFile(tmpFilePath, binaryData, fsperm.FilePerm)
	require.NoError(t, err)

	// 二進制文件應該仍能讀取，但可能產生意外結果
	data, err := ReadCSVDirect(tmpFilePath)
	// CSV 讀取器通常不會出錯，但數據可能不符合預期
	assert.NoError(t, err)
	assert.NotNil(t, data)
}

func TestReadCSVDirect_LargeFile(t *testing.T) {
	// 創建較大的 CSV 文件
	tmpDir := t.TempDir()
	tmpFilePath := filepath.Join(tmpDir, "test_large.csv")

	tmpFile, err := os.Create(tmpFilePath)
	require.NoError(t, err)

	// 寫入標題
	_, err = tmpFile.WriteString("Time,Channel1,Channel2,Channel3\n")
	require.NoError(t, err)

	// 寫入 1000 行數據
	const numRows = 1000
	for i := 0; i < numRows; i++ {
		line := fmt.Sprintf("%.3f,%d,%d,%d\n", float64(i)*0.001, i, i*2, i*3)
		_, err = tmpFile.WriteString(line)
		require.NoError(t, err)
	}

	require.NoError(t, tmpFile.Close())

	// 測試讀取大文件
	data, err := ReadCSVDirect(tmpFilePath)
	assert.NoError(t, err)
	assert.Len(t, data, numRows+1) // 標題 + 1000 行數據

	// 檢查標題
	assert.Equal(t, []string{"Time", "Channel1", "Channel2", "Channel3"}, data[0])

	// 檢查第一行數據
	assert.Equal(t, []string{"0.000", "0", "0", "0"}, data[1])

	// 檢查最後一行數據
	assert.Equal(t, []string{"0.999", "999", "1998", "2997"}, data[numRows])
}

func TestReadCSVDirect_UTF8WithBOM(t *testing.T) {
	// 創建包含 UTF-8 BOM 的文件
	tmpDir := t.TempDir()
	tmpFilePath := filepath.Join(tmpDir, "test_bom.csv")

	// UTF-8 BOM + CSV 內容
	bomBytes := csvutil.BOMBytes()
	csvContent := "Name,Value\n測試,123\n"

	file, err := os.OpenFile(tmpFilePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fsperm.FilePerm)
	require.NoError(t, err)

	_, err = file.Write(bomBytes)
	require.NoError(t, err)
	_, err = file.WriteString(csvContent)
	require.NoError(t, err)
	require.NoError(t, file.Close())

	// 測試讀取包含 BOM 的文件
	data, err := ReadCSVDirect(tmpFilePath)
	assert.NoError(t, err)
	assert.Len(t, data, 2)

	// 第一個欄位可能包含 BOM 字符
	assert.Contains(t, data[0][0], "Name") // 可能是 "\uFEFFName" 或 "Name"
	assert.Equal(t, "Value", data[0][1])
	assert.Equal(t, []string{"測試", "123"}, data[1])
}

// TestReadCSVRecords_FromReader 釘住 reader-based 核心：feed io.Reader 給
// ReadCSVRecords 必須得到與 ReadCSVDirect（path-based wrapper）相同的結果，
// 含 csv.Reader 設定（TrimLeadingSpace / LazyQuotes / FieldsPerRecord=-1）。
func TestReadCSVRecords_FromReader(t *testing.T) {
	csvContent := "Time,Ch1,Ch2\n1.0,100,50\n2.0,200,100\n3.0,300,150"

	records, err := ReadCSVRecords(strings.NewReader(csvContent))
	require.NoError(t, err)
	require.Len(t, records, 4)
	assert.Equal(t, []string{"Time", "Ch1", "Ch2"}, records[0])
	assert.Equal(t, []string{"1.0", "100", "50"}, records[1])
	assert.Equal(t, []string{"3.0", "300", "150"}, records[3])
}

// TestReadCSVRecords_FromReaderWithBOM pins that the reader-based core keeps the
// load-bearing PeekBOM step — a BOM-prefixed payload must be consumed so the
// first field is not polluted with U+FEFF.
func TestReadCSVRecords_FromReaderWithBOM(t *testing.T) {
	payload := append([]byte{}, csvutil.BOMBytes()...)
	payload = append(payload, []byte("Name,Value\n測試,123\n")...)

	records, err := ReadCSVRecords(bytes.NewReader(payload))
	require.NoError(t, err)
	require.Len(t, records, 2)
	// BOM 已被 PeekBOM 消化，第一欄不應殘留 U+FEFF。
	assert.Equal(t, "Name", records[0][0], "BOM must be stripped, first field should be clean")
	assert.Equal(t, "Value", records[0][1])
	assert.Equal(t, []string{"測試", "123"}, records[1])
}

// TestReadCSVRecords_MatchesReadCSVDirect 確認 wrapper 與 reader 核心對同一輸入
// 產生 byte-for-byte 相同的 records（wrapper 只是「開檔 + delegate」不改語意）。
func TestReadCSVRecords_MatchesReadCSVDirect(t *testing.T) {
	csvContent := "A,B,C\n1,2\n3,4,5,6\n  7,  8,  9"

	tmpDir := t.TempDir()
	tmpFilePath := filepath.Join(tmpDir, "match.csv")
	require.NoError(t, os.WriteFile(tmpFilePath, []byte(csvContent), fsperm.FilePerm))

	fromPath, err := ReadCSVDirect(tmpFilePath)
	require.NoError(t, err)

	fromReader, err := ReadCSVRecords(strings.NewReader(csvContent))
	require.NoError(t, err)

	assert.Equal(t, fromPath, fromReader,
		"reader-based core and path-based wrapper must produce identical records")
}

// TestReadCSVRecords_ReaderError 釘住 reader-boundary 失敗面：當底層 io.Reader 在
// 串流途中報錯（iotest.ErrReader），ReadCSVRecords 必須把錯誤往上傳（不得 panic、
// 不得回傳「err==nil 但 records 看似有效」的偽結果）。BOM peek 是第一個讀取點，
// 因此 reader 的錯誤在 "peek BOM" 階段被包裝傳出。
func TestReadCSVRecords_ReaderError(t *testing.T) {
	records, err := ReadCSVRecords(iotest.ErrReader(errors.New("boom")))

	require.Error(t, err)
	require.ErrorContains(t, err, "boom", "底層 reader 的錯誤必須往上傳遞")
	assert.Nil(t, records, "reader 出錯時不得回傳偽造的非 nil records")
}

// TestReadCSVRecords_EmptyMatchesEmptyFile 把「reader 空輸入」與「空檔案經 path
// wrapper」綁在一起：不硬編碼空輸入是否算錯誤，只斷言兩條路徑的 error-ness 與
// 結果一致（reader empty 必須與 file empty 行為相同）。
func TestReadCSVRecords_EmptyMatchesEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	emptyPath := filepath.Join(tmpDir, "empty.csv")
	require.NoError(t, os.WriteFile(emptyPath, []byte(""), fsperm.FilePerm))

	fromFile, fileErr := ReadCSVDirect(emptyPath)
	fromReader, readerErr := ReadCSVRecords(strings.NewReader(""))

	assert.Equal(t, fileErr == nil, readerErr == nil,
		"reader 空輸入與空檔案必須有相同的 error-ness")
	assert.Equal(t, fromFile, fromReader,
		"reader 空輸入與空檔案必須產生相同的 records")
}

func TestReadCSVDirect_RelativeAndAbsolutePaths(t *testing.T) {
	// 創建測試文件
	tmpDir := t.TempDir()
	tmpFilePath := filepath.Join(tmpDir, "test_path.csv")

	csvContent := "A,B\n1,2\n"
	err := os.WriteFile(tmpFilePath, []byte(csvContent), fsperm.FilePerm)
	require.NoError(t, err)

	// 測試絕對路徑
	t.Run("absolute path", func(t *testing.T) {
		absolutePath, err := filepath.Abs(tmpFilePath)
		require.NoError(t, err)

		data, err := ReadCSVDirect(absolutePath)
		assert.NoError(t, err)
		assert.Len(t, data, 2)
		assert.Equal(t, []string{"A", "B"}, data[0])
	})

	// 測試相對路徑
	t.Run("relative path", func(t *testing.T) {
		// 改變工作目錄到臨時目錄
		t.Chdir(tmpDir)

		relativePath := filepath.Base(tmpFilePath)
		data, err := ReadCSVDirect(relativePath)
		assert.NoError(t, err)
		assert.Len(t, data, 2)
		assert.Equal(t, []string{"A", "B"}, data[0])
	})
}
