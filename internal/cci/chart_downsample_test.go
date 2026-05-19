package cci

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCapUnionIndices_BoundsAndPreservesEnds 是 cross-compare review Claude P2
// 的 regression test：union 後的索引總數可能超過 pairCount × threshold（12 對相關
// 曲線最壞 60k 點），需 hard cap 防止前端渲染壓垮。stride decimation 必須：
//
//  1. 結果長度 <= cap（用 cap*2 buffer 但內容不能無限長）
//  2. 保留首索引（index 0 或最小值）— zoom 起點必須對齊
//  3. 保留末索引 — zoom 終點必須對齊
//  4. 維持遞增序
func TestCapUnionIndices_BoundsAndPreservesEnds(t *testing.T) {
	t.Run("under_cap_passthrough", func(t *testing.T) {
		in := []int{0, 5, 10, 100}
		got := capUnionIndices(in, 100)
		assert.Equal(t, in, got, "under cap 應直接 passthrough")
	})

	// 邊界輸入：不該 panic 也不該 silently 拋 nil 給上游當有資料用
	t.Run("zero_limit_passthrough", func(t *testing.T) {
		in := []int{0, 1, 2}
		got := capUnionIndices(in, 0)
		assert.Equal(t, in, got, "limit=0 是退化輸入，原樣 return 比 panic 安全")
	})

	t.Run("negative_limit_passthrough", func(t *testing.T) {
		in := []int{5, 10}
		got := capUnionIndices(in, -1)
		assert.Equal(t, in, got, "limit<0 同樣退化")
	})

	t.Run("empty_input", func(t *testing.T) {
		got := capUnionIndices([]int{}, 100)
		assert.Empty(t, got, "空輸入回空，不該 panic")
	})

	t.Run("nil_input", func(t *testing.T) {
		got := capUnionIndices(nil, 100)
		assert.Empty(t, got)
	})

	t.Run("over_cap_decimates_keeps_ends", func(t *testing.T) {
		in := make([]int, 1000)
		for i := range in {
			in[i] = i * 3
		}
		const cap = 100
		got := capUnionIndices(in, cap)

		assert.LessOrEqual(t, len(got), cap+1, "cap 上限（+1 末尾保留容差）")
		assert.Equal(t, in[0], got[0], "首索引必須保留")
		assert.Equal(t, in[len(in)-1], got[len(got)-1], "末索引必須保留以保持 zoom 終點")

		for i := 1; i < len(got); i++ {
			assert.Greater(t, got[i], got[i-1], "遞增序保持")
		}
	})

	t.Run("vastly_over_cap_strides_correctly", func(t *testing.T) {
		// 模擬 12 對 × 5000 threshold ≈ 60k 點極端 case
		in := make([]int, 60000)
		for i := range in {
			in[i] = i
		}
		const cap = 5000
		got := capUnionIndices(in, cap)

		assert.LessOrEqual(t, len(got), cap+1, "60k → 5k 必須真壓回")
		assert.Equal(t, 0, got[0])
		assert.Equal(t, 59999, got[len(got)-1])
	})

	// codex re-review P2 regression: floor-division stride 在 len % cap != 0 時會超 cap。
	// 例如 len=29999, cap=10000 用 floor 算出 stride=2 → 輸出 ≈ 15000 > cap。
	// ceiling division 修正後保證 output <= cap + 1。
	t.Run("non_multiple_len_still_within_cap", func(t *testing.T) {
		cases := []struct {
			name string
			n    int
			cap  int
		}{
			{"29999_cap_10000_codex_repro", 29999, 10000},
			{"10001_cap_10000_just_over", 10001, 10000},
			{"19999_cap_10000", 19999, 10000},
			{"50001_cap_10000", 50001, 10000},
			{"29999_cap_5000", 29999, 5000},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				in := make([]int, tc.n)
				for i := range in {
					in[i] = i
				}
				got := capUnionIndices(in, tc.cap)
				assert.LessOrEqualf(t, len(got), tc.cap+1,
					"len=%d cap=%d → got %d 點，超過 cap+1 (ceiling stride 失效)",
					tc.n, tc.cap, len(got))
				assert.Equal(t, 0, got[0], "首索引保留")
				assert.Equal(t, tc.n-1, got[len(got)-1], "末索引保留")
			})
		}
	})
}

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
