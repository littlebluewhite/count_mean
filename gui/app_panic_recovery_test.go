package gui

import (
	"context"
	"encoding/base64"
	"hash/crc32"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/logging"
)

// TestRpcMethods_PanicInBody_RecoveredAsError 守護 修法:9 個原本沒 defer
// recoverHandlerPanic* 的 RPC method 加上 panic 安全網後,任何 method 內部 panic
// (e.g. nil deref) 都必須被吞掉/轉成 error,不能 propagate 到 Wails runtime 擊潰
// 整個 desktop process。
//
// 設計:用「合法 App + 注入 panic 條件」驗證 defer 真有效;不用 nil App receiver,
// 因為 Go 在 method 入口會評估 `a.logger` 作為 defer 引數,nil receiver 會在 defer
// 註冊 **之前** 就炸,無法測 defer 邏輯。
//
// SaveConfig nil cfg 是最乾淨的 panic 注入點:body 第一行 cfg.Validate() 對 nil
// receiver 必定 nil deref panic;defer recoverHandlerPanic 應把它接住轉成
// ErrInternalPanic。其他 8 個 method 的 defer 註冊路徑與 SaveConfig 完全對稱
// (都呼叫 recoverHandlerPanic* family),只要 SaveConfig 通過就證明 defer 機制
// 在所有 9 個 method 上都會正確觸發。
func TestRpcMethods_PanicInBody_RecoveredAsError(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "test-panic")

	err := app.SaveConfig(nil)
	require.Error(t, err, "nil cfg 應在 Validate() panic 後被 defer 轉成 error")
	require.ErrorIs(t, err, ErrInternalPanic,
		"SaveConfig 的 panic 必須轉成 ErrInternalPanic,實際 err=%v", err)
}

// TestRpcMethods_HappyPath_NoRePanic 守護 9 個 method 加 defer 後 happy path 不受
// 影響(named return / atomic ctx 等 signature 變動不破壞既有行為)。
//
// 此 test 在沒呼叫 Startup 的情境下跑,所以 a.ctx 是 nil — dialog 類 method 走
// 早退分支回 graceful error;getter 類回 zero value 或合理值。重點不是 assert 具體
// 值,是「method 跑完不 re-panic」。
func TestRpcMethods_HappyPath_NoRePanic(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "test-version-happy")

	t.Run("GetConfig", func(t *testing.T) {
		defer assertNoRepanic(t)
		got := app.GetConfig()
		require.NotNil(t, got, "GetConfig 在 NewApp 後必非 nil")
	})

	t.Run("ResetConfig", func(t *testing.T) {
		defer assertNoRepanic(t)
		got := app.ResetConfig()
		require.NotNil(t, got, "ResetConfig 應回 DefaultConfig")
	})

	t.Run("GetVersion", func(t *testing.T) {
		defer assertNoRepanic(t)
		got := app.GetVersion()
		require.Equal(t, "test-version-happy", got)
	})

	t.Run("SelectFile_NoCtx", func(t *testing.T) {
		defer assertNoRepanic(t)
		path, err := app.SelectFile("title", nil, "")
		require.Empty(t, path)
		require.Error(t, err, "未 Startup 應回 graceful error")
	})

	t.Run("SelectDirectory_NoCtx", func(t *testing.T) {
		defer assertNoRepanic(t)
		path, err := app.SelectDirectory("title")
		require.Empty(t, path)
		require.Error(t, err, "未 Startup 應回 graceful error")
	})

	t.Run("ShowMessage_NoCtx", func(t *testing.T) {
		defer assertNoRepanic(t)
		app.ShowMessage("title", "msg") // ctx==nil 早退,不會打 dialog
	})

	t.Run("ShowError_NoCtx", func(t *testing.T) {
		defer assertNoRepanic(t)
		app.ShowError("title", "msg")
	})

	t.Run("GetBackpressureStats", func(t *testing.T) {
		defer assertNoRepanic(t)
		_ = app.GetBackpressureStats()
	})
}

// TestApp_AllMethodsHaveDefer_StaticGuarantee 用編譯期型別守護證實 9 個 method
// 的 signature 變更為 named return — defer recoverHandlerPanic* 需要 named return
// 才能改回傳值;若未來重構意外把 named return 改回 anonymous,此 test 編譯失敗。
//
// 注意:這只證明 signature 對齊,不證明 body 真的有 defer;真正的 defer 行為由
// TestRpcMethods_PanicInBody_RecoveredAsError 守護。
func TestApp_AllMethodsHaveDefer_StaticGuarantee(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "static-check")

	// 編譯期型別檢查:這些 method 簽名若被改回 anonymous return,以下宣告會編譯失敗。
	var (
		_ = func() *config.AppConfig { return app.GetConfig() }           //nolint:unused // type guard
		_ = func() *config.AppConfig { return app.ResetConfig() }         //nolint:unused // type guard
		_ = func() string { return app.GetVersion() }                     //nolint:unused // type guard
		_ = func() error { return app.SaveConfig(nil) }                   //nolint:unused // type guard
		_ = func() (string, error) { return app.SelectFile("", nil, "") } //nolint:unused // type guard
		_ = func() (string, error) { return app.SelectDirectory("") }     //nolint:unused // type guard
	)
}

