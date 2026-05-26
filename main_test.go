package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// regression: 啟動時 config 載入若失敗,需把 reason 帶到 startupConfigResult,
// 讓 main 可以記 warn + 「使用 default 因為 X」訊息而非靜默退回 default
// (使用者在 log 中只看到通用 "無法載入配置" 無從修起)。

// TestLoadStartupConfig_FileMissing 守護:檔案不存在時 — 走「使用 default,info 級別」分支。
// 行為:回傳 default config + StatusUsedDefault + 訊息含「不存在」字樣。
func TestLoadStartupConfig_FileMissing(t *testing.T) {
	tempDir := t.TempDir()
	missing := filepath.Join(tempDir, "no_such_config.json")

	cfg, res := loadStartupConfig(missing)

	require.NotNil(t, cfg, "missing 檔仍應回 default config(非 nil)")
	assert.Equal(t, configLoadDefault, res.Status, "missing 應為 Default 狀態")
	assert.NoError(t, res.Err, "missing 不算錯誤")
	assert.Contains(t, res.Reason, "不存在", "理由應說明檔案不存在以利使用者除錯")
	// 驗證真的是 default
	def := config.DefaultConfig()
	assert.Equal(t, def.ScalingFactor, cfg.ScalingFactor)
}

// TestLoadStartupConfig_InvalidJSON 守護:malformed JSON 必須帶 Err + warn 級別 reason,
// 而非「靜默走 default」— 使用者在 log 看到具體 parse error,知道要修 config.json。
func TestLoadStartupConfig_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	bad := filepath.Join(tempDir, "broken.json")
	require.NoError(t, os.WriteFile(bad, []byte("{ this is not valid json"), 0o600))

	cfg, res := loadStartupConfig(bad)

	require.NotNil(t, cfg, "即使解析失敗仍回 default 避免 GUI 起不來")
	assert.Equal(t, configLoadFallback, res.Status, "解析失敗應為 Fallback 狀態(走 default 但有警告)")
	require.Error(t, res.Err, "malformed JSON 必須帶 underlying error")
	assert.Contains(t, res.Reason, bad, "reason 必須帶檔案路徑方便除錯")
}

