package cci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDownsampleCCI_BelowThreshold_NoChange 樣本數 <= threshold 時應原樣回傳，
// 不浪費 LTTB 計算也不分配新 slice。
func TestDownsampleCCI_BelowThreshold_NoChange(t *testing.T) {
	result := &CCIAnalysisResult{
		TimeValues: []float64{0, 1, 2, 3},
		PairResults: []CCIResult{
			{PairName: "A", Values: []float64{10, 20, 30, 40}},
		},
	}

	out, err := downsampleCCIResult(result, 100)
	require.NoError(t, err)
	assert.Same(t, result, out, "threshold 大於資料長度時應直接回傳原物件")
}

// TestDownsampleCCI_UnionPreservesNonFirstPairPeaks 是 codex review P2 finding 的 regression test。
// 構造兩對 CCIResult：
//   - pair 0 完全平緩（y=0）
//   - pair 1 在中間索引附近有一個窄峰
//
// 舊版「以第一個 pair 為 representative」策略會基於 pair 0 的平緩做 LTTB，
// 完全可能略過 pair 1 的峰所在的索引。union 版本對每個 pair 各自跑 LTTB，
// 應該保留 pair 1 的峰值索引。
func TestDownsampleCCI_UnionPreservesNonFirstPairPeaks(t *testing.T) {
	// peakIdx 故意挑 510 而非整數倍：對 threshold=50 / n=1000 的 LTTB
	// bucket size ≈ 20.79 而言，510 不會落在 bucketStart。若舊「first-pair
	// representative」實作對 Flat 跑 LTTB，bucket 24 會選 bucketStart=500，
	// 而 pair 1 在 index 500 的值是 0；500 之後的 510 才是峰。
	const n = 1000
	const peakIdx = 510

	timeValues := make([]float64, n)
	flatPair := make([]float64, n)
	peakPair := make([]float64, n)

	for i := range timeValues {
		timeValues[i] = float64(i) * 0.001
		flatPair[i] = 0
		peakPair[i] = 0
	}

	// pair 1 的窄峰：peakIdx 處衝高至 100，相鄰也較低，模擬高 variance 局部事件
	peakPair[peakIdx] = 100
	peakPair[peakIdx-1] = 30
	peakPair[peakIdx+1] = 30

	result := &CCIAnalysisResult{
		TimeValues: timeValues,
		PairResults: []CCIResult{
			{PairName: "Flat", Values: flatPair},
			{PairName: "WithPeak", Values: peakPair},
		},
	}

	const threshold = 50

	out, err := downsampleCCIResult(result, threshold)
	require.NoError(t, err)
	require.NotSame(t, result, out, "資料量 > threshold，應產生新降採樣結果")
	require.Len(t, out.PairResults, 2)

	var peakKept bool

	for _, v := range out.PairResults[1].Values {
		if v == 100 {
			peakKept = true

			break
		}
	}

	assert.True(t, peakKept,
		"pair 1 的窄峰 (y=100) 應被 union LTTB 保留；若僅用 pair 0 representative 會丟失")
}

// TestDownsampleCCI_SharedXAxis 確認所有 pair 與 TimeValues 對齊同一組索引。
func TestDownsampleCCI_SharedXAxis(t *testing.T) {
	const n = 500

	timeValues := make([]float64, n)
	pair0 := make([]float64, n)
	pair1 := make([]float64, n)

	for i := range timeValues {
		timeValues[i] = float64(i) * 0.01
		pair0[i] = float64(i)
		pair1[i] = float64(n - i)
	}

	result := &CCIAnalysisResult{
		TimeValues: timeValues,
		PairResults: []CCIResult{
			{PairName: "P0", Values: pair0},
			{PairName: "P1", Values: pair1},
		},
	}

	out, err := downsampleCCIResult(result, 50)
	require.NoError(t, err)
	require.Len(t, out.PairResults, 2)

	expectLen := len(out.TimeValues)
	assert.Len(t, out.PairResults[0].Values, expectLen,
		"P0 length 必須等於 TimeValues length，保證 X 軸對齊")
	assert.Len(t, out.PairResults[1].Values, expectLen,
		"P1 length 必須等於 TimeValues length，保證 X 軸對齊")
}

// 選 A 嚴格後，舊 TestDownsampleCCI_EmptyPair_Preserved 已過時：
// 它把「長度不符 → silent passthrough」當合約 pin 住，但此行為違反
// CCIAnalysisResult invariant（Values 必須與 TimeValues 1:1 對齊）。
// 對應 reject 行為已搬到 chart_guards_test.go:TestDownsampleCCI_MismatchedLength。
