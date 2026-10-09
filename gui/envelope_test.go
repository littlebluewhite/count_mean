package gui

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
	"count_mean/internal/logging"
)

// envelopeTestErrors 回兩個 envelope 測試共用的錯誤:pathErr 是一般 error(帶植入目錄的
// 開檔失敗);analyzerErr 是 analyzer 回的 *i18n.Error(cci 解析 EMG 失敗的形狀),cause 為
// pathErr。兩者 redact 後的 zh-TW 文字為 redactedPathErr / "解析 EMG 檔案失敗: " + redactedPathErr。
func envelopeTestErrors() (pathErr, analyzerErr error) {
	pathErr = fmt.Errorf("讀取失敗: %w", &fs.PathError{
		Op:   "open",
		Path: "/Users/alice/" + plantedDirMarker + "/emg.csv",
		Err:  fs.ErrNotExist,
	})

	return pathErr, i18n.WrapError(pathErr, i18n.KeyErrorCCIParseEMGFailed)
}

const redactedPathErr = "讀取失敗: open <redacted-path>/emg.csv: file does not exist"

// TestFailMessage_LocalizedAndRedacted 釘住 failMessage 的 Message 形狀:
// i18n.T(key)(目前 locale)+ ": " + redact 後的 i18n.Localize(err)。
//
//   - zh-TW / en-US:前綴隨 locale 切換,err 文字的目錄段一律換成 `<redacted-path>/`
//   - analyzer 回的 *i18n.Error 也依目前 locale 渲染(ADR-0048);它的 cause 是一般 error,
//     保留原文
//   - catalog 沒有的 key:i18n.T 回 key 本身(bare-key fallback),err 文字照樣 redact
func TestFailMessage_LocalizedAndRedacted(t *testing.T) {
	prevLocale := i18n.GetLocale()
	t.Cleanup(func() { i18n.SetLocale(prevLocale) })

	app := &App{logger: logging.NewLogger(logging.LevelInfo, io.Discard, false)}
	pathErr, analyzerErr := envelopeTestErrors()

	cases := []struct {
		name   string
		locale i18n.Locale
		key    string
		err    error
		want   string
	}{
		{"zh-TW", i18n.LocaleZhTW, i18n.KeyErrorHandlerAnalysisFailed, pathErr, "分析失敗: " + redactedPathErr},
		{"en-US", i18n.LocaleEnUS, i18n.KeyErrorHandlerAnalysisFailed, pathErr, "Analysis failed: " + redactedPathErr},
		{"analyzer_error_zh-TW", i18n.LocaleZhTW, i18n.KeyErrorHandlerAnalysisFailed, analyzerErr,
			"分析失敗: 解析 EMG 檔案失敗: " + redactedPathErr},
		{"analyzer_error_en-US", i18n.LocaleEnUS, i18n.KeyErrorHandlerAnalysisFailed, analyzerErr,
			"Analysis failed: Failed to parse EMG file: " + redactedPathErr},
		{"missing_key_bare_fallback", i18n.LocaleZhTW, "error.handler.no_such_key", pathErr,
			"error.handler.no_such_key: " + redactedPathErr},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i18n.SetLocale(tc.locale)

			assert.Equal(t, tc.want, app.failMessage(tc.key, tc.err))
		})
	}
}

// TestInputMessageAndRedactText_LocalizedAndRedacted 釘住 inputMessage 與 redactText
// (MuscleRatioSubjectDTO.Error 等字串欄位):無前綴、redact 後的 i18n.Localize(err),
// nil 回空字串。
func TestInputMessageAndRedactText_LocalizedAndRedacted(t *testing.T) {
	prevLocale := i18n.GetLocale()
	t.Cleanup(func() { i18n.SetLocale(prevLocale) })

	_, analyzerErr := envelopeTestErrors()

	cases := []struct {
		name   string
		locale i18n.Locale
		err    error
		want   string
	}{
		{"zh-TW", i18n.LocaleZhTW, analyzerErr, "解析 EMG 檔案失敗: " + redactedPathErr},
		{"en-US", i18n.LocaleEnUS, analyzerErr, "Failed to parse EMG file: " + redactedPathErr},
		{"nil", i18n.LocaleEnUS, nil, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i18n.SetLocale(tc.locale)

			assert.Equal(t, tc.want, inputMessage(tc.err))
			assert.Equal(t, tc.want, redactText(tc.err))
		})
	}
}

