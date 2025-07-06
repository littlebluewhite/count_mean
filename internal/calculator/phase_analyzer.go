package calculator

import (
	"count_mean/internal/models"
	"count_mean/util"
	"fmt"
)

// PhaseAnalyzer 處理階段分析
type PhaseAnalyzer struct {
	scalingFactor int
	phaseLabels   []string
}

// NewPhaseAnalyzer 創建新的階段分析器
func NewPhaseAnalyzer(scalingFactor int, phaseLabels []string) *PhaseAnalyzer {
	return &PhaseAnalyzer{
		scalingFactor: scalingFactor,
		phaseLabels:   phaseLabels,
	}
}

// AnalyzeResult 階段分析結果
type AnalyzeResult struct {
	PhaseResults []models.PhaseAnalysisResult `json:"phase_results"`
	MaxTimeIndex map[int]float64              `json:"max_time_index"` // 每個通道最大值出現的時間
}

// Analyze 分析不同階段的數據
func (p *PhaseAnalyzer) Analyze(dataset *models.EMGDataset, phases []models.TimeRange) (*AnalyzeResult, error) {
	if dataset == nil || len(dataset.Data) == 0 {
		return nil, fmt.Errorf("數據集為空")
	}

	if len(phases) != len(p.phaseLabels) {
		return nil, fmt.Errorf("階段數量與標籤數量不匹配")
	}

	channelCount := len(dataset.Data[0].Channels)

	// 初始化階段數據收集器
	phaseData := make([]map[int][]float64, len(phases))
	for i := range phaseData {
		phaseData[i] = make(map[int][]float64)
	}

	allData := make(map[int][]float64) // 用於找到全局最大值的時間
	timeData := make([]float64, 0, len(dataset.Data))

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

	return &AnalyzeResult{
		PhaseResults: results,
		MaxTimeIndex: maxTimeIndex,
	}, nil
}

// AnalyzeFromRawData 從原始字符串數據進行階段分析
func (p *PhaseAnalyzer) AnalyzeFromRawData(records [][]string, phaseStrings []string) (*AnalyzeResult, error) {
	dataset, err := p.parseRawData(records)
	if err != nil {
		return nil, fmt.Errorf("解析數據失敗: %w", err)
	}

	phases, err := p.parsePhases(phaseStrings)
	if err != nil {
		return nil, fmt.Errorf("解析階段失敗: %w", err)
	}

	return p.Analyze(dataset, phases)
}

// parseRawData 解析原始字符串數據
func (p *PhaseAnalyzer) parseRawData(records [][]string) (*models.EMGDataset, error) {
	if len(records) < 2 {
		return nil, fmt.Errorf("數據至少需要包含標題行和一行數據")
	}

	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}

	// 複製標題
	copy(dataset.Headers, records[0])

	// 解析數據
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue
		}

		// 解析時間
		timeVal, err := util.Str2Number[float64, int](row[0], p.scalingFactor)
		if err != nil {
			return nil, fmt.Errorf("解析時間值失敗在第 %d 行: %w", i+1, err)
		}

		// 解析通道數據
		channels := make([]float64, 0, len(row)-1)
		for j := 1; j < len(row); j++ {
			val, err := util.Str2Number[float64, int](row[j], p.scalingFactor)
			if err != nil {
				return nil, fmt.Errorf("解析數據失敗在第 %d 行第 %d 列: %w", i+1, j+1, err)
			}
			channels = append(channels, val)
		}

		data := models.EMGData{
			Time:     timeVal,
			Channels: channels,
		}

		dataset.Data = append(dataset.Data, data)
	}

	return dataset, nil
}

// parsePhases 解析階段字符串為時間範圍
func (p *PhaseAnalyzer) parsePhases(phaseStrings []string) ([]models.TimeRange, error) {
	if len(phaseStrings) < 5 {
		return nil, fmt.Errorf("需要至少 5 個時間點來定義 4 個階段")
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

	// 創建時間範圍
	phases := make([]models.TimeRange, len(p.phaseLabels))
	for i := 0; i < len(p.phaseLabels) && i+1 < len(timePoints); i++ {
		phases[i] = models.TimeRange{
			Start: timePoints[i],
			End:   timePoints[i+1],
		}
	}

	return phases, nil
}
