package io

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/security/fsperm"
)

// newReadCSVHandler 建立 InputDir=OutputDir=OperateDir=inDir 的 handler。
func newReadCSVHandler(inDir string) *CSVHandler {
	cfg := config.DefaultConfig()
	cfg.InputDir = inDir
	cfg.OutputDir = inDir
	cfg.OperateDir = inDir

	return NewCSVHandler(cfg)
}

// TestReadCSV_LiteralPercentFilename 釘住:檔名含字面 `%`(BTS 匯出 `SF_8_BTS%_*.csv`、
// `report 50%.csv`)必須原樣讀取,不論檔案在 InputDir 內或外。
// 先前 InputDir 內的檔走 strict 路徑,殘留 `%` 被當成 URL-encoding 攻擊而誤拒。
func TestReadCSV_LiteralPercentFilename(t *testing.T) {
	t.Parallel()

	inDir := t.TempDir()
	handler := newReadCSVHandler(inDir)

	for _, name := range []string{"SF_8_BTS%_6.10.csv", "report 50%.csv"} {
		for where, dir := range map[string]string{"inside": inDir, "outside": t.TempDir()} {
			t.Run(where+"/"+name, func(t *testing.T) {
				t.Parallel()
				p := filepath.Join(dir, name)
				require.NoError(t, os.WriteFile(p, []byte("Time,Ch1\n1,2\n"), fsperm.FilePerm))

				records, err := handler.ReadCSV(p)
				require.NoError(t, err)
				require.Equal(t, [][]string{{"Time", "Ch1"}, {"1", "2"}}, records)
			})
		}
	}
}

// TestReadCSV_PlusFilenameReadsExactFile 釘住:`+` 不被 URL-decode 成空格。
// `a b.csv`(decoy)與 `a+b.csv` 並存時,讀 `a+b.csv` 必須拿到它自己的內容。
func TestReadCSV_PlusFilenameReadsExactFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	handler := newReadCSVHandler(dir)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a b.csv"), []byte("H\ndecoy\n"), fsperm.FilePerm))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a+b.csv"), []byte("H\nexact\n"), fsperm.FilePerm))

	records, err := handler.ReadCSV(filepath.Join(dir, "a+b.csv"))
	require.NoError(t, err)
	require.Equal(t, [][]string{{"H"}, {"exact"}}, records)
}

// TestReadCSV_OverLimitMessage 釘住:超過 100MB 回 key error.io.file_too_large,
// 訊息帶實際 MB 數(無條件進位,至少 101)與上限,不帶路徑。用 sparse file,不佔磁碟。
func TestReadCSV_OverLimitMessage(t *testing.T) {
	t.Parallel()

	const mb = int64(1024 * 1024)

	for name, tc := range map[string]struct {
		size int64
		want string
	}{
		"101MB":         {101 * mb, "檔案過大（101 MB，上限 100 MB），請分割檔案後再試"},
		"limit_plus_1B": {100*mb + 1, "檔案過大（101 MB，上限 100 MB），請分割檔案後再試"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			handler := newReadCSVHandler(dir)

			p := filepath.Join(dir, "huge.csv")
			f, err := os.Create(p) //nolint:gosec // p 位於 t.TempDir()
			require.NoError(t, err)
			require.NoError(t, f.Truncate(tc.size))
			require.NoError(t, f.Close())

			_, err = handler.ReadCSV(p)
			require.Error(t, err)

			var keyed *i18n.Error
			require.True(t, errors.As(err, &keyed), "應為 *i18n.Error,實際 %T", err)
			require.Equal(t, i18n.KeyErrorIOFileTooLarge, keyed.Key)
			require.Equal(t, tc.want, err.Error())
			require.NotContains(t, err.Error(), dir, "過大訊息不得帶路徑")
		})
	}
}

// TestReadCSV_RejectsNonRegular 釘住:目錄(即使名稱以 .csv 結尾)不是 regular file,必須拒絕。
func TestReadCSV_RejectsNonRegular(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	handler := newReadCSVHandler(dir)

	p := filepath.Join(dir, "folder.csv")
	require.NoError(t, os.Mkdir(p, fsperm.DirPerm))

	records, err := handler.ReadCSV(p)
	require.Nil(t, records)

	var keyed *i18n.Error
	require.True(t, errors.As(err, &keyed), "應為 *i18n.Error,實際 %T", err)
	require.Equal(t, i18n.KeyErrorIONotRegularFile, keyed.Key)
	require.NotContains(t, err.Error(), dir, "非一般檔案訊息不得帶路徑")
}