// writeEnvelopeTestManifest 在 dir 寫一份單列 manifest(Subject 為 subject),回其路徑。
func writeEnvelopeTestManifest(t *testing.T, dir, subject string) string {
	t.Helper()

	path := filepath.Join(dir, "manifest.csv")
	require.NoError(t, os.WriteFile(path, []byte(
		"Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n"+
			subject+",motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8",
	), 0o600))

	return path
}

// TestEnvelope_LocalizesAnalyzerErrors 釘住 ADR-0048 端到端:[[Domain analyzer]] 回的
// *i18n.Error 經 handler 的 envelope 依目前 locale 呈現,zh-TW 與 key 化之前逐位元組相同。
// 每個 analyzer 一列(經真實 handler 觸發)。
func TestEnvelope_LocalizesAnalyzerErrors(t *testing.T) {
	prevLocale := i18n.GetLocale()
	t.Cleanup(func() { i18n.SetLocale(prevLocale) })

	cases := []struct {
		name string
		call func(t *testing.T) string
		zhTW string
		enUS string
	}{
		{
			name: "AnalyzeCCI_InvalidSubjectIndex",
			call: func(t *testing.T) string {
				dir := t.TempDir()
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.AnalyzeCCI(CCIParams{
					ManifestFile: writeEnvelopeTestManifest(t, dir, "S1"),
					DataFolder:   dir,
					SubjectIndex: 5,
				})
				require.NoError(t, err)
				require.False(t, result.Success)

				return result.Message
			},
			zhTW: "分析失敗: 無效的主題索引: 5 (共有 1 個主題)",
			enUS: "Analysis failed: Invalid subject index: 5 (1 subjects available)",
		},
		{
			name: "AnalyzeMuscleRatio_SubjectError",
			call: func(t *testing.T) string {
				dir := t.TempDir()
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.AnalyzeMuscleRatio(MuscleRatioParams{
					ManifestFile: writeEnvelopeTestManifest(t, dir, "   "),
					DataFolder:   dir,
				})
				require.NoError(t, err)
				require.Len(t, result.Subjects, 1)
				require.False(t, result.Subjects[0].Success)

				return result.Subjects[0].Error
			},
			zhTW: "Subject 名稱為空",
			enUS: "Subject name is empty",
		},
		{
			// DataFolder 不存在:phase_sync 的「資料夾不存在 (%s)」帶路徑 Arg,植入目錄段被 redact。
			name: "AnalyzePhaseSync_DataFolderMissing",
			call: func(t *testing.T) string {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.AnalyzePhaseSync(PhaseSyncParams{
					ManifestFile: writeEnvelopeTestManifest(t, t.TempDir(), "S1"),
					DataFolder:   filepath.Join(plantDir(t), "missing"),
					StartPhase:   "P0",
					EndPhase:     "P2",
				})
				require.NoError(t, err)
				require.False(t, result.Success)

				return result.Message
			},
			zhTW: "分析失敗: 資料夾不存在 (<redacted-path>/missing): base folder not found",
			enUS: "Analysis failed: Data folder does not exist (<redacted-path>/missing): base folder not found",
		},
		{
			// 同上,經 NPS 的 *phase_sync.AnalysisError(StageLoad → 載入資料失敗前綴)。
			name: "AnalyzeNormalizedPhaseSync_DataFolderMissing",
			call: func(t *testing.T) string {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.AnalyzeNormalizedPhaseSync(NormalizedPhaseSyncParams{
					ManifestFile:    writeEnvelopeTestManifest(t, t.TempDir(), "S1"),
					DataFolder:      filepath.Join(plantDir(t), "missing"),
					NormStartPhase:  "P0",
					NormEndPhase:    "P2",
					StatsStartPhase: "P0",
					StatsEndPhase:   "P2",
				})
				require.NoError(t, err)
				require.False(t, result.Success)

				return result.Message
			},
			zhTW: "載入資料失敗: 資料夾不存在 (<redacted-path>/missing): base folder not found",
			enUS: "Failed to load data: Data folder does not exist (<redacted-path>/missing): base folder not found",
		},
	}

	for _, tc := range cases {
		for _, locale := range []i18n.Locale{i18n.LocaleZhTW, i18n.LocaleEnUS} {
			t.Run(tc.name+"/"+string(locale), func(t *testing.T) {
				i18n.SetLocale(locale)

				want := tc.zhTW
				if locale == i18n.LocaleEnUS {
					want = tc.enUS
				}
				assert.Equal(t, want, tc.call(t))
			})
		}
	}
}

