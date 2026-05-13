package cci

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sync/errgroup"

	"count_mean/internal/calculator"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
	"count_mean/internal/synchronizer"
	"count_mean/util"
	"count_mean/internal/csvutil"
	"count_mean/internal/security/fsperm"
)

// CCIParams holds parameters for CCI analysis.
type CCIParams struct {
	ManifestFile string
	DataFolder   string
	SubjectIndex int
}

// CCIAnalyzer orchestrates the CCI Rudolph analysis pipeline.
// 不持有 PathValidator — 改為每個分析請求在 loadEMGData 內 new 一個 request-scoped instance，
// 避免多請求並行時互相覆寫 base path。
type CCIAnalyzer struct {
	manifestParser   *parsers.PhaseManifestParser
	emgParser        *parsers.EMGParser
	timeSynchronizer *synchronizer.TimeSynchronizer
	logger           *logging.Logger
}

// NewCCIAnalyzer creates a new CCI analyzer instance.
func NewCCIAnalyzer() *CCIAnalyzer {
	return &CCIAnalyzer{
		manifestParser:   parsers.NewPhaseManifestParser(),
		emgParser:        parsers.NewEMGParser(),
		timeSynchronizer: synchronizer.NewTimeSynchronizer(),
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

	manifest, err := a.loadAndValidate(params)
	if err != nil {
		return nil, err
	}

	emgData, err := a.loadEMGData(params.DataFolder, manifest)
	if err != nil {
		return nil, err
	}

	channelMap, err := BuildChannelMap(emgData.Headers)
	if err != nil {
		return nil, fmt.Errorf("建立通道映射失敗: %w", err)
	}

	gaitStart, gaitEnd, phasePercents, phaseTimes, err := a.calculateGaitCycle(
		manifest, emgData)
	if err != nil {
		return nil, err
	}

	rangeResult, err := a.emgParser.GetDataInTimeRange(emgData, gaitStart, gaitEnd)
	if err != nil {
		return nil, fmt.Errorf("提取步態週期 EMG 數據失敗: %w", err)
	}

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
// 改用 request-scoped PathValidator：原本 a.pathValidator 是 analyzer instance 持有的
// singleton，每個並發請求都會 SetAllowedBasePaths 覆寫，造成跨請求污染。
// 改為每次呼叫 new 一個 PathValidator，base path 屬於該請求。
func (a *CCIAnalyzer) loadEMGData(
	dataFolder string, manifest *models.PhaseManifest,
) (*models.PhaseSyncEMGData, error) {
	baseFolder := dataFolder
	if resolved, err := filepath.EvalSymlinks(baseFolder); err == nil {
		baseFolder = resolved
	}

	pathValidator := security.NewPathValidator([]string{baseFolder})

	emgPath, err := pathValidator.GetSafePath(baseFolder, manifest.EMGFile)
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
// 用 models.AllPhases() + PhasePoint.IsMotionIndex() 集中 domain invariant，
// 避免「D 與 O 是 motion index」散落多處。
func getPhasePointDefs(manifest *models.PhaseManifest) []phasePointDef {
	allPhases := models.AllPhases()
	defs := make([]phasePointDef, 0, len(allPhases))

	for _, phase := range allPhases {
		value, _, _ := parsers.GetPhaseValue(&manifest.PhasePoints, phase)
		defs = append(defs, phasePointDef{
			name:          string(phase),
			value:         value,
			isMotionIndex: phase.IsMotionIndex(),
		})
	}

	return defs
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
// 各 pair 計算 CPU-bound 且彼此獨立（讀同一份唯讀 emgData / channelMap、寫獨立 slot），
// 用 errgroup 跑滿 GOMAXPROCS；缺通道的 pair 留 zero-value (PairName == "") 作 skip 哨兵，
// 在 main goroutine 過濾並彙整成 map，避免並行寫 map。
func (a *CCIAnalyzer) computeAllPairs(
	subject string,
	emgData *models.PhaseSyncEMGData,
	channelMap map[string]string,
	phasePercents map[string]float64,
	phaseTimes map[string]float64,
) *CCIAnalysisResult {
	pairs := DefaultMusclePairs()
	slots := make([]CCIResult, len(pairs))

	var g errgroup.Group

	for i, pair := range pairs {
		g.Go(func() error {
			cciValues := a.computeSinglePair(pair, emgData, channelMap)
			if cciValues == nil {
				return nil
			}

			slots[i] = CCIResult{
				PairName: pair.Name,
				Values:   cciValues,
			}

			return nil
		})
	}

	// computeSinglePair 目前所有路徑都 return nil（失敗以 nil slot 表達），所以 g.Wait()
	// 不會回 error。顯式 err 接住未來變動：如果 computeSinglePair 改成回 error，
	// 這個分支就會啟動並送到 logger，避免靜默吞錯。
	if err := g.Wait(); err != nil {
		a.logger.Warn("CCI pair worker reported unexpected error",
			map[string]interface{}{"error": err.Error()})
	}

	pairResults := make([]CCIResult, 0, len(slots))
	meanCurves := make(map[string][]float64, len(slots))

	for _, r := range slots {
		if r.PairName == "" {
			continue
		}

		pairResults = append(pairResults, r)
		meanCurves[r.PairName] = r.Values
	}

	return &CCIAnalysisResult{
		Subject:       subject,
		PairResults:   pairResults,
		TimeValues:    emgData.Time,
		PhasePercents: phasePercents,
		PhaseTimes:    phaseTimes,
		MeanCurves:    meanCurves,
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
// result.Subject 來自外部 manifest，需先做 SanitizeFileName 去除路徑分隔符與特殊字元，
// 否則 "../foo" 之類的值會讓 filepath.Join 寫到 outputDir 之外（路徑穿越）。
func (a *CCIAnalyzer) ExportToCSV(
	result *CCIAnalysisResult, outputDir string,
) (string, error) {
	if err := os.MkdirAll(outputDir, fsperm.DirPerm); err != nil {
		return "", fmt.Errorf("建立輸出目錄失敗: %w", err)
	}

	safeSubject := calculator.SanitizeFileName(result.Subject)
	fileName := fmt.Sprintf("%s_CCI_Rudolph.csv", safeSubject)
	outputPath := filepath.Join(outputDir, fileName)

	return outputPath, writeCSVFile(outputPath, result, a.logger)
}

// writeCSVFile writes the CCI result data to a CSV file.
//
// Flush 與 Close 的錯誤都顯式回傳：csv.Writer 把寫入錯誤延後到 Flush 才顯露，
// 若僅靠 defer 忽略，caller 看到 nil 但檔案內容可能不完整（磁碟滿、網路磁碟掉線等）。
//
// NaN/Inf 處理：CalculateCCIRudolph 對 invalid input（NaN/Inf input 或負值除以零）
// 回傳 math.NaN()。若 cell 直接 fmt.Sprintf("%.6f", NaN) 會寫入字面 "NaN"，下游
// Excel / pandas / R 都會誤讀為字串非數值。修法（codex C-4 Wave 6）：
//   - cell 為 NaN/Inf → 輸出空字串（CSV 慣例代表 missing data）
//   - 若整 row 所有 pair 都 NaN/Inf → skip 整 row 並計入 droppedRowCount
//   - 結束時 log warning，與 large_file_handler 的 -Inf channel skip 邏輯一致
func writeCSVFile(outputPath string, result *CCIAnalysisResult, logger *logging.Logger) (err error) {
	// fsperm.WriteFlags 在 unix 加入 O_NOFOLLOW 拒絕 symlink 攻擊；Windows 行為不變。
	file, err := os.OpenFile(filepath.Clean(outputPath), fsperm.WriteFlags, fsperm.FilePerm)
	if err != nil {
		return fmt.Errorf("建立 CSV 檔案失敗: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("關閉檔案失敗: %w", closeErr)
		}
	}()

	// Write UTF-8 BOM
	if err := csvutil.WriteBOM(file); err != nil {
		return fmt.Errorf("寫入 BOM 失敗: %w", err)
	}

	writer := csv.NewWriter(file)

	// Header row: Time (s), Gait Cycle (%), then each pair
	header := []string{"Time (s)", "Gait Cycle (%)"}
	for _, pr := range result.PairResults {
		header = append(header, pr.PairName)
	}

	if err := writer.Write(csvutil.SanitizeHeaderRow(header)); err != nil {
		return fmt.Errorf("寫入標題行失敗: %w", err)
	}

	// Data rows using actual time points
	duration := result.GaitEndTime - result.GaitStartTime
	if duration <= 0 {
		return fmt.Errorf("無效的步態週期區間: start=%v end=%v", result.GaitStartTime, result.GaitEndTime)
	}
	numPoints := len(result.TimeValues)

	var droppedRowCount int

	for i := 0; i < numPoints; i++ {
		t := result.TimeValues[i]
		pct := (t - result.GaitStartTime) / duration * 100

		// 先收集所有 pair cells；判斷 row 是否全 non-finite。
		pairCells := make([]string, 0, len(result.PairResults))
		allNonFinite := true

		for _, pr := range result.PairResults {
			if i >= len(pr.Values) {
				pairCells = append(pairCells, "")
				continue
			}

			v := pr.Values[i]
			if math.IsNaN(v) || math.IsInf(v, 0) {
				pairCells = append(pairCells, "") // missing-data 慣例
				continue
			}

			pairCells = append(pairCells, fmt.Sprintf("%.6f", v))
			allNonFinite = false
		}

		// 若整 row 所有 pair 都是 NaN/Inf（或 row 沒有 pair 但有 time），skip。
		// 沒 pair 的特殊 case 不視為「全 NaN」 — 仍寫入 time/gait 兩欄。
		if len(result.PairResults) > 0 && allNonFinite {
			droppedRowCount++
			continue
		}

		row := []string{
			fmt.Sprintf("%.4f", t),
			fmt.Sprintf("%.2f", pct),
		}
		row = append(row, pairCells...)

		if err := writer.Write(row); err != nil {
			return fmt.Errorf("寫入數據行失敗: %w", err)
		}
	}

	if droppedRowCount > 0 && logger != nil {
		logger.Warn("CCI 匯出 CSV 時跳過全 NaN/Inf 的 row", map[string]interface{}{
			"dropped_rows": droppedRowCount,
			"total_rows":   numPoints,
			"output_path":  outputPath,
		})
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("CSV flush 失敗: %w", err)
	}

	return nil
}

// GenerateReport produces a text summary of the CCI analysis.
func GenerateReport(result *CCIAnalysisResult) string {
	var sb strings.Builder

	sb.WriteString("=== CCI Rudolph 分析報告 ===\n")
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

	return sb.String()
}

// computePairStats returns mean and max of a CCI curve.
// Caller (FormatResultReport) guards against empty values, so util helpers
// that assume len > 0 are safe here.
func computePairStats(values []float64) (float64, float64) {
	mean := util.ArrayMean(values)
	maxVal, _ := util.ArrayMax(values)

	return mean, maxVal
}
