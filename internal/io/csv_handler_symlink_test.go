//go:build !windows

package io

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/security/fsperm"
)

// leafSymlinkHandler 建立 OutputDir = allowedDir 的 handler,並在 allowedDir/output.csv
// 植入指向 target 的 leaf symlink,回傳 handler 與連結路徑。
func leafSymlinkHandler(t *testing.T, allowedDir, target string) (*CSVHandler, string) {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.InputDir = allowedDir
	cfg.OutputDir = allowedDir
	cfg.OperateDir = allowedDir

	symlinkPath := filepath.Join(allowedDir, "output.csv")
	require.NoError(t, os.Symlink(target, symlinkPath))

	return NewCSVHandler(cfg), symlinkPath
}

// requireReplacedByRegularFile 斷言 path 已不是 symlink,而是寫入新內容的一般檔。
func requireReplacedByRegularFile(t *testing.T, path string) {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.True(t, info.Mode().IsRegular(), "leaf symlink 應被 rename 取代成一般檔,mode=%v", info.Mode())

	got, err := os.ReadFile(path) //nolint:gosec // t.TempDir 內的測試檔
	require.NoError(t, err)
	require.Contains(t, string(got), "1.0,100")
}

// TestCSVHandler_LeafSymlinkEscapingBaseIsReplaced 釘住 leaf symlink 的語意:
// 輸出位置上的 symlink 指向 base 之外時,atomic 寫入(tmp → rename)取代「連結本身」,
// 不跟到目標 —— victim 不動,連結變成一般檔。
//
// 先前 File-based 寫檔直接 open 目標路徑,resolve 後比對 base 才拒寫;改走 placement
// (fsperm.AtomicWriteFile)後 rename 不跟 leaf symlink,改為取代。parent component 是
// symlink 的攻擊面仍然拒寫,見 TestCSVHandler_ParentSymlinkRejected。
func TestCSVHandler_LeafSymlinkEscapingBaseIsReplaced(t *testing.T) {
	t.Parallel()

	allowedDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	// outsideDir 內的 victim,模擬「攻擊者要被覆寫的敏感檔」
	victim := filepath.Join(outsideDir, "victim.csv")
	require.NoError(t, os.WriteFile(victim, []byte("untouched\n"), fsperm.FilePerm))

	// allowed/output.csv → outsideDir/victim.csv,leaf 為 symlink 指向 base 外
	handler, symlinkPath := leafSymlinkHandler(t, allowedDir, victim)

	data := [][]string{{"Time", "Ch1"}, {"1.0", "100"}}
	got, err := handler.WriteFileCSV(WriteRequest{Filename: "output.csv"}, data)
	require.NoError(t, err)
	require.Equal(t, symlinkPath, got)

	victimContent, err := os.ReadFile(victim) //nolint:gosec // t.TempDir 內的測試檔
	require.NoError(t, err)
	require.Equal(t, "untouched\n", string(victimContent), "victim 不該透過 leaf-symlink 被覆寫")

	requireReplacedByRegularFile(t, symlinkPath)
}

// TestCSVHandler_LeafSymlinkWithinBaseIsReplaced 釘住同一語意的 base 內版本:
// symlink 指向 base 內的目標時一樣取代連結本身,目標不被覆寫。
func TestCSVHandler_LeafSymlinkWithinBaseIsReplaced(t *testing.T) {
	t.Parallel()

	allowedDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	// allowed/real.csv 是真實檔,allowed/output.csv → real.csv
	real := filepath.Join(allowedDir, "real.csv")
	require.NoError(t, os.WriteFile(real, []byte("placeholder"), fsperm.FilePerm))

	handler, symlinkPath := leafSymlinkHandler(t, allowedDir, real)

	data := [][]string{{"Time", "Ch1"}, {"1.0", "100"}}
	_, err = handler.WriteFileCSV(WriteRequest{Filename: "output.csv"}, data)
	require.NoError(t, err)

	got, err := os.ReadFile(real) //nolint:gosec // t.TempDir 內的測試檔
	require.NoError(t, err)
	require.Equal(t, "placeholder", string(got), "real.csv 不該透過 symlink 被覆寫")

	requireReplacedByRegularFile(t, symlinkPath)
}
