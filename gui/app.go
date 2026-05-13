// Package gui provides the graphical user interface for the EMG data analysis application.
// It implements the Wails v2 desktop application with support for file operations,
// data processing, and chart generation.
package gui

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"count_mean/internal/calculator"
	"count_mean/internal/cci"
	"count_mean/internal/chart"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/phase_sync"
	"count_mean/internal/synchronizer"
	"count_mean/internal/validation"
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
	ErrInvalidPhasePoint  = errors.New("無效的分期點代碼")
	ErrNoCSVFilesInFolder = errors.New("資料夾中沒有找到CSV文件")
)

// appState 把受 config 影響的 5 個 dependency + config 打包成 atomic snapshot。
// applyConfig 用 atomic.Pointer.Store 一次性 swap,讓 Wails 並行 RPC 場景下
// in-flight 分析 method 看到的永遠是一致的 snapshot,不會出現「半新半舊」
// mixed state。前一輪 Wave 2 (d45ee1f) 解了「不一致」但引入了非原子重建 race,
// 本次 atomic 化補上 (cross-compare review fresh bug hunt P2)。
type appState struct {
	config        *config.AppConfig
	csvHandler    *io.CSVHandler
	maxMeanCalc   *calculator.MaxMeanCalculator
	normalizer    *calculator.Normalizer
	phaseAnalyzer *calculator.PhaseAnalyzer
}

// App struct.
type App struct {
	ctx               context.Context          //nolint:containedctx // Required by Wails framework
	state             atomic.Pointer[appState] // 取代原 5 個 mutable 欄位,保證 swap 原子性
	logger            *logging.Logger
	chartGen          *chart.EChartsGenerator
	validator         *validation.InputValidator
	phaseSyncAnalyzer *phase_sync.PhaseSyncAnalyzer
	cciAnalyzer       *cci.CCIAnalyzer
	progressManager   *ProgressManager
	version           string
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
func NewApp(cfg *config.AppConfig, version string) *App {
	progressManager := NewProgressManager()

	initialState := buildAppState(cfg)
	initialState.maxMeanCalc.SetProgressCallback(progressManager.CreateProgressCallback())

	a := &App{
		logger:            logging.GetLogger("app"),
		chartGen:          chart.NewEChartsGenerator(),
		validator:         validation.NewInputValidator(),
		phaseSyncAnalyzer: phase_sync.NewPhaseSyncAnalyzer(),
		cciAnalyzer:       cci.NewCCIAnalyzer(),
		progressManager:   progressManager,
		version:           version,
	}
	a.state.Store(initialState)
	return a
}

// Startup is called when the app starts. The context is saved.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.logger.Info("Wails 應用程序啟動")

	// 確保必要的目錄存在
	s := a.state.Load()
	if err := s.config.EnsureDirectories(); err != nil {
		a.logger.Error("無法創建必要目錄", err)
	}

	// 啟動進度管理器
	a.progressManager.Start()
}

