package gui

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gonum.org/v1/plot/vg"

	"count_mean/internal/chart"
	"count_mean/internal/models"
)

// Chart generation constants.
const (
	chartDefaultWidth  = 800   // 預設圖表寬度
	chartDefaultHeight = 600   // 預設圖表高度
	chartFileMode      = 0o600 // 文件權限
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
		row := records[i]

		timeVal, err := strconv.ParseFloat(row[0], 64)
		if err != nil {
			continue
		}

		channels := make([]float64, len(row)-1)

		for j := 1; j < len(row); j++ {
			if val, err := strconv.ParseFloat(row[j], 64); err == nil {
				channels[j-1] = val
			}
		}

		dataset.Data = append(dataset.Data, models.EMGData{Time: timeVal, Channels: channels})
	}

	return dataset
}

// GenerateInteractiveChart returns HTML content of an interactive chart.
func (a *App) GenerateInteractiveChart(params *InteractiveChartParams) (string, error) {
	// 讀取 CSV 檔案
	records, err := a.csvHandler.ReadCSV(params.FilePath)
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
	outputPath := filepath.Join(a.config.OutputDir, fmt.Sprintf("%s_%s.png", baseName, params.Title))

	if err := os.WriteFile(outputPath, pngData, chartFileMode); err != nil {
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
func (a *App) GenerateChart(params ChartParams) (*ChartResult, error) {
	if params.ImageData != "" {
		return a.savePNGFromBase64(params)
	}

	a.logger.Info("開始生成圖表", map[string]interface{}{
		"file_path": params.FilePath,
		"columns":   params.Columns,
		"title":     params.Title,
	})

	if params.FilePath == "" {
		return nil, ErrNoDataFile
	}

	if len(params.Columns) == 0 {
		return nil, ErrNoColumns
	}

	records, err := a.ReadCSVWithPathValidation(params.FilePath, a.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("讀取 CSV 檔案失敗: %w", err)
	}

	if len(records) < 2 {
		return nil, ErrInvalidCSVFormat
	}

	dataset := buildDatasetFromRecords(records)

	// 準備圖表配置
	chartConfig := chart.Config{
		Title:      params.Title,
		XAxisLabel: "Time (s)",
		YAxisLabel: "Value",
		Width:      vg.Length(chartDefaultWidth),
		Height:     vg.Length(chartDefaultHeight),
		Columns:    make([]string, len(params.Columns)),
	}

	for i, colIndex := range params.Columns {
		if colIndex < len(dataset.Headers) {
			chartConfig.Columns[i] = dataset.Headers[colIndex]
		}
	}

	outputPath := filepath.Join(a.config.OutputDir, fmt.Sprintf("%s_chart.png",
		strings.TrimSuffix(filepath.Base(params.FilePath), filepath.Ext(params.FilePath))))

	a.logger.Info("圖表生成完成", map[string]interface{}{
		"output_file":  outputPath,
		"column_count": len(params.Columns),
		"data_points":  len(dataset.Data),
	})

	return &ChartResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    fmt.Sprintf("圖表已成功生成並保存到: %s", outputPath),
	}, nil
}
