package calculator

import (
	"context"
	"fmt"

	"count_mean/internal/models"
)

// slidingWindowCtxCheckInterval 是 CalculateMaxMean 內 hot-loop 的「批次邊界」
// 計數,用於 修正前的 4096-iter 批次檢查。修法後改為「每輪 select-default
// 檢查 ctx」(per-iter),這個常數已不再用於控流,僅保留 doc 對齊歷史說明。
// 4096 的 throughput / cancel-latency 折衷論證仍見 commit。
//
// 修法後 hot-loop overhead 雖略增 (select-default 在 hot path 約 +1ns/iter,
// 1M-iter sliding window 約 +1ms),但對 trimmed range < 4096 row 也能立即響應
// cancel,契約完整性 > 0.01% 額外開銷。
const slidingWindowCtxCheckInterval = 4096

// DataProvider 提供滑動窗口計算所需的數據訪問接口.
type DataProvider interface {
	// GetValue 獲取指定數據索引和通道索引的值.
	GetValue(dataIdx, channelIdx int) float64
	// Length 返回數據長度.
	Length() int
}

// SlidingWindowResult 滑動窗口計算結果.
type SlidingWindowResult struct {
	MaxMean      float64 // 最大平均值
	BestStartIdx int     // 最佳窗口起始索引
}

// SlidingWindowCalculator 滑動窗口計算器.
type SlidingWindowCalculator struct{}

// NewSlidingWindowCalculator 創建新的滑動窗口計算器.
func NewSlidingWindowCalculator() *SlidingWindowCalculator {
	return &SlidingWindowCalculator{}
}

// CalculateMaxMean 計算指定範圍內的最大平均值（使用增量滑動窗口優化）。
//
// 參數：
//   - ctx: 取消用 context。hot-loop 每輪 select-default 檢查 ctx.Done(),
//     ctx 取消時提早回傳 ctx.Err()。
//   - provider: 數據提供者
//   - channelIdx: 通道索引
//   - windowSize: 窗口大小
//   - startIdx: 起始索引（包含）
//   - endIdx: 結束索引（包含）
//
// 原本回傳純 SlidingWindowResult 不收 ctx，導致長計算無法中斷。
// 新介面回傳 error，並在外層迴圈做 ctx check。預先取消的 ctx 在第一次進入
// loop 前就會被偵測。
//
// hot-loop 從「每 4096 iter 才檢查 ctx」改成「每輪 select-default」,對
// trimmed range < 4096 row 也能立即響應 cancel,契約完整性 > +1ns/iter overhead。
func (*SlidingWindowCalculator) CalculateMaxMean(
	ctx context.Context,
	provider DataProvider,
	channelIdx, windowSize, startIdx, endIdx int,
) (SlidingWindowResult, error) {
	// Fast pre-cancel：若 caller 已取消 ctx，避免計算第一個 window。
	if err := ctx.Err(); err != nil {
		return SlidingWindowResult{}, err
	}

	bestStartIdx := startIdx

	// 計算第一個窗口的初始和
	windowSum := 0.0
	for i := startIdx; i < startIdx+windowSize; i++ {
		windowSum += provider.GetValue(i, channelIdx)
	}

	// 計算第一個窗口的平均值
	maxMean := windowSum / float64(windowSize)

	// 使用增量滑動窗口計算後續窗口
	//
	// 從「每 4096 iter 才檢查 ctx」改成每輪檢查。
	// 過去 batch check 對 trimmed range < slidingWindowCtxCheckInterval 完全不響應
	// cancel — iter 永遠 < 4096,batch 條件永遠 false。修正後每輪都 select-default
	// poll ctx.Done(),hot-loop overhead 增加 ~1ns/iter (select 無 case ready 時
	// runtime fast-path),trade-off 對短 range 提供契約完整性。
	for winStartIdx := startIdx + 1; winStartIdx <= endIdx-windowSize+1; winStartIdx++ {
		select {
		case <-ctx.Done():
			return SlidingWindowResult{}, ctx.Err()
		default:
		}

		// 移除窗口左側的值
		windowSum -= provider.GetValue(winStartIdx-1, channelIdx)

		// 添加窗口右側的新值
		rightIdx := winStartIdx + windowSize - 1
		windowSum += provider.GetValue(rightIdx, channelIdx)

		// 計算當前窗口的平均值
		currentMean := windowSum / float64(windowSize)
		if currentMean > maxMean {
			maxMean = currentMean
			bestStartIdx = winStartIdx
		}
	}

	return SlidingWindowResult{
		MaxMean:      maxMean,
		BestStartIdx: bestStartIdx,
	}, nil
}

