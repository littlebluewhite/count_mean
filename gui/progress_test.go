package gui

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

// fakeEmitter 是 progressEmitter 的 thread-safe spy,用來在 unit test 中觀察
// ProgressManager 觸發了哪些 ProgressInfo,而不需要 boot 整個 Wails runtime。
type fakeEmitter struct {
	mu   sync.Mutex
	sent []models.ProgressInfo
}

//nolint:gocritic // hugeParam: 與 progressEmitter signature 對齊
func (f *fakeEmitter) emit(info models.ProgressInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, info)
}

func (f *fakeEmitter) snapshot() []models.ProgressInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]models.ProgressInfo, len(f.sent))
	copy(out, f.sent)

	return out
}

// newTestProgressManager 構造直接走 fake emitter 的 ProgressManager,
// 跳過 Wails ctx 解析以保持 unit test 自足。
func newTestProgressManager(emit progressEmitter) *ProgressManager {
	pm := NewProgressManager(func() context.Context { return nil })
	pm.emit = emit

	return pm
}

// TestProgressManager_UpdateProgress_EmitsToWailsEvents 守護(Wave 3
// Batch S)的核心契約:UpdateProgress 必須把每筆 ProgressInfo 透過注入的
// emitter 推送出去(production 由 NewProgressManager 內接到
// runtime.EventsEmit)。若回退到舊 polling 模型(state 存好,但沒呼叫 emit),
// 此測試會在 sent 長度為 0 時失敗,提早攔下 regression。
func TestProgressManager_UpdateProgress_EmitsToWailsEvents(t *testing.T) {
	spy := &fakeEmitter{}
	pm := newTestProgressManager(spy.emit)
	// 把節流關掉,避免時間相依干擾;100% 路徑會另外驗證節流仍生效
	pm.SetUpdateBuffer(0)

	info := models.ProgressInfo{
		CurrentStep:  5,
		TotalSteps:   10,
		Percentage:   50,
		Status:       "running",
		ChannelIndex: 1,
		ChannelName:  "Ch1",
	}

	pm.UpdateProgress(info)

	sent := spy.snapshot()
	require.Len(t, sent, 1, "UpdateProgress 必須觸發一次 emit(events push 模型)")
	assert.Equal(t, info, sent[0], "emit 的內容必須與 caller 傳入的 ProgressInfo 一致")
}

// TestProgressManager_UpdateProgress_ThrottleNonComplete 守護更新節流不被 emit
// 改寫破壞:同一個 updateBuffer 視窗內、< 100% 的更新只能 emit 一次。
// 否則 calculator 高頻 progress 會把 Wails IPC 打爆(舊實作的 lastUpdateAt
// 短路保留,此 test 守護)。
func TestProgressManager_UpdateProgress_ThrottleNonComplete(t *testing.T) {
	spy := &fakeEmitter{}
	pm := newTestProgressManager(spy.emit)
	pm.SetUpdateBuffer(50 * time.Millisecond)

	base := models.ProgressInfo{Percentage: 25, Status: "running", TotalSteps: 100, CurrentStep: 25}
	pm.UpdateProgress(base)
	// 立刻第二次:應被節流
	pm.UpdateProgress(base)
	pm.UpdateProgress(base)

	sent := spy.snapshot()
	assert.Len(t, sent, 1, "節流視窗內 < 100%% 的更新只能 emit 一次")
}

// TestProgressManager_UpdateProgress_CompletionBypassesThrottle 守護完成事件
// 必須無條件 emit。若節流誤把 100% 也擋下,前端永遠收不到「完成」訊號 —
// 屬於 user-visible 卡死(progress bar 停在 99%)。
func TestProgressManager_UpdateProgress_CompletionBypassesThrottle(t *testing.T) {
	spy := &fakeEmitter{}
	pm := newTestProgressManager(spy.emit)
	pm.SetUpdateBuffer(10 * time.Second) // 故意設超大視窗,證明 100% 不受節流

	pm.UpdateProgress(models.ProgressInfo{Percentage: 25, Status: "running"})
	pm.UpdateProgress(models.ProgressInfo{Percentage: progressCompletionPercent, Status: "done"})

	sent := spy.snapshot()
	require.Len(t, sent, 2, "100%% 必須 bypass throttle 立即 emit")
	assert.InDelta(t, 100.0, sent[1].Percentage, 0.0001)
	assert.Equal(t, "done", sent[1].Status)
}

