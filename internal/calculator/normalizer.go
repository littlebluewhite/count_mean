package calculator

import (
	"fmt"
	"math"
	"time"

	calcerrors "count_mean/internal/errors"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/util"
)

// Normalizer 處理數據標準化.
type Normalizer struct {
	scalingFactor int
	logger        *logging.Logger
	dataParser    *parsers.DataParser
}

// NewNormalizer 創建新的標準化器.
func NewNormalizer(scalingFactor int) *Normalizer {
	logger := logging.GetLogger("normalizer")

	return &Normalizer{
		scalingFactor: scalingFactor,
		logger:        logger,
		dataParser:    parsers.NewDataParserWithLogger(scalingFactor, logger),
	}
}

// Normalize 標準化數據集（每個值除以參考值）。
//
// **Reference 契約**：reference.Data 必須**恰好一行**（MVC/MAX 模式：每條肌肉
// 一個 peak）。多行 reference 過去被 Normalize 取 Data[0] 後靜默忽略其餘行，
// 導致使用者誤以為「reference 全行都被採用」—— 改為 fail-fast，
// 回傳 ErrReferenceMultipleRows。若 caller 需要逐行掃描最大值再標準化，
// 請改用 NormalizeByRangeMax（內部處理 PhaseSyncEMGData）。
func (n *Normalizer) Normalize(dataset, reference *models.EMGDataset) (*models.EMGDataset, error) {
	if n == nil {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "標準化器為空")
	}

	startTime := time.Now()

	// 使用 IsEmpty() 統一檢查 nil 和空數據
	if dataset.IsEmpty() || reference.IsEmpty() {
		err := calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "數據集或參考數據集為空")
		n.logger.Error("標準化輸入驗證失敗", err, map[string]any{
			"dataset_empty":   dataset.IsEmpty(),
			"reference_empty": reference.IsEmpty(),
		})

		return nil, err
	}

	// reference 必須恰好一行（MVC/MAX 單行契約）。多行靜默忽略歷史包袱在此終止。
	if len(reference.Data) != 1 {
		err := calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrReferenceMultipleRows,
			fmt.Sprintf("參考數據集必須恰好一行，實際為 %d 行", len(reference.Data)),
			map[string]any{
				"reference_rows": len(reference.Data),
			},
		)
		n.logger.Error("參考數據集行數違反單行契約", err, map[string]any{
			"reference_rows": len(reference.Data),
		})

		return nil, err
	}

	n.logger.Info("開始數據標準化", map[string]any{
		"dataset_points":   dataset.DataPointCount(),
		"reference_points": reference.DataPointCount(),
		"scaling_factor":   n.scalingFactor,
	})

	// 檢查通道數是否匹配
	if dataset.ChannelCount() != reference.ChannelCount() {
		err := calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrChannelMismatch,
			"數據集和參考數據集的通道數不匹配",
			map[string]any{
				"dataset_channels":   dataset.ChannelCount(),
				"reference_channels": reference.ChannelCount(),
			},
		)
		n.logger.Error("通道數不匹配", err, map[string]any{
			"dataset_channels":   dataset.ChannelCount(),
			"reference_channels": reference.ChannelCount(),
		})

		return nil, err
	}

	// Headers vs ChannelCount mismatch fail-fast。
	//
	// 規格契約:Headers[0] 為時間欄、Headers[1..] 為各通道名 — 因此
	// len(Headers)-1 必須等於 ChannelCount()。過去若 caller 構造的 EMGDataset
	// Headers 與 Data[0].Channels 寬度不一致 (header/data desync — 通常是
	// upstream parser bug 或測試 fixture 失誤),Normalize 不會擋,直接把錯誤的
	// Headers 複製到輸出,下游 CSV writer 寫出「欄位數與資料行不一致」的損壞
	// CSV,Excel/pandas 讀進來時行為各異 (truncate / pad / error)。
	//
	// 早期 assert 能讓 ragged dataset 在 boundary 就被擋下,避免污染下游。
	expectedHeaderCount := dataset.ChannelCount() + 1
	if len(dataset.Headers) != expectedHeaderCount {
		err := calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrChannelMismatch,
			fmt.Sprintf(
				"數據集 Headers 寬度 %d 與通道數 %d 不一致(期望 Headers 寬度 = 通道數 + 1)",
				len(dataset.Headers), dataset.ChannelCount(),
			),
			map[string]any{
				"headers_len":      len(dataset.Headers),
				"channel_count":    dataset.ChannelCount(),
				"expected_headers": expectedHeaderCount,
			},
		)
		n.logger.Error("Headers/ChannelCount 寬度不一致", err, map[string]any{
			"headers_len":      len(dataset.Headers),
			"channel_count":    dataset.ChannelCount(),
			"expected_headers": expectedHeaderCount,
		})

		return nil, err
	}

	// dataset.ChannelCount() 只取 Data[0] 寬度,row 1..N 沒被檢查;
	// 過去 hot loop 對 wider row 會 `refValues[j]` index-out-of-range panic,
	// narrower row 則 silent emit ragged dataset 污染下游。改為在進 loop
	// 之前一次驗證所有 row,讓 fast path 100% 命中、把 fallback alloc 拿掉。
	expectedChannelCount := reference.ChannelCount()
	for i, row := range dataset.Data {
		if len(row.Channels) != expectedChannelCount {
			err := calcerrors.NewCalculatorErrorWithContext(
				calcerrors.ErrChannelMismatch,
				fmt.Sprintf(
					"數據集第 %d 行通道數 %d 與參考數據集通道數 %d 不匹配",
					i+1, len(row.Channels), expectedChannelCount,
				),
				map[string]any{
					"row_index":          i + 1,
					"row_channels":       len(row.Channels),
					"reference_channels": expectedChannelCount,
				},
			)
			n.logger.Error("數據行通道數不匹配", err, map[string]any{
				"row_index":          i + 1,
				"row_channels":       len(row.Channels),
				"reference_channels": expectedChannelCount,
			})

			return nil, err
		}
	}

	result := &models.EMGDataset{
		Headers:               make([]string, len(dataset.Headers)),
		Data:                  make([]models.EMGData, 0, dataset.DataPointCount()),
		OriginalTimePrecision: dataset.OriginalTimePrecision,
	}

	// 複製標題
	copy(result.Headers, dataset.Headers)

	// 獲取參考值（使用第一行數據）
	refValues := reference.Data[0].Channels
	n.logger.Debug("使用參考值", map[string]any{
		"reference_values": refValues,
	})

	if err := n.validateReferenceValues(refValues); err != nil {
		return nil, err
	}

	// 預配 totalPoints*channelCount 大小的 buffer，逐 row 切 sub-slice：
	// 30 萬點 × 16 channel 級別下，per-row make 會產生 30 萬次 allocation；
	// 改用三索引切片 buf[i:j:j] 限制 cap，未來若有 append(channels, ...) 也只會新配置不會寫穿。
	// 下游消費者 (csv writer / convertNormalizedDataToArray / chart) 全部僅讀，sub-slice 安全。
	// 所有 row 已在上方統一驗證寬度,fast path 100% 命中,不再需要 fallback alloc。
	totalPoints := len(dataset.Data)
	channelCount := len(refValues)

	var buffer []float64
	if totalPoints > 0 && channelCount > 0 {
		buffer = make([]float64, totalPoints*channelCount)
	}

	for i, data := range dataset.Data {
		start := i * channelCount
		end := start + channelCount
		normalizedChannels := buffer[start:end:end]

		for j, val := range data.Channels {
			normalizedChannels[j] = val / refValues[j]
		}

		normalizedData := models.EMGData{
			Time:     data.Time,
			Channels: normalizedChannels,
		}

		result.Data = append(result.Data, normalizedData)
	}

	duration := time.Since(startTime)
	n.logger.Info("數據標準化完成", map[string]any{
		"duration_ms":      duration.Milliseconds(),
		"processed_points": result.DataPointCount(),
		"channel_count":    result.ChannelCount(),
	})

	return result, nil
}

