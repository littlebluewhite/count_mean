// Package phase_sync provides phase synchronization analysis for EMG, motion,
// and force plate data. It coordinates data validation, time synchronization,
// and statistical calculations across multiple data sources.
//
//nolint:revive // package name with underscore maintained for backward compatibility
package phase_sync

import (
	"context"
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
}

// NewPhaseSyncAnalyzer 創建新的分期同步分析器.
// 不持有 PathValidator — 改為每個分析請求在 validateEMGFilePath 內基於 baseFolder 建立。
func NewPhaseSyncAnalyzer() *PhaseSyncAnalyzer {
	return &PhaseSyncAnalyzer{
		manifestParser:  parsers.NewPhaseManifestParser(),
		emgParser:       parsers.NewEMGParser(),
		motionParser:    parsers.NewMotionParser(),
		ancParser:       parsers.NewANCParser(),
		phaseCalculator: synchronizer.NewPhaseCalculator(),
		statsCalculator: calculator.NewEMGStatisticsCalculator(defaultEMGStatsPrecision),
	}
}

// validationContext 驗證上下文，用於在驗證步驟之間傳遞數據.
// pathValidator 在 validateEMGFilePath 內基於 baseFolder 建立，後續驗證共用，
// 改為 request-scoped 後 analyzer 不再持有 PathValidator 欄位 — 避免並發污染。
type validationContext struct {
	params        *models.AnalysisParams
	manifests     []models.PhaseManifest
	manifest      models.PhaseManifest
	baseFolder    string
	emgFilePath   string
	pathValidator *security.PathValidator
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
// 第一個用到 PathValidator 的步驟負責 lazily 建立 request-scoped instance；
// 後續 validateMotionFile / validateForceFile 共用 ctx.pathValidator。
func validateEMGFilePath(_ *PhaseSyncAnalyzer, ctx *validationContext) error {
	baseFolder := ctx.params.DataFolder
	if resolvedBase, err := filepath.EvalSymlinks(baseFolder); err == nil {
		baseFolder = resolvedBase
	}

	ctx.baseFolder = baseFolder
	ctx.pathValidator = security.NewPathValidator([]string{baseFolder})

	emgFilePath, err := ctx.pathValidator.GetSafePath(baseFolder, ctx.manifest.EMGFile)
	if err != nil {
		return fmt.Errorf("EMG 檔案路徑驗證失敗: %w", err)
	}

	// 解析符號連結並再次驗證
	if resolvedPath, err := filepath.EvalSymlinks(emgFilePath); err == nil {
		if err := ctx.pathValidator.ValidateFilePath(resolvedPath); err != nil {
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

	motionFilePath, err := ctx.pathValidator.GetSafePath(ctx.baseFolder, ctx.manifest.MotionFile)
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

	forceFilePath, err := ctx.pathValidator.GetSafePath(ctx.baseFolder, ctx.manifest.ForceFile)
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
// 只檢 force-time phase（!IsMotionIndex()），motion-index 的 D 與 O 由
// validateMotionPhasePoints 另行驗證。用 PhasePoint.IsMotionIndex() 集中
// domain invariant，避免逐項硬編碼 "P0"/"P1"/.../"L"。
func validateForcePhasePoints(manifest *models.PhaseManifest, maxForceTime float64) error {
	for _, phase := range models.AllPhases() {
		if phase.IsMotionIndex() {
			continue
		}

		value, _, _ := parsers.GetPhaseValue(&manifest.PhasePoints, phase)
		if value > 0 && value > maxForceTime {
			return fmt.Errorf("%s 分期點時間 %.3f 超出 Force Plate 數據範圍 (最大: %.3f): %w",
				phase, value, maxForceTime, ErrPhasePointOutOfRange)
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

// LoadedPhaseSyncContext 表示完成驗證、EMG 解析與分期時間區間計算的中間結果，
// 供需要相同前置步驟的多個分析流程共用。
type LoadedPhaseSyncContext struct {
	Manifest       *models.PhaseManifest
	EMGData        *models.PhaseSyncEMGData
	PhaseTimeRange *models.PhaseTimeRange
}

// LoadAndExtractRange 執行分期同步分析的前置流程：路徑驗證、manifest 解析、
// EMG 檔案解析、分期時間區間計算與 EMG 時間範圍驗證。回傳給多個下游分析共用。
// validateEMGFilePath 內部會 lazily 建立 request-scoped PathValidator 存入 ctx，
// 不再對 analyzer-level singleton 做 SetAllowedBasePaths。
func (analyzer *PhaseSyncAnalyzer) LoadAndExtractRange(
	params *models.AnalysisParams,
) (*LoadedPhaseSyncContext, error) {
	ctx := &validationContext{params: params}
	if err := analyzer.runValidationPipeline(ctx); err != nil {
		return nil, err
	}

	emgData, err := analyzer.emgParser.ParseFile(ctx.emgFilePath)
	if err != nil {
		return nil, fmt.Errorf("解析 EMG 檔案失敗: %w", err)
	}

	phaseTimeRange, err := analyzer.phaseCalculator.GetPhaseTimeRange(
		ctx.manifest.PhasePoints,
		params.StartPhase,
		params.EndPhase,
		ctx.manifest.EMGMotionOffset,
	)
	if err != nil {
		return nil, fmt.Errorf("計算分期時間範圍失敗: %w", err)
	}

	if err := validateEMGTimeRange(emgData, phaseTimeRange, ctx.manifest.EMGMotionOffset); err != nil {
		return nil, err
	}

	manifestCopy := ctx.manifest

	return &LoadedPhaseSyncContext{
		Manifest:       &manifestCopy,
		EMGData:        emgData,
		PhaseTimeRange: phaseTimeRange,
	}, nil
}

// AnalyzePhaseSync 執行分期同步分析。
//
// ctx 在每個 step 邊界檢查（load / extract / stats），caller 可在分析中途取消
// 整段工作（Wails Shutdown / 使用者中止）。內部的 file load 與 stats 計算本身
// 仍同步執行，未來若改為 streaming 可再深度 plumb ctx。
func (analyzer *PhaseSyncAnalyzer) AnalyzePhaseSync(
	ctx context.Context, params *models.AnalysisParams,
) (*models.EMGStatistics, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	loaded, err := analyzer.LoadAndExtractRange(params)
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
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

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 7. 計算統計信息
	stats, err := analyzer.statsCalculator.CalculateStatistics(
		rangeResult.Data,
		calculator.StatisticsParams{
			Subject:    loaded.Manifest.Subject,
			StartPhase: params.StartPhase,
			StartTime:  rangeResult.ActualStartTime,
			EndPhase:   params.EndPhase,
			EndTime:    rangeResult.ActualEndTime,
		},
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

// GenerateAnalysisReport 生成分析報告.
func GenerateAnalysisReport(stats *models.EMGStatistics) string {
	return calculator.FormatStatisticsReport(stats)
}
