package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"count_mean/internal/security/fsperm"
)

func TestI18n_Basic(t *testing.T) {
	i18nInstance := NewI18n()

	// 測試默認翻譯
	if err := i18nInstance.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}

	// 測試繁體中文翻譯
	i18nInstance.SetLocale(LocaleZhTW)

	if msg := i18nInstance.T("app.title"); msg != "EMG 數據分析工具" {
		t.Errorf("繁體中文翻譯錯誤，期望 'EMG 數據分析工具'，實際 '%s'", msg)
	}

	// 測試簡體中文翻譯
	i18nInstance.SetLocale(LocaleZhCN)

	if msg := i18nInstance.T("app.title"); msg != "EMG 数据分析工具" {
		t.Errorf("簡體中文翻譯錯誤，期望 'EMG 数据分析工具'，實際 '%s'", msg)
	}

	// 測試英文翻譯
	i18nInstance.SetLocale(LocaleEnUS)

	if msg := i18nInstance.T("app.title"); msg != "EMG Data Analysis Tool" {
		t.Errorf("英文翻譯錯誤，期望 'EMG Data Analysis Tool'，實際 '%s'", msg)
	}

	// 測試日文翻譯
	i18nInstance.SetLocale(LocaleJaJP)

	if msg := i18nInstance.T("app.title"); msg != "EMGデータ解析ツール" {
		t.Errorf("日文翻譯錯誤，期望 'EMGデータ解析ツール'，實際 '%s'", msg)
	}
}

func TestI18n_Fallback(t *testing.T) {
	i18nInstance := NewI18n()

	// 載入內建翻譯
	if err := i18nInstance.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}

	// 測試不存在的鍵值
	i18nInstance.SetLocale(LocaleZhTW)

	if msg := i18nInstance.T("nonexistent.key"); msg != "nonexistent.key" {
		t.Errorf("fallback 翻譯錯誤，期望 'nonexistent.key'，實際 '%s'", msg)
	}
}

func TestI18n_WithArgs(t *testing.T) {
	i18nInstance := NewI18n()

	// 載入內建翻譯
	if err := i18nInstance.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}

	i18nInstance.SetLocale(LocaleZhTW)

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
	if err := os.MkdirAll(tmpDir, fsperm.DirPerm); err != nil {
		t.Fatalf("無法創建測試目錄: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	i18nInstance := NewI18n()

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
	newI18nInstance := NewI18n()
	if err := newI18nInstance.LoadTranslations(tmpDir); err != nil {
		t.Fatalf("重新載入翻譯失敗: %v", err)
	}

	// 驗證翻譯是否正確
	newI18nInstance.SetLocale(LocaleZhTW)

	if msg := newI18nInstance.T("app.title"); msg != "EMG 數據分析工具" {
		t.Errorf("重新載入的翻譯錯誤，期望 'EMG 數據分析工具'，實際 '%s'", msg)
	}
}

func TestLocaleDetection(t *testing.T) {
	i18nInstance := NewI18n()

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
	i18nInstance := NewI18n()

	locales := i18nInstance.GetSupportedLocales()
	expectedLocales := []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}

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
	i18nInstance := NewI18n()

	testCases := []struct {
		locale   Locale
		expected string
	}{
		{LocaleZhTW, "繁體中文"},
		{LocaleZhCN, "简体中文"},
		{LocaleEnUS, "English"},
		{LocaleJaJP, "日本語"},
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
	if err := InitI18n("./nonexistent"); err != nil {
		t.Fatalf("初始化全局國際化失敗: %v", err)
	}

	// 測試設置語言
	SetLocale(LocaleEnUS)

	if locale := GetLocale(); locale != LocaleEnUS {
		t.Errorf("設置語言失敗，期望 '%s'，實際 '%s'", LocaleEnUS, locale)
	}

	// 測試翻譯
	msg := T("app.title")
	if msg != "EMG Data Analysis Tool" {
		t.Errorf("全局翻譯錯誤，期望 'EMG Data Analysis Tool'，實際 '%s'", msg)
	}

	// 測試支持的語言
	locales := GetSupportedLocales()
	if len(locales) != 4 {
		t.Errorf("全局支持語言數量錯誤，期望 4，實際 %d", len(locales))
	}
}

// muscleRatioKeys 是 migration 新增的 i18n key（11 keys + 4 (3c) follow-up = 15 keys），供下方守門測試共用。
var muscleRatioKeys = []string{
	KeyErrorMuscleRatioOutputDirInvalid,
	KeyErrorMuscleRatioParseManifestFailed,
	KeyErrorMuscleRatioEmptyManifest,
	KeyErrorMuscleRatioMkdirFailed,
	KeyErrorMuscleRatioSubjectEmptyName,
	KeyErrorMuscleRatioSubjectParseEMGFailed,
	KeyErrorMuscleRatioSubjectWriteOutput1Failed,
	KeyErrorMuscleRatioSubjectWriteOutput2Failed,
	KeyErrorMuscleRatioHandlerAnalysisFailed,
	KeyStatusMuscleRatioProcessedCount,
	KeyStatusMuscleRatioPartialWarning,

	KeyErrorMuscleRatioSubjectEmptyEMG,
	KeyErrorMuscleRatioSubjectInsufficientPhases,
	KeyErrorMuscleRatioSubjectPhaseOutOfEMGRange,
	KeyErrorMuscleRatioSubjectCollision,
	KeyErrorMuscleRatioCancelled, // 批次取消
}

// TestT_MuscleRatioKeysVerbCompat 驗證 muscle_ratio catalog entries 中 %v / %d
// 經 i18n.T 注入動態值後輸出正確（i18n.T 內部走 fmt.Sprintf）。
func TestT_MuscleRatioKeysVerbCompat(t *testing.T) {
	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	inst.SetLocale(LocaleZhTW)

	innerErr := errors.New("boom")

	cases := []struct {
		name string
		key  string
		args []any
		want string
	}{
		// zh-TW colon style 統一為「:%v」(無空格,Asian convention)
		{"%v with error (subject.parse_emg_failed)", KeyErrorMuscleRatioSubjectParseEMGFailed, []any{innerErr}, "解析 EMG 檔案失敗:boom"},
		{"%v with error (subject.write_output1_failed)", KeyErrorMuscleRatioSubjectWriteOutput1Failed, []any{innerErr}, "寫入 Output 1 失敗:boom"},
		{"%v with error (subject.write_output2_failed)", KeyErrorMuscleRatioSubjectWriteOutput2Failed, []any{innerErr}, "寫入 Output 2 失敗（Output 1 已產出）:boom"},
		{"%v with error (handler.analysis_failed)", KeyErrorMuscleRatioHandlerAnalysisFailed, []any{innerErr}, "分析失敗:boom"},
		{"%d with int (status.processed_count)", KeyStatusMuscleRatioProcessedCount, []any{5}, "已處理 5 個主題"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := inst.T(tc.key, tc.args...)
			if got != tc.want {
				t.Errorf("T(%q, %v) = %q, want %q", tc.key, tc.args, got, tc.want)
			}
		})
	}
}

