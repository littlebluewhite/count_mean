package cci

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
	"count_mean/internal/synchronizer"
)

// File permission constants.
const (
	dirPermission  = 0o750
	filePermission = 0o600
)

// CCIParams holds parameters for CCI analysis.
type CCIParams struct {
	ManifestFile string
	DataFolder   string
	SubjectIndex int
}

// CCIAnalyzer orchestrates the CCI Rudolph analysis pipeline.
type CCIAnalyzer struct {
	manifestParser   *parsers.PhaseManifestParser
	emgParser        *parsers.EMGParser
	timeSynchronizer *synchronizer.TimeSynchronizer
	pathValidator    *security.PathValidator
	logger           *logging.Logger
}

// NewCCIAnalyzer creates a new CCI analyzer instance.
func NewCCIAnalyzer() *CCIAnalyzer {
	return &CCIAnalyzer{
		manifestParser:   parsers.NewPhaseManifestParser(),
		emgParser:        parsers.NewEMGParser(),
		timeSynchronizer: synchronizer.NewTimeSynchronizer(),
		pathValidator:    security.NewPathValidator([]string{}),
		logger:           logging.GetLogger("cci_analyzer"),
	}
}

// AnalyzeCCI executes the full CCI Rudolph analysis pipeline.
func (a *CCIAnalyzer) AnalyzeCCI(params *CCIParams) (*CCIAnalysisResult, error) {
	a.logger.Info("開始 CCI Rudolph 分析", map[string]interface{}{
		"manifest": params.ManifestFile,
		"folder":   params.DataFolder,
		"subject":  params.SubjectIndex,
	})

	// 1. Parse manifest and validate subject
	manifest, err := a.loadAndValidate(params)
	if err != nil {
		return nil, err
	}

	// 2. Resolve and parse EMG file
	emgData, err := a.loadEMGData(params.DataFolder, manifest)
	if err != nil {
		return nil, err
	}

	// 3. Build channel mapping
	channelMap, err := BuildChannelMap(emgData.Headers)
	if err != nil {
		return nil, fmt.Errorf("建立通道映射失敗: %w", err)
	}

	// 4. Calculate gait cycle boundaries and phase percentages
	gaitStart, gaitEnd, phasePercents, phaseTimes, err := a.calculateGaitCycle(
		manifest, emgData)
	if err != nil {
		return nil, err
	}

	// 5. Extract EMG data within gait cycle
	rangeResult, err := a.emgParser.GetDataInTimeRange(emgData, gaitStart, gaitEnd)
	if err != nil {
		return nil, fmt.Errorf("提取步態週期 EMG 數據失敗: %w", err)
	}

	// 6. Calculate CCI for all muscle pairs (raw time points, no normalization)
	result := a.computeAllPairs(
		manifest.Subject, rangeResult.Data, channelMap, phasePercents, phaseTimes)
	result.GaitStartTime = gaitStart
	result.GaitEndTime = gaitEnd

	a.logger.Info("CCI 分析完成", map[string]interface{}{"subject": manifest.Subject})

	return result, nil
}

// loadAndValidate parses the manifest and validates the subject index.
//
//nolint:err113 // dynamic errors for user-facing output
func (a *CCIAnalyzer) loadAndValidate(params *CCIParams) (*models.PhaseManifest, error) {
	manifests, err := a.manifestParser.ParseFile(params.ManifestFile)
	if err != nil {
		return nil, fmt.Errorf("解析分期總檔案失敗: %w", err)
	}

	if params.SubjectIndex < 0 || params.SubjectIndex >= len(manifests) {
		return nil, fmt.Errorf(
			"無效的主題索引: %d (共有 %d 個主題)", params.SubjectIndex, len(manifests))
	}

	return &manifests[params.SubjectIndex], nil
}

// loadEMGData resolves EMG file path and parses EMG data.
func (a *CCIAnalyzer) loadEMGData(
	dataFolder string, manifest *models.PhaseManifest,
) (*models.PhaseSyncEMGData, error) {
	baseFolder := dataFolder
	if resolved, err := filepath.EvalSymlinks(baseFolder); err == nil {
		baseFolder = resolved
	}

	a.pathValidator.SetAllowedBasePaths([]string{baseFolder})

	emgPath, err := a.pathValidator.GetSafePath(baseFolder, manifest.EMGFile)
	if err != nil {
		return nil, fmt.Errorf("EMG 檔案路徑驗證失敗: %w", err)
	}

	if _, err := os.Stat(emgPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("EMG 檔案不存在: %s", emgPath)
	}

	emgData, err := a.emgParser.ParseFile(emgPath)
	if err != nil {
		return nil, fmt.Errorf("解析 EMG 檔案失敗: %w", err)
	}

	return emgData, nil
}

