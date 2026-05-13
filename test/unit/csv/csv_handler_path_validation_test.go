package csv_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/security/fsperm"
)

// TestCSVHandler_ReadCSVRejectsSensitivePaths validates that the readCSVCore
// path-validation gate (added in Wave 5 PR1) blocks attacker-controlled paths
// passed through both ReadCSV (strict, allowlist-enforced) and ReadCSVExternal
// (lenient, sensitive-prefix-blocked).
func TestCSVHandler_ReadCSVRejectsSensitivePaths(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = tempDir
	cfg.OutputDir = tempDir
	cfg.OperateDir = tempDir
	handler := io.NewCSVHandler(cfg)

	// Valid baseline: csv inside the allowlist directory should succeed via both APIs.
	validCSV := filepath.Join(tempDir, "valid.csv")
	require.NoError(t, os.WriteFile(validCSV, []byte("h\n1\n"), fsperm.FilePerm))

	t.Run("strict_within_allowlist_ok", func(t *testing.T) {
		t.Parallel()
		records, err := handler.ReadCSV(validCSV)
		require.NoError(t, err)
		require.Equal(t, [][]string{{"h"}, {"1"}}, records)
	})

	t.Run("strict_outside_allowlist_rejected", func(t *testing.T) {
		t.Parallel()
		// /etc is outside InputDir/OutputDir/OperateDir → must reject before
		// any os.Stat call would expose the file's existence.
		_, err := handler.ReadCSV("/etc/passwd")
		require.Error(t, err)
		require.Contains(t, err.Error(), "路徑")
	})

	t.Run("external_sensitive_dir_rejected", func(t *testing.T) {
		t.Parallel()
		// ReadCSVExternal must still block /etc / /root / system dirs.
		_, err := handler.ReadCSVExternal("/etc/passwd")
		require.Error(t, err)
		require.Contains(t, err.Error(), "路徑")
	})

	t.Run("traversal_rejected_both_modes", func(t *testing.T) {
		t.Parallel()
		_, err := handler.ReadCSV("../../etc/passwd")
		require.Error(t, err)
		require.Contains(t, err.Error(), "路徑")

		_, err = handler.ReadCSVExternal("../../etc/passwd")
		require.Error(t, err)
		require.Contains(t, err.Error(), "路徑")
	})

	t.Run("external_user_dir_allowed", func(t *testing.T) {
		t.Parallel()
		// 不在 allowlist 但無敏感前綴的 user-selected 檔，external mode 應允許讀取。
		// 用第二個 t.TempDir 模擬「外部目錄」。
		externalDir := t.TempDir()
		externalCSV := filepath.Join(externalDir, "user_picked.csv")
		require.NoError(t, os.WriteFile(externalCSV, []byte("h\n1\n"), fsperm.FilePerm))

		records, err := handler.ReadCSVExternal(externalCSV)
		require.NoError(t, err)
		require.NotNil(t, records)
	})

	t.Run("external_user_dir_strict_rejected", func(t *testing.T) {
		t.Parallel()
		// 同一份外部檔走 strict ReadCSV 必須 reject — 確認 routing 沒漏掉。
		externalDir := t.TempDir()
		externalCSV := filepath.Join(externalDir, "user_picked.csv")
		require.NoError(t, os.WriteFile(externalCSV, []byte("h\n1\n"), fsperm.FilePerm))

		_, err := handler.ReadCSV(externalCSV)
		require.Error(t, err)
		require.Contains(t, err.Error(), "路徑")
	})
}
