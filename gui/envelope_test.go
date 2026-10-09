package gui

import (
	"fmt"
	"io"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"

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
