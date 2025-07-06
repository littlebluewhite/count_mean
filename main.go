package main

import (
	"bufio"
	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

func main() {
	// 載入配置
	cfg, err := config.LoadConfig("config.json")
	if err != nil {
		log.Printf("載入配置失敗，使用默認配置: %v", err)
		cfg = config.DefaultConfig()
	}

	// 確保必要的目錄存在
	if err := cfg.EnsureDirectories(); err != nil {
		log.Fatalf("無法創建必要目錄: %v", err)
	}

	// 創建模組實例
	csvHandler := io.NewCSVHandler(cfg)
	maxMeanCalc := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)
	normalizer := calculator.NewNormalizer(cfg.ScalingFactor)
	phaseAnalyzer := calculator.NewPhaseAnalyzer(cfg.ScalingFactor, cfg.PhaseLabels)

	for {
		fmt.Println("EMG 資料分析工具")
		fmt.Println("1. 最大平均值計算")
		fmt.Println("2. 資料標準化")
		fmt.Println("3. 階段分析")
		fmt.Println("4. 設定配置")
		fmt.Println("0. 退出")
		fmt.Print("請選擇功能: ")

		var choice int
		if _, err := fmt.Scanf("%d", &choice); err != nil {
			fmt.Printf("輸入錯誤: %v\n", err)
			continue
		}

		switch choice {
		case 1:
			if err := handleMaxMeanCalculation(csvHandler, maxMeanCalc); err != nil {
				fmt.Printf("最大平均值計算失敗: %v\n", err)
			}
		case 2:
			if err := handleNormalization(csvHandler, normalizer); err != nil {
				fmt.Printf("資料標準化失敗: %v\n", err)
			}
		case 3:
			if err := handlePhaseAnalysis(csvHandler, phaseAnalyzer, cfg); err != nil {
				fmt.Printf("階段分析失敗: %v\n", err)
			}
		case 4:
			if err := handleConfigSettings(cfg); err != nil {
				fmt.Printf("配置設定失敗: %v\n", err)
			}
		case 0:
			fmt.Println("感謝使用！")
			return
		default:
			fmt.Println("無效選擇，請重新輸入")
		}
		fmt.Println()
	}
}

func handleMaxMeanCalculation(csvHandler *io.CSVHandler, calc *calculator.MaxMeanCalculator) error {
	fmt.Println("\n=== 最大平均值計算 ===")
	fmt.Println("1. 處理單一檔案")
	fmt.Println("2. 批量處理資料夾")
	fmt.Print("請選擇處理模式: ")

	var mode int
	if _, err := fmt.Scanf("%d", &mode); err != nil {
		return fmt.Errorf("輸入處理模式失敗: %w", err)
	}

	switch mode {
	case 1:
		return handleSingleFileMaxMean(csvHandler, calc)
	case 2:
		return handleDirectoryMaxMean(csvHandler, calc)
	default:
		return fmt.Errorf("無效的處理模式")
	}
}

func handleSingleFileMaxMean(csvHandler *io.CSVHandler, calc *calculator.MaxMeanCalculator) error {
	// 顯示可用的輸入文件
	files, err := csvHandler.ListInputFiles()
	if err != nil {
		fmt.Printf("警告：無法列出輸入文件: %v\n", err)
	} else if len(files) > 0 {
		fmt.Println("可用的輸入文件:")
		for i, file := range files {
			fmt.Printf("  %d. %s\n", i+1, file)
		}
		fmt.Println()
	}

	// 讀取輸入檔案並獲取檔名
	records, originalFileName, err := csvHandler.ReadCSVFromPromptWithName("請輸入檔案名稱（不含.csv）: ")
	if err != nil {
		return fmt.Errorf("讀取檔案失敗: %w", err)
	}

	return processSingleFileMaxMean(csvHandler, calc, records, "", originalFileName)
}