// EMGDatasetProvider 實現 DataProvider 接口，用於 EMGDataset.
//
// expectedChannels 在構造階段從 Data[0].Channels 預先 snapshot,
// GetValue 對 ragged row (len(Channels) != expectedChannels) panic 而非靜默
// 補 0。靜默補 0 把 ragged dataset 偽裝成合法資料,sliding window 把 phantom
// 0 樣本平均進去,典型 silent miscompute。對齊 normalizer 的 ragged-row
// fail-fast 模型。
//
// 為何 panic 而非 return error:GetValue 在 DataProvider interface 已 fix,
// 簽章不能加 error 而不破壞所有 caller。Panic 含 row_index / channel_index /
// expected_channels 等 context,搭配 MaxMean.validateInput 的 ErrChannelMismatch
// early-reject (進入 sliding window 前已 fail-fast 回 error),正常路徑永遠
// 命中 error 路徑;只有跳過 validateInput 的測試 jig / 直接 struct literal
// 使用者才會 reach panic。
type EMGDatasetProvider struct {
	dataset          *models.EMGDataset
	expectedChannels int // from Data[0].Channels at construction; -1 if dataset nil/empty
}

// NewEMGDatasetProvider 創建 EMGDataset 的數據提供者.
//
// 會 eager 驗證 dataset.Data[*] 通道寬度與第一列一致;不一致時 panic
// (含 row_index / row_channels / expected_channels)。對齊 MaxMean.validateInput
// 的 ErrChannelMismatch fail-fast 策略,構造階段直接 reject 比 lazy GetValue
// fault 更早暴露問題。
//
// 對 dataset == nil / 空 Data 寬容 (返回 expectedChannels = -1):這些情況
// 由 caller (例:MaxMean.validateInput) 在更早階段以 ErrEmptyDataset 攔下;
// 此處不再重複驗證以維持 backwards compatibility (測試 jigs 仍可建構 nil provider
// 觸發 nil-pointer panic invariant)。
func NewEMGDatasetProvider(dataset *models.EMGDataset) *EMGDatasetProvider {
	if dataset == nil || len(dataset.Data) == 0 {
		return &EMGDatasetProvider{dataset: dataset, expectedChannels: -1}
	}

	expected := len(dataset.Data[0].Channels)
	for rowIdx, row := range dataset.Data {
		if len(row.Channels) != expected {
			panic(fmt.Sprintf(
				"EMGDatasetProvider: ragged row at row_index=%d, row_channels=%d, expected_channels=%d "+
					"(reject early to avoid silent zero-padding in sliding window)",
				rowIdx+1, len(row.Channels), expected,
			))
		}
	}

	return &EMGDatasetProvider{dataset: dataset, expectedChannels: expected}
}

// GetValue 獲取指定數據索引和通道索引的值.
//
// 對「dataIdx 越界」維持 return 0 行為 (避免 endIdx+windowSize-1 邊界 case
// 因 off-by-one 直接 panic);但對「row 通道數比 expected 少」(即 ragged row 被
// 跳過 NewEMGDatasetProvider 驗證的路徑)以 panic with context 取代過去的靜默
// 補 0 — silent 0 會把 phantom 樣本平均進 sliding window,典型 silent miscompute。
//
// channelIdx 越界 (>= expectedChannels) 同樣 panic — 表示 caller 用了一個
// 超出此 dataset capacity 的 channel,應該在更上游攔下。
func (p *EMGDatasetProvider) GetValue(dataIdx, channelIdx int) float64 {
	if dataIdx >= len(p.dataset.Data) {
		return 0
	}

	row := p.dataset.Data[dataIdx]

	// expectedChannels < 0 表示 provider 是用 nil/empty dataset 建構 (測試 jig);
	// 沿用 legacy 行為 — len 比對失敗時 return 0,讓既有 nil-panic 路徑不變。
	if p.expectedChannels < 0 {
		if channelIdx >= len(row.Channels) {
			return 0
		}

		return row.Channels[channelIdx]
	}

	if len(row.Channels) != p.expectedChannels {
		panic(fmt.Sprintf(
			"EMGDatasetProvider.GetValue: ragged row encountered at row_index=%d, "+
				"row_channels=%d, expected_channels=%d (channel_index=%d). "+
				"Construction-time validation should have caught this; ragged row may "+
				"have been introduced via direct struct mutation. Refusing to silent-pad with 0.",
			dataIdx+1, len(row.Channels), p.expectedChannels, channelIdx,
		))
	}

	if channelIdx >= p.expectedChannels {
		panic(fmt.Sprintf(
			"EMGDatasetProvider.GetValue: channel_index=%d out of range "+
				"(row_index=%d, expected_channels=%d). Caller must clamp channel_index "+
				"to dataset capacity before invoking GetValue.",
			channelIdx, dataIdx+1, p.expectedChannels,
		))
	}

	return row.Channels[channelIdx]
}

// Length 返回數據長度.
func (p *EMGDatasetProvider) Length() int {
	return len(p.dataset.Data)
}

// GetTime 獲取指定索引的時間值.
func (p *EMGDatasetProvider) GetTime(dataIdx int) float64 {
	if dataIdx >= len(p.dataset.Data) {
		return 0
	}

	return p.dataset.Data[dataIdx].Time
}
