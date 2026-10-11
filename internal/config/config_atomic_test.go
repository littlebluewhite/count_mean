package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSaveConfigAtomic_RoundTrip verifies that SaveConfigAtomic writes a config
// that LoadConfig can round-trip back to an equal value.
func TestSaveConfigAtomic_RoundTrip(t *testing.T) {
	c := DefaultConfig()
	path := filepath.Join(t.TempDir(), "config.json")

	err := c.SaveConfigAtomic(path)
	require.NoError(t, err)

	loaded, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, c, loaded)
}

// TestSaveConfigAtomic_CreatesParentDir verifies that SaveConfigAtomic creates
// missing parent directories automatically.
func TestSaveConfigAtomic_CreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "deep", "config.json")

	err := DefaultConfig().SaveConfigAtomic(path)
	require.NoError(t, err)

	_, err = os.Stat(path)
	require.NoError(t, err, "config file must exist after SaveConfigAtomic")
}

// TestSaveConfigAtomic_MkdirErrorHidesParentDir 釘住 parent 目錄建立失敗時,自己內插的
// 目錄以 redact.DirPath 帶入、整條隱藏(ADR-0059);路徑只剩 cause 的 *PathError 文字裡,
// 交給 sink 端 redact。
func TestSaveConfigAtomic_MkdirErrorHidesParentDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	err := DefaultConfig().SaveConfigAtomic(filepath.Join(blocker, "sub", "config.json"))
	require.Error(t, err)
	require.True(t, strings.HasPrefix(err.Error(), "無法建立 config parent 目錄 <redacted-path>/: "),
		"err = %v", err)
}

// TestSaveConfigAtomic_StaleTmpOrphanDoesNotBlock is a regression test for the
// P2 bug: the current implementation uses a fixed tmp name (filename+".tmp") and
// opens it with O_EXCL. A stale orphan left by a prior crash permanently blocks
// SaveConfigAtomic with EEXIST until manually deleted.
//
// This test is RED on the current (unmodified) code — SaveConfigAtomic returns
// "建立 tmp config 檔失敗: ... file exists" because O_EXCL collides with the
// pre-planted orphan. It will turn GREEN after the planned refactor switches to a
// crypto-random tmp name, which sidesteps the fixed-name collision entirely.
func TestSaveConfigAtomic_StaleTmpOrphanDoesNotBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Pre-plant the fixed-name orphan exactly as the buggy code collides with.
	orphan := path + ".tmp"
	err := os.WriteFile(orphan, []byte("stale"), 0o600)
	require.NoError(t, err, "setup: could not write stale orphan")

	// SaveConfigAtomic must succeed even when the fixed .tmp name already exists.
	err = DefaultConfig().SaveConfigAtomic(path)
	require.NoError(t, err, "SaveConfigAtomic must not be blocked by a stale .tmp orphan")

	// The real config file must be readable and intact.
	loaded, err := LoadConfig(path)
	require.NoError(t, err)
	require.Equal(t, DefaultConfig(), loaded)
}
