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

	// StartTime/EndTime 是 *float64:直接記 params 會印出指標位址,改記解參考後的值(nil = 未給)。
	rangeCtx := map[string]any{"start_time": nil, "end_time": nil}
	if params.StartTime != nil {
		rangeCtx["start_time"] = *params.StartTime
	}

	if params.EndTime != nil {
		rangeCtx["end_time"] = *params.EndTime
	}

	a.logger.Debug("計算參數", rangeCtx)

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
	records, err := s.csvHandler.ReadCSV(params.InputPath)
	if err != nil {
		return nil, fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 取得檔案名稱（不含路徑和副檔名）
	originalFileName := TrimCSVExtension(filepath.Base(params.InputPath))

	// 計算:範圍兩端是否有給由 calculator 解讀(ADR-0050)。ctx 取自 Wails lifecycle,
	// Shutdown 時取消長計算;s.maxMeanCalc 與 s.csvHandler 出自同一份 snapshot。
	run, err := s.maxMeanCalc.CalculateFromRawDataInRange(a.context(), records, params.WindowSize,
		calculator.MaxMeanRange{Start: params.StartTime, End: params.EndTime})
	if err != nil {
		return nil, fmt.Errorf("計算失敗: %w", err)
	}

	// 輸出結果
	outputFile := buildOutputFilename(originalFileName, SuffixMaxMean)

	outputPath, writeErr := s.csvHandler.WriteMaxMean(
		io.WriteRequest{Filename: outputFile},
		records[0], run.Results, run.Start, run.End,
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
// StartTime/EndTime 為原域秒;nil(前端欄位留空)= 未給,0 是合法邊界值。
type MaxMeanParams struct {
	InputPath  string   `json:"inputPath"`
	WindowSize int      `json:"windowSize"`
	StartTime  *float64 `json:"startTime,omitempty"`
	EndTime    *float64 `json:"endTime,omitempty"`
	IsBatch    bool     `json:"isBatch"`
}

// MaxMeanResult holds the result of maximum mean calculation.
type MaxMeanResult struct {
	OutputPath string `json:"outputPath"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
}
