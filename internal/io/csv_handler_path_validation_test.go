package io

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/security/fsperm"
)

// TestCSVHandler_ReadCSVRejectsSensitivePaths 釘住 ReadCSV 唯一入口的路徑守門:
// 任意不含敏感位置的絕對路徑可讀(不再區分 InputDir 內外),/etc 與 traversal 仍擋。
func TestCSVHandler_ReadCSVRejectsSensitivePaths(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = tempDir
	cfg.OutputDir = tempDir
	cfg.OperateDir = tempDir
	handler := NewCSVHandler(cfg)

	validCSV := filepath.Join(tempDir, "valid.csv")
	require.NoError(t, os.WriteFile(validCSV, []byte("h\n1\n"), fsperm.FilePerm))

	t.Run("within_input_dir_ok", func(t *testing.T) {
		t.Parallel()
		records, err := handler.ReadCSV(validCSV)
		require.NoError(t, err)
		require.Equal(t, [][]string{{"h"}, {"1"}}, records)
	})

	t.Run("sensitive_dir_rejected", func(t *testing.T) {
		t.Parallel()
		_, err := handler.ReadCSV("/etc/passwd")
		require.Error(t, err)
		require.Contains(t, err.Error(), "路徑")
	})

	t.Run("traversal_rejected", func(t *testing.T) {
		t.Parallel()
		_, err := handler.ReadCSV("../../etc/passwd")
		require.Error(t, err)
		require.Contains(t, err.Error(), "路徑")
	})

	t.Run("external_user_dir_allowed", func(t *testing.T) {
		t.Parallel()
		// 不在 InputDir 內但無敏感位置的 user-selected 檔,同一個入口直接可讀。
		externalCSV := filepath.Join(t.TempDir(), "user_picked.csv")
		require.NoError(t, os.WriteFile(externalCSV, []byte("h\n1\n"), fsperm.FilePerm))

		records, err := handler.ReadCSV(externalCSV)
		require.NoError(t, err)
		require.NotNil(t, records)
	})
}
