// Package i18n provides internationalization support for the EMG data analysis
// application, supporting Traditional Chinese, Simplified Chinese, English,
// and Japanese locales with dynamic translation loading and fallback handling.
package i18n

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"

	"count_mean/internal/security/fsperm"
)

// LoadTranslations hardening constants.
const (
	// maxTranslationFileBytes caps individual translation JSON files at 4 MiB.
	// Builtin locale dictionaries total ~50 KiB each; even a generous user
	// override with verbose translations should stay well under 1 MiB. 4 MiB
	// is a defensive ceiling — anything larger is almost certainly a misuse
	// or an attack (e.g. zip-bomb-style nesting payload, GB-of-keys DoS).
	maxTranslationFileBytes int64 = 4 * 1024 * 1024

	// maxTranslationJSONDepth caps JSON structural nesting. Builtin catalog
	// is a flat string→string map (depth 2 from outer object). Anything
	// deeper is malformed for our use case — reject before json.Unmarshal
	// allocates pathological structures.
	maxTranslationJSONDepth = 8
)

// LoadTranslations hardening errors.
var (
	// ErrTranslationFileTooLarge is returned when a translation JSON file
	// exceeds maxTranslationFileBytes (4 MiB).
	ErrTranslationFileTooLarge = errors.New("翻譯檔案超過大小上限")

	// ErrTranslationJSONTooDeep is returned when JSON nesting exceeds
	// maxTranslationJSONDepth.
	ErrTranslationJSONTooDeep = errors.New("翻譯 JSON 嵌套深度超過上限")

	// ErrTranslationKeyUnknown is returned when user JSON contains a key
	// not present in the builtin catalog for the target locale.
	ErrTranslationKeyUnknown = errors.New("翻譯 key 不存在於內建字典")

	// ErrTranslationKeyMissing is returned when user JSON omits a key
	// that the builtin catalog defines (caller must provide complete coverage).
	ErrTranslationKeyMissing = errors.New("翻譯 key 缺失")

	// ErrTranslationFormatVerbUnsupported is returned when a translation value
	// contains %w (Go's wrap-error verb, not supported by fmt.Sprintf).
	ErrTranslationFormatVerbUnsupported = errors.New("翻譯字串含不支援的 format verb %w")

	// ErrTranslationVerbCountMismatch is returned when a user-supplied
	// translation has a different number of fmt verbs than the master locale.
	// Drift between the format string (user-supplied) and the args (compiled
	// in source code) causes %!(EXTRA …) / %!(MISSING) tokens in production.
	ErrTranslationVerbCountMismatch = errors.New("翻譯字串 verb 數量與 master locale 不符")
)

// translationVerbRegexp matches Go fmt-style verbs:
//
//	%[<flags>][<width>][.<precision>][<verb>]
//
// %% escapes are not verbs and are excluded by leading group `[^%]`.
//
// We allow common verbs: d, f, s, v, t, b, c, e, g, x, X, q, T, U, p
// — reject %w (wrap-error, not Sprintf-safe) via dedicated regex below.
//
//nolint:gochecknoglobals // compile-once regex
var translationVerbRegexp = regexp.MustCompile(`%[+\-# 0]*\d*(?:\.\d+)*[dfsbcvtgxXeEqUTp]`)

