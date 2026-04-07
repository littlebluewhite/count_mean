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

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"count_mean/internal/calculator"
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
	ErrNoCSVFilesInFolder = errors.New("資料夾中沒有找到CSV文件")
)

// App struct.
type App struct {
	ctx               context.Context //nolint:containedctx // Required by Wails framework
	config            *config.AppConfig
	logger            *logging.Logger
	csvHandler        *io.CSVHandler
	maxMeanCalc       *calculator.MaxMeanCalculator
	normalizer        *calculator.Normalizer
	phaseAnalyzer     *calculator.PhaseAnalyzer
	chartGen          *chart.EChartsGenerator
	validator         *validation.InputValidator
	phaseSyncAnalyzer *phase_sync.PhaseSyncAnalyzer
	progressManager   *ProgressManager
	version           string
}

// NewApp creates a new App application struct.
func NewApp(cfg *config.AppConfig, version string) *App {
	// 創建模組實例
	csvHandler := io.NewCSVHandler(cfg)
	maxMeanCalc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
	normalizer := calculator.NewNormalizer(cfg.ScalingFactor)
	phaseAnalyzer := calculator.NewPhaseAnalyzer(cfg.ScalingFactor, cfg.PhaseLabels)
	chartGen := chart.NewEChartsGenerator()
	validator := validation.NewInputValidator()
	logger := logging.GetLogger("app")
	phaseSyncAnalyzer := phase_sync.NewPhaseSyncAnalyzer()
	progressManager := NewProgressManager()

	// 設置計算器的進度回調
	maxMeanCalc.SetProgressCallback(progressManager.CreateProgressCallback())

	return &App{
		config:            cfg,
		logger:            logger,
		csvHandler:        csvHandler,
		maxMeanCalc:       maxMeanCalc,
		normalizer:        normalizer,
		phaseAnalyzer:     phaseAnalyzer,
		chartGen:          chartGen,
		validator:         validator,
		phaseSyncAnalyzer: phaseSyncAnalyzer,
		progressManager:   progressManager,
		version:           version,
	}
}

// Startup is called when the app starts. The context is saved.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.logger.Info("Wails 應用程序啟動")

	// 確保必要的目錄存在
	if err := a.config.EnsureDirectories(); err != nil {
		a.logger.Error("無法創建必要目錄", err)
	}

	// 啟動進度管理器
	a.progressManager.Start()
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() *config.AppConfig {
	return a.config
}

// SaveConfig saves the configuration.
func (a *App) SaveConfig(cfg *config.AppConfig) error {
	a.config = cfg

	if err := cfg.SaveConfig("./config.json"); err != nil {
		return fmt.Errorf("儲存設定檔失敗: %w", err)
	}

	return nil
}

// ResetConfig resets to default configuration.
func (a *App) ResetConfig() *config.AppConfig {
	a.config = config.DefaultConfig()
	return a.config
}

// GetVersion returns the application version string.
func (a *App) GetVersion() string {
	return a.version
}