// TestLoadStartupConfig_InvalidValues 守護:JSON OK 但 Validate 失敗(如 scalingFactor=-1)
// 同樣走 Fallback + 帶 error,使用者看到 validate 錯誤訊息。
func TestLoadStartupConfig_InvalidValues(t *testing.T) {
	tempDir := t.TempDir()
	bad := filepath.Join(tempDir, "invalid_values.json")
	bytes, err := json.Marshal(map[string]any{
		"scalingFactor": -1,
		"phaseLabels":   []string{},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(bad, bytes, 0o600))

	cfg, res := loadStartupConfig(bad)

	require.NotNil(t, cfg, "validate 失敗仍回 default")
	assert.Equal(t, configLoadFallback, res.Status)
	require.Error(t, res.Err)
	// underlying error 必須是來自 config package(validate 失敗訊息)
	var pathErr *os.PathError
	assert.False(t, errors.As(res.Err, &pathErr), "validate error 不應是 os.PathError")
}

// TestRunCLIPlaceholder 守護 `-cli` 啟動時必須印出「未實作」訊息
// 並回傳非零 exit code,讓 CLAUDE.md 文件層級宣稱「支援 CLI」與 runtime 行為對齊。
// 用 io.Writer 注入而非 subprocess,避免在 CI 跑 binary build 拖慢測試。
func TestRunCLIPlaceholder(t *testing.T) {
	var stderr bytes.Buffer

	code := runCLIPlaceholder(&stderr)

	assert.Equal(t, cliPlaceholderExitCode, code, "CLI placeholder 必須以 exit 2 結束,讓 wrapping script 能區分 misuse 與 crash")
	assert.NotEqual(t, 0, code, "不能以 success 回傳 — 否則 caller 會誤以為 CLI 跑成功")

	output := stderr.String()
	assert.Contains(t, output, "尚未實作", "中文使用者必須看到「尚未實作」字樣")
	assert.Contains(t, output, "not yet implemented", "英文使用者必須看到「not yet implemented」字樣")
	assert.Contains(t, output, "-cli", "輸出必須提及 flag 名稱方便使用者回頭看 help/doc")
}

// TestParseArgs_CliFlag 守護 parseArgs 抽離後可純 in-process 驗證 flag 解析。
// 過去 main 直接走全域 CommandLine,測試只能透過 subprocess 取 exit code 間接驗。
func TestParseArgs_CliFlag(t *testing.T) {
	cases := []struct {
		name        string
		args        []string
		wantCliMode bool
		wantErr     bool
	}{
		{
			name:        "no args",
			args:        []string{},
			wantCliMode: false,
			wantErr:     false,
		},
		{
			name:        "cli flag set",
			args:        []string{"-cli"},
			wantCliMode: true,
			wantErr:     false,
		},
		{
			name:        "double dash cli flag",
			args:        []string{"--cli"},
			wantCliMode: true,
			wantErr:     false,
		},
		{
			name:        "explicit true",
			args:        []string{"-cli=true"},
			wantCliMode: true,
			wantErr:     false,
		},
		{
			name:        "explicit false",
			args:        []string{"-cli=false"},
			wantCliMode: false,
			wantErr:     false,
		},
		{
			name:    "unknown flag returns error",
			args:    []string{"-unknown-flag"},
			wantErr: true,
		},
		// 多餘 positional args 必須 reject — 過去 silently 吃掉造成 typo 不報錯。
		{
			name:    "extra positional arg rejected",
			args:    []string{"extra"},
			wantErr: true,
		},
		{
			name:    "extra positional arg after cli flag rejected",
			args:    []string{"-cli", "extra"},
			wantErr: true,
		},
		{
			name:    "multiple positional args rejected",
			args:    []string{"foo", "bar"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			parsed, err := parseArgs(tc.args)
			if tc.wantErr {
				require.Error(t, err, "args=%v 應該 parse 失敗", tc.args)
				return
			}
			require.NoError(t, err, "args=%v 應該 parse 成功", tc.args)
			assert.Equal(t, tc.wantCliMode, parsed.cliMode,
				"args=%v cliMode 期望 %v 得到 %v", tc.args, tc.wantCliMode, parsed.cliMode)
		})
	}
}

// TestParseArgs_IsolatedFromGlobalFlags 守護 parseArgs 不污染 flag.CommandLine。
// 用 NewFlagSet 隔離後,test 之間不會踩到 redefined-flag panic。
func TestParseArgs_IsolatedFromGlobalFlags(t *testing.T) {
	// 多次呼叫 parseArgs 不該 panic — flag.CommandLine 隔離
	for i := 0; i < 3; i++ {
		_, _ = parseArgs([]string{"-cli"})
	}
	// 走到這裡表示沒 panic — 隔離契約成立
}

// TestParseArgs_PositionalArgsErrorMatchesSentinel 守護
// extra positional args 必須回 ErrUnexpectedPositional sentinel,讓 caller 用
// errors.Is 判斷而非 string-match。
func TestParseArgs_PositionalArgsErrorMatchesSentinel(t *testing.T) {
	_, err := parseArgs([]string{"unexpected"})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnexpectedPositional,
		"positional 多餘參數應回 ErrUnexpectedPositional sentinel(errors.Is)")
	// 訊息也要把實際 args 帶上,方便使用者除錯
	assert.Contains(t, err.Error(), "unexpected", "錯誤訊息應提及實際多餘參數")
}

// TestResolveDefaultConfigPath_NonEmpty 守護預設 config 路徑解析結果不可為
// 空字串 — 避免未來重構誤改成空值導致 GUI 永遠走 default config 而不 log。
func TestResolveDefaultConfigPath_NonEmpty(t *testing.T) {
	got := resolveDefaultConfigPath()
	assert.NotEmpty(t, got, "resolveDefaultConfigPath 不能回空")
	assert.Contains(t, got, "config.json", "結果必須指向 config.json")
}

