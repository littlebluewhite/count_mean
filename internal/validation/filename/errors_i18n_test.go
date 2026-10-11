package filename

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
)

// TestFilenameErrors_KeyedAndLocalized 釘住 ValidateName / ValidateFilename 的 12 個
// producer(11 個 key;「不能為空」有 TrimSpace 前後兩處)回帶 key 的 i18n.Error
// (取代 errors.ValidationError):
//
//   - errors.As 取得 *i18n.Error,Key 是 error.filename.*
//   - err.Error() 是 zh-TW、與改前 Message 逐位元組相同,與目前 locale 無關;
//     i18n.Localize 在 en-US 下為英文
//   - 只帶觸發規則的片段(非法字元、保留字段、副檔名),不回顯整個檔名
func TestFilenameErrors_KeyedAndLocalized(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale(i18n.LocaleZhTW) })

	v := NewValidator()

	cases := []struct {
		name    string
		build   func() error
		key     string
		zhTW    string
		enUS    string
		notEcho string // 不得出現在 Error() 的檔名片段
	}{
		{
			name:  "空字串",
			build: func() error { return ValidateName("") },
			key:   i18n.KeyErrorFilenameEmpty,
			zhTW:  "檔案名稱不能為空",
			enUS:  "File name cannot be empty",
		},
		{
			name:  "純空白(TrimSpace 後為空)",
			build: func() error { return ValidateName("   ") },
			key:   i18n.KeyErrorFilenameEmpty,
			zhTW:  "檔案名稱不能為空",
			enUS:  "File name cannot be empty",
		},
		{
			name:    "tab",
			build:   func() error { return ValidateName("Jane\tdata.csv") },
			key:     i18n.KeyErrorFilenameTab,
			zhTW:    "檔案名稱不可包含 tab 字元",
			enUS:    "File name must not contain a tab character",
			notEcho: "Jane",
		},
		{
			name:    "路徑分隔符",
			build:   func() error { return ValidateName("Jane/data.csv") },
			key:     i18n.KeyErrorFilenamePathSeparator,
			zhTW:    "檔案名稱不可包含路徑分隔符（/ 或 \\）— 請改用 PathValidator",
			enUS:    "File name must not contain path separators (/ or \\); use PathValidator instead",
			notEcho: "Jane",
		},
		{
			name:  "Windows 磁碟代號",
			build: func() error { return ValidateName("C:Jane.csv") },
			key:   i18n.KeyErrorFilenameDriveLetter,
			zhTW:  "檔案名稱不可以 Windows 磁碟代號開頭 (drive letter, 例 C:foo.csv)",
			enUS:  "File name must not start with a Windows drive letter (e.g. C:foo.csv)",
		},
		{
			name:    "Unicode 格式字元",
			build:   func() error { return ValidateName("Jane" + string(rune(0x202E)) + "vsc.exe") },
			key:     i18n.KeyErrorFilenameFormatChar,
			zhTW:    "檔案名稱包含禁用的 Unicode 格式字元: U+202E",
			enUS:    "File name contains a forbidden Unicode format character: U+202E",
			notEcho: "Jane",
		},
		{
			name:    "控制字元",
			build:   func() error { return ValidateName("Jane\x01.csv") },
			key:     i18n.KeyErrorFilenameControlChar,
			zhTW:    "檔案名稱包含非法控制字符",
			enUS:    "File name contains illegal control characters",
			notEcho: "Jane",
		},
		{
			name:    "非法字元",
			build:   func() error { return ValidateName("Jane<1>.csv") },
			key:     i18n.KeyErrorFilenameIllegalChar,
			zhTW:    "檔案名稱包含非法字符: <",
			enUS:    "File name contains an illegal character: <",
			notEcho: "Jane",
		},
		{
			name:    "保留字(只帶命中的點分段)",
			build:   func() error { return ValidateName("Jane.con.csv") },
			key:     i18n.KeyErrorFilenameReserved,
			zhTW:    "檔案名稱不能使用保留字: CON",
			enUS:    "File name must not use a reserved name: CON",
			notEcho: "Jane",
		},
		{
			name:  "過長",
			build: func() error { return ValidateName(strings.Repeat("a", 256)) },
			key:   i18n.KeyErrorFilenameTooLong,
			zhTW:  "檔案名稱過長 (最大 255 字符)",
			enUS:  "File name is too long (max 255 characters)",
		},
		{
			name:    "缺少副檔名",
			build:   func() error { return v.ValidateFilename("Jane") },
			key:     i18n.KeyErrorFilenameNoExtension,
			zhTW:    "檔案名稱缺少副檔名",
			enUS:    "File name has no extension",
			notEcho: "Jane",
		},
		{
			name:    "不支援的副檔名(只帶副檔名)",
			build:   func() error { return v.ValidateFilename("Jane.txt") },
			key:     i18n.KeyErrorFilenameUnsupportedExt,
			zhTW:    "不支援的檔案副檔名: .txt",
			enUS:    "Unsupported file extension: .txt",
			notEcho: "Jane",
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
				assert.NotContains(t, err.Error(), tc.notEcho, "錯誤不得回顯整個檔名")
			}
		})
	}
}