// TestProgressManager_CreateProgressCallback_BindsToUpdate 守護 calculator
// 透過 SetProgressCallback 接到的函式確實就是 UpdateProgress(events 推播
// 的 entry point)。中斷此 binding 等同進度報告整條斷掉。
func TestProgressManager_CreateProgressCallback_BindsToUpdate(t *testing.T) {
	spy := &fakeEmitter{}
	pm := newTestProgressManager(spy.emit)
	pm.SetUpdateBuffer(0)

	cb := pm.CreateProgressCallback()
	cb(models.ProgressInfo{Percentage: 30, Status: "running"})

	sent := spy.snapshot()
	require.Len(t, sent, 1, "CreateProgressCallback 回傳的函式必須走進 UpdateProgress")
	assert.InDelta(t, 30.0, sent[0].Percentage, 0.0001)
}

// TestProgressManager_WailsEmitter_SafeForNilCtx 守護真正用於 production 的
// wailsProgressEmitter:若 ctxFn 回傳 nil(unit test / Startup 之前),
// 必須 silent skip 而非 panic / log.Fatalf(runtime.EventsEmit 對非 Wails ctx
// 會 Fatalf 整個 process,這條 fallback 是防線)。
func TestProgressManager_WailsEmitter_SafeForNilCtx(t *testing.T) {
	emit := wailsProgressEmitter(func() context.Context { return nil })

	// 不應 panic 也不應 Fatalf;成功跑完即 PASS。
	emit(models.ProgressInfo{Percentage: 50, Status: "should not crash"})
}

// TestProgressManager_WailsEmitter_SafeForNonWailsCtx 守護當 ctxFn 回傳的是
// context.Background()(non-Wails ctx)時 emitter 也不會 Fatalf。
// 這對應 App.context() 在 Startup 前的 fallback 路徑,以及 unit test 場景。
func TestProgressManager_WailsEmitter_SafeForNonWailsCtx(t *testing.T) {
	emit := wailsProgressEmitter(func() context.Context { return context.Background() })

	// runtime.EventsEmit 對沒有 "events" value 的 ctx 會 log.Fatalf;
	// wailsProgressEmitter 必須在呼叫前自己擋住 — 跑完不 crash 即 PASS。
	emit(models.ProgressInfo{Percentage: 50, Status: "should not crash"})
}

// TestHasWailsEventsHandler_MatrixedCtxStates 釘住 hasWailsEventsHandler
// 的所有分支 — 此 helper 嚴格只認 Wails native string key,不受 typed
// test marker 影響(避免 test-only signal 誤觸發 runtime.EventsEmit Fatalf)。
//
// 此 test 是 typed key 重構的第一道守門:若有人未來把 wailsEventsCtxKey
// 改成 typed key,production code 會 silently 拿不到 Wails 注入的 value,
// 進而永久 short-circuit emit pipeline — 此 test 在 CI 立刻抓到。
func TestHasWailsEventsHandler_MatrixedCtxStates(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context //nolint:containedctx // 測試用,刻意儲存
		want bool
	}{
		{"nil_ctx", nil, false},
		{"background_no_marker", context.Background(), false},
		{
			name: "wails_native_events_key",
			//nolint:staticcheck // SA1029: 模擬 Wails framework 的 string key 注入
			ctx:  context.WithValue(context.Background(), wailsEventsCtxKey, "fake-events-handler"), //nolint:staticcheck // SA1029
			want: true,
		},
		// 重點:typed test marker 不該觸發 hasWailsEventsHandler(避免後續
		// runtime.EventsEmit 撞 log.Fatalf)
		{
			name: "test_typed_marker_only",
			ctx:  context.WithValue(context.Background(), progressCtxReadyKey{}, true),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hasWailsEventsHandler(tc.ctx)
			assert.Equal(t, tc.want, got, "hasWailsEventsHandler mismatch for %s", tc.name)
		})
	}
}

