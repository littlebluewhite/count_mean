package gui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"count_mean/internal/calculator"
	"count_mean/internal/chart"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security/fsperm"
)

// Chart generation errors.
var (
	ErrInvalidImageFormat = errors.New("無效的圖片數據格式")
	ErrNoDataFile         = errors.New("請選擇資料檔案")
	ErrNoColumns          = errors.New("請選擇至少一個欄位")
	ErrInvalidCSVFormat   = errors.New("CSV 檔案格式無效：需要至少包含標題和一行數據")
)

// buildDatasetFromRecords constructs an EMGDataset from CSV records.
// Records must have at least 2 rows (header + data). First column is time.
func buildDatasetFromRecords(records [][]string) *models.EMGDataset {
	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}
	copy(dataset.Headers, records[0])

	for i := 1; i < len(records); i++ {
		timeVal, channels, ok := parsers.ParseTimeAndChannels(records[i], 1)
		if !ok {
			continue
		}

		dataset.Data = append(dataset.Data, models.EMGData{Time: timeVal, Channels: channels})
	}

	return dataset
}

// GenerateInteractiveChart returns HTML content of an interactive chart.
//
// 走 readCSVWithPathValidation 路由（同 GetCSVHeaders），避免使用者透過 file
// dialog 餵入任意絕對路徑後 bypass 驗證。
func (a *App) GenerateInteractiveChart(params *InteractiveChartParams) (string, error) {
	s := a.state.Load()
	records, err := a.readCSVWithPathValidation(s, params.FilePath, s.config.InputDir)
	if err != nil {
		return "", fmt.Errorf("讀取 CSV 檔案失敗: %w", err)
	}

	if len(records) < 2 {
		return "", ErrInvalidCSVFormat
	}

	dataset := buildDatasetFromRecords(records)

	// 準備互動式圖表配置
	chartConfig := chart.InteractiveChartConfig{
		Title:           params.Title,
		XAxisLabel:      "Time (s)",
		YAxisLabel:      "Value",
		SelectedColumns: params.Columns,
		ColumnNames:     nil,
		ShowAllColumns:  false,
		Width:           params.Width,
		Height:          params.Height,
	}

	// 生成互動式圖表 HTML
	var buf bytes.Buffer

	err = a.chartGen.RenderChartToWriter(dataset, chartConfig, &buf)
	if err != nil {
		return "", fmt.Errorf("生成互動式圖表失敗: %w", err)
	}

	return buf.String(), nil
}

// savePNGFromBase64 從 base64 數據保存 PNG 檔案.
func (a *App) savePNGFromBase64(params ChartParams) (*ChartResult, error) {
	dataURL := params.ImageData
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		return nil, ErrInvalidImageFormat
	}

	base64Data := strings.TrimPrefix(dataURL, "data:image/png;base64,")

	pngData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, fmt.Errorf("解碼圖片數據失敗: %w", err)
	}

	fileName := filepath.Base(params.FilePath)
	baseName := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	// params.Title 來自前端，需先 sanitize 避免路徑穿越（"../x" 之類）。
	// 與 cci_handlers.go:114 的 params.Subject 處理對稱（cross-compare review 補 Wave 2 缺漏）。
	safeTitle := calculator.SanitizeFileName(params.Title)
	s := a.state.Load()
	outputPath := filepath.Join(s.config.OutputDir, fmt.Sprintf("%s_%s.png", baseName, safeTitle))

	if err := fsperm.WriteFileNoFollow(outputPath, pngData); err != nil {
		return nil, fmt.Errorf("保存圖片失敗: %w", err)
	}

	a.logger.Info("圖表下載完成", map[string]interface{}{
		"output_file": outputPath,
		"file_size":   len(pngData),
	})

	return &ChartResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    fmt.Sprintf("圖表已成功下載至: %s", outputPath),
	}, nil
}

// GenerateChart 依照目前預覽設定輸出 PNG 到 output_dir.
// 僅支援透過 params.ImageData 帶入前端 canvas 截圖的路徑；舊版的靜態 PNG
// 生成路徑（chart.Generator）已移除，因為它建出 chartConfig 後從未實際寫檔，
// 卻回傳 Success — 對前端是 silent bug。前端缺 ImageData 應改用互動圖表。
func (a *App) GenerateChart(params ChartParams) (*ChartResult, error) {
	if params.ImageData == "" {
		return nil, ErrInvalidImageFormat
	}

	return a.savePNGFromBase64(params)
}
