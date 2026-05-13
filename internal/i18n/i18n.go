// Package i18n provides internationalization support for the EMG data analysis
// application, supporting Traditional Chinese, Simplified Chinese, English,
// and Japanese locales with dynamic translation loading and fallback handling.
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"count_mean/internal/security/fsperm"
)

// Locale represents a supported locale.
type Locale string

// Locale constants for supported languages.
const (
	LocaleZhTW Locale = "zh-TW" // 繁體中文（台灣）
	LocaleZhCN Locale = "zh-CN" // 簡體中文（中國）
	LocaleEnUS Locale = "en-US" // 英文（美國）
	LocaleJaJP Locale = "ja-JP" // 日文（日本）.
)

// Translation key constants.
const (
	// UI Interface.
	KeyAppTitle        = "app.title"
	KeyMenuFile        = "menu.file"
	KeyMenuSettings    = "menu.settings"
	KeyMenuHelp        = "menu.help"
	KeyButtonBrowse    = "button.browse"
	KeyButtonCalculate = "button.calculate"
	KeyButtonCancel    = "button.cancel"
	KeyButtonSave      = "button.save"
	KeyButtonReset     = "button.reset"
	KeyButtonOK        = "button.ok"

	// Function Tabs.
	KeyTabMaxmean   = "tab.maxmean"
	KeyTabNormalize = "tab.normalize"
	KeyTabPhase     = "tab.phase"
	KeyTabSettings  = "tab.settings"

	// Form Labels.
	KeyLabelProcessingMode = "label.processing_mode"
	KeyLabelSingleFile     = "label.single_file"
	KeyLabelBatchFolder    = "label.batch_folder"
	KeyLabelFilePath       = "label.file_path"
	KeyLabelFolderPath     = "label.folder_path"
	KeyLabelWindowSize     = "label.window_size"
	KeyLabelStartRange     = "label.start_range"
	KeyLabelEndRange       = "label.end_range"
	KeyLabelMainFile       = "label.main_file"
	KeyLabelReferenceFile  = "label.reference_file"
	KeyLabelOutputName     = "label.output_name"
	KeyLabelPhaseLabels    = "label.phase_labels"
	KeyLabelScalingFactor  = "label.scaling_factor"
	KeyLabelPrecision      = "label.precision"
	KeyLabelOutputFormat   = "label.output_format"
	KeyLabelBOMEnabled     = "label.bom_enabled"
	KeyLabelInputDir       = "label.input_dir"
	KeyLabelOutputDir      = "label.output_dir"
	KeyLabelOperateDir     = "label.operate_dir"
	KeyLabelLanguage       = "label.language"

	// Error Messages.
	KeyErrorFileNotFound     = "error.file_not_found"
	KeyErrorInvalidPath      = "error.invalid_path"
	KeyErrorInvalidCSV       = "error.invalid_csv"
	KeyErrorFileTooLarge     = "error.file_too_large"
	KeyErrorInsufficientData = "error.insufficient_data"
	KeyErrorCalculationFail  = "error.calculation_failed"
	KeyErrorMemoryLimit      = "error.memory_limit"
	KeyErrorValidationFail   = "error.validation_failed"

	// Success Messages.
	KeySuccessCalcComplete = "success.calculation_complete"
	KeySuccessFileSaved    = "success.file_saved"
	KeySuccessSettingsSave = "success.settings_saved"

	// Status Messages.
	KeyStatusReady         = "status.ready"
	KeyStatusProcessing    = "status.processing"
	KeyStatusLoading       = "status.loading"
	KeyStatusSaving        = "status.saving"
	KeyStatusComplete      = "status.complete"
	KeyStatusLargeFileProc = "status.large_file_processing"

	// Dialog Titles.
	KeyDialogSelectFile   = "dialog.select_file"
	KeyDialogSelectFolder = "dialog.select_folder"
	KeyDialogError        = "dialog.error"
	KeyDialogSuccess      = "dialog.success"
	KeyDialogWarning      = "dialog.warning"
	KeyDialogInfo         = "dialog.info"

	// Help Text.
	KeyHelpWindowSize    = "help.window_size"
	KeyHelpTimeRange     = "help.time_range"
	KeyHelpScalingFactor = "help.scaling_factor"
	KeyHelpPhaseLabels   = "help.phase_labels"
)

// translationEntry holds translations for all locales for a single key.
type translationEntry struct {
	zhTW string
	zhCN string
	enUS string
	jaJP string
}

