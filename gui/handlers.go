package gui

import (
	"count_mean/internal/calculator"
	"count_mean/internal/chart"
	"count_mean/internal/config"
	"count_mean/internal/errors"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"fmt"
	"fyne.io/fyne/v2/widget"
	"gonum.org/v1/plot/vg"
	"path/filepath"
	"strconv"
	"strings"
)

// executeMaxMeanCalculation 執行最大平均值計算
func (a *App) executeMaxMeanCalculation(mode, filePath, dirPath, windowSizeStr, startRangeStr, endRangeStr string) {
	logger := a.logger.WithContext("operation", "max_mean_calculation")
	logger.Info("開始執行最大平均值計算", map[string]interface{}{
		"mode":            mode,
		"file_path":       filePath,
		"dir_path":        dirPath,
		"window_size_str": windowSizeStr,
		"start_range_str": startRangeStr,
		"end_range_str":   endRangeStr,
	})

	a.updateStatus("執行計算中...")

	// 驗證窗口大小
	windowSize, err := a.validator.ValidateWindowSize(windowSizeStr)
	if err != nil {
		a.handleValidationError("窗口大小驗證失敗", err, logger)
		return
	}

	// 驗證時間範圍
	startRange, endRange, useCustomRange, err := a.validator.ValidateTimeRange(startRangeStr, endRangeStr)
	if err != nil {
		a.handleValidationError("時間範圍驗證失敗", err, logger)
		return
	}

	if mode == "處理單一檔案" {
		if filePath == "" {
			err := errors.NewValidationError("file_path", filePath, "請選擇要處理的CSV檔案")
			a.handleValidationError("檔案路徑驗證失敗", err, logger)
			return
		}

		// 驗證檔案名稱
		filename := filepath.Base(filePath)
		if validateErr := a.validator.ValidateFilename(filename); validateErr != nil {
			a.handleValidationError("檔案名稱驗證失敗", validateErr, logger)
			return
		}

		err = a.executeSingleFileCalculation(filePath, windowSize, startRange, endRange, useCustomRange)
	} else {
		if dirPath == "" {
			err := errors.NewValidationError("dir_path", dirPath, "請選擇要處理的資料夾")
			a.handleValidationError("資料夾路徑驗證失敗", err, logger)
			return
		}

		// 驗證目錄路徑
		if validateErr := a.validator.ValidateDirectoryPath(dirPath); validateErr != nil {
			a.handleValidationError("目錄路徑驗證失敗", validateErr, logger)
			return
		}

		err = a.executeBatchCalculation(dirPath, windowSize, startRange, endRange, useCustomRange)
	}

	if err != nil {
		a.showError(fmt.Sprintf("計算失敗: %v", err))
	} else {
		a.updateStatus("計算完成！")
		a.showInfo("計算成功完成，結果已保存到輸出目錄")
	}
}

// executeSingleFileCalculation 執行單檔案計算
func (a *App) executeSingleFileCalculation(filePath string, windowSize int, startRange, endRange float64, useCustomRange bool) error {
	// 檢查檔案是否在允許的目錄範圍內，決定使用哪種讀取方法
	var records [][]string
	var err error

	// 檢查檔案是否為絕對路徑且在輸入目錄外
	if filepath.IsAbs(filePath) {
		fileDir := filepath.Dir(filePath)
		relPath, relErr := filepath.Rel(a.config.InputDir, fileDir)
		if relErr != nil || strings.HasPrefix(relPath, "..") {
			// 檔案在輸入目錄外，使用外部讀取方法（跳過路徑驗證）
			records, err = a.csvHandler.ReadCSVExternal(filePath)
		} else {
			// 檔案在輸入目錄內，使用正常讀取方法
			records, err = a.csvHandler.ReadCSV(filePath)
		}
	} else {
		// 相對路徑，使用正常讀取方法
		records, err = a.csvHandler.ReadCSV(filePath)
	}

	if err != nil {
		return fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 取得檔案名稱（不含路徑和副檔名）
	fileName := filepath.Base(filePath)
	originalFileName := strings.TrimSuffix(fileName, ".csv")

	// 如果沒有使用自定義範圍，從數據中獲取預設範圍
	if !useCustomRange {
		if len(records) > 1 && len(records[1]) > 0 {
			startRange, _ = strconv.ParseFloat(records[1][0], 64)
		}
		if len(records) > 1 && len(records[len(records)-1]) > 0 {
			endRange, _ = strconv.ParseFloat(records[len(records)-1][0], 64)
		}
	}

	// 計算最大平均值
	var results []models.MaxMeanResult
	if startRange == 0 && endRange == 0 {
		results, err = a.maxMeanCalc.CalculateFromRawData(records, windowSize)
	} else {
		results, err = a.maxMeanCalc.CalculateFromRawDataWithRange(records, windowSize, startRange, endRange)
	}

	if err != nil {
		return fmt.Errorf("計算失敗: %w", err)
	}

	// 輸出結果
	outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, startRange, endRange)
	outputFile := fmt.Sprintf("%s_最大平均值計算.csv", originalFileName)

	return a.csvHandler.WriteCSVToOutput(outputFile, outputData)
}