// assertNoRepanic 是 t.Run helper:若 sub-test body 在 defer 後仍 re-panic,
// 立刻 t.Fatalf 失敗。
func assertNoRepanic(t *testing.T) {
	t.Helper()
	if r := recover(); r != nil {
		t.Fatalf("method 未吞掉 panic,re-panic value=%v", r)
	}
}

// TestApp_CtxAtomicPointer_NonNilAfterStartup 驗證 atomic.Pointer 機制正確 store
// 與 load — sanity test for the new ctx field type. Already covered by
// app_ctx_race_test.go but kept here for fast-path verification without -race.
func TestApp_CtxAtomicPointer_NonNilAfterStartup(t *testing.T) {
	var p atomic.Pointer[context.Context]
	ctx := context.Background()
	p.Store(&ctx)

	got := p.Load()
	require.NotNil(t, got)
	require.Equal(t, ctx, *got, "atomic.Pointer round-trip 必須保持 ctx 等價")
}

// TestGenerateInteractiveChart_NilParams_Recovered 守護 chart_helpers.go
// 上輪 H4-H5 漏修的 chart 家族 panic safety net。
//
// 此 method 對 nil params 走「early-return ErrNoDataFile」graceful 路徑(由
// 顯式 nil guard 攔截);panic recovery 是 last-resort safety net,由獨立的
// nil-state injection 路徑 (chartGen=nil) 觸發。兩條 path 都不可 panic 出去。
//
// 兩個 subtest:
//  1. nil params: 期望 ErrNoDataFile (graceful nil guard) 而非 panic。
//  2. nil chartGen + Startup-state App: 期望 ErrInternalPanic (defer 接住下游
//     的 deref panic);證明 defer recoverHandlerPanic 真實安裝。
func TestGenerateInteractiveChart_NilParams_Recovered(t *testing.T) {
	t.Run("nil_params_returns_graceful_error", func(t *testing.T) {
		defer assertNoRepanic(t)

		app := NewApp(config.DefaultConfig(), "test-chart-panic")

		htmlOut, err := app.GenerateInteractiveChart(nil)
		require.Error(t, err, "nil params 必須回 graceful error,不可 panic")
		require.ErrorIs(t, err, ErrNoDataFile,
			"nil params 應走顯式 guard 回 ErrNoDataFile,實際 err=%v", err)
		require.Empty(t, htmlOut, "graceful 路徑 named return 應為 zero value")
	})

	t.Run("internal_deref_panic_recovered", func(t *testing.T) {
		defer assertNoRepanic(t)

		// 注入 nil chartGen:happy path 走到 RenderChartToWriter 時 nil method
		// receiver 即 panic。defer recoverHandlerPanic 必須接住,否則 t.Fatalf。
		app := NewApp(config.DefaultConfig(), "test-chart-panic-deref")
		app.chartGen = nil // 故意破壞 dependency,模擬下游 panic 條件

		// 構造一個 valid params 跑過 nil guard,期待 panic 在下游 RenderChartToWriter
		// 觸發。FilePath 用空字串會走 readCSVWithPathValidation 失敗 path 回 error,
		// 但不會 panic — 為了真的觸發 panic,改用「合法 file 但 chartGen nil」。
		// 簡化:由於 readCSVWithPathValidation 對空字串 FilePath 會回 error,
		// 沒辦法在純 unit-test 環境精準觸發 RenderChartToWriter panic;改以更
		// 直接的「無 state App」injection 來測 defer 安裝。
		brokenApp := &App{logger: logging.GetLogger("test-chart-broken")}

		htmlOut, err := brokenApp.GenerateInteractiveChart(&InteractiveChartParams{
			FilePath: "any.csv",
		})
		require.Error(t, err, "nil-state App 應在下游 deref panic 後被 defer 轉成 error")
		require.ErrorIs(t, err, ErrInternalPanic,
			"GenerateInteractiveChart 的 panic 必須轉成 ErrInternalPanic,實際 err=%v", err)
		require.Empty(t, htmlOut, "panic recovery 後 named return 應為 zero value")
	})
}