// translationData is the centralized translation data.
//
//nolint:gochecknoglobals // Global translation data is intentional for i18n
var translationData = map[string]translationEntry{
	// UI Interface
	KeyAppTitle:        {"EMG 數據分析工具", "EMG 数据分析工具", "EMG Data Analysis Tool", "EMGデータ解析ツール"},
	KeyMenuFile:        {"檔案", "文件", "File", "ファイル"},
	KeyMenuSettings:    {"設定", "设置", "Settings", "設定"},
	KeyMenuHelp:        {"幫助", "帮助", "Help", "ヘルプ"},
	KeyButtonBrowse:    {"瀏覽", "浏览", "Browse", "参照"},
	KeyButtonCalculate: {"計算", "计算", "Calculate", "計算"},
	KeyButtonCancel:    {"取消", "取消", "Cancel", "キャンセル"},
	KeyButtonSave:      {"保存", "保存", "Save", "保存"},
	KeyButtonReset:     {"重置", "重置", "Reset", "リセット"},
	KeyButtonOK:        {"確定", "确定", "OK", "OK"},

	// Function Tabs
	KeyTabMaxmean:   {"最大平均值計算", "最大平均值计算", "Max Mean Calculation", "最大平均値計算"},
	KeyTabNormalize: {"資料標準化", "数据标准化", "Data Normalization", "データ正規化"},
	KeyTabPhase:     {"階段分析", "阶段分析", "Phase Analysis", "段階解析"},
	KeyTabSettings:  {"配置設定", "配置设置", "Configuration", "設定"},

	// Form Labels
	KeyLabelProcessingMode: {"處理模式", "处理模式", "Processing Mode", "処理モード"},
	KeyLabelSingleFile:     {"處理單一檔案", "处理单一文件", "Process Single File", "単一ファイル処理"},
	KeyLabelBatchFolder:    {"批量處理資料夾", "批量处理文件夹", "Batch Process Folder", "フォルダ一括処理"},
	KeyLabelFilePath:       {"檔案路徑", "文件路径", "File Path", "ファイルパス"},
	KeyLabelFolderPath:     {"資料夾路徑", "文件夹路径", "Folder Path", "フォルダパス"},
	KeyLabelWindowSize:     {"窗口大小", "窗口大小", "Window Size", "ウィンドウサイズ"},
	KeyLabelStartRange:     {"開始範圍秒數", "开始范围秒数", "Start Range (seconds)", "開始範囲（秒）"},
	KeyLabelEndRange:       {"結束範圍秒數", "结束范围秒数", "End Range (seconds)", "終了範囲（秒）"},
	KeyLabelMainFile:       {"主要資料檔案", "主要数据文件", "Main Data File", "メインデータファイル"},
	KeyLabelReferenceFile:  {"參考資料檔案", "参考数据文件", "Reference Data File", "参照データファイル"},
	KeyLabelOutputName:     {"輸出檔名", "输出文件名", "Output Filename", "出力ファイル名"},
	KeyLabelPhaseLabels:    {"階段標籤", "阶段标签", "Phase Labels", "段階ラベル"},
	KeyLabelScalingFactor:  {"縮放因子", "缩放因子", "Scaling Factor", "スケーリング係数"},
	KeyLabelPrecision:      {"精度", "精度", "Precision", "精度"},
	KeyLabelOutputFormat:   {"輸出格式", "输出格式", "Output Format", "出力形式"},
	KeyLabelBOMEnabled:     {"啟用 BOM", "启用 BOM", "Enable BOM", "BOM有効"},
	KeyLabelInputDir:       {"輸入目錄", "输入目录", "Input Directory", "入力ディレクトリ"},
	KeyLabelOutputDir:      {"輸出目錄", "输出目录", "Output Directory", "出力ディレクトリ"},
	KeyLabelOperateDir:     {"操作目錄", "操作目录", "Operation Directory", "操作ディレクトリ"},
	KeyLabelLanguage:       {"語言", "语言", "Language", "言語"},

	// Error Messages
	KeyErrorFileNotFound: {"檔案未找到", "文件未找到", "File not found", "ファイルが見つかりません"},
	KeyErrorInvalidPath:  {"無效的檔案路徑", "无效的文件路径", "Invalid file path", "無効なファイルパス"},
	KeyErrorInvalidCSV:   {"無效的 CSV 檔案格式", "无效的 CSV 文件格式", "Invalid CSV file format", "無効なCSVファイル形式"},
	KeyErrorFileTooLarge: {
		"檔案過大，請使用大文件處理功能",
		"文件过大，请使用大文件处理功能",
		"File too large, please use large file processing",
		"ファイルが大きすぎます。大容量ファイル処理機能を使用してください",
	},
	KeyErrorInsufficientData: {"資料不足", "数据不足", "Insufficient data", "データが不足しています"},
	KeyErrorCalculationFail:  {"計算失敗", "计算失败", "Calculation failed", "計算が失敗しました"},
	KeyErrorMemoryLimit:      {"記憶體不足", "内存不足", "Memory limit exceeded", "メモリ不足"},
	KeyErrorValidationFail:   {"驗證失敗", "验证失败", "Validation failed", "検証に失敗しました"},

	// Success Messages
	KeySuccessCalcComplete: {"計算成功完成", "计算成功完成", "Calculation completed successfully", "計算が正常に完了しました"},
	KeySuccessFileSaved:    {"檔案已成功保存", "文件已成功保存", "File saved successfully", "ファイルが正常に保存されました"},
	KeySuccessSettingsSave: {"設定已成功保存", "设置已成功保存", "Settings saved successfully", "設定が正常に保存されました"},

	// Status Messages
	KeyStatusReady:      {"就緒", "就绪", "Ready", "準備完了"},
	KeyStatusProcessing: {"處理中...", "处理中...", "Processing...", "処理中..."},
	KeyStatusLoading:    {"載入中...", "加载中...", "Loading...", "読み込み中..."},
	KeyStatusSaving:     {"保存中...", "保存中...", "Saving...", "保存中..."},
	KeyStatusComplete:   {"完成", "完成", "Complete", "完了"},
	KeyStatusLargeFileProc: {
		"處理大文件中... %.1f%%",
		"处理大文件中... %.1f%%",
		"Processing large file... %.1f%%",
		"大容量ファイル処理中... %.1f%%",
	},

	// Dialog Titles
	KeyDialogSelectFile:   {"選擇檔案", "选择文件", "Select File", "ファイル選択"},
	KeyDialogSelectFolder: {"選擇資料夾", "选择文件夹", "Select Folder", "フォルダ選択"},
	KeyDialogError:        {"錯誤", "错误", "Error", "エラー"},
	KeyDialogSuccess:      {"成功", "成功", "Success", "成功"},
	KeyDialogWarning:      {"警告", "警告", "Warning", "警告"},
	KeyDialogInfo:         {"資訊", "信息", "Information", "情報"},

	// Help Text
	KeyHelpWindowSize: {
		"滑動窗口的大小（數據點數）",
		"滑动窗口的大小（数据点数）",
		"Size of the sliding window (number of data points)",
		"スライディングウィンドウのサイズ（データポイント数）",
	},
	KeyHelpTimeRange: {
		"指定分析的時間範圍（秒）",
		"指定分析的时间范围（秒）",
		"Specify the time range for analysis (seconds)",
		"解析の時間範囲を指定（秒）",
	},
	KeyHelpScalingFactor: {"數據縮放因子（10的冪次）", "数据缩放因子（10的幂次）", "Data scaling factor (power of 10)", "データスケーリング係数（10の累乗）"},
	KeyHelpPhaseLabels:   {"用換行分隔的階段標籤", "用换行分隔的阶段标签", "Phase labels separated by newlines", "改行で区切られた段階ラベル"},
}

