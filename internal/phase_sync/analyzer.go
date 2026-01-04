package phase_sync

import (
	"fmt"
	"os"
	"path/filepath"

	"count_mean/internal/calculator"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
	"count_mean/internal/synchronizer"
)

// PhaseSyncAnalyzer 分期同步分析器
type PhaseSyncAnalyzer struct {
	manifestParser  *parsers.PhaseManifestParser
	emgParser       *parsers.EMGParser
	motionParser    *parsers.MotionParser
	ancParser       *parsers.ANCParser
	phaseCalculator *synchronizer.PhaseCalculator
	statsCalculator *calculator.EMGStatisticsCalculator
	pathValidator   *security.PathValidator
}

// NewPhaseSyncAnalyzer 創建新的分期同步分析器
func NewPhaseSyncAnalyzer() *PhaseSyncAnalyzer {
	return &PhaseSyncAnalyzer{
		manifestParser:  parsers.NewPhaseManifestParser(),
		emgParser:       parsers.NewEMGParser(),
		motionParser:    parsers.NewMotionParser(),
		ancParser:       parsers.NewANCParser(),
		phaseCalculator: synchronizer.NewPhaseCalculator(),
		statsCalculator: calculator.NewEMGStatisticsCalculator(6),
		pathValidator:   security.NewPathValidator([]string{}),
	}
}

// AnalyzePhaseSync 執行分期同步分析
func (analyzer *PhaseSyncAnalyzer) AnalyzePhaseSync(params *models.AnalysisParams) (*models.EMGStatistics, error) {
	// 1. 設置允許的基礎路徑為數據資料夾
	analyzer.pathValidator.SetAllowedBasePaths([]string{params.DataFolder})

	// 2. 解析分期總檔案
	manifests, err := analyzer.manifestParser.ParseFile(params.ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("解析分期總檔案失敗: %w", err)
	}

	// 3. 驗證主題索引
	if params.SubjectIndex < 0 || params.SubjectIndex >= len(manifests) {
		return nil, fmt.Errorf("無效的主題索引: %d (共有 %d 個主題)",
			params.SubjectIndex, len(manifests))
	}

	manifest := manifests[params.SubjectIndex]

	// 4. 驗證分期總檔案數據
	if err := parsers.ValidatePhaseManifest(manifest); err != nil {
		return nil, fmt.Errorf("分期總檔案數據驗證失敗: %w", err)
	}

	// 5. 驗證分期點順序
	if err := analyzer.phaseCalculator.ValidatePhaseOrder(params.StartPhase, params.EndPhase); err != nil {
		return nil, err
	}

	// 6. 構建並驗證 EMG 檔案路徑（限制於資料夾範圍內）
	baseFolder := params.DataFolder
	if resolvedBase, err := filepath.EvalSymlinks(baseFolder); err == nil {
		baseFolder = resolvedBase
	}

	analyzer.pathValidator.SetAllowedBasePaths([]string{baseFolder})

	emgFilePath, err := analyzer.pathValidator.GetSafePath(baseFolder, manifest.EMGFile)
	if err != nil {
		return nil, fmt.Errorf("EMG 檔案路徑驗證失敗: %w", err)
	}

	// 解析符號連結並再次驗證，避免透過 symlink 繞過目錄限制
	if resolvedPath, err := filepath.EvalSymlinks(emgFilePath); err == nil {
		if err := analyzer.pathValidator.ValidateFilePath(resolvedPath); err != nil {
			return nil, fmt.Errorf("EMG 檔案路徑驗證失敗: %w", err)
		}
		emgFilePath = resolvedPath
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("EMG 檔案路徑解析失敗: %w", err)
	}

	// 6.1 構建並驗證 Motion 檔案路徑及內容
	if manifest.MotionFile != "" {
		motionFilePath, err := analyzer.pathValidator.GetSafePath(baseFolder, manifest.MotionFile)
		if err != nil {
			return nil, fmt.Errorf("Motion 檔案路徑驗證失敗: %w", err)
		}
		if _, err := os.Stat(motionFilePath); os.IsNotExist(err) {
			return nil, fmt.Errorf("Motion 檔案不存在: %s", motionFilePath)
		}

		// 解析 Motion 檔案內容進行驗證
		motionData, err := analyzer.motionParser.ParseFile(motionFilePath)
		if err != nil {
			return nil, fmt.Errorf("解析 Motion 檔案失敗: %w", err)
		}

		// 獲取最大 Motion index
		maxMotionIndex := 0
		if len(motionData.Indices) > 0 {
			maxMotionIndex = motionData.Indices[len(motionData.Indices)-1]
		}

		// 驗證 D 分期點 (motion index 類型)
		if manifest.PhasePoints.D > 0 && manifest.PhasePoints.D > maxMotionIndex {
			return nil, fmt.Errorf("D 分期點 index %d 超出 Motion 數據範圍 (最大: %d)",
				manifest.PhasePoints.D, maxMotionIndex)
		}

		// 驗證 O 分期點 (motion index 類型)
		if manifest.PhasePoints.O > 0 && manifest.PhasePoints.O > maxMotionIndex {
			return nil, fmt.Errorf("O 分期點 index %d 超出 Motion 數據範圍 (最大: %d)",
				manifest.PhasePoints.O, maxMotionIndex)
		}

		// 驗證 EMGMotionOffset 是否在 Motion 數據範圍內
		if manifest.EMGMotionOffset > 0 && manifest.EMGMotionOffset > maxMotionIndex {
			return nil, fmt.Errorf("EMGMotionOffset %d 超出 Motion 數據範圍 (最大: %d)",
				manifest.EMGMotionOffset, maxMotionIndex)
		}
	}

	// 6.2 構建並驗證 Force Plate 檔案路徑及內容
	if manifest.ForceFile != "" {
		forceFilePath, err := analyzer.pathValidator.GetSafePath(baseFolder, manifest.ForceFile)
		if err != nil {
			return nil, fmt.Errorf("Force Plate 檔案路徑驗證失敗: %w", err)
		}
		if _, err := os.Stat(forceFilePath); os.IsNotExist(err) {
			return nil, fmt.Errorf("Force Plate 檔案不存在: %s", forceFilePath)
		}

		// 解析 Force Plate 檔案內容進行驗證
		forceData, err := analyzer.ancParser.ParseFile(forceFilePath)
		if err != nil {
			return nil, fmt.Errorf("解析 Force Plate 檔案失敗: %w", err)
		}

		// 獲取最大時間
		maxForceTime := 0.0
		if len(forceData.Time) > 0 {
			maxForceTime = forceData.Time[len(forceData.Time)-1]
		}

		// 驗證各個 Force Plate 時間點
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
				return nil, fmt.Errorf("%s 分期點時間 %.3f 超出 Force Plate 數據範圍 (最大: %.3f)",
					point.name, point.value, maxForceTime)
			}
		}
	}

	// 7. 解析 EMG 檔案
	emgData, err := analyzer.emgParser.ParseFile(emgFilePath)
	if err != nil {
		return nil, fmt.Errorf("解析 EMG 檔案失敗: %w", err)
	}

	// 8. 計算分期時間範圍
	phaseTimeRange, err := analyzer.phaseCalculator.GetPhaseTimeRange(
		manifest.PhasePoints,
		params.StartPhase,
		params.EndPhase,
		manifest.EMGMotionOffset,
	)
	if err != nil {
		return nil, fmt.Errorf("計算分期時間範圍失敗: %w", err)
	}

	// 8.1 驗證計算出的 EMG 時間範圍是否在 EMG 數據範圍內
	emgMinTime := 0.0
	emgMaxTime := 0.0
	if len(emgData.Time) > 0 {
		emgMinTime = emgData.Time[0]
		emgMaxTime = emgData.Time[len(emgData.Time)-1]
	}

	if phaseTimeRange.StartTime < emgMinTime {
		return nil, fmt.Errorf("計算出的 EMG 開始時間 %.3f 小於 EMG 數據最小時間 %.3f (EMGMotionOffset: %d)",
			phaseTimeRange.StartTime, emgMinTime, manifest.EMGMotionOffset)
	}
	if phaseTimeRange.EndTime > emgMaxTime {
		return nil, fmt.Errorf("計算出的 EMG 結束時間 %.3f 超出 EMG 數據範圍 (最大: %.3f, EMGMotionOffset: %d)",
			phaseTimeRange.EndTime, emgMaxTime, manifest.EMGMotionOffset)
	}

	// 9. 提取指定時間範圍的 EMG 數據
	rangeEMGData, err := analyzer.emgParser.GetDataInTimeRange(
		emgData,
		phaseTimeRange.StartTime,
		phaseTimeRange.EndTime,
	)
	if err != nil {
		return nil, fmt.Errorf("提取 EMG 時間範圍數據失敗: %w", err)
	}

	// 10. 計算統計信息
	stats, err := analyzer.statsCalculator.CalculateStatistics(
		rangeEMGData,
		params.StartPhase,
		phaseTimeRange.StartTime,
		params.EndPhase,
		phaseTimeRange.EndTime,
		manifest.Subject,
	)
	if err != nil {
		return nil, fmt.Errorf("計算統計信息失敗: %w", err)
	}

	return stats, nil
}