// executeBatchCalculation 執行批量計算
func (a *App) executeBatchCalculation(dirPath string, windowSize int, startRange, endRange float64, useCustomRange bool) error {
	// 取得資料夾名稱（相對於輸入目錄）
	var dirName string
	var csvFiles []string
	var err error

	// 檢查是否為輸入目錄的子目錄
	if filepath.IsAbs(dirPath) {
		// 如果是絕對路徑，檢查是否在輸入目錄下
		relPath, err := filepath.Rel(a.config.InputDir, dirPath)
		if err != nil || strings.HasPrefix(relPath, "..") {
			// 不在輸入目錄下，直接使用目錄中的文件
			files, err := filepath.Glob(filepath.Join(dirPath, "*.csv"))
			if err != nil {
				return fmt.Errorf("搜尋CSV文件失敗: %w", err)
			}
			if len(files) == 0 {
				return fmt.Errorf("資料夾中沒有找到CSV文件")
			}
			// 對於外部目錄，直接處理文件
			return a.executeBatchCalculationDirect(files, filepath.Base(dirPath), windowSize, startRange, endRange, useCustomRange)
		}
		dirName = relPath
	} else {
		dirName = dirPath
	}

	// 使用CSV處理器的目錄方法列出文件
	csvFiles, err = a.csvHandler.ListCSVFilesInDirectory(dirName)
	if err != nil {
		return fmt.Errorf("列出CSV文件失敗: %w", err)
	}

	if len(csvFiles) == 0 {
		return fmt.Errorf("資料夾中沒有找到CSV文件")
	}

	// 處理每個文件
	for _, fileName := range csvFiles {
		fileBaseName := strings.TrimSuffix(fileName, ".csv")

		records, err := a.csvHandler.ReadCSVFromDirectory(dirName, fileName)
		if err != nil {
			continue // 跳過有錯誤的文件
		}

		// 如果沒有使用自定義範圍，從數據中獲取預設範圍
		actualStartRange, actualEndRange := startRange, endRange
		if !useCustomRange {
			if len(records) > 1 && len(records[1]) > 0 {
				actualStartRange, _ = strconv.ParseFloat(records[1][0], 64)
			}
			if len(records) > 1 && len(records[len(records)-1]) > 0 {
				actualEndRange, _ = strconv.ParseFloat(records[len(records)-1][0], 64)
			}
		}

		// 計算最大平均值
		var results []models.MaxMeanResult
		if actualStartRange == 0 && actualEndRange == 0 {
			results, err = a.maxMeanCalc.CalculateFromRawData(records, windowSize)
		} else {
			results, err = a.maxMeanCalc.CalculateFromRawDataWithRange(records, windowSize, actualStartRange, actualEndRange)
		}

		if err != nil {
			continue // 跳過計算失敗的文件
		}

		// 輸出結果
		outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, actualStartRange, actualEndRange)
		outputFile := fmt.Sprintf("%s_最大平均值計算.csv", fileBaseName)

		a.csvHandler.WriteCSVToOutputDirectory(filepath.Base(dirName), outputFile, outputData)
	}

	return nil
}