// SelectFile opens a file dialog for file selection.
func (a *App) SelectFile(title string, filters []runtime.FileFilter, buttonType string) (string, error) {
	var defaultDir string

	a.logger.Debug("選擇文件對話框", map[string]interface{}{"buttonType": buttonType})

	switch buttonType {
	case FileTypeInput:
		defaultDir = a.config.InputDir
	case FileTypeOutput:
		defaultDir = a.config.OutputDir
	case FileTypeOperate:
		defaultDir = a.config.OperateDir
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
	options := runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: a.config.InputDir,
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
func (a *App) calculateMaxMeanSingle(params MaxMeanParams) (*MaxMeanResult, error) {
	// 使用統一的 CSV 讀取方法（包含路徑驗證）
	records, err := a.ReadCSVWithPathValidation(params.InputPath, a.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 取得檔案名稱（不含路徑和副檔名）
	originalFileName := TrimCSVExtension(filepath.Base(params.InputPath))

	// 解析時間範圍並計算
	startRange, endRange := ResolveTimeRange(records, params.StartTime, params.EndTime)

	results, err := a.calculateWithTimeRange(records, params.WindowSize, startRange, endRange)
	if err != nil {
		return nil, fmt.Errorf("計算失敗: %w", err)
	}

	// 輸出結果
	outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, startRange, endRange)
	outputFile := buildOutputFilename(originalFileName, SuffixMaxMean)

	if err := a.csvHandler.WriteCSVToOutput(outputFile, outputData); err != nil {
		return nil, fmt.Errorf("寫入輸出檔案失敗: %w", err)
	}

	// 準備回傳結果
	return &MaxMeanResult{
		OutputPath: filepath.Join(a.config.OutputDir, outputFile),
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

// discoverBatchFiles 解析輸入路徑並找到所有CSV檔案.
func (a *App) discoverBatchFiles(inputPath string) (*batchFileDiscoveryResult, error) {
	if !filepath.IsAbs(inputPath) {
		return &batchFileDiscoveryResult{dirName: inputPath}, nil
	}

	relPath, err := filepath.Rel(a.config.InputDir, inputPath)
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
func (a *App) executeBatchLoop(
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

		result, err := a.processSingleBatchFile(records, entry.displayName, ctx)
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
func (a *App) processSingleBatchFile(
	records [][]string,
	fileBaseName string,
	ctx *batchProcessContext,
) (*batchProcessResult, error) {
	startRange, endRange := ResolveTimeRange(records, ctx.startTime, ctx.endTime)

	results, err := a.calculateWithTimeRange(records, ctx.windowSize, startRange, endRange)
	if err != nil {
		return nil, err
	}

	outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, startRange, endRange)
	outputFile := buildOutputFilename(fileBaseName, SuffixMaxMean)

	if writeErr := a.csvHandler.WriteCSVToOutputDirectory(ctx.outputDirName, outputFile, outputData); writeErr != nil {
		return nil, fmt.Errorf("寫入CSV輸出失敗: %w", writeErr)
	}

	return &batchProcessResult{
		headers: records[0],
		results: convertMaxMeanResultsToArray(results),
	}, nil
}

// calculateMaxMeanBatch 批次處理資料夾中的所有CSV檔案.
func (a *App) calculateMaxMeanBatch(params MaxMeanParams) (*MaxMeanResult, error) {
	if err := a.validator.ValidateDirectoryPath(params.InputPath); err != nil {
		return nil, fmt.Errorf("目錄路徑驗證失敗: %w", err)
	}

	discovery, err := a.discoverBatchFiles(params.InputPath)
	if err != nil {
		return nil, err
	}

	if discovery.isDirect {
		return a.executeBatchCalculationDirect(
			discovery.fullPaths,
			discovery.dirName,
			params.WindowSize,
			params.StartTime,
			params.EndTime,
		)
	}

	csvFiles, err := a.csvHandler.ListCSVFilesInDirectory(discovery.dirName)
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
				return a.csvHandler.ReadCSVFromDirectory(discovery.dirName, fn)
			},
		}
	}

	return a.executeBatchLoop(entries, ctx, filepath.Join(a.config.OutputDir, discovery.dirName))
}

// executeBatchCalculationDirect 直接處理外部目錄的批次計算.
func (a *App) executeBatchCalculationDirect(
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
				return a.csvHandler.ReadCSVExternal(fp)
			},
		}
	}

	return a.executeBatchLoop(entries, ctx, outputDirName)
}

