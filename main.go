// Package main is the entry point for the EMG Data Analysis Tool application.
package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"count_mean/gui"
	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
)

// ErrUnexpectedPositional 表示 main 收到非 flag 的多餘 positional 參數。
// 讓 caller 可用 errors.Is 判斷,不必 string-match;test 也用此 sentinel 鎖契約。
var ErrUnexpectedPositional = errors.New("unexpected positional arguments")

// Version is set at build time via -ldflags "-X main.Version=vX.X.X".
var Version = "dev"

// Window dimensions for the application.
const (
	windowWidth  = 1024
	windowHeight = 768
)

// Background color RGB values.
const (
	bgColorR = 27
	bgColorG = 38
	bgColorB = 54
)

//go:embed all:frontend/dist
var assets embed.FS

// configLoadStatus 表示啟動時 config 載入的最終狀態。
type configLoadStatus int

// Config load status constants.
const (
	configLoadLoaded   configLoadStatus = iota // 使用者 config.json 載入成功
	configLoadDefault                          // 檔案不存在,回退 default(正常情境)
	configLoadFallback                         // 檔案存在但解析/驗證失敗,回退 default(異常情境)
)

// startupConfigResult 包裝 config 載入的最終狀態與原因。
//
// 原本 main 直接 `if err != nil { Error(...); cfg = Default() }` 是
// 一刀切的靜默退回 — 使用者無法分辨「沒設定」(正常)與「設定壞了」(需修),
// 且 missing case 在 LoadConfig 內部就吃掉返回 default、根本不會走到 err 分支。
// 改成回傳結構化結果,main 依 Status 走不同 log 級別與訊息,reason 帶上原因供除錯。
type startupConfigResult struct {
	Status configLoadStatus
	Reason string // 人類可讀的原因,例如 "config.json 不存在"、"解析失敗 ..."
	Err    error  // underlying error(若有),供 log 帶 stack/code
}

// loadStartupConfig 載入啟動時的 config,並分類最終結果。
//
// 行為:
//   - 檔案不存在 → 回 default + Status=Default + reason 標明「不存在」,無 error
//   - 檔案存在但解析/驗證失敗 → 回 default + Status=Fallback + 帶原始 error
//   - 檔案存在且有效 → 回使用者設定 + Status=Loaded
//
// 永遠回傳非 nil *AppConfig,讓 GUI 起得來;呼叫端依 Status 決定 log 級別。
func loadStartupConfig(path string) (*config.AppConfig, startupConfigResult) {
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		return config.DefaultConfig(), startupConfigResult{
			Status: configLoadDefault,
			Reason: fmt.Sprintf("config 檔案不存在(%s),使用 default 設定", path),
		}
	}

	cfg, err := config.LoadConfig(path)
	if err != nil {
		return config.DefaultConfig(), startupConfigResult{
			Status: configLoadFallback,
			Reason: fmt.Sprintf("config 檔案 %s 載入失敗,使用 default 設定", path),
			Err:    err,
		}
	}

	return cfg, startupConfigResult{Status: configLoadLoaded}
}

// CLI placeholder exit code — distinct from "normal failure" (1) so that
// scripts wrapping the binary can distinguish "you asked for unsupported
// mode" from "the app crashed". POSIX convention reserves 2 for "misuse
// of shell builtins / unsupported invocation".
const cliPlaceholderExitCode = 2

// fallbackConfigPath 保留供既有 main_test.go 引用 — 實際邏輯改委派給
// internal/config.FallbackConfigPath / ResolveDefaultConfigPath。
//
// 把 resolveDefaultConfigPath 搬到 internal/config 作 single source of
// truth,讓 gui.App.SaveConfig 也能用同一個路徑解析(讀/寫對稱)。main 這邊
// 留 thin wrapper 維持原本的 const + func 名稱契約不破 main_test.go 既有 assertion。
const fallbackConfigPath = config.FallbackConfigPath

// userConfigSubdir 與 fallbackConfigPath 對稱,引用 internal/config 版本。
const userConfigSubdir = config.UserConfigSubdir

