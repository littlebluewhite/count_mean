package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
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

// muscleRatioKeys 是 P3-E migration 新增的 i18n key（11 keys + 4 (3c) follow-up = 15 keys），供下方守門測試共用。
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
		args []interface{}
		want string
	}{
		{"%v with error (subject.parse_emg_failed)", KeyErrorMuscleRatioSubjectParseEMGFailed, []interface{}{innerErr}, "解析 EMG 檔案失敗: boom"},
		{"%v with error (subject.write_output1_failed)", KeyErrorMuscleRatioSubjectWriteOutput1Failed, []interface{}{innerErr}, "寫入 Output 1 失敗: boom"},
		{"%v with error (subject.write_output2_failed)", KeyErrorMuscleRatioSubjectWriteOutput2Failed, []interface{}{innerErr}, "寫入 Output 2 失敗（Output 1 已產出）: boom"},
		{"%v with error (handler.analysis_failed)", KeyErrorMuscleRatioHandlerAnalysisFailed, []interface{}{innerErr}, "分析失敗: boom"},
		{"%d with int (status.processed_count)", KeyStatusMuscleRatioProcessedCount, []interface{}{5}, "已處理 5 個主題"},
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

// TestI18n_AllMuscleRatioKeysCovered 驗證 P3-E 11 個新 key 在 4 locale 都有 entry，
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
// future 加任何含 verb 的 key 都會 auto cover；P3-E Phase 5 (catalog 一致性 tooling) 的核心守門。
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