// ExportResults 導出分析結果
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

// LoadManifestSubjects 載入分期總檔案中的所有主題
func (analyzer *PhaseSyncAnalyzer) LoadManifestSubjects(manifestPath string) ([]string, error) {
	manifests, err := analyzer.manifestParser.ParseFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("解析分期總檔案失敗: %w", err)
	}

	subjects := make([]string, len(manifests))
	for i, manifest := range manifests {
		subjects[i] = manifest.Subject
	}

	return subjects, nil
}

// FindDataFiles 在指定資料夾中查找數據檔案
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

// AnalysisResult 分析結果
type AnalysisResult struct {
	Statistics  *models.EMGStatistics
	OutputPath  string
	Report      string
	ElapsedTime float64
}

// GenerateAnalysisReport 生成分析報告
func GenerateAnalysisReport(stats *models.EMGStatistics) string {
	return calculator.FormatStatisticsReport(stats)
}

// ValidateDataFiles 驗證數據檔案是否存在
func ValidateDataFiles(dataFolder string, manifest models.PhaseManifest) error {
	// 檢查 EMG 檔案
	emgPath := filepath.Join(dataFolder, manifest.EMGFile)
	if !fileExists(emgPath) {
		return fmt.Errorf("找不到 EMG 檔案: %s", emgPath)
	}

	// 檢查 Motion 檔案
	motionPath := filepath.Join(dataFolder, manifest.MotionFile)
	if !fileExists(motionPath) {
		return fmt.Errorf("找不到 Motion 檔案: %s", motionPath)
	}

	// 檢查力板檔案
	forcePath := filepath.Join(dataFolder, manifest.ForceFile)
	if !fileExists(forcePath) {
		return fmt.Errorf("找不到力板檔案: %s", forcePath)
	}

	return nil
}

// fileExists 檢查檔案是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
