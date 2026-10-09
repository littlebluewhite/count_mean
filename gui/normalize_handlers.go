package gui

import (
	"fmt"
	"path/filepath"

	"count_mean/internal/io"
)

// NormalizeData performs data normalization.
func (a *App) NormalizeData(params NormalizeParams) (result *NormalizeResult, err error) {
	defer recoverHandlerPanic("NormalizeData", a.logger, &err)

	s := a.state.Load()

	a.logger.Info("開始資料標準化", map[string]any{
		"main_file":      params.MainFile,
		"reference_file": params.ReferenceFile,
		"output_path":    params.OutputPath,
	})

	// 驗證輸入
	if params.MainFile == "" {
		return nil, ErrNoMainFile
	}

	if params.ReferenceFile == "" {
		return nil, ErrNoReferenceFile
	}

	// 讀取主要資料檔案（包含路徑驗證）
	mainRecords, err := a.readCSVWithPathValidation(s, params.MainFile, s.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取主要資料檔案失敗: %w", err)
	}

	// 讀取參考資料檔案（包含路徑驗證）
	refRecords, err := a.readCSVWithPathValidation(s, params.ReferenceFile, s.config.OperateDir)
	if err != nil {
		return nil, fmt.Errorf("讀取參考資料檔案失敗: %w", err)
	}

	// 執行標準化
	normalizedData, err := s.normalizer.NormalizeFromRawData(mainRecords, refRecords)
	if err != nil {
		return nil, fmt.Errorf("標準化計算失敗: %w", err)
	}

	// 生成輸出檔名並保存結果
	mainBaseName := TrimCSVExtension(filepath.Base(params.MainFile))
	outputName := resolveOutputName(params.OutputPath, mainBaseName, SuffixNormalized)

	outputPath, err := s.csvHandler.WriteNormalized(
		io.WriteRequest{Filename: outputName}, normalizedData,
	)
	if err != nil {
		return nil, fmt.Errorf("保存結果失敗: %w", err)
	}

	a.logger.Info("資料標準化完成", map[string]any{
		"output_file":   outputPath,
		"data_points":   len(normalizedData.Data),
		"channel_count": len(normalizedData.Headers) - 1,
	})

	return &NormalizeResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    "資料標準化成功完成",
	}, nil
}

// NormalizeParams holds parameters for data normalization.
type NormalizeParams struct {
	MainFile      string `json:"mainFile"`
	ReferenceFile string `json:"referenceFile"`
	OutputPath    string `json:"outputPath"`
}

// NormalizeResult holds the result of data normalization.
type NormalizeResult struct {
	OutputPath string `json:"outputPath"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
}
