package gui

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"count_mean/internal/logging"
	"count_mean/internal/models"
)

// Progress event constants.
const (
	// ProgressEventName 是 Wails frontend 訂閱進度更新時必須使用的事件名。
	// 前端 code: `EventsOn("progress", handler)`；後端只在這裡定義一次,避免散落。
	ProgressEventName         = "progress"
	defaultUpdateInterval     = 100 * time.Millisecond // 默認更新節流間隔
	progressCompletionPercent = 100                    // 完成百分比
)

// wailsEventsCtxKey 是 Wails runtime 在 Startup 把 frontend.Events 注入 ctx
// 時使用的 key。詳見 wails/v2@v2.12.0 internal/app/app_production.go:77 與
// pkg/runtime/runtime.go:53 — Wails 用裸 string literal `"events"`,我們在
// 此處用同一個 string 字面值做 ctx 就緒探測。
//
// 為何不用 typed key:Wails framework 本身用 string literal,我們若用自己
// 的 typed key 會與 Wails 的 string key collision-free,結果就是「永遠拿不
// 到 Wails 注入的 value」,等同永久 short-circuit。typed key 只在我們自己
// 注入時有意義(test 模擬 ctx 就緒)。
//
// staticcheck SA1029(避免 built-in 型別作 ctx key)在此處是 false positive —
// 我們不是「定義」這個 key,而是「讀取」Wails 已定義的 string key。針對
// progressCtxReadyKey 等我們「自己注入」的 key,則用 typed struct{} 規避。
const wailsEventsCtxKey = "events"

// progressCtxReadyKey 是我們自己定義的 typed ctx key,給單元/整合測試在不
// 起 Wails runtime 的情況下標註「ctx 就緒,可以 emit」。production code 不
// 注入此 key — wailsProgressEmitter 只在 ctx 帶 wailsEventsCtxKey 或本 key
// 任一者時才 emit。
//
// struct{} 零大小 + 不可被外部偽造(unexported)。
type progressCtxReadyKey struct{}

// progressEmitter 抽象出「把 ProgressInfo 推到 frontend」的依賴,讓 ProgressManager
// 不直接綁死在 wailsruntime.EventsEmit — 既能避免 runtime.EventsEmit 在 ctx
// 為 nil 或非 Wails ctx 時 log.Fatalf 整個 process,也讓 unit test 能注入 fake
// emitter 驗證行為。
//
// production 使用的 emitter 由 NewProgressManager 注入,內部呼叫
// runtime.EventsEmit(ctx, ProgressEventName, info)。test code 可注入 spy。
//
// ProgressInfo by-value 是設計:介面強制 by-value(ProgressCallback),
// 傳給 runtime.EventsEmit 也是 by-value(interface{} 裝箱),這裡收 value
// 比再多一層 pointer 更直白。
type progressEmitter func(info models.ProgressInfo) //nolint:gocritic // hugeParam: 介面強制 by-value

// ProgressManager 管理進度報告;以 Wails Events 推播模型把每筆 ProgressInfo
// 即時推給 frontend(EventsOn("progress", ...)),取代舊的 polling + subscriber
// channel 設計。
//
// 並行契約:UpdateProgress 會被 calculator 從 collector goroutine 同步呼叫
// (見 models.ProgressTracker doc),但設定 callback 的路徑(SetProgressCallback
// at applyConfig)可能與 UpdateProgress 並行。lastUpdateAt / emitter 由 mutex 保護。
type ProgressManager struct {
	logger       *logging.Logger
	mutex        sync.Mutex
	emit         progressEmitter
	lastUpdateAt time.Time
	updateBuffer time.Duration
}

// NewProgressManager 創建以 Wails Events 推播的進度管理器。
//
// ctxFn 必須回傳 Wails Startup 注入的 lifecycle context(見 App.context()),
// 提供 closure 而非直接傳 ctx 是因為 NewApp 在 App.Startup 之前就被呼叫,
// 那時 App.ctx 還是 nil — closure 讓 emit 在實際被觸發時(分析開始後)才解析 ctx。
//
// 若 ctxFn() 回傳的 ctx 不是 Wails 注入的 ctx(例如 unit test 直接 NewApp
// 不走 Startup,a.ctx 仍為 nil → fallback 到 context.Background()),
// wailsProgressEmitter 內部會短路,跳過 runtime.EventsEmit 以免 log.Fatalf。
func NewProgressManager(ctxFn func() context.Context) *ProgressManager {
	emit := wailsProgressEmitter(ctxFn)

	return &ProgressManager{
		logger:       logging.GetLogger("progress_manager"),
		emit:         emit,
		updateBuffer: defaultUpdateInterval,
	}
}

