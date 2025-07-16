package main

import (
	"count_mean/internal/calculator"
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"fmt"
	"os"
	"path/filepath"
)

// 示範結構化日誌使用
func main() {
	// 初始化日誌系統（JSON格式）
	if err := logging.InitLogger(logging.LevelDebug, "./logs", true); err != nil {
		fmt.Printf("日誌初始化失敗: %v\n", err)
		os.Exit(1)
	}

	logger := logging.GetLogger("demo")
	logger.Info("結構化日誌示範開始", map[string]interface{}{
		"demo_version": "1.0",
		"format":       "json",
	})

	// 創建配置
	cfg := config.DefaultConfig()
	cfg.LogLevel = "debug"
	cfg.LogFormat = "json"

	// 創建計算器實例
	calculator := calculator.NewMaxMeanCalculator(cfg.ScalingFactor)

	// 創建 CSV 處理器
	csvHandler := io.NewCSVHandler(cfg)

	// 示範數據
	testData := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"0.1", "100.5", "50.2"},
		{"0.2", "120.3", "55.1"},
		{"0.3", "110.8", "52.3"},
		{"0.4", "130.2", "58.7"},
		{"0.5", "125.6", "56.9"},
	}

	logger.Info("創建測試數據", map[string]interface{}{
		"rows":     len(testData),
		"channels": len(testData[0]) - 1,
	})

	// 寫入測試文件
	testFile := filepath.Join(cfg.InputDir, "demo_test.csv")
	if err := os.MkdirAll(cfg.InputDir, 0755); err != nil {
		logger.Fatal("無法創建輸入目錄", err)
	}

	if err := csvHandler.WriteCSV(testFile, testData); err != nil {
		logger.Error("寫入測試文件失敗", err)
		return
	}

	// 讀取並計算
	results, err := calculator.CalculateFromRawData(testData, 2)
	if err != nil {
		logger.Error("計算失敗", err)
		return
	}

	logger.Info("計算完成", map[string]interface{}{
		"result_count": len(results),
		"results":      results,
	})

	// 寫入結果
	outputData := csvHandler.ConvertMaxMeanResultsToCSV(testData[0], results, 0.1, 0.5)
	outputFile := filepath.Join(cfg.OutputDir, "demo_results.csv")
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		logger.Fatal("無法創建輸出目錄", err)
	}

	if err := csvHandler.WriteCSV(outputFile, outputData); err != nil {
		logger.Error("寫入結果失敗", err)
		return
	}

	logger.Info("結構化日誌示範完成", map[string]interface{}{
		"input_file":  testFile,
		"output_file": outputFile,
		"success":     true,
	})

	fmt.Println("結構化日誌示範完成！請查看 ./logs 目錄中的日誌文件")
}
