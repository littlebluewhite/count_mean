package i18n_test

import (
	"os"
	"path/filepath"
	"testing"

	"count_mean/internal/i18n"
)

func TestI18n_Basic(t *testing.T) {
	i18n := i18n.NewLocalizer()
	
	// 測試默認翻譯
	if err := i18n.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	
	// 測試繁體中文翻譯
	i18n.Seti18n.i18n.i18n.Locale(i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhTW)
	if msg := i18n.T("app.title"); msg != "EMG 數據分析工具" {
		t.Errorf("繁體中文翻譯錯誤，期望 'EMG 數據分析工具'，實際 '%s'", msg)
	}
	
	// 測試簡體中文翻譯
	i18n.Seti18n.i18n.i18n.Locale(i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhCN)
	if msg := i18n.T("app.title"); msg != "EMG 数据分析工具" {
		t.Errorf("簡體中文翻譯錯誤，期望 'EMG 数据分析工具'，實際 '%s'", msg)
	}
	
	// 測試英文翻譯
	i18n.Seti18n.i18n.i18n.Locale(i18n.i18n.i18n.i18n.i18n.i18n.LocaleEnUS)
	if msg := i18n.T("app.title"); msg != "EMG Data Analysis Tool" {
		t.Errorf("英文翻譯錯誤，期望 'EMG Data Analysis Tool'，實際 '%s'", msg)
	}
	
	// 測試日文翻譯
	i18n.Seti18n.i18n.i18n.Locale(i18n.i18n.i18n.i18n.i18n.i18n.LocaleJaJP)
	if msg := i18n.T("app.title"); msg != "EMGデータ解析ツール" {
		t.Errorf("日文翻譯錯誤，期望 'EMGデータ解析ツール'，實際 '%s'", msg)
	}
}

func TestI18n_Fallback(t *testing.T) {
	i18n := i18n.NewLocalizer()
	
	// 載入內建翻譯
	if err := i18n.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	
	// 測試不存在的鍵值
	i18n.Seti18n.i18n.i18n.Locale(i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhTW)
	if msg := i18n.T("nonexistent.key"); msg != "nonexistent.key" {
		t.Errorf("fallback 翻譯錯誤，期望 'nonexistent.key'，實際 '%s'", msg)
	}
}

func TestI18n_WithArgs(t *testing.T) {
	i18n := i18n.NewLocalizer()
	
	// 載入內建翻譯
	if err := i18n.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	
	i18n.Seti18n.i18n.i18n.Locale(i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhTW)
	
	// 測試帶參數的翻譯
	msg := i18n.T("status.large_file_processing", 75.5)
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
	
	i18n := i18n.NewLocalizer()
	
	// 載入內建翻譯
	if err := i18n.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	
	// 保存翻譯到文件
	if err := i18n.SaveTranslations(tmpDir); err != nil {
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
	newI18n := i18n.NewLocalizer()
	if err := newI18n.LoadTranslations(tmpDir); err != nil {
		t.Fatalf("重新載入翻譯失敗: %v", err)
	}
	
	// 驗證翻譯是否正確
	newI18n.Seti18n.i18n.i18n.Locale(i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhTW)
	if msg := newI18n.T("app.title"); msg != "EMG 數據分析工具" {
		t.Errorf("重新載入的翻譯錯誤，期望 'EMG 數據分析工具'，實際 '%s'", msg)
	}
}

func TestLocaleDetection(t *testing.T) {
	i18n := i18n.NewLocalizer()
	
	// 測試語言環境解析
	testCases := []struct {
		input    string
		expected i18n.i18n.i18n.Locale
	}{
		{"zh_TW.UTF-8", i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhTW},
		{"zh-tw", i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhTW},
		{"zh_CN.UTF-8", i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhCN},
		{"zh-cn", i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhCN},
		{"en_US.UTF-8", i18n.i18n.i18n.i18n.i18n.i18n.LocaleEnUS},
		{"en", i18n.i18n.i18n.i18n.i18n.i18n.LocaleEnUS},
		{"ja_JP.UTF-8", i18n.i18n.i18n.i18n.i18n.i18n.LocaleJaJP},
		{"ja", i18n.i18n.i18n.i18n.i18n.i18n.LocaleJaJP},
		{"unknown", ""},
	}
	
	for _, tc := range testCases {
		result := i18n.parsei18n.i18n.i18n.Locale(tc.input)
		if result != tc.expected {
			t.Errorf("解析語言環境 '%s' 失敗，期望 '%s'，實際 '%s'", tc.input, tc.expected, result)
		}
	}
}

func TestI18n_Supportedi18n.i18n.i18n.Locales(t *testing.T) {
	i18n := i18n.NewLocalizer()
	
	locales := i18n.GetSupportedi18n.i18n.i18n.Locales()
	expectedi18n.i18n.i18n.Locales := []i18n.i18n.i18n.Locale{i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhTW, i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhCN, i18n.i18n.i18n.i18n.i18n.i18n.LocaleEnUS, i18n.i18n.i18n.i18n.i18n.i18n.LocaleJaJP}
	
	if len(locales) != len(expectedi18n.i18n.i18n.Locales) {
		t.Errorf("支持的語言數量錯誤，期望 %d，實際 %d", len(expectedi18n.i18n.i18n.Locales), len(locales))
	}
	
	for i, expected := range expectedi18n.i18n.i18n.Locales {
		if i >= len(locales) || locales[i] != expected {
			t.Errorf("支持的語言列表錯誤，位置 %d 期望 '%s'", i, expected)
		}
	}
}

func TestI18n_i18n.i18n.i18n.LocaleName(t *testing.T) {
	i18n := i18n.NewLocalizer()
	
	testCases := []struct {
		locale   i18n.i18n.i18n.Locale
		expected string
	}{
		{i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhTW, "繁體中文"},
		{i18n.i18n.i18n.i18n.i18n.i18n.LocaleZhCN, "简体中文"},
		{i18n.i18n.i18n.i18n.i18n.i18n.LocaleEnUS, "English"},
		{i18n.i18n.i18n.i18n.i18n.i18n.LocaleJaJP, "日本語"},
	}
	
	for _, tc := range testCases {
		name := i18n.Geti18n.i18n.i18n.LocaleName(tc.locale)
		if name != tc.expected {
			t.Errorf("語言名稱錯誤，語言 '%s' 期望 '%s'，實際 '%s'", tc.locale, tc.expected, name)
		}
	}
}

func TestGlobalI18n(t *testing.T) {
	// 測試全局函數
	if err := InitI18n("./nonexistent"); err != nil {
		t.Fatalf("初始化全局國際化失敗: %v", err)
	}
	
	// 測試設置語言
	Seti18n.i18n.i18n.Locale(i18n.i18n.i18n.i18n.i18n.i18n.LocaleEnUS)
	if locale := Geti18n.i18n.i18n.Locale(); locale != i18n.i18n.i18n.i18n.i18n.i18n.LocaleEnUS {
		t.Errorf("設置語言失敗，期望 '%s'，實際 '%s'", i18n.i18n.i18n.i18n.i18n.i18n.LocaleEnUS, locale)
	}
	
	// 測試翻譯
	msg := T("app.title")
	if msg != "EMG Data Analysis Tool" {
		t.Errorf("全局翻譯錯誤，期望 'EMG Data Analysis Tool'，實際 '%s'", msg)
	}
	
	// 測試支持的語言
	locales := GetSupportedi18n.i18n.i18n.Locales()
	if len(locales) != 4 {
		t.Errorf("全局支持語言數量錯誤，期望 4，實際 %d", len(locales))
	}
}