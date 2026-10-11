package csv

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
)

// TestCSVValidationErrors_KeyedAndLocalized 釘住 validation/csv 的 10 個 producer 回帶 key
// 的 i18n.Error(取代 errors.ValidationError):
//
//   - errors.As 取得 *i18n.Error,Key 是 error.csv.*
//   - err.Error() 是 zh-TW、與目前 locale 無關;i18n.Localize 在 en-US 下為英文
//   - 訊息只帶行號 / 欄號,不回顯 cell 內容(標題文字、超長 cell)
func TestCSVValidationErrors_KeyedAndLocalized(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale(i18n.LocaleZhTW) })

	v := NewValidator()

	cases := []struct {
		name    string
		build   func() error
		key     string
		zhTW    string
		enUS    string
		notEcho string // 不得出現在 Error() 的原值
	}{
		{
			name:  "空 records",
			build: func() error { return v.ValidateCSVData([][]string{}) },
			key:   i18n.KeyErrorCSVEmpty,
			zhTW:  "CSV 檔案為空",
			enUS:  "CSV file is empty",
		},
		{
			name:  "只有標題行",
			build: func() error { return v.ValidateCSVData([][]string{{"Time", "Ch1"}}) },
			key:   i18n.KeyErrorCSVNoDataRow,
			zhTW:  "CSV 檔案至少需要包含標題行和一行資料",
			enUS:  "CSV file must contain a header row and at least one data row",
		},
		{
			name:  "標題行為空",
			build: func() error { return v.ValidateCSVData([][]string{{}, {"0.1"}}) },
			key:   i18n.KeyErrorCSVHeaderEmpty,
			zhTW:  "CSV 標題行為空",
			enUS:  "CSV header row is empty",
		},
		{
			name:  "標題 cell 純空白",
			build: func() error { return v.ValidateCSVData([][]string{{"Time", "  ", "Ch1"}, {"1", "2", "3"}}) },
			key:   i18n.KeyErrorCSVHeaderCellEmpty,
			zhTW:  "CSV 第 2 欄標題為空或純空白",
			enUS:  "CSV header in column 2 is empty or whitespace-only",
		},
		{
			name: "標題重複(不回顯標題文字)",
			build: func() error {
				return v.ValidateCSVData([][]string{{"Time", "Patient_Jane", " patient_jane"}, {"1", "2", "3"}})
			},
			key:     i18n.KeyErrorCSVHeaderDuplicate,
			zhTW:    "CSV 第 3 欄標題與第 2 欄重複（normalize 後相同）",
			enUS:    "CSV header in column 3 duplicates column 2 (identical after normalisation)",
			notEcho: "Jane",
		},
		{
			name: "欄位數量過多",
			build: func() error {
				header := make([]string, 1001)
				for i := range header {
					header[i] = fmt.Sprintf("C%d", i)
				}
				return v.ValidateCSVData([][]string{header, header})
			},
			key:  i18n.KeyErrorCSVTooManyColumns,
			zhTW: "CSV 欄位數量過多 (最大 1000 欄)",
			enUS: "Too many CSV columns (max 1000)",
		},
		{
			name: "資料行數過多",
			build: func() error {
				row := []string{"1"}
				records := make([][]string, 1000001)
				for i := range records {
					records[i] = row
				}
				records[0] = []string{"Time"}
				return v.ValidateCSVData(records)
			},
			key:  i18n.KeyErrorCSVTooManyRows,
			zhTW: "CSV 資料行數過多 (最大 1,000,000 行)",
			enUS: "Too many CSV rows (max 1,000,000)",
		},
		{
			name:  "欄位數量不一致",
			build: func() error { return v.ValidateCSVData([][]string{{"Time", "Ch1"}, {"1", "2"}, {"3"}}) },
			key:   i18n.KeyErrorCSVRowColumnMismatch,
			zhTW:  "第 3 行的欄位數量不一致",
			enUS:  "Row 3 has an inconsistent number of columns",
		},
		{
			name:    "cell 過長(CheckCells,不回顯內容)",
			build:   func() error { return CheckCells([][]string{{"h"}, {"ok", strings.Repeat("Jane", 8193)}}) },
			key:     i18n.KeyErrorCSVCellTooLong,
			zhTW:    "第 2 行第 2 欄的內容過長 (最大 32KB)",
			enUS:    "Cell at row 2, column 2 is too long (max 32KB)",
			notEcho: "Jane",
		},
		{
			name:  "cell 非 UTF-8",
			build: func() error { return v.ValidateCSVData([][]string{{"Time", "Ch1"}, {"1", "\xa4\xa4"}}) },
			key:   i18n.KeyErrorCSVCellNotUTF8,
			zhTW:  "第 2 行第 2 欄包含非 UTF-8 字符",
			enUS:  "Cell at row 2, column 2 contains non-UTF-8 characters",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build()
			require.Error(t, err)

			var keyed *i18n.Error
			require.True(t, errors.As(err, &keyed), "應為 *i18n.Error,實際 %T", err)
			assert.Equal(t, tc.key, keyed.Key)

			i18n.SetLocale(i18n.LocaleEnUS)
			assert.Equal(t, tc.zhTW, err.Error(), "Error() 固定是 zh-TW")
			assert.Equal(t, tc.enUS, i18n.Localize(err), "Localize 依目前 locale(en-US)")
			i18n.SetLocale(i18n.LocaleZhTW)

			if tc.notEcho != "" {
				assert.NotContains(t, err.Error(), tc.notEcho, "錯誤不得回顯 cell 內容")
			}
		})
	}
}
