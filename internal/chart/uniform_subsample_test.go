package chart

import (
	"testing"

	"count_mean/internal/models"
)

// TestUniformSubsample_MaxOutputPointsCap 釘住 修補:
// UniformSubsample 加 OOM cap (uniformSubsampleMaxOutputPoints) — 防止
// caller 對 100M-row 資料集呼叫 UniformSubsample(_, 1) (rate ≤ 1 fast-path)
// 或極低 rate 時噴出超大輸出耗盡記憶體。
//
// 與 calculator backpressure 設計對齊:UI / 序列化 / 圖表渲染都需 hard cap。
//
// 修法:輸出資料超過 uniformSubsampleMaxOutputPoints (200_000) 時,在末端做
// 確定性 stride decimation 壓回上限,首末筆保留以維持 zoom 範圍正確。
func TestUniformSubsample_MaxOutputPointsCap(t *testing.T) {
	// 構造一個遠超 cap 的 dataset
	const oversizedN = uniformSubsampleMaxOutputPoints + 50_000

	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data:    make([]models.EMGData, oversizedN),
	}
	for i := 0; i < oversizedN; i++ {
		dataset.Data[i] = models.EMGData{
			Time:     float64(i) * 0.001,
			Channels: []float64{float64(i)},
		}
	}

	// samplingRate = 1 → pass-through 路徑,但有 cap 後不再原樣回傳
	got := UniformSubsample(dataset, 1)
	if got == nil {
		t.Fatal("UniformSubsample returned nil")
	}

	if len(got.Data) > uniformSubsampleMaxOutputPoints+1 {
		t.Errorf("output exceeded MaxOutputPoints cap: len=%d cap=%d",
			len(got.Data), uniformSubsampleMaxOutputPoints)
	}

	// 首筆 / 末筆必須保留 (zoom 範圍正確性)
	if got.Data[0].Time != dataset.Data[0].Time {
		t.Errorf("first sample not preserved: got %v want %v",
			got.Data[0].Time, dataset.Data[0].Time)
	}
	last := got.Data[len(got.Data)-1]
	want := dataset.Data[len(dataset.Data)-1]
	if last.Time != want.Time {
		t.Errorf("last sample not preserved: got %v want %v", last.Time, want.Time)
	}
}

// TestUniformSubsample_LastIndexComparison_NotTimeFloat 釘住 修補:
// 末筆判斷從「比較 Time 浮點」改成「比較 index」。
//
// 修補前:
//
//	if sampledData.Data[len-1].Time != dataset.Data[lastIdx].Time { append last }
//
// 若 samplingRate 剛好讓 stride 覆蓋到 lastIdx 但 Time 因為浮點精度有 ULP 差,
// 比較會誤判「不同」而 append 末筆 → 末筆重複。
// 修補後改 index 比較,確定性。
//
// 此 test 構造一個 stride 剛好命中 last 但 Time 在中間有微小浮點誤差的場景,
// 確認末筆不會被重複 append。
func TestUniformSubsample_LastIndexComparison_NotTimeFloat(t *testing.T) {
	// samplingRate = 3,n = 10 → stride 命中 idx 0,3,6,9
	// idx 9 = lastIdx,所以「末筆已被 stride 覆蓋」
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{0}},
			{Time: 0.1, Channels: []float64{1}},
			{Time: 0.2, Channels: []float64{2}},
			{Time: 0.3, Channels: []float64{3}}, // ← stride idx 3
			{Time: 0.4, Channels: []float64{4}},
			{Time: 0.5, Channels: []float64{5}},
			{Time: 0.6, Channels: []float64{6}}, // ← stride idx 6
			{Time: 0.7, Channels: []float64{7}},
			{Time: 0.8, Channels: []float64{8}},
			{Time: 0.9, Channels: []float64{9}}, // ← stride idx 9 = lastIdx
		},
	}

	got := UniformSubsample(dataset, 3)
	// 預期 indices: 0, 3, 6, 9 = 4 筆。若 失敗 (誤 append 末筆),會變 5 筆。
	if len(got.Data) != 4 {
		t.Errorf("expected 4 samples (0,3,6,9), got %d: %+v", len(got.Data), got.Data)
	}
	// 末筆必須是 idx 9 (Time = 0.9)
	if got.Data[len(got.Data)-1].Time != 0.9 {
		t.Errorf("last sample wrong: got Time=%v want 0.9", got.Data[len(got.Data)-1].Time)
	}
}