// NormalizeFromRawData 從原始字符串數據進行標準化.
func (n *Normalizer) NormalizeFromRawData(records, reference [][]string) (*models.EMGDataset, error) {
	if n == nil {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrEmptyDataset, "標準化器為空")
	}

	n.logger.Info("開始從原始數據進行標準化", map[string]any{
		"main_records":      len(records),
		"reference_records": len(reference),
	})

	dataset, err := n.dataParser.ParseRawDataWithOptions(records, parsers.ParseOptions{
		DetectTimePrecision: true,
		LogVerbose:          true,
	})
	if err != nil {
		n.logger.Error("主數據解析失敗", err)
		return nil, fmt.Errorf("解析主數據失敗: %w", err)
	}

	refDataset, err := n.parseReferenceData(reference)
	if err != nil {
		n.logger.Error("參考數據解析失敗", err)
		return nil, fmt.Errorf("解析參考數據失敗: %w", err)
	}

	return n.Normalize(dataset, refDataset)
}

// parseReferenceData 解析參考數據（特殊格式，第一列是標籤而非時間）.
func (n *Normalizer) parseReferenceData(records [][]string) (*models.EMGDataset, error) {
	n.logger.Debug("開始解析參考數據", map[string]any{
		"record_count": len(records),
	})

	if len(records) < 2 {
		err := calcerrors.NewCalculatorErrorWithContext(
			calcerrors.ErrInvalidReferenceData,
			"參考數據至少需要包含標題行和一行數據",
			map[string]any{
				"record_count": len(records),
			},
		)
		n.logger.Error("參考數據結構驗證失敗", err, map[string]any{
			"record_count": len(records),
		})

		return nil, err
	}

	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}

	// 複製標題
	copy(dataset.Headers, records[0])

	// 解析數據行（跳過標題）
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue // 跳過無效行
		}

		// 參考數據不需要時間值，使用索引作為時間
		data := models.EMGData{
			Time:     float64(i - 1), // 使用行索引作為時間
			Channels: make([]float64, 0, len(row)-1),
		}

		// 解析通道數據（從第二列開始，跳過標籤列）
		for j := 1; j < len(row); j++ {
			val, err := util.Str2Number[float64, int](row[j], n.scalingFactor)
			if err != nil {
				n.logger.Error("參考數據通道值解析失敗", err, map[string]any{
					"row_number":    i + 1,
					"column_number": j + 1,
					"value":         row[j],
				})

				return nil, fmt.Errorf("解析參考數據失敗在行 %d 列 %d: %w", i+1, j+1, err)
			}

			data.Channels = append(data.Channels, val)
		}

		dataset.Data = append(dataset.Data, data)
	}

	// 檢查是否有有效數據
	if dataset.IsEmpty() {
		return nil, calcerrors.NewCalculatorError(calcerrors.ErrInvalidReferenceData, "參考數據解析後為空")
	}

	n.logger.Debug("參考數據解析完成", map[string]any{
		"parsed_records": dataset.DataPointCount(),
		"channel_count":  dataset.ChannelCount(),
		"header_count":   len(dataset.Headers),
	})

	return dataset, nil
}