// executeBatchCalculationDirect 直接處理外部目錄的文件
func (a *App) executeBatchCalculationDirect(files []string, outputDirName string, windowSize int, startRange, endRange float64, useCustomRange bool) error {
	// 處理每個文件
	for _, fullPath := range files {
		fileName := filepath.Base(fullPath)
		fileBaseName := strings.TrimSuffix(fileName, ".csv")

		records, err := a.csvHandler.ReadCSVExternal(fullPath)
		if err != nil {
			continue // 跳過有錯誤的文件
		}

		// 如果沒有使用自定義範圍，從數據中獲取預設範圍
		actualStartRange, actualEndRange := startRange, endRange
		if !useCustomRange {
			if len(records) > 1 && len(records[1]) > 0 {
				actualStartRange, _ = strconv.ParseFloat(records[1][0], 64)
			}
			if len(records) > 1 && len(records[len(records)-1]) > 0 {
				actualEndRange, _ = strconv.ParseFloat(records[len(records)-1][0], 64)
			}
		}

		// 計算最大平均值
		var results []models.MaxMeanResult
		if actualStartRange == 0 && actualEndRange == 0 {
			results, err = a.maxMeanCalc.CalculateFromRawData(records, windowSize)
		} else {
			results, err = a.maxMeanCalc.CalculateFromRawDataWithRange(records, windowSize, actualStartRange, actualEndRange)
		}

		if err != nil {
			continue // 跳過計算失敗的文件
		}

		// 輸出結果
		outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, actualStartRange, actualEndRange)
		outputFile := fmt.Sprintf("%s_最大平均值計算.csv", fileBaseName)

		a.csvHandler.WriteCSVToOutputDirectory(outputDirName, outputFile, outputData)
	}

	return nil
}

// executeNormalizationWithCallback 執行資料標準化（帶回調）
func (a *App) executeNormalizationWithCallback(mainFilePath, refFilePath, outputName string, callback func(success bool)) {
	success := false
	defer func() {
		callback(success)
	}()

	a.executeNormalizationInternal(mainFilePath, refFilePath, outputName, &success)
}

// executeNormalization 執行資料標準化
func (a *App) executeNormalization(mainFilePath, refFilePath, outputName string) {
	success := false
	a.executeNormalizationInternal(mainFilePath, refFilePath, outputName, &success)
}

// executeNormalizationInternal 執行資料標準化內部實現
func (a *App) executeNormalizationInternal(mainFilePath, refFilePath, outputName string, success *bool) {
	a.updateStatus("執行標準化中...")

	// 驗證輸入
	if mainFilePath == "" {
		a.showError("請選擇主要資料檔案")
		return
	}

	if refFilePath == "" {
		a.showError("請選擇參考資料檔案")
		return
	}

	// 讀取主要資料檔案
	mainRecords, err := a.csvHandler.ReadCSV(mainFilePath)
	if err != nil {
		a.showError(fmt.Sprintf("讀取主要資料檔案失敗: %v", err))
		return
	}

	// 讀取參考資料檔案
	refRecords, err := a.csvHandler.ReadCSV(refFilePath)
	if err != nil {
		a.showError(fmt.Sprintf("讀取參考資料檔案失敗: %v", err))
		return
	}

	// 執行標準化
	normalizedData, err := a.normalizer.NormalizeFromRawData(mainRecords, refRecords)
	if err != nil {
		a.showError(fmt.Sprintf("標準化計算失敗: %v", err))
		return
	}

	// 生成輸出檔名
	if outputName == "" {
		mainFileName := filepath.Base(mainFilePath)
		mainBaseName := strings.TrimSuffix(mainFileName, ".csv")
		outputName = fmt.Sprintf("%s_標準化.csv", mainBaseName)
	} else if !strings.HasSuffix(outputName, ".csv") {
		outputName += ".csv"
	}

	// 轉換為CSV格式
	outputData := a.csvHandler.ConvertNormalizedDataToCSV(normalizedData)

	// 保存結果
	err = a.csvHandler.WriteCSVToOutput(outputName, outputData)
	if err != nil {
		a.showError(fmt.Sprintf("保存結果失敗: %v", err))
		return
	}

	a.updateStatus("標準化完成！")
	a.showInfo("資料標準化成功完成，結果已保存到輸出目錄")
	*success = true
}

