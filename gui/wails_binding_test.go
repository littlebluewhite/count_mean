package gui

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/i18n"
)

// TestGetAvailablePhases_ReturnsStringMapForWailsCompatibility 守護 Wails v2
// TypeScript binding 契約 —— 此 method 的 return type 必須是 map[string][]string
// 而非 map[string][]models.PhasePoint，否則 Wails 生成的 App.d.ts 會引用
// 未定義的 models.PhasePoint 型別，導致前端 TypeScript build 失敗
// （codex Wave 4 PR-F1 P2 regression）。
//
// GetAvailablePhases 加 defer recoverHandlerPanicValue 後不能再用 nil App
// receiver —— defer 引數 `a.logger` 在 method entry 就被評估,nil receiver 在 defer
// 註冊前就會 panic。改用 NewApp() 建合法 App,業務 happy-path 行為與 nil-app 等價
// (都走 synchronizer.GetAvailable*Phases pure dispatch)。
func TestGetAvailablePhases_ReturnsStringMapForWailsCompatibility(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "test-get-available-phases")

	// 編譯期 type 守護：若 GetAvailablePhases 改回 map[string][]models.PhasePoint，
	// 下一行賦值會編譯失敗，提醒開發者必須在 Wails 邊界保持 []string。
	var phases map[string][]string = app.GetAvailablePhases() //nolint:staticcheck // ST1023: type 宣告刻意保留作為編譯期型別守護

	assert.Len(t, phases["start"], 10, "start phases 應有 10 個")
	assert.Len(t, phases["end"], 9, "end phases 應有 9 個（P0 不能當結束）")

	// 確認 wire 字串值與既有前端硬編碼一致
	assert.Equal(t,
		[]string{"P0", "P1", "P2", "S", "C", "D", "T0", "T", "O", "L"},
		phases["start"])
	assert.Equal(t,
		[]string{"P1", "P2", "S", "C", "D", "T0", "T", "O", "L"},
		phases["end"])
}

// TestApplyConfig_RebuildsComponents 是 cross-compare review 補的 Wave 2 P1 fix
// (SaveConfig component rebuild) 的 regression test。
// 過去 SaveConfig 只更新 a.config 不重建 csvHandler/maxMeanCalc/normalizer/phaseAnalyzer，
// 使用者改 ScalingFactor 後計算仍用舊值 — user-visible silent bug。修法用 applyConfig
// 在更新 config 時同步重建受影響元件。本 test 驗證重建確實發生（pointer 改變）。
func TestApplyConfig_RebuildsComponents(t *testing.T) {
	cfg1 := config.DefaultConfig()
	cfg1.ScalingFactor = 1

	app := NewApp(cfg1, "test-version")

	// 快照初始元件指標
	oldState := app.state.Load()

	// 套用新 config，應觸發重建（atomic.Pointer.Store 替換整個 appState）
	cfg2 := config.DefaultConfig()
	cfg2.ScalingFactor = 10
	app.applyConfig(cfg2)

	newState := app.state.Load()
	require.Same(t, cfg2, newState.config, "newState.config 必須指向新 cfg")
	assert.NotSame(t, oldState, newState, "整個 appState 必須是新實例（atomic swap）")
	assert.NotSame(t, oldState.csvHandler, newState.csvHandler, "csvHandler 必須是新實例（會持有 cfg2）")
	assert.NotSame(t, oldState.maxMeanCalc, newState.maxMeanCalc, "maxMeanCalc 必須是新實例（持有 ScalingFactor=10）")
	assert.NotSame(t, oldState.normalizer, newState.normalizer, "normalizer 必須是新實例")
	assert.NotSame(t, oldState.phaseAnalyzer, newState.phaseAnalyzer, "phaseAnalyzer 必須是新實例")

	// 行為斷言: 單純比 pointer 不能保證重建用的是「新 cfg」。
	// 例如未來若 buildAppState 被改成 `buildAppState(stale_cfg)` 或漏傳 cfg 參數,
	// pointer 仍會不同(allocation 還在發生)但 ScalingFactor 仍是 1 — 此 case
	// 是 cross-compare review P1 finding 的 worst case,pointer-identity 測不到。
	// 確認 maxMeanCalc 內部 ScalingFactor 確實是 cfg2.ScalingFactor 才足夠強。
	assert.Equal(t, 10, newState.config.ScalingFactor,
		"newState.config.ScalingFactor 必須等於 cfg2 (10)")
	assert.Equal(t, 10, newState.maxMeanCalc.ScalingFactor(),
		"重建後 maxMeanCalc 必須吃進 cfg2 的 ScalingFactor (10) — pointer 不同但內部仍是舊值即 regression")
}

