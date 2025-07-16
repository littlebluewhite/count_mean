package parsers

import (
	"bytes"
	"encoding/csv"
	"io"
	"os"
)

// BOMBytes UTF-8 BOM
var BOMBytes = []byte{0xEF, 0xBB, 0xBF}

// ReadCSVDirect 直接讀取 CSV 檔案，不進行路徑驗證（用於分期同步分析）
func ReadCSVDirect(filepath string) ([][]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// 讀取檔案內容並檢查 BOM
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	// 移除 BOM 字符（如果存在）
	if bytes.HasPrefix(content, BOMBytes) {
		content = content[len(BOMBytes):]
	}

	// 使用處理過的內容建立 CSV reader
	reader := csv.NewReader(bytes.NewReader(content))
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // 允許不同行有不同的欄位數

	return reader.ReadAll()
}
