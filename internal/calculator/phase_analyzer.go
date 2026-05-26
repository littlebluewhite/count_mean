package calculator

import (
	"fmt"
	"time"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/util"
)

// MinTimePointsForPhases is the minimum number of time points needed to define 4 phases.
const MinTimePointsForPhases = 5

// PhaseAnalyzer 處理階段分析.
type PhaseAnalyzer struct {
	scalingFactor int
	phaseLabels   []string
	logger        *logging.Logger
	dataParser    *parsers.DataParser
}

// NewPhaseAnalyzer 創建新的階段分析器.
func NewPhaseAnalyzer(scalingFactor int, phaseLabels []string) *PhaseAnalyzer {
	logger := logging.GetLogger("phase_analyzer")

	return &PhaseAnalyzer{
		scalingFactor: scalingFactor,
		phaseLabels:   phaseLabels,
		logger:        logger,
		dataParser:    parsers.NewDataParserWithLogger(scalingFactor, logger),
	}
}

// AnalyzeResult 階段分析結果.
type AnalyzeResult struct {
	PhaseResults []models.PhaseAnalysisResult `json:"phase_results"`
	MaxTimeIndex map[int]float64              `json:"max_time_index"` // 每個通道最大值出現的時間
}

// phaseDataCollector 收集階段數據的結構.
type phaseDataCollector struct {
	phaseData []map[int][]float64
	allData   map[int][]float64
	timeData  []float64
}

// validateAnalyzeInput 驗證分析輸入.
func (p *PhaseAnalyzer) validateAnalyzeInput(dataset *models.EMGDataset, phases []models.TimeRange) error {
	if dataset.IsEmpty() {
		err := calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "數據集為空")
		p.logger.Error("階段分析輸入驗證失敗", err, map[string]any{"dataset_empty": true})

		return err
	}

	if len(phases) != len(p.phaseLabels) {
		err := calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrPhaseMismatch,
			"階段數量與標籤數量不匹配",
			map[string]any{"phase_count": len(phases), "label_count": len(p.phaseLabels)},
		)
		p.logger.Error("階段配置不匹配", err, map[string]any{"phase_count": len(phases), "label_count": len(p.phaseLabels)})

		return err
	}

	return nil
}

// collectPhaseData 收集各階段的數據.
func (p *PhaseAnalyzer) collectPhaseData(dataset *models.EMGDataset, phases []models.TimeRange) *phaseDataCollector {
	collector := &phaseDataCollector{
		phaseData: make([]map[int][]float64, len(phases)),
		allData:   make(map[int][]float64),
		timeData:  make([]float64, 0, dataset.DataPointCount()),
	}

	for i := range collector.phaseData {
		collector.phaseData[i] = make(map[int][]float64)
	}

	for _, data := range dataset.Data {
		collector.timeData = append(collector.timeData, data.Time)
		p.assignDataToPhases(data, phases, collector.phaseData)
		p.collectGlobalData(data, collector.allData)
	}

	return collector
}

// assignDataToPhases 將數據點分配到對應的階段.
//
// 嚴格不等式（>, <）是刻意設計：避免落在 phase.Start / phase.End 的邊界樣本
// 同時計入兩個相鄰 phase 造成 double counting。對於 EMG / gait 分析，phase
// 邊界通常是事件時間點（heel strike、起跳離地等），該瞬間樣本歸屬本來就
// 不明確；寧可丟一個樣本也不要污染兩端 phase 的統計值。
// (post-review 2026-05-13：確認此 intent 並 lock in，避免未來 reviewer 再次
// 誤判為 boundary bug。)
func (*PhaseAnalyzer) assignDataToPhases(
	data models.EMGData, phases []models.TimeRange, phaseData []map[int][]float64,
) {
	for phaseIdx, phase := range phases {
		if data.Time > phase.Start && data.Time < phase.End {
			for chIdx, val := range data.Channels {
				phaseData[phaseIdx][chIdx] = append(phaseData[phaseIdx][chIdx], val)
			}
		}
	}
}

// collectGlobalData 收集全局數據.
func (*PhaseAnalyzer) collectGlobalData(data models.EMGData, allData map[int][]float64) {
	for chIdx, val := range data.Channels {
		allData[chIdx] = append(allData[chIdx], val)
	}
}

// computePhaseStatistics 計算每個階段的統計值.
func (p *PhaseAnalyzer) computePhaseStatistics(
	phaseData []map[int][]float64, channelCount int,
) []models.PhaseAnalysisResult {
	results := make([]models.PhaseAnalysisResult, 0, len(p.phaseLabels))

	for phaseIdx, phaseName := range p.phaseLabels {
		maxValues := make(map[int]float64)
		meanValues := make(map[int]float64)

		for chIdx := 0; chIdx < channelCount; chIdx++ {
			if data, exists := phaseData[phaseIdx][chIdx]; exists && len(data) > 0 {
				maxVal, _ := util.ArrayMax(data)
				maxValues[chIdx] = maxVal
				meanValues[chIdx] = util.ArrayMean(data)
			}
		}

		results = append(results, models.PhaseAnalysisResult{
			PhaseName:  phaseName,
			MaxValues:  maxValues,
			MeanValues: meanValues,
		})
	}

	return results
}