// TestI18n_NoCatalogPercentWVerb 守門：translationData 中任何 entry 含 %w verb 都 fail。
//
// i18n.T 內部用 fmt.Sprintf，而 %w 只有 fmt.Errorf 認得；放進 catalog 會輸出 "%!w(...)"
// 破壞 user-facing 訊息。Error wrap 必須由 caller 用
// `fmt.Errorf("%s: %w", i18n.T(key), innerErr)` pattern 處理。
//
// 實作策略：用既有 SaveTranslations 把 catalog 寫成 JSON 後 ReadFile + 掃字面，
// 避開為了 test 新增 catalog iteration API surface。
func TestI18n_NoCatalogPercentWVerb(t *testing.T) {
	tmpDir := t.TempDir()
	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	if err := inst.SaveTranslations(tmpDir); err != nil {
		t.Fatalf("保存翻譯失敗: %v", err)
	}

	for _, loc := range []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP} {
		path := filepath.Join(tmpDir, string(loc)+".json")
		data, err := os.ReadFile(path) //nolint:gosec // tmpDir is t.TempDir()
		if err != nil {
			t.Fatalf("讀取 %s 失敗: %v", path, err)
		}

		var trs map[string]string
		if err := json.Unmarshal(data, &trs); err != nil {
			t.Fatalf("解析 %s 失敗: %v", path, err)
		}

		for k, v := range trs {
			if strings.Contains(v, "%w") {
				t.Errorf("locale %s key %q 含 %%w verb: %q\nfmt.Sprintf 不認 %%w；error wrap 須由 caller 用 fmt.Errorf(\"%%s: %%w\", i18n.T(key), err) pattern", loc, k, v)
			}
		}
	}
}

// TestI18n_AllMuscleRatioKeysCovered 驗證 11 個新 key 在 4 locale 都有 entry，
// 沒有 fallback 到 key 本身（formatKeyWithArgs 路徑）。
func TestI18n_AllMuscleRatioKeysCovered(t *testing.T) {
	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}

	locales := []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}
	for _, loc := range locales {
		inst.SetLocale(loc)

		for _, k := range muscleRatioKeys {
			msg := inst.T(k)
			if msg == "" {
				t.Errorf("locale %s key %q 翻譯為空字串", loc, k)
			}
			if msg == k {
				t.Errorf("locale %s key %q fallback 到 key 本身（catalog 缺 entry）", loc, k)
			}
		}
	}
}

// TestI18n_CallerWrapPatternPreservesErrorsIs 預先驗 Phase 3 核心 wrap pattern：
// `fmt.Errorf("%s: %w", i18n.T(key), inner)` 仍能讓 errors.Is 穿透 wrap chain。
// Phase 3 analyzer.go 的 3 處 %w 改造（OutputDir / ParseManifest / Mkdir）都依賴此 contract。
func TestI18n_CallerWrapPatternPreservesErrorsIs(t *testing.T) {
	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	inst.SetLocale(LocaleZhTW)

	inner := errors.New("inner sentinel")
	wrapped := fmt.Errorf("%s: %w", inst.T(KeyErrorMuscleRatioOutputDirInvalid), inner)

	if !errors.Is(wrapped, inner) {
		t.Errorf("errors.Is 應穿透 wrap chain；got false")
	}

	msg := wrapped.Error()
	if !strings.Contains(msg, "OutputDir 驗證失敗") {
		t.Errorf("wrapped error 應含 i18n 翻譯訊息；got %q", msg)
	}
	if !strings.Contains(msg, "inner sentinel") {
		t.Errorf("wrapped error 應含 inner error；got %q", msg)
	}
}

