package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/security/fsperm"
)

// TestLoadConfig_RejectsLeafSymlink 釘住 LoadConfig 過去直接
// os.OpenFile(filename, fsperm.ReadFlags),Unix 上 fsperm.ReadFlags 帶
// O_NOFOLLOW (kernel 拒絕 leaf symlink),Windows 上 ReadFlags **不含** O_NOFOLLOW
// (Windows 標準 syscall 無等價物 — 見 flags_windows.go package doc),
// 結果 Windows 完全沒擋 leaf symlink。
//
// 修法後 LoadConfig 在 open 前以 os.Lstat 顯式偵測 ModeSymlink / ModeIrregular
// (Windows reparse point 走後者),三平台統一 reject leaf symlink。Unix 仍保有
// O_NOFOLLOW double-protection;Windows 由此 caller-side check 補上等價防禦。
//
// 攻擊場景:把 config.json 換成 symlink → /etc/passwd (Unix) 或
// reparse point → C:\Windows\System32\... (Windows)。修法前 Windows kernel 跟
// 著解析,LoadConfig 讀到敏感檔;修法後 Lstat 看到 ModeSymlink 直接 reject,
// kernel 從不接觸真實 target。
//
// `os.Symlink` 在 Windows 需 Developer Mode / admin,不可用時 Skip。
func TestLoadConfig_RejectsLeafSymlink(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	realConfigPath := filepath.Join(tempDir, "real_config.json")
	const validJSON = `{
		"scalingFactor": 10,
		"phaseLabels": ["階段1", "階段2", "階段3", "階段4"],
		"precision": 5,
		"outputFormat": "csv",
		"bomEnabled": true,
		"inputDir": "./input",
		"outputDir": "./output",
		"operateDir": "./value_operate",
		"language": "zh-TW"
	}`
	require.NoError(t, os.WriteFile(realConfigPath, []byte(validJSON), fsperm.FilePerm),
		"setup: write real config")

	// 攻擊者把 config.json 換成 symlink → real_config.json (任何目標都可,核心
	// 在於「leaf 是 symlink 就拒」不論 target 是哪個檔)
	configPath := filepath.Join(tempDir, "config.json")
	if err := os.Symlink(realConfigPath, configPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("os.Symlink on Windows requires Developer Mode / admin; skipping: %v", err)
		}
		t.Fatalf("setup: Symlink failed: %v", err)
	}

	cfg, loadErr := LoadConfig(configPath)
	require.Error(t, loadErr, "leaf symlink 應被 Lstat 攔截,實際通過 (讀到 %v)", cfg)
	require.Contains(t, loadErr.Error(), "symlink",
		"error message should mention symlink rejection rationale")
	require.Nil(t, cfg, "fail-fast: 攻擊路徑不應產生 config 物件")
}

// TestLoadConfig_RejectsAllLeafSymlinks_IncludingInternalAlias 釘住 
// LoadConfig 對所有 leaf symlink (含內部 alias 指向同 dir 下另一檔) 均 reject —
// 不細分「指向 base 內」「指向 base 外」。Lstat-based 判斷比 EvalSymlinks-then-
// matchAnyBase 簡單可靠,trade-off 是用戶若需要 alias 必須直接傳真實檔名。
//
// 對齊 Unix O_NOFOLLOW 行為:過去 fsperm.ReadFlags 在 Unix 上會直接擋所有 leaf
// symlink (不分內外),Windows 沒擋;修法後三平台統一 reject。
func TestLoadConfig_RejectsAllLeafSymlinks_IncludingInternalAlias(t *testing.T) {
	t.Parallel()

	parentDir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	// real config
	realPath := filepath.Join(parentDir, "config.json")
	const validJSON = `{
		"scalingFactor": 10,
		"phaseLabels": ["階段1", "階段2", "階段3", "階段4"],
		"precision": 5,
		"outputFormat": "csv",
		"bomEnabled": true,
		"inputDir": "./input",
		"outputDir": "./output",
		"operateDir": "./value_operate",
		"language": "zh-TW"
	}`
	require.NoError(t, os.WriteFile(realPath, []byte(validJSON), fsperm.FilePerm))

	// alias symlink in same parent
	aliasPath := filepath.Join(parentDir, "alias.json")
	if err := os.Symlink(realPath, aliasPath); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("os.Symlink on Windows requires Developer Mode / admin; skipping: %v", err)
		}
		t.Fatalf("setup: Symlink failed: %v", err)
	}

	// 即便 alias 指向 base 內的合法檔,LoadConfig 也 reject — leaf-symlink 一律
	// 不接受,讓 path 來源語意保持「直接檔案」單一可信路徑。
	cfg, loadErr := LoadConfig(aliasPath)
	require.Error(t, loadErr, "leaf alias symlink 應被 Lstat 攔截")
	require.Contains(t, loadErr.Error(), "symlink")
	require.Nil(t, cfg)

	// 對比:直接讀 realPath (非 symlink) 應成功
	cfg, loadErr = LoadConfig(realPath)
	require.NoError(t, loadErr)
	require.NotNil(t, cfg)
	require.Equal(t, 10, cfg.ScalingFactor)
}

// TestLoadConfig_NonExistentFile_StillReturnsDefault 釘住 backwards-compat:
// 舊行為對「config.json 不存在」回 DefaultConfig 而非 error;OpenReadValidated 走
// %w wrapping 路徑,os.IsNotExist 應仍 trigger。
func TestLoadConfig_NonExistentFile_StillReturnsDefault(t *testing.T) {
	t.Parallel()

	parentDir := t.TempDir()
	missingPath := filepath.Join(parentDir, "does_not_exist.json")

	cfg, err := LoadConfig(missingPath)
	require.NoError(t, err, "missing config 應 fallback 到 DefaultConfig,不應 error")
	require.NotNil(t, cfg)
	require.Equal(t, DefaultConfig().ScalingFactor, cfg.ScalingFactor)
}