// TestIsProgressCtxReady_MatrixedCtxStates 釘住 isProgressCtxReady 的
// superset 行為:同時認 Wails native key 與 test typed key。
//
// 與 hasWailsEventsHandler 分離測試的理由:isProgressCtxReady 是給 test 用,
// hasWailsEventsHandler 是給 production wailsProgressEmitter 用。兩條路徑
// 行為差異(test marker 是否被認)是 設計的核心。
func TestIsProgressCtxReady_MatrixedCtxStates(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context //nolint:containedctx // 測試用,刻意儲存
		want bool
	}{
		{"nil_ctx", nil, false},
		{"background_no_marker", context.Background(), false},
		{
			name: "wails_native_events_key",
			ctx:  context.WithValue(context.Background(), wailsEventsCtxKey, "fake-events-handler"), //nolint:staticcheck // SA1029: simulate Wails framework
			want: true,
		},
		{
			name: "test_typed_marker_key",
			ctx:  context.WithValue(context.Background(), progressCtxReadyKey{}, true),
			want: true,
		},
		{
			name: "both_keys_present",
			ctx: context.WithValue(
				context.WithValue(context.Background(), progressCtxReadyKey{}, true),
				wailsEventsCtxKey, "fake-events-handler", //nolint:staticcheck // SA1029: simulate Wails framework
			),
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isProgressCtxReady(tc.ctx)
			assert.Equal(t, tc.want, got, "isProgressCtxReady mismatch for %s", tc.name)
		})
	}
}

// TestWailsProgressEmitter_OnlyWailsCtxTriggersEmit 是 integration smoke
// test:wailsProgressEmitter 必須只在 ctx 帶真實 Wails events key 時才嘗試
// emit(避免 test typed key 誤觸發 runtime.EventsEmit 撞 Fatalf)。
//
// 由於我們不能真的 boot Wails runtime 在 unit test 跑 EventsEmit(會殺
// process),改用「探測 ctxFn 被呼叫了幾次 + 模擬 Wails ctx」的等價斷言:
//
//	純 typed marker ctx → emitter 內部 hasWailsEventsHandler=false → silent skip
//	不會走到 runtime.EventsEmit,test process 不 crash。
//
// 此 test 守的核心 invariant:typed marker 路徑不該洩漏到 production emit。
func TestWailsProgressEmitter_OnlyWailsCtxTriggersEmit(t *testing.T) {
	// 純 typed marker ctx:wailsProgressEmitter 應 silent skip 不真 EventsEmit
	ctxFn := func() context.Context {
		return context.WithValue(context.Background(), progressCtxReadyKey{}, true)
	}

	emit := wailsProgressEmitter(ctxFn)

	// 跑完不 crash 就 PASS — 證明 typed marker 沒有走進 runtime.EventsEmit
	// (走進的話 process 會被 log.Fatalf 殺掉, test runner 直接終止)。
	emit(models.ProgressInfo{Percentage: 50, Status: "must-not-crash"})
}

// TestProgressEventName_ContractWithFrontend 守護 backend 與 frontend 對事件名
// 的硬編碼合約。前端 main.js 透過 EventsOn("progress", handler) 訂閱,
// 一旦此常數被改動(typo / 重新命名),frontend 將收不到任何 progress
// 更新。把常數 freeze 在 test 裡讓人為改動立刻被攔下。
func TestProgressEventName_ContractWithFrontend(t *testing.T) {
	assert.Equal(t, "progress", ProgressEventName,
		"frontend main.js 訂閱的事件名是 \"progress\";若需要重新命名,"+
			"必須同步更新 frontend/src/main.js 的 EventsOn 字串")
}