// context returns the Wails-supplied lifecycle context (set in Startup).
// Falls back to context.Background() when Startup hasn't been called yet —
// typically in unit tests that instantiate App directly without driving the
// Wails framework. Production callers always have a.ctx non-nil because Wails
// invokes Startup before any user-facing method.
//
// 取代各 entry method 散落的 context.Background()：先前 calculateWithTimeRange
// 接 ctx 的設計目標是讓 Wails Shutdown 能取消 long-running 計算，但 caller
// 永遠傳 Background()，整套 cancellation chain 變成「實作了沒接線」
// （Wave 6 review P1 — senior-software-engineer / refactor-specialist 收斂）。
func (a *App) context() context.Context {
	if a.ctx != nil {
		return a.ctx
	}

	return context.Background()
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() *config.AppConfig {
	s := a.state.Load()
	return s.config
}

// SaveConfig saves the configuration.
// 必須一併重建 csvHandler / maxMeanCalc / normalizer / phaseAnalyzer，
// 否則它們仍持有舊 cfg（ScalingFactor、Precision、OutputDir、PhaseLabels），
// 使用者改設定後計算與輸出仍走舊值 — 是 user-visible silent bug。
func (a *App) SaveConfig(cfg *config.AppConfig) error {
	if err := cfg.SaveConfig("./config.json"); err != nil {
		return fmt.Errorf("儲存設定檔失敗: %w", err)
	}

	a.applyConfig(cfg)

	return nil
}

// ResetConfig resets to default configuration.
// 重建相依元件以確保新 config 生效（同 SaveConfig 的理由）。
func (a *App) ResetConfig() *config.AppConfig {
	cfg := config.DefaultConfig()
	a.applyConfig(cfg)

	return cfg
}

// applyConfig 用 atomic.Pointer.Store 一次性 swap 整個 appState snapshot,
// 取代原本連續寫 5 個 pointer 欄位的非原子模式 (Wave 2 遺留 race)。
// Wails 並行 RPC 場景下 in-flight 分析 method 看到的 snapshot 永遠一致 —
// SaveConfig 期間正在跑的分析仍用舊 snapshot,新分析才看到新 snapshot。
func (a *App) applyConfig(cfg *config.AppConfig) {
	newState := buildAppState(cfg)
	newState.maxMeanCalc.SetProgressCallback(a.progressManager.CreateProgressCallback())
	a.state.Store(newState)
}

// GetVersion returns the application version string.
func (a *App) GetVersion() string {
	return a.version
}

// SelectFile opens a file dialog for file selection.
func (a *App) SelectFile(title string, filters []runtime.FileFilter, buttonType string) (string, error) {
	cfg := a.state.Load().config

	var defaultDir string

	a.logger.Debug("選擇文件對話框", map[string]interface{}{"buttonType": buttonType})

	switch buttonType {
	case FileTypeInput:
		defaultDir = cfg.InputDir
	case FileTypeOutput:
		defaultDir = cfg.OutputDir
	case FileTypeOperate:
		defaultDir = cfg.OperateDir
	}

	a.logger.Debug("預設目錄", map[string]interface{}{"defaultDir": defaultDir})

	options := runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDir,
		Filters:          filters,
	}

	file, err := runtime.OpenFileDialog(a.ctx, options)
	if err != nil {
		return "", fmt.Errorf("開啟檔案對話框失敗: %w", err)
	}

	return file, nil
}

// SelectDirectory opens a directory dialog.
func (a *App) SelectDirectory(title string) (string, error) {
	s := a.state.Load()
	options := runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: s.config.InputDir,
	}

	dir, err := runtime.OpenDirectoryDialog(a.ctx, options)
	if err != nil {
		return "", fmt.Errorf("開啟目錄對話框失敗: %w", err)
	}

	return dir, nil
}

// CalculateMaxMean calculates maximum mean values.
func (a *App) CalculateMaxMean(params MaxMeanParams) (*MaxMeanResult, error) {
	a.logger.Info("開始最大平均值計算", map[string]interface{}{
		"input_path":  params.InputPath,
		"window_size": params.WindowSize,
		"is_batch":    params.IsBatch,
	})

	a.logger.Debug("計算參數", map[string]interface{}{"params": params})

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
	startRange, endRange := ResolveTimeRange(records, params.StartTime, params.EndTime)

	results, err := a.calculateWithTimeRange(a.context(), s, records, params.WindowSize, startRange, endRange)
	if err != nil {
		return nil, fmt.Errorf("計算失敗: %w", err)
	}

	// 輸出結果
	outputData := s.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, startRange, endRange)
	outputFile := buildOutputFilename(originalFileName, SuffixMaxMean)

	if err := s.csvHandler.WriteCSVToOutput(outputFile, outputData); err != nil {
		return nil, fmt.Errorf("寫入輸出檔案失敗: %w", err)
	}

	// 準備回傳結果
	return &MaxMeanResult{
		OutputPath: filepath.Join(s.config.OutputDir, outputFile),
		Headers:    records[0],
		Results:    convertMaxMeanResultsToArray(results),
	}, nil
}

// batchFileDiscoveryResult 存放批次檔案搜尋結果.
type batchFileDiscoveryResult struct {
	dirName   string
	isDirect  bool // true if processing external directory directly
	fullPaths []string
}

// discoverBatchFiles 解析輸入路徑並找到所有CSV檔案。
//
// `s` 由 caller (calculateMaxMeanBatch) 顯式傳入，與 helper 內部曾經做的第二次
// a.state.Load() 相比能避免 cross-snapshot 撕裂：若批次 entry 抓 snapshot 後、
// helper 重新 Load 之前觸發 SaveConfig (改 InputDir)，原本會用新 InputDir 解析
// inputPath 但用舊 csvHandler 列檔，將輸入導向錯誤目錄（codex Wave 7 finding）。
func (a *App) discoverBatchFiles(s *appState, inputPath string) (*batchFileDiscoveryResult, error) {
	if !filepath.IsAbs(inputPath) {
		return &batchFileDiscoveryResult{dirName: inputPath}, nil
	}

	relPath, err := filepath.Rel(s.config.InputDir, inputPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return discoverExternalBatchFiles(inputPath)
	}

	return &batchFileDiscoveryResult{dirName: relPath}, nil
}