// translationPercentWRegexp matches the %w verb (fmt.Errorf-only, panic-prone
// when fed to Sprintf). Used as a positive reject.
//
//nolint:gochecknoglobals // compile-once regex
var translationPercentWRegexp = regexp.MustCompile(`%[+\-# 0]*\d*(?:\.\d+)*w`)

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

	// Muscle Ratio Analysis Messages — surfaced through SubjectResult.Error or
	// MuscleRatioResult.Message。%w verb 禁止:catalog 字串只能含 fmt.Sprintf-
	// compatible verbs (%s %v %d %q);error wrap 由 caller 用
	// `fmt.Errorf("%s: %w", i18n.T(key), innerErr)` pattern 處理。
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

	KeyErrorMuscleRatioSubjectEmptyEMG           = "error.muscle_ratio.subject.empty_emg"
	KeyErrorMuscleRatioSubjectInsufficientPhases = "error.muscle_ratio.subject.insufficient_phases"
	KeyErrorMuscleRatioSubjectPhaseOutOfEMGRange = "error.muscle_ratio.subject.phase_out_of_emg_range"
	KeyErrorMuscleRatioSubjectCollision          = "error.muscle_ratio.subject_collision"

	// 批次取消(Wails Shutdown / user cancel)時 caller 訊息。
	KeyErrorMuscleRatioCancelled = "error.muscle_ratio.cancelled"

	// CCI Analysis Messages — 與 muscle_ratio 的格式一致:user-facing 錯誤訊息由
	// i18n.T 取得後再 wrap;catalog 字串只含 fmt.Sprintf-compatible verbs
	// (%s %v %d %.3f),error wrap 由 caller 用
	// `fmt.Errorf("%s: %w", i18n.T(key), inner)` pattern 處理。
	KeyErrorCCIParseManifestFailed     = "error.cci.parse_manifest_failed"
	KeyErrorCCIInvalidSubjectIndex     = "error.cci.invalid_subject_index"
	KeyErrorCCIParseEMGFailed          = "error.cci.parse_emg_failed"
	KeyErrorCCIBuildChannelMapFailed   = "error.cci.build_channel_map_failed"
	KeyErrorCCIExtractGaitRangeFailed  = "error.cci.extract_gait_range_failed"
	KeyErrorCCIInsufficientPhasePoints = "error.cci.insufficient_phase_points"
	KeyErrorCCIGaitDurationTooSmall    = "error.cci.gait_duration_too_small"
	KeyErrorCCIEMGEmpty                = "error.cci.emg_empty"
	KeyErrorCCIGaitStartBelowEMGMin    = "error.cci.gait_start_below_emg_min"
	KeyErrorCCIGaitEndAboveEMGMax      = "error.cci.gait_end_above_emg_max"
	KeyErrorCCIOutputDirInvalid        = "error.cci.output_dir_invalid"
	KeyErrorCCIMkdirFailed             = "error.cci.mkdir_failed"
	KeyErrorCCIChannelLenMismatch      = "error.cci.channel_len_mismatch"
	KeyErrorCCIMissingMuscleChannel    = "error.cci.missing_muscle_channel"
	KeyErrorCCIRenderChartFailed       = "error.cci.render_chart_failed"

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

// builtinDicts 是 4 個內建翻譯字典的 router map。每個 Locale 對應一個
// translations_<locale>.go 提供的 immutable map。
// 「unknown locale 回 zh-TW」是 buildTranslationMap 的契約,此 map 本身不負責 fallback。
//
//nolint:gochecknoglobals // builtin locale dictionaries are intentional for i18n
var builtinDicts = map[Locale]map[string]string{
	LocaleZhTW: translationsZhTW,
	LocaleZhCN: translationsZhCN,
	LocaleEnUS: translationsEnUS,
	LocaleJaJP: translationsJaJP,
}

