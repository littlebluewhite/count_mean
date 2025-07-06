package main

import (
	"count_mean/internal/config"
	"count_mean/internal/errors"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
	"fmt"
	"os"
	"testing"
)

// 國際化集成測試
func TestI18nIntegration(t *testing.T) {
	// 初始化日誌
	if err := logging.InitLogger(logging.LevelInfo, "./logs", false); err != nil {
		t.Fatalf("日誌初始化失敗: %v", err)
	}

	logger := logging.GetLogger("i18n_integration_test")
	logger.Info("國際化集成測試開始")

	// 創建測試配置
	cfg := config.DefaultConfig()
	cfg.Language = "en-US"
	cfg.TranslationsDir = "./test_translations"

	// 確保測試目錄存在
	if err := os.MkdirAll(cfg.TranslationsDir, 0755); err != nil {
		t.Fatalf("無法創建測試目錄: %v", err)
	}
	defer os.RemoveAll(cfg.TranslationsDir)

	// 初始化國際化系統
	if err := i18n.InitI18n(cfg.TranslationsDir); err != nil {
		t.Fatalf("初始化國際化系統失敗: %v", err)
	}

	// 設置語言
	i18n.SetLocale(i18n.Locale(cfg.Language))

	// 測試基本翻譯功能
	t.Run("BasicTranslation", func(t *testing.T) {
		msg := i18n.T("app.title")
		expected := "EMG Data Analysis Tool"
		if msg != expected {
			t.Errorf("基本翻譯失敗，期望 '%s'，實際 '%s'", expected, msg)
		}
	})

	// 測試錯誤訊息翻譯
	t.Run("ErrorTranslation", func(t *testing.T) {
		err := errors.GetI18nFileNotFoundError()
		expected := "File not found"
		if err.Message != expected {
			t.Errorf("錯誤翻譯失敗，期望 '%s'，實際 '%s'", expected, err.Message)
		}
	})

	// 測試動態語言切換
	t.Run("LanguageSwitching", func(t *testing.T) {
		// 切換到繁體中文
		i18n.SetLocale(i18n.LocaleZhTW)
		msg := i18n.T("app.title")
		expected := "EMG 數據分析工具"
		if msg != expected {
			t.Errorf("語言切換失敗，期望 '%s'，實際 '%s'", expected, msg)
		}

		// 切換到簡體中文
		i18n.SetLocale(i18n.LocaleZhCN)
		msg = i18n.T("app.title")
		expected = "EMG 数据分析工具"
		if msg != expected {
			t.Errorf("語言切換失敗，期望 '%s'，實際 '%s'", expected, msg)
		}
	})

	// 測試參數化翻譯
	t.Run("ParameterizedTranslation", func(t *testing.T) {
		i18n.SetLocale(i18n.LocaleEnUS)
		msg := i18n.T("status.large_file_processing", 42.5)
		expected := "Processing large file... 42.5%"
		if msg != expected {
			t.Errorf("參數化翻譯失敗，期望 '%s'，實際 '%s'", expected, msg)
		}
	})

	// 測試所有支持的語言
	t.Run("AllSupportedLanguages", func(t *testing.T) {
		locales := i18n.GetSupportedLocales()
		expectedCount := 4
		if len(locales) != expectedCount {
			t.Errorf("支持語言數量錯誤，期望 %d，實際 %d", expectedCount, len(locales))
		}

		// 測試每種語言的基本翻譯
		for _, locale := range locales {
			i18n.SetLocale(locale)
			msg := i18n.T("app.title")
			if msg == "app.title" {
				t.Errorf("語言 %s 的翻譯失敗，返回了鍵值", locale)
			}
		}
	})

	// 測試 fallback 機制
	t.Run("FallbackMechanism", func(t *testing.T) {
		i18n.SetLocale(i18n.LocaleZhTW)
		msg := i18n.T("nonexistent.key")
		expected := "nonexistent.key"
		if msg != expected {
			t.Errorf("Fallback 機制失敗，期望 '%s'，實際 '%s'", expected, msg)
		}
	})

	logger.Info("國際化集成測試完成")
}

// 如果直接運行此文件，執行測試
func main() {
	if len(os.Args) > 1 && os.Args[1] == "test" {
		testing.Main(func(pat, str string) (bool, error) { return true, nil },
			[]testing.InternalTest{
				{"TestI18nIntegration", TestI18nIntegration},
			},
			[]testing.InternalBenchmark{},
			[]testing.InternalExample{})
	} else {
		// 運行演示
		runI18nDemo()
	}
}

func runI18nDemo() {
	fmt.Println("=== 國際化集成測試演示 ===")

	// 初始化日誌
	if err := logging.InitLogger(logging.LevelInfo, "./logs", false); err != nil {
		fmt.Printf("日誌初始化失敗: %v\n", err)
		return
	}

	// 初始化配置
	cfg := config.DefaultConfig()

	// 初始化國際化
	if err := i18n.InitI18n(cfg.TranslationsDir); err != nil {
		fmt.Printf("初始化國際化系統失敗: %v\n", err)
		return
	}

	// 演示不同語言的錯誤訊息
	locales := i18n.GetSupportedLocales()

	fmt.Println("=== 多語言錯誤訊息演示 ===")
	for _, locale := range locales {
		i18n.SetLocale(locale)
		fmt.Printf("\n語言: %s (%s)\n", i18n.GetLocaleName(locale), locale)

		// 文件相關錯誤
		fmt.Printf("  檔案未找到: %s\n", errors.GetI18nFileNotFoundError().Message)
		fmt.Printf("  檔案過大: %s\n", errors.GetI18nFileTooLargeError().Message)
		fmt.Printf("  無效CSV: %s\n", errors.GetI18nInvalidCSVError().Message)

		// 計算相關錯誤
		fmt.Printf("  計算失敗: %s\n", errors.GetI18nCalculationFailedError().Message)
		fmt.Printf("  資料不足: %s\n", errors.GetI18nInsufficientDataError().Message)
		fmt.Printf("  記憶體不足: %s\n", errors.GetI18nMemoryLimitError().Message)
	}

	fmt.Println("\n=== 多語言界面元素演示 ===")
	for _, locale := range locales {
		i18n.SetLocale(locale)
		fmt.Printf("\n語言: %s\n", i18n.GetLocaleName(locale))
		fmt.Printf("  按鈕: %s, %s, %s\n",
			i18n.T("button.calculate"),
			i18n.T("button.save"),
			i18n.T("button.cancel"))
		fmt.Printf("  狀態: %s\n", i18n.T("status.processing"))
		fmt.Printf("  對話框: %s, %s\n",
			i18n.T("dialog.error"),
			i18n.T("dialog.success"))
	}

	fmt.Println("\n國際化集成測試演示完成！")
}
