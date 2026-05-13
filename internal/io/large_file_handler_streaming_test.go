package io

import (
	"math"
	"math/rand"
	"testing"

	"count_mean/internal/models"
)

// runLegacySlidingWindow 是 PR-D 重寫前的舊版演算法 in-test 鏡像。
// 每加入一筆記錄就遍歷整個 dataBuffer 重新 sum，作為 cross-validation
// ground truth。prod 已刪除對應實作（calculateSlidingWindow / manageDataBuffer），
// 這裡保留 byte-for-byte 等價的 logic 給 TestSlidingWindow_StreamingMatchesLegacy。
func runLegacySlidingWindow(data []models.EMGData, windowSize int) ([]float64, [][2]float64) {
	if windowSize <= 0 || len(data) == 0 {
		return nil, nil
	}

	var (
		dataBuffer   []models.EMGData
		maxMeans     []float64
		bestTimes    [][2]float64
		channelCount = -1
	)

	for i := range data {
		if channelCount < 0 {
			channelCount = len(data[i].Channels)
			if channelCount == 0 {
				return nil, nil
			}
			maxMeans = make([]float64, channelCount)
			bestTimes = make([][2]float64, channelCount)
			for c := range maxMeans {
				maxMeans[c] = math.Inf(-1)
			}
		}

		dataBuffer = append(dataBuffer, data[i])
		if len(dataBuffer) >= windowSize {
			calculateSlidingWindowLegacy(dataBuffer, windowSize, maxMeans, bestTimes)
			dataBuffer = manageDataBufferLegacy(dataBuffer, windowSize)
		}
	}

	return maxMeans, bestTimes
}

// calculateSlidingWindowLegacy 直接拷貝舊版 (*LargeFileHandler).calculateSlidingWindow
// 的三層 for-loop。與 prod 結果做 ULP-tolerant 比較。
func calculateSlidingWindowLegacy(
	data []models.EMGData,
	windowSize int,
	maxMeans []float64,
	bestTimes [][2]float64,
) {
	if len(data) < windowSize {
		return
	}

	channelCount := len(data[0].Channels)

	for channelIdx := 0; channelIdx < channelCount; channelIdx++ {
		for startIdx := 0; startIdx <= len(data)-windowSize; startIdx++ {
			sum := 0.0
			for i := startIdx; i < startIdx+windowSize; i++ {
				if channelIdx < len(data[i].Channels) {
					sum += data[i].Channels[channelIdx]
				}
			}

			mean := sum / float64(windowSize)
			if mean > maxMeans[channelIdx] {
				maxMeans[channelIdx] = mean
				bestTimes[channelIdx][0] = data[startIdx].Time
				bestTimes[channelIdx][1] = data[startIdx+windowSize-1].Time
			}
		}
	}
}

// manageDataBufferLegacy 拷貝舊版裁切邏輯。test fixture 不需要 bufferPool 回收。
func manageDataBufferLegacy(dataBuffer []models.EMGData, windowSize int) []models.EMGData {
	bufferLimit := windowSize * 3
	if len(dataBuffer) < bufferLimit {
		return dataBuffer
	}

	keepCount := windowSize * 2
	if keepCount >= len(dataBuffer) {
		return dataBuffer
	}

	copy(dataBuffer, dataBuffer[len(dataBuffer)-keepCount:])
	return dataBuffer[:keepCount]
}

// runStreamingSlidingWindow 用 prod 新版 state 一筆筆 feed 模擬 streaming，
// 抽離 ProcessLargeFileInChunks 的 csv parsing 部份方便單元測試。
func runStreamingSlidingWindow(data []models.EMGData, windowSize int) ([]float64, [][2]float64) {
	state := &slidingWindowState{
		windowSize:      windowSize,
		recalibInterval: chooseRecalibInterval(windowSize),
	}
	for i := range data {
		state.feed(&data[i])
	}
	return state.channelMaxMeans, state.channelBestTimes
}