// TestI18n_VerbConsistencyAcrossLocales 守門：translationData 中每個 key 在 4 個 locale
// 的 verb sequence（%v %d %s %.4f %q 等）必須一致。
//
// 範例 fail：zh-TW "已處理 %d 個主題" 對應 en-US "Processed subjects" 漏掉 %d，
// 跑 i18n.T(key, 5) 後英文 user 看到的 message 沒被 inject 數字 → silent bug。
//
// 用 zh-TW 為基準（source language），對其他 3 locale 比對 verb sequence。
// future 加任何含 verb 的 key 都會 auto cover；Phase 5 (catalog 一致性 tooling) 的核心守門。
func TestI18n_VerbConsistencyAcrossLocales(t *testing.T) {
	tmpDir := t.TempDir()
	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	if err := inst.SaveTranslations(tmpDir); err != nil {
		t.Fatalf("保存翻譯失敗: %v", err)
	}

	locales := []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}

	loadCatalog := func(loc Locale) map[string]string {
		path := filepath.Join(tmpDir, string(loc)+".json")
		data, err := os.ReadFile(path) //nolint:gosec // tmpDir is t.TempDir()
		if err != nil {
			t.Fatalf("讀取 %s 失敗: %v", path, err)
		}

		var trs map[string]string
		if err := json.Unmarshal(data, &trs); err != nil {
			t.Fatalf("解析 %s 失敗: %v", path, err)
		}

		return trs
	}

	catalogs := make(map[Locale]map[string]string, len(locales))
	for _, loc := range locales {
		catalogs[loc] = loadCatalog(loc)
	}

	// Go fmt verb 結構：%[flags][width].[precision][verb_letter]
	// 簡化版只 capture 含 letter 的合法 verb，不細究 width/precision 數值是否語意一致。
	verbRe := regexp.MustCompile(`%[+#\- 0]*\d*\.?\d*[a-zA-Z]`)

	base := catalogs[LocaleZhTW]
	for key, baseMsg := range base {
		baseVerbs := verbRe.FindAllString(baseMsg, -1)

		for _, loc := range locales {
			if loc == LocaleZhTW {
				continue
			}

			msg, ok := catalogs[loc][key]
			if !ok {
				t.Errorf("locale %s 缺 key %q", loc, key)
				continue
			}

			verbs := verbRe.FindAllString(msg, -1)
			if !reflect.DeepEqual(verbs, baseVerbs) {
				t.Errorf("locale %s key %q verb sequence %v 與 zh-TW %v 不一致", loc, key, verbs, baseVerbs)
			}
		}
	}
}

// TestI18n_GetTranslationMap exercises the new public API used by the Wails GUI
// binding (Phase 1 frontend i18n MVP). Covers per-locale lookup, fallback for
// unknown locales, snapshot isolation, and the ConfigPanel keys' coverage.
func TestI18n_GetTranslationMap(t *testing.T) {
	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}

	// (1) Each locale returns its own translation for app.title.
	cases := []struct {
		locale Locale
		want   string
	}{
		{LocaleZhTW, "EMG 數據分析工具"},
		{LocaleZhCN, "EMG 数据分析工具"},
		{LocaleEnUS, "EMG Data Analysis Tool"},
		{LocaleJaJP, "EMGデータ解析ツール"},
	}
	for _, tc := range cases {
		m := inst.GetTranslationMap(tc.locale)
		if got := m[KeyAppTitle]; got != tc.want {
			t.Errorf("locale %s: KeyAppTitle = %q, want %q", tc.locale, got, tc.want)
		}
	}

	// (2) Unknown locale falls back to the configured fallback (zh-TW).
	unknownMap := inst.GetTranslationMap(Locale("xx-YY"))
	if got := unknownMap[KeyAppTitle]; got != "EMG 數據分析工具" {
		t.Errorf("unknown locale fallback: KeyAppTitle = %q, want zh-TW value", got)
	}

	// (3) Returned map is a copy — mutating it must not affect later reads.
	m := inst.GetTranslationMap(LocaleEnUS)
	m[KeyAppTitle] = "MUTATED"
	again := inst.GetTranslationMap(LocaleEnUS)
	if again[KeyAppTitle] != "EMG Data Analysis Tool" {
		t.Errorf("snapshot leak: mutation affected internal state, got %q", again[KeyAppTitle])
	}

	// (4) Global wrapper returns same locale.
	if got := GetTranslationMap(LocaleEnUS)[KeyAppTitle]; got != "EMG Data Analysis Tool" {
		t.Errorf("global GetTranslationMap returned %q, want EMG Data Analysis Tool", got)
	}

	// (5) ConfigPanel Phase 1 keys: every new key must exist in all 4 locales
	// with a non-empty value (guards against silent catalog gaps).
	configKeys := []string{
		KeyConfigPanelTitle, KeyConfigButtonBack, KeyConfigButtonSave,
		KeyConfigButtonReset, KeyConfigButtonImport, KeyConfigButtonBrowse,
		KeyConfigSectionDataProcessing, KeyConfigSectionDirectories,
		KeyConfigSectionPhaseLabels, KeyConfigSectionAdvanced,
		KeyConfigLabelScalingFactor, KeyConfigHelpScalingFactor,
		KeyConfigLabelPrecision, KeyConfigHelpPrecision,
		KeyConfigLabelOutputFormat, KeyConfigHelpOutputFormat,
		KeyConfigOptionOutputCSV, KeyConfigOptionOutputJSON, KeyConfigOptionOutputXLSX,
		KeyConfigLabelBOMEnabled, KeyConfigHelpBOMEnabled,
		KeyConfigLabelInputDir, KeyConfigHelpInputDir,
		KeyConfigLabelOutputDir, KeyConfigHelpOutputDir,
		KeyConfigLabelOperateDir, KeyConfigHelpOperateDir,
		KeyConfigLabelPhaseLabels, KeyConfigHelpPhaseLabels,
		KeyConfigLabelLogLevel, KeyConfigHelpLogLevel,
		KeyConfigOptionLogLevelDebug, KeyConfigOptionLogLevelInfo,
		KeyConfigOptionLogLevelWarn, KeyConfigOptionLogLevelError,
		KeyConfigLabelUILanguage, KeyConfigHelpUILanguage,
	}
	locales := []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}
	for _, loc := range locales {
		dict := inst.GetTranslationMap(loc)
		for _, key := range configKeys {
			v, ok := dict[key]
			if !ok {
				t.Errorf("locale %s missing ConfigPanel key %q", loc, key)
				continue
			}
			if strings.TrimSpace(v) == "" {
				t.Errorf("locale %s key %q is empty string", loc, key)
			}
		}
	}
}

