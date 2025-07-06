package gui

import (
	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"fmt"
	"fyne.io/fyne/v2/widget"
	"path/filepath"
	"strconv"
	"strings"
)

// executeMaxMeanCalculation 執行最大平均值計算
func (a *App) executeMaxMeanCalculation(mode, filePath, dirPath, windowSizeStr, startRangeStr, endRangeStr string) {
	a.updateStatus("執行計算中...")

	// 解析窗口大小
	windowSize, err := strconv.Atoi(windowSizeStr)
	if err != nil || windowSize <= 0 {
		a.showError("無效的窗口大小")
		return
	}

	// 解析時間範圍
	var startRange, endRange float64
	var useCustomRange bool

	if startRangeStr != "" {
		startRange, err = strconv.ParseFloat(startRangeStr, 64)
		if err != nil {
			a.showError("無效的開始範圍秒數")
			return
		}
		useCustomRange = true
	}

	if endRangeStr != "" {
		endRange, err = strconv.ParseFloat(endRangeStr, 64)
		if err != nil {
			a.showError("無效的結束範圍秒數")
			return
		}
		useCustomRange = true
	}

	if mode == "處理單一檔案" {
		if filePath == "" {
			a.showError("請選擇要處理的CSV檔案")
			return
		}
		err = a.executeSingleFileCalculation(filePath, windowSize, startRange, endRange, useCustomRange)
	} else {
		if dirPath == "" {
			a.showError("請選擇要處理的資料夾")
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
	// 讀取檔案
	records, err := a.csvHandler.ReadCSV(filePath)
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
	// 列出資料夾中的CSV文件
	files, err := filepath.Glob(filepath.Join(dirPath, "*.csv"))
	if err != nil {
		return fmt.Errorf("搜尋CSV文件失敗: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("資料夾中沒有找到CSV文件")
	}

	// 取得資料夾名稱
	dirName := filepath.Base(dirPath)

	// 處理每個文件
	for _, fullPath := range files {
		fileName := filepath.Base(fullPath)
		fileBaseName := strings.TrimSuffix(fileName, ".csv")

		records, err := a.csvHandler.ReadCSV(fullPath)
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

		a.csvHandler.WriteCSVToOutputDirectory(dirName, outputFile, outputData)
	}

	return nil
}

// executeNormalization 執行資料標準化
func (a *App) executeNormalization(mainFilePath, refFilePath, outputName string) {
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
	a.updateStatus("保存配置中...")

	// 解析縮放因子
	scalingFactor, err := strconv.Atoi(scalingFactorStr)
	if err != nil || scalingFactor <= 0 {
		a.showError("無效的縮放因子，必須是大於0的整數")
		return
	}

	// 解析精度
	precision, err := strconv.Atoi(precisionStr)
	if err != nil || precision < 0 || precision > 15 {
		a.showError("無效的精度，必須是0-15之間的整數")
		return
	}

	// 解析階段標籤
	var phaseLabels []string
	if strings.TrimSpace(phaseLabelsText) != "" {
		lines := strings.Split(phaseLabelsText, "\n")
		for _, line := range lines {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				phaseLabels = append(phaseLabels, trimmed)
			}
		}
	}

	if len(phaseLabels) == 0 {
		a.showError("請至少輸入一個階段標籤")
		return
	}

	// 驗證目錄
	if strings.TrimSpace(inputDir) == "" {
		a.showError("輸入目錄不能為空")
		return
	}
	if strings.TrimSpace(outputDir) == "" {
		a.showError("輸出目錄不能為空")
		return
	}
	if strings.TrimSpace(operateDir) == "" {
		a.showError("操作目錄不能為空")
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
