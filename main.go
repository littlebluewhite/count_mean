package main

import (
	"count_mean/gui"
	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
	"os"
)

func main() {
	// 載入配置（首先嘗試載入配置以獲取日誌設定）
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		// 如果無法載入配置，使用默認配置
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
		// 如果無法初始化日誌，使用stderr fallback
		logging.Error("無法初始化日誌系統", err)
	}

	// 初始化國際化系統
	if err := i18n.InitI18n(cfg.TranslationsDir); err != nil {
		logging.Error("初始化國際化系統失敗", err)
	} else {
		// 設置配置中指定的語言
		i18n.SetLocale(i18n.Locale(cfg.Language))
	}

	logger := logging.GetLogger("main")
	logger.Info("應用程序啟動", map[string]interface{}{
		"log_level":        cfg.LogLevel,
		"log_format":       cfg.LogFormat,
		"log_directory":    cfg.LogDirectory,
		"language":         cfg.Language,
		"translations_dir": cfg.TranslationsDir,
	})

	if err != nil {
		logger.Warn("載入配置失敗，使用默認配置", map[string]interface{}{
			"error": err.Error(),
		})
	} else {
		logger.Info("配置載入成功")
	}

	// 確保必要的目錄存在
	if err := cfg.EnsureDirectories(); err != nil {
		logger.Fatal("無法創建必要目錄", err)
		os.Exit(1)
	}

	logger.Info("目錄初始化完成", map[string]interface{}{
		"input_dir":   cfg.InputDir,
		"output_dir":  cfg.OutputDir,
		"operate_dir": cfg.OperateDir,
	})

	// 創建並運行GUI應用程式
	logger.Info("啟動 GUI 應用程序")
	app := gui.NewApp(cfg)
	app.Run()
}
