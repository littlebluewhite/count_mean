// Package csv provides CSV data validation functionality.
package csv

import (
	"fmt"
	"strings"

	"count_mean/internal/errors"
)

// Validator provides CSV data validation functionality.
type Validator struct{}

// NewValidator creates a new CSV validator.
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateCSVData 驗證 user-picked 讀取 pipeline 的 CSV 結構與 cell sanity。
//
// 只留結構檢查 + CheckCells（cell ≤ 32KB）+ UTF-8；不再做公式 / script / SQL /
// 命令注入偵測 — 注入防禦只在寫出側（csvutil）。
func (v *Validator) ValidateCSVData(records [][]string, filename string) error {
	// Check for empty data
	if len(records) == 0 {
		return errors.NewValidationError("csv_data", len(records), "CSV 檔案為空")
	}

	// Check for minimum rows (header + at least one data row)
	if len(records) < 2 {
		return errors.NewValidationError("csv_data", len(records),
			"CSV 檔案至少需要包含標題行和一行資料")
	}

	// Validate header row
	if len(records[0]) == 0 {
		return errors.NewValidationError("csv_data", records[0], "CSV 標題行為空")
	}

	// 偵測 duplicate / empty header。下游 parser 用「header → column index」
	// 單射映射，重複 header 會 silently 覆寫 index、空 header 會被 strings.Index
	// 抓到錯誤的 column，導致資料解讀偏移而無 error。validator 必須在第一關擋下。
	if err := validateHeaderUniqueness(records[0]); err != nil {
		return err
	}

	expectedColumns := len(records[0])

	// Check for excessive columns (potential DoS attack)
	if expectedColumns > 1000 {
		return errors.NewValidationError("csv_data", expectedColumns,
			"CSV 欄位數量過多 (最大 1000 欄)")
	}

	// Check for excessive rows (potential DoS attack, max 1 million rows)
	if len(records) > 1000000 { //nolint:mnd // 1000000 is 1 million rows limit
		return errors.NewValidationError("csv_data", len(records),
			"CSV 資料行數過多 (最大 1,000,000 行)")
	}

	if err := CheckCells(records); err != nil {
		return err
	}

	for i, record := range records {
		if err := validateRow(record, i+1, expectedColumns, filename); err != nil {
			return err
		}
	}

	return nil
}

// validateHeaderUniqueness 對 header row 做 duplicate / empty 偵測。
//
// 規則：
//   - Empty / 純空白 cell → reject（下游 strings.Index 找 column 會抓錯）
//   - Trim+ToLower 後重複 → reject（map[string]int 會 silently 覆寫，造成 channel 對應錯）
//
// case-insensitive + trim 取所謂 "normalized key"：CSV 工具（Excel、numbers）對
// header 通常 case-insensitive 比對；user 不小心輸入 "Channel1" 與 "channel1"
// 視為 collision。
func validateHeaderUniqueness(header []string) error {
	seen := make(map[string]int, len(header))
	for i, cell := range header {
		normalized := strings.ToLower(strings.TrimSpace(cell))
		if normalized == "" {
			return errors.NewValidationError("csv_data",
				map[string]any{"column_index": i + 1, "value": cell},
				fmt.Sprintf("CSV 第 %d 欄標題為空或純空白", i+1))
		}
		if prev, exists := seen[normalized]; exists {
			return errors.NewValidationError("csv_data",
				map[string]any{
					"column_index":     i + 1,
					"value":            cell,
					"duplicate_of_col": prev + 1,
					"normalized_value": normalized,
				},
				fmt.Sprintf("CSV 第 %d 欄標題 %q 與第 %d 欄重複（normalize 後相同）",
					i+1, cell, prev+1))
		}
		seen[normalized] = i
	}
	return nil
}

// validateRow 檢查單一 row 的欄位數與（user-picked pipeline 專屬的）UTF-8。
func validateRow(record []string, row, expectedColumns int, filename string) error {
	if len(record) != expectedColumns {
		return errors.NewValidationError("csv_data",
			map[string]any{
				"row":           row,
				"expected_cols": expectedColumns,
				"actual_cols":   len(record),
				"filename":      filename,
			},
			fmt.Sprintf("第 %d 行的欄位數量不一致", row))
	}

	for j, cell := range record {
		if err := checkUTF8(cell, row, j+1, filename); err != nil {
			return err
		}
	}

	return nil
}
