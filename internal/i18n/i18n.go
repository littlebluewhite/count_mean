package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Locale represents a supported locale
type Locale string

const (
	LocaleZhTW Locale = "zh-TW" // 繁體中文（台灣）
	LocaleZhCN Locale = "zh-CN" // 簡體中文（中國）
	LocaleEnUS Locale = "en-US" // 英文（美國）
	LocaleJaJP Locale = "ja-JP" // 日文（日本）
)

// I18n manages internationalization
type I18n struct {
	currentLocale Locale
	messages      map[Locale]map[string]string
	mutex         sync.RWMutex
	fallback      Locale
}

// NewI18n creates a new internationalization manager
func NewI18n() *I18n {
	return &I18n{
		currentLocale: LocaleZhTW, // 默認使用繁體中文
		messages:      make(map[Locale]map[string]string),
		fallback:      LocaleZhTW,
	}
}

// LoadTranslations loads translation files from a directory
func (i *I18n) LoadTranslations(translationsDir string) error {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	// Load each supported locale
	locales := []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}

	for _, locale := range locales {
		filename := filepath.Join(translationsDir, fmt.Sprintf("%s.json", locale))

		// Check if file exists
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			// Use built-in translations if file doesn't exist
			i.messages[locale] = i.getBuiltinTranslations(locale)
			continue
		}

		// Load from file
		data, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("無法讀取翻譯文件 %s: %w", filename, err)
		}

		var translations map[string]string
		if err := json.Unmarshal(data, &translations); err != nil {
			return fmt.Errorf("解析翻譯文件 %s 失敗: %w", filename, err)
		}

		i.messages[locale] = translations
	}

	return nil
}

// SetLocale sets the current locale
func (i *I18n) SetLocale(locale Locale) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.currentLocale = locale
}

// GetLocale returns the current locale
func (i *I18n) GetLocale() Locale {
	i.mutex.RLock()
	defer i.mutex.RUnlock()
	return i.currentLocale
}

// T translates a message key
func (i *I18n) T(key string, args ...interface{}) string {
	i.mutex.RLock()
	defer i.mutex.RUnlock()

	// Try current locale first
	if messages, exists := i.messages[i.currentLocale]; exists {
		if message, found := messages[key]; found {
			if len(args) > 0 {
				return fmt.Sprintf(message, args...)
			}
			return message
		}
	}

	// Fallback to default locale
	if i.currentLocale != i.fallback {
		if messages, exists := i.messages[i.fallback]; exists {
			if message, found := messages[key]; found {
				if len(args) > 0 {
					return fmt.Sprintf(message, args...)
				}
				return message
			}
		}
	}

	// Return key if no translation found
	if len(args) > 0 {
		return fmt.Sprintf("%s: %v", key, args)
	}
	return key
}

// GetSupportedLocales returns list of supported locales
func (i *I18n) GetSupportedLocales() []Locale {
	return []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}
}

// GetLocaleName returns the display name of a locale
func (i *I18n) GetLocaleName(locale Locale) string {
	switch locale {
	case LocaleZhTW:
		return "繁體中文"
	case LocaleZhCN:
		return "简体中文"
	case LocaleEnUS:
		return "English"
	case LocaleJaJP:
		return "日本語"
	default:
		return string(locale)
	}
}

// DetectSystemLocale attempts to detect the system locale
func (i *I18n) DetectSystemLocale() Locale {
	// Check environment variables
	envVars := []string{"LC_ALL", "LC_MESSAGES", "LANG"}

	for _, envVar := range envVars {
		if value := os.Getenv(envVar); value != "" {
			locale := i.parseLocale(value)
			if locale != "" {
				return locale
			}
		}
	}

	// Default fallback
	return LocaleZhTW
}

// parseLocale parses locale string and returns supported locale
func (i *I18n) parseLocale(localeStr string) Locale {
	localeStr = strings.ToLower(localeStr)

	// Handle common variations
	if strings.HasPrefix(localeStr, "zh_tw") || strings.HasPrefix(localeStr, "zh-tw") {
		return LocaleZhTW
	}
	if strings.HasPrefix(localeStr, "zh_cn") || strings.HasPrefix(localeStr, "zh-cn") {
		return LocaleZhCN
	}
	if strings.HasPrefix(localeStr, "en") {
		return LocaleEnUS
	}
	if strings.HasPrefix(localeStr, "ja") {
		return LocaleJaJP
	}

	return ""
}