// TestProgressManager_InitialFrameBypassesThrottle 釘住 核心 fix:
// ProgressManager 第一次 UpdateProgress (lastUpdateAt is zero) 不能被
// throttle 抑制 — 否則前端永遠收不到「分析開始」訊號。
//
// 配合 ProgressTracker 端的修法(lastUpdateAt 初始 zero / Start 強制 emit
// 首筆),這條測試證明 GUI 端 ProgressManager 不會在 boundary 把首筆吞掉。
func TestProgressManager_InitialFrameBypassesThrottle(t *testing.T) {
	spy := &fakeEmitter{}
	pm := newTestProgressManager(spy.emit)
	// 故意設超大 buffer,證明 initial frame (lastUpdateAt is zero) 必出
	pm.SetUpdateBuffer(10 * time.Second)

	info := models.ProgressInfo{
		CurrentStep: 0,
		TotalSteps:  10,
		Percentage:  0,
		Status:      "初始化",
	}
	pm.UpdateProgress(info)

	sent := spy.snapshot()
	require.Len(t, sent, 1, "ProgressManager 的首筆 progress 必須 bypass throttle (P1-J H27)")
	assert.Equal(t, 0, sent[0].CurrentStep)
	assert.InDelta(t, 0.0, sent[0].Percentage, 0.0001)
}

// TestProgressManager_StepZeroAlwaysEmits 守護後續任何 CurrentStep=0 的事件
// 都要 bypass throttle。模擬 calculator 因多階段分析在中途又把 Tracker
// 重置回 0 觸發新一輪 progress 的場景(尚未實作的需求但 API 不應該
// silently 吃掉這個訊號)。
func TestProgressManager_StepZeroAlwaysEmits(t *testing.T) {
	spy := &fakeEmitter{}
	pm := newTestProgressManager(spy.emit)
	pm.SetUpdateBuffer(10 * time.Second)

	// 第一筆 25%(initial frame bypass 出去)
	pm.UpdateProgress(models.ProgressInfo{CurrentStep: 2, Percentage: 25, Status: "stage A"})

	// 第二筆 CurrentStep=0(模擬新階段啟動),即使在 buffer 內也必出。
	pm.UpdateProgress(models.ProgressInfo{CurrentStep: 0, Percentage: 0, Status: "stage B start"})

	sent := spy.snapshot()
	require.Len(t, sent, 2,
		"CurrentStep=0 必須 bypass throttle (observed %d events)", len(sent))
	assert.Equal(t, "stage B start", sent[1].Status)
}

