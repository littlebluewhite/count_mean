package gui

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// TestGetAvailablePhases_ReturnsStringMapForWailsCompatibility 守護 Wails v2
// TypeScript binding 契約 —— 此 method 的 return type 必須是 map[string][]string
// 而非 map[string][]models.PhasePoint，否則 Wails 生成的 App.d.ts 會引用
// 未定義的 models.PhasePoint 型別，導致前端 TypeScript build 失敗
// （codex Wave 4 PR-F1 P2 regression）。
func TestGetAvailablePhases_ReturnsStringMapForWailsCompatibility(t *testing.T) {
	var app *App // method 不解 receiver field，nil 安全

	// 編譯期 type 守護：若 GetAvailablePhases 改回 map[string][]models.PhasePoint，
	// 下一行賦值會編譯失敗，提醒開發者必須在 Wails 邊界保持 []string。
	var phases map[string][]string = app.GetAvailablePhases()

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

// TestGenerateChart_InvalidImageData_PropagatesError 是 cross-compare review 補的
// Wave 2 P1 fix (GenerateChart silent-success 移除) 的 regression test。
// 過去 chart.Generator 靜態 PNG 路徑會建出 chartConfig 但從未 WriteFile，仍回傳
// Success=true → 對前端是 silent success。修法後改為「沒有 ImageData = 錯誤」。
// 守護此契約：空 ImageData / 非 PNG dataURL 必須 return error 而非 nil result。
func TestGenerateChart_InvalidImageData_PropagatesError(t *testing.T) {
	var app *App // 兩個錯誤分支都不解 receiver field — nil 安全

	t.Run("empty_ImageData", func(t *testing.T) {
		result, err := app.GenerateChart(ChartParams{
			Title:    "test",
			FilePath: "irrelevant.csv",
			// ImageData intentionally empty
		})
		require.Nil(t, result, "empty ImageData should NOT return a result (silent-success regression)")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInvalidImageFormat),
			"want ErrInvalidImageFormat, got %v", err)
	})

	t.Run("non_png_data_url", func(t *testing.T) {
		result, err := app.GenerateChart(ChartParams{
			Title:     "test",
			FilePath:  "irrelevant.csv",
			ImageData: "data:image/svg+xml;base64,PHN2Zy8+",
		})
		require.Nil(t, result, "non-png dataURL should NOT return a result")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrInvalidImageFormat),
			"want ErrInvalidImageFormat, got %v", err)
	})
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

	// 行為斷言 (Wave 6 PR2 補強): 單純比 pointer 不能保證重建用的是「新 cfg」。
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