func handleDirectoryMaxMean(csvHandler *io.CSVHandler, calc *calculator.MaxMeanCalculator) error {
	// 顯示可用的輸入資料夾
	dirs, err := csvHandler.ListInputDirectories()
	if err != nil {
		fmt.Printf("警告：無法列出輸入目錄: %v\n", err)
	} else if len(dirs) > 0 {
		fmt.Println("可用的輸入資料夾:")
		for i, dir := range dirs {
			fmt.Printf("  %d. %s\n", i+1, dir)
		}
		fmt.Println()
	}

	fmt.Print("請輸入要處理的資料夾名稱: ")
	reader := bufio.NewReader(os.Stdin)
	dirName, _ := reader.ReadString('\n')
	dirName = strings.TrimSpace(dirName)

	// 獲取窗口大小
	fmt.Print("請輸入窗口大小（資料點數）: ")
	var windowSize int
	if _, err := fmt.Scanf("%d", &windowSize); err != nil {
		return fmt.Errorf("輸入窗口大小失敗: %w", err)
	}

	// 獲取時間範圍
	var startRange, endRange float64
	var useCustomRange bool

	fmt.Print("請輸入開始範圍秒數（按Enter使用預設值）: ")
	reader = bufio.NewReader(os.Stdin)
	startStr, _ := reader.ReadString('\n')
	startStr = strings.TrimSpace(startStr)

	if startStr != "" {
		startRange, err = strconv.ParseFloat(startStr, 64)
		if err != nil {
			return fmt.Errorf("解析開始範圍失敗: %w", err)
		}
		useCustomRange = true
	}

	fmt.Print("請輸入結束範圍秒數（按Enter使用預設值）: ")
	endStr, _ := reader.ReadString('\n')
	endStr = strings.TrimSpace(endStr)

	if endStr != "" {
		endRange, err = strconv.ParseFloat(endStr, 64)
		if err != nil {
			return fmt.Errorf("解析結束範圍失敗: %w", err)
		}
		useCustomRange = true
	}

	// 列出資料夾中的CSV文件
	csvFiles, err := csvHandler.ListCSVFilesInDirectory(dirName)
	if err != nil {
		return fmt.Errorf("讀取資料夾失敗: %w", err)
	}

	if len(csvFiles) == 0 {
		return fmt.Errorf("資料夾中沒有找到CSV文件")
	}

	fmt.Printf("找到 %d 個CSV文件，開始批量處理...\n", len(csvFiles))

	// 處理每個文件
	for i, fileName := range csvFiles {
		fmt.Printf("處理文件 %d/%d: %s\n", i+1, len(csvFiles), fileName)

		records, err := csvHandler.ReadCSVFromDirectory(dirName, fileName)
		if err != nil {
			fmt.Printf("跳過文件 %s：%v\n", fileName, err)
			continue
		}

		// 提取檔名（不含.csv）
		fileBaseName := strings.TrimSuffix(fileName, ".csv")
		if err := processBatchFileMaxMean(csvHandler, calc, records, dirName, fileBaseName, windowSize, startRange, endRange, useCustomRange); err != nil {
			fmt.Printf("處理文件 %s 失敗：%v\n", fileName, err)
			continue
		}
	}

	fmt.Printf("批量處理完成！結果已保存至 ./output/%s/\n", dirName)
	return nil
}

func processSingleFileMaxMean(csvHandler *io.CSVHandler, calc *calculator.MaxMeanCalculator, records [][]string, dirName, originalFileName string) error {
	// 獲取窗口大小
	fmt.Print("請輸入窗口大小（資料點數）: ")
	var windowSize int
	if _, err := fmt.Scanf("%d", &windowSize); err != nil {
		return fmt.Errorf("輸入窗口大小失敗: %w", err)
	}
	return processFileWithParams(csvHandler, calc, records, originalFileName, dirName, windowSize)
}

func processBatchFileMaxMean(csvHandler *io.CSVHandler, calc *calculator.MaxMeanCalculator, records [][]string, dirName, originalFileName string, windowSize int, startRange, endRange float64, useCustomRange bool) error {
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
	var err error
	if startRange == 0 && endRange == 0 {
		// 使用原始方法（全範圍）
		results, err = calc.CalculateFromRawData(records, windowSize)
	} else {
		// 使用指定範圍
		results, err = calc.CalculateFromRawDataWithRange(records, windowSize, startRange, endRange)
	}

	if err != nil {
		return fmt.Errorf("計算失敗: %w", err)
	}

	// 轉換為CSV格式並輸出
	outputData := csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, startRange, endRange)
	outputFile := fmt.Sprintf("%s_最大平均值計算.csv", originalFileName)

	// 批量處理模式，保存到對應的子目錄
	if err := csvHandler.WriteCSVToOutputDirectory(dirName, outputFile, outputData); err != nil {
		return fmt.Errorf("寫入輸出檔案失敗: %w", err)
	}

	return nil
}

