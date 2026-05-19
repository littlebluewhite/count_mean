package gui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/models"
)

// TestApplyConfig_DoesNotResetInFlightThrottle 釘住 修法:applyConfig
// **不應**呼叫 a.progressManager.Reset()。Reset() 會把 lastUpdateAt 歸零,
// 該欄位是 *in-flight throttle state* — 若 SaveConfig 在分析進行中被觸發,
// 舊行為會 wipe 當前 calc 的 throttle 視窗,造成下一筆 progress 可能繞過
// throttle 直接 flood IPC。in-flight state 屬計算過程的瞬態,不該因為
// user-configurable knob 改變而被打斷。
//
// 設計取捨(option (a) of P1-24):applyConfig 只重設 user-configurable knob
// (csvHandler / calculator / normalizer / phaseAnalyzer 整個 snapshot,
// 透過 a.state.Store 原子 swap),不碰 lastUpdateAt / updateBuffer 等
// transient state。
//
// fast-second-run 場景(舊 註解擔心的)在現行 calculator 路徑下由
// ProgressManager 的 `step == 0` 與 isInitial(lastUpdateAt zero)兩條
// bypass rule 涵蓋;不需要 applyConfig 額外擾動 throttle state。
//
// 驗證策略:
//  1. 注入 fake emitter,把 updateBuffer 設超大,把 lastUpdateAt 推到 non-zero。
//  2. 呼叫 applyConfig 切到新 cfg。
//  3. 餵入一筆 CurrentStep=5 / Percentage=50 的 progress —— 此筆既非 100%
//     也非 CurrentStep=0,**必須仍被 throttle 抑制**(因 lastUpdateAt 未被清零)。
//
// 若 applyConfig 違規呼叫 Reset(),第二筆會 bypass throttle 出現在 spy → fail。
func TestApplyConfig_DoesNotResetInFlightThrottle(t *testing.T) {
	cfg := config.DefaultConfig()
	app := NewApp(cfg, "test-apply-config-no-reset")

	// 注入 spy emitter 取代 production wails emitter,讓 throttle 行為可觀察。
	spy := &fakeEmitter{}
	app.progressManager.emit = spy.emit
	// 超大 buffer 確保「lastUpdateAt 是否被 reset」是後續第二筆能否 emit 的唯一判準。
	app.progressManager.SetUpdateBuffer(10 * time.Second)

	// 第一筆:CurrentStep=5 / Percentage=50;首筆會吃 isInitial bypass
	// (lastUpdateAt 為 zero),emit 後 lastUpdateAt 被更新為 now。
	app.progressManager.UpdateProgress(models.ProgressInfo{
		CurrentStep:  5,
		TotalSteps:   100,
		Percentage:   50,
		Status:       "in-flight before applyConfig",
		ChannelIndex: 0,
	})
	require.Len(t, spy.snapshot(), 1, "前置 UpdateProgress 必須先觸發一次 emit,讓 throttle 進入 in-flight state")

	// 切換配置 — applyConfig 不應觸發 Reset。lastUpdateAt 維持 non-zero,
	// 後續非 final / 非 CurrentStep=0 的事件會繼續被 throttle 抑制。
	newCfg := config.DefaultConfig()
	newCfg.ScalingFactor = cfg.ScalingFactor + 1
	app.applyConfig(newCfg)

	// 第二筆:刻意 CurrentStep=5 / Percentage=50(非 100% 也非 step=0)。
	// 若 applyConfig 仍呼叫 Reset(),lastUpdateAt 被歸零,此筆會 bypass throttle
	// 出現在 spy → 違反 契約。
	app.progressManager.UpdateProgress(models.ProgressInfo{
		CurrentStep:  5,
		TotalSteps:   100,
		Percentage:   50,
		Status:       "in-flight after applyConfig",
		ChannelIndex: 0,
	})

	sent := spy.snapshot()
	require.Len(t, sent, 1,
		"applyConfig 不應 reset in-flight throttle state;第二筆 progress 應被 throttle 吞掉 "+
			"(observed %d events, statuses=%v)", len(sent), statusesOf(sent))
	assert.Equal(t, "in-flight before applyConfig", sent[0].Status,
		"唯一 emit 的應是 applyConfig 之前的 in-flight progress")
}

// statusesOf 是 t.Logf debug 用,把 ProgressInfo slice 的 status 字段摘出來。
func statusesOf(infos []models.ProgressInfo) []string {
	out := make([]string, len(infos))
	for i, info := range infos {
		out[i] = info.Status
	}
	return out
}