// computeMaxTimeIndex 計算每個通道全局最大值出現的時間.
func (*PhaseAnalyzer) computeMaxTimeIndex(
	allData map[int][]float64, timeData []float64, channelCount int,
) map[int]float64 {
	maxTimeIndex := make(map[int]float64)

	for chIdx := 0; chIdx < channelCount; chIdx++ {
		if data, exists := allData[chIdx]; exists && len(data) > 0 {
			_, maxIdx := util.ArrayMax(data)
			if maxIdx < len(timeData) {
				maxTimeIndex[chIdx] = timeData[maxIdx]
			}
		}
	}

	return maxTimeIndex
}

// Analyze 分析不同階段的數據.
func (p *PhaseAnalyzer) Analyze(dataset *models.EMGDataset, phases []models.TimeRange) (*AnalyzeResult, error) {
	if p == nil {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "階段分析器為空")
	}

	// 顯式 nil dataset 防線 — IsEmpty() 雖對 nil receiver 安全，但下游
	// collectPhaseData / computePhaseStatistics 使用 dataset.Data / Headers 不會
	// 自動 nil-handle。在入口顯式拒絕讓 contract 明確。訊息對齊既有測試期望「數據集為空」。
	if dataset == nil {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "數據集為空")
	}

	startTime := time.Now()

	if err := p.validateAnalyzeInput(dataset, phases); err != nil {
		return nil, err
	}

	p.logger.Info("開始階段分析", map[string]any{
		"phase_count":    len(phases),
		"data_points":    dataset.DataPointCount(),
		"channel_count":  dataset.ChannelCount(),
		"scaling_factor": p.scalingFactor,
	})

	channelCount := dataset.ChannelCount()
	collector := p.collectPhaseData(dataset, phases)
	results := p.computePhaseStatistics(collector.phaseData, channelCount)
	maxTimeIndex := p.computeMaxTimeIndex(collector.allData, collector.timeData, channelCount)

	duration := time.Since(startTime)
	p.logger.Info("階段分析完成", map[string]any{
		"duration_ms":   duration.Milliseconds(),
		"phase_count":   len(results),
		"channel_count": channelCount,
	})

	return &AnalyzeResult{
		PhaseResults: results,
		MaxTimeIndex: maxTimeIndex,
	}, nil
}

// AnalyzeFromRawData 從原始字符串數據進行階段分析.
func (p *PhaseAnalyzer) AnalyzeFromRawData(records [][]string, phaseStrings []string) (*AnalyzeResult, error) {
	if p == nil {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "階段分析器為空")
	}

	p.logger.Info("開始從原始數據進行階段分析", map[string]any{
		"record_count":  len(records),
		"phase_strings": phaseStrings,
	})

	dataset, err := p.dataParser.ParseRawData(records)
	if err != nil {
		p.logger.Error("階段分析數據解析失敗", err)
		return nil, fmt.Errorf("解析數據失敗: %w", err)
	}

	phases, err := p.parsePhases(phaseStrings)
	if err != nil {
		p.logger.Error("階段配置解析失敗", err)
		return nil, fmt.Errorf("解析階段失敗: %w", err)
	}

	return p.Analyze(dataset, phases)
}

// parsePhases 解析階段字符串為時間範圍.
func (p *PhaseAnalyzer) parsePhases(phaseStrings []string) ([]models.TimeRange, error) {
	if len(phaseStrings) < MinTimePointsForPhases {
		return nil, calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrInsufficientData,
			"需要至少 5 個時間點來定義 4 個階段",
			map[string]any{
				"provided_points": len(phaseStrings),
				"required_points": MinTimePointsForPhases,
			},
		)
	}

	// 解析時間點
	timePoints := make([]float64, len(phaseStrings))

	for i, timeStr := range phaseStrings {
		val, err := util.Str2Number[float64, int](timeStr, p.scalingFactor)
		if err != nil {
			return nil, fmt.Errorf("解析時間點 '%s' 失敗: %w", timeStr, err)
		}

		timePoints[i] = val
	}

	// 驗證時間點數量是否足夠定義所有階段
	requiredTimePoints := len(p.phaseLabels) + 1
	if len(timePoints) < requiredTimePoints {
		return nil, calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrInsufficientData,
			fmt.Sprintf("需要至少 %d 個時間點來定義 %d 個階段", requiredTimePoints, len(p.phaseLabels)),
			map[string]any{
				"provided_points": len(timePoints),
				"required_points": requiredTimePoints,
				"phase_count":     len(p.phaseLabels),
			},
		)
	}

	// 創建時間範圍
	phases := make([]models.TimeRange, len(p.phaseLabels))
	for i := 0; i < len(p.phaseLabels); i++ {
		phases[i] = models.TimeRange{
			Start: timePoints[i],
			End:   timePoints[i+1],
		}
	}

	return phases, nil
}