// phasePointDef defines a phase point for iteration.
type phasePointDef struct {
	name          string
	value         float64
	isMotionIndex bool
}

// getPhasePointDefs extracts all phase point definitions from a manifest.
func getPhasePointDefs(manifest *models.PhaseManifest) []phasePointDef {
	return []phasePointDef{
		{"P0", manifest.PhasePoints.P0, false},
		{"P1", manifest.PhasePoints.P1, false},
		{"P2", manifest.PhasePoints.P2, false},
		{"S", manifest.PhasePoints.S, false},
		{"C", manifest.PhasePoints.C, false},
		{"D", float64(manifest.PhasePoints.D), true},
		{"T0", manifest.PhasePoints.T0, false},
		{"T", manifest.PhasePoints.T, false},
		{"O", float64(manifest.PhasePoints.O), true},
		{"L", manifest.PhasePoints.L, false},
	}
}

// calculateGaitCycle determines gait cycle boundaries and phase percentages.
//
//nolint:err113 // dynamic error for user-facing output
func (a *CCIAnalyzer) calculateGaitCycle(
	manifest *models.PhaseManifest, emgData *models.PhaseSyncEMGData,
) (float64, float64, map[string]float64, map[string]float64, error) {
	points := getPhasePointDefs(manifest)
	emgTimes := make(map[string]float64)

	for _, pt := range points {
		if pt.value == 0 {
			continue
		}

		var emgTime float64
		if pt.isMotionIndex {
			emgTime = a.timeSynchronizer.MotionIndexToEMGTime(
				int(pt.value), manifest.EMGMotionOffset)
		} else {
			emgTime = a.timeSynchronizer.ForceTimeToEMGTime(
				pt.value, manifest.EMGMotionOffset)
		}

		emgTimes[pt.name] = emgTime
	}

	if len(emgTimes) < 2 {
		return 0, 0, nil, nil, fmt.Errorf("分期點不足，至少需要 2 個有效分期點")
	}

	// Find min/max EMG times as gait cycle boundaries
	gaitStart := math.Inf(1)
	gaitEnd := math.Inf(-1)

	for _, t := range emgTimes {
		gaitStart = math.Min(gaitStart, t)
		gaitEnd = math.Max(gaitEnd, t)
	}

	// Validate against EMG data range
	if err := validateEMGBounds(emgData, gaitStart, gaitEnd); err != nil {
		return 0, 0, nil, nil, err
	}

	// Calculate phase percentages and keep actual times
	duration := gaitEnd - gaitStart
	phasePercents := make(map[string]float64, len(emgTimes))
	phaseTimes := make(map[string]float64, len(emgTimes))

	for name, t := range emgTimes {
		pct := (t - gaitStart) / duration * 100
		phasePercents[name] = math.Max(0, math.Min(100, pct))
		phaseTimes[name] = t
	}

	return gaitStart, gaitEnd, phasePercents, phaseTimes, nil
}

// validateEMGBounds checks gait cycle times are within EMG data range.
//
//nolint:err113 // dynamic error for user-facing output
func validateEMGBounds(
	emgData *models.PhaseSyncEMGData, gaitStart, gaitEnd float64,
) error {
	if len(emgData.Time) == 0 {
		return fmt.Errorf("EMG 數據為空")
	}

	emgMin := emgData.Time[0]
	emgMax := emgData.Time[len(emgData.Time)-1]

	if gaitStart < emgMin {
		return fmt.Errorf(
			"步態週期開始時間 %.3f 小於 EMG 數據最小時間 %.3f", gaitStart, emgMin)
	}

	if gaitEnd > emgMax {
		return fmt.Errorf(
			"步態週期結束時間 %.3f 超出 EMG 數據範圍 (最大: %.3f)", gaitEnd, emgMax)
	}

	return nil
}

// computeAllPairs calculates CCI for all 12 muscle pairs using raw time points.
func (a *CCIAnalyzer) computeAllPairs(
	subject string,
	emgData *models.PhaseSyncEMGData,
	channelMap map[string]string,
	phasePercents map[string]float64,
	phaseTimes map[string]float64,
) *CCIAnalysisResult {
	pairs := DefaultMusclePairs()
	pairResults := make([]CCIResult, 0, len(pairs))
	meanCurves := make(map[string][]float64, len(pairs))
	sdCurves := make(map[string][]float64, len(pairs))
	dataLen := len(emgData.Time)

	for _, pair := range pairs {
		cciValues := a.computeSinglePair(pair, emgData, channelMap)
		if cciValues == nil {
			continue
		}

		pairResults = append(pairResults, CCIResult{
			PairName: pair.Name,
			Values:   cciValues,
		})

		meanCurves[pair.Name] = cciValues
		sdCurves[pair.Name] = make([]float64, dataLen)
	}

	return &CCIAnalysisResult{
		Subject:       subject,
		PairResults:   pairResults,
		TimeValues:    emgData.Time,
		PhasePercents: phasePercents,
		PhaseTimes:    phaseTimes,
		MeanCurves:    meanCurves,
		SDCurves:      sdCurves,
	}
}

