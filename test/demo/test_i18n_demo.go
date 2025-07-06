package main

import (
	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
	"fmt"
	"os"
)

// 示範國際化功能
func main() {
	// 初始化日誌
	if err := logging.InitLogger(logging.LevelInfo, "./logs", false); err != nil {
		fmt.Printf("日誌初始化失敗: %v\n", err)
		os.Exit(1)
	}

	logger := logging.GetLogger("i18n_demo")
	logger.Info("國際化功能示範開始")

	// 初始化配置
	cfg := config.DefaultConfig()

	// 初始化國際化系統
	if err := i18n.InitI18n(cfg.TranslationsDir); err != nil {
		logger.Error("初始化國際化系統失敗", err)
		return
	}

	// 保存翻譯文件到磁盤（供用戶自定義）
	if err := os.MkdirAll(cfg.TranslationsDir, 0755); err != nil {
		logger.Error("創建翻譯目錄失敗", err)
		return
	}

	// 使用內部 API 保存翻譯文件
	globalI18n := &i18n.I18n{}
	if err := globalI18n.LoadTranslations("./nonexistent"); err != nil {
		logger.Error("載入內建翻譯失敗", err)
		return
	}
	if err := globalI18n.SaveTranslations(cfg.TranslationsDir); err != nil {
		logger.Error("保存翻譯文件失敗", err)
		return
	}

	fmt.Println("=== EMG 數據分析工具 - 國際化功能示範 ===")
	fmt.Printf("翻譯文件已保存到: %s\n", cfg.TranslationsDir)
	fmt.Println()

	// 示範各種語言
	locales := i18n.GetSupportedLocales()

	for _, locale := range locales {
		fmt.Printf("=== %s (%s) ===\n", i18n.GetLocaleName(locale), locale)
		i18n.SetLocale(locale)

		// 應用標題
		fmt.Printf("應用標題: %s\n", i18n.T("app.title"))

		// 主要功能
		fmt.Printf("功能模塊:\n")
		fmt.Printf("  - %s\n", i18n.T("tab.maxmean"))
		fmt.Printf("  - %s\n", i18n.T("tab.normalize"))
		fmt.Printf("  - %s\n", i18n.T("tab.phase"))
		fmt.Printf("  - %s\n", i18n.T("tab.settings"))

		// 常用按鈕
		fmt.Printf("常用按鈕: %s | %s | %s | %s\n",
			i18n.T("button.browse"),
			i18n.T("button.calculate"),
			i18n.T("button.save"),
			i18n.T("button.cancel"))

		// 錯誤訊息示例
		fmt.Printf("錯誤訊息示例:\n")
		fmt.Printf("  - %s\n", i18n.T("error.file_not_found"))
		fmt.Printf("  - %s\n", i18n.T("error.file_too_large"))
		fmt.Printf("  - %s\n", i18n.T("error.calculation_failed"))

		// 狀態訊息（帶參數）
		fmt.Printf("動態狀態: %s\n", i18n.T("status.large_file_processing", 65.3))

		// 對話框標題
		fmt.Printf("對話框: %s | %s | %s\n",
			i18n.T("dialog.error"),
			i18n.T("dialog.success"),
			i18n.T("dialog.warning"))

		fmt.Println()
	}

	// 示範系統語言環境檢測
	fmt.Println("=== 系統語言環境檢測 ===")
	detectedLocale := globalI18n.DetectSystemLocale()
	fmt.Printf("檢測到的系統語言: %s (%s)\n", i18n.GetLocaleName(detectedLocale), detectedLocale)

	// 設置為檢測到的語言
	i18n.SetLocale(detectedLocale)
	fmt.Printf("使用檢測語言顯示歡迎訊息: %s\n", i18n.T("app.title"))
	fmt.Println()

	// 示範表單標籤翻譯
	fmt.Println("=== 表單標籤示例 ===")
	i18n.SetLocale(i18n.LocaleEnUS) // 使用英文示範
	fmt.Println("英文表單標籤:")
	fmt.Printf("  %s: [選擇檔案]\n", i18n.T("label.file_path"))
	fmt.Printf("  %s: [100]\n", i18n.T("label.window_size"))
	fmt.Printf("  %s: [0.0]\n", i18n.T("label.start_range"))
	fmt.Printf("  %s: [10.0]\n", i18n.T("label.end_range"))
	fmt.Printf("  %s: [10]\n", i18n.T("label.scaling_factor"))
	fmt.Println()

	// 示範處理模式翻譯
	fmt.Println("=== 處理模式翻譯 ===")
	for _, locale := range locales {
		i18n.SetLocale(locale)
		fmt.Printf("%s: %s | %s\n",
			i18n.GetLocaleName(locale),
			i18n.T("label.single_file"),
			i18n.T("label.batch_folder"))
	}
	fmt.Println()

	// 示範幫助文字
	fmt.Println("=== 幫助文字示例 ===")
	i18n.SetLocale(i18n.LocaleZhTW)
	fmt.Printf("窗口大小幫助: %s\n", i18n.T("help.window_size"))
	fmt.Printf("時間範圍幫助: %s\n", i18n.T("help.time_range"))
	fmt.Printf("縮放因子幫助: %s\n", i18n.T("help.scaling_factor"))
	fmt.Println()

	// 顯示功能特色
	fmt.Println("=== 國際化功能特色 ===")
	fmt.Println("✓ 支持4種語言: 繁體中文、簡體中文、英文、日文")
	fmt.Println("✓ 自動檢測系統語言環境")
	fmt.Println("✓ 支持動態語言切換")
	fmt.Println("✓ 支持參數化翻譯")
	fmt.Println("✓ 翻譯文件可外部自定義")
	fmt.Println("✓ 內建完整的翻譯庫")
	fmt.Println("✓ 支持 fallback 機制")
	fmt.Println("✓ 線程安全的翻譯操作")
	fmt.Println("✓ 統一的全局翻譯接口")

	logger.Info("國際化功能示範完成", map[string]interface{}{
		"supported_locales": len(locales),
		"detected_locale":   detectedLocale,
		"translations_dir":  cfg.TranslationsDir,
	})

	fmt.Println("\n國際化功能示範完成！")
	fmt.Printf("翻譯文件位置: %s\n", cfg.TranslationsDir)
	fmt.Println("您可以編輯翻譯文件來自定義應用的語言顯示。")
}
