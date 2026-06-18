// Package gui provides the graphical user interface for the EMG data analysis application.
// It implements the Wails v2 desktop application with support for file operations,
// data processing, and chart generation.
package gui

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"count_mean/internal/calculator"
	"count_mean/internal/cci"
	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"count_mean/internal/maxmean"
	"count_mean/internal/models"
	"count_mean/internal/muscle_ratio"
	"count_mean/internal/phase_sync"
	"count_mean/internal/security/redact"
	"count_mean/internal/synchronizer"
	"count_mean/internal/validation/filename"
)

// Sentinel errors for validation.
var (
	ErrNoMainFile         = errors.New("請選擇主要資料檔案")
	ErrNoReferenceFile    = errors.New("請選擇參考資料檔案")
	ErrNoInputFile        = errors.New("請選擇資料檔案")
	ErrNoPhaseLabels      = errors.New("請輸入階段標籤")
	ErrNoValidPhaseLabels = errors.New("請輸入有效的階段標籤")
	ErrNoCSVHeaders       = errors.New("CSV 檔案沒有標題行")
	ErrNoManifestFile     = errors.New("請選擇分期總檔案")
	ErrNoDataFolder       = errors.New("請選擇數據資料夾")
	ErrNoPhaseSelection   = errors.New("請選擇開始和結束分期點")
	ErrLocaleEmpty        = errors.New("locale 不可為空字串")
	ErrLocaleUnsupported  = errors.New("不支援的 locale")
	ErrInvalidPhasePoint  = errors.New("無效的分期點代碼")
	ErrInvalidPhaseRange  = errors.New("分期時間區間不合法（起始須小於結束且為有限值）")
	ErrNoCSVFilesInFolder = maxmean.ErrNoCSVFilesInFolder
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
	filenameValidator   *filename.Validator
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
		filenameValidator:   filename.NewValidator(),
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

// SelectFile opens a file dialog for file selection.
func (a *App) SelectFile(title string, filters []runtime.FileFilter, buttonType string) (path string, err error) {
	defer recoverHandlerPanic("SelectFile", a.logger, &err)

	cfg := a.state.Load().config

	var defaultDir string

	a.logger.Debug("選擇文件對話框", map[string]any{"buttonType": buttonType})

	switch buttonType {
	case FileTypeInput:
		defaultDir = cfg.InputDir
	case FileTypeOutput:
		defaultDir = cfg.OutputDir
	case FileTypeOperate:
		defaultDir = cfg.OperateDir
	}

	a.logger.Debug("預設目錄", map[string]any{"defaultDir": defaultDir})

	options := runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDir,
		Filters:          filters,
	}

	// Wails Startup 設 a.ctx 之前若 frontend 已啟動並呼叫 RPC 會 nil deref;實務
	// 上極少發生(Wails 在 ctx ready 後才暴露 binding),仍加防線回 graceful error。
	ctx := a.loadCtx()
	if ctx == nil {
		return "", ErrAppNotReady
	}

	file, err := runtime.OpenFileDialog(ctx, options)
	if err != nil {
		return "", fmt.Errorf("開啟檔案對話框失敗: %w", err)
	}

	return file, nil
}

// SelectDirectory opens a directory dialog.
func (a *App) SelectDirectory(title string) (path string, err error) {
	defer recoverHandlerPanic("SelectDirectory", a.logger, &err)

	s := a.state.Load()
	options := runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: s.config.InputDir,
	}

	ctx := a.loadCtx()
	if ctx == nil {
		return "", ErrAppNotReady
	}

	dir, err := runtime.OpenDirectoryDialog(ctx, options)
	if err != nil {
		return "", fmt.Errorf("開啟目錄對話框失敗: %w", err)
	}

	return dir, nil
}