// TestSlidingWindow_StreamingMatchesLegacy 對合成資料集同時跑新版 streaming
// 與舊版三層 for-loop，以 ULP-tolerant 比較 channelMaxMeans 與 channelBestTimes。
//
// 容差選擇：|new - old| <= 1e-10 * max(|new|, |old|, 1.0)
// rolling sum 與重新累加在 IEEE 754 下加法順序不同必有 ULP 差異，
// 1e-10 相對誤差對 1000-windowSize、float64 輸入綽綽有餘（理論 ULP ~1e-13）。
func TestSlidingWindow_StreamingMatchesLegacy(t *testing.T) {
	cases := []struct {
		name       string
		records    int
		channels   int
		windowSize int
	}{
		{"small_w10_n100_c4", 100, 4, 10},
		{"medium_w100_n1000_c8", 1000, 8, 100},
		{"large_w500_n3000_c16", 3000, 16, 500},
		{"exact_boundary_w100_n100", 100, 4, 100},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := synthesizeEMGData(tc.records, tc.channels, int64(tc.records)+int64(tc.windowSize))

			legacyMaxMeans, legacyBestTimes := runLegacySlidingWindow(data, tc.windowSize)
			streamMaxMeans, streamBestTimes := runStreamingSlidingWindow(data, tc.windowSize)

			if len(legacyMaxMeans) != len(streamMaxMeans) {
				t.Fatalf("channel count mismatch: legacy=%d stream=%d", len(legacyMaxMeans), len(streamMaxMeans))
			}

			for c := range legacyMaxMeans {
				if !nearlyEqual(streamMaxMeans[c], legacyMaxMeans[c], 1e-10) {
					t.Errorf("ch%d maxMean: legacy=%.15f stream=%.15f delta=%.3e",
						c, legacyMaxMeans[c], streamMaxMeans[c],
						math.Abs(streamMaxMeans[c]-legacyMaxMeans[c]))
				}
				// best times 用同一筆記錄的 Time 取自 data，沒有累積誤差，要嚴格相等
				if streamBestTimes[c][0] != legacyBestTimes[c][0] ||
					streamBestTimes[c][1] != legacyBestTimes[c][1] {
					t.Errorf("ch%d bestTimes: legacy=%v stream=%v",
						c, legacyBestTimes[c], streamBestTimes[c])
				}
			}
		})
	}
}

// TestSlidingWindow_EdgeCases 覆蓋 plan 列出的 4 個邊界情況。
func TestSlidingWindow_EdgeCases(t *testing.T) {
	t.Run("windowSize_1_every_record_is_candidate", func(t *testing.T) {
		t.Parallel()
		data := []models.EMGData{
			{Time: 0.1, Channels: []float64{5, 10}},
			{Time: 0.2, Channels: []float64{8, 3}},
			{Time: 0.3, Channels: []float64{2, 12}},
		}
		maxMeans, bestTimes := runStreamingSlidingWindow(data, 1)
		// ch0 max=8@0.2; ch1 max=12@0.3
		expected := []float64{8, 12}
		expectedTimes := [][2]float64{{0.2, 0.2}, {0.3, 0.3}}
		for c := range expected {
			if maxMeans[c] != expected[c] {
				t.Errorf("ch%d maxMean: want %v got %v", c, expected[c], maxMeans[c])
			}
			if bestTimes[c] != expectedTimes[c] {
				t.Errorf("ch%d bestTimes: want %v got %v", c, expectedTimes[c], bestTimes[c])
			}
		}
	})

	t.Run("N_less_than_windowSize_no_results", func(t *testing.T) {
		t.Parallel()
		// windowSize=100 但只給 50 筆 → 永遠湊不滿窗口
		data := synthesizeEMGData(50, 3, 50)
		maxMeans, _ := runStreamingSlidingWindow(data, 100)
		if maxMeans == nil {
			t.Fatal("expect channelMaxMeans initialized to length 3")
		}
		if len(maxMeans) != 3 {
			t.Fatalf("expect 3 channels, got %d", len(maxMeans))
		}
		for c, v := range maxMeans {
			if !math.IsInf(v, -1) {
				t.Errorf("ch%d expect -Inf (no window completed), got %v", c, v)
			}
		}
	})

	t.Run("empty_channels_short_circuits", func(t *testing.T) {
		t.Parallel()
		// 第一筆 channels=[] → initRingIfNeeded 應 short-circuit，不該無限迴圈
		data := []models.EMGData{
			{Time: 0.1, Channels: []float64{}},
			{Time: 0.2, Channels: []float64{}},
		}
		maxMeans, bestTimes := runStreamingSlidingWindow(data, 1)
		if maxMeans != nil {
			t.Errorf("expect nil maxMeans for empty channels, got %v", maxMeans)
		}
		if bestTimes != nil {
			t.Errorf("expect nil bestTimes for empty channels, got %v", bestTimes)
		}
	})

	t.Run("all_negative_values_keeps_max_finite", func(t *testing.T) {
		t.Parallel()
		// math.Inf(-1) 初始化讓全負資料仍能找到 max（最不負那個）
		data := []models.EMGData{
			{Time: 0.1, Channels: []float64{-10, -20}},
			{Time: 0.2, Channels: []float64{-5, -25}},
			{Time: 0.3, Channels: []float64{-8, -15}},
		}
		maxMeans, _ := runStreamingSlidingWindow(data, 2)
		// ch0: avg(-10,-5)=-7.5, avg(-5,-8)=-6.5 → max=-6.5
		// ch1: avg(-20,-25)=-22.5, avg(-25,-15)=-20.0 → max=-20.0
		if !nearlyEqual(maxMeans[0], -6.5, 1e-12) {
			t.Errorf("ch0: want -6.5, got %v", maxMeans[0])
		}
		if !nearlyEqual(maxMeans[1], -20.0, 1e-12) {
			t.Errorf("ch1: want -20.0, got %v", maxMeans[1])
		}
	})
}

