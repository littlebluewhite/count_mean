package gui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"count_mean/internal/models"
)

// TestConvertPhaseResultToAnalysis_ChannelMappingNoOffByOne 釘住 phase 分析的
// channel 對應 off-by-one(whole-project review 補審 P1)。
//
// models.PhaseAnalysisResult.MaxValues / MeanValues 是 map[int]float64,key 為
// 0-based channel index(computePhaseStatistics 以 chIdx:=0; chIdx<channelCount
// 填充,key 0 = Ch1)。先前 convertPhaseResultToAnalysis 用 maxValues[colIdx-1]:
// key 0(Ch1)落到 index -1 被 guard 丟棄、其餘整體下移一格、末槽補 0 → 每個
// phase 的顯示值全錯位(磁碟 CSV 正確,僅 webview 顯示與回傳結果)。
func TestConvertPhaseResultToAnalysis_ChannelMappingNoOffByOne(t *testing.T) {
	phaseResult := &models.PhaseAnalysisResult{
		PhaseName:  "stance",
		MaxValues:  map[int]float64{0: 10, 1: 20, 2: 30}, // Ch1=10, Ch2=20, Ch3=30
		MeanValues: map[int]float64{0: 1, 1: 2, 2: 3},
	}

	got := convertPhaseResultToAnalysis(phaseResult, 3)

	assert.Equal(t, []float64{10, 20, 30}, got.MaxValues,
		"MaxValues 應 1:1 對應 0-based channel(key0=Ch1→index0),不可位移")
	assert.Equal(t, []float64{1, 2, 3}, got.Average,
		"MeanValues(Average)同樣不可位移")
	assert.Equal(t, "stance", got.PhaseLabel)
}
