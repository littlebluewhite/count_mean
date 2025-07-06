package main

import (
	"count_mean/internal/config"
	"count_mean/internal/io"
	"count_mean/internal/logging"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 示範大文件處理功能
func main() {
	// 初始化日誌系統
	if err := logging.InitLogger(logging.LevelInfo, "./logs", false); err != nil {
		fmt.Printf("日誌初始化失敗: %v\n", err)
		os.Exit(1)
	}

	logger := logging.GetLogger("large_file_demo")
	logger.Info("大文件處理示範開始", map[string]interface{}{
		"demo_version": "1.0",
	})

	// 創建配置
	cfg := config.DefaultConfig()
	cfg.LogLevel = "info"
	cfg.LogFormat = "text"

	// 創建 CSV 處理器
	csvHandler := io.NewCSVHandler(cfg)

	// 創建大型測試數據文件
	testFile := filepath.Join(cfg.InputDir, "large_test_data.csv")
	if err := os.MkdirAll(cfg.InputDir, 0755); err != nil {
		logger.Fatal("無法創建輸入目錄", err)
	}

	if err := createLargeTestFile(testFile, 50000); err != nil {
		logger.Error("創建大型測試文件失敗", err)
		return
	}

	logger.Info("大型測試文件創建完成", map[string]interface{}{
		"file_path": testFile,
	})

	// 測試文件信息獲取
	fmt.Println("=== 測試文件信息獲取 ===")
	fileInfo, err := csvHandler.GetFileInfo(testFile)
	if err != nil {
		logger.Error("獲取文件信息失敗", err)
		return
	}

	fmt.Printf("文件路徑: %s\n", fileInfo.Path)
	fmt.Printf("文件大小: %d bytes (%.2f MB)\n", fileInfo.Size, float64(fileInfo.Size)/1024/1024)
	fmt.Printf("行數: %d\n", fileInfo.LineCount)
	fmt.Printf("列數: %d\n", fileInfo.ColumnCount)
	fmt.Printf("是否為大文件: %v\n", fileInfo.IsLarge)

	// 測試流式讀取
	fmt.Println("\n=== 測試流式讀取 ===")
	progressCount := 0
	readCallback := func(processed, total int64, percentage float64) {
		progressCount++
		if progressCount%10 == 0 || percentage >= 100.0 {
			fmt.Printf("讀取進度: %.1f%% (%d/%d 行)\n", percentage, processed, total)
		}
	}

	readResult, err := csvHandler.ReadLargeCSVStreaming(testFile, readCallback)
	if err != nil {
		logger.Error("流式讀取失敗", err)
		return
	}

	fmt.Printf("流式讀取完成:\n")
	fmt.Printf("  處理行數: %d\n", readResult.ProcessedLines)
	fmt.Printf("  耗時: %v\n", readResult.Duration)
	fmt.Printf("  記憶體使用: %.2f MB\n", float64(readResult.MemoryUsed)/1024/1024)

	// 測試大文件計算處理
	fmt.Println("\n=== 測試大文件計算處理 ===")
	windowSize := 100
	progressCount = 0
	calcCallback := func(processed, total int64, percentage float64) {
		progressCount++
		if progressCount%20 == 0 || percentage >= 100.0 {
			fmt.Printf("計算進度: %.1f%% (%d/%d 行)\n", percentage, processed, total)
		}
	}

	calcResult, err := csvHandler.ProcessLargeFile(testFile, windowSize, calcCallback)
	if err != nil {
		logger.Error("大文件計算處理失敗", err)
		return
	}

	fmt.Printf("大文件計算完成:\n")
	fmt.Printf("  處理行數: %d\n", calcResult.ProcessedLines)
	fmt.Printf("  結果數量: %d\n", len(calcResult.Results))
	fmt.Printf("  耗時: %v\n", calcResult.Duration)
	fmt.Printf("  記憶體使用: %.2f MB\n", float64(calcResult.MemoryUsed)/1024/1024)

	// 顯示部分計算結果
	fmt.Printf("\n前5個通道的計算結果:\n")
	for i, result := range calcResult.Results {
		if i >= 5 {
			break
		}
		fmt.Printf("  通道 %d: 最大平均值=%.6f, 時間範圍=[%.3f, %.3f]\n",
			result.ColumnIndex, result.MaxMean, result.StartTime, result.EndTime)
	}

	// 測試結果寫入
	fmt.Println("\n=== 測試結果寫入 ===")
	outputData := csvHandler.ConvertMaxMeanResultsToCSV(calcResult.Headers, calcResult.Results, 0.0, 100.0)
	outputFile := filepath.Join(cfg.OutputDir, "large_file_results.csv")

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		logger.Fatal("無法創建輸出目錄", err)
	}

	writeCallback := func(processed, total int64, percentage float64) {
		if percentage >= 100.0 {
			fmt.Printf("寫入完成: 100.0%% (%d/%d 行)\n", processed, total)
		}
	}

	if err := csvHandler.WriteLargeCSVStreaming(outputFile, outputData, writeCallback); err != nil {
		logger.Error("寫入結果失敗", err)
		return
	}

	fmt.Printf("結果已保存到: %s\n", outputFile)

	logger.Info("大文件處理示範完成", map[string]interface{}{
		"input_file":  testFile,
		"output_file": outputFile,
		"success":     true,
	})

	fmt.Println("\n大文件處理示範完成！")
	fmt.Println("功能特色:")
	fmt.Println("  ✓ 自動檢測大文件")
	fmt.Println("  ✓ 記憶體高效的流式處理")
	fmt.Println("  ✓ 實時進度追蹤")
	fmt.Println("  ✓ 滑動視窗計算")
	fmt.Println("  ✓ 自動記憶體管理")
	fmt.Println("  ✓ 可配置的處理參數")
}

// createLargeTestFile 創建大型測試文件
func createLargeTestFile(filename string, rows int) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// 寫入標題行
	headers := []string{"Time"}
	for i := 1; i <= 10; i++ {
		headers = append(headers, fmt.Sprintf("Ch%d", i))
	}
	file.WriteString(strings.Join(headers, ",") + "\n")

	// 寫入數據行
	for i := 0; i < rows; i++ {
		time := float64(i) * 0.001 // 1ms 間隔
		row := []string{fmt.Sprintf("%.3f", time)}

		// 生成每個通道的模擬 EMG 數據
		for ch := 1; ch <= 10; ch++ {
			// 模擬 EMG 訊號：基線 + 隨機噪聲 + 週期性激活
			baseline := 50.0
			noise := (float64(i*ch) * 17) // 偽隨機噪聲
			noise = float64(int(noise)%100) - 50.0

			// 模擬肌肉激活（每1000個樣本有一次激活）
			activation := 0.0
			if i%1000 >= 100 && i%1000 <= 200 {
				activation = 200.0 + float64(ch)*10.0
			}

			value := baseline + noise*0.5 + activation
			row = append(row, fmt.Sprintf("%.3f", value))
		}

		file.WriteString(strings.Join(row, ",") + "\n")
	}

	return nil
}
