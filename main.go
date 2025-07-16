package main

import (
	"count_mean/gui"
	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
	"embed"

	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// 載入配置
	cfg, err := config.LoadConfig("./config.json")
	if err != nil {
		logging.Error("無法載入配置", err)
		cfg = config.DefaultConfig()
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

	// 初始化國際化系統
	if err := i18n.InitI18n(cfg.TranslationsDir); err != nil {
		logging.Error("初始化國際化系統失敗", err)
	} else {
		i18n.SetLocale(i18n.Locale(cfg.Language))
	}

	// 創建應用實例
	app := gui.NewApp(cfg)

	// 創建 Wails 應用
	err = wails.Run(&options.App{
		Title:            "EMG 資料分析工具",
		Width:            1024,
		Height:           768,
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: app.Startup,
		Bind: []interface{}{
			app,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: false,
			CSSDropProperty:    "--wails-drop-target",
			CSSDropValue:       "drop",
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}