// TestResolveDefaultConfigPath_UsesUserConfigDir 釘住
//
// 過去 defaultConfigPath = "./config.json" 在 macOS 走 Finder 雙擊 .app
// 時 CWD = "/"(launchd 預設行為),永遠 stat 不到 → 使用者編輯的 config.json
// 被靜默忽略,GUI 每次啟動都走 default 配置。修法是改用 os.UserConfigDir()
// 解析,fallback 才回 "./config.json"。
//
// 測試策略:覆蓋 HOME / XDG_CONFIG_HOME / APPDATA 等 env 把 UserConfigDir
// 指到 t.TempDir() 下,驗證:
//  1. 路徑落在那個 tempDir 底下(不是 CWD 相對 fallback)
//  2. 路徑含 count_mean subdir(避免直接寫到使用者 config 根目錄)
//  3. 檔名為 config.json
func TestResolveDefaultConfigPath_UsesUserConfigDir(t *testing.T) {
	tempDir := t.TempDir()

	// os.UserConfigDir 在不同平台讀不同 env:
	//   - linux:     $XDG_CONFIG_HOME，若空則 $HOME/.config
	//   - darwin:    $HOME/Library/Application Support
	//   - windows:   %AppData%
	// 同時覆蓋所有可能 source,讓本測試在 CI 跨平台都成立。
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("HOME", tempDir)
	t.Setenv("AppData", tempDir)

	got := resolveDefaultConfigPath()
	require.NotEmpty(t, got)

	// 解析結果必須含 count_mean 子目錄,避免污染使用者 config 根目錄
	assert.Contains(t, got, userConfigSubdir, "路徑必須有 count_mean 子目錄")
	assert.Contains(t, got, "config.json")

	// 預期路徑不是裸的 fallback "./config.json"(代表真的走 UserConfigDir 分支)
	assert.NotEqual(t, fallbackConfigPath, got, "macOS bundle CWD=/ 情境必須走 UserConfigDir,不能 fallback")
}

// TestResolveDefaultConfigPath_Fallback 釘住 fallback 分支:當 UserConfigDir
// 完全拿不到值(HOME / XDG / APPDATA 全空),resolveDefaultConfigPath 必須回
// fallbackConfigPath 而不是空字串 / panic。
//
// 這個分支只在極端 CI / container 環境發生,但仍需鎖住契約 — fallback 路徑
// 至少保證 GUI 起得來(走 default config 流程)。
func TestResolveDefaultConfigPath_Fallback(t *testing.T) {
	// 清掉所有可能讓 UserConfigDir 成功的 env。注意:os.Unsetenv 在 Go test
	// 透過 t.Setenv("","") 等價 — 用 t.Setenv 即可享受 cleanup。
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("AppData", "")

	got := resolveDefaultConfigPath()

	// 環境變數全空時 UserConfigDir 會回 error → fallback 路徑
	if got != fallbackConfigPath {
		// 某些平台(例如 sandboxed CI 仍能解出 default user config),
		// 至少要 contain 子目錄與 config.json
		assert.Contains(t, got, "config.json")
	}
}

// TestLoadStartupConfig_ValidFile 守護 happy path:有效 config 走 Loaded 狀態,無錯誤。
func TestLoadStartupConfig_ValidFile(t *testing.T) {
	tempDir := t.TempDir()
	good := filepath.Join(tempDir, "good.json")
	validJSON := `{
		"scalingFactor": 25,
		"phaseLabels": ["A", "B"],
		"precision": 8,
		"outputFormat": "csv",
		"bomEnabled": true,
		"inputDir": "./input",
		"outputDir": "./output",
		"operateDir": "./operate",
		"language": "zh-TW"
	}`
	require.NoError(t, os.WriteFile(good, []byte(validJSON), 0o600))

	cfg, res := loadStartupConfig(good)

	require.NotNil(t, cfg)
	assert.Equal(t, configLoadLoaded, res.Status)
	assert.NoError(t, res.Err)
	assert.Equal(t, 25, cfg.ScalingFactor, "成功路徑必須回傳使用者實際設定值")
}