// executePhaseAnalysis 執行階段分析
func (a *App) executePhaseAnalysis(dataFilePath, phaseFilePath string, phaseLabels []string, outputName string) {
	a.updateStatus("執行階段分析中...")

	// 驗證輸入
	if dataFilePath == "" {
		a.showError("請選擇資料檔案")
		return
	}

	if len(phaseLabels) == 0 || (len(phaseLabels) == 1 && strings.TrimSpace(phaseLabels[0]) == "") {
		a.showError("請輸入階段標籤")
		return
	}

	// 清理階段標籤（移除空白行）
	var cleanLabels []string
	for _, label := range phaseLabels {
		if trimmed := strings.TrimSpace(label); trimmed != "" {
			cleanLabels = append(cleanLabels, trimmed)
		}
	}

	if len(cleanLabels) == 0 {
		a.showError("請輸入有效的階段標籤")
		return
	}

	// 讀取資料檔案
	dataRecords, err := a.csvHandler.ReadCSV(dataFilePath)
	if err != nil {
		a.showError(fmt.Sprintf("讀取資料檔案失敗: %v", err))
		return
	}

	// 執行階段分析 (使用正確的API)
	analysisResult, err := a.phaseAnalyzer.AnalyzeFromRawData(dataRecords, cleanLabels)
	if err != nil {
		a.showError(fmt.Sprintf("階段分析失敗: %v", err))
		return
	}

	// 生成輸出檔名
	if outputName == "" {
		dataFileName := filepath.Base(dataFilePath)
		dataBaseName := strings.TrimSuffix(dataFileName, ".csv")
		outputName = fmt.Sprintf("%s_階段分析.csv", dataBaseName)
	} else if !strings.HasSuffix(outputName, ".csv") {
		outputName += ".csv"
	}

	// 轉換為CSV格式 (使用正確的API)
	outputData := a.csvHandler.ConvertPhaseAnalysisToCSV(dataRecords[0], &analysisResult.PhaseResults[0], analysisResult.MaxTimeIndex)

	// 保存結果
	err = a.csvHandler.WriteCSVToOutput(outputName, outputData)
	if err != nil {
		a.showError(fmt.Sprintf("保存結果失敗: %v", err))
		return
	}

	a.updateStatus("階段分析完成！")
	a.showInfo("階段分析成功完成，結果已保存到輸出目錄")
}

