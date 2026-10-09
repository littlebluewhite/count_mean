package gui

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/cci"
	"count_mean/internal/config"
)

// TestAnalyzeCCI_PanicInBody_CaughtAsInternalPanic 釘住 panic 的使用者可見契約:
// AnalyzeCCI 首句 `defer recoverHandlerPanic` 把 body 內任何 panic 轉成
// (nil, ErrInternalPanic) 走 named-return 通道上拋。
//
// 注入手法:把 a.logger 設為 nil。defer 之後的第一條 `a.logger.Info`(entry log)
// 即 nil-deref panic,模擬任一 body 內不可預期 panic。
//
// 驗證目的:確認在 nil-logger 下 AnalyzeCCI 不 panic 出來,且 errors.Is(err, ErrInternalPanic)
// 為 true、result 為 nil(panic 不降級成 failed-result)。
func TestAnalyzeCCI_PanicInBody_CaughtAsInternalPanic(t *testing.T) {
	app := &App{logger: nil, cciAnalyzer: cci.NewCCIAnalyzer()}
	app.state.Store(&appState{config: &config.AppConfig{OutputDir: t.TempDir()}})

	var (
		result *CCIResult
		err    error
	)
	require.NotPanics(t, func() {
		result, err = app.AnalyzeCCI(CCIParams{
			ManifestFile: filepath.Join(t.TempDir(), "x.csv"),
			DataFolder:   t.TempDir(),
			SubjectIndex: 0,
		})
	}, "body 內 panic 必須被首句 defer recoverHandlerPanic 攔住,不可 propagate")

	// panic 走 err 通道:errors.Is(err, ErrInternalPanic) 為 true、result 為 nil。
	require.Error(t, err, "body 內 panic 應轉成 non-nil err")
	assert.True(t, errors.Is(err, ErrInternalPanic),
		"panic 應被包成 ErrInternalPanic,got %v", err)
	assert.Nil(t, result, "panic 路徑不該回 result(panic 不降級成 failed-result)")
}