// getBuiltinTranslations returns built-in translations for a locale
func (i *I18n) getBuiltinTranslations(locale Locale) map[string]string {
	switch locale {
	case LocaleZhTW:
		return map[string]string{
			// UI 界面
			"app.title":        "EMG 數據分析工具",
			"menu.file":        "檔案",
			"menu.settings":    "設定",
			"menu.help":        "幫助",
			"button.browse":    "瀏覽",
			"button.calculate": "計算",
			"button.cancel":    "取消",
			"button.save":      "保存",
			"button.reset":     "重置",
			"button.ok":        "確定",

			// 功能標籤
			"tab.maxmean":   "最大平均值計算",
			"tab.normalize": "資料標準化",
			"tab.phase":     "階段分析",
			"tab.settings":  "配置設定",

			// 表單標籤
			"label.processing_mode": "處理模式",
			"label.single_file":     "處理單一檔案",
			"label.batch_folder":    "批量處理資料夾",
			"label.file_path":       "檔案路徑",
			"label.folder_path":     "資料夾路徑",
			"label.window_size":     "窗口大小",
			"label.start_range":     "開始範圍秒數",
			"label.end_range":       "結束範圍秒數",
			"label.main_file":       "主要資料檔案",
			"label.reference_file":  "參考資料檔案",
			"label.output_name":     "輸出檔名",
			"label.phase_labels":    "階段標籤",
			"label.scaling_factor":  "縮放因子",
			"label.precision":       "精度",
			"label.output_format":   "輸出格式",
			"label.bom_enabled":     "啟用 BOM",
			"label.input_dir":       "輸入目錄",
			"label.output_dir":      "輸出目錄",
			"label.operate_dir":     "操作目錄",
			"label.language":        "語言",

			// 錯誤訊息
			"error.file_not_found":     "檔案未找到",
			"error.invalid_path":       "無效的檔案路徑",
			"error.invalid_csv":        "無效的 CSV 檔案格式",
			"error.file_too_large":     "檔案過大，請使用大文件處理功能",
			"error.insufficient_data":  "資料不足",
			"error.calculation_failed": "計算失敗",
			"error.memory_limit":       "記憶體不足",
			"error.validation_failed":  "驗證失敗",

			// 成功訊息
			"success.calculation_complete": "計算成功完成",
			"success.file_saved":           "檔案已成功保存",
			"success.settings_saved":       "設定已成功保存",

			// 狀態訊息
			"status.ready":                 "就緒",
			"status.processing":            "處理中...",
			"status.loading":               "載入中...",
			"status.saving":                "保存中...",
			"status.complete":              "完成",
			"status.large_file_processing": "處理大文件中... %.1f%%",

			// 對話框標題
			"dialog.select_file":   "選擇檔案",
			"dialog.select_folder": "選擇資料夾",
			"dialog.error":         "錯誤",
			"dialog.success":       "成功",
			"dialog.warning":       "警告",
			"dialog.info":          "資訊",

			// 幫助文字
			"help.window_size":    "滑動窗口的大小（數據點數）",
			"help.time_range":     "指定分析的時間範圍（秒）",
			"help.scaling_factor": "數據縮放因子（10的冪次）",
			"help.phase_labels":   "用換行分隔的階段標籤",
		}

	case LocaleZhCN:
		return map[string]string{
			// UI 界面
			"app.title":        "EMG 数据分析工具",
			"menu.file":        "文件",
			"menu.settings":    "设置",
			"menu.help":        "帮助",
			"button.browse":    "浏览",
			"button.calculate": "计算",
			"button.cancel":    "取消",
			"button.save":      "保存",
			"button.reset":     "重置",
			"button.ok":        "确定",

			// 功能标签
			"tab.maxmean":   "最大平均值计算",
			"tab.normalize": "数据标准化",
			"tab.phase":     "阶段分析",
			"tab.settings":  "配置设置",

			// 表单标签
			"label.processing_mode": "处理模式",
			"label.single_file":     "处理单一文件",
			"label.batch_folder":    "批量处理文件夹",
			"label.file_path":       "文件路径",
			"label.folder_path":     "文件夹路径",
			"label.window_size":     "窗口大小",
			"label.start_range":     "开始范围秒数",
			"label.end_range":       "结束范围秒数",
			"label.main_file":       "主要数据文件",
			"label.reference_file":  "参考数据文件",
			"label.output_name":     "输出文件名",
			"label.phase_labels":    "阶段标签",
			"label.scaling_factor":  "缩放因子",
			"label.precision":       "精度",
			"label.output_format":   "输出格式",
			"label.bom_enabled":     "启用 BOM",
			"label.input_dir":       "输入目录",
			"label.output_dir":      "输出目录",
			"label.operate_dir":     "操作目录",
			"label.language":        "语言",

			// 错误信息
			"error.file_not_found":     "文件未找到",
			"error.invalid_path":       "无效的文件路径",
			"error.invalid_csv":        "无效的 CSV 文件格式",
			"error.file_too_large":     "文件过大，请使用大文件处理功能",
			"error.insufficient_data":  "数据不足",
			"error.calculation_failed": "计算失败",
			"error.memory_limit":       "内存不足",
			"error.validation_failed":  "验证失败",

			// 成功信息
			"success.calculation_complete": "计算成功完成",
			"success.file_saved":           "文件已成功保存",
			"success.settings_saved":       "设置已成功保存",

			// 状态信息
			"status.ready":                 "就绪",
			"status.processing":            "处理中...",
			"status.loading":               "加载中...",
			"status.saving":                "保存中...",
			"status.complete":              "完成",
			"status.large_file_processing": "处理大文件中... %.1f%%",

			// 对话框标题
			"dialog.select_file":   "选择文件",
			"dialog.select_folder": "选择文件夹",
			"dialog.error":         "错误",
			"dialog.success":       "成功",
			"dialog.warning":       "警告",
			"dialog.info":          "信息",

			// 帮助文字
			"help.window_size":    "滑动窗口的大小（数据点数）",
			"help.time_range":     "指定分析的时间范围（秒）",
			"help.scaling_factor": "数据缩放因子（10的幂次）",
			"help.phase_labels":   "用换行分隔的阶段标签",
		}

	case LocaleEnUS:
		return map[string]string{
			// UI Interface
			"app.title":        "EMG Data Analysis Tool",
			"menu.file":        "File",
			"menu.settings":    "Settings",
			"menu.help":        "Help",
			"button.browse":    "Browse",
			"button.calculate": "Calculate",
			"button.cancel":    "Cancel",
			"button.save":      "Save",
			"button.reset":     "Reset",
			"button.ok":        "OK",

			// Function Tabs
			"tab.maxmean":   "Max Mean Calculation",
			"tab.normalize": "Data Normalization",
			"tab.phase":     "Phase Analysis",
			"tab.settings":  "Configuration",

			// Form Labels
			"label.processing_mode": "Processing Mode",
			"label.single_file":     "Process Single File",
			"label.batch_folder":    "Batch Process Folder",
			"label.file_path":       "File Path",
			"label.folder_path":     "Folder Path",
			"label.window_size":     "Window Size",
			"label.start_range":     "Start Range (seconds)",
			"label.end_range":       "End Range (seconds)",
			"label.main_file":       "Main Data File",
			"label.reference_file":  "Reference Data File",
			"label.output_name":     "Output Filename",
			"label.phase_labels":    "Phase Labels",
			"label.scaling_factor":  "Scaling Factor",
			"label.precision":       "Precision",
			"label.output_format":   "Output Format",
			"label.bom_enabled":     "Enable BOM",
			"label.input_dir":       "Input Directory",
			"label.output_dir":      "Output Directory",
			"label.operate_dir":     "Operation Directory",
			"label.language":        "Language",

			// Error Messages
			"error.file_not_found":     "File not found",
			"error.invalid_path":       "Invalid file path",
			"error.invalid_csv":        "Invalid CSV file format",
			"error.file_too_large":     "File too large, please use large file processing",
			"error.insufficient_data":  "Insufficient data",
			"error.calculation_failed": "Calculation failed",
			"error.memory_limit":       "Memory limit exceeded",
			"error.validation_failed":  "Validation failed",

			// Success Messages
			"success.calculation_complete": "Calculation completed successfully",
			"success.file_saved":           "File saved successfully",
			"success.settings_saved":       "Settings saved successfully",

			// Status Messages
			"status.ready":                 "Ready",
			"status.processing":            "Processing...",
			"status.loading":               "Loading...",
			"status.saving":                "Saving...",
			"status.complete":              "Complete",
			"status.large_file_processing": "Processing large file... %.1f%%",

			// Dialog Titles
			"dialog.select_file":   "Select File",
			"dialog.select_folder": "Select Folder",
			"dialog.error":         "Error",
			"dialog.success":       "Success",
			"dialog.warning":       "Warning",
			"dialog.info":          "Information",

			// Help Text
			"help.window_size":    "Size of the sliding window (number of data points)",
			"help.time_range":     "Specify the time range for analysis (seconds)",
			"help.scaling_factor": "Data scaling factor (power of 10)",
			"help.phase_labels":   "Phase labels separated by newlines",
		}

	case LocaleJaJP:
		return map[string]string{
			// UI インターフェース
			"app.title":        "EMGデータ解析ツール",
			"menu.file":        "ファイル",
			"menu.settings":    "設定",
			"menu.help":        "ヘルプ",
			"button.browse":    "参照",
			"button.calculate": "計算",
			"button.cancel":    "キャンセル",
			"button.save":      "保存",
			"button.reset":     "リセット",
			"button.ok":        "OK",

			// 機能タブ
			"tab.maxmean":   "最大平均値計算",
			"tab.normalize": "データ正規化",
			"tab.phase":     "段階解析",
			"tab.settings":  "設定",

			// フォームラベル
			"label.processing_mode": "処理モード",
			"label.single_file":     "単一ファイル処理",
			"label.batch_folder":    "フォルダ一括処理",
			"label.file_path":       "ファイルパス",
			"label.folder_path":     "フォルダパス",
			"label.window_size":     "ウィンドウサイズ",
			"label.start_range":     "開始範囲（秒）",
			"label.end_range":       "終了範囲（秒）",
			"label.main_file":       "メインデータファイル",
			"label.reference_file":  "参照データファイル",
			"label.output_name":     "出力ファイル名",
			"label.phase_labels":    "段階ラベル",
			"label.scaling_factor":  "スケーリング係数",
			"label.precision":       "精度",
			"label.output_format":   "出力形式",
			"label.bom_enabled":     "BOM有効",
			"label.input_dir":       "入力ディレクトリ",
			"label.output_dir":      "出力ディレクトリ",
			"label.operate_dir":     "操作ディレクトリ",
			"label.language":        "言語",

			// エラーメッセージ
			"error.file_not_found":     "ファイルが見つかりません",
			"error.invalid_path":       "無効なファイルパス",
			"error.invalid_csv":        "無効なCSVファイル形式",
			"error.file_too_large":     "ファイルが大きすぎます。大容量ファイル処理機能を使用してください",
			"error.insufficient_data":  "データが不足しています",
			"error.calculation_failed": "計算が失敗しました",
			"error.memory_limit":       "メモリ不足",
			"error.validation_failed":  "検証に失敗しました",

			// 成功メッセージ
			"success.calculation_complete": "計算が正常に完了しました",
			"success.file_saved":           "ファイルが正常に保存されました",
			"success.settings_saved":       "設定が正常に保存されました",

			// ステータスメッセージ
			"status.ready":                 "準備完了",
			"status.processing":            "処理中...",
			"status.loading":               "読み込み中...",
			"status.saving":                "保存中...",
			"status.complete":              "完了",
			"status.large_file_processing": "大容量ファイル処理中... %.1f%%",

			// ダイアログタイトル
			"dialog.select_file":   "ファイル選択",
			"dialog.select_folder": "フォルダ選択",
			"dialog.error":         "エラー",
			"dialog.success":       "成功",
			"dialog.warning":       "警告",
			"dialog.info":          "情報",

			// ヘルプテキスト
			"help.window_size":    "スライディングウィンドウのサイズ（データポイント数）",
			"help.time_range":     "解析の時間範囲を指定（秒）",
			"help.scaling_factor": "データスケーリング係数（10の累乗）",
			"help.phase_labels":   "改行で区切られた段階ラベル",
		}

	default:
		return make(map[string]string)
	}
}