// TestUniformSubsample_Rate2Branch_RespectsCap 守護 
//
// past rate <= 1 branch 在 dataset > cap 時會放大 rate 把輸出壓回 cap,但
// rate >= 2 branch 卻完全沒這層守門 — 對 1M 點資料,caller 傳 rate=2 會噴
// 出 500K 輸出,遠超 200K cap,違反「UniformSubsample 輸出永遠 ≤ cap」契約。
//
// 修法後 rate >= 2 branch 估算輸出長度,超出 cap 時放大 rate 壓回 cap
// (與 rate <= 1 branch 相同的 ceil division)。此 test 守住兩條 invariant:
//
//  1. 對 1M-點 dataset 傳 rate=2,輸出仍 ≤ cap+1 (+1 允許末筆 append)
//  2. 末筆保留 (zoom 範圍契約不變)
//  3. 控制組:小於 cap 的場景 (10K 點 rate=2),行為不變(輸出 = 5000 點)
func TestUniformSubsample_Rate2Branch_RespectsCap(t *testing.T) {
	t.Run("oversized_input_rate_2_capped", func(t *testing.T) {
		// 構造 1M 點 dataset,rate=2 原本會噴 500K 輸出 (>> 200K cap)
		const n = 1_000_000
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    make([]models.EMGData, n),
		}
		for i := 0; i < n; i++ {
			dataset.Data[i] = models.EMGData{
				Time:     float64(i) * 0.001,
				Channels: []float64{float64(i)},
			}
		}

		got := UniformSubsample(dataset, 2)
		if got == nil {
			t.Fatal("UniformSubsample returned nil")
		}

		// 輸出必須 ≤ cap+1 (+1 給末筆 append 邊界)
		if len(got.Data) > uniformSubsampleMaxOutputPoints+1 {
			t.Errorf("rate=2 branch 違反 cap 契約: len=%d cap=%d (原 rate 走完後輸出超出 cap)",
				len(got.Data), uniformSubsampleMaxOutputPoints)
		}

		// 末筆仍保留 (zoom 範圍正確性)
		last := got.Data[len(got.Data)-1]
		want := dataset.Data[len(dataset.Data)-1]
		if last.Time != want.Time {
			t.Errorf("末筆未保留: got Time=%v want %v", last.Time, want.Time)
		}
	})

	t.Run("within_cap_rate_2_unchanged", func(t *testing.T) {
		// 控制組:10K 點 rate=2 → 預期 5000 點,遠低於 cap,行為應不變
		const n = 10_000
		dataset := &models.EMGDataset{
			Headers: []string{"Time", "Ch1"},
			Data:    make([]models.EMGData, n),
		}
		for i := 0; i < n; i++ {
			dataset.Data[i] = models.EMGData{
				Time:     float64(i) * 0.001,
				Channels: []float64{float64(i)},
			}
		}

		got := UniformSubsample(dataset, 2)
		// stride 2 → 5000 個 sampled indices + 末筆可能 append (n 為偶數,lastIdx=9999,
		// 9999%2 != 0,所以末筆會被 append) → 5001 點
		expected := n/2 + 1
		if len(got.Data) != expected {
			t.Errorf("控制組:10K 點 rate=2 預期 %d 筆,got %d (cap 不應在此 path 觸發)",
				expected, len(got.Data))
		}
	})
}

// TestUniformSubsample_StrideMissesLast_AppendsLast 確認當 stride 漏掉末筆時,
// 修補後仍會 append 末筆 (zoom 範圍正確性的核心契約)。
func TestUniformSubsample_StrideMissesLast_AppendsLast(t *testing.T) {
	// samplingRate = 3,n = 11 → stride 命中 idx 0,3,6,9;lastIdx = 10 未被覆蓋
	dataset := &models.EMGDataset{
		Headers: []string{"Time", "Ch1"},
		Data: []models.EMGData{
			{Time: 0.0, Channels: []float64{0}},
			{Time: 0.1, Channels: []float64{1}},
			{Time: 0.2, Channels: []float64{2}},
			{Time: 0.3, Channels: []float64{3}},
			{Time: 0.4, Channels: []float64{4}},
			{Time: 0.5, Channels: []float64{5}},
			{Time: 0.6, Channels: []float64{6}},
			{Time: 0.7, Channels: []float64{7}},
			{Time: 0.8, Channels: []float64{8}},
			{Time: 0.9, Channels: []float64{9}},
			{Time: 1.0, Channels: []float64{10}}, // lastIdx
		},
	}

	got := UniformSubsample(dataset, 3)
	// 預期 indices: 0, 3, 6, 9 + lastIdx 10 = 5 筆
	if len(got.Data) != 5 {
		t.Errorf("expected 5 samples (stride 0,3,6,9 + last 10), got %d", len(got.Data))
	}
	if got.Data[len(got.Data)-1].Time != 1.0 {
		t.Errorf("last sample wrong: got Time=%v want 1.0", got.Data[len(got.Data)-1].Time)
	}
}