// wailsProgressEmitter 回傳一個用 runtime.EventsEmit 推播 ProgressInfo 的 emitter。
//
// 安全性:runtime.EventsEmit 對非 Wails ctx 會 log.Fatalf,所以我們先驗證
// ctx 是否帶 Wails 注入的 events handler(透過 hasWailsEventsHandler),沒帶
// 則 silently skip 並 debug log — 預期只在 unit test 或 Wails Startup 前的
// 極早期 race 出現。
//
// 在 closure 內加 defer recover() 把任何來自 runtime.EventsEmit / ctxFn 的
// panic 吸收掉。過去這條 path 沒 recover,frontend handler / Wails dispatch 鏈
// 任一處 panic 會 propagate 到 calculator collector goroutine(透過
// ProgressCallback 同步呼叫),把整條 worker 打死 → 使用者看到分析卡死。
// 這條 deferred recover 把 panic 降級為 Debug log;丟掉這筆 progress 事件不
// 致命(throttle 機制下下一筆會補,或 final 100% 必能補)。
func wailsProgressEmitter(ctxFn func() context.Context) progressEmitter {
	log := logging.GetLogger("progress_emitter")

	return func(info models.ProgressInfo) {
		// 把任何 panic(來自 ctxFn / runtime.EventsEmit / Wails 內部
		// dispatch / frontend.Events handler)轉成 Debug log,絕對不向 caller
		// (calculator collector goroutine)propagate。失去這筆 progress 不
		// 致命(後續 frame 會補)。
		defer func() {
			if r := recover(); r != nil {
				log.Error("EventsEmit panicked",
					//nolint:err113 // dynamic recovered value
					fmt.Errorf("%v", r),
					map[string]any{
						"percentage": info.Percentage,
						"status":     info.Status,
					})
			}
		}()

		ctx := ctxFn()
		if !hasWailsEventsHandler(ctx) {
			log.Debug("跳過進度事件:Wails ctx 尚未就緒", map[string]any{
				"percentage": info.Percentage,
				"status":     info.Status,
			})

			return
		}

		runtime.EventsEmit(ctx, ProgressEventName, info)
	}
}

// hasWailsEventsHandler 檢查 ctx 是否帶 Wails framework 注入的 frontend.Events
// handler。回傳 true 表示可以安全地呼叫 runtime.EventsEmit 而不會被 Wails
// 內部 log.Fatalf 擊潰(runtime.EventsEmit 會 type-assert "events" 為
// frontend.Events,nil 即觸發 Fatalf)。
//
// 為何不把 wailsEventsCtxKey 改成 typed key:Wails framework 用 string literal
// `"events"` 來 WithValue / Value,任何不同型別的 ctx key 都會 collision-free
// 拿不到 value(等於 production 永遠 short-circuit)。staticcheck SA1029 對此
// 情境是 false positive — 我們不是「定義」一個帶 string key 的 value,而是
// 「探測 Wails 已定義的 string key」是否存在。
//
// 為何用獨立 function 包裝:
//   - 把 //nolint:staticcheck 範圍縮到最小(只此一行)
//   - 給 unit test 可注入 fake events value 並驗證 readiness check 邏輯
//   - 留 hook point 若未來 Wails minor 升級改 key,只需改這裡一處
func hasWailsEventsHandler(ctx context.Context) bool {
	if ctx == nil {
		return false
	}

	v := ctx.Value(wailsEventsCtxKey) //nolint:staticcheck // SA1029: Wails internal contract uses string key; see wailsEventsCtxKey doc
	if v == nil {
		return false
	}

	// 嚴格 type-assert — 不能只 nil-check interface,因為 caller 可能塞
	// typed nil pointer(*frontend.Events(nil) / *string(nil) 等),表面 != nil
	// 但底層是 nil。runtime.EventsEmit 對 typed nil 做 type-assert 會 panic
	// (或 Wails 內部 log.Fatalf)。用 reflect 抓底層 nil 是最低成本守門:
	// 任何 pointer / chan / func / interface / map / slice 的 typed nil
	// 都會被 IsNil() 識別並 reject。其他 type (struct / string 等)IsNil 會
	// panic,所以 wrap kind check。
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Slice:
		if rv.IsNil() {
			return false
		}
	default:
		// 非 pointer-like kind (string / struct / int 等)— IsNil 會 panic,
		// 走原本 != nil 已足夠;production Wails 注入的 frontend.Events 是
		// pointer,落在上面 case,真正 nil-typed 攻擊面被擋住。
	}
	return true
}