// SaveTranslations saves current translations to files
func (i *I18n) SaveTranslations(translationsDir string) error {
	i.mutex.RLock()
	defer i.mutex.RUnlock()

	// Ensure directory exists
	if err := os.MkdirAll(translationsDir, 0755); err != nil {
		return fmt.Errorf("無法創建翻譯目錄: %w", err)
	}

	// Save each locale
	for locale, messages := range i.messages {
		filename := filepath.Join(translationsDir, fmt.Sprintf("%s.json", locale))

		data, err := json.MarshalIndent(messages, "", "  ")
		if err != nil {
			return fmt.Errorf("序列化翻譯失敗 %s: %w", locale, err)
		}

		if err := os.WriteFile(filename, data, 0644); err != nil {
			return fmt.Errorf("寫入翻譯文件失敗 %s: %w", filename, err)
		}
	}

	return nil
}

// Global instance
var globalI18n *I18n

// InitI18n initializes the global i18n instance
func InitI18n(translationsDir string) error {
	globalI18n = NewI18n()

	// Try to detect system locale
	systemLocale := globalI18n.DetectSystemLocale()
	globalI18n.SetLocale(systemLocale)

	// Load translations
	return globalI18n.LoadTranslations(translationsDir)
}

// Global functions for convenience
func T(key string, args ...interface{}) string {
	if globalI18n == nil {
		return key
	}
	return globalI18n.T(key, args...)
}

func SetLocale(locale Locale) {
	if globalI18n != nil {
		globalI18n.SetLocale(locale)
	}
}

func GetLocale() Locale {
	if globalI18n == nil {
		return LocaleZhTW
	}
	return globalI18n.GetLocale()
}

func GetSupportedLocales() []Locale {
	if globalI18n == nil {
		return []Locale{LocaleZhTW}
	}
	return globalI18n.GetSupportedLocales()
}

func GetLocaleName(locale Locale) string {
	if globalI18n == nil {
		return string(locale)
	}
	return globalI18n.GetLocaleName(locale)
}