// discoverExternalBatchFiles 搜尋外部目錄中的CSV檔案.
func discoverExternalBatchFiles(inputPath string) (*batchFileDiscoveryResult, error) {
	files, err := filepath.Glob(filepath.Join(inputPath, "*.csv"))
	if err != nil {
		return nil, fmt.Errorf("搜尋CSV文件失敗: %w", err)
	}

	if len(files) == 0 {
		return nil, ErrNoCSVFilesInFolder
	}

	return &batchFileDiscoveryResult{
		dirName:   filepath.Base(inputPath),
		isDirect:  true,
		fullPaths: files,
	}, nil
}

// batchProcessContext 批次處理上下文.
type batchProcessContext struct {
	windowSize    int
	startTime     float64
	endTime       float64
	outputDirName string
}

// batchProcessResult 批次處理單檔結果.
type batchProcessResult struct {
	headers []string
	results [][]float64
}

// batchFileEntry represents a file to process in batch mode.
type batchFileEntry struct {
	displayName string
	readFunc    func() ([][]string, error)
}

// executeBatchLoop processes batch file entries and accumulates results.
//
// `s` 必須由 caller (calculateMaxMeanBatch / executeBatchCalculationDirect) 取得
// 並一路傳到 processSingleBatchFile,確保整個批次內所有檔案使用同一份 snapshot
// (csvHandler / maxMeanCalc 配對一致),避免中途 SaveConfig 造成撕裂。
func (a *App) executeBatchLoop(
	s *appState,
	entries []batchFileEntry,
	ctx *batchProcessContext,
	outputPath string,
) (*MaxMeanResult, error) {
	var allHeaders []string
	// Pre-allocate with estimated capacity (assume ~10 results per file on average)
	estimatedCapacity := 10
	allResults := make([][]float64, 0, len(entries)*estimatedCapacity)
	successCount := 0
	failCount := 0

	for _, entry := range entries {
		records, err := entry.readFunc()
		if err != nil {
			failCount++

			a.logger.Error("讀取檔案失敗", err, map[string]interface{}{"file": entry.displayName})

			continue
		}

		result, err := a.processSingleBatchFile(s, records, entry.displayName, ctx)
		if err != nil {
			failCount++

			a.logger.Error("處理檔案失敗", err, map[string]interface{}{"file": entry.displayName})

			continue
		}

		if len(allHeaders) == 0 {
			allHeaders = result.headers
		}

		allResults = append(allResults, result.results...)
		successCount++

		a.logger.Info("檔案處理成功", map[string]interface{}{
			"file":          entry.displayName,
			"results_count": len(result.results),
		})
	}

	message := fmt.Sprintf("批次處理完成：成功 %d 個檔案，失敗 %d 個檔案", successCount, failCount)

	return &MaxMeanResult{
		OutputPath: outputPath,
		Headers:    allHeaders,
		Results:    allResults,
		Success:    successCount > 0,
		Message:    message,
	}, nil
}

// processSingleBatchFile 處理批次中的單一檔案.
// 使用 caller 傳入的 *appState snapshot,csvHandler 與 maxMeanCalc 必為同源(同一次 cfg)。
func (a *App) processSingleBatchFile(
	s *appState,
	records [][]string,
	fileBaseName string,
	ctx *batchProcessContext,
) (*batchProcessResult, error) {
	startRange, endRange := ResolveTimeRange(records, ctx.startTime, ctx.endTime)

	results, err := a.calculateWithTimeRange(a.context(), s, records, ctx.windowSize, startRange, endRange)
	if err != nil {
		return nil, err
	}

	outputData := s.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, startRange, endRange)
	outputFile := buildOutputFilename(fileBaseName, SuffixMaxMean)

	if writeErr := s.csvHandler.WriteCSVToOutputDirectory(ctx.outputDirName, outputFile, outputData); writeErr != nil {
		return nil, fmt.Errorf("寫入CSV輸出失敗: %w", writeErr)
	}

	return &batchProcessResult{
		headers: records[0],
		results: convertMaxMeanResultsToArray(results),
	}, nil
}

