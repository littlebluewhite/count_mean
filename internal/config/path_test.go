package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolveDefaultConfigPath_UsesUserConfigDir 釘住 從 main.go 搬家後,
// 行為等價於原版 resolveDefaultConfigPath — 走 os.UserConfigDir() 解析,
// 落在 count_mean 子目錄底下、檔名為 config.json。
func TestResolveDefaultConfigPath_UsesUserConfigDir(t *testing.T) {
	tempDir := t.TempDir()

	// 跨平台覆蓋:linux 用 XDG_CONFIG_HOME,darwin 用 HOME,windows 用 AppData。
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("HOME", tempDir)
	t.Setenv("AppData", tempDir)

	got := ResolveDefaultConfigPath()
	assert.NotEmpty(t, got)
	assert.Contains(t, got, UserConfigSubdir, "路徑必須有 count_mean 子目錄")
	assert.Contains(t, got, "config.json")
	assert.NotEqual(t, FallbackConfigPath, got, "正常情境必須走 UserConfigDir,不能 fallback")
}

// TestResolveDefaultConfigPath_Fallback 釘住 fallback:env 全空時走
// FallbackConfigPath 而非 panic / 空字串。
func TestResolveDefaultConfigPath_Fallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("AppData", "")

	got := ResolveDefaultConfigPath()
	assert.NotEmpty(t, got)

	if got != FallbackConfigPath {
		// 某些平台仍能解出 default user config (sandbox 等),容許這條替代路徑,
		// 但仍必須是個合法 config.json file path,而不是空字串。
		assert.Equal(t, "config.json", filepath.Base(got))
	}
}
