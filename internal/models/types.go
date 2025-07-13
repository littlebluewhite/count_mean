package models

import "time"

// EMGData 代表 EMG 數據的結構
type EMGData struct {
	Time     float64   `json:"time"`
	Channels []float64 `json:"channels"`
}

// EMGDataset 代表完整的 EMG 數據集
type EMGDataset struct {
	Headers               []string  `json:"headers"`
	Data                  []EMGData `json:"data"`
	OriginalTimePrecision int       `json:"original_time_precision"` // 原始時間欄位的小數位數
}

// MaxMeanResult 代表最大平均值計算結果
type MaxMeanResult struct {
	ColumnIndex int     `json:"column_index"`
	StartTime   float64 `json:"start_time"`
	EndTime     float64 `json:"end_time"`
	MaxMean     float64 `json:"max_mean"`
}

// PhaseAnalysisResult 代表階段分析結果
type PhaseAnalysisResult struct {
	PhaseName  string          `json:"phase_name"`
	MaxValues  map[int]float64 `json:"max_values"`
	MeanValues map[int]float64 `json:"mean_values"`
}

// AnalysisConfig 代表分析配置
type AnalysisConfig struct {
	ScalingFactor int         `json:"scaling_factor"`
	WindowSize    int         `json:"window_size"`
	PhaseLabels   []string    `json:"phase_labels"`
	Phases        []TimeRange `json:"phases"`
	CreatedAt     time.Time   `json:"created_at"`
}

// TimeRange 代表時間範圍
type TimeRange struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}

// ProcessingOptions 代表處理選項
type ProcessingOptions struct {
	ValidateInput bool   `json:"validate_input"`
	Precision     int    `json:"precision"`
	OutputFormat  string `json:"output_format"`
}
