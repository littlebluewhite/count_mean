package gui

import (
	"fmt"
	"path/filepath"

	"count_mean/internal/calculator"
	"count_mean/internal/io"
)

// CalculateMaxMean calculates maximum mean values.
func (a *App) CalculateMaxMean(params MaxMeanParams) (result *MaxMeanResult, err error) {
	defer recoverHandlerPanic("CalculateMaxMean", a.logger, &err)

	a.logger.Info("開始最大平均值計算", map[string]any{
		"input_path":  params.InputPath,
		"window_size": params.WindowSize,
		"is_batch":    params.IsBatch,
	})

	a.logger.Debug("計算參數", map[string]any{"params": params})

	// 批次處理模式
	if params.IsBatch {
		return a.calculateMaxMeanBatch(params)
	}

	// 單檔案處理模式
	return a.calculateMaxMeanSingle(params)
}

// calculateMaxMeanSingle 處理單個檔案.
//
// snapshot pattern: entry 在 line 開頭一次性取得 *appState,後續所有 helper 透過
// 顯式參數共享同一份 snapshot,杜絕 SaveConfig 在分析過程中換 cfg 造成的撕裂。
func (a *App) calculateMaxMeanSingle(params MaxMeanParams) (*MaxMeanResult, error) {
	s := a.state.Load()

	// 使用統一的 CSV 讀取方法（包含路徑驗證）
	records, err := a.readCSVWithPathValidation(s, params.InputPath, s.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 取得檔案名稱（不含路徑和副檔名）
	originalFileName := TrimCSVExtension(filepath.Base(params.InputPath))

	// 解析時間範圍並計算
	startRange, endRange := calculator.ResolveTimeRange(records, params.StartTime, params.EndTime)

	results, err := a.calculateWithTimeRange(a.context(), s, records, params.WindowSize, startRange, endRange)
	if err != nil {
		return nil, fmt.Errorf("計算失敗: %w", err)
	}

	// 輸出結果
	outputFile := buildOutputFilename(originalFileName, SuffixMaxMean)

	outputPath, writeErr := s.csvHandler.WriteMaxMean(
		io.WriteRequest{Filename: outputFile},
		records[0], results, startRange, endRange,
	)
	if writeErr != nil {
		return nil, fmt.Errorf("寫入輸出檔案失敗: %w", writeErr)
	}

	// 準備回傳結果。Success/Message 必須顯式設定 — 前端依 result.success 判定成敗,
	// 漏設會讓 bool 零值 false 使成功計算被誤判為失敗(對齊批次 RunBatch 與
	// NormalizeData 的 envelope)。
	return &MaxMeanResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    fmt.Sprintf("最大平均值計算成功完成，結果已保存到: %s", filepath.Base(outputPath)),
	}, nil
}

// MaxMeanParams holds parameters for maximum mean calculation.
type MaxMeanParams struct {
	InputPath  string  `json:"inputPath"`
	WindowSize int     `json:"windowSize"`
	StartTime  float64 `json:"startTime"`
	EndTime    float64 `json:"endTime"`
	IsBatch    bool    `json:"isBatch"`
}

// MaxMeanResult holds the result of maximum mean calculation.
type MaxMeanResult struct {
	OutputPath string `json:"outputPath"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
}
