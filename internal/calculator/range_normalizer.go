package calculator

import (
	"errors"
	"fmt"
	"math"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// ErrEmptyEMGData 表示 NormalizeByRangeMax 收到 nil PhaseSyncEMGData。
var ErrEmptyEMGData = errors.New("EMG 數據為空")

// ErrMissingChannelMax 表示 buildNormalizedDataset 在 channelMaxes 找不到 header
// 對應的最大值（過去靜默除以零值產生 NaN/Inf，改為 fail-fast）。
var ErrMissingChannelMax = errors.New("channelMaxes 缺少通道最大值")

// ErrMissingChannelData 表示 buildNormalizedDataset 在 data.Channels 找不到 header
// 對應的資料切片（過去靜默產出長度 0 的 normalized channel）。
var ErrMissingChannelData = errors.New("data.Channels 缺少通道資料")

// ErrChannelMaxNaN 表示某條通道最大值為 NaN，無法用於標準化。
var ErrChannelMaxNaN = errors.New("通道最大值為 NaN")

// ErrChannelMaxInf 表示某條通道最大值為 ±Inf，無法用於標準化。
var ErrChannelMaxInf = errors.New("通道最大值為 Inf")

// ErrMissingChannelInRange 表示「分期區間切出來的 EMG slice 缺某條 header 對應的通道」 —
// 與 ErrZeroChannelMax 拆開以避免一個錯誤訊息要同時解釋兩種完全不同的根因:
//   - ErrZeroChannelMax: 通道存在,區間內所有 sample 都是 0 (例:設備故障 / 量測未啟動)
//   - ErrMissingChannelInRange: 通道根本不在 PhaseSyncEMGData.Channels (例:header 與 channel
//     map 不同步、parser bug、user 提供的時間區間切出空 slice)
//
// 舊版兩種狀況都丟同一個 ErrZeroChannelMax,前端看到「最大值為 0」會誤導使用者
// 「動作做了但量不到肌電」,實際根因是 channel mapping 出問題。拆開讓前端能各自顯示
// 「資料缺失」vs「實測為 0」兩種訊息,debug 速度直接 ×10。
type ErrMissingChannelInRange struct {
	Channel   string
	StartTime float64
	EndTime   float64
}

// Error 實作 error 介面。
func (e *ErrMissingChannelInRange) Error() string {
	return fmt.Sprintf(
		"通道 %q 在分期區間 [%.6f, %.6f] 內缺少資料(channel 未提供或區間切出空 slice)",
		e.Channel, e.StartTime, e.EndTime,
	)
}

// ErrZeroChannelMax 表示某條肌肉在指定分期區間內最大值為零，
// 無法用於標準化（會導致除以零）。
type ErrZeroChannelMax struct {
	Channel   string
	StartTime float64
	EndTime   float64
}

// Error 實作 error 介面。錯誤訊息包含問題肌肉名稱與分期區間，
// 方便前端顯示給使用者，協助調整分期點重試。
func (e *ErrZeroChannelMax) Error() string {
	return fmt.Sprintf(
		"肌肉 %q 在分期區間 [%.6f, %.6f] 內最大值為 0，無法標準化",
		e.Channel, e.StartTime, e.EndTime,
	)
}

// RangeNormalizer 在指定時間區間內以每條肌肉的最大值作為除數，
// 對整段 EMG 資料做標準化（區間內最大值會變成 1.0）。
// 這是一種類似 MVC peak normalization 的縮放方式。
type RangeNormalizer struct{}

// NewRangeNormalizer 建立 RangeNormalizer。
func NewRangeNormalizer() *RangeNormalizer {
	return &RangeNormalizer{}
}

// NormalizeByRangeMax 對輸入 EMG 資料的每條肌肉，在 [startTime, endTime]
// 內找最大值，然後將該肌肉的全部資料（含區間外）除以該最大值。
//
// 回傳：
//   - 標準化後的 PhaseSyncEMGData（新物件，不修改輸入）
//   - map of channel name -> 標準化前的最大值（供前端顯示透明度資訊）
//   - error：若任一肌肉在區間內最大值為 0，回傳 *ErrZeroChannelMax
func (n *RangeNormalizer) NormalizeByRangeMax(
	data *models.PhaseSyncEMGData,
	startTime, endTime float64,
) (*models.PhaseSyncEMGData, map[string]float64, error) {
	if data == nil {
		return nil, nil, ErrEmptyEMGData
	}

	rangeResult, err := parsers.GetEMGDataInTimeRange(data, startTime, endTime)
	if err != nil {
		return nil, nil, fmt.Errorf("提取分期區間 EMG 數據失敗: %w", err)
	}

	channelMaxes, err := computeChannelMaxes(rangeResult, data.Headers)
	if err != nil {
		return nil, nil, err
	}

	normalized, err := buildNormalizedDataset(data, channelMaxes)
	if err != nil {
		return nil, nil, err
	}

	return normalized, channelMaxes, nil
}

// computeChannelMaxes 計算每條肌肉在分期區間內的最大值，
// 若有任一肌肉最大值為 0 則回傳 ErrZeroChannelMax。
//
// util.ArrayMax 用 `>` 比較對 NaN 不安全 —— NaN 在中間/尾端會被
// silent skip，最大值落在 non-NaN 元素上；下游 buildNormalizedDataset 偵測
// 不到 NaN，NaN sample 會以「除以 non-NaN max」流入 normalized output。
// 修法：在 channelData 單次掃描中同時做 NaN/Inf fail-fast 與 max 累積。
// 不再呼叫 util.ArrayMax (二次掃描) — 對 30 萬點 × 16 channel 級別資料,
// 從 2 walk 減為 1 walk 減半 memory bandwidth 與 cache miss。
//
// single-pass 重構。過去 NaN/Inf scan 跑完才呼叫 ArrayMax 做第二輪
// 掃描(O(2N))。NaN/Inf 是 hot path 必過的 fail-fast、max 是 hot path 必算
// 的結果,合併進同一個 for-range 即可省一輪 cache fill。對 csv parser 餵
// 進來的 100K+ 點 channelData,合併後實測 ~5% 改善(主要省 L2 cache miss)。
//
// parsers.ParseFloatCell 對 "NaN"/"Inf" 字面是刻意接受的(見 parse_helpers.go
// 註解),因此這個 scan 是計算端的最後一道防線。
func computeChannelMaxes(
	rangeResult *parsers.EMGTimeRangeResult,
	headers []string,
) (map[string]float64, error) {
	channelMaxes := make(map[string]float64, len(headers))

	for _, name := range headers {
		channelData, ok := rangeResult.Data.Channels[name]
		if !ok || len(channelData) == 0 {
			// 拆出 ErrMissingChannelInRange — 「通道缺鍵 / 區間切出空 slice」
			// 與「通道存在但實測全為 0」是兩種完全不同的根因,前端訊息要分流。
			return nil, &ErrMissingChannelInRange{
				Channel:   name,
				StartTime: rangeResult.ActualStartTime,
				EndTime:   rangeResult.ActualEndTime,
			}
		}

		// 單次掃描同時做 NaN/Inf fail-fast 與 max 累積。
		// 用 channelData[0] 作為初始 max — 已由上方 len(channelData) == 0 守住,
		// 此處取 [0] 不會越界。
		maxVal := channelData[0]
		for _, v := range channelData {
			switch {
			case math.IsNaN(v):
				return nil, fmt.Errorf("%w: %q", ErrChannelMaxNaN, name)
			case math.IsInf(v, 0):
				return nil, fmt.Errorf("%w: %q", ErrChannelMaxInf, name)
			}
			if v > maxVal {
				maxVal = v
			}
		}

		if maxVal == 0 {
			return nil, &ErrZeroChannelMax{
				Channel:   name,
				StartTime: rangeResult.ActualStartTime,
				EndTime:   rangeResult.ActualEndTime,
			}
		}

		channelMaxes[name] = maxVal
	}

	return channelMaxes, nil
}

// buildNormalizedDataset 以 channelMaxes 對 data 做標準化，產生新物件。
// 時間序列與標頭直接複製；通道值逐項除以該通道的最大值。
//
// 
//   - 以 data.Headers 為迭代主軸（保留順序、避免 map 迭代隨機性）。
//   - channelMaxes 缺鍵時回明確錯誤（過去靜默用零值除法產生 NaN/Inf）。
//   - data.Channels 缺鍵時同樣回錯（過去產生長度 0 的 normalized channel）。
//   - maxVal 為 NaN/Inf 時 fail-fast，避免下游全資料污染為 NaN/Inf。
func buildNormalizedDataset(
	data *models.PhaseSyncEMGData,
	channelMaxes map[string]float64,
) (*models.PhaseSyncEMGData, error) {
	normalized := &models.PhaseSyncEMGData{
		Time:     make([]float64, len(data.Time)),
		Channels: make(map[string][]float64, len(data.Channels)),
		Headers:  append([]string(nil), data.Headers...),
	}
	copy(normalized.Time, data.Time)

	for _, name := range data.Headers {
		maxVal, ok := channelMaxes[name]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrMissingChannelMax, name)
		}

		if math.IsNaN(maxVal) {
			return nil, fmt.Errorf("%w: %q", ErrChannelMaxNaN, name)
		}

		if math.IsInf(maxVal, 0) {
			return nil, fmt.Errorf("%w: %q", ErrChannelMaxInf, name)
		}

		src, ok := data.Channels[name]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrMissingChannelData, name)
		}

		dst := make([]float64, len(src))
		for i, v := range src {
			dst[i] = v / maxVal
		}

		normalized.Channels[name] = dst
	}

	return normalized, nil
}
