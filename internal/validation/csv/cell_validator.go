package csv

import (
	"fmt"
	"unicode/utf8"

	"count_mean/internal/errors"
)

// maxCellBytes 是單一 cell 的長度上限（32KB），防止 DoS。
const maxCellBytes = 32768

// CheckCells 對讀取側所有 pipeline 共用的 cell sanity check：每個 cell ≤ 32KB。
//
// 只檢查長度，不檢查 UTF-8 — manifest（V.16）實際為 Big5 編碼，UTF-8 規則不可
// 進入 manifest / domain pipeline。注入防禦只在寫出側（csvutil 逸出公式起首字元）。
// 錯誤訊息只帶 row/col（1-based），不含檔名或路徑，避免外洩 patient 資料夾名。
func CheckCells(records [][]string) error {
	for i, record := range records {
		for j, cell := range record {
			if len(cell) > maxCellBytes {
				return errors.NewValidationError("csv_cell",
					map[string]any{"row": i + 1, "col": j + 1, "length": len(cell)},
					fmt.Sprintf("第 %d 行第 %d 欄的內容過長 (最大 32KB)", i+1, j+1))
			}
		}
	}

	return nil
}

// checkUTF8 檢查 cell 為合法 UTF-8（僅 user-picked 讀取 pipeline 使用）。
func checkUTF8(cell string, row, col int, filename string) error {
	if !utf8.ValidString(cell) {
		return errors.NewValidationError("csv_cell",
			map[string]any{"row": row, "col": col, "filename": filename},
			fmt.Sprintf("第 %d 行第 %d 欄包含非 UTF-8 字符", row, col))
	}

	return nil
}