// buildTranslationMap returns a fresh copy of the builtin dictionary for locale.
// Callers may mutate the returned map without affecting internal state.
//
// **Unknown locale fallback**:任何不在 builtinDicts 的 locale(例如 "fr-FR")
// 都會 fall back 到 zh-TW,確保 GetTranslationMap 的 stateless path
// (globalI18n == nil)永遠回完整字典而非 nil/空 map。這層保護讓 unit test 與
// early-init caller 不必先檢查 locale 合法性。
func buildTranslationMap(locale Locale) map[string]string {
	src, ok := builtinDicts[locale]
	if !ok {
		src = builtinDicts[LocaleZhTW]
	}

	result := make(map[string]string, len(src))
	for k, v := range src {
		result[k] = v
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
//
// External JSON 採 overlay 而非 replace:先以 buildTranslationMap 載入完整
// builtin catalog 作 base,再把 JSON 的 key/value 覆寫上去。舊版 release 留下
// 的 JSON 缺新版加入的 key 時,i18n.T 仍可從 builtin 取得翻譯而非回 raw key
// (後者會在 UI 字面 render 出 catalog 內部識別字)。T() 與 GetTranslationMap
// 共享同一份 i.messages[locale],兩條 path 都帶 builtin 保底。
//
// # 嚴格 input validation
//
// 在 overlay 之前對 user-supplied JSON 做嚴格驗證,攻擊面包含:oversized
// payload (memory DoS)、deep nesting (parser pathological cases)、未知 key
// (trust leak)、%w verb (Sprintf 不接受,production string 變 `%!w(...)`)、
// verb count mismatch (出現 %!(EXTRA …) / %!(MISSING) tokens)。任一條件命中
// 即 reject 整個 locale 載入,不可 silent partial-apply。
//
// # Atomic publish (all-or-nothing)
//
// 在 staging map (newMessages) 內處理所有 locale,只在**全部成功**後才把 staging
// commit 到 i.messages。任一 locale 失敗即 return err、i.messages 完全不變,避免
// partial state(例如 zh-TW 是新版、en-US 是舊版的詭異組合)。LoadTranslations
// 是 cold path,鎖區間覆蓋整個讀檔 + 驗證 + commit 對效能無影響。
func (i *I18n) LoadTranslations(translationsDir string) error {
	i.mutex.Lock()
	defer i.mutex.Unlock()

	// Load each supported locale
	locales := []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}

	// Staging map — 全部 locale 都跑完驗證才整批 commit 到 i.messages。
	// 任一步失敗 return err,i.messages 完全不變(no partial state)。
	newMessages := make(map[Locale]map[string]string, len(locales))

	for _, locale := range locales {
		// Built-in 字典作 base — 確保所有 key 都有預設值，即使外部 JSON 缺某些 key。
		merged := buildTranslationMap(locale)

		filename := filepath.Join(translationsDir, fmt.Sprintf("%s.json", locale))

		// Check if file exists
		info, statErr := os.Stat(filename)
		if os.IsNotExist(statErr) {
			// 沒有外部 JSON：直接用 builtin。
			newMessages[locale] = merged

			continue
		}
		if statErr != nil {
			return fmt.Errorf("無法檢查翻譯文件 %s: %w", filename, statErr)
		}

		// File size cap:在 ReadFile 之前看 stat,避免 4 MiB+ 的 payload 把
		// process memory 吃光(LoadTranslations 在 init path,無 streaming 機會)。
		if info.Size() > maxTranslationFileBytes {
			return fmt.Errorf("%w: %s (%d bytes > %d)",
				ErrTranslationFileTooLarge, filename, info.Size(), maxTranslationFileBytes)
		}

		// Load from file
		data, err := os.ReadFile(filename) //nolint:gosec // filename is constructed from known values
		if err != nil {
			return fmt.Errorf("無法讀取翻譯文件 %s: %w", filename, err)
		}

		// JSON depth cap:用 json.Decoder + Token 流走訪追蹤深度,Unmarshal
		// 全量配置之前 fail-fast。
		if err := checkTranslationJSONDepth(data); err != nil {
			return fmt.Errorf("翻譯文件 %s: %w", filename, err)
		}

		var translations map[string]string
		if err := json.Unmarshal(data, &translations); err != nil {
			return fmt.Errorf("解析翻譯文件 %s 失敗: %w", filename, err)
		}

		// Key set 嚴格 match — user JSON 不可含 builtin 沒有的 key(trust leak);
		// 漏 key 由 overlay 從 builtin 補。iterate user translations 比對。
		if err := validateTranslationKeys(merged, translations); err != nil {
			return fmt.Errorf("翻譯文件 %s: %w", filename, err)
		}

		// Format verb 守門:reject %w,並驗 verb count 與 master locale 一致。
		// master locale = zh-TW builtin(catalog 主版本)。
		masterDict := builtinDicts[LocaleZhTW]
		for k, v := range translations {
			if translationPercentWRegexp.MatchString(v) {
				return fmt.Errorf("%w: file=%s key=%q value=%q",
					ErrTranslationFormatVerbUnsupported, filename, k, v)
			}
			masterValue, ok := masterDict[k]
			if !ok {
				// builtin zh-TW 沒這個 key — 但 validateTranslationKeys 已守過,
				// 走到這裡的 key 必然在 merged 內,但可能僅存於其他 locale 的
				// builtin。fallback 到 merged[k] 比 zh-TW key 漏 case 更保守。
				masterValue = merged[k]
			}
			masterVerbs := countTranslationVerbs(masterValue)
			userVerbs := countTranslationVerbs(v)
			if masterVerbs != userVerbs {
				return fmt.Errorf("%w: file=%s key=%q master=%d user=%d",
					ErrTranslationVerbCountMismatch, filename, k, masterVerbs, userVerbs)
			}
		}

		// Overlay：外部 JSON 覆蓋 builtin 對應 key，缺漏的 key 保留 builtin。
		for k, v := range translations {
			merged[k] = v
		}

		// 寫入 staging map(newMessages),**不直接**更新 i.messages。
		newMessages[locale] = merged
	}

	// 所有 locale 都驗證 + merge 成功,整批 commit。對 concurrent reader 來說,
	// 要嘛全部看到舊 state,要嘛全部看到新 state,沒有半新半舊的情境。
	i.messages = newMessages

	return nil
}

// checkTranslationJSONDepth walks the JSON token stream tracking `{`/`[` open
// vs `}`/`]` close depth. Returns ErrTranslationJSONTooDeep if depth exceeds
// maxTranslationJSONDepth at any point. Cheap O(n) — no allocation per element
// beyond what json.Decoder already does for token streaming.
func checkTranslationJSONDepth(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	depth := 0
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			// 結構性錯誤交給 json.Unmarshal 報更清楚的訊息;這裡只負責 depth。
			return nil //nolint:nilerr // let Unmarshal generate the parse error
		}
		switch delim, ok := tok.(json.Delim); {
		case ok && (delim == '{' || delim == '['):
			depth++
			if depth > maxTranslationJSONDepth {
				return fmt.Errorf("%w: depth=%d max=%d",
					ErrTranslationJSONTooDeep, depth, maxTranslationJSONDepth)
			}
		case ok && (delim == '}' || delim == ']'):
			depth--
		}
	}
}