// TestI18n_Phase2NormalizedOutputLabels guards the Output 1 / Output 2 result
// labels that the normalized phase-sync result panel renders side by side.
// Regression for codex review P3: line 2028 previously emitted a hard-coded
// Chinese 'Output 2(分期統計):' even when the surrounding labels were i18n'd,
// producing a mixed-language result panel in en/ja/zh-CN. Catching this kind
// of leak requires verifying that BOTH labels resolve in all four locales and
// — crucially — that non-zh locales contain no Han characters (which would
// indicate a partial translation slipped through).
func TestI18n_Phase2NormalizedOutputLabels(t *testing.T) {
	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}

	pairKeys := []string{
		KeyResultLabelOutputNorm,
		KeyResultLabelOutputStats,
	}
	// Check only en-US for Han leakage (ja-JP legitimately contains kanji).
	hanRe := regexp.MustCompile(`\p{Han}`)

	for _, loc := range []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP} {
		dict := inst.GetTranslationMap(loc)
		for _, key := range pairKeys {
			v, ok := dict[key]
			if !ok || strings.TrimSpace(v) == "" {
				t.Errorf("locale %s key %q missing or empty", loc, key)
				continue
			}
			if loc == LocaleEnUS && hanRe.MatchString(v) {
				t.Errorf("locale en-US key %q contains Han characters %q — likely partial catalog leak", key, v)
			}
		}
	}
}

// TestI18n_MainJSDoesNotHardCodeOutputStatsLabel is a direct regression test
// for codex review P3 — main.js previously emitted 'Output 2（分期統計）：' as
// a raw string literal next to a t()-wrapped Output 1 label, producing a
// mixed-language result panel under non-zh locales. Guards against the literal
// re-appearing if someone later refactors the normalized phase-sync result rendering.
func TestI18n_MainJSDoesNotHardCodeOutputStatsLabel(t *testing.T) {
	mainJSPath := "../../../frontend/src/main.js"
	data, err := os.ReadFile(mainJSPath) //nolint:gosec // fixed path within test tree
	if err != nil {
		t.Skipf("frontend/src/main.js not readable (%v); test is GUI-codebase-specific", err)
	}
	body := string(data)
	// Variants the bug has appeared in (fullwidth and ASCII punctuation forms).
	badLiterals := []string{
		"'Output 2（分期統計）：'",
		"\"Output 2（分期統計）：\"",
		"'Output 2(分期統計):'",
	}
	for _, lit := range badLiterals {
		if strings.Contains(body, lit) {
			t.Errorf("frontend/src/main.js still contains hard-coded label %q — should call t('result.label.output_stats') instead", lit)
		}
	}
}

// TestI18n_LoadTranslations_OverlayMergesBuiltin 守護 當外部
// translationsDir 提供的 JSON 是舊版（缺新版加入的 key），LoadTranslations
// 必須以「外部 JSON overlay 在 builtin 上」的方式合併，而非直接 replace。
// 否則 i18n.T(key) 取不到 builtin fallback，會回 raw key（catalog 內部識別
// 字會字面 render 到使用者面前），這正是 muscle_ratio handler L74 走到
// i18n.T(KeyErrorMuscleRatioHandlerAnalysisFailed) 時遇到的破口。
func TestI18n_LoadTranslations_OverlayMergesBuiltin(t *testing.T) {
	tempDir := t.TempDir()

	// 舊版 zh-TW 外部 JSON：只含 app.title，缺 muscle_ratio.handler.* 等新 key。
	partialJSON := `{"app.title": "舊版 EMG"}`
	path := filepath.Join(tempDir, "zh-TW.json")
	if err := os.WriteFile(path, []byte(partialJSON), fsperm.FilePerm); err != nil {
		t.Fatalf("write partial json: %v", err)
	}

	inst := NewI18n()
	if err := inst.LoadTranslations(tempDir); err != nil {
		t.Fatalf("LoadTranslations: %v", err)
	}
	inst.SetLocale(LocaleZhTW)

	// (a) 外部 JSON 覆蓋 builtin（舊版翻譯生效）
	if got := inst.T("app.title"); got != "舊版 EMG" {
		t.Errorf("external JSON should override built-in for app.title: got %q, want %q", got, "舊版 EMG")
	}

	// (b) 外部 JSON 缺的 key，T() 必須從 builtin 取得翻譯，不可回 raw key。
	//     用 muscle_ratio.handler.analysis_failed 因為它是 CDX-1 issue 的真實受害者。
	got := inst.T(KeyErrorMuscleRatioHandlerAnalysisFailed, errors.New("boom"))
	if got == KeyErrorMuscleRatioHandlerAnalysisFailed {
		t.Errorf("T() returned raw key — builtin overlay failed: got %q", got)
	}
	if strings.HasPrefix(got, "error.muscle_ratio") {
		t.Errorf("T() returned key-shaped string — builtin not merged: got %q", got)
	}
	// builtin zh-TW 翻譯應該包含「分析失敗」
	if !strings.Contains(got, "分析失敗") {
		t.Errorf("T() result %q should contain 「分析失敗」 from builtin zh-TW", got)
	}
}

