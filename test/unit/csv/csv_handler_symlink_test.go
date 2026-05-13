//go:build !windows

package csv_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/security/fsperm"
)

// TestCSVHandler_WriteRejectsSymlink verifies open-time symlink rejection via
// the O_NOFOLLOW flag (Wave 5 PR1). 此測試**非**真正的 TOCTOU race 測試：
// 它不啟 goroutine、不刻意製造 check/use 之間的時間窗，純粹驗證 syscall 層
// 對 symlink 的拒絕。檔案先前命名為 toctou_test.go 誤導了後續 reviewer
// （post-review 2026-05-13 重命名為 symlink_test.go 以符實際 scope）。
//
// 攻擊場景：擁有 OutputDir 寫權限的攻擊者在預期輸出檔名植入指向敏感檔案
// （如 /etc/passwd）的 symlink；應用下次 CSV 匯出若沒有 O_NOFOLLOW 會把
// symlink 跟到底並覆寫敏感檔案。加上 O_NOFOLLOW 後 kernel 直接回 ELOOP 拒絕。
//
// 若要追加真正的 race window 測試，需用 goroutine 在 OpenFile 進行中
// swap symlink；但 O_NOFOLLOW 本身是 atomic syscall，userspace race 已
// 被排除，補測效益有限。
func TestCSVHandler_WriteRejectsSymlink(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = tempDir
	cfg.OutputDir = tempDir
	cfg.OperateDir = tempDir
	handler := io.NewCSVHandler(cfg)

	// Plant a symlink at the would-be output filename pointing somewhere else.
	victim := filepath.Join(tempDir, "victim.csv")
	require.NoError(t, os.WriteFile(victim, []byte("untouched\n"), fsperm.FilePerm))

	symlinkPath := filepath.Join(tempDir, "output.csv")
	require.NoError(t, os.Symlink(victim, symlinkPath))

	// WriteCSV through the symlinked path must fail (not silently follow).
	data := [][]string{{"Time", "Ch1"}, {"1.0", "100"}}
	err := handler.WriteCSV(symlinkPath, data)
	require.Error(t, err)

	// The error should originate from the open syscall — ELOOP on darwin/linux.
	// We don't pin the exact wording because Go's error wrap varies by version;
	// just require that the message references the path or file operation.
	require.True(t,
		strings.Contains(err.Error(), "建立檔案") ||
			strings.Contains(err.Error(), "symbolic") ||
			strings.Contains(err.Error(), "too many"),
		"expected open-time symlink rejection error, got: %v", err,
	)

	// Confirm the victim file was NOT overwritten (the whole point of the test).
	got, err := os.ReadFile(victim)
	require.NoError(t, err)
	require.Equal(t, "untouched\n", string(got))
}