func processFileWithParams(csvHandler *io.CSVHandler, calc *calculator.MaxMeanCalculator, records [][]string, originalFileName, dirName string, windowSize int) error {
	// 獲取時間範圍
	var startRange, endRange float64
	var err error

	fmt.Print("請輸入開始範圍秒數（按Enter使用預設值）: ")
	reader := bufio.NewReader(os.Stdin)
	startStr, _ := reader.ReadString('\n')
	startStr = strings.TrimSpace(startStr)

	if startStr != "" {
		startRange, err = strconv.ParseFloat(startStr, 64)
		if err != nil {
			return fmt.Errorf("解析開始範圍失敗: %w", err)
		}
	} else {
		// 使用第一筆資料的時間
		if len(records) > 1 && len(records[1]) > 0 {
			startRange, _ = strconv.ParseFloat(records[1][0], 64)
		}
	}

	fmt.Print("請輸入結束範圍秒數（按Enter使用預設值）: ")
	endStr, _ := reader.ReadString('\n')
	endStr = strings.TrimSpace(endStr)

	if endStr != "" {
		endRange, err = strconv.ParseFloat(endStr, 64)
		if err != nil {
			return fmt.Errorf("解析結束範圍失敗: %w", err)
		}
	} else {
		// 使用最後一筆資料的時間
		if len(records) > 1 && len(records[len(records)-1]) > 0 {
			endRange, _ = strconv.ParseFloat(records[len(records)-1][0], 64)
		}
	}

	// 計算最大平均值
	var results []models.MaxMeanResult
	if startRange == 0 && endRange == 0 {
		// 使用原始方法（全範圍）
		results, err = calc.CalculateFromRawData(records, windowSize)
	} else {
		// 使用指定範圍
		results, err = calc.CalculateFromRawDataWithRange(records, windowSize, startRange, endRange)
	}

	if err != nil {
		return fmt.Errorf("計算失敗: %w", err)
	}

	// 轉換為CSV格式並輸出
	outputData := csvHandler.ConvertMaxMeanResultsToCSV(records[0], results, startRange, endRange)
	outputFile := fmt.Sprintf("%s_最大平均值計算.csv", originalFileName)

	// 單文件模式，保存到主輸出目錄
	if err := csvHandler.WriteCSVToOutput(outputFile, outputData); err != nil {
		return fmt.Errorf("寫入輸出檔案失敗: %w", err)
	}
	fmt.Printf("計算完成！結果已保存至 ./output/%s\n", outputFile)

	return nil
}

func handleNormalization(csvHandler *io.CSVHandler, normalizer *calculator.Normalizer) error {
	fmt.Println("\n=== 資料標準化 ===")

	// 顯示可用的輸入文件
	files, err := csvHandler.ListInputFiles()
	if err != nil {
		fmt.Printf("警告：無法列出輸入文件: %v\n", err)
	} else if len(files) > 0 {
		fmt.Println("可用的輸入文件:")
		for i, file := range files {
			fmt.Printf("  %d. %s\n", i+1, file)
		}
		fmt.Println()
	}

	// 讀取主數據檔案
	records, err := csvHandler.ReadCSVFromPrompt("請輸入主數據檔案名稱（不含.csv）: ")
	if err != nil {
		return fmt.Errorf("讀取主數據檔案失敗: %w", err)
	}

	// 讀取參考數據檔案
	reference, err := csvHandler.ReadCSVFromPrompt("請輸入參考數據檔案名稱（不含.csv）: ")
	if err != nil {
		return fmt.Errorf("讀取參考數據檔案失敗: %w", err)
	}

	// 執行標準化
	result, err := normalizer.NormalizeFromRawData(records, reference)
	if err != nil {
		return fmt.Errorf("標準化失敗: %w", err)
	}

	// 轉換為CSV格式並輸出
	outputData := csvHandler.ConvertNormalizedDataToCSV(result)
	outputFile := "normalized_result.csv"

	if err := csvHandler.WriteCSVToOutput(outputFile, outputData); err != nil {
		return fmt.Errorf("寫入輸出檔案失敗: %w", err)
	}

	fmt.Printf("標準化完成！結果已保存至 ./output/%s\n", outputFile)
	return nil
}