// TestApp_ApplyConfigConcurrentWithReads_NoRace 釘住 fresh bug hunt 找到的真 race:
// applyConfig 在 SaveConfig/ResetConfig 路徑下連續寫 5 個 pointer 欄位
// (config / csvHandler / maxMeanCalc / normalizer / phaseAnalyzer),App struct
// 無 mutex/atomic 保護。Wails v2 預設並行 RPC 場景下 in-flight 讀方法
// (GetConfig 等 35 個 binding) 與 applyConfig 並行讀寫，會觸發 data race —
// 嚴重時造成「半新半舊」mixed state 下的 silent 錯誤計算結果。
//
// -race detector 是這個 test 的關鍵:沒有 -race 即使有 race 也可能 PASS。
// 修法 (atomic.Pointer[appState]) 前 -race 必抓到 DATA RACE on App.config 等欄位;
// 修法後讀寫都走 atomic 應 clean。
func TestApp_ApplyConfigConcurrentWithReads_NoRace(t *testing.T) {
	if !raceEnabled {
		t.Skip("requires -race; without race detector this test is a no-op (no assertion, only goroutine join)")
	}

	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1
	app := NewApp(cfg, "test-race")

	const writers = 4
	const readers = 6
	const iters = 100

	var wg sync.WaitGroup

	// writers: 反覆 applyConfig 改變 ScalingFactor,觸發 5 個欄位的非原子重建
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				c := config.DefaultConfig()
				c.ScalingFactor = (id*iters + j) % 10
				app.applyConfig(c)
			}
		}(i)
	}

	// readers: 反覆呼叫 GetConfig (讀 a.config) 觸發與 writers 的 read/write race
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				_ = app.GetConfig()
			}
		}()
	}

	wg.Wait()
}

// TestApp_SnapshotConsistency_UnderConcurrentApply 釘住 Wave 6 PR1 的 snapshot
// 撕裂修正:atomic.Pointer 只解 memory race,但若 entry method 取得 snapshot 後
// 路徑上有 helper 自行 a.state.Load(),仍會看到不同 snapshot — 即「半新半舊」
// 邏輯 race 在更高層復發。PR1 修法把 helper 簽名改為顯式接 *appState,杜絕該路徑。
//
// 此 test 用「entry 取 snapshot → 走 helper-like 操作 → 內部一致檢查」模擬:
// reader 取到 *appState 後,即便中途 SaveConfig 將 a.state 替換為新 snapshot,
// 手中的 *appState 內部 (config / maxMeanCalc) 仍必須來自同一次 buildAppState。
// 一旦不變式被破壞 (例如未來重構誤把 maxMeanCalc 改成 lazy load),此 test 會
// 在 -race 或 mismatch counter 兩條路徑都失敗。
func TestApp_SnapshotConsistency_UnderConcurrentApply(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.ScalingFactor = 1
	app := NewApp(cfg, "test-snapshot")

	const writers = 4
	const readers = 6
	const iters = 200

	var (
		wg            sync.WaitGroup
		teardownCount atomic.Int64
	)

	// writers: 反覆套用不同 ScalingFactor 的 cfg,逼出 buildAppState 重建路徑
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				c := config.DefaultConfig()
				if (id+j)%2 == 0 {
					c.ScalingFactor = 1
				} else {
					c.ScalingFactor = 10
				}
				app.applyConfig(c)
			}
		}(i)
	}

	// readers: snapshot 後在「兩次取欄位之間」插入 Gosched,模擬 helper 過程中
	// 被 scheduler 中斷的情境。snapshot 內部一致則不論 scheduler 何時切換,
	// s.config.ScalingFactor 永遠等於 s.maxMeanCalc.ScalingFactor()。
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				s := app.state.Load()
				cfgSF := s.config.ScalingFactor
				runtime.Gosched() // 模擬 helper 處理過程中可能的 schedule point
				mcSF := s.maxMeanCalc.ScalingFactor()
				if cfgSF != mcSF {
					teardownCount.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	if c := teardownCount.Load(); c > 0 {
		t.Fatalf("snapshot 撕裂:%d 次 cfg.ScalingFactor 與 maxMeanCalc.ScalingFactor() 不一致 — buildAppState 可能未原子建構", c)
	}
}

