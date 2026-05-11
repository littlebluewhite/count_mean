package calculator

import (
	"fmt"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

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
type RangeNormalizer struct {
	emgParser *parsers.EMGParser
}

// NewRangeNormalizer 建立 RangeNormalizer。
func NewRangeNormalizer() *RangeNormalizer {
	return &RangeNormalizer{
		emgParser: parsers.NewEMGParser(),
	}
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
		return nil, nil, fmt.Errorf("EMG 數據為空")
	}

	rangeResult, err := n.emgParser.GetDataInTimeRange(data, startTime, endTime)
	if err != nil {
		return nil, nil, fmt.Errorf("提取分期區間 EMG 數據失敗: %w", err)
	}

	channelMaxes, err := computeChannelMaxes(rangeResult, data.Headers)
	if err != nil {
		return nil, nil, err
	}

	return buildNormalizedDataset(data, channelMaxes), channelMaxes, nil
}

// computeChannelMaxes 計算每條肌肉在分期區間內的最大值，
// 若有任一肌肉最大值為 0 則回傳 ErrZeroChannelMax。
func computeChannelMaxes(
	rangeResult *parsers.EMGTimeRangeResult,
	headers []string,
) (map[string]float64, error) {
	channelMaxes := make(map[string]float64, len(headers))

	for _, name := range headers {
		channelData, ok := rangeResult.Data.Channels[name]
		if !ok || len(channelData) == 0 {
			return nil, &ErrZeroChannelMax{
				Channel:   name,
				StartTime: rangeResult.ActualStartTime,
				EndTime:   rangeResult.ActualEndTime,
			}
		}

		maxVal := channelData[0]
		for _, v := range channelData[1:] {
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
func buildNormalizedDataset(
	data *models.PhaseSyncEMGData,
	channelMaxes map[string]float64,
) *models.PhaseSyncEMGData {
	normalized := &models.PhaseSyncEMGData{
		Time:     make([]float64, len(data.Time)),
		Channels: make(map[string][]float64, len(data.Channels)),
		Headers:  append([]string(nil), data.Headers...),
	}
	copy(normalized.Time, data.Time)

	for name, maxVal := range channelMaxes {
		src := data.Channels[name]
		dst := make([]float64, len(src))

		for i, v := range src {
			dst[i] = v / maxVal
		}

		normalized.Channels[name] = dst
	}

	return normalized
}