// CalculateMaxMean calculates maximum mean values.
func (a *App) CalculateMaxMean(params MaxMeanParams) (result *MaxMeanResult, err error) {
	defer recoverHandlerPanic("CalculateMaxMean", a.logger, &err)

	a.logger.Info("開始最大平均值計算", map[string]any{
		"input_path":  params.InputPath,
		"window_size": params.WindowSize,
		"is_batch":    params.IsBatch,
	})

	a.logger.Debug("計算參數", map[string]any{"params": params})

	// 批次處理模式
	if params.IsBatch {
		return a.calculateMaxMeanBatch(params)
	}

	// 單檔案處理模式
	return a.calculateMaxMeanSingle(params)
}

// calculateMaxMeanSingle 處理單個檔案.
//
// snapshot pattern: entry 在 line 開頭一次性取得 *appState,後續所有 helper 透過
// 顯式參數共享同一份 snapshot,杜絕 SaveConfig 在分析過程中換 cfg 造成的撕裂。
func (a *App) calculateMaxMeanSingle(params MaxMeanParams) (*MaxMeanResult, error) {
	s := a.state.Load()

	// 使用統一的 CSV 讀取方法（包含路徑驗證）
	records, err := a.readCSVWithPathValidation(s, params.InputPath, s.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 取得檔案名稱（不含路徑和副檔名）
	originalFileName := TrimCSVExtension(filepath.Base(params.InputPath))

	// 解析時間範圍並計算
	startRange, endRange := calculator.ResolveTimeRange(records, params.StartTime, params.EndTime)

	results, err := a.calculateWithTimeRange(a.context(), s, records, params.WindowSize, startRange, endRange)
	if err != nil {
		return nil, fmt.Errorf("計算失敗: %w", err)
	}

	// 輸出結果
	outputFile := buildOutputFilename(originalFileName, SuffixMaxMean)

	outputPath, writeErr := s.csvHandler.WriteMaxMean(
		io.WriteRequest{Filename: outputFile},
		records[0], results, startRange, endRange,
	)
	if writeErr != nil {
		return nil, fmt.Errorf("寫入輸出檔案失敗: %w", writeErr)
	}

	// 準備回傳結果。Success/Message 必須顯式設定 — 前端依 result.success 判定成敗,
	// 漏設會讓 bool 零值 false 使成功計算被誤判為失敗(對齊批次 RunBatch 與
	// NormalizeData 的 envelope)。
	return &MaxMeanResult{
		OutputPath: outputPath,
		Headers:    records[0],
		Results:    convertMaxMeanResultsToArray(results, s.config.ScalingFactor),
		Success:    true,
		Message:    fmt.Sprintf("最大平均值計算成功完成，結果已保存到: %s", outputPath),
	}, nil
}

// NormalizeData performs data normalization.
func (a *App) NormalizeData(params NormalizeParams) (result *NormalizeResult, err error) {
	defer recoverHandlerPanic("NormalizeData", a.logger, &err)

	s := a.state.Load()

	a.logger.Info("開始資料標準化", map[string]any{
		"main_file":      params.MainFile,
		"reference_file": params.ReferenceFile,
		"output_path":    params.OutputPath,
	})

	// 驗證輸入
	if params.MainFile == "" {
		return nil, ErrNoMainFile
	}

	if params.ReferenceFile == "" {
		return nil, ErrNoReferenceFile
	}

	// 讀取主要資料檔案（包含路徑驗證）
	mainRecords, err := a.readCSVWithPathValidation(s, params.MainFile, s.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取主要資料檔案失敗: %w", err)
	}

	// 讀取參考資料檔案（包含路徑驗證）
	refRecords, err := a.readCSVWithPathValidation(s, params.ReferenceFile, s.config.OperateDir)
	if err != nil {
		return nil, fmt.Errorf("讀取參考資料檔案失敗: %w", err)
	}

	// 執行標準化
	normalizedData, err := s.normalizer.NormalizeFromRawData(mainRecords, refRecords)
	if err != nil {
		return nil, fmt.Errorf("標準化計算失敗: %w", err)
	}

	// 生成輸出檔名並保存結果
	mainBaseName := TrimCSVExtension(filepath.Base(params.MainFile))
	outputName := resolveOutputName(params.OutputPath, mainBaseName, SuffixNormalized)

	outputPath, err := s.csvHandler.WriteNormalized(
		io.WriteRequest{Filename: outputName}, normalizedData,
	)
	if err != nil {
		return nil, fmt.Errorf("保存結果失敗: %w", err)
	}
	data := convertNormalizedDataToArray(normalizedData)

	a.logger.Info("資料標準化完成", map[string]any{
		"output_file":   outputPath,
		"data_points":   len(data),
		"channel_count": len(normalizedData.Headers) - 1,
	})

	return &NormalizeResult{
		OutputPath: outputPath,
		Headers:    normalizedData.Headers,
		Data:       data,
		Success:    true,
		Message:    "資料標準化成功完成",
	}, nil
}

// validatePhaseParams validates phase analysis parameters and returns the phase
// labels and time ranges parsed from params.Phases. 名稱與邊界皆來自前端傳入,
// 不再耦合 config.PhaseLabels。
func validatePhaseParams(params PhaseParams) (labels []string, ranges []models.TimeRange, err error) {
	if params.InputFile == "" {
		return nil, nil, ErrNoInputFile
	}

	if len(params.Phases) == 0 {
		return nil, nil, ErrNoPhaseLabels
	}

	labels = make([]string, 0, len(params.Phases))
	ranges = make([]models.TimeRange, 0, len(params.Phases))

	for _, ph := range params.Phases {
		name := strings.TrimSpace(ph.Name)
		if name == "" {
			return nil, nil, ErrNoValidPhaseLabels
		}

		// 邊界以原始字串收,用 strconv.ParseFloat 嚴格解析整串:拒空字串 / 部分解析
		// (前端 parseFloat 會把 "1.2foo" 截成 1.2、"0x10" 截成 0,這類截斷誤輸入若以
		// 數字傳入會被當合法值)/ 非數值。
		start, errStart := strconv.ParseFloat(strings.TrimSpace(ph.StartTime), 64)
		end, errEnd := strconv.ParseFloat(strings.TrimSpace(ph.EndTime), 64)
		if errStart != nil || errEnd != nil {
			return nil, nil, ErrInvalidPhaseRange
		}

		// 邊界須有限且 start < end:`!(start < end)` 同時擋 NaN(NaN 的任何比較皆為
		// false)與反序 / 零長度;另顯式擋 ±Inf —— ParseFloat 接受 "Inf",且
		// `-Inf < +Inf` 為 true 會繞過上式。
		if math.IsInf(start, 0) || math.IsInf(end, 0) || !(start < end) {
			return nil, nil, ErrInvalidPhaseRange
		}

		labels = append(labels, name)
		ranges = append(ranges, models.TimeRange{Start: start, End: end})
	}

	return labels, ranges, nil
}

// generatePhaseOutputName generates the output filename for phase analysis.
func generatePhaseOutputName(inputFile, outputPath string) string {
	baseName := TrimCSVExtension(filepath.Base(inputFile))
	return resolveOutputName(outputPath, baseName, SuffixPhaseAnalysis)
}

// convertPhaseResultToAnalysis converts a PhaseAnalysisResult to PhaseAnalysis.
func convertPhaseResultToAnalysis(phaseResult *models.PhaseAnalysisResult, channelCount int) PhaseAnalysis {
	maxValues := make([]float64, channelCount)
	meanValues := make([]float64, channelCount)

	// MaxValues/MeanValues 的 key 是 0-based channel index(key 0 = Ch1,見
	// calculator.computePhaseStatistics)。直接以 colIdx 對應輸出槽;先前的
	// colIdx-1 會丟掉 Ch1 並整體位移一格。
	for colIdx, val := range phaseResult.MaxValues {
		if colIdx >= 0 && colIdx < len(maxValues) {
			maxValues[colIdx] = val
		}
	}

	for colIdx, val := range phaseResult.MeanValues {
		if colIdx >= 0 && colIdx < len(meanValues) {
			meanValues[colIdx] = val
		}
	}

	return PhaseAnalysis{
		PhaseLabel: phaseResult.PhaseName,
		StartTime:  0,
		EndTime:    0,
		Duration:   0,
		Average:    meanValues,
		MaxValues:  maxValues,
		MinValues:  []float64{},
	}
}

// phaseRunData 是 AnalyzePhases 的 [[AnalysisHandler[P, R]]] R 參數:把 Execute
// 步驟產出的 records + analysis result 一起透出給 caller 端做 envelope 組裝。
// records 之所以要透出,是因為 headers (records[0]) 與 channelCount 都從 records
// 推算,WriteCSV 簽章只回 outputPath、不回這兩個衍生資料。
type phaseRunData struct {
	records        [][]string
	analysisResult *calculator.AnalyzeResult
}

// AnalyzePhases performs phase analysis.
//
// 走 [[AnalysisHandler[P, R]]] 樣板:Validate / Execute / WriteCSV 三 closure 分別綁
// `validatePhaseParams`、`readCSVWithPathValidation + per-call analyzer.AnalyzeFromRawDataWithRanges`、
// `csvHandler.WritePhaseAnalysis`。panic safety + entry/exit log 由樣板透過
// [[HandlerRun]] 收乾;handler body 只剩 metadata log + result envelope 組裝。
//
// 錯誤通道契約 (`(result, err)` dual channel) 完全不變:Validate / Execute / WriteCSV
// 任一步失敗都透過 Run 回傳的 err 走 named return,result 為 nil。
func (a *App) AnalyzePhases(params PhaseParams) (result *PhaseResult, err error) {
	s := a.state.Load()

	a.logger.Info("階段分析參數", map[string]any{
		"input_file":  params.InputFile,
		"phase_count": len(params.Phases),
		"output_path": params.OutputPath,
	})

	// labels / ranges 跨 closure 共用:Validate 內由 validatePhaseParams 產出(名稱與
	// 時間區間皆來自前端 params.Phases),Execute 內以 labels 建 per-call analyzer、
	// ranges 作為顯式 phase 邊界,result envelope 組裝時用 len(labels) 作為 phase loop
	// guard。closure capture 是必要 trick — 樣板的 Validate 簽章 `func(P) error` 不允許
	// 多回傳值。
	var (
		labels []string
		ranges []models.TimeRange
	)

	handler := &AnalysisHandler[PhaseParams, phaseRunData]{
		Name:   "階段分析",
		Logger: a.logger,
		CSV:    s.csvHandler,
		Validate: func(p PhaseParams) error {
			ls, rs, validateErr := validatePhaseParams(p)
			if validateErr != nil {
				return validateErr
			}
			labels = ls
			ranges = rs
			return nil
		},
		Execute: func(ctx context.Context, p PhaseParams) (phaseRunData, error) {
			records, readErr := a.readCSVWithPathValidation(s, p.InputFile, s.config.InputDir)
			if readErr != nil {
				return phaseRunData{}, fmt.Errorf("讀取資料檔案失敗: %w", readErr)
			}

			// per-call analyzer:phase 名稱用前端 labels、邊界用前端 ranges
			// (AnalyzeFromRawDataWithRanges 不從字串解析時間點),與 config.PhaseLabels 解耦。
			analyzer := calculator.NewPhaseAnalyzer(s.config.ScalingFactor, labels)

			analysisResult, analyzeErr := analyzer.AnalyzeFromRawDataWithRanges(records, ranges)
			if analyzeErr != nil {
				return phaseRunData{}, fmt.Errorf("階段分析失敗: %w", analyzeErr)
			}

			return phaseRunData{records: records, analysisResult: analysisResult}, nil
		},
		WriteCSV: func(handler *io.CSVHandler, data phaseRunData) (string, error) {
			// 生成輸出檔名並寫入。Multi-phase merge 與 time-index dedup 由
			// CSVHandler.WritePhaseAnalysis 吸進 io 套件 — 此前 caller 需要自己
			// phaseRows[1:] skip header 與 fullRows[3:] dedup time row, 那層 row
			// layout leakage 已經消失。
			outputName := generatePhaseOutputName(params.InputFile, params.OutputPath)
			outputPath, writeErr := handler.WritePhaseAnalysis(
				io.WriteRequest{Filename: outputName}, data.records[0], data.analysisResult,
			)
			if writeErr != nil {
				return "", fmt.Errorf("保存結果失敗: %w", writeErr)
			}

			return outputPath, nil
		},
	}

	data, outputPath, err := handler.Run(a.context(), params)
	if err != nil {
		return nil, err
	}

	// 轉換分析結果
	channelCount := len(data.records[0]) - 1
	results := make([]PhaseAnalysis, 0, len(data.analysisResult.PhaseResults))

	for i, phaseResult := range data.analysisResult.PhaseResults {
		if i >= len(labels) {
			break
		}

		results = append(results, convertPhaseResultToAnalysis(&phaseResult, channelCount))
	}

	a.logger.Info("階段分析輸出", map[string]any{
		"output_file":   outputPath,
		"phase_count":   len(results),
		"channel_count": channelCount,
	})

	return &PhaseResult{
		OutputPath: outputPath,
		Headers:    data.records[0],
		Results:    results,
		Success:    true,
		Message:    fmt.Sprintf("階段分析成功完成，結果已保存到: %s", outputPath),
	}, nil
}

// ShowMessage displays an informational dialog. Fire-and-forget 無 error 回傳,
// panic 必須在 handler 內 swallow 否則 Wails runtime 整個 process 倒;ctx==nil
// 時直接跳過 MessageDialog(Wails runtime 會把 nil ctx 視為 fatal)。
func (a *App) ShowMessage(title, message string) {
	defer recoverHandlerPanicVoid("ShowMessage", a.logger)

	ctx := a.loadCtx()
	if ctx == nil {
		return
	}

	//nolint:errcheck,gosec // Return value intentionally ignored for fire-and-forget UI dialog
	runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: message,
	})
}