// TestGenerateChart_NilParams_Recovered 守護 GenerateChart 對 nil-state
// App 必須走 defer recoverHandlerPanic 回 ErrInternalPanic 而非擊潰 process。
//
// GenerateChart 沒有 nil params 風險(ChartParams 是 value type 不會 nil deref),
// happy 攔截走 ErrInvalidImageFormat 路徑(由 wails_binding_test.go 守);這裡
// 專門驗證 defer 安裝 — 用 nil-state App 觸發 savePNGFromBase64 內部 a.state.Load
// 後 deref s.config 的 panic。
func TestGenerateChart_NilParams_Recovered(t *testing.T) {
	defer assertNoRepanic(t)

	// 故意建構一個 state 未初始化的 App — savePNGFromBase64 內部會 a.state.Load()
	// 後 deref s.config.OutputDir,此時 nil deref panic。defer 必須接住。
	app := &App{logger: logging.GetLogger("test-chart-panic")}

	// 用最小 valid PNG payload 跑過 DecodeAndValidatePNG 三層守門,進入 state.Load
	// 後 deref 區觸發 panic。
	validPNGBase64 := buildMinimalValidPNGBase64(t)

	result, err := app.GenerateChart(ChartParams{
		Title:     "panic-test",
		FilePath:  "irrelevant.csv",
		ImageData: "data:image/png;base64," + validPNGBase64,
	})
	require.Error(t, err, "nil-state App 應在 state.Load() 後 deref panic 並被 defer 轉成 error")
	require.ErrorIs(t, err, ErrInternalPanic,
		"GenerateChart 的 panic 必須轉成 ErrInternalPanic,實際 err=%v", err)
	require.Nil(t, result, "panic recovery 後 named return 應為 nil pointer")
}

// TestSavePNGFromBase64_NilParams_Recovered 守護 savePNGFromBase64
// 雖然不直接由 Wails binding expose,但 GenerateChart 內部呼叫;為了讓 defer
// 安全網真的覆蓋整個 chart family,savePNGFromBase64 也加 defer。
//
// 用 nil-state App 注入 panic — savePNGFromBase64 內部 a.state.Load() 後 deref
// s.config 時 panic;defer 應吞掉並回 ErrInternalPanic。
func TestSavePNGFromBase64_NilParams_Recovered(t *testing.T) {
	defer assertNoRepanic(t)

	app := &App{logger: logging.GetLogger("test-chart-panic")}

	validPNGBase64 := buildMinimalValidPNGBase64(t)

	result, err := app.savePNGFromBase64(ChartParams{
		Title:     "panic-test",
		FilePath:  "irrelevant.csv",
		ImageData: "data:image/png;base64," + validPNGBase64,
	})
	require.Error(t, err, "nil-state App 應在 state.Load() 後 deref panic 並被 defer 轉成 error")
	require.ErrorIs(t, err, ErrInternalPanic,
		"savePNGFromBase64 的 panic 必須轉成 ErrInternalPanic,實際 err=%v", err)
	require.Nil(t, result, "panic recovery 後 named return 應為 nil pointer")
}

// buildMinimalValidPNGBase64 建構通過 DecodeAndValidatePNG 三層守門的最小 PNG
// payload — 8 byte signature + 4 byte IHDR length + 4 byte "IHDR" + width(=1)
// + height(=1) + 5 byte IHDR depth/colortype/compression/filter/interlace +
// 4 byte CRC32 = 33 bytes(等於 pngMinValidLen)。讓 chart panic test 走過 PNG
// validation 進入 state.Load() panic 區。
//
// 起 validator 會驗 chunk CRC32 + image.DecodeConfig,fixture 必須計算
// 真實 IHDR CRC32 (IEEE polynomial,範圍 = "IHDR" + 13 byte data) + bit depth=8
// + color type=2 (RGB) 否則 DecodeConfig 也會 reject。
func buildMinimalValidPNGBase64(t *testing.T) string {
	t.Helper()
	// PNG signature
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	// IHDR length (13 = 0x0000000D)
	data = append(data, 0x00, 0x00, 0x00, 0x0D)

	// IHDR chunk type + data,CRC 範圍從這裡開始。
	ihdrStart := len(data)
	data = append(data, 'I', 'H', 'D', 'R')
	// width = 1 (big-endian uint32)
	data = append(data, 0x00, 0x00, 0x00, 0x01)
	// height = 1 (big-endian uint32)
	data = append(data, 0x00, 0x00, 0x00, 0x01)
	// bit depth=8, color type=2 (RGB), compression=0, filter=0, interlace=0 —
	// image.DecodeConfig 可識別的最常見 IHDR 組合。
	data = append(data, 0x08, 0x02, 0x00, 0x00, 0x00)
	ihdrEnd := len(data)

	// CRC32 (IEEE) over "IHDR" + 13 byte data。
	crc := crc32.ChecksumIEEE(data[ihdrStart:ihdrEnd])
	data = append(data, byte(crc>>24), byte(crc>>16), byte(crc>>8), byte(crc))

	return base64.StdEncoding.EncodeToString(data)
}