// buildTranslationMap builds the translation map for a specific locale.
func buildTranslationMap(locale Locale) map[string]string {
	result := make(map[string]string, len(translationData))

	for key, entry := range translationData {
		switch locale {
		case LocaleZhTW:
			result[key] = entry.zhTW
		case LocaleZhCN:
			result[key] = entry.zhCN
		case LocaleEnUS:
			result[key] = entry.enUS
		case LocaleJaJP:
			result[key] = entry.jaJP
		}
	}

	return result
}

// I18n manages internationalization.
type I18n struct {
	currentLocale Locale
	messages      map[Locale]map[string]string
	mutex         sync.RWMutex
	fallback      Locale
}

// NewI18n creates a new internationalization manager.
func NewI18n() *I18n {
	return &I18n{
		currentLocale: LocaleZhTW, // 默認使用繁體中文
		messages:      make(map[Locale]map[string]string),
		fallback:      LocaleZhTW,
	}
}

// LoadTranslations loads translation files from a directory.
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
			i.messages[locale] = buildTranslationMap(locale)

			continue
		}

		// Load from file
		data, err := os.ReadFile(filename) //nolint:gosec // filename is constructed from known values
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

// SetLocale sets the current locale.
func (i *I18n) SetLocale(locale Locale) {
	i.mutex.Lock()
	defer i.mutex.Unlock()
	i.currentLocale = locale
}

// GetLocale returns the current locale.
func (i *I18n) GetLocale() Locale {
	i.mutex.RLock()
	defer i.mutex.RUnlock()

	return i.currentLocale
}

// T translates a message key.
func (i *I18n) T(key string, args ...interface{}) string {
	i.mutex.RLock()
	defer i.mutex.RUnlock()

	// Try current locale first
	if message := i.getMessage(i.currentLocale, key); message != "" {
		return i.formatMessage(message, args)
	}

	// Fallback to default locale
	if i.currentLocale != i.fallback {
		if message := i.getMessage(i.fallback, key); message != "" {
			return i.formatMessage(message, args)
		}
	}

	// Return key if no translation found
	return i.formatKeyWithArgs(key, args)
}