// TestGetTranslations_ReturnsLocaleDictionary 守護 Phase 1 frontend i18n MVP 契約 —
// GetTranslations 必須 return map[string]string (Wails binding ↔ frontend i18n.js 對齊),
// 且 4 個 supported locale 必須回對應字典。未知 locale 由 i18n.GetTranslationMap
// fallback 到 zh-TW;若該行為改變(例如改回 nil)會讓前端啟動時 t() 全失效。
//
// GetTranslations 加 defer recoverHandlerPanicValue 後不能再 nil receiver
// (a.logger 在 defer 引數評估階段被解,nil panic 在 recover 註冊前)。改用 NewApp。
func TestGetTranslations_ReturnsLocaleDictionary(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "test-get-translations")

	cases := []struct {
		locale string
		want   string
	}{
		{"zh-TW", "系統配置"},
		{"zh-CN", "系统配置"},
		{"en-US", "System Configuration"},
		{"ja-JP", "システム設定"},
	}
	for _, tc := range cases {
		dict := app.GetTranslations(tc.locale)
		require.NotNil(t, dict, "locale=%s 字典不應為 nil", tc.locale)
		assert.Equal(t, tc.want, dict[i18n.KeyConfigPanelTitle],
			"locale=%s KeyConfigPanelTitle = %q, want %q", tc.locale, dict[i18n.KeyConfigPanelTitle], tc.want)
	}

	fallbackDict := app.GetTranslations("xx-YY")
	assert.Equal(t, "系統配置", fallbackDict[i18n.KeyConfigPanelTitle],
		"unknown locale 應 fallback 到 zh-TW")
}

// TestSetLanguage_ValidLocale_NoError 守護 4 個 supported locale 都被 SetLanguage 接受。
//
// SetLanguage 加 defer recoverHandlerPanic 後 nil receiver 失效(同上理由),
// 改用 NewApp。
func TestSetLanguage_ValidLocale_NoError(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "test-set-language-valid")

	for _, loc := range []string{"zh-TW", "zh-CN", "en-US", "ja-JP"} {
		err := app.SetLanguage(loc)
		require.NoError(t, err, "locale=%s 必須被接受", loc)
	}
}

// TestSetLanguage_InvalidLocale_Errors 守護未知 locale 一定 return error,
// 避免前端誤傳髒字串污染 backend i18n state(case-sensitive 比對)。
//
// 同 TestSetLanguage_ValidLocale_NoError,改用 NewApp 因為加了 defer。
func TestSetLanguage_InvalidLocale_Errors(t *testing.T) {
	app := NewApp(config.DefaultConfig(), "test-set-language-invalid")

	cases := []string{"", "xx-YY", "english", "ZH-TW", "fr-FR", "zh"}
	for _, loc := range cases {
		err := app.SetLanguage(loc)
		require.Error(t, err, "locale=%q 必須被拒絕", loc)
	}
}

// TestSaveConfig_InvalidLocale_Rejected 守護 SaveConfig 必須先呼叫
// cfg.Validate() 才能寫檔。否則 cfg.Language="fr-FR" 等 unsupported locale 會被
// 寫進 config.json,下次 LoadConfig.Validate() 又會拒絕並 revert 為 default —
// 使用者的修改實質上「保存後 reboot 就消失」,且毫無錯誤回饋。
//
// 後 SaveConfig 用 a.configPath 而非 hardcoded "./config.json",test 改用
// NewAppWithConfigPath 顯式注入 path 到 t.TempDir 下,不再依賴 t.Chdir。
func TestSaveConfig_InvalidLocale_Rejected(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Language = "fr-FR" // unsupported — Validate() 會 reject

	tmpConfigPath := filepath.Join(t.TempDir(), "config.json")
	app := NewAppWithConfigPath(config.DefaultConfig(), "test", tmpConfigPath)

	err := app.SaveConfig(cfg)
	require.Error(t, err, "SaveConfig 應在寫檔前以 cfg.Validate() 攔下無效 locale")

	// 確認 config.json 沒被寫出(fix 前會寫,fix 後不會寫)
	_, statErr := os.Stat(tmpConfigPath)
	require.True(t, os.IsNotExist(statErr),
		"無效 locale 不可保存 — 期望 config.json 不存在,實際 statErr=%v", statErr)
}

// TestSaveConfig_ValidLocale_Saved 守護 happy path 不被 fix 誤傷:
// 合法 locale 仍應寫入 config.json,且 app 持有的 config 被更新。
//
// 後 SaveConfig 用 a.configPath 而非 hardcoded "./config.json",test 用
// NewAppWithConfigPath 顯式注入 path 到 t.TempDir 下,assert 在那個位置看到檔。
func TestSaveConfig_ValidLocale_Saved(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Language = "en-US"

	tmpConfigPath := filepath.Join(t.TempDir(), "config.json")
	app := NewAppWithConfigPath(config.DefaultConfig(), "test", tmpConfigPath)

	require.NoError(t, app.SaveConfig(cfg), "合法 locale 必須成功保存")

	_, statErr := os.Stat(tmpConfigPath)
	require.NoError(t, statErr, "config.json 必須被寫出到注入路徑")

	// app 內部 state 也應反映新 cfg
	gotCfg := app.GetConfig()
	require.Equal(t, "en-US", gotCfg.Language, "applyConfig 必須把新 cfg 載入 atomic state")
}