// TestI18n_GetTranslationMap_PartialJSONFallsBackToBuiltIn ensures that when
// translationsDir contains a partial locale JSON (e.g. produced by an earlier
// release that lacked the new panel.* / config.* keys), the snapshot returned
// to the frontend still has all built-in keys populated. Regression for codex
// review P2 — without this fix the missing keys would render literally in the
// Wails WebView ("panel.maxmean.title" instead of the translated label).
func TestI18n_GetTranslationMap_PartialJSONFallsBackToBuiltIn(t *testing.T) {
	tempDir := t.TempDir()

	// 只放 en-US 且僅含 app.title — 缺所有新版 panel.* / config.* 等 key
	partialJSON := `{"app.title": "Custom EMG Tool"}`
	path := filepath.Join(tempDir, "en-US.json")
	if err := os.WriteFile(path, []byte(partialJSON), fsperm.FilePerm); err != nil {
		t.Fatalf("write partial json: %v", err)
	}

	inst := NewI18n()
	if err := inst.LoadTranslations(tempDir); err != nil {
		t.Fatalf("LoadTranslations: %v", err)
	}

	m := inst.GetTranslationMap(LocaleEnUS)

	// (a) loaded value 覆蓋 built-in
	if got := m["app.title"]; got != "Custom EMG Tool" {
		t.Errorf("loaded translation should override built-in: app.title = %q, want %q",
			got, "Custom EMG Tool")
	}

	// (b) missing key fall back to built-in — 缺的 key 不該字面 render
	v, ok := m[KeyPanelMaxmeanTitle]
	if !ok {
		t.Fatalf("missing key should fall back to built-in dictionary: %s absent from snapshot", KeyPanelMaxmeanTitle)
	}
	if strings.TrimSpace(v) == "" {
		t.Errorf("fallback value for %s is empty — expected built-in translation", KeyPanelMaxmeanTitle)
	}
	if v == KeyPanelMaxmeanTitle {
		t.Errorf("fallback returned the key itself (%q) — built-in dictionary not used", v)
	}
}

// TestI18n_GlobalI18n_ConcurrentReadWrite_NoRace 守門 globalI18n pointer
// 必須 atomic — 否則 -race build 在 InitI18n 與 T / SetLocale / GetLocale
// 並發呼叫時會偵測到 data race (裸 *I18n write/read 無 happens-before 關係)。
//
// 測試策略:
//   - 1 個 writer goroutine 反覆呼叫 InitI18n(觸發 globalI18n.Store)
//   - N 個 reader goroutine 並發呼叫 T / SetLocale / GetLocale / GetTranslationMap
//     (觸發 globalI18n.Load)
//
// 用 -race 跑必發現 race;沒有 -race 也至少驗 race-free smoke test 不會 panic / 失敗。
// 不檢查 T() 的返回值內容(過渡期可能拿到 nil → return key,或 init 完拿到 catalog),
// 只在乎 race detector 不噴錯,以及 panic-free。
func TestI18n_GlobalI18n_ConcurrentReadWrite_NoRace(t *testing.T) {
	// 確保 t.Cleanup 不會把 globalI18n 留在 corrupted 狀態影響後續 test。
	prev := globalI18n.Load()
	t.Cleanup(func() { globalI18n.Store(prev) })

	const (
		writers        = 2
		readers        = 8
		iterationsEach = 200
	)

	var wg sync.WaitGroup

	// Writers: 反覆 InitI18n (re-initialise globalI18n)。
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterationsEach; j++ {
				if err := InitI18n("./nonexistent"); err != nil {
					t.Errorf("InitI18n: %v", err)
					return
				}
			}
		}()
	}

	// Readers: 並發呼叫 T / SetLocale / GetLocale / GetTranslationMap。
	locales := []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < iterationsEach; j++ {
				loc := locales[(idx+j)%len(locales)]
				SetLocale(loc)
				_ = GetLocale()
				_ = T(KeyAppTitle)
				_ = T(KeyErrorMuscleRatioHandlerAnalysisFailed, errors.New("boom"))
				_ = GetTranslationMap(loc)
				_ = GetLocaleName(loc)
			}
		}(i)
	}

	wg.Wait()
}