// computeSinglePair calculates CCI for one muscle pair.
func (a *CCIAnalyzer) computeSinglePair(
	pair MusclePair,
	emgData *models.PhaseSyncEMGData,
	channelMap map[string]string,
) []float64 {
	header1, ok1 := channelMap[pair.Muscle1]
	header2, ok2 := channelMap[pair.Muscle2]

	if !ok1 || !ok2 {
		a.logger.Warn("跳過肌肉配對（缺少通道）", map[string]interface{}{
			"pair": pair.Name,
		})

		return nil
	}

	ch1Data := emgData.Channels[header1]
	ch2Data := emgData.Channels[header2]

	cciValues, err := CalculateCCITimeSeries(ch1Data, ch2Data)
	if err != nil {
		a.logger.Warn("CCI 計算失敗", map[string]interface{}{
			"pair":  pair.Name,
			"error": err.Error(),
		})

		return nil
	}

	return cciValues
}

// ExportToCSV exports CCI results to a CSV file.
func (a *CCIAnalyzer) ExportToCSV(
	result *CCIAnalysisResult, outputDir string,
) (string, error) {
	if err := os.MkdirAll(outputDir, dirPermission); err != nil {
		return "", fmt.Errorf("建立輸出目錄失敗: %w", err)
	}

	fileName := fmt.Sprintf("%s_CCI_Rudolph.csv", result.Subject)
	outputPath := filepath.Join(outputDir, fileName)

	return outputPath, writeCSVFile(outputPath, result)
}

// writeCSVFile writes the CCI result data to a CSV file.
func writeCSVFile(outputPath string, result *CCIAnalysisResult) error {
	file, err := os.OpenFile(
		filepath.Clean(outputPath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, filePermission)
	if err != nil {
		return fmt.Errorf("建立 CSV 檔案失敗: %w", err)
	}
	defer file.Close()

	// Write UTF-8 BOM
	if _, err := file.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return fmt.Errorf("寫入 BOM 失敗: %w", err)
	}

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Header row: Time (s), Gait Cycle (%), then each pair
	header := []string{"Time (s)", "Gait Cycle (%)"}
	for _, pr := range result.PairResults {
		header = append(header, pr.PairName)
	}

	if err := writer.Write(header); err != nil {
		return fmt.Errorf("寫入標題行失敗: %w", err)
	}

	// Data rows using actual time points
	duration := result.GaitEndTime - result.GaitStartTime
	numPoints := len(result.TimeValues)

	for i := 0; i < numPoints; i++ {
		t := result.TimeValues[i]
		pct := (t - result.GaitStartTime) / duration * 100
		row := []string{
			fmt.Sprintf("%.4f", t),
			fmt.Sprintf("%.2f", pct),
		}

		for _, pr := range result.PairResults {
			if i < len(pr.Values) {
				row = append(row, fmt.Sprintf("%.6f", pr.Values[i]))
			} else {
				row = append(row, "")
			}
		}

		if err := writer.Write(row); err != nil {
			return fmt.Errorf("寫入數據行失敗: %w", err)
		}
	}

	return nil
}

// GenerateReport produces a text summary of the CCI analysis.
func GenerateReport(result *CCIAnalysisResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("=== CCI Rudolph 分析報告 ===\n"))
	sb.WriteString(fmt.Sprintf("主題: %s\n", result.Subject))
	sb.WriteString(fmt.Sprintf("肌肉配對數: %d\n\n", len(result.PairResults)))

	for _, pr := range result.PairResults {
		if len(pr.Values) == 0 {
			continue
		}

		mean, max := computePairStats(pr.Values)
		sb.WriteString(fmt.Sprintf("  %s: 平均 CCI = %.4f, 最大 CCI = %.4f\n",
			pr.PairName, mean, max))
	}

	sb.WriteString(fmt.Sprintf("\n分期點位置 (步態週期 %%):\n"))

	phaseOrder := []string{"P0", "P1", "P2", "S", "C", "D", "T0", "T", "O", "L"}
	for _, name := range phaseOrder {
		if pct, ok := result.PhasePercents[name]; ok {
			sb.WriteString(fmt.Sprintf("  %s: %.1f%%\n", name, pct))
		}
	}

	return sb.String()
}

// computePairStats returns mean and max of a CCI curve.
func computePairStats(values []float64) (float64, float64) {
	sum := 0.0
	maxVal := 0.0

	for _, v := range values {
		sum += v
		maxVal = math.Max(maxVal, v)
	}

	return sum / float64(len(values)), maxVal
}