// validateTranslationKeys ensures the user-supplied translations contain no key
// outside the builtin set. Missing keys (builtin has, user doesn't) are
// tolerated — overlay semantics fill them from builtin. Extra keys (user has,
// builtin doesn't) are rejected to prevent trust leak.
//
// 刻意保留 partial-OK 語意 — strict-equal 比對會強迫每次 locale 更新都 all-or-nothing,
// 破壞 partial JSON 自動 fallback 到 builtin 的契約。
func validateTranslationKeys(builtin, user map[string]string) error {
	for k := range user {
		if _, ok := builtin[k]; !ok {
			return fmt.Errorf("%w: %q", ErrTranslationKeyUnknown, k)
		}
	}
	return nil
}

// countTranslationVerbs returns the number of fmt verbs in a translation
// string. Used to verify user-supplied translations have the same arg count
// as the master locale entry.
func countTranslationVerbs(s string) int {
	return len(translationVerbRegexp.FindAllString(s, -1))
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
// 用內建翻譯作基底,確保即使外部 translationsDir 提供 partial JSON(舊版
// release 缺少新版 key),前端拿到的仍是完整字典 — 缺失的 key 由內建翻譯補齊,
// 不會在 UI 字面 render 出 catalog 內部識別字。
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
//
// 中文區域變體對應:
//   - zh-HK / zh-MO → LocaleZhTW (繁體中文,書面用語雖有差異仍共用 catalog)
//   - zh-SG → LocaleZhCN (新加坡華語使用簡體)
//   - 裸 zh → LocaleZhTW (與 InitI18n 預設 fallback 對齊)
//
// 偵測順序刻意把具體 region(zh-tw / zh-cn / zh-hk / zh-mo / zh-sg)放在裸 zh
// 之前,避免 HasPrefix("zh") 把 zh-cn 誤判為 zh-TW。
func parseLocale(localeStr string) Locale {
	localeStr = strings.ToLower(localeStr)

	// Handle common variations
	switch {
	case strings.HasPrefix(localeStr, "zh_tw"), strings.HasPrefix(localeStr, "zh-tw"):
		return LocaleZhTW
	case strings.HasPrefix(localeStr, "zh_cn"), strings.HasPrefix(localeStr, "zh-cn"):
		return LocaleZhCN
	case strings.HasPrefix(localeStr, "zh_hk"), strings.HasPrefix(localeStr, "zh-hk"),
		strings.HasPrefix(localeStr, "zh_mo"), strings.HasPrefix(localeStr, "zh-mo"):
		// HK / MO 使用繁體中文 — 雖然書面用語有差異但與 zh-TW 共用 catalog
		return LocaleZhTW
	case strings.HasPrefix(localeStr, "zh_sg"), strings.HasPrefix(localeStr, "zh-sg"):
		// 新加坡華語使用簡體中文
		return LocaleZhCN
	case strings.HasPrefix(localeStr, "zh"):
		// 裸 zh 或其他未知 zh 變體(zh-Hant、zh-Hans、zh-Latn 等)fallback 到 zh-TW;
		// 與 DetectSystemLocale 的最終 default 對齊,避免 caller 因「找到 ja 卻沒找到 zh」誤判。
		return LocaleZhTW
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

// Global instance — 用 atomic.Pointer 讓 InitI18n 與 T / SetLocale /
// GetTranslationMap 等讀者之間沒有 data race。每個 *I18n instance 內部的
// currentLocale / messages 已有自己的 RWMutex,atomic.Pointer 只負責「哪個
// instance 是當下生效的」這層 indirection。
//
//nolint:gochecknoglobals // Global i18n instance is intentional for convenience
var globalI18n atomic.Pointer[I18n]

// InitI18n initializes the global i18n instance.
func InitI18n(translationsDir string) error {
	inst := NewI18n()

	// Try to detect system locale
	systemLocale := inst.DetectSystemLocale()
	inst.SetLocale(systemLocale)

	// Load translations 必須在 atomic.Store 之前完成,確保其他 goroutine
	// 一拿到指標就能讀到完整的 messages map。
	if err := inst.LoadTranslations(translationsDir); err != nil {
		return err
	}

	globalI18n.Store(inst)

	return nil
}

// T translates a message key using the global i18n instance.
func T(key string, args ...interface{}) string {
	inst := globalI18n.Load()
	if inst == nil {
		return key
	}

	return inst.T(key, args...)
}

// SetLocale sets the current locale using the global i18n instance.
func SetLocale(locale Locale) {
	if inst := globalI18n.Load(); inst != nil {
		inst.SetLocale(locale)
	}
}

// GetLocale returns the current locale using the global i18n instance.
func GetLocale() Locale {
	inst := globalI18n.Load()
	if inst == nil {
		return LocaleZhTW
	}

	return inst.GetLocale()
}

// GetTranslationMap returns a snapshot of all translations for the given locale
// using the global i18n instance. Returns built-in translations if the global
// instance has not been initialised (testing / pre-init paths).
func GetTranslationMap(locale Locale) map[string]string {
	inst := globalI18n.Load()
	if inst == nil {
		return buildTranslationMap(locale)
	}

	return inst.GetTranslationMap(locale)
}

// GetSupportedLocales returns the list of supported locales using the global
// i18n instance。4 個 locale 是編譯期常數,不依賴 globalI18n 初始化狀態 —
// caller(如 gui.SetLanguage)不必先呼叫 InitI18n 即可拿到完整 list。
func GetSupportedLocales() []Locale {
	return []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}
}

// GetLocaleName returns the display name of a locale using the global i18n instance.
func GetLocaleName(locale Locale) string {
	inst := globalI18n.Load()
	if inst == nil {
		return string(locale)
	}

	return inst.GetLocaleName(locale)
}
