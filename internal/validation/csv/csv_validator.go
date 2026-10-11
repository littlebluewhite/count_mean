// Package csv provides CSV data validation functionality.
package csv

import (
	"strings"

	"count_mean/internal/i18n"
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
//
// 錯誤是帶 key 的 i18n.Error（error.csv.*），caller 層 Localize；Args 只有行號 / 欄號，
// 不回顯 cell 內容，也不收檔名。
func (v *Validator) ValidateCSVData(records [][]string) error {
	// Check for empty data
	if len(records) == 0 {
		return i18n.NewError(i18n.KeyErrorCSVEmpty)
	}

	// Check for minimum rows (header + at least one data row)
	if len(records) < 2 {
		return i18n.NewError(i18n.KeyErrorCSVNoDataRow)
	}

	// Validate header row
	if len(records[0]) == 0 {
		return i18n.NewError(i18n.KeyErrorCSVHeaderEmpty)
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
		return i18n.NewError(i18n.KeyErrorCSVTooManyColumns)
	}

	// Check for excessive rows (potential DoS attack, max 1 million rows)
	if len(records) > 1000000 { //nolint:mnd // 1000000 is 1 million rows limit
		return i18n.NewError(i18n.KeyErrorCSVTooManyRows)
	}

	if err := CheckCells(records); err != nil {
		return err
	}

	for i, record := range records {
		if err := validateRow(record, i+1, expectedColumns); err != nil {
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
// 視為 collision。錯誤只帶欄號，不回顯標題文字。
func validateHeaderUniqueness(header []string) error {
	seen := make(map[string]int, len(header))
	for i, cell := range header {
		normalized := strings.ToLower(strings.TrimSpace(cell))
		if normalized == "" {
			return i18n.NewError(i18n.KeyErrorCSVHeaderCellEmpty, i+1)
		}
		if prev, exists := seen[normalized]; exists {
			return i18n.NewError(i18n.KeyErrorCSVHeaderDuplicate, i+1, prev+1)
		}
		seen[normalized] = i
	}
	return nil
}

// validateRow 檢查單一 row 的欄位數與（user-picked pipeline 專屬的）UTF-8。
func validateRow(record []string, row, expectedColumns int) error {
	if len(record) != expectedColumns {
		return i18n.NewError(i18n.KeyErrorCSVRowColumnMismatch, row)
	}

	for j, cell := range record {
		if err := checkUTF8(cell, row, j+1); err != nil {
			return err
		}
	}

	return nil
}