// calculateMaxMeanBatch 批次處理資料夾中的所有CSV檔案.
func (a *App) calculateMaxMeanBatch(params MaxMeanParams) (*MaxMeanResult, error) {
	s := a.state.Load()

	if err := a.validator.ValidateDirectoryPath(params.InputPath); err != nil {
		return nil, fmt.Errorf("目錄路徑驗證失敗: %w", err)
	}

	discovery, err := a.discoverBatchFiles(s, params.InputPath)
	if err != nil {
		return nil, err
	}

	if discovery.isDirect {
		return a.executeBatchCalculationDirect(
			s,
			discovery.fullPaths,
			discovery.dirName,
			params.WindowSize,
			params.StartTime,
			params.EndTime,
		)
	}

	csvFiles, err := s.csvHandler.ListCSVFilesInDirectory(discovery.dirName)
	if err != nil {
		return nil, fmt.Errorf("列出CSV文件失敗: %w", err)
	}

	if len(csvFiles) == 0 {
		return nil, ErrNoCSVFilesInFolder
	}

	ctx := &batchProcessContext{
		windowSize:    params.WindowSize,
		startTime:     params.StartTime,
		endTime:       params.EndTime,
		outputDirName: filepath.Base(discovery.dirName),
	}

	entries := make([]batchFileEntry, len(csvFiles))

	for i, fileName := range csvFiles {
		fn := fileName
		entries[i] = batchFileEntry{
			displayName: TrimCSVExtension(fn),
			readFunc: func() ([][]string, error) {
				return s.csvHandler.ReadCSVFromDirectory(discovery.dirName, fn)
			},
		}
	}

	return a.executeBatchLoop(s, entries, ctx, filepath.Join(s.config.OutputDir, discovery.dirName))
}

// executeBatchCalculationDirect 直接處理外部目錄的批次計算.
func (a *App) executeBatchCalculationDirect(
	s *appState,
	files []string,
	outputDirName string,
	windowSize int,
	startTime, endTime float64,
) (*MaxMeanResult, error) {
	ctx := &batchProcessContext{
		windowSize:    windowSize,
		startTime:     startTime,
		endTime:       endTime,
		outputDirName: outputDirName,
	}

	entries := make([]batchFileEntry, len(files))

	for i, fullPath := range files {
		fp := fullPath
		entries[i] = batchFileEntry{
			displayName: TrimCSVExtension(filepath.Base(fp)),
			readFunc: func() ([][]string, error) {
				return s.csvHandler.ReadCSVExternal(fp)
			},
		}
	}

	return a.executeBatchLoop(s, entries, ctx, outputDirName)
}

