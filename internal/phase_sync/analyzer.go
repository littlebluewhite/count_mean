package phase_sync

import (
	"fmt"
	"os"
	"path/filepath"

	"count_mean/internal/calculator"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
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
	}
}

// AnalyzePhaseSync 執行分期同步分析
func (analyzer *PhaseSyncAnalyzer) AnalyzePhaseSync(params *models.AnalysisParams) (*models.EMGStatistics, error) {
	// 1. 解析分期總檔案
	manifests, err := analyzer.manifestParser.ParseFile(params.ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("解析分期總檔案失敗: %w", err)
	}

	// 2. 驗證主題索引
	if params.SubjectIndex < 0 || params.SubjectIndex >= len(manifests) {
		return nil, fmt.Errorf("無效的主題索引: %d (共有 %d 個主題)",
			params.SubjectIndex, len(manifests))
	}

	manifest := manifests[params.SubjectIndex]

	// 3. 驗證分期總檔案數據
	if err := parsers.ValidatePhaseManifest(manifest); err != nil {
		return nil, fmt.Errorf("分期總檔案數據驗證失敗: %w", err)
	}

	// 4. 驗證分期點順序
	if err := analyzer.phaseCalculator.ValidatePhaseOrder(params.StartPhase, params.EndPhase); err != nil {
		return nil, err
	}

	// 5. 構建檔案路徑
	var emgFilePath string

	// 檢查是否為絕對路徑
	if filepath.IsAbs(manifest.EMGFile) {
		// 如果是絕對路徑，直接使用
		emgFilePath = manifest.EMGFile
	} else {
		// 如果是相對路徑，與資料夾組合
		emgFilePath = filepath.Join(params.DataFolder, manifest.EMGFile)
	}

	// motionFilePath := filepath.Join(params.DataFolder, manifest.MotionFile)
	// forceFilePath := filepath.Join(params.DataFolder, manifest.ForceFile)

	// 6. 解析 EMG 檔案
	emgData, err := analyzer.emgParser.ParseFile(emgFilePath)
	if err != nil {
		return nil, fmt.Errorf("解析 EMG 檔案失敗: %w", err)
	}

	// 7. 計算分期時間範圍
	phaseTimeRange, err := analyzer.phaseCalculator.GetPhaseTimeRange(
		manifest.PhasePoints,
		params.StartPhase,
		params.EndPhase,
		manifest.EMGMotionOffset,
	)
	if err != nil {
		return nil, fmt.Errorf("計算分期時間範圍失敗: %w", err)
	}

	// 8. 提取指定時間範圍的 EMG 數據
	rangeEMGData, err := analyzer.emgParser.GetDataInTimeRange(
		emgData,
		phaseTimeRange.StartTime,
		phaseTimeRange.EndTime,
	)
	if err != nil {
		return nil, fmt.Errorf("提取 EMG 時間範圍數據失敗: %w", err)
	}

	// 9. 計算統計信息
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
