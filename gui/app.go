// Package gui provides the graphical user interface for the EMG data analysis application.
// It implements the Wails v2 desktop application with support for file operations,
// data processing, and chart generation.
package gui

import (
	"context"
	"fmt"
	"sync/atomic"

	"count_mean/internal/calculator"
	"count_mean/internal/cci"
	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"count_mean/internal/muscle_ratio"
	"count_mean/internal/phase_sync"
)

// appState 把受 config 影響的 5 個 dependency + config 打包成 atomic snapshot。
// applyConfig 用 atomic.Pointer.Store 一次性 swap,讓 Wails 並行 RPC 場景下
// in-flight 分析 method 看到的永遠是一致的 snapshot,不會出現「半新半舊」
// mixed state。
type appState struct {
	config        *config.AppConfig
	csvHandler    *io.CSVHandler
	maxMeanCalc   *calculator.MaxMeanCalculator
	normalizer    *calculator.Normalizer
	phaseAnalyzer *calculator.PhaseAnalyzer
}

// App struct.
//
// ctx 用 atomic.Pointer[context.Context]:Wails Startup 從 main goroutine 寫入,
// 但 RPC method 會被 Wails runtime 從多個 goroutine 並發讀;裸 context.Context
// 欄位讀寫無同步,race detector 會抓到 DATA RACE。atomic.Pointer 比 RWMutex
// 更精確表達「one-shot write, frequent lock-free read」這個 ctx lifecycle 模式,
// happens-before 符合 Go memory model;Wails 在 Startup 後不會再改 ctx。
//
// configPath 由 constructor 注入,SaveConfig 用此路徑寫,與 main.go
// loadStartupConfig 讀的路徑對稱;避免 macOS bundle 啟動時寫不到的 silent bug。
type App struct {
	ctx                 atomic.Pointer[context.Context] //nolint:containedctx // Required by Wails framework
	state               atomic.Pointer[appState]        // 取代原 5 個 mutable 欄位,保證 swap 原子性
	logger              *logging.Logger
	phaseSyncAnalyzer   *phase_sync.PhaseSyncAnalyzer
	cciAnalyzer         *cci.CCIAnalyzer
	muscleRatioAnalyzer *muscle_ratio.Analyzer
	progressManager     *ProgressManager
	version             string
	configPath          string // SaveConfig 寫入路徑;與啟動讀路徑對稱 (read/write symmetry)。
}

// loadCtx 安全地讀取 a.ctx — Wails Startup 前回傳 nil,Startup 後回傳 lifecycle ctx。
// 用 helper 包裝可避免散落各處的 .Load() + 解 pointer 重複 boilerplate。
func (a *App) loadCtx() context.Context {
	if p := a.ctx.Load(); p != nil {
		return *p
	}

	return nil
}

// buildAppState 把 cfg 與 5 個受 config 影響的 dependency 打包成 immutable snapshot。
// progressCallback 不在這裡 wire,由 caller (NewApp / applyConfig) 在 Store 之前
// 套到 newState.maxMeanCalc — 因為 callback 來源於 a.progressManager。
func buildAppState(cfg *config.AppConfig) *appState {
	return &appState{
		config:        cfg,
		csvHandler:    io.NewCSVHandler(cfg),
		maxMeanCalc:   calculator.NewMaxMeanCalculator(cfg.ScalingFactor),
		normalizer:    calculator.NewNormalizer(cfg.ScalingFactor),
		phaseAnalyzer: calculator.NewPhaseAnalyzer(cfg.ScalingFactor, cfg.PhaseLabels),
	}
}

// NewApp creates a new App application struct.
//
// progressManager 注入閉包 `a.context` 作為 ctxFn — Wails Startup 前 a.ctx
// 為 nil,emitter 內部會 short-circuit 跳過 EventsEmit;Startup 完成後
// a.ctx 帶 Wails 注入的 "events" value,事件即推給前端。
//
// configPath 走 config.ResolveDefaultConfigPath() 作為 default,讓 production
// 路徑與 main.go::runGUI 注入的讀取路徑保持對稱;test 想注入自訂 path 改用
// NewAppWithConfigPath。
func NewApp(cfg *config.AppConfig, version string) *App {
	return NewAppWithConfigPath(cfg, version, config.ResolveDefaultConfigPath())
}