// NormalizeData performs data normalization.
func (a *App) NormalizeData(params NormalizeParams) (*NormalizeResult, error) {
	s := a.state.Load()

	a.logger.Info("開始資料標準化", map[string]interface{}{
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
	outputData := s.csvHandler.ConvertNormalizedDataToCSV(normalizedData)

	if err = s.csvHandler.WriteCSVToOutput(outputName, outputData); err != nil {
		return nil, fmt.Errorf("保存結果失敗: %w", err)
	}

	// 準備回傳結果
	outputPath := filepath.Join(s.config.OutputDir, outputName)
	data := convertNormalizedDataToArray(normalizedData)

	a.logger.Info("資料標準化完成", map[string]interface{}{
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

// validatePhaseParams validates phase analysis parameters and returns cleaned labels.
func validatePhaseParams(params PhaseParams) ([]string, error) {
	if params.InputFile == "" {
		return nil, ErrNoInputFile
	}

	if len(params.PhaseLabels) == 0 {
		return nil, ErrNoPhaseLabels
	}

	// 清理階段標籤（移除空白）
	cleanLabels := make([]string, 0, len(params.PhaseLabels))

	for _, label := range params.PhaseLabels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			cleanLabels = append(cleanLabels, trimmed)
		}
	}

	if len(cleanLabels) == 0 {
		return nil, ErrNoValidPhaseLabels
	}

	return cleanLabels, nil
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

	for colIdx, val := range phaseResult.MaxValues {
		if colIdx-1 >= 0 && colIdx-1 < len(maxValues) {
			maxValues[colIdx-1] = val
		}
	}

	for colIdx, val := range phaseResult.MeanValues {
		if colIdx-1 >= 0 && colIdx-1 < len(meanValues) {
			meanValues[colIdx-1] = val
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

// AnalyzePhases performs phase analysis.
func (a *App) AnalyzePhases(params PhaseParams) (*PhaseResult, error) {
	s := a.state.Load()

	a.logger.Info("開始階段分析", map[string]interface{}{
		"input_file":   params.InputFile,
		"phase_labels": params.PhaseLabels,
		"output_path":  params.OutputPath,
	})

	// 驗證輸入
	cleanLabels, err := validatePhaseParams(params)
	if err != nil {
		return nil, err
	}

	// 讀取資料檔案（包含路徑驗證）
	records, err := a.readCSVWithPathValidation(s, params.InputFile, s.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取資料檔案失敗: %w", err)
	}

	// 執行階段分析
	analysisResult, err := s.phaseAnalyzer.AnalyzeFromRawData(records, cleanLabels)
	if err != nil {
		return nil, fmt.Errorf("階段分析失敗: %w", err)
	}

	// 生成輸出檔名並保存結果。
	// 此前只用 PhaseResults[0] → 其餘 phase 資料完全丟失。
	// 改為迴圈合併所有 phase 的 rows 進同一個 CSV，結構：
	//   row 0:       header
	//   row 1-2N:    每個 phase 的「最大值 / 平均值」（共 2 行 × len(PhaseResults)）
	//   row last:    「整個階段最大值出現在_秒」（whole-phase 全域資料，只放一次）
	//
	// 注意：ConvertPhaseAnalysisToCSV 的 maxTimeIndex 參數會讓它在尾端加 time row。
	// 對每個 phase 都附這一行會造成重複（codex review 指出的問題）。
	// 處理方式：先把 phase rows 全部不帶 maxTimeIndex 收集，最後再單獨加一次。
	outputName := generatePhaseOutputName(params.InputFile, params.OutputPath)

	var outputData [][]string

	for i := range analysisResult.PhaseResults {
		phaseRows := s.csvHandler.ConvertPhaseAnalysisToCSV(
			records[0], &analysisResult.PhaseResults[i], nil, // nil → 不在每 phase 後附 time row
		)

		if i == 0 {
			outputData = phaseRows // 含 header
			continue
		}

		// 後續 phase 跳過 header row（phaseRows[0]），只附加 max/mean 兩行
		if len(phaseRows) > 1 {
			outputData = append(outputData, phaseRows[1:]...)
		}
	}

	// 全域 time-index row：所有 phase 都加完後，只附加一次
	if len(analysisResult.MaxTimeIndex) > 0 && len(analysisResult.PhaseResults) > 0 {
		fullRows := s.csvHandler.ConvertPhaseAnalysisToCSV(
			records[0], &analysisResult.PhaseResults[0], analysisResult.MaxTimeIndex,
		)
		if len(fullRows) > 3 {
			outputData = append(outputData, fullRows[3:]...)
		}
	}

	if err = s.csvHandler.WriteCSVToOutput(outputName, outputData); err != nil {
		return nil, fmt.Errorf("保存結果失敗: %w", err)
	}

	// 轉換分析結果
	outputPath := filepath.Join(s.config.OutputDir, outputName)
	channelCount := len(records[0]) - 1
	results := make([]PhaseAnalysis, 0, len(analysisResult.PhaseResults))

	for i, phaseResult := range analysisResult.PhaseResults {
		if i >= len(cleanLabels) {
			break
		}

		results = append(results, convertPhaseResultToAnalysis(&phaseResult, channelCount))
	}

	a.logger.Info("階段分析完成", map[string]interface{}{
		"output_file":   outputPath,
		"phase_count":   len(results),
		"channel_count": channelCount,
	})

	return &PhaseResult{
		OutputPath: outputPath,
		Headers:    records[0],
		Results:    results,
		Success:    true,
		Message:    fmt.Sprintf("階段分析成功完成，結果已保存到: %s", outputPath),
	}, nil
}

// ShowMessage displays an informational dialog.
func (a *App) ShowMessage(title, message string) {
	//nolint:errcheck,gosec // Return value intentionally ignored for fire-and-forget UI dialog
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type:    runtime.InfoDialog,
		Title:   title,
		Message: message,
	})
}

// ShowError displays an error dialog.
func (a *App) ShowError(title, message string) {
	//nolint:errcheck,gosec // Return value intentionally ignored for fire-and-forget UI dialog
	runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
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
func (a *App) GetCSVHeaders(params CSVHeadersParams) ([]string, error) {
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

// InteractiveChartParams holds parameters for generating interactive ECharts.
type InteractiveChartParams struct {
	FilePath string `json:"filePath"`
	Columns  []int  `json:"columns"`
	Title    string `json:"title"`
	Width    string `json:"width"`
	Height   string `json:"height"`
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

// ChartParams holds parameters for chart generation.
type ChartParams struct {
	FilePath  string `json:"filePath"`
	Columns   []int  `json:"columns"`
	Title     string `json:"title"`
	ImageData string `json:"imageData"` // base64 PNG 數據
}

// ChartResult holds the result of chart generation.
type ChartResult struct {
	OutputPath  string `json:"outputPath"`
	HTMLContent string `json:"htmlContent"`
	Success     bool   `json:"success"`
	Message     string `json:"message"`
}

// PhaseParams holds parameters for phase analysis.
type PhaseParams struct {
	InputFile   string   `json:"inputFile"`
	PhaseLabels []string `json:"phaseLabels"`
	OutputPath  string   `json:"outputPath"`
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
func (a *App) LoadPhaseManifest(manifestPath string) ([]string, error) {
	a.logger.Info("載入分期總檔案", map[string]interface{}{"path": manifestPath})

	subjects, err := a.phaseSyncAnalyzer.LoadManifestSubjects(manifestPath)
	if err != nil {
		a.logger.Error("載入分期總檔案失敗", err, map[string]interface{}{})
		return nil, fmt.Errorf("載入分期總檔案失敗: %w", err)
	}

	a.logger.Info("成功載入分期總檔案", map[string]interface{}{"subjects": len(subjects)})

	return subjects, nil
}

// GetAvailablePhases 獲取可用的分期點列表.
// 回傳 []string 而非 []models.PhasePoint —— Wails v2 TypeScript binding
// generator 不會為 named string type emit type alias，若直接回 PhasePoint，
// 生成的 App.d.ts 會引用未定義的 models.PhasePoint 型別，破壞前端 build
// （codex Wave 4 PR-F1 P2 指出）。內部以 PhasePoint 計算後在邊界 cast 為
// string，前端 wire 結構零變動。
func (*App) GetAvailablePhases() map[string][]string {
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

// AnalyzePhaseSync 執行分期同步分析.
func (a *App) AnalyzePhaseSync(params PhaseSyncParams) (*PhaseSyncResult, error) {
	s := a.state.Load()
	a.logger.Info("開始分期同步分析", map[string]interface{}{"params": params})

	// 參數驗證
	if params.ManifestFile == "" {
		return nil, ErrNoManifestFile
	}

	if params.DataFolder == "" {
		return nil, ErrNoDataFolder
	}

	if params.StartPhase == "" || params.EndPhase == "" {
		return nil, ErrNoPhaseSelection
	}

	if !params.StartPhase.IsValid() || !params.EndPhase.IsValid() {
		return nil, fmt.Errorf("StartPhase=%q EndPhase=%q: %w",
			params.StartPhase, params.EndPhase, ErrInvalidPhasePoint)
	}

	// 創建分析參數
	analysisParams := &models.AnalysisParams{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		StartPhase:   params.StartPhase,
		EndPhase:     params.EndPhase,
		SubjectIndex: params.SubjectIndex,
	}

	// 執行分析
	stats, err := a.phaseSyncAnalyzer.AnalyzePhaseSync(a.context(), analysisParams)
	if err != nil {
		a.logger.Error("分期同步分析失敗", err, map[string]interface{}{})

		return &PhaseSyncResult{
			Success: false,
			Message: fmt.Sprintf("分析失敗: %v", err),
		}, nil
	}

	// 導出結果
	outputPath, err := a.phaseSyncAnalyzer.ExportResults(stats, s.config.OutputDir)
	if err != nil {
		a.logger.Error("導出結果失敗", err, map[string]interface{}{})

		return &PhaseSyncResult{
			Success: false,
			Message: fmt.Sprintf("導出失敗: %v", err),
		}, nil
	}

	// 生成報告
	report := phase_sync.GenerateAnalysisReport(stats)

	// 返回結果
	result := &PhaseSyncResult{
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

	a.logger.Info("分期同步分析完成", map[string]interface{}{"outputPath": outputPath})

	return result, nil
}

// GetCurrentProgress 獲取當前進度信息.
func (a *App) GetCurrentProgress() *models.ProgressInfo {
	return a.progressManager.GetCurrentProgress()
}

// IsProgressActive 檢查進度管理器是否活躍.
func (a *App) IsProgressActive() bool {
	return a.progressManager.IsActive()
}

// GetBackpressureStats 獲取背壓控制統計信息.
func (a *App) GetBackpressureStats() models.BackpressureStats {
	s := a.state.Load()
	return s.maxMeanCalc.GetBackpressureStats()
}
