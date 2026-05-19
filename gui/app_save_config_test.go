package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// TestSaveConfig_AtomicAndUsesInjectedPath 釘住 fix:
//
//  1. App.SaveConfig 用 a.configPath 而不是 hardcoded "./config.json" —
//     確保 read path (loadStartupConfig 走 ResolveDefaultConfigPath) 與
//     write path (SaveConfig) 對稱。
//  2. Parent dir 不存在時 SaveConfig 必須自動 MkdirAll — 模擬使用者第一次
//     在 macOS 啟動 .app bundle,~/Library/Application Support/count_mean/
//     尚不存在的場景。
//  3. Atomic write — 中途 crash 不會留下半寫 partial JSON;成功 commit 後
//     final path 含完整 valid JSON,沒有 leftover .tmp 殘留。
//
// PoC 對 unfixed code 必失敗:hardcoded "./config.json" 寫不到 t.TempDir()
// 內的注入路徑,assert 看不到檔。
func TestSaveConfig_AtomicAndUsesInjectedPath(t *testing.T) {
	// 注入一條深層 path,parent dir 尚不存在 — 強迫 MkdirAll path 走起來。
	tempRoot := t.TempDir()
	configPath := filepath.Join(tempRoot, "count_mean", "config.json")

	// 確認 parent dir 真的不存在,證明 MkdirAll 走起來才能成功。
	if _, err := os.Stat(filepath.Dir(configPath)); !os.IsNotExist(err) {
		t.Fatalf("test 前提失敗:parent dir 預期不存在,但 stat err=%v", err)
	}

	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 42 // 改個值區分原 default,等下驗讀回來是不是這個。

	app := NewAppWithConfigPath(cfg, "test-version", configPath)

	// 改一個欄位再呼叫 SaveConfig 模擬「使用者修了設定」。
	modifiedCfg := config.DefaultConfig()
	modifiedCfg.ScalingFactor = 99

	require.NoError(t, app.SaveConfig(modifiedCfg))

	// 1) Final path 必須存在,內容是 modifiedCfg 而非 zero。
	require.FileExists(t, configPath, "SaveConfig 必須寫到注入路徑,不是 hardcoded ./config.json")

	rawJSON, readErr := os.ReadFile(configPath)
	require.NoError(t, readErr)

	var loaded config.AppConfig
	require.NoError(t, json.Unmarshal(rawJSON, &loaded))
	assert.Equal(t, 99, loaded.ScalingFactor, "寫入的內容必須是 modified cfg,不是 default")

	// 2) Parent dir 已被 MkdirAll 建立。
	stat, statErr := os.Stat(filepath.Dir(configPath))
	require.NoError(t, statErr)
	assert.True(t, stat.IsDir(), "parent dir 必須被建立成功")

	// 3) Atomic commit 完成 — 不應有 .tmp 殘留。
	entries, readDirErr := os.ReadDir(filepath.Dir(configPath))
	require.NoError(t, readDirErr)

	for _, e := range entries {
		if e.Name() != filepath.Base(configPath) {
			t.Errorf("atomic commit 後不該有殘留檔案(發現:%s)", e.Name())
		}
	}
}

// TestSaveConfig_OverwriteAtomic 釘住:對既有 config 覆寫時走 atomic 流程,
// 中途 fail 不會破壞原 file。簡化版:成功 overwrite 必須產出新內容,
// 沒中間狀態。
func TestSaveConfig_OverwriteAtomic(t *testing.T) {
	tempRoot := t.TempDir()
	configPath := filepath.Join(tempRoot, "count_mean", "config.json")

	// 預先寫好一份 cfg
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	initial := config.DefaultConfig()
	initial.ScalingFactor = 10
	rawInitial, _ := json.Marshal(initial)
	require.NoError(t, os.WriteFile(configPath, rawInitial, 0o600))

	// SaveConfig 覆寫
	app := NewAppWithConfigPath(config.DefaultConfig(), "test", configPath)
	updated := config.DefaultConfig()
	updated.ScalingFactor = 999

	require.NoError(t, app.SaveConfig(updated))

	// 讀回確認是 updated 的值
	rawAfter, _ := os.ReadFile(configPath)
	var loaded config.AppConfig
	require.NoError(t, json.Unmarshal(rawAfter, &loaded))
	assert.Equal(t, 999, loaded.ScalingFactor, "overwrite 後讀回必須是新值")
}
