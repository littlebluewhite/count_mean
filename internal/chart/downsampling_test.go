package chart

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLTTBDownsample_BelowOrAtThreshold_ReturnsAllIndices(t *testing.T) {
	xs := []float64{0, 1, 2, 3, 4}
	ys := []float64{0, 1, 4, 9, 16}

	indices := LTTBDownsample(xs, ys, 10)

	assert.Equal(t, []int{0, 1, 2, 3, 4}, indices,
		"threshold > len: 全索引回傳")
}

func TestLTTBDownsample_EqualThreshold_ReturnsAllIndices(t *testing.T) {
	xs := []float64{0, 1, 2, 3, 4}
	ys := []float64{0, 1, 4, 9, 16}

	indices := LTTBDownsample(xs, ys, 5)

	assert.Equal(t, []int{0, 1, 2, 3, 4}, indices,
		"threshold == len: 全索引回傳")
}

func TestLTTBDownsample_ThresholdTwoOrLess_ReturnsAllIndices(t *testing.T) {
	xs := []float64{0, 1, 2, 3, 4}
	ys := []float64{0, 1, 4, 9, 16}

	indices := LTTBDownsample(xs, ys, 2)

	assert.Equal(t, []int{0, 1, 2, 3, 4}, indices,
		"threshold <= 2: LTTB 無意義，退化為全索引")
}

func TestLTTBDownsample_PreservesEndpoints(t *testing.T) {
	n := 1000
	xs := make([]float64, n)
	ys := make([]float64, n)

	for i := range xs {
		xs[i] = float64(i)
		ys[i] = math.Sin(float64(i) * 0.05)
	}

	indices := LTTBDownsample(xs, ys, 50)

	require.Len(t, indices, 50)
	assert.Equal(t, 0, indices[0], "第一個索引必為 0")
	assert.Equal(t, n-1, indices[len(indices)-1], "最後一個索引必為 n-1")
}

func TestLTTBDownsample_IndicesAreMonotonic(t *testing.T) {
	n := 10_000
	xs := make([]float64, n)
	ys := make([]float64, n)

	for i := range xs {
		xs[i] = float64(i) * 0.001
		ys[i] = math.Sin(xs[i]*10) + 0.3*math.Sin(xs[i]*47)
	}

	indices := LTTBDownsample(xs, ys, 200)

	require.Len(t, indices, 200)

	for i := 1; i < len(indices); i++ {
		assert.Greater(t, indices[i], indices[i-1],
			"索引應嚴格遞增 (i=%d, prev=%d, curr=%d)",
			i, indices[i-1], indices[i])
	}
}

func TestLTTBDownsample_IndicesInBounds(t *testing.T) {
	n := 5_000
	xs := make([]float64, n)
	ys := make([]float64, n)

	for i := range xs {
		xs[i] = float64(i)
		ys[i] = float64(i % 100)
	}

	indices := LTTBDownsample(xs, ys, 100)

	for _, idx := range indices {
		assert.GreaterOrEqual(t, idx, 0)
		assert.Less(t, idx, n)
	}
}

func TestLTTBDownsample_MismatchedLength_Panics(t *testing.T) {
	xs := []float64{0, 1, 2, 3}
	ys := []float64{0, 1}

	assert.Panics(t, func() {
		LTTBDownsample(xs, ys, 3)
	}, "xs/ys 長度不一致應 panic")
}

func TestLTTBDownsample_CapturesAmplitudeInHighVarianceRegions(t *testing.T) {
	// 1000 點曲線：前 500 點 y=0，後 500 點正弦震盪 (振幅 ~1.0)。
	// LTTB bucket size 固定，前後各約 25 個取樣點；驗證的不是取樣分佈
	// 而是「後半段取到的 Y 值覆蓋原訊號振幅」— 即 LTTB 在每個 bucket
	// 內成功挑出局部極值，而非平庸地選 bucketStart。
	n := 1000
	xs := make([]float64, n)
	ys := make([]float64, n)

	for i := range xs {
		xs[i] = float64(i)
		if i < n/2 {
			ys[i] = 0
		} else {
			ys[i] = math.Sin(float64(i) * 0.5)
		}
	}

	indices := LTTBDownsample(xs, ys, 50)

	var minY, maxY float64

	for _, idx := range indices {
		if idx >= n/2 {
			if ys[idx] < minY {
				minY = ys[idx]
			}

			if ys[idx] > maxY {
				maxY = ys[idx]
			}
		}
	}

	assert.Greater(t, maxY-minY, 1.5,
		"LTTB 應挑到正弦的峰與谷 (Y 範圍 %.3f..%.3f)，覆蓋振幅至少 1.5", minY, maxY)
}

