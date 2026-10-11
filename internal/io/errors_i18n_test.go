package io

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
	"count_mean/internal/security/fsperm"
)

// TestReadCSVErrors_KeyedAndLocalized 釘住 ReadCSV 的錯誤是帶 key 的 i18n.Error
// (取代 errors.AppError,失去 [CODE] 與「詳細」外框):
//
//   - errors.As 取得的第一個 *i18n.Error 是 error.io.*;內層 validation 錯誤也是帶 key 的,
//     整條鏈在 en-US 下全英文
//   - err.Error() 是 zh-TW、與目前 locale 無關;i18n.Localize 在 en-US 下為英文
//   - 非一般檔案、過大不帶路徑;fsperm sentinel 不當 Cause(文字不重複)
//   - 開檔錯誤保留原 error 當 Cause,errors.Is(os.ErrNotExist) 照常成立
func TestReadCSVErrors_KeyedAndLocalized(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale(i18n.LocaleZhTW) })

	dir := t.TempDir()
	handler := newReadCSVHandler(dir)

	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), fsperm.FilePerm))
		return p
	}

	const mb = int64(1024 * 1024)

	huge := filepath.Join(dir, "huge.csv")
	f, err := os.Create(huge) //nolint:gosec // huge 位於 t.TempDir()
	require.NoError(t, err)
	require.NoError(t, f.Truncate(101*mb))
	require.NoError(t, f.Close())

	folder := filepath.Join(dir, "folder.csv")
	require.NoError(t, os.Mkdir(folder, fsperm.DirPerm))

	cases := []struct {
		name  string
		path  string
		key   string
		zhTW  string
		enUS  string
		cause bool // 訊息後接 ": " + Cause 的原文(OS / encoding/csv 的英文)
	}{
		{
			name: "檔名驗證(內層 error.filename.*)",
			path: filepath.Join(dir, "Jane.CON.csv"),
			key:  i18n.KeyErrorIOFilenameInvalid,
			zhTW: "檔案名稱驗證失敗: 檔案名稱不能使用保留字: CON",
			enUS: "File name validation failed: File name must not use a reserved name: CON",
		},
		{
			name: "非一般檔案(不帶路徑)",
			path: folder,
			key:  i18n.KeyErrorIONotRegularFile,
			zhTW: "不是一般檔案",
			enUS: "Not a regular file",
		},
		{
			name: "過大(只帶 MB 數)",
			path: huge,
			key:  i18n.KeyErrorIOFileTooLarge,
			zhTW: "檔案過大（101 MB，上限 100 MB），請分割檔案後再試",
			enUS: "File too large (101 MB, limit 100 MB); please split the file and try again",
		},
		{
			name:  "不存在",
			path:  filepath.Join(dir, "missing.csv"),
			key:   i18n.KeyErrorIOFileNotFound,
			zhTW:  "無法獲取文件信息",
			enUS:  "Unable to get file information",
			cause: true,
		},
		{
			name:  "CSV 語法錯誤",
			path:  write("malformed.csv", "Time,Ch1\n1.0,\"unclosed\n2.0,100\n"),
			key:   i18n.KeyErrorIOReadCSVDataFailed,
			zhTW:  "無法讀取 CSV 資料",
			enUS:  "Unable to read CSV data",
			cause: true,
		},
		{
			name: "只有標題行",
			path: write("header_only.csv", "Time,Ch1\n"),
			key:  i18n.KeyErrorIOInsufficientRows,
			zhTW: "資料不足，檔案至少需要包含標題行和一行數據",
			enUS: "Insufficient data: the file must contain a header row and at least one data row",
		},
		{
			name: "結構驗證(內層 error.csv.*)",
			path: write("dup.csv", "Time,Ch1,ch1\n1,2,3\n"),
			key:  i18n.KeyErrorIOCSVValidationFailed,
			zhTW: "CSV 資料驗證失敗: CSV 第 3 欄標題與第 2 欄重複（normalize 後相同）",
			enUS: "CSV data validation failed: CSV header in column 3 duplicates column 2 (identical after normalisation)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records, err := handler.ReadCSV(tc.path)
			require.Nil(t, records)
			require.Error(t, err)

			var keyed *i18n.Error
			require.True(t, errors.As(err, &keyed), "應為 *i18n.Error,實際 %T", err)
			assert.Equal(t, tc.key, keyed.Key)

			zhTW, enUS := tc.zhTW, tc.enUS
			if tc.cause {
				require.NotNil(t, keyed.Cause)
				zhTW += ": " + keyed.Cause.Error()
				enUS += ": " + keyed.Cause.Error()
			}

			i18n.SetLocale(i18n.LocaleEnUS)
			assert.Equal(t, zhTW, err.Error(), "Error() 固定是 zh-TW")
			assert.Equal(t, enUS, i18n.Localize(err), "Localize 依目前 locale(en-US)")
			i18n.SetLocale(i18n.LocaleZhTW)
		})
	}

	t.Run("不存在仍可 errors.Is(os.ErrNotExist)", func(t *testing.T) {
		_, err := handler.ReadCSV(filepath.Join(dir, "missing.csv"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("路徑驗證(內層 security 訊息不在範圍)", func(t *testing.T) {
		_, err := handler.ReadCSV("../../../etc/passwd.csv")
		require.Error(t, err)

		var keyed *i18n.Error
		require.True(t, errors.As(err, &keyed), "應為 *i18n.Error,實際 %T", err)
		assert.Equal(t, i18n.KeyErrorIOPathInvalid, keyed.Key)

		i18n.SetLocale(i18n.LocaleEnUS)
		assert.True(t, strings.HasPrefix(err.Error(), "路徑驗證失敗: "), "got %q", err.Error())
		assert.True(t, strings.HasPrefix(i18n.Localize(err), "Path validation failed: "), "got %q", i18n.Localize(err))
		i18n.SetLocale(i18n.LocaleZhTW)
	})
}

// TestOpenFailure_KeyedWithCause 釘住 OpenRegularLimited 的其他開檔錯誤(權限等)包成
// error.io.open_failed,原 error 當 Cause。這條在 ReadCSV 上難以跨平台觸發,直接測 openFailure。
func TestOpenFailure_KeyedWithCause(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale(i18n.LocaleZhTW) })

	cause := os.ErrPermission
	err := openFailure(cause)

	var keyed *i18n.Error
	require.True(t, errors.As(err, &keyed), "應為 *i18n.Error,實際 %T", err)
	assert.Equal(t, i18n.KeyErrorIOOpenFailed, keyed.Key)
	assert.ErrorIs(t, err, os.ErrPermission)

	i18n.SetLocale(i18n.LocaleEnUS)
	assert.Equal(t, "無法開啟檔案: permission denied", err.Error())
	assert.Equal(t, "Unable to open file: permission denied", i18n.Localize(err))
}
