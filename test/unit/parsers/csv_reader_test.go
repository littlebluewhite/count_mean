package parsers

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/parsers"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			name:        "CSV with quotes",
			csvContent:  "Name,Description,Value\n\"Test Name\",\"A test, with comma\",123\n\"Another Name\",\"Simple desc\",456",
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
			tmpFile, err := os.CreateTemp("", "test_csv_*.csv")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())

			_, err = tmpFile.WriteString(tt.csvContent)
			require.NoError(t, err)
			tmpFile.Close()

			// 測試 ReadCSVDirect
			data, err := parsers.ReadCSVDirect(tmpFile.Name())

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
	_, err := parsers.ReadCSVDirect("nonexistent_file.csv")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no such file or directory")
}

func TestReadCSVDirect_FilePermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("跳過權限測試，因為正在以 root 用戶運行")
	}

	// 創建測試文件
	tmpFile, err := os.CreateTemp("", "test_permission_*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.WriteString("test,data\n1,2")
	require.NoError(t, err)
	tmpFile.Close()

	// 移除讀取權限
	err = os.Chmod(tmpFile.Name(), 0000)
	require.NoError(t, err)
	defer os.Chmod(tmpFile.Name(), 0644) // 恢復權限以便清理

	// 測試權限被拒絕的情況
	_, err = parsers.ReadCSVDirect(tmpFile.Name())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

func TestReadCSVDirect_InvalidDirectory(t *testing.T) {
	// 測試嘗試讀取目錄而不是文件
	tmpDir := t.TempDir()
	_, err := parsers.ReadCSVDirect(tmpDir)
	assert.Error(t, err)
}

func TestReadCSVDirect_BinaryFile(t *testing.T) {
	// 創建二進制文件
	tmpFile, err := os.CreateTemp("", "test_binary_*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// 寫入二進制數據
	binaryData := []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}
	err = os.WriteFile(tmpFile.Name(), binaryData, 0644)
	require.NoError(t, err)

	// 二進制文件應該仍能讀取，但可能產生意外結果
	data, err := parsers.ReadCSVDirect(tmpFile.Name())
	// CSV 讀取器通常不會出錯，但數據可能不符合預期
	assert.NoError(t, err)
	assert.NotNil(t, data)
}

func TestReadCSVDirect_LargeFile(t *testing.T) {
	// 創建較大的 CSV 文件
	tmpFile, err := os.CreateTemp("", "test_large_*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// 寫入標題
	_, err = tmpFile.WriteString("Time,Channel1,Channel2,Channel3\n")
	require.NoError(t, err)

	// 寫入 1000 行數據
	for i := 0; i < 1000; i++ {
		line := fmt.Sprintf("%.3f,%d,%d,%d\n", float64(i)*0.001, i, i*2, i*3)
		_, err = tmpFile.WriteString(line)
		require.NoError(t, err)
	}
	tmpFile.Close()

	// 測試讀取大文件
	data, err := parsers.ReadCSVDirect(tmpFile.Name())
	assert.NoError(t, err)
	assert.Len(t, data, 1001) // 標題 + 1000 行數據

	// 檢查標題
	assert.Equal(t, []string{"Time", "Channel1", "Channel2", "Channel3"}, data[0])

	// 檢查第一行數據
	assert.Equal(t, []string{"0.000", "0", "0", "0"}, data[1])

	// 檢查最後一行數據
	assert.Equal(t, []string{"0.999", "999", "1998", "2997"}, data[1000])
}

func TestReadCSVDirect_UTF8WithBOM(t *testing.T) {
	// 創建包含 UTF-8 BOM 的文件
	tmpFile, err := os.CreateTemp("", "test_bom_*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// UTF-8 BOM + CSV 內容
	bomBytes := []byte{0xEF, 0xBB, 0xBF}
	csvContent := "Name,Value\n測試,123\n"

	file, err := os.OpenFile(tmpFile.Name(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	require.NoError(t, err)

	_, err = file.Write(bomBytes)
	require.NoError(t, err)
	_, err = file.WriteString(csvContent)
	require.NoError(t, err)
	file.Close()

	// 測試讀取包含 BOM 的文件
	data, err := parsers.ReadCSVDirect(tmpFile.Name())
	assert.NoError(t, err)
	assert.Len(t, data, 2)

	// 第一個欄位可能包含 BOM 字符
	assert.Contains(t, data[0][0], "Name") // 可能是 "\uFEFFName" 或 "Name"
	assert.Equal(t, "Value", data[0][1])
	assert.Equal(t, []string{"測試", "123"}, data[1])
}

func TestReadCSVDirect_RelativeAndAbsolutePaths(t *testing.T) {
	// 創建測試文件
	tmpFile, err := os.CreateTemp("", "test_path_*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	csvContent := "A,B\n1,2\n"
	err = os.WriteFile(tmpFile.Name(), []byte(csvContent), 0644)
	require.NoError(t, err)

	// 測試絕對路徑
	t.Run("absolute path", func(t *testing.T) {
		absolutePath, err := filepath.Abs(tmpFile.Name())
		require.NoError(t, err)

		data, err := parsers.ReadCSVDirect(absolutePath)
		assert.NoError(t, err)
		assert.Len(t, data, 2)
		assert.Equal(t, []string{"A", "B"}, data[0])
	})

	// 測試相對路徑
	t.Run("relative path", func(t *testing.T) {
		// 改變工作目錄到臨時目錄
		originalDir, err := os.Getwd()
		require.NoError(t, err)
		defer os.Chdir(originalDir)

		tmpDir := filepath.Dir(tmpFile.Name())
		err = os.Chdir(tmpDir)
		require.NoError(t, err)

		relativePath := filepath.Base(tmpFile.Name())
		data, err := parsers.ReadCSVDirect(relativePath)
		assert.NoError(t, err)
		assert.Len(t, data, 2)
		assert.Equal(t, []string{"A", "B"}, data[0])
	})
}