func handlePhaseAnalysis(csvHandler *io.CSVHandler, analyzer *calculator.PhaseAnalyzer, cfg *config.AppConfig) error {
	fmt.Println("\n=== 階段分析 ===")

	// 顯示可用的輸入文件
	files, err := csvHandler.ListInputFiles()
	if err != nil {
		fmt.Printf("警告：無法列出輸入文件: %v\n", err)
	} else if len(files) > 0 {
		fmt.Println("可用的輸入文件:")
		for i, file := range files {
			fmt.Printf("  %d. %s\n", i+1, file)
		}
		fmt.Println()
	}

	// 讀取數據檔案
	records, err := csvHandler.ReadCSVFromPrompt("請輸入數據檔案名稱（不含.csv）: ")
	if err != nil {
		return fmt.Errorf("讀取數據檔案失敗: %w", err)
	}

	// 獲取階段時間點
	fmt.Printf("請輸入 %d 個階段的時間點（共需 %d 個時間點）:\n", len(cfg.PhaseLabels), len(cfg.PhaseLabels)+1)
	reader := bufio.NewReader(os.Stdin)
	phaseStrings := make([]string, 0, len(cfg.PhaseLabels)+1)

	for i := 0; i <= len(cfg.PhaseLabels); i++ {
		fmt.Printf("時間點 %d: ", i+1)
		timeStr, _ := reader.ReadString('\n')
		phaseStrings = append(phaseStrings, strings.TrimSpace(timeStr))
	}

	// 執行階段分析
	result, err := analyzer.AnalyzeFromRawData(records, phaseStrings)
	if err != nil {
		return fmt.Errorf("階段分析失敗: %w", err)
	}

	// 為每個階段生成輸出檔案
	for i, phaseResult := range result.PhaseResults {
		outputData := csvHandler.ConvertPhaseAnalysisToCSV(records[0], &phaseResult, result.MaxTimeIndex)
		outputFile := fmt.Sprintf("phase_%d_%s.csv", i+1, strings.ReplaceAll(phaseResult.PhaseName, " ", "_"))

		if err := csvHandler.WriteCSVToOutput(outputFile, outputData); err != nil {
			return fmt.Errorf("寫入階段 %d 輸出檔案失敗: %w", i+1, err)
		}

		fmt.Printf("階段 %d (%s) 分析完成！結果已保存至 ./output/%s\n", i+1, phaseResult.PhaseName, outputFile)
	}

	return nil
}

func handleConfigSettings(cfg *config.AppConfig) error {
	fmt.Println("\n=== 配置設定 ===")
	fmt.Printf("目前配置:\n")
	fmt.Printf("1. 縮放因子: %d\n", cfg.ScalingFactor)
	fmt.Printf("2. 精度: %d\n", cfg.Precision)
	fmt.Printf("3. 輸出格式: %s\n", cfg.OutputFormat)
	fmt.Printf("4. BOM啟用: %t\n", cfg.BOMEnabled)
	fmt.Printf("5. 階段標籤: %v\n", cfg.PhaseLabels)

	fmt.Println("\n請選擇要修改的項目（0-5，0表示保存並退出）:")
	var choice int
	if _, err := fmt.Scanf("%d", &choice); err != nil {
		return fmt.Errorf("輸入選擇失敗: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)

	switch choice {
	case 1:
		fmt.Print("請輸入新的縮放因子: ")
		var factor int
		if _, err := fmt.Scanf("%d", &factor); err != nil {
			return fmt.Errorf("輸入縮放因子失敗: %w", err)
		}
		cfg.ScalingFactor = factor
	case 2:
		fmt.Print("請輸入新的精度: ")
		var precision int
		if _, err := fmt.Scanf("%d", &precision); err != nil {
			return fmt.Errorf("輸入精度失敗: %w", err)
		}
		cfg.Precision = precision
	case 3:
		fmt.Print("請輸入新的輸出格式 (csv/json/xlsx): ")
		format, _ := reader.ReadString('\n')
		cfg.OutputFormat = strings.TrimSpace(format)
	case 4:
		fmt.Print("啟用BOM? (true/false): ")
		var enabled string
		if _, err := fmt.Scanf("%s", &enabled); err != nil {
			return fmt.Errorf("輸入BOM設定失敗: %w", err)
		}
		cfg.BOMEnabled = strings.ToLower(enabled) == "true"
	case 5:
		fmt.Print("請輸入階段標籤數量: ")
		var count int
		if _, err := fmt.Scanf("%d", &count); err != nil {
			return fmt.Errorf("輸入標籤數量失敗: %w", err)
		}

		labels := make([]string, 0, count)
		for i := 0; i < count; i++ {
			fmt.Printf("階段 %d 標籤: ", i+1)
			label, _ := reader.ReadString('\n')
			labels = append(labels, strings.TrimSpace(label))
		}
		cfg.PhaseLabels = labels
	case 0:
		// 驗證配置
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("配置驗證失敗: %w", err)
		}

		// 保存配置
		if err := cfg.SaveConfig("config.json"); err != nil {
			return fmt.Errorf("保存配置失敗: %w", err)
		}

		fmt.Println("配置已保存！")
		return nil
	default:
		fmt.Println("無效選擇")
	}

	return handleConfigSettings(cfg) // 遞迴調用繼續設定
}