// getMessage retrieves a message for a locale.
func (i *I18n) getMessage(locale Locale, key string) string {
	if messages, exists := i.messages[locale]; exists {
		if message, found := messages[key]; found {
			return message
		}
	}

	return ""
}

// formatMessage formats a message with args if present.
func (*I18n) formatMessage(message string, args []interface{}) string {
	if len(args) > 0 {
		return fmt.Sprintf(message, args...)
	}

	return message
}

// formatKeyWithArgs formats a key with args as fallback.
func (*I18n) formatKeyWithArgs(key string, args []interface{}) string {
	if len(args) > 0 {
		return fmt.Sprintf("%s: %v", key, args)
	}

	return key
}

// GetSupportedLocales returns list of supported locales.
func (*I18n) GetSupportedLocales() []Locale {
	return []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}
}

// localeNames maps locales to their display names.
//
//nolint:gochecknoglobals // localeNames is a constant-like map for locale display names
var localeNames = map[Locale]string{
	LocaleZhTW: "繁體中文",
	LocaleZhCN: "简体中文",
	LocaleEnUS: "English",
	LocaleJaJP: "日本語",
}

// GetLocaleName returns the display name of a locale.
func (*I18n) GetLocaleName(locale Locale) string {
	if name, ok := localeNames[locale]; ok {
		return name
	}

	return string(locale)
}

// DetectSystemLocale attempts to detect the system locale.
func (*I18n) DetectSystemLocale() Locale {
	// Check environment variables
	envVars := []string{"LC_ALL", "LC_MESSAGES", "LANG"}

	for _, envVar := range envVars {
		if value := os.Getenv(envVar); value != "" {
			locale := parseLocale(value)
			if locale != "" {
				return locale
			}
		}
	}

	// Default fallback
	return LocaleZhTW
}

// parseLocale parses locale string and returns supported locale.
func parseLocale(localeStr string) Locale {
	localeStr = strings.ToLower(localeStr)

	// Handle common variations
	switch {
	case strings.HasPrefix(localeStr, "zh_tw"), strings.HasPrefix(localeStr, "zh-tw"):
		return LocaleZhTW
	case strings.HasPrefix(localeStr, "zh_cn"), strings.HasPrefix(localeStr, "zh-cn"):
		return LocaleZhCN
	case strings.HasPrefix(localeStr, "en"):
		return LocaleEnUS
	case strings.HasPrefix(localeStr, "ja"):
		return LocaleJaJP
	default:
		return ""
	}
}

// SaveTranslations saves current translations to files.
func (i *I18n) SaveTranslations(translationsDir string) error {
	i.mutex.RLock()
	defer i.mutex.RUnlock()

	// Ensure directory exists
	if err := os.MkdirAll(translationsDir, fsperm.DirPerm); err != nil {
		return fmt.Errorf("無法創建翻譯目錄: %w", err)
	}

	// Save each locale
	for locale, messages := range i.messages {
		filename := filepath.Join(translationsDir, fmt.Sprintf("%s.json", locale))

		data, err := json.MarshalIndent(messages, "", "  ")
		if err != nil {
			return fmt.Errorf("序列化翻譯失敗 %s: %w", locale, err)
		}

		if err := fsperm.WriteFileNoFollow(filename, data); err != nil {
			return fmt.Errorf("寫入翻譯文件失敗 %s: %w", filename, err)
		}
	}

	return nil
}

// Global instance.
//
//nolint:gochecknoglobals // Global i18n instance is intentional for convenience
var globalI18n *I18n

// InitI18n initializes the global i18n instance.
func InitI18n(translationsDir string) error {
	globalI18n = NewI18n()

	// Try to detect system locale
	systemLocale := globalI18n.DetectSystemLocale()
	globalI18n.SetLocale(systemLocale)

	// Load translations
	return globalI18n.LoadTranslations(translationsDir)
}

// T translates a message key using the global i18n instance.
func T(key string, args ...interface{}) string {
	if globalI18n == nil {
		return key
	}

	return globalI18n.T(key, args...)
}

// SetLocale sets the current locale using the global i18n instance.
func SetLocale(locale Locale) {
	if globalI18n != nil {
		globalI18n.SetLocale(locale)
	}
}

// GetLocale returns the current locale using the global i18n instance.
func GetLocale() Locale {
	if globalI18n == nil {
		return LocaleZhTW
	}

	return globalI18n.GetLocale()
}

// GetSupportedLocales returns the list of supported locales using the global i18n instance.
func GetSupportedLocales() []Locale {
	if globalI18n == nil {
		return []Locale{LocaleZhTW}
	}

	return globalI18n.GetSupportedLocales()
}

// GetLocaleName returns the display name of a locale using the global i18n instance.
func GetLocaleName(locale Locale) string {
	if globalI18n == nil {
		return string(locale)
	}

	return globalI18n.GetLocaleName(locale)
}
