package parsers

import (
	"encoding/csv"
	"os"
)

// ReadCSVDirect 直接讀取 CSV 檔案，不進行路徑驗證（用於分期同步分析）
func ReadCSVDirect(filepath string) ([][]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // 允許不同行有不同的欄位數

	return reader.ReadAll()
}