// isProgressCtxReady 是 hasWailsEventsHandler 的 superset:除了 Wails native
// events handler 之外也認 progressCtxReadyKey{} typed key — 後者只在 test
// 環境注入,供 unit/integration test 在不起 Wails runtime 的前提下標註
// 「ctx 已就緒」。production code path 走 hasWailsEventsHandler 即可。
//
// 為何分兩個 helper:hasWailsEventsHandler 用於 wailsProgressEmitter(真調
// runtime.EventsEmit,只有 Wails 原生 ctx 能通過);isProgressCtxReady 用於
// test helper 與 future 可能的 mock emit pipeline。兩條路徑分離避免 test-only
// signal 誤觸發 runtime.EventsEmit 撞 log.Fatalf。
func isProgressCtxReady(ctx context.Context) bool {
	if ctx == nil {
		return false
	}

	if hasWailsEventsHandler(ctx) {
		return true
	}

	return ctx.Value(progressCtxReadyKey{}) != nil
}

// SetUpdateBuffer 設置更新間隔節流(避免高頻 progress 把 IPC 打爆)。
func (pm *ProgressManager) SetUpdateBuffer(duration time.Duration) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	pm.updateBuffer = duration
}

// UpdateProgress 更新進度(實作 models.ProgressCallback 介面)。
//
// Throttle bypass 規則(補強):
//   - 100%(完成)永遠 emit,確保前端能收到「完成」訊號。
//   - 初始 frame(lastUpdateAt 是 zero value,代表本 ProgressManager 尚未
//     emit 過)永遠 emit。fast second run 場景:同一個 ProgressManager
//     在連續兩次分析間若繼承前次 lastUpdateAt 會把第二次的首筆 0% 抑制,
//     使前端誤以為「沒在跑」。caller 在 Reset() 把 lastUpdateAt 歸零後,
//     第一筆便自動 bypass throttle。
//   - 顯式 step=0(從 Start 觸發的初始事件)永遠 emit;讓 GUI 能可靠收到
//     analysis 啟動訊號。
//   - 其餘事件按 updateBuffer 節流以保護 Wails IPC。
//
// ProgressInfo by-value:由 calculator 透過 ProgressCallback 同步呼叫,
// 介面強制 by-value;不引入 pointer indirection 維持與舊 API 相容。
//
//nolint:gocritic // hugeParam: ProgressCallback 介面強制 by-value
func (pm *ProgressManager) UpdateProgress(info models.ProgressInfo) {
	pm.mutex.Lock()
	now := time.Now()

	isFinal := info.Percentage >= progressCompletionPercent
	isInitial := pm.lastUpdateAt.IsZero() || info.CurrentStep == 0
	if !isFinal && !isInitial &&
		pm.lastUpdateAt.Add(pm.updateBuffer).After(now) {
		pm.mutex.Unlock()

		return
	}

	pm.lastUpdateAt = now
	emit := pm.emit
	pm.mutex.Unlock()

	if emit == nil {
		return
	}

	emit(info)
	pm.logger.Debug("進度事件已發送", map[string]any{
		"percentage":    info.Percentage,
		"status":        info.Status,
		"channel_index": info.ChannelIndex,
	})
}

// Reset 把 ProgressManager 的 throttle state 歸零(lastUpdateAt → zero value),
// 讓「下一次 analysis 的首筆 progress」必能 bypass throttle。
//
// 呼叫時機: caller 在每次開新 analysis run 之前。沒有 Reset 的話,
// 若兩次 analysis 在 < updateBuffer 時間內接續觸發,第二次 run 的首筆 0%
// 會被前次 run 的 lastUpdateAt 抑制掉,前端會收不到「新 run 已啟動」的訊號。
//
// 注意:Reset 只重設 throttle state,不影響 emit / mutex / updateBuffer。
func (pm *ProgressManager) Reset() {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()
	pm.lastUpdateAt = time.Time{}
}

// CreateProgressCallback 創建可傳給 calculator.SetProgressCallback 的回調。
func (pm *ProgressManager) CreateProgressCallback() models.ProgressCallback {
	return pm.UpdateProgress
}