// saveConfiguration 保存配置設定
func (a *App) saveConfiguration(scalingFactorStr, precisionStr, outputFormat string, bomEnabled bool, phaseLabelsText, inputDir, outputDir, operateDir string) {
	logger := a.logger.WithContext("operation", "save_configuration")
	logger.Info("開始保存配置")

	a.updateStatus("保存配置中...")

	// 驗證縮放因子
	scalingFactor, err := a.validator.ValidateScalingFactor(scalingFactorStr)
	if err != nil {
		a.handleValidationError("縮放因子驗證失敗", err, logger)
		return
	}

	// 驗證精度
	precision, err := a.validator.ValidatePrecision(precisionStr)
	if err != nil {
		a.handleValidationError("精度驗證失敗", err, logger)
		return
	}

	// 驗證輸出格式
	if err := a.validator.ValidateOutputFormat(outputFormat); err != nil {
		a.handleValidationError("輸出格式驗證失敗", err, logger)
		return
	}

	// 驗證階段標籤
	phaseLabels, err := a.validator.ValidatePhaseLabels(phaseLabelsText)
	if err != nil {
		a.handleValidationError("階段標籤驗證失敗", err, logger)
		return
	}

	// 驗證目錄路徑
	if err := a.validator.ValidateDirectoryPath(inputDir); err != nil {
		a.handleValidationError("輸入目錄驗證失敗", err, logger)
		return
	}
	if err := a.validator.ValidateDirectoryPath(outputDir); err != nil {
		a.handleValidationError("輸出目錄驗證失敗", err, logger)
		return
	}
	if err := a.validator.ValidateDirectoryPath(operateDir); err != nil {
		a.handleValidationError("操作目錄驗證失敗", err, logger)
		return
	}

	// 更新配置
	a.config.ScalingFactor = scalingFactor
	a.config.Precision = precision
	a.config.OutputFormat = outputFormat
	a.config.BOMEnabled = bomEnabled
	a.config.PhaseLabels = phaseLabels
	a.config.InputDir = strings.TrimSpace(inputDir)
	a.config.OutputDir = strings.TrimSpace(outputDir)
	a.config.OperateDir = strings.TrimSpace(operateDir)

	// 驗證配置
	if err := a.config.Validate(); err != nil {
		a.showError(fmt.Sprintf("配置驗證失敗: %v", err))
		return
	}

	// 保存到檔案
	if err := a.config.SaveConfig("config.json"); err != nil {
		a.showError(fmt.Sprintf("保存配置檔案失敗: %v", err))
		return
	}

	// 確保目錄存在
	if err := a.config.EnsureDirectories(); err != nil {
		a.showError(fmt.Sprintf("創建目錄失敗: %v", err))
		return
	}

	// 更新模組實例的配置
	a.csvHandler = io.NewCSVHandler(a.config)
	a.maxMeanCalc = calculator.NewMaxMeanCalculator(a.config.ScalingFactor)
	a.normalizer = calculator.NewNormalizer(a.config.ScalingFactor)
	a.phaseAnalyzer = calculator.NewPhaseAnalyzer(a.config.ScalingFactor, a.config.PhaseLabels)

	a.updateStatus("配置保存成功！")
	a.showInfo("配置已成功保存並應用")
}

// resetToDefaults 重置為默認配置
func (a *App) resetToDefaults(scalingFactorEntry, precisionEntry *widget.Entry, outputFormatRadio *widget.RadioGroup, bomCheck *widget.Check, phaseLabelsEntry *widget.Entry, inputDirEntry, outputDirEntry, operateDirEntry *widget.Entry) {
	defaultConfig := config.DefaultConfig()

	// 更新UI元件
	scalingFactorEntry.SetText(fmt.Sprintf("%d", defaultConfig.ScalingFactor))
	precisionEntry.SetText(fmt.Sprintf("%d", defaultConfig.Precision))
	outputFormatRadio.SetSelected(defaultConfig.OutputFormat)
	bomCheck.SetChecked(defaultConfig.BOMEnabled)
	phaseLabelsEntry.SetText(strings.Join(defaultConfig.PhaseLabels, "\n"))
	inputDirEntry.SetText(defaultConfig.InputDir)
	outputDirEntry.SetText(defaultConfig.OutputDir)
	operateDirEntry.SetText(defaultConfig.OperateDir)

	a.updateStatus("已重置為默認配置")
}

// handleValidationError 處理驗證錯誤
func (a *App) handleValidationError(context string, err error, logger *logging.Logger) {
	logger.Error(context, err, map[string]interface{}{
		"error_type": "validation_error",
	})

	// 檢查是否為結構化錯誤
	if appErr, ok := err.(*errors.AppError); ok {
		a.showError(appErr.Message)
	} else if validationErr, ok := err.(*errors.ValidationError); ok {
		a.showError(validationErr.Message)
	} else {
		a.showError(fmt.Sprintf("%s: %v", context, err))
	}
}

