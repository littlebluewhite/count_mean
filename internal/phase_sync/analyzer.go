// Package phase_sync provides phase synchronization analysis for EMG, motion,
// and force plate data. It coordinates data validation, time synchronization,
// and statistical calculations across multiple data sources.
//
//nolint:revive // package name with underscore maintained for backward compatibility
package phase_sync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"count_mean/internal/calculator"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
	"count_mean/internal/synchronizer"
)

// Default precision for EMG statistics.
const defaultEMGStatsPrecision = 6

// Validation errors.
var (
	// ErrInvalidSubjectIndex indicates an invalid subject index.
	ErrInvalidSubjectIndex = errors.New("invalid subject index")
	// ErrFileNotFound indicates a file was not found.
	ErrFileNotFound = errors.New("file not found")
	// ErrPhasePointOutOfRange indicates a phase point is out of data range.
	ErrPhasePointOutOfRange = errors.New("phase point out of data range")
	// ErrEMGTimeOutOfRange indicates EMG time is out of data range.
	ErrEMGTimeOutOfRange = errors.New("EMG time out of data range")
)

// PhaseSyncAnalyzer 分期同步分析器.
type PhaseSyncAnalyzer struct {
	manifestParser  *parsers.PhaseManifestParser
	emgParser       *parsers.EMGParser
	motionParser    *parsers.MotionParser
	ancParser       *parsers.ANCParser
	phaseCalculator *synchronizer.PhaseCalculator
	statsCalculator *calculator.EMGStatisticsCalculator
	pathValidator   *security.PathValidator
}

// NewPhaseSyncAnalyzer 創建新的分期同步分析器.
func NewPhaseSyncAnalyzer() *PhaseSyncAnalyzer {
	return &PhaseSyncAnalyzer{
		manifestParser:  parsers.NewPhaseManifestParser(),
		emgParser:       parsers.NewEMGParser(),
		motionParser:    parsers.NewMotionParser(),
		ancParser:       parsers.NewANCParser(),
		phaseCalculator: synchronizer.NewPhaseCalculator(),
		statsCalculator: calculator.NewEMGStatisticsCalculator(defaultEMGStatsPrecision),
		pathValidator:   security.NewPathValidator([]string{}),
	}
}

// validationContext 驗證上下文，用於在驗證步驟之間傳遞數據.
type validationContext struct {
	params      *models.AnalysisParams
	manifests   []models.PhaseManifest
	manifest    models.PhaseManifest
	baseFolder  string
	emgFilePath string
}