// ShowError displays an error dialog (same fire-and-forget pattern as ShowMessage).
func (a *App) ShowError(title, message string) {
	defer recoverHandlerPanicVoid("ShowError", a.logger)

	ctx := a.loadCtx()
	if ctx == nil {
		return
	}

	//nolint:errcheck,gosec // Return value intentionally ignored for fire-and-forget UI dialog
	runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
		Type:    runtime.ErrorDialog,
		Title:   title,
		Message: message,
	})
}

// CSVHeadersParams holds parameters for GetCSVHeaders.
type CSVHeadersParams struct {
	FilePath string `json:"filePath"`
}

// GetCSVHeaders returns the first row (headers) of a CSV file.
//
// 走 readCSVWithPathValidation 路由（內部走嚴格 allowlist、外部走 lenient
// performBasicSecurityChecks），避免之前 raw ReadCSV bypass 驗證導致任意檔讀取。
func (a *App) GetCSVHeaders(params CSVHeadersParams) (headers []string, err error) {
	defer recoverHandlerPanic("GetCSVHeaders", a.logger, &err)

	s := a.state.Load()
	records, err := a.readCSVWithPathValidation(s, params.FilePath, s.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取 CSV 標題失敗: %w", err)
	}
	// 確保有標題行
	if len(records) == 0 {
		return nil, ErrNoCSVHeaders
	}

	return records[0], nil
}