// validateReferenceValues 對參考值進行 fail-fast 檢查（P1-A2-1 / P0-4）：
//   - NaN：val / NaN = NaN，整批 normalize 結果污染為 NaN → ErrNaNReference。
//   - ±Inf：val / Inf = 0 或 ±Inf，數值都不可用 → ErrInfReference。
//   - 0：division by zero → ErrZeroReference。
//
// 三類錯誤過去都丟同一個 ErrZeroReference,callers 無法 programmatically
// 分辨「資料品質問題 retry/clean」vs「真實 0 MVC refuse run」;改為三路 dispatch
// 對齊 range_normalizer.ErrChannelMaxNaN / ErrChannelMaxInf 已有的拆分模式。
// 錯誤訊息保留通道索引（1-based）方便使用者排查。
func (n *Normalizer) validateReferenceValues(refValues []float64) error {
	for i, refVal := range refValues {
		var (
			message  string
			label    string
			sentinel error
		)

		switch {
		case math.IsNaN(refVal):
			message = fmt.Sprintf("參考值在通道 %d 為 NaN，無法進行標準化", i+1)
			label = "NaN"
			sentinel = calcerrors.ErrNaNReference
		case math.IsInf(refVal, 0):
			message = fmt.Sprintf("參考值在通道 %d 為 Inf，無法進行標準化", i+1)
			label = "Inf"
			sentinel = calcerrors.ErrInfReference
		case refVal == 0:
			message = fmt.Sprintf("參考值在通道 %d 為零，無法進行標準化", i+1)
			label = "Zero"
			sentinel = calcerrors.ErrZeroReference
		default:
			continue
		}

		err := calcerrors.NewCalculatorErrorWithContext(
			sentinel,
			message,
			map[string]any{
				"channel_index":   i + 1,
				"reference_value": label,
			},
		)
		n.logger.Error("參考值無效", err, map[string]any{
			"channel_index":   i + 1,
			"reference_value": label,
		})

		return err
	}

	return nil
}
