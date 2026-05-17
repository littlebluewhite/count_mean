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

	// Muscle Ratio Analysis Messages (P3-E migration — backend i18n).
	// User-facing strings surfaced through SubjectResult.Error or MuscleRatioResult.Message.
	// %w verb 禁止：catalog 字串只能含 fmt.Sprintf-compatible verbs (%s %v %d %q)；
	// error wrap 由 caller 用 `fmt.Errorf("%s: %w", i18n.T(key), innerErr)` pattern 處理。
	KeyErrorMuscleRatioOutputDirInvalid          = "error.muscle_ratio.output_dir_invalid"
	KeyErrorMuscleRatioParseManifestFailed       = "error.muscle_ratio.parse_manifest_failed"
	KeyErrorMuscleRatioEmptyManifest             = "error.muscle_ratio.empty_manifest"
	KeyErrorMuscleRatioMkdirFailed               = "error.muscle_ratio.mkdir_failed"
	KeyErrorMuscleRatioSubjectEmptyName          = "error.muscle_ratio.subject.empty_name"
	KeyErrorMuscleRatioSubjectParseEMGFailed     = "error.muscle_ratio.subject.parse_emg_failed"
	KeyErrorMuscleRatioSubjectWriteOutput1Failed = "error.muscle_ratio.subject.write_output1_failed"
	KeyErrorMuscleRatioSubjectWriteOutput2Failed = "error.muscle_ratio.subject.write_output2_failed"
	KeyErrorMuscleRatioHandlerAnalysisFailed     = "error.muscle_ratio.handler.analysis_failed"
	KeyStatusMuscleRatioProcessedCount           = "status.muscle_ratio.processed_count"
	KeyStatusMuscleRatioPartialWarning           = "status.muscle_ratio.partial_warning"

	// P3-E (3c) follow-up keys — Phase 3 grep pattern 漏命中、Phase 3 收尾補上。
	KeyErrorMuscleRatioSubjectEmptyEMG           = "error.muscle_ratio.subject.empty_emg"
	KeyErrorMuscleRatioSubjectInsufficientPhases = "error.muscle_ratio.subject.insufficient_phases"
	KeyErrorMuscleRatioSubjectPhaseOutOfEMGRange = "error.muscle_ratio.subject.phase_out_of_emg_range"
	KeyErrorMuscleRatioSubjectCollision          = "error.muscle_ratio.subject_collision"

	// ConfigPanel — Phase 1 frontend i18n MVP keys (2026-05-14).
	// 用於前端 frontend/src/main.js showConfigPanel() 字串 catalog 化。
	// 命名前綴 "config." 避免與既有泛用 Key 衝突(KeyLabelLanguage 等仍保留供他處 reuse)。
	KeyConfigPanelTitle            = "config.panel.title"
	KeyConfigButtonBack            = "config.button.back"
	KeyConfigButtonSave            = "config.button.save"
	KeyConfigButtonReset           = "config.button.reset"
	KeyConfigButtonImport          = "config.button.import"
	KeyConfigButtonBrowse          = "config.button.browse"
	KeyConfigSectionDataProcessing = "config.section.data_processing"
	KeyConfigSectionDirectories    = "config.section.directories"
	KeyConfigSectionPhaseLabels    = "config.section.phase_labels"
	KeyConfigSectionAdvanced       = "config.section.advanced"
	KeyConfigLabelScalingFactor    = "config.label.scaling_factor"
	KeyConfigHelpScalingFactor     = "config.help.scaling_factor"
	KeyConfigLabelPrecision        = "config.label.precision"
	KeyConfigHelpPrecision         = "config.help.precision"
	KeyConfigLabelOutputFormat     = "config.label.output_format"
	KeyConfigHelpOutputFormat      = "config.help.output_format"
	KeyConfigOptionOutputCSV       = "config.option.output_csv"
	KeyConfigOptionOutputJSON      = "config.option.output_json"
	KeyConfigOptionOutputXLSX      = "config.option.output_xlsx"
	KeyConfigLabelBOMEnabled       = "config.label.bom_enabled"
	KeyConfigHelpBOMEnabled        = "config.help.bom_enabled"
	KeyConfigLabelInputDir         = "config.label.input_dir"
	KeyConfigHelpInputDir          = "config.help.input_dir"
	KeyConfigLabelOutputDir        = "config.label.output_dir"
	KeyConfigHelpOutputDir         = "config.help.output_dir"
	KeyConfigLabelOperateDir       = "config.label.operate_dir"
	KeyConfigHelpOperateDir        = "config.help.operate_dir"
	KeyConfigLabelPhaseLabels      = "config.label.phase_labels"
	KeyConfigHelpPhaseLabels       = "config.help.phase_labels"
	KeyConfigLabelLogLevel         = "config.label.log_level"
	KeyConfigHelpLogLevel          = "config.help.log_level"
	KeyConfigOptionLogLevelDebug   = "config.option.log_level_debug"
	KeyConfigOptionLogLevelInfo    = "config.option.log_level_info"
	KeyConfigOptionLogLevelWarn    = "config.option.log_level_warn"
	KeyConfigOptionLogLevelError   = "config.option.log_level_error"
	KeyConfigLabelUILanguage       = "config.label.ui_language"
	KeyConfigHelpUILanguage        = "config.help.ui_language"

	// Phase 2 main UI — header + main menu (2026-05-14).
	KeyHeaderAppTitle = "header.app_title"
	KeyHeaderSubtitle = "header.subtitle"

	KeyMenuButtonMaxmeanTitle   = "menu.button.maxmean.title"
	KeyMenuButtonMaxmeanDesc    = "menu.button.maxmean.description"
	KeyMenuButtonNormalizeTitle = "menu.button.normalize.title"
	KeyMenuButtonNormalizeDesc  = "menu.button.normalize.description"
	KeyMenuButtonChartTitle     = "menu.button.chart.title"
	KeyMenuButtonChartDesc      = "menu.button.chart.description"
	KeyMenuButtonPhaseTitle     = "menu.button.phase.title"
	KeyMenuButtonPhaseDesc      = "menu.button.phase.description"
	KeyMenuButtonPhaseSyncTitle = "menu.button.phasesync.title"
	KeyMenuButtonPhaseSyncDesc  = "menu.button.phasesync.description"
	KeyMenuButtonCCITitle       = "menu.button.cci.title"
	KeyMenuButtonCCIDesc        = "menu.button.cci.description"
	KeyMenuButtonNormPhaseTitle = "menu.button.normalizedphasesync.title"
	KeyMenuButtonNormPhaseDesc  = "menu.button.normalizedphasesync.description"
	KeyMenuButtonMuscleRatTitle = "menu.button.muscleratio.title"
	KeyMenuButtonMuscleRatDesc  = "menu.button.muscleratio.description"
	KeyMenuButtonConfigTitle    = "menu.button.config.title"
	KeyMenuButtonConfigDesc     = "menu.button.config.description"

	// Phase 2 panel titles & descriptions (2026-05-14).
	KeyPanelMaxmeanTitle     = "panel.maxmean.title"
	KeyPanelNormalizeTitle   = "panel.normalize.title"
	KeyPanelChartTitle       = "panel.chart.title"
	KeyPanelPhaseTitle       = "panel.phase.title"
	KeyPanelPhaseSyncTitle   = "panel.phasesync.title"
	KeyPanelCCITitle         = "panel.cci.title"
	KeyPanelNormPhaseTitle   = "panel.normalizedphasesync.title"
	KeyPanelNormPhaseDesc    = "panel.normalizedphasesync.description"
	KeyPanelMuscleRatioTitle = "panel.muscleratio.title"
	KeyPanelMuscleRatioDesc  = "panel.muscleratio.description"

	// Phase 2 common buttons. (KeyButtonBrowse 已有 legacy 定義 in line 35,reuse 之)
	KeyButtonBack              = "button.back"
	KeyButtonStartCalculate    = "button.start_calculate"
	KeyButtonStartNormalize    = "button.start_normalize"
	KeyButtonStartAnalyze      = "button.start_analyze"
	KeyButtonStartBatchAnalyze = "button.start_batch_analyze"
	KeyButtonDownloadChart     = "button.download_chart"
	KeyButtonOpenOutputFolder  = "button.open_output_folder"

	// Phase 2 form labels — process mode (MaxMean panel).
	KeyFormLabelProcessMode     = "form.label.process_mode"
	KeyFormOptionSingleFile     = "form.option.single_file"
	KeyFormOptionBatchFolder    = "form.option.batch_folder"
	KeyFormLabelInputFile       = "form.label.input_file"
	KeyFormLabelInputFolder     = "form.label.input_folder"
	KeyFormLabelWindowSize      = "form.label.window_size"
	KeyFormHelpWindowSize       = "form.help.window_size"
	KeyFormLabelTimeRange       = "form.label.time_range"
	KeyFormPlaceholderStartTime = "form.placeholder.start_time"
	KeyFormPlaceholderEndTime   = "form.placeholder.end_time"
	KeyFormHelpTimeRange        = "form.help.time_range"

	// Phase 2 form labels — Normalize panel.
	KeyFormLabelMainFile         = "form.label.main_file"
	KeyFormLabelReferenceFile    = "form.label.reference_file"
	KeyFormLabelOutputName       = "form.label.output_name"
	KeyFormPlaceholderOutputName = "form.placeholder.output_name"

	// Phase 2 form labels — Chart panel.
	KeyFormLabelChartFile      = "form.label.chart_file"
	KeyFormLabelChartTitle     = "form.label.chart_title"
	KeyFormDefaultChartTitle   = "form.default.chart_title"
	KeyFormLabelSelectColumns  = "form.label.select_columns"
	KeyFormHelpSelectFileFirst = "form.help.select_file_first"
	KeyChartPreviewTitle       = "chart.preview.title"

	// Phase 2 form labels — Phase panel.
	KeyFormLabelPhaseFile         = "form.label.phase_file"
	KeyFormLabelPhasePoints       = "form.label.phase_points"
	KeyFormPlaceholderPhasePoints = "form.placeholder.phase_points"
	KeyFormHelpPhasePoints        = "form.help.phase_points"
	KeyFormLabelPhaseLabels       = "form.label.phase_labels"
	KeyFormPlaceholderPhaseLabels = "form.placeholder.phase_labels"

	// Phase 2 shared labels — phasesync / cci / normalized / muscle panels.
	// 編號 ("1.", "2." ...) 留在前端 template literal 不入 catalog (跨語言一致)。
	KeyFormLabelManifest                = "form.label.manifest"
	KeyFormPlaceholderManifest          = "form.placeholder.manifest"
	KeyFormLabelDataFolder              = "form.label.data_folder"
	KeyFormPlaceholderDataFolder        = "form.placeholder.data_folder"
	KeyFormPlaceholderDataFolderEmg     = "form.placeholder.data_folder_emg"
	KeyFormLabelSubject                 = "form.label.subject"
	KeyFormPlaceholderLoadManifestFirst = "form.placeholder.load_manifest_first"
	KeyFormOptionSelectSubject          = "form.option.select_subject"
	KeyFormLabelStartPhase              = "form.label.start_phase"
	KeyFormLabelEndPhase                = "form.label.end_phase"
	KeyFormOptionSelect                 = "form.option.select"

	// Phase 2 form labels — normalized phase sync extras.
	KeyFormLabelNormStartPhase  = "form.label.norm_start_phase"
	KeyFormLabelNormEndPhase    = "form.label.norm_end_phase"
	KeyFormLabelStatsStartPhase = "form.label.stats_start_phase"
	KeyFormLabelStatsEndPhase   = "form.label.stats_end_phase"

	// Phase 2 result section labels.
	KeyResultSectionTitle        = "result.section.title"
	KeyResultLabelSubject        = "result.label.subject"
	KeyResultLabelAnalysisRange  = "result.label.analysis_range"
	KeyResultLabelOutputFile     = "result.label.output_file"
	KeyResultLabelAnalysisReport = "result.label.analysis_report"
	KeyResultLabelMuscles        = "result.label.muscles"
	KeyResultLabelPhasePositions = "result.label.phase_positions"
	KeyResultLabelMusclePairs    = "result.label.muscle_pairs"
	KeyResultLabelCSVOutput      = "result.label.csv_output"
	KeyResultLabelNormRange      = "result.label.norm_range"
	KeyResultLabelStatsRange     = "result.label.stats_range"
	KeyResultLabelOutputNorm     = "result.label.output_normalized"
	KeyResultLabelOutputStats    = "result.label.output_stats"
	KeyResultLabelPhaseDisplay   = "result.label.phase_display"
	KeyResultMuscleSubjectCount  = "result.muscle.subject_count"
	KeyResultNormalizedHelpText  = "result.normalized.help_text"

	// Phase 2 table headers (Normalized & MuscleRatio result tables).
	KeyTableHeaderMuscle      = "table.header.muscle"
	KeyTableHeaderNormMax     = "table.header.norm_max"
	KeyTableHeaderNormMean    = "table.header.norm_mean"
	KeyTableHeaderSubject     = "table.header.subject"
	KeyTableHeaderStatus      = "table.header.status"
	KeyTableHeaderOutputAll   = "table.header.output_all"
	KeyTableHeaderOutputPhase = "table.header.output_phase"
	KeyTableHeaderDuration    = "table.header.duration"
	KeyTableHeaderMessage     = "table.header.message"

	// Phase 2 dialog title supplements (KeyDialogError/Success/Warning/Info already exist).
	KeyDialogTitleComplete      = "dialog.title.complete"
	KeyDialogTitlePartialFailed = "dialog.title.partial_failed"
	KeyDialogTitleHint          = "dialog.title.hint"

	// Phase 2 static error messages.
	KeyErrorMsgOnlyCSV               = "error.msg.only_csv"
	KeyErrorMsgSelectInputFile       = "error.msg.select_input_file"
	KeyErrorMsgSelectInputFolder     = "error.msg.select_input_folder"
	KeyErrorMsgSelectBothFiles       = "error.msg.select_both_files"
	KeyErrorMsgChartSelectColumns    = "error.msg.chart_select_columns"
	KeyErrorMsgChartNotFound         = "error.msg.chart_not_found"
	KeyErrorMsgCCIChartNotFound      = "error.msg.cci_chart_not_found"
	KeyErrorMsgPhaseInputs           = "error.msg.phase_inputs"
	KeyErrorMsgPhasePointsCount      = "error.msg.phase_points_count"
	KeyErrorMsgFillRequiredFields    = "error.msg.fill_required_fields"
	KeyErrorMsgMuscleFillFields      = "error.msg.muscle_fill_fields"
	KeyErrorMsgOpenOutputFolderFail  = "error.msg.open_output_folder"
	KeyErrorMsgEChartsNotFound       = "error.msg.echarts_not_found"
	KeyErrorMsgChartElementNotFound  = "error.msg.chart_element_not_found"
	KeyErrorMsgChartInstanceNotFound = "error.msg.chart_instance_not_found"

	// Phase 2 dynamic error messages (with %v for error / path).
	KeyErrorMsgCalculationFailedDyn   = "error.msg.calculation_failed"
	KeyErrorMsgNormalizationFailedDyn = "error.msg.normalization_failed"
	KeyErrorMsgChartGenFailedDyn      = "error.msg.chart_generation_failed"
	KeyErrorMsgChartDownloadFailedDyn = "error.msg.chart_download_failed"
	KeyErrorMsgPhaseAnalysisFailedDyn = "error.msg.phase_analysis_failed"
	KeyErrorMsgConfigSaveFailedDyn    = "error.msg.config_save_failed"
	KeyErrorMsgConfigResetFailedDyn   = "error.msg.config_reset_failed"
	KeyErrorMsgConfigImportFailedDyn  = "error.msg.config_import_failed"
	KeyErrorMsgFilePickerFailedDyn    = "error.msg.file_picker_failed"
	KeyErrorMsgLoadColumnsFailedDyn   = "error.msg.load_columns_failed"
	KeyErrorMsgChartPreviewFailedDyn  = "error.msg.chart_preview_failed"
	KeyErrorMsgLoadSubjectsFailedDyn  = "error.msg.load_subjects_failed"
	KeyErrorMsgAnalysisFailedDyn      = "error.msg.analysis_failed_dynamic"
	KeyErrorMsgLanguageSwitchFailed   = "error.msg.language_switch_failed"

	// Phase 2 success messages (most contain %s for output path).
	KeyOkMsgCalculationDone        = "success.msg.calculation_done"
	KeyOkMsgNormalizationDone      = "success.msg.normalization_done"
	KeyOkMsgChartGenerated         = "success.msg.chart_generated"
	KeyOkMsgChartDownloaded        = "success.msg.chart_downloaded"
	KeyOkMsgPhaseAnalysisDone      = "success.msg.phase_analysis_done"
	KeyOkMsgConfigSaved            = "success.msg.config_saved"
	KeyOkMsgConfigReset            = "success.msg.config_reset"
	KeyOkMsgConfigImported         = "success.msg.config_imported"
	KeyOkMsgAnalysisDone           = "success.msg.analysis_done"
	KeyOkMsgCCIAnalysisDone        = "success.msg.cci_analysis_done"
	KeyOkMsgNormalizedAnalysisDone = "success.msg.normalized_analysis_done"

	// Phase 2 info messages.
	KeyInfoMsgOutputFolder = "info.msg.output_folder"

	// Phase 2 status bar messages (status bar bottom of window).
	KeyStatusAppReady              = "status.app_ready"
	KeyStatusCalculationRunning    = "status.calculation_running"
	KeyStatusCalculationDone       = "status.calculation_done"
	KeyStatusCalculationFailed     = "status.calculation_failed"
	KeyStatusNormalizationRunning  = "status.normalization_running"
	KeyStatusNormalizationDone     = "status.normalization_done"
	KeyStatusNormalizationFailed   = "status.normalization_failed"
	KeyStatusChartGenerating       = "status.chart_generating"
	KeyStatusChartGenerated        = "status.chart_generated"
	KeyStatusChartGenerationFailed = "status.chart_generation_failed"
	KeyStatusChartDownloading      = "status.chart_downloading"
	KeyStatusChartDownloadDone     = "status.chart_download_done"
	KeyStatusChartDownloadFailed   = "status.chart_download_failed"
	KeyStatusPhaseAnalysisRunning  = "status.phase_analysis_running"
	KeyStatusPhaseAnalysisDone     = "status.phase_analysis_done"
	KeyStatusPhaseAnalysisFailed   = "status.phase_analysis_failed"
	KeyStatusPhasesyncRunning      = "status.phasesync_running"
	KeyStatusCCIRunning            = "status.cci_running"
	KeyStatusCCIChartDownloading   = "status.cci_chart_downloading"
	KeyStatusNormalizedRunning     = "status.normalized_running"
	KeyStatusMuscleRunning         = "status.muscle_running"
	KeyStatusMusclePartialFailed   = "status.muscle_partial_failed"
	KeyStatusAnalysisDone          = "status.analysis_done"
	KeyStatusAnalysisFailed        = "status.analysis_failed"
	KeyStatusSubjectsLoaded        = "status.subjects_loaded"

	// Phase 2 dynamic column-loading status.
	KeyStatusLoadingColumns    = "status.loading_columns"
	KeyStatusLoadColumnsFailed = "status.load_columns_failed"

	// Phase 2 console warnings.
	KeyWarnGetVersionFailed = "warning.get_version_failed"

	// Phase 2 chart placeholder (used by index.html before user opens chart panel).
	KeyChartTitlePlaceholder = "chart.title.placeholder"

	// Phase 2 import config: invalid format error (thrown by main.js, surfaced via ShowError).
	KeyErrorMsgInvalidConfigFormat = "error.msg.invalid_config_format"
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

	// Muscle Ratio Analysis Messages (P3-E migration — backend i18n).
	// Phase 5 翻譯落地（2026-05-14）：zh-CN / en-US / ja-JP 為 best-effort 翻譯，建議由 native speaker review。
	// Verb sequence（%v %d %s %.4f %q）4 locale 必須一致，由 TestI18n_VerbConsistencyAcrossLocales 守門。
	KeyErrorMuscleRatioOutputDirInvalid: {
		"OutputDir 驗證失敗",
		"OutputDir 验证失败",
		"OutputDir validation failed",
		"OutputDir の検証に失敗しました",
	},
	KeyErrorMuscleRatioParseManifestFailed: {
		"解析分期總檔案失敗",
		"解析分期总文件失败",
		"Failed to parse manifest file",
		"マニフェストファイルの解析に失敗しました",
	},
	KeyErrorMuscleRatioEmptyManifest: {
		"分期總檔案沒有任何主題記錄",
		"分期总文件没有任何主题记录",
		"Manifest file has no subject records",
		"マニフェストファイルにサブジェクトレコードがありません",
	},
	KeyErrorMuscleRatioMkdirFailed: {
		"建立輸出目錄失敗",
		"创建输出目录失败",
		"Failed to create output directory",
		"出力ディレクトリの作成に失敗しました",
	},
	KeyErrorMuscleRatioSubjectEmptyName: {
		"Subject 名稱為空",
		"Subject 名称为空",
		"Subject name is empty",
		"サブジェクト名が空です",
	},
	KeyErrorMuscleRatioSubjectParseEMGFailed: {
		"解析 EMG 檔案失敗: %v",
		"解析 EMG 文件失败: %v",
		"Failed to parse EMG file: %v",
		"EMGファイルの解析に失敗しました: %v",
	},
	KeyErrorMuscleRatioSubjectWriteOutput1Failed: {
		"寫入 Output 1 失敗: %v",
		"写入 Output 1 失败: %v",
		"Failed to write Output 1: %v",
		"Output 1 の書き込みに失敗しました: %v",
	},
	KeyErrorMuscleRatioSubjectWriteOutput2Failed: {
		"寫入 Output 2 失敗（Output 1 已產出）: %v",
		"写入 Output 2 失败（Output 1 已生成）: %v",
		"Failed to write Output 2 (Output 1 was produced): %v",
		"Output 2 の書き込みに失敗しました (Output 1 は出力済み): %v",
	},
	KeyErrorMuscleRatioHandlerAnalysisFailed: {
		"分析失敗: %v",
		"分析失败: %v",
		"Analysis failed: %v",
		"解析に失敗しました: %v",
	},
	KeyStatusMuscleRatioProcessedCount: {
		"已處理 %d 個主題",
		"已处理 %d 个主题",
		"Processed %d subjects",
		"%d 件のサブジェクトを処理しました",
	},
	KeyStatusMuscleRatioPartialWarning: {
		"（部分主題未完成，請查看各 row 的錯誤訊息）",
		"（部分主题未完成，请查看各 row 的错误消息）",
		"(Some subjects incomplete; see error messages in each row)",
		"(一部のサブジェクトが未完了です。各行のエラーメッセージを確認してください)",
	},

	// P3-E (3c) follow-up entries — Phase 3 grep pattern 漏命中、Phase 3 收尾補上。
	KeyErrorMuscleRatioSubjectEmptyEMG: {
		"EMG 資料為空",
		"EMG 数据为空",
		"EMG data is empty",
		"EMGデータが空です",
	},
	KeyErrorMuscleRatioSubjectInsufficientPhases: {
		"有效分期點不足 2 個，跳過 Output 2",
		"有效分期点不足 2 个，跳过 Output 2",
		"Fewer than 2 valid phase points; skipping Output 2",
		"有効なフェーズポイントが 2 つ未満のため、Output 2 をスキップします",
	},
	KeyErrorMuscleRatioSubjectPhaseOutOfEMGRange: {
		"phase %s 時間 %.4f 落在 EMG 範圍 [%.4f, %.4f] 外，跳過 Output 2",
		"phase %s 时间 %.4f 落在 EMG 范围 [%.4f, %.4f] 外，跳过 Output 2",
		"Phase %s at time %.4f is outside EMG range [%.4f, %.4f]; skipping Output 2",
		"フェーズ %s の時刻 %.4f は EMG 範囲 [%.4f, %.4f] 外です。Output 2 をスキップします",
	},
	KeyErrorMuscleRatioSubjectCollision: {
		"輸出檔名衝突: subject %q 與 %q 經 SanitizeFileName 後同為 %q (case-insensitive)",
		"输出文件名冲突: subject %q 与 %q 经 SanitizeFileName 后同为 %q (case-insensitive)",
		"Output filename collision: subjects %q and %q both sanitize to %q (case-insensitive)",
		"出力ファイル名の競合: サブジェクト %q と %q は SanitizeFileName 後に同じ %q になります (case-insensitive)",
	},

	// ConfigPanel — Phase 1 frontend i18n MVP entries (2026-05-14).
	// zh-CN / en-US / ja-JP 為 best-effort 翻譯;native speaker review pending。
	// Emoji 在 4 locale 一致(視覺符號跨語言通用)。
	KeyConfigPanelTitle:   {"系統配置", "系统配置", "System Configuration", "システム設定"},
	KeyConfigButtonBack:   {"返回", "返回", "Back", "戻る"},
	KeyConfigButtonSave:   {"儲存設定", "保存设置", "Save Settings", "設定を保存"},
	KeyConfigButtonReset:  {"重設為預設值", "重置为默认值", "Reset to Defaults", "デフォルトに戻す"},
	KeyConfigButtonImport: {"匯入設定", "导入设置", "Import Settings", "設定をインポート"},
	KeyConfigButtonBrowse: {"瀏覽", "浏览", "Browse", "参照"},

	KeyConfigSectionDataProcessing: {"📊 數據處理設定", "📊 数据处理设置", "📊 Data Processing", "📊 データ処理"},
	KeyConfigSectionDirectories:    {"📁 目錄設定", "📁 目录设置", "📁 Directories", "📁 ディレクトリ"},
	KeyConfigSectionPhaseLabels:    {"🏷️ 階段標籤設定", "🏷️ 阶段标签设置", "🏷️ Phase Labels", "🏷️ フェーズラベル"},
	KeyConfigSectionAdvanced:       {"🔧 進階設定", "🔧 高级设置", "🔧 Advanced", "🔧 詳細設定"},

	KeyConfigLabelScalingFactor: {"縮放因子", "缩放因子", "Scaling Factor", "スケーリング係数"},
	KeyConfigHelpScalingFactor: {
		"數據縮放倍數,用於放大微小信號",
		"数据缩放倍数,用于放大微小信号",
		"Data scaling factor used to amplify small signals",
		"データのスケーリング倍率。微小な信号を増幅します",
	},
	KeyConfigLabelPrecision:    {"精度(小數位數)", "精度(小数位数)", "Precision (decimal places)", "精度(小数桁数)"},
	KeyConfigHelpPrecision:     {"輸出數據的小數位數", "输出数据的小数位数", "Number of decimal places in output", "出力データの小数桁数"},
	KeyConfigLabelOutputFormat: {"輸出格式", "输出格式", "Output Format", "出力形式"},
	KeyConfigHelpOutputFormat:  {"輸出檔案的格式", "输出文件的格式", "Format of the output file", "出力ファイルの形式"},
	KeyConfigOptionOutputCSV: {
		"CSV(逗號分隔值)",
		"CSV(逗号分隔值)",
		"CSV (Comma-Separated Values)",
		"CSV (カンマ区切り)",
	},
	KeyConfigOptionOutputJSON: {
		"JSON(JavaScript 對象表示法)",
		"JSON(JavaScript 对象表示法)",
		"JSON (JavaScript Object Notation)",
		"JSON (JavaScript オブジェクト記法)",
	},
	KeyConfigOptionOutputXLSX: {
		"XLSX(Excel 檔案)",
		"XLSX(Excel 文件)",
		"XLSX (Excel File)",
		"XLSX (Excel ファイル)",
	},
	KeyConfigLabelBOMEnabled: {
		"啟用 BOM(字節順序標記)",
		"启用 BOM(字节顺序标记)",
		"Enable BOM (Byte Order Mark)",
		"BOM (バイト順マーク) を有効化",
	},
	KeyConfigHelpBOMEnabled: {
		"在 CSV 檔案開頭添加 BOM,改善 Excel 相容性",
		"在 CSV 文件开头添加 BOM,改善 Excel 兼容性",
		"Adds BOM at the start of CSV files for better Excel compatibility",
		"CSV ファイルの先頭に BOM を追加し、Excel との互換性を向上させます",
	},

	KeyConfigLabelInputDir:   {"預設輸入目錄", "默认输入目录", "Default Input Directory", "デフォルト入力ディレクトリ"},
	KeyConfigHelpInputDir:    {"預設的資料檔案來源目錄", "默认的数据文件来源目录", "Default source directory for data files", "データファイルのデフォルトソースディレクトリ"},
	KeyConfigLabelOutputDir:  {"預設輸出目錄", "默认输出目录", "Default Output Directory", "デフォルト出力ディレクトリ"},
	KeyConfigHelpOutputDir:   {"處理結果的儲存目錄", "处理结果的存储目录", "Directory where results are saved", "処理結果を保存するディレクトリ"},
	KeyConfigLabelOperateDir: {"參考資料目錄", "参考资料目录", "Reference Data Directory", "参照データディレクトリ"},
	KeyConfigHelpOperateDir:  {"存放參考檔案的目錄", "存放参考文件的目录", "Directory holding reference files", "参照ファイルを保持するディレクトリ"},

	KeyConfigLabelPhaseLabels: {"階段標籤(每行一個)", "阶段标签(每行一个)", "Phase Labels (one per line)", "フェーズラベル (1行に1つ)"},
	KeyConfigHelpPhaseLabels:  {"定義階段分析時使用的標籤名稱", "定义阶段分析时使用的标签名称", "Label names used during phase analysis", "フェーズ解析時に使用するラベル名"},

	KeyConfigLabelLogLevel:       {"日誌級別", "日志级别", "Log Level", "ログレベル"},
	KeyConfigHelpLogLevel:        {"控制日誌輸出的詳細程度", "控制日志输出的详细程度", "Controls verbosity of log output", "ログ出力の詳細度を制御"},
	KeyConfigOptionLogLevelDebug: {"Debug(除錯)", "Debug(调试)", "Debug", "Debug (デバッグ)"},
	KeyConfigOptionLogLevelInfo:  {"Info(資訊)", "Info(信息)", "Info", "Info (情報)"},
	KeyConfigOptionLogLevelWarn:  {"Warn(警告)", "Warn(警告)", "Warn", "Warn (警告)"},
	KeyConfigOptionLogLevelError: {"Error(錯誤)", "Error(错误)", "Error", "Error (エラー)"},
	KeyConfigLabelUILanguage:     {"介面語言", "界面语言", "Interface Language", "インターフェース言語"},
	KeyConfigHelpUILanguage:      {"應用程序的顯示語言", "应用程序的显示语言", "Application display language", "アプリケーションの表示言語"},

	// Phase 2 main UI entries (2026-05-14).
	KeyHeaderAppTitle: {"EMG 資料分析工具", "EMG 数据分析工具", "EMG Data Analysis Tool", "EMG データ解析ツール"},
	KeyHeaderSubtitle: {"選擇要執行的分析功能", "选择要执行的分析功能", "Select an analysis function to run", "実行する解析機能を選択"},

	KeyMenuButtonMaxmeanTitle:   {"最大平均值計算", "最大平均值计算", "Max Mean Calculation", "最大平均値計算"},
	KeyMenuButtonMaxmeanDesc:    {"計算 EMG 資料的最大平均值", "计算 EMG 数据的最大平均值", "Calculate the maximum mean of EMG data", "EMG データの最大平均値を計算"},
	KeyMenuButtonNormalizeTitle: {"資料標準化", "数据标准化", "Data Normalisation", "データ正規化"},
	KeyMenuButtonNormalizeDesc:  {"使用參考值標準化資料", "使用参考值标准化数据", "Normalise data using reference values", "参照値でデータを正規化"},
	KeyMenuButtonChartTitle:     {"資料做圖", "数据做图", "Data Visualisation", "データ可視化"},
	KeyMenuButtonChartDesc:      {"生成資料視覺化圖表", "生成数据可视化图表", "Generate data visualisation charts", "データ可視化チャートを生成"},
	KeyMenuButtonPhaseTitle:     {"階段分析", "阶段分析", "Phase Analysis", "段階解析"},
	KeyMenuButtonPhaseDesc:      {"分析不同運動階段的資料", "分析不同运动阶段的数据", "Analyse data across motion phases", "異なる運動段階のデータを解析"},
	KeyMenuButtonPhaseSyncTitle: {"分期同步分析", "分期同步分析", "Phase Sync Analysis", "フェーズ同期解析"},
	KeyMenuButtonPhaseSyncDesc: {
		"同步分析多種資料的分期訊號",
		"同步分析多种数据的分期信号",
		"Synchronised analysis of phase signals across data sources",
		"複数データのフェーズ信号を同期解析",
	},
	KeyMenuButtonCCITitle: {"共同收縮分析", "共同收缩分析", "Co-Contraction Analysis", "共収縮解析"},
	KeyMenuButtonCCIDesc: {
		"CCI Rudolph 共同收縮指標計算與作圖",
		"CCI Rudolph 共同收缩指标计算与作图",
		"CCI Rudolph co-contraction index computation and plotting",
		"CCI Rudolph 共収縮指数の計算と描画",
	},
	KeyMenuButtonNormPhaseTitle: {"標準化分期同步分析", "标准化分期同步分析", "Normalised Phase Sync Analysis", "正規化フェーズ同期解析"},
	KeyMenuButtonNormPhaseDesc: {
		"於分期區間內以肌肉最大值標準化,並輸出分期統計",
		"于分期区间内以肌肉最大值标准化,并输出分期统计",
		"Normalise by each muscle's max within the phase range, then output phase statistics",
		"フェーズ範囲内で各筋肉の最大値で正規化し、フェーズ統計を出力",
	},
	KeyMenuButtonMuscleRatTitle: {"肌肉比值分析", "肌肉比值分析", "Muscle Ratio Analysis", "筋肉比解析"},
	KeyMenuButtonMuscleRatDesc: {
		"批次計算 4 對右側肌肉的比值時間序列與分期點切片",
		"批次计算 4 对右侧肌肉的比值时间序列与分期点切片",
		"Batch-compute time series and phase-point slices for 4 right-side muscle pairs",
		"右側4対の筋肉について比率時系列とフェーズ点スライスをバッチ計算",
	},
	KeyMenuButtonConfigTitle: {"系統配置", "系统配置", "System Configuration", "システム設定"},
	KeyMenuButtonConfigDesc:  {"設定預設路徑和參數", "设置默认路径和参数", "Configure default paths and parameters", "デフォルトパスとパラメータを設定"},

	// Phase 2 panel titles & descriptions.
	KeyPanelMaxmeanTitle:   {"最大平均值計算", "最大平均值计算", "Max Mean Calculation", "最大平均値計算"},
	KeyPanelNormalizeTitle: {"資料標準化", "数据标准化", "Data Normalisation", "データ正規化"},
	KeyPanelChartTitle:     {"資料做圖", "数据做图", "Data Visualisation", "データ可視化"},
	KeyPanelPhaseTitle:     {"階段分析", "阶段分析", "Phase Analysis", "段階解析"},
	KeyPanelPhaseSyncTitle: {"分期同步分析", "分期同步分析", "Phase Sync Analysis", "フェーズ同期解析"},
	KeyPanelCCITitle:       {"共同收縮分析 (CCI Rudolph)", "共同收缩分析 (CCI Rudolph)", "Co-Contraction Analysis (CCI Rudolph)", "共収縮解析 (CCI Rudolph)"},
	KeyPanelNormPhaseTitle: {"標準化分期同步分析", "标准化分期同步分析", "Normalised Phase Sync Analysis", "正規化フェーズ同期解析"},
	KeyPanelNormPhaseDesc: {
		"先以每條肌肉在「標準化區間」內的最大值做標準化(除數),再對標準化後的資料於「統計區間」內計算分期同步統計。一次產生兩個檔案。標準化區間與統計區間可獨立選擇(預設兩者相同,可分別調整)。",
		"先以每条肌肉在「标准化区间」内的最大值做标准化(除数),再对标准化后的数据于「统计区间」内计算分期同步统计。一次产生两个文件。标准化区间与统计区间可独立选择(默认两者相同,可分别调整)。",
		"First normalises each muscle by its maximum within the \"normalisation range\", then computes phase-sync statistics over the normalised data within the \"statistics range\". Two files are produced in one run. The two ranges can be selected independently (defaulting to the same range, adjustable separately).",
		"まず各筋肉について「正規化範囲」内の最大値で正規化(除数)し、その後正規化済みデータに対して「統計範囲」内でフェーズ同期統計を算出します。1回の実行で2つのファイルを生成します。正規化範囲と統計範囲は個別に選択可能(既定では同一、別々に調整可)。",
	},
	KeyPanelMuscleRatioTitle: {"肌肉比值分析", "肌肉比值分析", "Muscle Ratio Analysis", "筋肉比解析"},
	KeyPanelMuscleRatioDesc: {
		"批次計算 manifest 中所有 subject 的 4 對右側肌肉比值:R.RA/R.ES、R.IL/R.GMax、R.RF/R.BF、R.TA&IO/R.MF。每個 subject 產出兩個 CSV:完整時間序列 + 分期點切片(10 個分期 + 9 個相鄰中間點 + 最多 2 個跨段中間點 = 最多 21 列)。",
		"批量计算 manifest 中所有 subject 的 4 对右侧肌肉比值:R.RA/R.ES、R.IL/R.GMax、R.RF/R.BF、R.TA&IO/R.MF。每个 subject 产出两个 CSV:完整时间序列 + 分期点切片(10 个分期 + 9 个相邻中间点 + 最多 2 个跨段中间点 = 最多 21 行)。",
		"Batch-compute four right-side muscle-pair ratios for every subject in the manifest: R.RA/R.ES, R.IL/R.GMax, R.RF/R.BF, R.TA&IO/R.MF. Each subject produces two CSVs: the full time series and a phase-point slice (10 phases + 9 adjacent midpoints + up to 2 biomechanical-interval midpoints = up to 21 rows).",
		"manifest 内の全 subject に対し、右側4対の筋肉比 (R.RA/R.ES、R.IL/R.GMax、R.RF/R.BF、R.TA&IO/R.MF) をバッチ計算します。各 subject につき2つの CSV を生成:完全な時系列 + フェーズ点スライス (10 フェーズ + 9 隣接中間点 + 最大 2 ステージ間中間点 = 最大 21 行)。",
	},

	// Phase 2 common buttons. (KeyButtonBrowse 用 legacy entry — 字串一致)
	KeyButtonBack:              {"返回", "返回", "Back", "戻る"},
	KeyButtonStartCalculate:    {"開始計算", "开始计算", "Start Calculation", "計算開始"},
	KeyButtonStartNormalize:    {"開始標準化", "开始标准化", "Start Normalisation", "正規化開始"},
	KeyButtonStartAnalyze:      {"開始分析", "开始分析", "Start Analysis", "解析開始"},
	KeyButtonStartBatchAnalyze: {"開始批次分析", "开始批量分析", "Start Batch Analysis", "バッチ解析開始"},
	KeyButtonDownloadChart:     {"下載圖表", "下载图表", "Download Chart", "チャートをダウンロード"},
	KeyButtonOpenOutputFolder:  {"開啟輸出資料夾", "打开输出文件夹", "Open Output Folder", "出力フォルダを開く"},

	// MaxMean panel form labels.
	KeyFormLabelProcessMode:     {"處理模式", "处理模式", "Processing Mode", "処理モード"},
	KeyFormOptionSingleFile:     {"單檔案處理", "单文件处理", "Single File", "単一ファイル"},
	KeyFormOptionBatchFolder:    {"批次處理資料夾", "批量处理文件夹", "Batch Folder", "フォルダ一括処理"},
	KeyFormLabelInputFile:       {"選擇資料檔案", "选择数据文件", "Select Data File", "データファイル選択"},
	KeyFormLabelInputFolder:     {"選擇資料夾", "选择文件夹", "Select Folder", "フォルダ選択"},
	KeyFormLabelWindowSize:      {"視窗大小(資料點數)", "窗口大小(数据点数)", "Window Size (data points)", "ウィンドウサイズ(データポイント数)"},
	KeyFormHelpWindowSize:       {"用於計算移動平均值的視窗大小", "用于计算移动平均值的窗口大小", "Window size used to compute the moving average", "移動平均を計算するためのウィンドウサイズ"},
	KeyFormLabelTimeRange:       {"時間範圍(選填)", "时间范围(选填)", "Time Range (optional)", "時間範囲 (任意)"},
	KeyFormPlaceholderStartTime: {"開始時間(秒)", "开始时间(秒)", "Start time (sec)", "開始時刻 (秒)"},
	KeyFormPlaceholderEndTime:   {"結束時間(秒)", "结束时间(秒)", "End time (sec)", "終了時刻 (秒)"},
	KeyFormHelpTimeRange:        {"留空表示處理整個檔案", "留空表示处理整个文件", "Leave blank to process the entire file", "空欄の場合はファイル全体を処理"},

	// Normalize panel form labels.
	KeyFormLabelMainFile:         {"主要資料檔案", "主要数据文件", "Main Data File", "メインデータファイル"},
	KeyFormLabelReferenceFile:    {"參考資料檔案", "参考数据文件", "Reference Data File", "参照データファイル"},
	KeyFormLabelOutputName:       {"輸出檔名(選填)", "输出文件名(选填)", "Output Filename (optional)", "出力ファイル名 (任意)"},
	KeyFormPlaceholderOutputName: {"留空使用預設名稱", "留空使用默认名称", "Leave blank for default name", "空欄の場合は既定名を使用"},

	// Chart panel form labels.
	KeyFormLabelChartFile:      {"選擇資料檔案", "选择数据文件", "Select Data File", "データファイル選択"},
	KeyFormLabelChartTitle:     {"圖表標題", "图表标题", "Chart Title", "チャートタイトル"},
	KeyFormDefaultChartTitle:   {"EMG 資料分析圖表", "EMG 数据分析图表", "EMG Data Analysis Chart", "EMG データ解析チャート"},
	KeyFormLabelSelectColumns:  {"選擇要顯示的欄位", "选择要显示的字段", "Select Columns to Display", "表示するカラムを選択"},
	KeyFormHelpSelectFileFirst: {"請先選擇檔案", "请先选择文件", "Please select a file first", "まずファイルを選択してください"},
	KeyChartPreviewTitle:       {"即時圖表預覽", "即时图表预览", "Live Chart Preview", "ライブチャートプレビュー"},

	// Phase panel form labels.
	KeyFormLabelPhaseFile:         {"選擇資料檔案", "选择数据文件", "Select Data File", "データファイル選択"},
	KeyFormLabelPhasePoints:       {"階段時間點", "阶段时间点", "Phase Time Points", "段階の時刻"},
	KeyFormPlaceholderPhasePoints: {"例如: 0.5, 1.0, 1.5, 2.0", "例如: 0.5, 1.0, 1.5, 2.0", "e.g. 0.5, 1.0, 1.5, 2.0", "例: 0.5, 1.0, 1.5, 2.0"},
	KeyFormHelpPhasePoints:        {"輸入各階段的時間點(秒),用逗號分隔", "输入各阶段的时间点(秒),用逗号分隔", "Enter phase time points in seconds, separated by commas", "各段階の時刻 (秒) をカンマ区切りで入力"},
	KeyFormLabelPhaseLabels:       {"階段標籤", "阶段标签", "Phase Labels", "段階ラベル"},
	KeyFormPlaceholderPhaseLabels: {"每行一個標籤", "每行一个标签", "One label per line", "1行に1ラベル"},

	// Shared labels — phasesync / cci / normalized / muscle.
	KeyFormLabelManifest:                {"選擇分期總檔案", "选择分期总文件", "Select Manifest File", "マニフェストファイル選択"},
	KeyFormPlaceholderManifest:          {"選擇或拖放分期總檔案 (.csv)", "选择或拖放分期总文件 (.csv)", "Select or drop the manifest file (.csv)", "マニフェストファイル (.csv) を選択またはドロップ"},
	KeyFormLabelDataFolder:              {"選擇數據資料夾", "选择数据文件夹", "Select Data Folder", "データフォルダ選択"},
	KeyFormPlaceholderDataFolder:        {"選擇包含所有數據檔案的資料夾", "选择包含所有数据文件的文件夹", "Select the folder containing all data files", "全データファイルを含むフォルダを選択"},
	KeyFormPlaceholderDataFolderEmg:     {"選擇包含 EMG 數據檔案的資料夾", "选择包含 EMG 数据文件的文件夹", "Select the folder containing EMG data files", "EMG データファイルを含むフォルダを選択"},
	KeyFormLabelSubject:                 {"選擇分析主題", "选择分析主题", "Select Subject", "解析対象を選択"},
	KeyFormPlaceholderLoadManifestFirst: {"請先載入分期總檔案", "请先载入分期总文件", "Load the manifest file first", "先にマニフェストファイルを読み込んでください"},
	KeyFormOptionSelectSubject:          {"請選擇主題", "请选择主题", "Select a subject", "対象を選択"},
	KeyFormLabelStartPhase:              {"開始分期點", "开始分期点", "Start Phase Point", "開始フェーズ点"},
	KeyFormLabelEndPhase:                {"結束分期點", "结束分期点", "End Phase Point", "終了フェーズ点"},
	KeyFormOptionSelect:                 {"請選擇", "请选择", "Select", "選択"},

	// Normalized phase sync extras.
	KeyFormLabelNormStartPhase:  {"標準化開始分期點", "标准化开始分期点", "Normalisation Start Phase Point", "正規化開始フェーズ点"},
	KeyFormLabelNormEndPhase:    {"標準化結束分期點", "标准化结束分期点", "Normalisation End Phase Point", "正規化終了フェーズ点"},
	KeyFormLabelStatsStartPhase: {"統計開始分期點", "统计开始分期点", "Statistics Start Phase Point", "統計開始フェーズ点"},
	KeyFormLabelStatsEndPhase:   {"統計結束分期點", "统计结束分期点", "Statistics End Phase Point", "統計終了フェーズ点"},

	// Result section labels.
	KeyResultSectionTitle:        {"分析結果", "分析结果", "Analysis Results", "解析結果"},
	KeyResultLabelSubject:        {"主題:", "主题:", "Subject:", "対象:"},
	KeyResultLabelAnalysisRange:  {"分析區間:", "分析区间:", "Analysis Range:", "解析範囲:"},
	KeyResultLabelOutputFile:     {"輸出檔案:", "输出文件:", "Output File:", "出力ファイル:"},
	KeyResultLabelAnalysisReport: {"分析報告", "分析报告", "Analysis Report", "解析レポート"},
	KeyResultLabelMuscles:        {"各肌肉數值", "各肌肉数值", "Per-Muscle Values", "筋肉別の数値"},
	KeyResultLabelPhasePositions: {"分期點位置 (步態週期 %):", "分期点位置 (步态周期 %):", "Phase positions (gait cycle %):", "フェーズ点位置 (歩行周期 %):"},
	KeyResultLabelMusclePairs:    {"肌肉配對:", "肌肉配对:", "Muscle Pairs:", "筋肉ペア:"},
	KeyResultLabelCSVOutput:      {"CSV 輸出:", "CSV 输出:", "CSV Output:", "CSV 出力:"},
	KeyResultLabelNormRange:      {"標準化區間:", "标准化区间:", "Normalisation Range:", "正規化範囲:"},
	KeyResultLabelStatsRange:     {"統計區間:", "统计区间:", "Statistics Range:", "統計範囲:"},
	KeyResultLabelOutputNorm:     {"Output 1(標準化 EMG):", "Output 1(标准化 EMG):", "Output 1 (Normalised EMG):", "Output 1 (正規化 EMG):"},
	KeyResultLabelOutputStats:    {"Output 2(分期統計):", "Output 2(分期统计):", "Output 2 (Phase Statistics):", "Output 2 (フェーズ統計):"},
	KeyResultLabelPhaseDisplay:   {"分期點顯示:", "分期点显示:", "Phase Points Display:", "フェーズ点表示:"},
	KeyResultMuscleSubjectCount:  {"共 %d 個主題", "共 %d 个主题", "%d subjects in total", "計 %d 件の対象"},
	KeyResultNormalizedHelpText: {
		"「標準化區間最大值」為標準化前在標準化區間內的原始最大值(用作除數);「標準化後區間平均」為標準化後在統計區間內的平均值。Output 2 中各肌肉最大值在「標準化區間與統計區間相同時」會列為 1.000000(區間內 max 除以自己 = 1);當兩區間不同時,該數值為 max(統計區間) / max(標準化區間),可能小於或大於 1。",
		"「标准化区间最大值」为标准化前在标准化区间内的原始最大值(用作除数);「标准化后区间平均」为标准化后在统计区间内的平均值。Output 2 中各肌肉最大值在「标准化区间与统计区间相同时」会列为 1.000000(区间内 max 除以自身 = 1);当两区间不同时,该数值为 max(统计区间) / max(标准化区间),可能小于或大于 1。",
		"\"Normalisation Range Max\" is the raw maximum within the normalisation range before normalisation (used as divisor); \"Normalised Range Mean\" is the mean within the statistics range after normalisation. In Output 2, each muscle's max is 1.000000 when the normalisation and statistics ranges coincide (range max divided by itself = 1); when the ranges differ, the value equals max(stats range) / max(norm range), which may be less than or greater than 1.",
		"「正規化範囲の最大値」は正規化前に正規化範囲内で取得した生の最大値(除数として使用)、「正規化後範囲の平均」は正規化後に統計範囲内で計算した平均値です。Output 2 では、正規化範囲と統計範囲が一致する場合、各筋肉の最大値は 1.000000 となります(範囲内 max を自身で割ると 1);両範囲が異なる場合、その値は max(統計範囲) / max(正規化範囲) となり、1 より小さくも大きくもなり得ます。",
	},

	// Table headers (Normalized & MuscleRatio panels).
	KeyTableHeaderMuscle:      {"肌肉", "肌肉", "Muscle", "筋肉"},
	KeyTableHeaderNormMax:     {"標準化區間最大值", "标准化区间最大值", "Normalisation Range Max", "正規化範囲の最大値"},
	KeyTableHeaderNormMean:    {"標準化後區間平均", "标准化后区间平均", "Normalised Range Mean", "正規化後範囲の平均"},
	KeyTableHeaderSubject:     {"主題", "主题", "Subject", "対象"},
	KeyTableHeaderStatus:      {"狀態", "状态", "Status", "状態"},
	KeyTableHeaderOutputAll:   {"Output 1 (時間序列)", "Output 1 (时间序列)", "Output 1 (Time Series)", "Output 1 (時系列)"},
	KeyTableHeaderOutputPhase: {"Output 2 (分期切片)", "Output 2 (分期切片)", "Output 2 (Phase Slice)", "Output 2 (フェーズスライス)"},
	KeyTableHeaderDuration:    {"耗時 (ms)", "耗时 (ms)", "Duration (ms)", "所要時間 (ms)"},
	KeyTableHeaderMessage:     {"訊息", "消息", "Message", "メッセージ"},

	// Phase 2 dialog title supplements.
	KeyDialogTitleComplete:      {"完成", "完成", "Complete", "完了"},
	KeyDialogTitlePartialFailed: {"部分失敗", "部分失败", "Partial Failure", "一部失敗"},
	KeyDialogTitleHint:          {"提示", "提示", "Notice", "お知らせ"},

	// Phase 2 static error messages.
	KeyErrorMsgOnlyCSV:               {"只支援 CSV 檔案", "只支持 CSV 文件", "Only CSV files are supported", "CSV ファイルのみ対応"},
	KeyErrorMsgSelectInputFile:       {"請選擇資料檔案", "请选择数据文件", "Please select a data file", "データファイルを選択してください"},
	KeyErrorMsgSelectInputFolder:     {"請選擇資料夾", "请选择文件夹", "Please select a folder", "フォルダを選択してください"},
	KeyErrorMsgSelectBothFiles:       {"請選擇主要資料檔案和參考資料檔案", "请选择主要数据文件和参考数据文件", "Please select both main and reference data files", "メインファイルと参照ファイルの両方を選択してください"},
	KeyErrorMsgChartSelectColumns:    {"請選擇至少一個欄位", "请选择至少一个字段", "Please select at least one column", "少なくとも1つのカラムを選択してください"},
	KeyErrorMsgChartNotFound:         {"請先預覽圖表", "请先预览图表", "Please preview the chart first", "先にチャートをプレビューしてください"},
	KeyErrorMsgCCIChartNotFound:      {"找不到圖表", "找不到图表", "Chart not found", "チャートが見つかりません"},
	KeyErrorMsgPhaseInputs:           {"請選擇資料檔案並輸入階段時間點", "请选择数据文件并输入阶段时间点", "Please select a data file and enter phase time points", "データファイルを選択し、段階の時刻を入力してください"},
	KeyErrorMsgPhasePointsCount:      {"時間點數量應該比標籤數量多1", "时间点数量应该比标签数量多1", "Number of time points must be one more than the number of labels", "時刻の数はラベル数より1つ多い必要があります"},
	KeyErrorMsgFillRequiredFields:    {"請填寫所有必要欄位", "请填写所有必要字段", "Please fill in all required fields", "必須項目をすべて入力してください"},
	KeyErrorMsgMuscleFillFields:      {"請選擇分期總檔案與數據資料夾", "请选择分期总文件与数据文件夹", "Please select both the manifest file and the data folder", "マニフェストファイルとデータフォルダの両方を選択してください"},
	KeyErrorMsgOpenOutputFolderFail:  {"無法開啟輸出資料夾", "无法打开输出文件夹", "Unable to open the output folder", "出力フォルダを開けません"},
	KeyErrorMsgEChartsNotFound:       {"ECharts 未找到", "ECharts 未找到", "ECharts not found", "ECharts が見つかりません"},
	KeyErrorMsgChartElementNotFound:  {"找不到圖表元素", "找不到图表元素", "Chart element not found", "チャート要素が見つかりません"},
	KeyErrorMsgChartInstanceNotFound: {"找不到圖表實例", "找不到图表实例", "Chart instance not found", "チャートインスタンスが見つかりません"},

	// Phase 2 dynamic error messages.
	KeyErrorMsgCalculationFailedDyn:   {"計算失敗:%v", "计算失败:%v", "Calculation failed: %v", "計算に失敗しました: %v"},
	KeyErrorMsgNormalizationFailedDyn: {"標準化失敗:%v", "标准化失败:%v", "Normalisation failed: %v", "正規化に失敗しました: %v"},
	KeyErrorMsgChartGenFailedDyn:      {"圖表生成失敗:%v", "图表生成失败:%v", "Chart generation failed: %v", "チャート生成に失敗しました: %v"},
	KeyErrorMsgChartDownloadFailedDyn: {"下載失敗:%v", "下载失败:%v", "Download failed: %v", "ダウンロードに失敗しました: %v"},
	KeyErrorMsgPhaseAnalysisFailedDyn: {"階段分析失敗:%v", "阶段分析失败:%v", "Phase analysis failed: %v", "段階解析に失敗しました: %v"},
	KeyErrorMsgConfigSaveFailedDyn:    {"儲存配置失敗:%v", "保存配置失败:%v", "Failed to save configuration: %v", "設定の保存に失敗しました: %v"},
	KeyErrorMsgConfigResetFailedDyn:   {"重設配置失敗:%v", "重置配置失败:%v", "Failed to reset configuration: %v", "設定のリセットに失敗しました: %v"},
	KeyErrorMsgConfigImportFailedDyn:  {"匯入配置失敗:%v", "导入配置失败:%v", "Failed to import configuration: %v", "設定のインポートに失敗しました: %v"},
	KeyErrorMsgFilePickerFailedDyn:    {"開啟檔案選擇器失敗:%v", "打开文件选择器失败:%v", "Failed to open file picker: %v", "ファイル選択ダイアログを開けませんでした: %v"},
	KeyErrorMsgLoadColumnsFailedDyn:   {"讀取欄位失敗:%v", "读取字段失败:%v", "Failed to load columns: %v", "カラムの読み込みに失敗しました: %v"},
	KeyErrorMsgChartPreviewFailedDyn:  {"即時預覽失敗:%v", "即时预览失败:%v", "Live preview failed: %v", "ライブプレビューに失敗しました: %v"},
	KeyErrorMsgLoadSubjectsFailedDyn:  {"載入主題失敗:%v", "载入主题失败:%v", "Failed to load subjects: %v", "対象の読み込みに失敗しました: %v"},
	KeyErrorMsgAnalysisFailedDyn:      {"分析失敗:%v", "分析失败:%v", "Analysis failed: %v", "解析に失敗しました: %v"},
	KeyErrorMsgLanguageSwitchFailed:   {"切換語言失敗:%v", "切换语言失败:%v", "Failed to switch language: %v", "言語切り替えに失敗しました: %v"},

	// Phase 2 success messages.
	KeyOkMsgCalculationDone: {
		"計算完成!結果已儲存至:\n%s",
		"计算完成!结果已保存至:\n%s",
		"Calculation complete. Result saved to:\n%s",
		"計算が完了しました。結果の保存先:\n%s",
	},
	KeyOkMsgNormalizationDone: {
		"標準化完成!結果已儲存至:\n%s",
		"标准化完成!结果已保存至:\n%s",
		"Normalisation complete. Result saved to:\n%s",
		"正規化が完了しました。結果の保存先:\n%s",
	},
	KeyOkMsgChartGenerated: {
		"圖表已生成並保存至:\n%s",
		"图表已生成并保存至:\n%s",
		"Chart generated and saved to:\n%s",
		"チャートを生成し保存しました:\n%s",
	},
	KeyOkMsgChartDownloaded: {"圖表已下載至:%s", "图表已下载至:%s", "Chart downloaded to: %s", "チャートをダウンロードしました: %s"},
	KeyOkMsgPhaseAnalysisDone: {
		"階段分析完成!結果已儲存至:\n%s",
		"阶段分析完成!结果已保存至:\n%s",
		"Phase analysis complete. Result saved to:\n%s",
		"段階解析が完了しました。結果の保存先:\n%s",
	},
	KeyOkMsgConfigSaved:     {"配置已儲存", "配置已保存", "Configuration saved", "設定を保存しました"},
	KeyOkMsgConfigReset:     {"配置已重設為預設值", "配置已重置为默认值", "Configuration reset to defaults", "設定をデフォルトに戻しました"},
	KeyOkMsgConfigImported:  {"配置已匯入並儲存", "配置已导入并保存", "Configuration imported and saved", "設定をインポートして保存しました"},
	KeyOkMsgAnalysisDone:    {"分析完成!結果已保存至:%s", "分析完成!结果已保存至:%s", "Analysis complete. Result saved to: %s", "解析が完了しました。結果の保存先: %s"},
	KeyOkMsgCCIAnalysisDone: {"分析完成!\nCSV: %s", "分析完成!\nCSV: %s", "Analysis complete.\nCSV: %s", "解析が完了しました。\nCSV: %s"},
	KeyOkMsgNormalizedAnalysisDone: {
		"分析完成!\n標準化 EMG: %s\n分期統計: %s",
		"分析完成!\n标准化 EMG: %s\n分期统计: %s",
		"Analysis complete.\nNormalised EMG: %s\nPhase statistics: %s",
		"解析が完了しました。\n正規化 EMG: %s\nフェーズ統計: %s",
	},

	// Phase 2 info messages.
	KeyInfoMsgOutputFolder: {"輸出檔案已保存至:%s", "输出文件已保存至:%s", "Output file saved to: %s", "出力ファイルの保存先: %s"},

	// Phase 2 status bar messages.
	KeyStatusAppReady:              {"應用程序已就緒", "应用程序已就绪", "Application ready", "アプリケーション準備完了"},
	KeyStatusCalculationRunning:    {"正在計算最大平均值...", "正在计算最大平均值...", "Computing max mean...", "最大平均値を計算中..."},
	KeyStatusCalculationDone:       {"計算完成", "计算完成", "Calculation complete", "計算完了"},
	KeyStatusCalculationFailed:     {"計算失敗", "计算失败", "Calculation failed", "計算失敗"},
	KeyStatusNormalizationRunning:  {"正在進行資料標準化...", "正在进行数据标准化...", "Normalising data...", "データを正規化中..."},
	KeyStatusNormalizationDone:     {"標準化完成", "标准化完成", "Normalisation complete", "正規化完了"},
	KeyStatusNormalizationFailed:   {"標準化失敗", "标准化失败", "Normalisation failed", "正規化失敗"},
	KeyStatusChartGenerating:       {"正在生成圖表...", "正在生成图表...", "Generating chart...", "チャートを生成中..."},
	KeyStatusChartGenerated:        {"圖表生成完成", "图表生成完成", "Chart generation complete", "チャート生成完了"},
	KeyStatusChartGenerationFailed: {"圖表生成失敗", "图表生成失败", "Chart generation failed", "チャート生成失敗"},
	KeyStatusChartDownloading:      {"正在下載圖表...", "正在下载图表...", "Downloading chart...", "チャートをダウンロード中..."},
	KeyStatusChartDownloadDone:     {"圖表下載完成", "图表下载完成", "Chart download complete", "チャートダウンロード完了"},
	KeyStatusChartDownloadFailed:   {"圖表下載失敗", "图表下载失败", "Chart download failed", "チャートダウンロード失敗"},
	KeyStatusPhaseAnalysisRunning:  {"正在進行階段分析...", "正在进行阶段分析...", "Running phase analysis...", "段階解析を実行中..."},
	KeyStatusPhaseAnalysisDone:     {"階段分析完成", "阶段分析完成", "Phase analysis complete", "段階解析完了"},
	KeyStatusPhaseAnalysisFailed:   {"階段分析失敗", "阶段分析失败", "Phase analysis failed", "段階解析失敗"},
	KeyStatusPhasesyncRunning:      {"正在進行分期同步分析...", "正在进行分期同步分析...", "Running phase sync analysis...", "フェーズ同期解析を実行中..."},
	KeyStatusCCIRunning:            {"正在進行 CCI Rudolph 分析...", "正在进行 CCI Rudolph 分析...", "Running CCI Rudolph analysis...", "CCI Rudolph 解析を実行中..."},
	KeyStatusCCIChartDownloading:   {"正在下載 CCI 圖表...", "正在下载 CCI 图表...", "Downloading CCI chart...", "CCI チャートをダウンロード中..."},
	KeyStatusNormalizedRunning:     {"正在進行標準化分期同步分析...", "正在进行标准化分期同步分析...", "Running normalised phase sync analysis...", "正規化フェーズ同期解析を実行中..."},
	KeyStatusMuscleRunning:         {"正在批次計算肌肉比值...", "正在批量计算肌肉比值...", "Batch-computing muscle ratios...", "筋肉比をバッチ計算中..."},
	KeyStatusMusclePartialFailed:   {"部分失敗", "部分失败", "Partial failure", "一部失敗"},
	KeyStatusAnalysisDone:          {"分析完成", "分析完成", "Analysis complete", "解析完了"},
	KeyStatusAnalysisFailed:        {"分析失敗", "分析失败", "Analysis failed", "解析失敗"},
	KeyStatusSubjectsLoaded:        {"已載入 %d 個主題", "已载入 %d 个主题", "Loaded %d subjects", "%d 件の対象を読み込みました"},

	// Phase 2 dynamic column-loading status.
	KeyStatusLoadingColumns:    {"載入欄位中...", "载入字段中...", "Loading columns...", "カラムを読み込み中..."},
	KeyStatusLoadColumnsFailed: {"載入欄位失敗", "载入字段失败", "Failed to load columns", "カラムの読み込みに失敗しました"},

	// Phase 2 console warnings.
	KeyWarnGetVersionFailed: {"無法取得版本號", "无法获取版本号", "Unable to retrieve version", "バージョンを取得できません"},

	// Phase 2 chart placeholder.
	KeyChartTitlePlaceholder: {"圖表標題", "图表标题", "Chart Title", "チャートタイトル"},

	// Phase 2 import config.
	KeyErrorMsgInvalidConfigFormat: {"無效的配置檔案格式", "无效的配置文件格式", "Invalid configuration file format", "無効な設定ファイル形式"},
}

