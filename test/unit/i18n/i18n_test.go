package i18n_test

import (
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/i18n"
)

func TestI18n_Basic(t *testing.T) {
	i18nInstance := i18n.NewI18n()
	
	// 測試默認翻譯
	if err := i18nInstance.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	
	// 測試繁體中文翻譯
	i18nInstance.SetLocale(i18n.LocaleZhTW)
	if msg := i18nInstance.T("app.title"); msg != "EMG 數據分析工具" {
		t.Errorf("繁體中文翻譯錯誤，期望 'EMG 數據分析工具'，實際 '%s'", msg)
	}
	
	// 測試簡體中文翻譯
	i18nInstance.SetLocale(i18n.LocaleZhCN)
	if msg := i18nInstance.T("app.title"); msg != "EMG 数据分析工具" {
		t.Errorf("簡體中文翻譯錯誤，期望 'EMG 数据分析工具'，實際 '%s'", msg)
	}
	
	// 測試英文翻譯
	i18nInstance.SetLocale(i18n.LocaleEnUS)
	if msg := i18nInstance.T("app.title"); msg != "EMG Data Analysis Tool" {
		t.Errorf("英文翻譯錯誤，期望 'EMG Data Analysis Tool'，實際 '%s'", msg)
	}
	
	// 測試日文翻譯
	i18nInstance.SetLocale(i18n.LocaleJaJP)
	if msg := i18nInstance.T("app.title"); msg != "EMGデータ解析ツール" {
		t.Errorf("日文翻譯錯誤，期望 'EMGデータ解析ツール'，實際 '%s'", msg)
	}
}

func TestI18n_Fallback(t *testing.T) {
	i18nInstance := i18n.NewI18n()
	
	// 載入內建翻譯
	if err := i18nInstance.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	
	// 測試不存在的鍵值
	i18nInstance.SetLocale(i18n.LocaleZhTW)
	if msg := i18nInstance.T("nonexistent.key"); msg != "nonexistent.key" {
		t.Errorf("fallback 翻譯錯誤，期望 'nonexistent.key'，實際 '%s'", msg)
	}
}

func TestI18n_WithArgs(t *testing.T) {
	i18nInstance := i18n.NewI18n()
	
	// 載入內建翻譯
	if err := i18nInstance.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	
	i18nInstance.SetLocale(i18n.LocaleZhTW)
	
	// 測試帶參數的翻譯
	msg := i18nInstance.T("status.large_file_processing", 75.5)
	expected := "處理大文件中... 75.5%"
	if msg != expected {
		t.Errorf("帶參數翻譯錯誤，期望 '%s'，實際 '%s'", expected, msg)
	}
}

func TestI18n_FileOperations(t *testing.T) {
	// 創建臨時目錄
	tmpDir := "./test_translations"
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		t.Fatalf("無法創建測試目錄: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	
	i18nInstance := i18n.NewI18n()
	
	// 載入內建翻譯
	if err := i18nInstance.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	
	// 保存翻譯到文件
	if err := i18nInstance.SaveTranslations(tmpDir); err != nil {
		t.Fatalf("保存翻譯失敗: %v", err)
	}
	
	// 檢查文件是否存在
	expectedFiles := []string{"zh-TW.json", "zh-CN.json", "en-US.json", "ja-JP.json"}
	for _, filename := range expectedFiles {
		filePath := filepath.Join(tmpDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("翻譯文件 %s 未創建", filename)
		}
	}
	
	// 重新載入翻譯
	newI18nInstance := i18n.NewI18n()
	if err := newI18nInstance.LoadTranslations(tmpDir); err != nil {
		t.Fatalf("重新載入翻譯失敗: %v", err)
	}
	
	// 驗證翻譯是否正確
	newI18nInstance.SetLocale(i18n.LocaleZhTW)
	if msg := newI18nInstance.T("app.title"); msg != "EMG 數據分析工具" {
		t.Errorf("重新載入的翻譯錯誤，期望 'EMG 數據分析工具'，實際 '%s'", msg)
	}
}

func TestLocaleDetection(t *testing.T) {
	i18nInstance := i18n.NewI18n()
	
	// 測試系統語言環境檢測
	locale := i18nInstance.DetectSystemLocale()
	
	// 確保返回的是支持的語言
	supportedLocales := i18nInstance.GetSupportedLocales()
	found := false
	for _, supported := range supportedLocales {
		if locale == supported {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("系統語言檢測返回了不支持的語言: '%s'", locale)
	}
}

func TestI18n_SupportedLocales(t *testing.T) {
	i18nInstance := i18n.NewI18n()
	
	locales := i18nInstance.GetSupportedLocales()
	expectedLocales := []i18n.Locale{i18n.LocaleZhTW, i18n.LocaleZhCN, i18n.LocaleEnUS, i18n.LocaleJaJP}
	
	if len(locales) != len(expectedLocales) {
		t.Errorf("支持的語言數量錯誤，期望 %d，實際 %d", len(expectedLocales), len(locales))
	}
	
	for i, expected := range expectedLocales {
		if i >= len(locales) || locales[i] != expected {
			t.Errorf("支持的語言列表錯誤，位置 %d 期望 '%s'", i, expected)
		}
	}
}

func TestI18n_LocaleName(t *testing.T) {
	i18nInstance := i18n.NewI18n()
	
	testCases := []struct {
		locale   i18n.Locale
		expected string
	}{
		{i18n.LocaleZhTW, "繁體中文"},
		{i18n.LocaleZhCN, "简体中文"},
		{i18n.LocaleEnUS, "English"},
		{i18n.LocaleJaJP, "日本語"},
	}
	
	for _, tc := range testCases {
		name := i18nInstance.GetLocaleName(tc.locale)
		if name != tc.expected {
			t.Errorf("語言名稱錯誤，語言 '%s' 期望 '%s'，實際 '%s'", tc.locale, tc.expected, name)
		}
	}
}

func TestGlobalI18n(t *testing.T) {
	// 測試全局函數
	if err := i18n.InitI18n("./nonexistent"); err != nil {
		t.Fatalf("初始化全局國際化失敗: %v", err)
	}
	
	// 測試設置語言
	i18n.SetLocale(i18n.LocaleEnUS)
	if locale := i18n.GetLocale(); locale != i18n.LocaleEnUS {
		t.Errorf("設置語言失敗，期望 '%s'，實際 '%s'", i18n.LocaleEnUS, locale)
	}
	
	// 測試翻譯
	msg := i18n.T("app.title")
	if msg != "EMG Data Analysis Tool" {
		t.Errorf("全局翻譯錯誤，期望 'EMG Data Analysis Tool'，實際 '%s'", msg)
	}
	
	// 測試支持的語言
	locales := i18n.GetSupportedLocales()
	if len(locales) != 4 {
		t.Errorf("全局支持語言數量錯誤，期望 4，實際 %d", len(locales))
	}
}