package calculator

import (
	"fmt"
	"time"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/parser"
	"count_mean/util"
)

// PhaseAnalyzer 處理階段分析
type PhaseAnalyzer struct {
	scalingFactor int
	phaseLabels   []string
	logger        *logging.Logger
	dataParser    *parser.DataParser
}

// NewPhaseAnalyzer 創建新的階段分析器
func NewPhaseAnalyzer(scalingFactor int, phaseLabels []string) *PhaseAnalyzer {
	logger := logging.GetLogger("phase_analyzer")
	return &PhaseAnalyzer{
		scalingFactor: scalingFactor,
		phaseLabels:   phaseLabels,
		logger:        logger,
		dataParser:    parser.NewDataParserWithLogger(scalingFactor, logger),
	}
}

// AnalyzeResult 階段分析結果
type AnalyzeResult struct {
	PhaseResults []models.PhaseAnalysisResult `json:"phase_results"`
	MaxTimeIndex map[int]float64              `json:"max_time_index"` // 每個通道最大值出現的時間
}

// Analyze 分析不同階段的數據
func (p *PhaseAnalyzer) Analyze(dataset *models.EMGDataset, phases []models.TimeRange) (*AnalyzeResult, error) {
	if p == nil {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "階段分析器為空")
	}

	startTime := time.Now()

	// 使用 IsEmpty() 統一檢查 nil 和空數據
	if dataset.IsEmpty() {
		err := calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "數據集為空")
		// 安全記錄：dataset 可能為 nil，避免存取其方法
		p.logger.Error("階段分析輸入驗證失敗", err, map[string]interface{}{
			"dataset_empty": true,
		})
		return nil, err
	}

	p.logger.Info("開始階段分析", map[string]interface{}{
		"phase_count":    len(phases),
		"data_points":    dataset.DataPointCount(),
		"channel_count":  dataset.ChannelCount(),
		"scaling_factor": p.scalingFactor,
	})

	if len(phases) != len(p.phaseLabels) {
		err := calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrPhaseMismatch,
			"階段數量與標籤數量不匹配",
			map[string]interface{}{
				"phase_count": len(phases),
				"label_count": len(p.phaseLabels),
			},
		)
		p.logger.Error("階段配置不匹配", err, map[string]interface{}{
			"phase_count": len(phases),
			"label_count": len(p.phaseLabels),
		})
		return nil, err
	}

	channelCount := dataset.ChannelCount()

	// 初始化階段數據收集器
	phaseData := make([]map[int][]float64, len(phases))
	for i := range phaseData {
		phaseData[i] = make(map[int][]float64)
	}

	allData := make(map[int][]float64) // 用於找到全局最大值的時間
	timeData := make([]float64, 0, dataset.DataPointCount())

	// 收集數據
	for _, data := range dataset.Data {
		timeData = append(timeData, data.Time)

		// 分配到對應階段
		for phaseIdx, phase := range phases {
			if data.Time > phase.Start && data.Time < phase.End {
				for chIdx, val := range data.Channels {
					phaseData[phaseIdx][chIdx] = append(phaseData[phaseIdx][chIdx], val)
				}
			}
		}

		// 收集全局數據
		for chIdx, val := range data.Channels {
			allData[chIdx] = append(allData[chIdx], val)
		}
	}

	// 分析每個階段
	results := make([]models.PhaseAnalysisResult, 0, len(phases))
	for phaseIdx, phaseName := range p.phaseLabels {
		maxValues := make(map[int]float64)
		meanValues := make(map[int]float64)

		for chIdx := 0; chIdx < channelCount; chIdx++ {
			if data, exists := phaseData[phaseIdx][chIdx]; exists && len(data) > 0 {
				maxVal, _ := util.ArrayMax(data)
				meanVal := util.ArrayMean(data)

				maxValues[chIdx] = maxVal
				meanValues[chIdx] = meanVal
			}
		}

		result := models.PhaseAnalysisResult{
			PhaseName:  phaseName,
			MaxValues:  maxValues,
			MeanValues: meanValues,
		}

		results = append(results, result)
	}

	// 計算全局最大值出現的時間
	maxTimeIndex := make(map[int]float64)
	for chIdx := 0; chIdx < channelCount; chIdx++ {
		if data, exists := allData[chIdx]; exists && len(data) > 0 {
			_, maxIdx := util.ArrayMax(data)
			if maxIdx < len(timeData) {
				maxTimeIndex[chIdx] = timeData[maxIdx]
			}
		}
	}

	duration := time.Since(startTime)
	p.logger.Info("階段分析完成", map[string]interface{}{
		"duration_ms":   duration.Milliseconds(),
		"phase_count":   len(results),
		"channel_count": channelCount,
	})

	return &AnalyzeResult{
		PhaseResults: results,
		MaxTimeIndex: maxTimeIndex,
	}, nil
}

// AnalyzeFromRawData 從原始字符串數據進行階段分析
func (p *PhaseAnalyzer) AnalyzeFromRawData(records [][]string, phaseStrings []string) (*AnalyzeResult, error) {
	if p == nil {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "階段分析器為空")
	}

	p.logger.Info("開始從原始數據進行階段分析", map[string]interface{}{
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

// parsePhases 解析階段字符串為時間範圍
func (p *PhaseAnalyzer) parsePhases(phaseStrings []string) ([]models.TimeRange, error) {
	if len(phaseStrings) < 5 {
		return nil, calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrInsufficientData,
			"需要至少 5 個時間點來定義 4 個階段",
			map[string]interface{}{
				"provided_points": len(phaseStrings),
				"required_points": 5,
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
			map[string]interface{}{
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