// Parameter structures

// MaxMeanParams holds parameters for maximum mean calculation.
type MaxMeanParams struct {
	InputPath  string  `json:"inputPath"`
	WindowSize int     `json:"windowSize"`
	StartTime  float64 `json:"startTime"`
	EndTime    float64 `json:"endTime"`
	IsBatch    bool    `json:"isBatch"`
}

// MaxMeanResult holds the result of maximum mean calculation.
type MaxMeanResult struct {
	OutputPath string      `json:"outputPath"`
	Headers    []string    `json:"headers"`
	Results    [][]float64 `json:"results"`
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
}

// NormalizeParams holds parameters for data normalization.
type NormalizeParams struct {
	MainFile      string `json:"mainFile"`
	ReferenceFile string `json:"referenceFile"`
	OutputPath    string `json:"outputPath"`
}

// NormalizeResult holds the result of data normalization.
type NormalizeResult struct {
	OutputPath string      `json:"outputPath"`
	Headers    []string    `json:"headers"`
	Data       [][]float64 `json:"data"`
	Success    bool        `json:"success"`
	Message    string      `json:"message"`
}

// ChartResult holds the result of chart generation.
type ChartResult struct {
	OutputPath  string `json:"outputPath"`
	HTMLContent string `json:"htmlContent"`
	Success     bool   `json:"success"`
	Message     string `json:"message"`
}