// NewAppWithConfigPath 是顯式注入 configPath 的 constructor,供 test / main
// 在已知 config 路徑時直接傳入。Production caller 大多走 NewApp(內部呼叫
// ResolveDefaultConfigPath)即可。
//
// 不對外 expose configPath setter — 一旦 App 啟動,read/write 路徑必須凍結;
// 改 configPath 應該透過重建 App 而非 mutate 既有 instance。
func NewAppWithConfigPath(cfg *config.AppConfig, version, configPath string) *App {
	a := &App{
		logger:              logging.GetLogger("app"),
		phaseSyncAnalyzer:   phase_sync.NewPhaseSyncAnalyzer(),
		cciAnalyzer:         cci.NewCCIAnalyzer(),
		muscleRatioAnalyzer: muscle_ratio.NewAnalyzer(),
		version:             version,
		configPath:          configPath,
	}
	a.progressManager = NewProgressManager(a.context)

	initialState := buildAppState(cfg)
	initialState.maxMeanCalc.SetProgressCallback(a.progressManager.CreateProgressCallback())
	a.state.Store(initialState)

	return a
}

// Startup is called when the app starts. The context is saved via
// atomic.Pointer.Store so並發 RPC method 透過 loadCtx 讀到一致 snapshot。
func (a *App) Startup(ctx context.Context) {
	defer recoverHandlerPanicVoid("Startup", a.logger)

	a.ctx.Store(&ctx)
	a.logger.Info("Wails 應用程序啟動")

	// 確保必要的目錄存在
	s := a.state.Load()
	if err := s.config.EnsureDirectories(); err != nil {
		a.logger.Error("無法創建必要目錄", err)
	}
}

// Shutdown is wired to Wails' OnShutdown hook (also triggered by SIGINT/SIGTERM
// via Wails' internal signal manager). Logs one Info line so 使用者能在 log 看到
// graceful shutdown 訊號(對比 abrupt SIGKILL)。
// ProgressManager 改為 Wails Events 推播後不再持有 goroutine,無需 Stop。
func (a *App) Shutdown(_ context.Context) {
	defer recoverHandlerPanicVoid("Shutdown", a.logger)

	a.logger.Info("Wails 應用程序關閉中")
}

// context returns the Wails-supplied lifecycle context (set in Startup).
// Falls back to context.Background() when Startup hasn't been called yet —
// typically in unit tests. Production callers always have a.ctx non-nil because
// Wails invokes Startup before any user-facing method.
//
// 用此 helper 取代各 entry method 散落的 context.Background(),讓
// calculateWithTimeRange 等接 ctx 的 long-running 計算能真的被 Wails Shutdown
// 取消(否則 cancellation chain 等於「實作了沒接線」)。
//
// 讀 ctx 走 atomic.Pointer.Load,Wails runtime 並發 dispatch 時不會與 Startup race。
func (a *App) context() context.Context {
	if ctx := a.loadCtx(); ctx != nil {
		return ctx
	}

	return context.Background()
}

// GetConfig returns the current configuration.
// defer recoverHandlerPanicValue 防 a.state 未初始化導致 nil deref panic
// 擊潰整個 Wails desktop process(getter 無 error return)。
func (a *App) GetConfig() (cfg *config.AppConfig) {
	defer recoverHandlerPanicValue("GetConfig", a.logger, &cfg)

	s := a.state.Load()
	return s.config
}

// SaveConfig saves the configuration.
//
// 必須一併重建 csvHandler / maxMeanCalc / normalizer / phaseAnalyzer,否則它們
// 仍持有舊 cfg(ScalingFactor、Precision、OutputDir、PhaseLabels),改設定後
// 計算與輸出仍走舊值 — 屬 user-visible silent bug。
//
// 寫檔前先呼叫 cfg.Validate():invalid locale("fr-FR")等不合法值會被寫入後,
// 下次 LoadConfig 又被 Validate 拒絕並回退 default — 使用者實際感受是
// 「修改 → 重啟 → 設定不見」+ 無錯誤回饋。
//
// 寫入走 SaveConfigAtomic:含 parent dir MkdirAll + tmp+rename atomic commit,
// 一併解決 macOS .app bundle 啟動 CWD=/ 寫不到、parent dir ENOENT、中途 crash
// 留下 partial JSON 三個合併坑。
func (a *App) SaveConfig(cfg *config.AppConfig) (err error) {
	defer recoverHandlerPanic("SaveConfig", a.logger, &err)

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("設定驗證失敗: %w", err)
	}

	// configPath 在 constructor 注入。若為空字串(舊版 caller / test 走 zero-init
	// App struct),fallback 到 ResolveDefaultConfigPath 保持向後相容。
	writePath := a.configPath
	if writePath == "" {
		writePath = config.ResolveDefaultConfigPath()
	}

	if err := cfg.SaveConfigAtomic(writePath); err != nil {
		return fmt.Errorf("儲存設定檔失敗: %w", err)
	}

	a.applyConfig(cfg)

	// 同步 backend i18n locale,避免 ConfigPanel 改語言後後端 logging / error
	// message 仍走啟動時的 locale。前端 i18n 由 SetLanguage 獨立驅動。Validate
	// 已保證 Language 是 supported locale,這裡保留 != "" 檢查防 refactor 破壞。
	if cfg.Language != "" {
		i18n.SetLocale(i18n.Locale(cfg.Language))
	}

	return nil
}