// resolveDefaultConfigPath 委派到 internal/config.ResolveDefaultConfigPath。
// 行為與舊版完全等價 — 搬家的目的是讓 GUI 內部 SaveConfig 也能引用同一個 helper,
// 杜絕 read/write 路徑不對稱。
func resolveDefaultConfigPath() string {
	return config.ResolveDefaultConfigPath()
}

// runCLIPlaceholder 處理 `-cli` flag:目前只是 placeholder — 告知使用者 CLI
// 模式尚未實作並回傳非零 exit code。提煉成獨立函式供測試在不啟動 GUI 的
// 情況下 verify 行為。
//
// CLAUDE.md 長期宣稱「支援 GUI + CLI」,但 codebase 從來沒有 CLI
// 實作。與其默默無視 `-cli` 然後啟動 GUI 讓使用者困惑,不如顯式告知並 exit 2,
// 讓 doc 與 runtime contract 對齊。真正的 CLI 由未來 task 補上(out of scope)。
//
// fmt.Fprintln 寫入 stderr 失敗本身就是 best-effort 訊息 — 即使寫不出去仍要
// 回 exit 2 讓 caller 判斷,所以刻意忽略 error。
//
//nolint:errcheck // best-effort stderr messages; failure can't change exit code
func runCLIPlaceholder(stderr io.Writer) int {
	fmt.Fprintln(stderr, "CLI 模式尚未實作 / CLI mode not yet implemented")
	fmt.Fprintln(stderr, "請以 GUI 模式啟動(移除 -cli flag)/ Run without -cli to launch the GUI.")
	return cliPlaceholderExitCode
}

// runGUI 啟動 Wails GUI。負責 config 載入、logger/i18n init、Wails options
// 組裝。從 main 拆出來方便將來測試或 wrap(目前 main_test 仍只測 CLI path)。
//
// OnShutdown 接 app.Shutdown:Wails 內部有 SIGINT/SIGTERM handler 會把信號
// 轉成「正常關窗」走 OnBeforeClose → OnShutdown 流程,所以這裡不需要自己
// 重複註冊 signal.Notify — 重複註冊會搶走 Wails 的訊號接收造成 race。
//
// config 路徑改用 defaultConfigPath const,移除字面 hard-code。
// 改用 resolveDefaultConfigPath() — macOS bundle 啟動 CWD=/ 永遠拿不到
// "./config.json",改走 os.UserConfigDir() 才能真的讀到使用者設定。
func runGUI() error {
	configPath := resolveDefaultConfigPath()
	cfg, loadRes := loadStartupConfig(configPath)
	switch loadRes.Status {
	case configLoadLoaded:
		// happy path:不額外 log,避免噪音
	case configLoadDefault:
		// 檔案不存在 — info 級別,讓使用者知道為何走 default
		logging.Info(loadRes.Reason)
	case configLoadFallback:
		// 檔案存在但壞掉 — warn 級別 + 原始 error,使用者可看到「為什麼」走 default
		logging.Warn(loadRes.Reason, map[string]interface{}{
			"error": loadRes.Err.Error(),
		})
	}

	// 解析日誌級別
	var logLevel logging.LogLevel

	switch cfg.LogLevel {
	case "debug":
		logLevel = logging.LevelDebug
	case "info":
		logLevel = logging.LevelInfo
	case "warn":
		logLevel = logging.LevelWarn
	case "error":
		logLevel = logging.LevelError
	default:
		logLevel = logging.LevelInfo
	}

	// 初始化日誌系統
	jsonFormat := cfg.LogFormat == "json"
	if err := logging.InitLogger(logLevel, cfg.LogDirectory, jsonFormat); err != nil {
		logging.Error("無法初始化日誌系統", err)
	}
	// 在 runGUI 結束 (Wails 主迴圈關閉) 後釋放 logger file handle。
	// 即使 InitLogger 失敗 (走 stderr fallback) ShutdownLogger 也 no-op safe。
	defer func() {
		if err := logging.ShutdownLogger(); err != nil {
			// 已經要關了,失敗只印 stderr — logger 自己可能已掛
			fmt.Fprintln(os.Stderr, "ShutdownLogger error:", err) //nolint:errcheck // best-effort
		}
	}()

	// 初始化國際化系統
	if err := i18n.InitI18n(cfg.TranslationsDir); err != nil {
		logging.Error("初始化國際化系統失敗", err)
	} else {
		i18n.SetLocale(i18n.Locale(cfg.Language))
	}

	// 創建應用實例 — 顯式注入 configPath,讓 SaveConfig 寫的位置與 loadStartupConfig
	// 讀的位置對稱。
	app := gui.NewAppWithConfigPath(cfg, Version, configPath)

	// 創建 Wails 應用
	if err := wails.Run(&options.App{
		Title:            fmt.Sprintf("EMG 資料分析工具 %s", Version),
		Width:            windowWidth,
		Height:           windowHeight,
		BackgroundColour: &options.RGBA{R: bgColorR, G: bgColorG, B: bgColorB, A: 1},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		Bind: []interface{}{
			app,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
			CSSDropProperty:    "--wails-drop-target",
			CSSDropValue:       "drop",
		},
	}); err != nil {
		return fmt.Errorf("wails 執行失敗: %w", err)
	}

	return nil
}

