package models

import (
	calcerrors "count_mean/internal/errors"
)

// EMGData 代表 EMG 數據的結構.
type EMGData struct {
	Time     float64   `json:"time"`
	Channels []float64 `json:"channels"`
}

// EMGDataset 代表完整的 EMG 數據集.
type EMGDataset struct {
	Headers               []string  `json:"headers"`
	Data                  []EMGData `json:"data"`
	OriginalTimePrecision int       `json:"original_time_precision"` // 原始時間欄位的小數位數
}

// ChannelCount 返回數據集中的通道數量.
func (d *EMGDataset) ChannelCount() int {
	if d == nil || len(d.Data) == 0 {
		return 0
	}

	return len(d.Data[0].Channels)
}

// DataPointCount 返回數據集中的數據點數量.
func (d *EMGDataset) DataPointCount() int {
	if d == nil {
		return 0
	}

	return len(d.Data)
}

// IsEmpty 檢查數據集是否為空.
func (d *EMGDataset) IsEmpty() bool {
	return d == nil || len(d.Data) == 0
}

// Validate 驗證數據集是否有效.
func (d *EMGDataset) Validate() error {
	if d.IsEmpty() {
		return calcerrors.ErrEmptyDataset
	}

	return nil
}

// MaxMeanResult 代表最大平均值計算結果.
type MaxMeanResult struct {
	ColumnIndex int     `json:"column_index"`
	StartTime   float64 `json:"start_time"`
	EndTime     float64 `json:"end_time"`
	MaxMean     float64 `json:"max_mean"`
}

// PhaseAnalysisResult 代表階段分析結果.
type PhaseAnalysisResult struct {
	PhaseName  string          `json:"phase_name"`
	MaxValues  map[int]float64 `json:"max_values"`
	MeanValues map[int]float64 `json:"mean_values"`
}

// TimeRange 代表時間範圍.
type TimeRange struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
}