// TestProgressManager_ConcurrentUpdateProgress_NoRace 是 補的 concurrent
// stress test:多 goroutine 同時打 UpdateProgress + Reset + SetUpdateBuffer 必須
// 在 -race detector 下 clean 通過。
//
// 為什麼需要:calculator 透過 ProgressCallback 從 collector goroutine 同步呼叫
// UpdateProgress,但 SetProgressCallback / SetUpdateBuffer 的路徑可能與
// UpdateProgress 並行(applyConfig 路徑)。lastUpdateAt / emitter / updateBuffer
// 三個欄位都靠 mutex 守護,任何 unsynchronized read/write 在 -race 下會被抓到。
//
// 此 test 用 8 個 writer × 1000 筆 progress 模擬 high-frequency calculator
// progress + 同時頻繁 Reset() / SetUpdateBuffer() 從 UI thread,逼出可能的
// race condition。
//
// 為何不 t.Skip on !race:
//   - 即使無 race detector 也要驗 emit 次數的下界(每筆 UpdateProgress 都該
//     被 ProgressManager 看到一次,不應有 panic / data corruption)。
//   - 有 -race 才能抓到真正的 data race。CI 用 `make test-race` 跑這條會啟用
//     race detector,本地 `go test ./gui/...` 不啟用仍能跑通(只少 race 檢查)。
//
// testing.Short() 仍 honored — CI 可用 -short 加速;default 完整跑。
//
//nolint:funlen // 8 goroutine × 1000 筆 stress test 需要足夠 inline 配置
func TestProgressManager_ConcurrentUpdateProgress_NoRace(t *testing.T) {
	if testing.Short() {
		t.Skip("跳過 high-load concurrent test(use -short=false to enable)")
	}

	const (
		numWriters       = 8
		updatesPerWriter = 1000
	)

	spy := &fakeEmitter{}
	pm := newTestProgressManager(spy.emit)
	pm.SetUpdateBuffer(0) // 關閉節流,讓每筆 update 都試圖 emit,壓力最大

	var (
		wg          sync.WaitGroup
		resetCount  atomic.Int64
		bufferCount atomic.Int64
	)

	// numWriters 個 progress 寫者:模擬 calculator collector goroutine
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < updatesPerWriter; i++ {
				pm.UpdateProgress(models.ProgressInfo{
					CurrentStep:  workerID*updatesPerWriter + i,
					TotalSteps:   numWriters * updatesPerWriter,
					Percentage:   float64(i) / float64(updatesPerWriter) * 100, //nolint:mnd // 百分比基數
					Status:       "running",
					ChannelIndex: workerID,
				})
			}
		}(w)
	}

	// 2 個 Reset 寫者:模擬 UI thread 啟動新一輪 analysis
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ { //nolint:mnd // 200 次 Reset 與 writers 並發足夠抓 race
				pm.Reset()
				resetCount.Add(1)
			}
		}()
	}

	// 1 個 SetUpdateBuffer 寫者:模擬 config reload 期間調整節流
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ { //nolint:mnd
			pm.SetUpdateBuffer(time.Duration(i) * time.Microsecond)
			bufferCount.Add(1)
		}
	}()

	wg.Wait()

	// 行為斷言:
	// 1) Reset / SetUpdateBuffer 真有跑完(沒被 panic 中斷)
	assert.Equal(t, int64(400), resetCount.Load(), "Reset 應跑滿 2 writers × 200 次") //nolint:mnd
	assert.Equal(t, int64(100), bufferCount.Load(), "SetUpdateBuffer 應跑滿 100 次")  //nolint:mnd

	// 2) emit 次數 > 0(節流關了,絕大多數 update 都應 emit;不要求 == numWriters
	//    × updatesPerWriter,因為 throttle / Reset 邊界仍會吃掉部分)
	emitCount := len(spy.snapshot())
	assert.Positive(t, emitCount, "至少要有部分 progress 被 emit 出去 — 全 0 代表 emit pipeline 整條斷掉")

	// 3) 行為一致性:不應該 emit 超過總投入量(防止 ProgressManager 內部
	//    意外重複 emit / 把同一筆 amplify)
	maxExpected := numWriters * updatesPerWriter
	assert.LessOrEqual(t, emitCount, maxExpected,
		"emit 次數不應超過總 UpdateProgress 投入量(%d)", maxExpected)
}

// TestProgressManager_ResetClearsThrottleState 釘住 fast second run:
// 一旦 caller 呼叫 Reset(),下一筆 UpdateProgress 必須 bypass throttle —
// 等同「新 analysis run」訊號。沒有 Reset 的話, long-lived ProgressManager
// 在兩次 run 之間繼承 lastUpdateAt,第二次 run 的首筆會被誤抑制。
func TestProgressManager_ResetClearsThrottleState(t *testing.T) {
	spy := &fakeEmitter{}
	pm := newTestProgressManager(spy.emit)
	pm.SetUpdateBuffer(10 * time.Second)

	// 第一次 run:emit 兩筆,第二筆(50%)按 buffer 被節流
	pm.UpdateProgress(models.ProgressInfo{CurrentStep: 1, Percentage: 10, Status: "run1 start"})
	pm.UpdateProgress(models.ProgressInfo{CurrentStep: 5, Percentage: 50, Status: "run1 mid"})

	// 模擬第二次 run 之前明確 Reset
	pm.Reset()

	// 第二次 run 的首筆(50%): 既非 CurrentStep=0 也非 100%,
	// 必須仰賴 Reset 後的 zero lastUpdateAt 才能 bypass
	pm.UpdateProgress(models.ProgressInfo{CurrentStep: 5, Percentage: 50, Status: "run2 start"})

	sent := spy.snapshot()
	// 預期至少 2 筆:第一筆(initial bypass) + 第二次 run 首筆(reset bypass)
	// run1 mid 在 buffer 內會被節流,屬於正常行為
	require.GreaterOrEqual(t, len(sent), 2,
		"Reset() 後的首筆必須 bypass throttle (observed %d events)", len(sent))
	// 最後一筆必須是 "run2 start"
	assert.Equal(t, "run2 start", sent[len(sent)-1].Status,
		"Reset 後的 UpdateProgress 應出現在 emit 串末")
}