// TestCapUnionIndices_BoundsAndPreservesEnds: union 後索引總數可能超過 pairCount × threshold，
// 需 hard cap 防前端渲染壓垮。stride decimation 必須：1) 長度 <= cap(+1 末尾容差) 2) 保留首索引
// 3) 保留末索引 4) 維持遞增序。
func TestCapUnionIndices_BoundsAndPreservesEnds(t *testing.T) {
	t.Run("under_cap_passthrough", func(t *testing.T) {
		in := []int{0, 5, 10, 100}
		got := capUnionIndices(in, 100)
		assert.Equal(t, in, got, "under cap 應直接 passthrough")
	})

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

// TestUnionLTTBIndices_DegeneratePassthrough_ReturnsNil 驗證三種退化條件均回傳 nil：
// 1) 時間序列長度 <= threshold（無需降採樣）
// 2) threshold <= 0（無效 threshold）
// 3) series 為空（無曲線可處理）
func TestUnionLTTBIndices_DegeneratePassthrough_ReturnsNil(t *testing.T) {
	t.Run("time_len_le_threshold", func(t *testing.T) {
		time := []float64{0, 1, 2, 3}
		series := [][]float64{{0, 1, 2, 3}}
		got := UnionLTTBIndices(time, series, 100)
		assert.Nil(t, got, "len(time)=4 <= threshold=100 應回傳 nil")
	})

	t.Run("threshold_zero", func(t *testing.T) {
		time := []float64{0, 1, 2, 3, 4, 5}
		series := [][]float64{{0, 1, 2, 3, 4, 5}}
		got := UnionLTTBIndices(time, series, 0)
		assert.Nil(t, got, "threshold=0 應回傳 nil")
	})

	t.Run("threshold_negative", func(t *testing.T) {
		time := []float64{0, 1, 2, 3, 4, 5}
		series := [][]float64{{0, 1, 2, 3, 4, 5}}
		got := UnionLTTBIndices(time, series, -5)
		assert.Nil(t, got, "threshold<0 應回傳 nil")
	})

	t.Run("empty_series", func(t *testing.T) {
		time := make([]float64, 500)
		for i := range time {
			time[i] = float64(i)
		}
		got := UnionLTTBIndices(time, [][]float64{}, 50)
		assert.Nil(t, got, "series 為空應回傳 nil")
	})
}

// TestUnionLTTBIndices_ReturnsSortedSharedIndices 驗證回傳索引嚴格遞增、範圍合法、
// 首尾對齊。兩條方向相反的曲線（遞增 vs. 遞減）會讓 LTTB 在不同位置選取最大面積點，
// union 後確保覆蓋兩條曲線的高 variance 區段。
func TestUnionLTTBIndices_ReturnsSortedSharedIndices(t *testing.T) {
	const n = 500
	const threshold = 50

	time := make([]float64, n)
	rising := make([]float64, n)
	falling := make([]float64, n)

	for i := range time {
		time[i] = float64(i) * 0.01
		rising[i] = float64(i)
		falling[i] = float64(n - i)
	}

	got := UnionLTTBIndices(time, [][]float64{rising, falling}, threshold)

	require.NotNil(t, got, "n=500 > threshold=50，應產生索引集")
	assert.Equal(t, 0, got[0], "首索引必須為 0")
	assert.Equal(t, n-1, got[len(got)-1], "末索引必須為 n-1")

	for i := 1; i < len(got); i++ {
		assert.Greater(t, got[i], got[i-1],
			"索引應嚴格遞增 (i=%d, prev=%d, curr=%d)", i, got[i-1], got[i])
		assert.Less(t, got[i], n, "索引必須在 [0, n) 範圍內")
	}
}

// TestUnionLTTBIndices_PreservesEndpoints 確認真實降採樣路徑（n=1000, threshold=50）
// 的輸出首尾各為 0 與 n-1。LTTB 本身保證首尾，union 不應破壞這個屬性。
func TestUnionLTTBIndices_PreservesEndpoints(t *testing.T) {
	const n = 1000
	const threshold = 50

	time := make([]float64, n)
	s := make([]float64, n)

	for i := range time {
		time[i] = float64(i) * 0.001
		s[i] = math.Sin(float64(i) * 0.05)
	}

	got := UnionLTTBIndices(time, [][]float64{s}, threshold)

	require.NotNil(t, got)
	assert.Equal(t, 0, got[0], "首索引必須為 0")
	assert.Equal(t, n-1, got[len(got)-1], "末索引必須為 n-1")
}

// TestUnionLTTBIndices_UnionPreservesNonRepresentativePeak 是 union vs. single-representative
// 的 key regression test。若只對平緩曲線跑一次 LTTB（representative 策略），
// 窄峰索引幾乎必然落在別的 bucket 邊緣而不被選取。
// union 策略對每條曲線各自跑 LTTB，確保峰值索引進入 union 集合。
func TestUnionLTTBIndices_UnionPreservesNonRepresentativePeak(t *testing.T) {
	const n = 1000
	const peakIdx = 510 // 故意不是 bucket 邊界的整數倍

	time := make([]float64, n)
	flat := make([]float64, n)
	peak := make([]float64, n)

	for i := range time {
		time[i] = float64(i) * 0.001
		flat[i] = 0
		peak[i] = 0
	}

	// 模擬窄峰事件：主峰高達 100，相鄰點拉高至 30 以增強局部 variance
	peak[peakIdx] = 100
	peak[peakIdx-1] = 30
	peak[peakIdx+1] = 30

	const threshold = 50

	got := UnionLTTBIndices(time, [][]float64{flat, peak}, threshold)

	require.NotNil(t, got)

	found := false

	for _, idx := range got {
		if idx == peakIdx {
			found = true

			break
		}
	}

	assert.True(t, found,
		"index %d（峰值點）應被 union LTTB 保留；single-representative 對 flat 跑 LTTB 會錯過它", peakIdx)
}

// TestUnionLTTBIndices_CapsAtThresholdTimesTwoPlusOne 驗證 kernel 確實接上 capUnionIndices，
// 使輸出不超過 threshold*2+1。構造多條曲線讓 union 索引集遠超 threshold，
// 確認 hard cap 生效。
func TestUnionLTTBIndices_CapsAtThresholdTimesTwoPlusOne(t *testing.T) {
	const n = 2000
	const threshold = 10 // 小 threshold 讓 threshold*2=20 更容易被突破

	time := make([]float64, n)
	for i := range time {
		time[i] = float64(i) * 0.001
	}

	// 建立 6 條各自在不同區段有峰值的曲線，讓 union 索引集遠超 threshold*2
	seriesCount := 6
	series := make([][]float64, seriesCount)

	for s := 0; s < seriesCount; s++ {
		vals := make([]float64, n)
		// 每條曲線在不同的 1/6 段插入高峰，其餘為 0
		segStart := s * (n / seriesCount)
		segEnd := segStart + n/seriesCount

		for i := segStart; i < segEnd; i++ {
			vals[i] = math.Sin(float64(i-segStart)*0.1) * 100
		}

		series[s] = vals
	}

	got := UnionLTTBIndices(time, series, threshold)

	require.NotNil(t, got)
	assert.LessOrEqual(t, len(got), threshold*2+1,
		"union 後應 cap 至 threshold*2+1=%d，got %d 點", threshold*2+1, len(got))
}