// TestSlidingWindow_NaNDoesNotPoisonRollingSum 是 codex review P2 finding A
// 的 regression test：NaN 進 rolling sum 後（NaN - NaN 仍是 NaN）會永久汙染該
// channel，導致後續正常 window 永遠拿不到 max。修法：feed 在 NaN row 重置 ring
// 狀態；最終 max 結果與 legacy「window 跨越 NaN row 不入選 max」等價。
func TestSlidingWindow_NaNDoesNotPoisonRollingSum(t *testing.T) {
	// codex 原始反例：windowSize=2, records=[1, NaN, 100, 100]
	// legacy: window [r2,r3] mean=100 為 max
	// 修前新版: NaN 汙染所有後續 → max=-Inf
	// 修後新版: NaN row reset → 後續 [r2,r3] 形成完整 window，max=100，bestTimes=[0.2, 0.3]
	data := []models.EMGData{
		{Time: 0.0, Channels: []float64{1}},
		{Time: 0.1, Channels: []float64{math.NaN()}},
		{Time: 0.2, Channels: []float64{100}},
		{Time: 0.3, Channels: []float64{100}},
	}
	maxMeans, bestTimes := runStreamingSlidingWindow(data, 2)
	if !nearlyEqual(maxMeans[0], 100.0, 1e-12) {
		t.Errorf("NaN 汙染 regression: want max=100, got %v", maxMeans[0])
	}
	if bestTimes[0] != [2]float64{0.2, 0.3} {
		t.Errorf("NaN bestTimes regression: want [0.2 0.3], got %v", bestTimes[0])
	}

	// 另一個 case：NaN 出現在 ring 已 wrap 後（rolling stage），驗證 reset 在 rollRing 階段也作用。
	// 注意：reset 之後 r4 是 recordsSeen=0 後的第一筆，本 case 不會再形成完整 window，
	// 因此主要驗證「重置不會破壞先前已建立的 max（25）」與「不會 stitching 出 35」。
	// post-reset rollRing 路徑由 TestSlidingWindow_NaNDoesNotStitchAcrossRows.data2 (windowSize=3) 覆蓋。
	data2 := []models.EMGData{
		{Time: 0.0, Channels: []float64{10, 20}},
		{Time: 0.1, Channels: []float64{20, 30}},
		{Time: 0.2, Channels: []float64{30, 40}},         // ring full, first compareMax
		{Time: 0.3, Channels: []float64{math.NaN(), 50}}, // 觸發 reset
		{Time: 0.4, Channels: []float64{40, 50}},
	}
	m2, _ := runStreamingSlidingWindow(data2, 2)
	// legacy semantics ch0 windows（NaN 進 sum → NaN window 不選為 max）:
	//   [r0,r1]=15, [r1,r2]=25, [r2,r3]=NaN, [r3,r4]=NaN → max=25
	// 修前新版：NaN return 不前進 → r2(30) 與 r4(40) 黏成相鄰 window=(30+40)/2=35（錯）
	// 修後新版：NaN row reset → r4 為 reset 後第一筆，無完整 window → max 維持 25
	if !nearlyEqual(m2[0], 25.0, 1e-12) {
		t.Errorf("NaN row stitching regression ch0: want 25 (legacy), got %v", m2[0])
	}
}