// NormalizeData performs data normalization.
func (a *App) NormalizeData(params NormalizeParams) (*NormalizeResult, error) {
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
	mainRecords, err := a.ReadCSVWithPathValidation(params.MainFile, a.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取主要資料檔案失敗: %w", err)
	}

	// 讀取參考資料檔案（包含路徑驗證）
	refRecords, err := a.ReadCSVWithPathValidation(params.ReferenceFile, a.config.OperateDir)
	if err != nil {
		return nil, fmt.Errorf("讀取參考資料檔案失敗: %w", err)
	}

	// 執行標準化
	normalizedData, err := a.normalizer.NormalizeFromRawData(mainRecords, refRecords)
	if err != nil {
		return nil, fmt.Errorf("標準化計算失敗: %w", err)
	}

	// 生成輸出檔名並保存結果
	mainBaseName := TrimCSVExtension(filepath.Base(params.MainFile))
	outputName := resolveOutputName(params.OutputPath, mainBaseName, SuffixNormalized)
	outputData := a.csvHandler.ConvertNormalizedDataToCSV(normalizedData)

	if err = a.csvHandler.WriteCSVToOutput(outputName, outputData); err != nil {
		return nil, fmt.Errorf("保存結果失敗: %w", err)
	}

	// 準備回傳結果
	outputPath := filepath.Join(a.config.OutputDir, outputName)
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
	records, err := a.ReadCSVWithPathValidation(params.InputFile, a.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取資料檔案失敗: %w", err)
	}

	// 執行階段分析
	analysisResult, err := a.phaseAnalyzer.AnalyzeFromRawData(records, cleanLabels)
	if err != nil {
		return nil, fmt.Errorf("階段分析失敗: %w", err)
	}

	// 生成輸出檔名並保存結果
	outputName := generatePhaseOutputName(params.InputFile, params.OutputPath)
	outputData := a.csvHandler.ConvertPhaseAnalysisToCSV(
		records[0], &analysisResult.PhaseResults[0], analysisResult.MaxTimeIndex,
	)

	if err = a.csvHandler.WriteCSVToOutput(outputName, outputData); err != nil {
		return nil, fmt.Errorf("保存結果失敗: %w", err)
	}

	// 轉換分析結果
	outputPath := filepath.Join(a.config.OutputDir, outputName)
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
func (a *App) GetCSVHeaders(params CSVHeadersParams) ([]string, error) {
	// 讀取 CSV 檔案
	records, err := a.csvHandler.ReadCSV(params.FilePath)
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
	ManifestFile string `json:"manifestFile"`
	DataFolder   string `json:"dataFolder"`
	StartPhase   string `json:"startPhase"`
	EndPhase     string `json:"endPhase"`
	SubjectIndex int    `json:"subjectIndex"`
}

// PhaseSyncResult 分期同步分析結果.
type PhaseSyncResult struct {
	OutputPath   string             `json:"outputPath"`
	Subject      string             `json:"subject"`
	StartPhase   string             `json:"startPhase"`
	StartTime    float64            `json:"startTime"`
	EndPhase     string             `json:"endPhase"`
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
func (*App) GetAvailablePhases() map[string][]string {
	return map[string][]string{
		"start": synchronizer.GetAvailableStartPhases(),
		"end":   synchronizer.GetAvailableEndPhases(),
	}
}

// AnalyzePhaseSync 執行分期同步分析.
func (a *App) AnalyzePhaseSync(params PhaseSyncParams) (*PhaseSyncResult, error) {
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

	// 創建分析參數
	analysisParams := &models.AnalysisParams{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		StartPhase:   params.StartPhase,
		EndPhase:     params.EndPhase,
		SubjectIndex: params.SubjectIndex,
	}

	// 執行分析
	stats, err := a.phaseSyncAnalyzer.AnalyzePhaseSync(analysisParams)
	if err != nil {
		a.logger.Error("分期同步分析失敗", err, map[string]interface{}{})

		return &PhaseSyncResult{
			Success: false,
			Message: fmt.Sprintf("分析失敗: %v", err),
		}, nil
	}

	// 導出結果
	outputPath, err := a.phaseSyncAnalyzer.ExportResults(stats, a.config.OutputDir)
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

// SubscribeToProgress 訂閱進度更新.
func (a *App) SubscribeToProgress() <-chan models.ProgressInfo {
	return a.progressManager.Subscribe()
}

// UnsubscribeFromProgress 取消訂閱進度更新.
func (a *App) UnsubscribeFromProgress(ch <-chan models.ProgressInfo) {
	a.progressManager.Unsubscribe(ch)
}

// IsProgressActive 檢查進度管理器是否活躍.
func (a *App) IsProgressActive() bool {
	return a.progressManager.IsActive()
}

// GetBackpressureStats 獲取背壓控制統計信息.
func (a *App) GetBackpressureStats() models.BackpressureStats {
	return a.maxMeanCalc.GetBackpressureStats()
}
