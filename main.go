package main

import (
	"count_mean/gui"
	"count_mean/internal/config"
	"log"
)

func main() {
	// 載入配置
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Printf("載入配置失敗，使用默認配置: %v", err)
		cfg = config.DefaultConfig()
	}

	// 確保必要的目錄存在
	if err := cfg.EnsureDirectories(); err != nil {
		log.Fatalf("無法創建必要目錄: %v", err)
	}

	// 創建並運行GUI應用程式
	app := gui.NewApp(cfg)
	app.Run()
}
