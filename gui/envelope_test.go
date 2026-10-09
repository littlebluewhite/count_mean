package gui

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
	"count_mean/internal/logging"
)

// TestFailMessage_LocalizedAndRedacted 釘住 failMessage 的 Message 形狀:
// i18n.T(key)(目前 locale)+ ": " + redact 後的 err 文字。
//
//   - zh-TW / en-US:前綴隨 locale 切換,err 文字的目錄段一律換成 `<redacted-path>/`
//   - catalog 沒有的 key:i18n.T 回 key 本身(bare-key fallback),err 文字照樣 redact
func TestFailMessage_LocalizedAndRedacted(t *testing.T) {
	prevLocale := i18n.GetLocale()
	t.Cleanup(func() { i18n.SetLocale(prevLocale) })

	app := &App{logger: logging.NewLogger(logging.LevelInfo, io.Discard, false)}
	pathErr := fmt.Errorf("讀取失敗: %w", &fs.PathError{
		Op:   "open",
		Path: "/Users/alice/" + plantedDirMarker + "/emg.csv",
		Err:  fs.ErrNotExist,
	})

	const redactedErr = "讀取失敗: open <redacted-path>/emg.csv: file does not exist"

	cases := []struct {
		name   string
		locale i18n.Locale
		key    string
		want   string
	}{
		{"zh-TW", i18n.LocaleZhTW, i18n.KeyErrorHandlerAnalysisFailed, "分析失敗: " + redactedErr},
		{"en-US", i18n.LocaleEnUS, i18n.KeyErrorHandlerAnalysisFailed, "Analysis failed: " + redactedErr},
		{"missing_key_bare_fallback", i18n.LocaleZhTW, "error.handler.no_such_key", "error.handler.no_such_key: " + redactedErr},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i18n.SetLocale(tc.locale)

			assert.Equal(t, tc.want, app.failMessage(tc.key, pathErr))
		})
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