// TestHandlerLogs_ExpectedFailureShape 釘住 failMessage 收斂後 handler 可預期失敗的
// log 形狀(ADR-0036;entry / exit 規則見 ADR-0035 Decision 5):
//
//   - 原 Tier-1(AnalyzeCCI)下游失敗:entry Info + 恰一筆 Error + exit Info。Error 由
//     failMessage 記:訊息為 localized 前綴(文字,不是 bare key),context 指名失敗的
//     handler 與 call site(key 跨 handler 共用、log 的 file:line 固定指向 envelope.go,
//     併發 Wails 呼叫下 entry log 也無法對應),另帶 i18n key 供跨 locale grep
//   - 原 Tier-2(AnalyzeMuscleRatio)驗證失敗:entry Info、無 exit Info、無 Error
//     (inputMessage 不 log)
func TestHandlerLogs_ExpectedFailureShape(t *testing.T) {
	newBufApp := func(t *testing.T) (*App, *bytes.Buffer) {
		t.Helper()

		var buf bytes.Buffer
		app := newRPCRedactTestApp(t, t.TempDir(), "")
		app.logger = logging.NewLogger(logging.LevelInfo, &buf, false)

		return app, &buf
	}

	t.Run("AnalyzeCCI_AnalyzerFailure", func(t *testing.T) {
		app, buf := newBufApp(t)

		result, err := app.AnalyzeCCI(CCIParams{
			ManifestFile: filepath.Join(t.TempDir(), "missing_manifest.csv"),
			DataFolder:   t.TempDir(),
		})
		require.NoError(t, err)
		require.False(t, result.Success)

		logs := buf.String()
		assert.Equal(t, 1, countLogLines(logs, "[INFO]", "開始CCI 分析"), logs)
		assert.Equal(t, 1, countLogLines(logs, "[ERROR]", ""), logs)
		assert.Equal(t, 1, countLogLines(logs, "[INFO]", "CCI 分析完成"), logs)

		key := i18n.KeyErrorHandlerAnalysisFailed
		assert.Equal(t, 1, countLogLines(logs, "[ERROR] "+i18n.T(key)+" (", ""), "Error 訊息應為 localized 前綴\n"+logs)
		assert.Equal(t, 0, countLogLines(logs, "[ERROR] "+key, ""), "Error 訊息不可是 bare key\n"+logs)
		assert.Equal(t, 1, countLogLines(logs, "[ERROR]", "handler=gui.(*App).AnalyzeCCI"), "Error 應指名失敗的 handler\n"+logs)
		assert.Equal(t, 1, countLogLines(logs, "[ERROR]", "caller=cci_handlers.go:"), "Error 應帶 handler 的 call site\n"+logs)
		assert.Equal(t, 1, countLogLines(logs, "[ERROR]", "i18n="+key), "Error 應帶 i18n key\n"+logs)
	})

	t.Run("AnalyzeMuscleRatio_ValidateFailure", func(t *testing.T) {
		app, buf := newBufApp(t)

		result, err := app.AnalyzeMuscleRatio(MuscleRatioParams{DataFolder: t.TempDir()})
		require.NoError(t, err)
		require.False(t, result.Success)

		logs := buf.String()
		assert.Equal(t, 1, countLogLines(logs, "[INFO]", "開始肌肉比值分析"), logs)
		assert.Equal(t, 0, countLogLines(logs, "", "肌肉比值分析完成"), logs)
		assert.Equal(t, 0, countLogLines(logs, "[ERROR]", ""), logs)
	})
}

// countLogLines 數 text log 中同時含 level 與 msg 的非空行數(空字串代表該條件不限)。
func countLogLines(logs, level, msg string) int {
	n := 0
	for _, line := range strings.Split(logs, "\n") {
		if line != "" && strings.Contains(line, level) && strings.Contains(line, msg) {
			n++
		}
	}

	return n
}
