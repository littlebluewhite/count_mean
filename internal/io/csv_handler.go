package io

import (
	"bufio"
	"count_mean/internal/config"
	"count_mean/internal/models"
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// CSVHandler 處理 CSV 檔案讀寫
type CSVHandler struct {
	config *config.AppConfig
}

// NewCSVHandler 創建新的 CSV 處理器
func NewCSVHandler(config *config.AppConfig) *CSVHandler {
	return &CSVHandler{
		config: config,
	}
}

// BOMBytes UTF-8 BOM
var BOMBytes = []byte{0xEF, 0xBB, 0xBF}

// ListInputFiles 列出輸入目錄中的CSV文件
func (h *CSVHandler) ListInputFiles() ([]string, error) {
	files, err := os.ReadDir(h.config.InputDir)
	if err != nil {
		return nil, fmt.Errorf("無法讀取輸入目錄 %s: %w", h.config.InputDir, err)
	}

	var csvFiles []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".csv") {
			csvFiles = append(csvFiles, file.Name())
		}
	}

	return csvFiles, nil
}

// ReadCSVFromPrompt 從使用者輸入讀取 CSV 檔案
func (h *CSVHandler) ReadCSVFromPrompt(prompt string) ([][]string, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	fileName, _ := reader.ReadString('\n')
	fileName = strings.TrimSpace(fileName)

	// 如果沒有副檔名，添加.csv
	if !strings.HasSuffix(fileName, ".csv") {
		fileName += ".csv"
	}

	// 建構完整路徑
	fullPath := filepath.Join(h.config.InputDir, fileName)

	return h.ReadCSV(fullPath)
}

// ReadCSV 讀取 CSV 檔案
func (h *CSVHandler) ReadCSV(filename string) ([][]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("無法開啟檔案 %s: %w", filename, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Error closing file %s: %v\n", file.Name(), err)
		}
	}()

	r := csv.NewReader(file)
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("無法讀取 CSV 資料從 %s: %w", filename, err)
	}

	// 驗證數據
	if len(records) < 2 {
		return nil, fmt.Errorf("檔案 %s 至少需要包含標題行和一行數據", filename)
	}

	return records, nil
}

// WriteCSVToOutput 寫入CSV文件到輸出目錄
func (h *CSVHandler) WriteCSVToOutput(filename string, data [][]string) error {
	// 確保輸出目錄存在
	if err := os.MkdirAll(h.config.OutputDir, 0755); err != nil {
		return fmt.Errorf("無法創建輸出目錄: %w", err)
	}

	// 建構完整輸出路徑
	fullPath := filepath.Join(h.config.OutputDir, filename)
	return h.WriteCSV(fullPath, data)
}

// WriteCSV 寫入 CSV 檔案
func (h *CSVHandler) WriteCSV(filename string, data [][]string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("無法建立檔案 %s: %w", filename, err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Error closing file %s: %v\n", file.Name(), err)
		}
	}()

	// 寫入 BOM（如果啟用）
	if h.config.BOMEnabled {
		if _, err := file.Write(BOMBytes); err != nil {
			return fmt.Errorf("無法寫入 BOM 到 %s: %w", filename, err)
		}
	}

	w := csv.NewWriter(file)
	if err := w.WriteAll(data); err != nil {
		return fmt.Errorf("無法寫入資料到 %s: %w", filename, err)
	}

	return nil
}

// ConvertMaxMeanResultsToCSV 將最大平均值結果轉換為 CSV 格式
func (h *CSVHandler) ConvertMaxMeanResultsToCSV(headers []string, results []models.MaxMeanResult) [][]string {
	data := make([][]string, 0, 4)

	// 添加標題
	data = append(data, headers)

	// 創建結果行
	startTimes := make([]string, 1, len(headers))
	endTimes := make([]string, 1, len(headers))
	maxMeans := make([]string, 1, len(headers))

	startTimes[0] = "開始秒數"
	endTimes[0] = "結束秒數"
	maxMeans[0] = "最大平均值"

	// 填充結果
	for _, result := range results {
		precision := fmt.Sprintf("%%.%df", h.config.Precision)

		startTimes = append(startTimes, fmt.Sprintf("%.2f", result.StartTime))
		endTimes = append(endTimes, fmt.Sprintf("%.2f", result.EndTime))
		maxMeans = append(maxMeans, fmt.Sprintf(precision, result.MaxMean/math.Pow10(h.config.ScalingFactor)))
	}

	data = append(data, startTimes)
	data = append(data, endTimes)
	data = append(data, maxMeans)

	return data
}

// ConvertNormalizedDataToCSV 將標準化數據轉換為 CSV 格式
func (h *CSVHandler) ConvertNormalizedDataToCSV(dataset *models.EMGDataset) [][]string {
	data := make([][]string, 0, len(dataset.Data)+1)

	// 添加標題
	data = append(data, dataset.Headers)

	// 添加數據
	precision := fmt.Sprintf("%%.%df", h.config.Precision)
	for _, emgData := range dataset.Data {
		row := make([]string, 0, len(dataset.Headers))

		// 時間列
		row = append(row, fmt.Sprintf("%.2f", emgData.Time))

		// 數據列
		for _, val := range emgData.Channels {
			row = append(row, fmt.Sprintf(precision, val))
		}

		data = append(data, row)
	}

	return data
}

// ConvertPhaseAnalysisToCSV 將階段分析結果轉換為 CSV 格式
func (h *CSVHandler) ConvertPhaseAnalysisToCSV(headers []string, result *models.PhaseAnalysisResult, maxTimeIndex map[int]float64) [][]string {
	data := make([][]string, 0, len(result.MaxValues)+len(result.MeanValues)+2)

	// 添加標題
	data = append(data, headers)

	precision := fmt.Sprintf("%%.%df", h.config.Precision)
	scalingFactor := float64(h.config.ScalingFactor)

	// 最大值行
	for i, phaseResult := range []models.PhaseAnalysisResult{*result} {
		maxRow := make([]string, 1, len(headers))
		maxRow[0] = phaseResult.PhaseName + " 最大值"

		for j := 1; j < len(headers); j++ {
			channelIdx := j - 1
			if maxVal, exists := phaseResult.MaxValues[channelIdx]; exists {
				maxRow = append(maxRow, fmt.Sprintf(precision, maxVal/math.Pow10(int(scalingFactor))))
			} else {
				maxRow = append(maxRow, "N/A")
			}
		}
		data = append(data, maxRow)

		// 平均值行
		meanRow := make([]string, 1, len(headers))
		meanRow[0] = phaseResult.PhaseName + " 平均值"

		for j := 1; j < len(headers); j++ {
			channelIdx := j - 1
			if meanVal, exists := phaseResult.MeanValues[channelIdx]; exists {
				meanRow = append(meanRow, fmt.Sprintf(precision, meanVal/math.Pow10(int(scalingFactor))))
			} else {
				meanRow = append(meanRow, "N/A")
			}
		}
		data = append(data, meanRow)

		// 只處理第一個結果（這個函數設計為處理單個階段）
		if i == 0 {
			break
		}
	}

	// 最大值時間行
	if len(maxTimeIndex) > 0 {
		timeRow := make([]string, 1, len(headers))
		timeRow[0] = "整個階段最大值出現在_秒"

		for j := 1; j < len(headers); j++ {
			channelIdx := j - 1
			if timeVal, exists := maxTimeIndex[channelIdx]; exists {
				timeRow = append(timeRow, fmt.Sprintf("%.2f", timeVal))
			} else {
				timeRow = append(timeRow, "N/A")
			}
		}
		data = append(data, timeRow)
	}

	return data
}
