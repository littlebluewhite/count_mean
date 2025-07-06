package main

import (
	"bufio"
	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"fmt"
	"log"
	"os"
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

	// 讀取輸入檔案
	records, err := csvHandler.ReadCSVFromPrompt("請輸入檔案名稱（不含.csv）: ")
	if err != nil {
		return fmt.Errorf("讀取檔案失敗: %w", err)
	}

	// 獲取窗口大小
	fmt.Print("請輸入窗口大小（資料點數）: ")
	var windowSize int
	if _, err := fmt.Scanf("%d", &windowSize); err != nil {
		return fmt.Errorf("輸入窗口大小失敗: %w", err)
	}

	// 計算最大平均值
	results, err := calc.CalculateFromRawData(records, windowSize)
	if err != nil {
		return fmt.Errorf("計算失敗: %w", err)
	}

	// 轉換為CSV格式並輸出
	outputData := csvHandler.ConvertMaxMeanResultsToCSV(records[0], results)
	outputFile := "maxmean_result.csv"

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