// executeChartGeneration 執行圖表生成（直接保存）
func (a *App) executeChartGeneration(filePath string, selectedColumns []string, chartTitle string) {
	a.updateStatus("正在生成圖表...")

	dataset, chartConfig, err := a.prepareChartData(filePath, selectedColumns, chartTitle)
	if err != nil {
		a.showError(err.Error())
		return
	}

	// 生成輸出檔案名
	baseName := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	outputFileName := fmt.Sprintf("%s_圖表.png", baseName)
	outputPath := filepath.Join(a.config.OutputDir, outputFileName)

	// 生成圖表
	err = a.chartGen.GenerateLineChart(dataset, chartConfig, outputPath)
	if err != nil {
		a.showError(fmt.Sprintf("生成圖表失敗: %v", err))
		return
	}

	a.updateStatus("圖表生成完成！")
	a.showInfo(fmt.Sprintf("圖表已保存到: %s", outputPath))
}

// executeChartPreview 執行圖表預覽
func (a *App) executeChartPreview(filePath string, selectedColumns []string, chartTitle string) {
	a.updateStatus("正在生成圖表預覽...")

	dataset, chartConfig, err := a.prepareChartData(filePath, selectedColumns, chartTitle)
	if err != nil {
		a.showError(err.Error())
		return
	}

	// 生成圖表圖像
	img, err := a.chartGen.GenerateLineChartImage(dataset, chartConfig)
	if err != nil {
		a.showError(fmt.Sprintf("生成圖表預覽失敗: %v", err))
		return
	}

	// 顯示預覽視窗
	a.showChartPreview(img, filePath, selectedColumns, chartTitle, dataset, chartConfig)
}

// prepareChartData 準備圖表數據
func (a *App) prepareChartData(filePath string, selectedColumns []string, chartTitle string) (*models.EMGDataset, chart.ChartConfig, error) {
	// 驗證輸入
	if filePath == "" {
		return nil, chart.ChartConfig{}, fmt.Errorf("請選擇CSV檔案")
	}
	if len(selectedColumns) == 0 {
		return nil, chart.ChartConfig{}, fmt.Errorf("請至少選擇一個數據列")
	}

	// 讀取CSV檔案
	records, err := a.csvHandler.ReadCSV(filePath)
	if err != nil {
		return nil, chart.ChartConfig{}, fmt.Errorf("讀取CSV檔案失敗: %v", err)
	}

	if len(records) < 2 {
		return nil, chart.ChartConfig{}, fmt.Errorf("CSV檔案格式無效：需要至少包含標題和一行數據")
	}

	// 創建 EMG 數據集
	dataset := &models.EMGDataset{
		Headers: make([]string, len(records[0])),
		Data:    make([]models.EMGData, 0, len(records)-1),
	}

	// 複製標題
	copy(dataset.Headers, records[0])

	// 手動解析數據（簡化版本）
	for i := 1; i < len(records); i++ {
		row := records[i]
		if len(row) < 2 {
			continue
		}

		// 解析時間
		timeVal := float64(i - 1) // 使用索引作為時間
		if len(row) > 0 && row[0] != "" {
			// 嘗試解析實際時間值
			var parseErr error
			if timeVal, parseErr = strconv.ParseFloat(row[0], 64); parseErr != nil {
				timeVal = float64(i - 1) // 回退到索引
			}
		}

		// 解析通道數據
		channels := make([]float64, 0, len(row)-1)
		for j := 1; j < len(row); j++ {
			if val, err := strconv.ParseFloat(row[j], 64); err == nil {
				channels = append(channels, val)
			} else {
				channels = append(channels, 0.0) // 默認值
			}
		}

		data := models.EMGData{
			Time:     timeVal,
			Channels: channels,
		}
		dataset.Data = append(dataset.Data, data)
	}

	if len(dataset.Data) == 0 {
		return nil, chart.ChartConfig{}, fmt.Errorf("解析後數據集為空")
	}

	// 創建圖表配置
	chartConfig := chart.ChartConfig{
		Title:      chartTitle,
		XAxisLabel: "時間 (秒)",
		YAxisLabel: "數值",
		Width:      vg.Points(800),
		Height:     vg.Points(600),
		Columns:    selectedColumns,
	}

	return dataset, chartConfig, nil
}