// validationStep 定義驗證步驟函數類型.
type validationStep func(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error

// validateManifestFile 驗證分期總檔案.
func validateManifestFile(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	manifests, err := analyzer.manifestParser.ParseFile(ctx.params.ManifestFile)
	if err != nil {
		return fmt.Errorf("解析分期總檔案失敗: %w", err)
	}

	ctx.manifests = manifests

	return nil
}

// validateSubjectIndex 驗證主題索引.
func validateSubjectIndex(_ *PhaseSyncAnalyzer, ctx *validationContext) error {
	if ctx.params.SubjectIndex < 0 || ctx.params.SubjectIndex >= len(ctx.manifests) {
		return fmt.Errorf("無效的主題索引: %d (共有 %d 個主題): %w",
			ctx.params.SubjectIndex, len(ctx.manifests), ErrInvalidSubjectIndex)
	}

	ctx.manifest = ctx.manifests[ctx.params.SubjectIndex]

	return nil
}

// validateManifestData 驗證分期總檔案數據.
func validateManifestData(_ *PhaseSyncAnalyzer, ctx *validationContext) error {
	if err := parsers.ValidatePhaseManifest(&ctx.manifest); err != nil {
		return fmt.Errorf("分期總檔案數據驗證失敗: %w", err)
	}

	return nil
}

// validatePhaseOrder 驗證分期點順序.
func validatePhaseOrder(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	if err := analyzer.phaseCalculator.ValidatePhaseOrder(ctx.params.StartPhase, ctx.params.EndPhase); err != nil {
		return fmt.Errorf("分期點順序驗證失敗: %w", err)
	}

	return nil
}

// validateEMGFilePath 驗證 EMG 檔案路徑.
func validateEMGFilePath(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	baseFolder := ctx.params.DataFolder
	if resolvedBase, err := filepath.EvalSymlinks(baseFolder); err == nil {
		baseFolder = resolvedBase
	}

	ctx.baseFolder = baseFolder
	analyzer.pathValidator.SetAllowedBasePaths([]string{baseFolder})

	emgFilePath, err := analyzer.pathValidator.GetSafePath(baseFolder, ctx.manifest.EMGFile)
	if err != nil {
		return fmt.Errorf("EMG 檔案路徑驗證失敗: %w", err)
	}

	// 解析符號連結並再次驗證
	if resolvedPath, err := filepath.EvalSymlinks(emgFilePath); err == nil {
		if err := analyzer.pathValidator.ValidateFilePath(resolvedPath); err != nil {
			return fmt.Errorf("EMG 檔案路徑驗證失敗: %w", err)
		}

		emgFilePath = resolvedPath
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("EMG 檔案路徑解析失敗: %w", err)
	}

	ctx.emgFilePath = emgFilePath

	return nil
}

// validateMotionFile 驗證 Motion 檔案.
//
//nolint:dupl // Similar to validateForceFile but handles different data type
func validateMotionFile(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	if ctx.manifest.MotionFile == "" {
		return nil
	}

	motionFilePath, err := analyzer.pathValidator.GetSafePath(ctx.baseFolder, ctx.manifest.MotionFile)
	if err != nil {
		return fmt.Errorf("Motion 檔案路徑驗證失敗: %w", err) //nolint:stylecheck // Chinese error message
	}

	if _, err := os.Stat(motionFilePath); os.IsNotExist(err) {
		//nolint:stylecheck // Chinese error message for user display
		return fmt.Errorf("Motion 檔案不存在 (%s): %w", motionFilePath, ErrFileNotFound)
	}

	motionData, err := analyzer.motionParser.ParseFile(motionFilePath)
	if err != nil {
		return fmt.Errorf("解析 Motion 檔案失敗: %w", err)
	}

	maxMotionIndex := 0
	if len(motionData.Indices) > 0 {
		maxMotionIndex = motionData.Indices[len(motionData.Indices)-1]
	}

	return validateMotionPhasePoints(&ctx.manifest, maxMotionIndex)
}

// validateMotionPhasePoints 驗證 Motion 相關分期點.
func validateMotionPhasePoints(manifest *models.PhaseManifest, maxMotionIndex int) error {
	if manifest.PhasePoints.D > 0 && manifest.PhasePoints.D > maxMotionIndex {
		//nolint:stylecheck // Chinese error message for user display
		return fmt.Errorf("D 分期點 index %d 超出 Motion 數據範圍 (最大: %d): %w",
			manifest.PhasePoints.D, maxMotionIndex, ErrPhasePointOutOfRange)
	}

	if manifest.PhasePoints.O > 0 && manifest.PhasePoints.O > maxMotionIndex {
		//nolint:stylecheck // Chinese error message for user display
		return fmt.Errorf("O 分期點 index %d 超出 Motion 數據範圍 (最大: %d): %w",
			manifest.PhasePoints.O, maxMotionIndex, ErrPhasePointOutOfRange)
	}

	if manifest.EMGMotionOffset > 0 && manifest.EMGMotionOffset > maxMotionIndex {
		return fmt.Errorf("EMGMotionOffset %d 超出 Motion 數據範圍 (最大: %d): %w",
			manifest.EMGMotionOffset, maxMotionIndex, ErrPhasePointOutOfRange)
	}

	return nil
}

// validateForceFile 驗證 Force Plate 檔案.
//
//nolint:dupl // Similar to validateMotionFile but handles different data type
func validateForceFile(analyzer *PhaseSyncAnalyzer, ctx *validationContext) error {
	if ctx.manifest.ForceFile == "" {
		return nil
	}

	forceFilePath, err := analyzer.pathValidator.GetSafePath(ctx.baseFolder, ctx.manifest.ForceFile)
	if err != nil {
		return fmt.Errorf("Force Plate 檔案路徑驗證失敗: %w", err) //nolint:stylecheck // Chinese error message
	}

	if _, err := os.Stat(forceFilePath); os.IsNotExist(err) {
		//nolint:stylecheck // Chinese error message for user display
		return fmt.Errorf("Force Plate 檔案不存在 (%s): %w", forceFilePath, ErrFileNotFound)
	}

	forceData, err := analyzer.ancParser.ParseFile(forceFilePath)
	if err != nil {
		return fmt.Errorf("解析 Force Plate 檔案失敗: %w", err)
	}

	maxForceTime := 0.0
	if len(forceData.Time) > 0 {
		maxForceTime = forceData.Time[len(forceData.Time)-1]
	}

	return validateForcePhasePoints(&ctx.manifest, maxForceTime)
}

// validateForcePhasePoints 驗證 Force Plate 相關分期點.
func validateForcePhasePoints(manifest *models.PhaseManifest, maxForceTime float64) error {
	forceTimePoints := []struct {
		name  string
		value float64
	}{
		{"P0", manifest.PhasePoints.P0},
		{"P1", manifest.PhasePoints.P1},
		{"P2", manifest.PhasePoints.P2},
		{"S", manifest.PhasePoints.S},
		{"C", manifest.PhasePoints.C},
		{"T0", manifest.PhasePoints.T0},
		{"T", manifest.PhasePoints.T},
		{"L", manifest.PhasePoints.L},
	}

	for _, point := range forceTimePoints {
		if point.value > 0 && point.value > maxForceTime {
			return fmt.Errorf("%s 分期點時間 %.3f 超出 Force Plate 數據範圍 (最大: %.3f): %w",
				point.name, point.value, maxForceTime, ErrPhasePointOutOfRange)
		}
	}

	return nil
}

// runValidationPipeline 執行驗證管線.
func (analyzer *PhaseSyncAnalyzer) runValidationPipeline(ctx *validationContext) error {
	steps := []validationStep{
		validateManifestFile,
		validateSubjectIndex,
		validateManifestData,
		validatePhaseOrder,
		validateEMGFilePath,
		validateMotionFile,
		validateForceFile,
	}

	for _, step := range steps {
		if err := step(analyzer, ctx); err != nil {
			return err
		}
	}

	return nil
}

// LoadedPhaseSyncContext 表示完成驗證與 EMG 解析的中間結果，供需要相同前置步驟
// 的多個分析流程共用。PhaseTimeRange 由 LoadAndExtractRange 在內部一併計算後填入；
// 直接呼叫 Load 取得的 context 中 PhaseTimeRange 為 nil，需後續呼叫 ResolvePhaseRange
// 取得具體區間。
type LoadedPhaseSyncContext struct {
	Manifest       *models.PhaseManifest
	EMGData        *models.PhaseSyncEMGData
	PhaseTimeRange *models.PhaseTimeRange
}

// Load 執行分期同步分析的前置載入流程：路徑驗證、manifest 解析、EMG 檔案解析。
// 不計算任何分期時間範圍（PhaseTimeRange 為 nil），供需要對同一份載入資料解析多組
// 不同分期區間的工作流共用（例如標準化分期同步分析需要分別解析 normalize 範圍與
// statistics 範圍）。後續呼叫 ResolvePhaseRange 取得具體時間區間。
func (analyzer *PhaseSyncAnalyzer) Load(
	params *models.AnalysisParams,
) (*LoadedPhaseSyncContext, error) {
	// 1. 設置允許的基礎路徑
	analyzer.pathValidator.SetAllowedBasePaths([]string{params.DataFolder})

	// 2. 執行驗證管線
	ctx := &validationContext{params: params}
	if err := analyzer.runValidationPipeline(ctx); err != nil {
		return nil, err
	}

	// 3. 解析 EMG 檔案
	emgData, err := analyzer.emgParser.ParseFile(ctx.emgFilePath)
	if err != nil {
		return nil, fmt.Errorf("解析 EMG 檔案失敗: %w", err)
	}

	manifestCopy := ctx.manifest

	return &LoadedPhaseSyncContext{
		Manifest:       &manifestCopy,
		EMGData:        emgData,
		PhaseTimeRange: nil,
	}, nil
}

// ResolvePhaseRange 從已 Load 的 context 解析指定的一對分期點為時間範圍，
// 並驗證該範圍落在 EMG 資料時間範圍內。同一個 LoadedPhaseSyncContext 可重複呼叫
// 此函式以取得多組不同分期區間。
//
// 先呼叫 phaseCalculator.ValidatePhaseOrder 顯式驗證 start < end（嚴格小於）
// 與 phase 名稱合法性。這個驗證對「直接走 Load + ResolvePhaseRange」的呼叫端
// （例如標準化分期同步分析對 Stats 那組區間）至關重要——否則 start == end 的
// 退化情形會穿透到 GetPhaseTimeRange，產生 zero-duration 區間。對「走
// LoadAndExtractRange」的呼叫端則與 Load 內 pipeline 的 validatePhaseOrder
// 形成冗餘檢查（無害）。
func (analyzer *PhaseSyncAnalyzer) ResolvePhaseRange(
	loaded *LoadedPhaseSyncContext,
	startPhase, endPhase string,
) (*models.PhaseTimeRange, error) {
	if err := analyzer.phaseCalculator.ValidatePhaseOrder(startPhase, endPhase); err != nil {
		return nil, fmt.Errorf("分期點順序驗證失敗: %w", err)
	}

	phaseTimeRange, err := analyzer.phaseCalculator.GetPhaseTimeRange(
		loaded.Manifest.PhasePoints,
		startPhase,
		endPhase,
		loaded.Manifest.EMGMotionOffset,
	)
	if err != nil {
		return nil, fmt.Errorf("計算分期時間範圍失敗: %w", err)
	}

	if err := validateEMGTimeRange(loaded.EMGData, phaseTimeRange, loaded.Manifest.EMGMotionOffset); err != nil {
		return nil, err
	}

	return phaseTimeRange, nil
}

// LoadAndExtractRange 為 Load + ResolvePhaseRange 的薄包裝，保留給只需要單一
// 分期區間的呼叫端使用（例如 AnalyzePhaseSync）。需要多組區間時請改用 Load
// 搭配多次 ResolvePhaseRange。
func (analyzer *PhaseSyncAnalyzer) LoadAndExtractRange(
	params *models.AnalysisParams,
) (*LoadedPhaseSyncContext, error) {
	loaded, err := analyzer.Load(params)
	if err != nil {
		return nil, err
	}

	phaseTimeRange, err := analyzer.ResolvePhaseRange(loaded, params.StartPhase, params.EndPhase)
	if err != nil {
		return nil, err
	}

	loaded.PhaseTimeRange = phaseTimeRange

	return loaded, nil
}

// AnalyzePhaseSync 執行分期同步分析.
func (analyzer *PhaseSyncAnalyzer) AnalyzePhaseSync(params *models.AnalysisParams) (*models.EMGStatistics, error) {
	loaded, err := analyzer.LoadAndExtractRange(params)
	if err != nil {
		return nil, err
	}

	// 6. 提取指定時間範圍的 EMG 數據
	rangeResult, err := analyzer.emgParser.GetDataInTimeRange(
		loaded.EMGData,
		loaded.PhaseTimeRange.StartTime,
		loaded.PhaseTimeRange.EndTime,
	)
	if err != nil {
		return nil, fmt.Errorf("提取 EMG 時間範圍數據失敗: %w", err)
	}

	// 7. 計算統計信息
	stats, err := analyzer.statsCalculator.CalculateStatistics(
		rangeResult.Data,
		params.StartPhase,
		rangeResult.ActualStartTime,
		params.EndPhase,
		rangeResult.ActualEndTime,
		loaded.Manifest.Subject,
	)
	if err != nil {
		return nil, fmt.Errorf("計算統計信息失敗: %w", err)
	}

	return stats, nil
}

// validateEMGTimeRange 驗證 EMG 時間範圍.
func validateEMGTimeRange(
	emgData *models.PhaseSyncEMGData,
	phaseTimeRange *models.PhaseTimeRange,
	emgMotionOffset int,
) error {
	emgMinTime := 0.0
	emgMaxTime := 0.0

	if len(emgData.Time) > 0 {
		emgMinTime = emgData.Time[0]
		emgMaxTime = emgData.Time[len(emgData.Time)-1]
	}

	if phaseTimeRange.StartTime < emgMinTime {
		return fmt.Errorf(
			"計算出的 EMG 開始時間 %.3f 小於 EMG 數據最小時間 %.3f (offset: %d): %w",
			phaseTimeRange.StartTime, emgMinTime, emgMotionOffset, ErrEMGTimeOutOfRange)
	}

	if phaseTimeRange.EndTime > emgMaxTime {
		return fmt.Errorf(
			"計算出的 EMG 結束時間 %.3f 超出 EMG 數據範圍 (最大: %.3f, offset: %d): %w",
			phaseTimeRange.EndTime, emgMaxTime, emgMotionOffset, ErrEMGTimeOutOfRange)
	}

	return nil
}

// ExportResults 導出分析結果.
func (analyzer *PhaseSyncAnalyzer) ExportResults(
	stats *models.EMGStatistics,
	outputDir string,
) (string, error) {
	// 生成輸出檔案名
	fileName := calculator.GenerateOutputFileName(
		stats.Subject,
		stats.StartPhase,
		stats.EndPhase,
	)

	outputPath := filepath.Join(outputDir, fileName)

	// 導出 CSV
	if err := analyzer.statsCalculator.ExportToCSV(stats, outputPath); err != nil {
		return "", fmt.Errorf("導出 CSV 失敗: %w", err)
	}

	return outputPath, nil
}

// LoadManifestSubjects 載入分期總檔案中的所有主題.
func (analyzer *PhaseSyncAnalyzer) LoadManifestSubjects(manifestPath string) ([]string, error) {
	manifests, err := analyzer.manifestParser.ParseFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("解析分期總檔案失敗: %w", err)
	}

	subjects := make([]string, len(manifests))
	for i := range manifests {
		subjects[i] = manifests[i].Subject
	}

	return subjects, nil
}