// ResetConfig resets to default configuration. 重建相依元件以確保新 config
// 生效(同 SaveConfig 的理由),defer recoverHandlerPanicValue 防 panic 擊潰 process。
func (a *App) ResetConfig() (cfg *config.AppConfig) {
	defer recoverHandlerPanicValue("ResetConfig", a.logger, &cfg)

	cfg = config.DefaultConfig()
	a.applyConfig(cfg)

	// 與 SaveConfig 對稱:重設配置時同步 backend i18n locale。
	if cfg.Language != "" {
		i18n.SetLocale(i18n.Locale(cfg.Language))
	}

	return cfg
}

// GetTranslations returns a snapshot of all translations for the given locale.
// Used by frontend/src/i18n.js at startup / language change to load the full
// dictionary into an in-memory cache (avoid per-string RPC). Empty or
// unrecognised locales fall back to zh-TW via i18n.GetTranslationMap.
// defer recoverHandlerPanicValue 是 defense-in-depth — 未來 i18n 若新增
// loading/parsing 邏輯導致 panic,不會擊潰整個 Wails desktop process。
func (a *App) GetTranslations(locale string) (m map[string]string) {
	defer recoverHandlerPanicValue("GetTranslations", a.logger, &m)

	return i18n.GetTranslationMap(i18n.Locale(locale))
}

// SetLanguage switches the backend i18n locale without persisting to config.json.
// Called by frontend when the user changes language dropdown so後續 backend
// error / log / dialog messages 立即用新 locale。Persistence 由 SaveConfig 處理 —
// caller 決定變動是 "preview" (SetLanguage only) 或 "permanent" (also SaveConfig)。
func (a *App) SetLanguage(locale string) (err error) {
	defer recoverHandlerPanic("SetLanguage", a.logger, &err)

	if locale == "" {
		return ErrLocaleEmpty
	}

	supported := i18n.GetSupportedLocales()
	for _, l := range supported {
		if string(l) == locale {
			i18n.SetLocale(l)

			return nil
		}
	}

	return fmt.Errorf("%w: %q (支援:%v)", ErrLocaleUnsupported, locale, supported)
}

// applyConfig 用 atomic.Pointer.Store 一次性 swap 整個 appState snapshot。
// Wails 並行 RPC 場景下 in-flight 分析 method 看到的 snapshot 永遠一致 —
// SaveConfig 期間正在跑的分析仍用舊 snapshot,新分析才看到新 snapshot。
//
// 刻意**不**呼叫 a.progressManager.Reset():Reset 會把 lastUpdateAt 歸零(in-flight
// throttle state);若 SaveConfig 在分析進行中被觸發(Wails RPC 並行),wipe
// in-flight throttle 會讓正在跑的 calc 下一筆 progress 繞過 throttle flood IPC,
// 並抹掉中途的 emit 節奏。applyConfig 屬「user-configurable knob 切換」,不該
// 擾動瞬態 throttle state。
//
// Fast-second-run 場景由 ProgressManager 的 step=0 bypass 與 isInitial
// (lastUpdateAt zero) 兩條 rule 共同涵蓋;若未來 calculator 改成 step≥1
// first frame,應由 calc 在 Start() 邊界主動 Reset,而非 piggyback 在 applyConfig。
func (a *App) applyConfig(cfg *config.AppConfig) {
	newState := buildAppState(cfg)
	newState.maxMeanCalc.SetProgressCallback(a.progressManager.CreateProgressCallback())
	a.state.Store(newState)
}

// GetVersion returns the application version string.
// defer recoverHandlerPanicValue 統一模式,避免未來新增邏輯時忘了加 panic safety net。
func (a *App) GetVersion() (out string) {
	defer recoverHandlerPanicValue("GetVersion", a.logger, &out)

	return a.version
}

// ChartResult holds the result of chart generation.
type ChartResult struct {
	OutputPath  string `json:"outputPath"`
	HTMLContent string `json:"htmlContent"`
	Success     bool   `json:"success"`
	Message     string `json:"message"`
}