// TestI18n_ColonStyle_Consistent 守門 zh-TW / zh-CN catalog 內所有
// 「<chinese>失敗/失败 + ASCII colon + content」一律無空格;en-US / ja-JP 維持
// 「<word> + ASCII colon + space + content」西文/日文標點慣例。drift case
// (例如「分析失敗: %v」與「計算失敗:%v」並存)會 fail。
func TestI18n_ColonStyle_Consistent(t *testing.T) {
	tmpDir := t.TempDir()
	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	if err := inst.SaveTranslations(tmpDir); err != nil {
		t.Fatalf("保存翻譯失敗: %v", err)
	}

	loadCatalog := func(loc Locale) map[string]string {
		path := filepath.Join(tmpDir, string(loc)+".json")
		data, err := os.ReadFile(path) //nolint:gosec // tmpDir is t.TempDir()
		if err != nil {
			t.Fatalf("讀取 %s 失敗: %v", path, err)
		}
		var trs map[string]string
		if err := json.Unmarshal(data, &trs); err != nil {
			t.Fatalf("解析 %s 失敗: %v", path, err)
		}
		return trs
	}

	// zh:「失敗 / 失败 + : + 任意」必須無空格。
	chineseFailRe := regexp.MustCompile(`(失敗|失败)([:：])( )`)

	// en/ja:既有 catalog 使用「<word> + : + space」西文慣例,反向守門「失敗:沒空格」
	// 出現在 en/ja(catalog mixing 漏 review)。
	enFailRe := regexp.MustCompile(`[Ff]ailed:[^\s]`)
	jaFailRe := regexp.MustCompile(`失敗しました:[^\s]`)

	for _, loc := range []Locale{LocaleZhTW, LocaleZhCN} {
		catalog := loadCatalog(loc)
		for k, v := range catalog {
			if chineseFailRe.MatchString(v) {
				t.Errorf("locale %s key %q value %q: 失敗/失败 後緊接 colon 不應有空格 (Asian convention)", loc, k, v)
			}
		}
	}

	// en-US:守門「failed:%v 沒空格」
	catalogEn := loadCatalog(LocaleEnUS)
	for k, v := range catalogEn {
		if enFailRe.MatchString(v) {
			t.Errorf("locale en-US key %q value %q: 'failed:' 後應有空格 (western convention)", k, v)
		}
	}

	// ja-JP:守門「失敗しました:%v 沒空格」
	catalogJa := loadCatalog(LocaleJaJP)
	for k, v := range catalogJa {
		if jaFailRe.MatchString(v) {
			t.Errorf("locale ja-JP key %q value %q: '失敗しました:' 後應有空格 (與 en-US 對齊)", k, v)
		}
	}
}

// TestI18n_VerbConsistencyAcrossLocales_UnionOfKeys 強化 既有
// TestI18n_VerbConsistencyAcrossLocales 只迭代 zh-TW key 集合作 base,若某 key
// 只存在於 zh-CN / en-US / ja-JP(zh-TW 漏定義)永遠不會被檢查。
//
// 改用「四 locale catalog union of keys」迭代,並對 4 個 locale 做 pairwise 比較;
// 若某 locale 缺 key 視為 error,若 verb sequence 不一致則 error。比 base-only
// version 嚴格,順便守門 catalog completeness。
func TestI18n_VerbConsistencyAcrossLocales_UnionOfKeys(t *testing.T) {
	tmpDir := t.TempDir()
	inst := NewI18n()
	if err := inst.LoadTranslations("./nonexistent"); err != nil {
		t.Fatalf("載入內建翻譯失敗: %v", err)
	}
	if err := inst.SaveTranslations(tmpDir); err != nil {
		t.Fatalf("保存翻譯失敗: %v", err)
	}

	locales := []Locale{LocaleZhTW, LocaleZhCN, LocaleEnUS, LocaleJaJP}
	catalogs := make(map[Locale]map[string]string, len(locales))
	for _, loc := range locales {
		path := filepath.Join(tmpDir, string(loc)+".json")
		data, err := os.ReadFile(path) //nolint:gosec // tmpDir is t.TempDir()
		if err != nil {
			t.Fatalf("讀取 %s 失敗: %v", path, err)
		}
		var trs map[string]string
		if err := json.Unmarshal(data, &trs); err != nil {
			t.Fatalf("解析 %s 失敗: %v", path, err)
		}
		catalogs[loc] = trs
	}

	// 建 union of keys (sorted for stable failure messages)
	unionSet := make(map[string]struct{})
	for _, c := range catalogs {
		for k := range c {
			unionSet[k] = struct{}{}
		}
	}
	union := make([]string, 0, len(unionSet))
	for k := range unionSet {
		union = append(union, k)
	}
	sort.Strings(union)

	verbRe := regexp.MustCompile(`%[+#\- 0]*\d*\.?\d*[a-zA-Z]`)

	// 對 union 每 key,以「第一個 having-this-key 的 locale」當 reference,
	// 然後跟其他 locale 對比 verb sequence。
	for _, key := range union {
		// 找 reference locale (first locale that has the key, in locales 順序)
		var refLoc Locale
		var refMsg string
		var refVerbs []string
		for _, loc := range locales {
			if msg, ok := catalogs[loc][key]; ok {
				refLoc = loc
				refMsg = msg
				refVerbs = verbRe.FindAllString(msg, -1)
				break
			}
		}
		_ = refMsg // referenced in error message

		// 跟其他 3 locale 比
		for _, loc := range locales {
			if loc == refLoc {
				continue
			}
			msg, ok := catalogs[loc][key]
			if !ok {
				t.Errorf("union key %q 在 locale %s 缺漏 (reference locale %s 有)", key, loc, refLoc)
				continue
			}
			verbs := verbRe.FindAllString(msg, -1)
			if !reflect.DeepEqual(verbs, refVerbs) {
				t.Errorf("key %q: locale %s verb sequence %v 與 reference locale %s %v 不一致",
					key, loc, verbs, refLoc, refVerbs)
			}
		}
	}
}

