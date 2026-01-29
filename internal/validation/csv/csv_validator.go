// Package csv provides CSV data validation functionality.
package csv

import (
	"fmt"

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

	// Validate data consistency and detect malicious content
	for i, record := range records {
		if err := v.validateRow(record, i+1, expectedColumns, filename); err != nil {
			return err
		}
	}

	return nil
}

// validateRow validates a single row of CSV data.
func (v *Validator) validateRow(record []string, row, expectedColumns int, filename string) error {
	if len(record) != expectedColumns {
		return errors.NewValidationError("csv_data",
			map[string]interface{}{
				"row":           row,
				"expected_cols": expectedColumns,
				"actual_cols":   len(record),
				"filename":      filename,
			},
			fmt.Sprintf("第 %d 行的欄位數量不一致", row))
	}

	// Validate each cell
	for j, cell := range record {
		ctx := NewCellContext(row, j+1, filename)
		if err := v.cellValidator.ValidateCell(cell, ctx); err != nil {
			return err
		}
	}

	return nil
}
