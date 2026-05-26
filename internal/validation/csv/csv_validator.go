// Package csv provides CSV data validation functionality.
package csv

import (
	"fmt"
	"strings"

	"count_mean/internal/errors"
)

// Validator provides CSV data validation functionality.
type Validator struct {
	cellValidator *CellValidator
}

// NewValidator creates a new CSV validator.
func NewValidator() *Validator {
	return &Validator{
		cellValidator: NewCellValidator(),
	}
}

// ValidateCSVData validates CSV data structure and detects malicious content.
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

	// Validate data consistency and detect malicious content.
	//
	// records[0] 一律當 header row 處理（IsHeader=true），跳過 SQL / Command
	// / DangerousFunctions 比對；其餘 body row 仍跑完整守門。
	for i, record := range records {
		isHeader := i == 0
		if err := v.validateRow(record, i+1, expectedColumns, filename, isHeader); err != nil {
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

// validateRow validates a single row of CSV data.
func (v *Validator) validateRow(record []string, row, expectedColumns int, filename string, isHeader bool) error {
	if isHeader {
		return v.ValidateHeaderRow(record, row, expectedColumns, filename)
	}

	return v.ValidateRow(record, row, expectedColumns, filename)
}

// ValidateRow exposes per-row cell-level validation for streaming callers.
//
// Large-file streaming 路徑（large_file_handler.processStreamingFile）
// 不能把整檔 materialize 進 [][]string 再呼 ValidateCSVData，記憶體會爆炸；改成
// row-by-row 在 executeStreamingLoop 內呼這支 helper，把 cell-level injection /
// DoS 守門延伸進大檔流式分支。expectedColumns < 0 表示「不檢查欄位數」，由 caller
// 自行處理 jagged row 容忍度（large_file_handler 自己有 len(record) != len(headers) 檢查）。
//
// 本 API 對 body row 跑「全部」cell-level 守門；header row 請改呼
// ValidateHeaderRow 以避免 EMG header（含 `Subject ID`、`=Channel1`）被誤判。
func (v *Validator) ValidateRow(record []string, row, expectedColumns int, filename string) error {
	return v.validateRowInternal(record, row, expectedColumns, filename, false)
}

// ValidateHeaderRow exposes per-row cell-level validation specifically for header rows.
//
// streaming 路徑（large_file_handler.processStreamingFile）跑 header row
// 時必須呼此 API 而非 ValidateRow，否則 EMG header 的 `Subject ID` / `Frame ID`
// 等合法欄位名會被 SQL/Command injection substring 比對誤判。formula starter（`=`）
// / script injection / control char / UTF-8 / suspicious extension 仍對 header
// 守門（CellValidator.ValidateCell 內部根據 ctx.IsHeader scoped）。
func (v *Validator) ValidateHeaderRow(record []string, row, expectedColumns int, filename string) error {
	return v.validateRowInternal(record, row, expectedColumns, filename, true)
}

func (v *Validator) validateRowInternal(record []string, row, expectedColumns int,
	filename string, isHeader bool) error {
	if expectedColumns >= 0 && len(record) != expectedColumns {
		return errors.NewValidationError("csv_data",
			map[string]any{
				"row":           row,
				"expected_cols": expectedColumns,
				"actual_cols":   len(record),
				"filename":      filename,
			},
			fmt.Sprintf("第 %d 行的欄位數量不一致", row))
	}

	// Validate each cell
	for j, cell := range record {
		var ctx *CellContext
		if isHeader {
			ctx = NewHeaderCellContext(row, j+1, filename)
		} else {
			ctx = NewCellContext(row, j+1, filename)
		}

		if err := v.cellValidator.ValidateCell(cell, ctx); err != nil {
			return err
		}
	}

	return nil
}