// buildTranslationMap builds the translation map for a specific locale.
// Unknown locales fall back to zh-TW so the returned map is never populated
// with empty strings — keeps the stateless GetTranslationMap (globalI18n == nil)
// path safe for unit tests and early-init callers (Phase 1 frontend i18n MVP).
func buildTranslationMap(locale Locale) map[string]string {
	result := make(map[string]string, len(translationData))

	effective := locale
	switch effective {
	case LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP:
		// supported locale, use as-is
	default:
		effective = LocaleZhTW
	}

	for key, entry := range translationData {
		switch effective {
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

// GetTranslationMap returns a snapshot of all translations for the given locale.
// Used by the Wails GUI binding to push the full dictionary to the frontend at once,
// avoiding per-string RPC overhead. Falls back to the configured fallback locale
// when the requested locale has no loaded messages. The returned map is a copy —
// callers may mutate it without affecting internal state.
//
// 用內建翻譯作基底,確保即使外部 translationsDir 提供 partial JSON(例如舊版
// release 產生、缺少新版加入的 panel.* / config.* 等 key),前端拿到的仍是完整
// 字典 — 缺失的 key 由內建翻譯補齊,而不會在 UI 字面 render(codex review P2 fix)。
func (i *I18n) GetTranslationMap(locale Locale) map[string]string {
	i.mutex.RLock()
	defer i.mutex.RUnlock()

	// Built-in 字典作 base — 對所有 supported locale 與 unknown locale 都會回完整字典
	// (buildTranslationMap 內部對 unknown locale 自動 fall back 為 zh-TW)。
	out := buildTranslationMap(locale)

	src, ok := i.messages[locale]
	if !ok {
		src = i.messages[i.fallback]
	}

	for k, v := range src {
		out[k] = v
	}

	return out
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

// GetTranslationMap returns a snapshot of all translations for the given locale
// using the global i18n instance. Returns built-in translations if the global
// instance has not been initialised (testing / pre-init paths).
func GetTranslationMap(locale Locale) map[string]string {
	if globalI18n == nil {
		return buildTranslationMap(locale)
	}

	return globalI18n.GetTranslationMap(locale)
}

// GetSupportedLocales returns the list of supported locales using the global i18n instance.
// 4 個 locale 是編譯期常數,不依賴 globalI18n 初始化狀態 —— 之前 nil-only-zhTW
// 的行為會誤導 caller(例如 gui.SetLanguage 認為只支援 zh-TW),修為與
// (*I18n).GetSupportedLocales 對齊(Phase 1 frontend i18n MVP)。
func GetSupportedLocales() []Locale {
	return []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}
}

// GetLocaleName returns the display name of a locale using the global i18n instance.
func GetLocaleName(locale Locale) string {
	if globalI18n == nil {
		return string(locale)
	}

	return globalI18n.GetLocaleName(locale)
}
