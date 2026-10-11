package csv

import (
	"unicode/utf8"

	"count_mean/internal/i18n"
)

// maxCellBytes 是單一 cell 的長度上限（32KB），防止 DoS。
const maxCellBytes = 32768

// CheckCells 對讀取側所有 pipeline 共用的 cell sanity check：每個 cell ≤ 32KB。
//
// 只檢查長度，不檢查 UTF-8 — manifest（V.16）實際為 Big5 編碼，UTF-8 規則不可
// 進入 manifest / domain pipeline。注入防禦只在寫出側（csvutil 逸出公式起首字元）。
// 錯誤是帶 key 的 i18n.Error，Args 只有 row/col（1-based），不含 cell 內容、檔名或路徑，
// 避免外洩 patient 資料夾名。
func CheckCells(records [][]string) error {
	for i, record := range records {
		for j, cell := range record {
			if len(cell) > maxCellBytes {
				return i18n.NewError(i18n.KeyErrorCSVCellTooLong, i+1, j+1)
			}
		}
	}

	return nil
}

// checkUTF8 檢查 cell 為合法 UTF-8（僅 user-picked 讀取 pipeline 使用）。
func checkUTF8(cell string, row, col int) error {
	if !utf8.ValidString(cell) {
		return i18n.NewError(i18n.KeyErrorCSVCellNotUTF8, row, col)
	}

	return nil
}