// TestSlidingWindow_NaNDoesNotStitchAcrossRows 是 codex review (cross-compare wave)
// 反例：當 NaN 前的 row 值高、NaN 後的 row 值低時，舊修法「跳過 NaN row 不前進」
// 會把非相鄰的高/低 row 黏成 window，false-positive 為 max。
// 真實 user 衝擊：max-mean 過度報告，bestTimes 對應 source 中不連續的時間段。
func TestSlidingWindow_NaNDoesNotStitchAcrossRows(t *testing.T) {
	// codex 反例：windowSize=2, records=[100, NaN, 1, 1]
	// legacy: window [r0,r1]=NaN, [r1,r2]=NaN, [r2,r3]=1 → max=1, bestTimes=[0.2, 0.3]
	// 修前新版（stitching bug）：跳過 r1，r0+r2 當作相鄰 window=(100+1)/2=50.5（錯）
	// 修後新版：NaN row reset → r2 開始重新累積，[r2,r3] mean=1 為唯一有效 max
	data := []models.EMGData{
		{Time: 0.0, Channels: []float64{100}},
		{Time: 0.1, Channels: []float64{math.NaN()}},
		{Time: 0.2, Channels: []float64{1}},
		{Time: 0.3, Channels: []float64{1}},
	}
	maxMeans, bestTimes := runStreamingSlidingWindow(data, 2)
	if !nearlyEqual(maxMeans[0], 1.0, 1e-12) {
		t.Errorf("stitching regression: want max=1 (legacy), got %v (stitched [100,1]=50.5?)",
			maxMeans[0])
	}
	if bestTimes[0] != [2]float64{0.2, 0.3} {
		t.Errorf("stitching bestTimes regression: want [0.2, 0.3], got %v", bestTimes[0])
	}

	// 另一個 case：NaN 在 rolling 階段觸發。windowSize=3，NaN 在 r3 → r4,r5,r6 為新 window
	data2 := []models.EMGData{
		{Time: 0.0, Channels: []float64{50}},
		{Time: 0.1, Channels: []float64{50}},
		{Time: 0.2, Channels: []float64{50}},         // [50,50,50] mean=50
		{Time: 0.3, Channels: []float64{math.NaN()}}, // reset
		{Time: 0.4, Channels: []float64{1}},
		{Time: 0.5, Channels: []float64{1}},
		{Time: 0.6, Channels: []float64{1}}, // [1,1,1] mean=1
	}
	m2, bt2 := runStreamingSlidingWindow(data2, 3)
	// legacy: 唯一全 finite window 是 [r0..r2] mean=50 與 [r4..r6] mean=1 → max=50
	// 修前 stitching: r0,r1,r2,r4 等等 → 可能 over-report
	// 修後 reset: 完整 window [r0..r2]=50 與 [r4..r6]=1 → max=50, bestTimes=[0.0, 0.2]
	if !nearlyEqual(m2[0], 50.0, 1e-12) {
		t.Errorf("rolling-stage stitching regression: want 50 got %v", m2[0])
	}
	if bt2[0] != [2]float64{0.0, 0.2} {
		t.Errorf("rolling-stage stitching bestTimes: want [0.0, 0.2] got %v", bt2[0])
	}
}

// TestSlidingWindow_OversizedWindowSmallFile 是 codex review P2 finding B 的
// regression test：當 windowSize 遠大於實際資料筆數時，舊修法在第一筆就 alloc
// windowSize × channels 的 ring。改為動態 grow 後，ringValues 只長到 N（≤ initialRingCap*次冪）。
func TestSlidingWindow_OversizedWindowSmallFile(t *testing.T) {
	state := &slidingWindowState{
		windowSize:      10_000_000, // 10M slots × 8 channels × 8 bytes = 640MB if eagerly allocated
		recalibInterval: chooseRecalibInterval(10_000_000),
	}
	data := synthesizeEMGData(50, 8, 999)
	for i := range data {
		state.feed(&data[i])
	}

	// 50 筆 record + 8 channels：ring 動態 grow，cap 從 64 起，append 50 筆永遠不會
	// grow 到 10M。len(ringValues) 應 == 50，cap 應 << windowSize。
	if got := len(state.ringValues); got != 50 {
		t.Errorf("ring length: want 50 (matches input records), got %d", got)
	}
	// N=50 < initialRingCap=64，append 永遠不會觸發 grow → cap 應恰好等於 initialRingCap
	if got := cap(state.ringValues); got != initialRingCap {
		t.Errorf("ring cap should remain at initialRingCap=%d for N<<windowSize: got cap=%d (windowSize=%d)",
			initialRingCap, got, state.windowSize)
	}

	// 結果：N < windowSize 永遠無完整 window，channelMaxMeans 維持 -Inf
	for c, v := range state.channelMaxMeans {
		if !math.IsInf(v, -1) {
			t.Errorf("ch%d: expect -Inf (no complete window), got %v", c, v)
		}
	}
}