// PhaseSpec 是單一分期的名稱與時間邊界,對應前端送出的 {name, startTime, endTime}。
//
// StartTime/EndTime 收「原始字串」:前端送使用者輸入的原字串,後端以 strconv.ParseFloat
// 嚴格解析整串。前端 parseFloat 會把 "1.2foo" 截成 1.2、"0x10" 截成 0、非數值成 NaN,
// 這些部分解析 / 截斷的誤輸入若以數字傳入會被當合法值;改收字串 + 嚴格解析可一律拒絕
// (含空字串 / "1.2foo" / "0x10")。validatePhaseParams 另擋 ParseFloat 仍接受的 ±Inf / NaN。
type PhaseSpec struct {
	Name      string `json:"name"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// PhaseParams holds parameters for phase analysis. Phases 由前端提供(名稱 + 邊界),
// 後端據此分析並以該名稱標記結果,不再耦合 config.PhaseLabels。
type PhaseParams struct {
	InputFile  string      `json:"inputFile"`
	Phases     []PhaseSpec `json:"phases"`
	OutputPath string      `json:"outputPath"`
}

// PhaseResult holds the result of phase analysis.
type PhaseResult struct {
	OutputPath string          `json:"outputPath"`
	Headers    []string        `json:"headers"`
	Results    []PhaseAnalysis `json:"results"`
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
}

// PhaseAnalysis holds individual phase analysis data.
type PhaseAnalysis struct {
	PhaseLabel string    `json:"phaseLabel"`
	StartTime  float64   `json:"startTime"`
	EndTime    float64   `json:"endTime"`
	Duration   float64   `json:"duration"`
	Average    []float64 `json:"average"`
	MaxValues  []float64 `json:"maxValues"`
	MinValues  []float64 `json:"minValues"`
}

// PhaseSyncParams 分期同步分析參數.
type PhaseSyncParams struct {
	ManifestFile string            `json:"manifestFile"`
	DataFolder   string            `json:"dataFolder"`
	StartPhase   models.PhasePoint `json:"startPhase"`
	EndPhase     models.PhasePoint `json:"endPhase"`
	SubjectIndex int               `json:"subjectIndex"`
}

// PhaseSyncResult 分期同步分析結果.
type PhaseSyncResult struct {
	OutputPath   string             `json:"outputPath"`
	Subject      string             `json:"subject"`
	StartPhase   models.PhasePoint  `json:"startPhase"`
	StartTime    float64            `json:"startTime"`
	EndPhase     models.PhasePoint  `json:"endPhase"`
	EndTime      float64            `json:"endTime"`
	ChannelNames []string           `json:"channelNames"`
	ChannelMeans map[string]float64 `json:"channelMeans"`
	ChannelMaxes map[string]float64 `json:"channelMaxes"`
	Report       string             `json:"report"`
	Success      bool               `json:"success"`
	Message      string             `json:"message"`
}

// LoadPhaseManifest 載入分期總檔案的主題列表.
func (a *App) LoadPhaseManifest(manifestPath string) (subjects []string, err error) {
	defer recoverHandlerPanic("LoadPhaseManifest", a.logger, &err)

	a.logger.Info("載入分期總檔案", map[string]any{"path": manifestPath})

	// 邊界路徑驗證,擋掉 "../etc/passwd" 之類的 traversal / 系統敏感目錄。
	if err := validateExternalPathInputs("分期總檔案", manifestPath); err != nil {
		return nil, err
	}

	subjects, err = a.phaseSyncAnalyzer.LoadManifestSubjects(manifestPath)
	if err != nil {
		a.logger.Error("載入分期總檔案失敗", err, map[string]any{})
		return nil, fmt.Errorf("載入分期總檔案失敗: %w", err)
	}

	a.logger.Info("成功載入分期總檔案", map[string]any{"subjects": len(subjects)})

	return subjects, nil
}

// GetAvailablePhases 獲取可用的分期點列表。
//
// 回傳 []string 而非 []models.PhasePoint:Wails v2 TypeScript binding generator
// 不會為 named string type emit type alias,直接回 PhasePoint 會讓生成的 App.d.ts
// 引用未定義的 models.PhasePoint 型別,破壞前端 build。內部以 PhasePoint 計算後
// 在邊界 cast 為 string,前端 wire 結構零變動。
func (a *App) GetAvailablePhases() (out map[string][]string) {
	defer recoverHandlerPanicValue("GetAvailablePhases", a.logger, &out)

	return map[string][]string{
		"start": phasePointSliceToString(synchronizer.GetAvailableStartPhases()),
		"end":   phasePointSliceToString(synchronizer.GetAvailableEndPhases()),
	}
}

// phasePointSliceToString 在 Wails 邊界把 PhasePoint slice cast 為 string slice。
func phasePointSliceToString(phases []models.PhasePoint) []string {
	out := make([]string, len(phases))
	for i, p := range phases {
		out[i] = string(p)
	}

	return out
}

// phaseSyncValidateGateErr 把 AnalyzePhaseSync 的 Validate closure 失敗包進來,
// 讓 caller 端能用 errors.As 區分「Validate 失敗 (走 err channel,維持
// path_validation_test 對 traversal manifest / data folder 的 (nil, err) 期待)」
// 與「Execute / WriteCSV 失敗 (走 failed-result + nil err)」,**維持既有 mixed
// 錯誤通道契約完全不動**。
//
// 樣板的 Run 對所有 closure 失敗統一走 err channel — PhaseSync 既有契約是
// mixed (Validate 走 err、Execute/Write 走 failed-result),所以 caller 端需要
// 一個 sentinel marker 區分 err 來源。inner 直接透傳原 err,Error() / Unwrap
// 都保留原 message + chain,所以 strings.Contains("路徑驗證失敗") /
// errors.Is(err, ErrPathTraversal) 等既有 assertion 全部仍綠。
type phaseSyncValidateGateErr struct{ inner error }

func (e *phaseSyncValidateGateErr) Error() string { return e.inner.Error() }
func (e *phaseSyncValidateGateErr) Unwrap() error { return e.inner }

// AnalyzePhaseSync 執行分期同步分析.
//
// 走 [[AnalysisHandler[P, R]]] 樣板:Validate / Execute / WriteCSV 三 closure 分別綁
// 「manifest/folder/phase 必填 + validateExternalPathInputs」、`phaseSyncAnalyzer.AnalyzePhaseSync`、
// `csvHandler.WritePhaseSyncResult`。panic safety + entry/exit log 由樣板透過
// [[HandlerRun]] 收乾;handler body 只剩 metadata log + result envelope 組裝 + err 來源分流。
//
// 錯誤通道契約 (dual / mixed) 完全不變:Validate 失敗 → `(nil, err)` 走 err
// channel;Execute / WriteCSV 失敗 → `(failedResult, nil)` 走 failed-result
// channel;panic 經 HandlerRun 攔成 ErrInternalPanic → `(nil, err)` 走 err channel。
// 三者由 phaseSyncValidateGateErr sentinel + ErrInternalPanic + stats nil-ness 分流。
func (a *App) AnalyzePhaseSync(params PhaseSyncParams) (result *PhaseSyncResult, err error) {
	s := a.state.Load()
	a.logger.Info("分期同步分析參數", map[string]any{"params": params})

	// 創建分析參數 — Validate / Execute 都吃這個指標,提前到 Run 外組裝
	// (PhaseSyncParams → *models.AnalysisParams 是純複製,不涉及 validation)。
	analysisParams := &models.AnalysisParams{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		StartPhase:   params.StartPhase,
		EndPhase:     params.EndPhase,
		SubjectIndex: params.SubjectIndex,
	}

	handler := &AnalysisHandler[*models.AnalysisParams, *models.EMGStatistics]{
		Name:   "分期同步分析",
		Logger: a.logger,
		CSV:    s.csvHandler,
		Validate: func(p *models.AnalysisParams) error {
			if err := validateManifestHandlerParams(p.ManifestFile, p.DataFolder); err != nil {
				return &phaseSyncValidateGateErr{inner: err}
			}
			if p.StartPhase == "" || p.EndPhase == "" {
				return &phaseSyncValidateGateErr{inner: ErrNoPhaseSelection}
			}
			if !p.StartPhase.IsValid() || !p.EndPhase.IsValid() {
				return &phaseSyncValidateGateErr{inner: fmt.Errorf(
					"StartPhase=%q EndPhase=%q: %w",
					p.StartPhase, p.EndPhase, ErrInvalidPhasePoint,
				)}
			}
			return nil
		},
		Execute: func(ctx context.Context, p *models.AnalysisParams) (*models.EMGStatistics, error) {
			return a.phaseSyncAnalyzer.AnalyzePhaseSync(ctx, p)
		},
		WriteCSV: func(handler *io.CSVHandler, stats *models.EMGStatistics) (string, error) {
			// 導出結果 — ADR-0001: 寫檔職責由 PhaseSyncAnalyzer 搬到 CSVHandler,
			// 與其他 Analysis pipeline family handler 走同一條 format-aware write 路徑。
			return handler.WritePhaseSyncResult(io.WriteRequest{}, stats)
		},
	}

	stats, outputPath, runErr := handler.Run(a.context(), analysisParams)
	if runErr != nil {
		// 1) Validate gate err → 走 err channel (path_validation_test 期待 (nil, err))。
		if gateErr, ok := errors.AsType[*phaseSyncValidateGateErr](runErr); ok {
			return nil, gateErr.inner
		}

		// 2) Panic 經 HandlerRun 攔成 ErrInternalPanic → 走 err channel (panic 不該降級成 failed-result)。
		if errors.Is(runErr, ErrInternalPanic) {
			return nil, runErr
		}

		// 3) WriteCSV 失敗:樣板會把 stats (Execute 已成功的結果) 一併透出。
		if stats != nil {
			a.logger.Error("導出結果失敗", runErr, map[string]any{})

			return &PhaseSyncResult{
				Success: false,
				Message: fmt.Sprintf("導出失敗: %s", redact.RedactForMessage(runErr)),
			}, nil
		}

		// 4) Execute 失敗:stats == nil 且不是 Validate / panic → analyzer 失敗。
		a.logger.Error("分期同步分析失敗", runErr, map[string]any{})

		return &PhaseSyncResult{
			Success: false,
			Message: fmt.Sprintf("分析失敗: %s", redact.RedactForMessage(runErr)),
		}, nil
	}

	// 生成報告
	report := phase_sync.GenerateAnalysisReport(stats)

	// 返回結果
	result = &PhaseSyncResult{
		OutputPath:   outputPath,
		Subject:      stats.Subject,
		StartPhase:   stats.StartPhase,
		StartTime:    stats.StartTime,
		EndPhase:     stats.EndPhase,
		EndTime:      stats.EndTime,
		ChannelNames: stats.ChannelNames,
		ChannelMeans: stats.ChannelMeans,
		ChannelMaxes: stats.ChannelMaxes,
		Report:       report,
		Success:      true,
		Message:      "分析完成",
	}

	a.logger.Info("分期同步分析輸出", map[string]any{"outputPath": outputPath})

	return result, nil
}