// parsedArgs 表示 main 解析 args 後的結果。供 test 在不啟動 GUI 的情況下驗證
// flag 解析行為。
//
// 過去 main 直接 `flag.Bool + flag.Parse()` 操作全域 CommandLine,
// 測試只能透過 os.Args 注入 + subprocess 取 exit code,既慢且難以斷言中間狀態。
// 抽 parseArgs(args []string) 接受顯式 slice,測試可純 in-process 驗證解析結果。
type parsedArgs struct {
	cliMode bool
}

// parseArgs 解析 argv-style 參數 (不含 program name)。回 error 時 caller 自行決定 exit。
//
// 為什麼用 flag.NewFlagSet 而非全域 flag:全域 CommandLine 在 test 之間有殘留狀態,
// 重複呼叫 Parse 會踩到 redefined-flag panic。NewFlagSet("main",ContinueOnError) 隔離,
// ContinueOnError 把解析失敗回給 caller 而非 os.Exit (default ExitOnError 行為)。
//
// 多餘 positional args 顯式 reject。flag 預設會 silently 把 `emg-tool foo bar`
// 的 "foo"、"bar" 收進 fs.Args() 然後不報錯 — 但 main 目前沒有任何子命令,positional
// 等同 typo/誤用。fail-loud 讓使用者立刻看到「你寫的東西沒被讀到」,不會誤以為
// 工具在按指令跑。
func parseArgs(args []string) (parsedArgs, error) {
	fs := flag.NewFlagSet("emg-tool", flag.ContinueOnError)
	// 把 usage 寫到 io.Discard 避免 test 在 stderr 亂噴 — caller 仍可 inspect error。
	fs.SetOutput(io.Discard)

	cliMode := fs.Bool("cli", false, "run in CLI mode (currently a placeholder; exits with code 2)")

	if err := fs.Parse(args); err != nil {
		return parsedArgs{}, fmt.Errorf("parse args: %w", err)
	}

	if fs.NArg() > 0 {
		return parsedArgs{}, fmt.Errorf("%w: %v", ErrUnexpectedPositional, fs.Args())
	}

	return parsedArgs{cliMode: *cliMode}, nil
}

func main() {
	// `-cli` 是 placeholder:CLAUDE.md 文件層級宣稱支援 CLI,實際上沒有實作。
	// 與其默默忽略 flag 直接啟 GUI(困惑),不如顯式告知並 exit 2。詳見
	// runCLIPlaceholder doc。
	parsed, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Argument error:", err.Error()) //nolint:errcheck // best-effort
		os.Exit(cliPlaceholderExitCode)
	}

	if parsed.cliMode {
		os.Exit(runCLIPlaceholder(os.Stderr))
	}

	if err := runGUI(); err != nil {
		// 保留原本「失敗印到 stdout」相容性(舊版用 println),但改成 stderr
		// 較合慣例 — 失敗訊息給人讀,exit code 給腳本判斷。
		fmt.Fprintln(os.Stderr, "Error:", err.Error()) //nolint:errcheck // best-effort failure message
		os.Exit(1)
	}
}