// TestParseLocale_ChineseRegionalVariants 守門 涵蓋常見中文區域變體
// (zh-HK / zh-MO / zh-SG / 裸 zh + 各種大小寫底線變形)正確 fallback。
func TestParseLocale_ChineseRegionalVariants(t *testing.T) {
	cases := []struct {
		input string
		want  Locale
	}{
		// 既有 case (regression baseline)
		{"zh_TW", LocaleZhTW},
		{"zh-TW", LocaleZhTW},
		{"zh-TW.UTF-8", LocaleZhTW},
		{"zh_CN", LocaleZhCN},
		{"zh-CN.UTF-8", LocaleZhCN},
		{"en_US.UTF-8", LocaleEnUS},
		{"ja_JP.UTF-8", LocaleJaJP},

		// 新增:HK / MO → zh-TW (繁體),SG → zh-CN (簡體)
		{"zh_HK", LocaleZhTW},
		{"zh-HK", LocaleZhTW},
		{"zh-HK.Big5", LocaleZhTW},
		{"zh_MO", LocaleZhTW},
		{"zh-MO", LocaleZhTW},
		{"zh_SG", LocaleZhCN},
		{"zh-SG.UTF-8", LocaleZhCN},

		// 裸 zh + 其他變體 → fallback zh-TW
		{"zh", LocaleZhTW},
		{"zh.UTF-8", LocaleZhTW},
		{"zh-Hant", LocaleZhTW},
		{"zh-Hans", LocaleZhTW},
		{"ZH-Hant-TW", LocaleZhTW}, // 大小寫應被正規化

		// non-supported language 維持空字串(caller 自行 fallback)
		{"fr_FR", ""},
		{"de_DE.UTF-8", ""},
		{"", ""},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := parseLocale(tc.input)
			if got != tc.want {
				t.Errorf("parseLocale(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestLoadTranslations_RejectsOversizedFile 釘住 修法:user-supplied
// translation JSON 超過 maxTranslationFileBytes (4 MiB) 必須在讀檔階段
// reject,不可走完 ReadFile + Unmarshal 的 memory cost。
func TestLoadTranslations_RejectsOversizedFile(t *testing.T) {
	tmpDir := t.TempDir()

	// 構造 4 MiB + 1 byte 的 JSON。內容必須是合法 JSON(物件) 才不會被前面的
	// JSON parse check 先攔下 — 但 size cap 應該在 ReadFile 之前就 fail。
	var buf strings.Builder
	buf.WriteString(`{"k":"`)
	// padding 到剛好超過 4 MiB
	for buf.Len() < 4*1024*1024+1 {
		buf.WriteString("x")
	}
	buf.WriteString(`"}`)
	path := filepath.Join(tmpDir, "zh-TW.json")
	if err := os.WriteFile(path, []byte(buf.String()), fsperm.FilePerm); err != nil {
		t.Fatalf("write oversized json: %v", err)
	}

	inst := NewI18n()
	err := inst.LoadTranslations(tmpDir)
	if err == nil {
		t.Fatalf("LoadTranslations 應 reject 超過 4 MiB 的 file,got nil")
	}
	if !errors.Is(err, ErrTranslationFileTooLarge) {
		t.Errorf("expected ErrTranslationFileTooLarge, got %v", err)
	}
}

// TestLoadTranslations_RejectsDeepNesting 釘住 JSON depth cap。
// User-supplied JSON 不該嵌很深(translation map 是平的 string→string,
// 任何 nesting 都異常),深度 > maxTranslationJSONDepth 立即 reject。
func TestLoadTranslations_RejectsDeepNesting(t *testing.T) {
	tmpDir := t.TempDir()

	// 構造嵌套 50 層的 JSON (遠超 cap 8)。
	depth := 50
	js := strings.Repeat(`{"k":`, depth) + `"v"` + strings.Repeat(`}`, depth)
	path := filepath.Join(tmpDir, "zh-TW.json")
	if err := os.WriteFile(path, []byte(js), fsperm.FilePerm); err != nil {
		t.Fatalf("write deep json: %v", err)
	}

	inst := NewI18n()
	err := inst.LoadTranslations(tmpDir)
	if err == nil {
		t.Fatalf("LoadTranslations 應 reject 深度過大的 JSON,got nil")
	}
	if !errors.Is(err, ErrTranslationJSONTooDeep) {
		t.Errorf("expected ErrTranslationJSONTooDeep, got %v", err)
	}
}

// TestLoadTranslations_RejectsExtraKeys 釘住 user JSON 不可含 builtin 沒有的 key。
// 攻擊面:額外 key 可被未來新增的 T(key) caller 引用,形成「翻譯文件影響程式行為」
// 的 trust leak;也可能是 typo 造成 silent loss。
func TestLoadTranslations_RejectsExtraKeys(t *testing.T) {
	tmpDir := t.TempDir()

	// 構造 zh-TW.json:包含 builtin 沒有的 fake key。
	fakeKey := "this.key.does.not.exist.in.builtin"
	js := fmt.Sprintf(`{%q: "hi"}`, fakeKey)
	path := filepath.Join(tmpDir, "zh-TW.json")
	if err := os.WriteFile(path, []byte(js), fsperm.FilePerm); err != nil {
		t.Fatalf("write json with extra key: %v", err)
	}

	inst := NewI18n()
	err := inst.LoadTranslations(tmpDir)
	if err == nil {
		t.Fatalf("LoadTranslations 應 reject 多餘的 key,got nil")
	}
	if !errors.Is(err, ErrTranslationKeyUnknown) {
		t.Errorf("expected ErrTranslationKeyUnknown, got %v", err)
	}
}

// TestLoadTranslations_RejectsPercentWVerb 釘住 user JSON 不可含 %w verb。
// %w 是 fmt 的 wrap-error 動詞,在 i18n.T 透過 fmt.Sprintf 走 user-supplied
// format string 時注入 %w 可造成 panic 或行為改變(fmt.Sprintf 不接受 %w,
// 會 render 為 `%!w(...)`,讓 production string 出現 internal token)。
func TestLoadTranslations_RejectsPercentWVerb(t *testing.T) {
	tmpDir := t.TempDir()

	// 用 app.title 這個 builtin key (確保 key check 通過),value 含 %w。
	js := `{"app.title": "EMG Tool %w"}`
	path := filepath.Join(tmpDir, "zh-TW.json")
	if err := os.WriteFile(path, []byte(js), fsperm.FilePerm); err != nil {
		t.Fatalf("write json with %%w: %v", err)
	}

	inst := NewI18n()
	err := inst.LoadTranslations(tmpDir)
	if err == nil {
		t.Fatalf("LoadTranslations 應 reject %%w verb,got nil")
	}
	if !errors.Is(err, ErrTranslationFormatVerbUnsupported) {
		t.Errorf("expected ErrTranslationFormatVerbUnsupported, got %v", err)
	}
}

// TestLoadTranslations_AcceptsEscapedPercentW 釘住:字面 %%w(escaped percent + 字面 w)
// 不該被當成 %w wrap-error verb reject。與 countTranslationVerbs 的 %% 剝離對稱 —
// %w 守門也須先剝 %% 再比對,否則合法的 "%%w" 文字會被誤拒。
func TestLoadTranslations_AcceptsEscapedPercentW(t *testing.T) {
	tmpDir := t.TempDir()

	js := `{"app.title": "EMG Tool %%w"}`
	path := filepath.Join(tmpDir, "zh-TW.json")
	if err := os.WriteFile(path, []byte(js), fsperm.FilePerm); err != nil {
		t.Fatalf("write json with escaped percent-w: %v", err)
	}

	inst := NewI18n()
	err := inst.LoadTranslations(tmpDir)
	if errors.Is(err, ErrTranslationFormatVerbUnsupported) {
		t.Errorf("字面 %%%%w 不該被當成 %%w verb reject,got %v", err)
	}
}

// TestLoadTranslations_RejectsVerbCountMismatch 釘住 user JSON 中
// 每個 key 的 fmt verb count 必須與 master locale(zh-TW builtin)一致。
// 否則 caller args (來自 source code) 與 format string (來自 user file) drift,
// 多出的 args 會被 Sprintf 顯示為 %!(EXTRA ...) 或缺少 args 變 %!(MISSING)。
func TestLoadTranslations_RejectsVerbCountMismatch(t *testing.T) {
	tmpDir := t.TempDir()

	// 找一個 builtin 有 %d 的 key — status.large_file_processing("處理大文件中... %.1f%%")
	// 含 1 個 verb。改成沒有 verb 的版本必須被 reject。
	js := `{"status.large_file_processing": "已完成"}`
	path := filepath.Join(tmpDir, "zh-TW.json")
	if err := os.WriteFile(path, []byte(js), fsperm.FilePerm); err != nil {
		t.Fatalf("write json with mismatched verbs: %v", err)
	}

	inst := NewI18n()
	err := inst.LoadTranslations(tmpDir)
	if err == nil {
		t.Fatalf("LoadTranslations 應 reject verb count mismatch,got nil")
	}
	if !errors.Is(err, ErrTranslationVerbCountMismatch) {
		t.Errorf("expected ErrTranslationVerbCountMismatch, got %v", err)
	}
}

// TestCountTranslationVerbs_PercentPercent 釘住 %% 計數修正:
// countTranslationVerbs 必須先剝離 %%,否則 regex 會把 %% 後的字元誤判為 verb。
func TestCountTranslationVerbs_PercentPercent(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"%%", 0},            // %% 是 escaped percent,非 verb
		{"%%d", 0},           // %% + 字面 d,非 verb
		{"%d%%", 1},          // 1 個真實 verb + escaped percent
		{"%.1f%% complete", 1}, // 1 個真實 verb + escaped percent + 字面字元
		{"%d %s", 2},         // sanity: 2 個真實 verb
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := countTranslationVerbs(tc.input)
			if got != tc.want {
				t.Errorf("countTranslationVerbs(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}