// FindDataFiles 在指定資料夾中查找數據檔案.
func FindDataFiles(folder string, patterns []string) ([]string, error) {
	var files []string

	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(folder, pattern))
		if err != nil {
			return nil, fmt.Errorf("搜索檔案失敗 (pattern: %s): %w", pattern, err)
		}

		files = append(files, matches...)
	}

	// 移除重複項目
	uniqueFiles := make(map[string]bool)

	var result []string

	for _, file := range files {
		if !uniqueFiles[file] {
			uniqueFiles[file] = true

			result = append(result, file)
		}
	}

	return result, nil
}

// AnalysisResult 分析結果.
type AnalysisResult struct {
	Statistics  *models.EMGStatistics
	OutputPath  string
	Report      string
	ElapsedTime float64
}

// GenerateAnalysisReport 生成分析報告.
func GenerateAnalysisReport(stats *models.EMGStatistics) string {
	return calculator.FormatStatisticsReport(stats)
}

// ValidateDataFiles 驗證數據檔案是否存在.
func ValidateDataFiles(dataFolder string, manifest *models.PhaseManifest) error {
	// 檢查 EMG 檔案
	emgPath := filepath.Join(dataFolder, manifest.EMGFile)
	if !fileExists(emgPath) {
		return fmt.Errorf("找不到 EMG 檔案 (%s): %w", emgPath, ErrFileNotFound)
	}

	// 檢查 Motion 檔案
	motionPath := filepath.Join(dataFolder, manifest.MotionFile)
	if !fileExists(motionPath) {
		return fmt.Errorf("找不到 Motion 檔案 (%s): %w", motionPath, ErrFileNotFound)
	}

	// 檢查力板檔案
	forcePath := filepath.Join(dataFolder, manifest.ForceFile)
	if !fileExists(forcePath) {
		return fmt.Errorf("找不到力板檔案 (%s): %w", forcePath, ErrFileNotFound)
	}

	return nil
}

// fileExists 檢查檔案是否存在.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