// TestSlidingWindow_RecalibrationConverges 證明週期校準後 windowSums 與從 ring
// 直接累加完全等價（recalibrate 沒有靜默 reset 副作用）。
func TestSlidingWindow_RecalibrationConverges(t *testing.T) {
	state := &slidingWindowState{
		windowSize:      50,
		recalibInterval: chooseRecalibInterval(50),
	}
	data := synthesizeEMGData(500, 4, 123)
	for i := range data {
		state.feed(&data[i])
	}

	// 手動 recalibrate 後 windowSums 不應變動（誤差 ≤ ULP）
	prevSums := append([]float64(nil), state.windowSums...)
	state.recalibrate()
	for c, v := range state.windowSums {
		if !nearlyEqual(v, prevSums[c], 1e-10) {
			t.Errorf("recalibrate drift ch%d: before=%.15f after=%.15f", c, prevSums[c], v)
		}
	}
}

// BenchmarkSlidingWindow_FeedOnly 量純演算法層 cost（跳過 CSV parsing / logger /
// path validator）。對照 BenchmarkLargeFileHandler_SlidingWindow（含全鏈路）：
// 純演算法應 < 50ms / 100K records，反映 O(n × channels) rolling sum 真實複雜度。
func BenchmarkSlidingWindow_FeedOnly(b *testing.B) {
	const (
		records    = 100_000
		channels   = 16
		windowSize = 1000
	)
	data := synthesizeEMGData(records, channels, 42)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		state := &slidingWindowState{
			windowSize:      windowSize,
			recalibInterval: chooseRecalibInterval(windowSize),
		}
		for j := range data {
			state.feed(&data[j])
		}
	}
}

// synthesizeEMGData 用固定 seed 產生可重現合成資料集。
func synthesizeEMGData(records, channels int, seed int64) []models.EMGData {
	rng := rand.New(rand.NewSource(seed)) //nolint:gosec // test fixture deterministic seed
	data := make([]models.EMGData, records)
	for i := 0; i < records; i++ {
		ch := make([]float64, channels)
		for c := 0; c < channels; c++ {
			ch[c] = rng.Float64()*100 - 50
		}
		data[i] = models.EMGData{
			Time:     float64(i) * 0.001,
			Channels: ch,
		}
	}
	return data
}

// nearlyEqual 是 ULP-tolerant 相對誤差比較：
//
//	|a - b| <= eps * max(|a|, |b|, 1.0)
//
// 1.0 floor 防止接近 0 時被相對誤差放大成假陽性。
func nearlyEqual(a, b, eps float64) bool {
	diff := math.Abs(a - b)
	scale := math.Max(math.Max(math.Abs(a), math.Abs(b)), 1.0)
	return diff <= eps*scale
}

// TestSlidingWindow_MismatchedChannelCountIncrementsDroppedCounter 驗證 channel
// 數量與初始 row 不一致時 feed 累計 droppedRowCount，供 ProcessLargeFileInChunks
// 結束時 log warning。歷史問題：原本 silent return 讓 operator 在資料毀損時
// 無感（review 抓到的觀測性缺口）。
func TestSlidingWindow_MismatchedChannelCountIncrementsDroppedCounter(t *testing.T) {
	state := &slidingWindowState{
		windowSize:      3,
		recalibInterval: chooseRecalibInterval(3),
	}

	// 先送 3 筆正常的雙通道 row，state.windowSums 會初始化為 len=2
	normal := []models.EMGData{
		{Time: 0.001, Channels: []float64{1.0, 2.0}},
		{Time: 0.002, Channels: []float64{1.5, 2.5}},
		{Time: 0.003, Channels: []float64{2.0, 3.0}},
	}
	for i := range normal {
		state.feed(&normal[i])
	}
	if state.droppedRowCount != 0 {
		t.Fatalf("expected 0 dropped rows after normal feed, got %d", state.droppedRowCount)
	}

	// 送 2 筆 channel-count 不一致的 row（單通道、三通道）
	mismatched := []models.EMGData{
		{Time: 0.004, Channels: []float64{42.0}},
		{Time: 0.005, Channels: []float64{1.0, 2.0, 3.0}},
	}
	for i := range mismatched {
		state.feed(&mismatched[i])
	}

	if state.droppedRowCount != 2 {
		t.Errorf("expected droppedRowCount=2, got %d", state.droppedRowCount)
	}
}
