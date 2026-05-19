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

// TestCSVHandler_WriteRejectsSymlinkEscapingBase 釘住 新政策:
// symlink 指向 base 之外的目標必須被拒絕。
//
// 原本 (之前) WriteCSV 依賴 fsperm.WriteFlags 的 O_NOFOLLOW 擋「leaf
// component 是 symlink」的 case;改走 OpenWriteValidated,內部 EvalSymlinks
// 解析後比對 allowed base paths,目標在 base 外才 reject — 不再因為「是 symlink」
// 而拒絕,而是因為「resolved target 落在 base 外」。
//
// 與 TestCSVHandler_ParentSymlinkRejected 互補:parent component 為 symlink 與
// leaf 為 symlink 兩種攻擊面都涵蓋。
func TestCSVHandler_WriteRejectsSymlinkEscapingBase(t *testing.T) {
	t.Parallel()

	allowedDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	outsideDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	cfg.InputDir = allowedDir
	cfg.OutputDir = allowedDir
	cfg.OperateDir = allowedDir
	handler := NewCSVHandler(cfg)

	// outsideDir 內的 victim,模擬「攻擊者要被覆寫的敏感檔」
	victim := filepath.Join(outsideDir, "victim.csv")
	require.NoError(t, os.WriteFile(victim, []byte("untouched\n"), fsperm.FilePerm))

	// allowed/output.csv → outsideDir/victim.csv,leaf 為 symlink 指向 base 外
	symlinkPath := filepath.Join(allowedDir, "output.csv")
	require.NoError(t, os.Symlink(victim, symlinkPath))

	// WriteCSV 必須 reject — resolved target 在 base 外
	data := [][]string{{"Time", "Ch1"}, {"1.0", "100"}}
	err = handler.WriteCSV(symlinkPath, data)
	require.Error(t, err, "leaf-symlink 指向 base 外的目標必須 reject")

	// victim 不該被覆寫
	got, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "untouched\n", string(got),
		"victim 不該透過 leaf-symlink 被覆寫")
}

// TestCSVHandler_WriteAllowsSymlinkWithinBase 釘住 補強政策的另一面:
// symlink 指向 base 內的目標,WriteCSV 必須放行 (resolved target 仍在 base 內)。
//
// 這與 fsperm.OpenWriteValidated 的 TestOpenWriteValidated_AcceptsInternalSymlink
// 對齊 — base 內部 symlink (e.g. 使用者在 OutputDir 內建立的 sub → real-sub) 是
// 合法 use case,不應一律拒絕。先前 WriteCSV 僅靠 O_NOFOLLOW 會把 leaf 為 symlink
// 的合法 case 也擋下,改成 boundary-based 政策後可正確區分。
func TestCSVHandler_WriteAllowsSymlinkWithinBase(t *testing.T) {
	t.Parallel()

	allowedDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	cfg := config.DefaultConfig()
	cfg.InputDir = allowedDir
	cfg.OutputDir = allowedDir
	cfg.OperateDir = allowedDir
	handler := NewCSVHandler(cfg)

	// allowed/real.csv 是真實檔,allowed/output.csv → real.csv
	real := filepath.Join(allowedDir, "real.csv")
	require.NoError(t, os.WriteFile(real, []byte("placeholder"), fsperm.FilePerm))

	symlinkPath := filepath.Join(allowedDir, "output.csv")
	require.NoError(t, os.Symlink(real, symlinkPath))

	// WriteCSV 應放行 — resolved target 在 base 內
	data := [][]string{{"Time", "Ch1"}, {"1.0", "100"}}
	err = handler.WriteCSV(symlinkPath, data)
	require.NoError(t, err, "base 內部 symlink 應允許")

	// real.csv 應被覆寫 (因為 symlink 跟到底)
	got, err := os.ReadFile(real)
	require.NoError(t, err)
	require.Contains(t, string(got), "1.0,100",
		"real.csv 應透過 symlink 被寫入新內容")
}
