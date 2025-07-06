package gui

import (
	"count_mean/internal/errors"
	"fmt"
	"path/filepath"
	"strings"
)

// processLargeFile 處理大文件
func (a *App) processLargeFile(filePath string, windowSize int, startRange, endRange float64, useCustomRange bool) error {
	a.logger.Info("開始處理大文件", map[string]interface{}{
		"file_path":        filePath,
		"window_size":      windowSize,
		"use_custom_range": useCustomRange,
	})

	// 取得檔案名稱（不含路徑和副檔名）
	fileName := filepath.Base(filePath)
	originalFileName := strings.TrimSuffix(fileName, ".csv")

	// 設置進度回調
	progressCallback := func(processed, total int64, percentage float64) {
		a.updateStatus(fmt.Sprintf("處理大文件中... %.1f%% (%d/%d)", percentage, processed, total))
	}

	// 使用流式處理計算最大平均值
	result, err := a.csvHandler.ProcessLargeFile(filePath, windowSize, progressCallback)
	if err != nil {
		return fmt.Errorf("大文件處理失敗: %w", err)
	}

	a.logger.Info("大文件處理完成", map[string]interface{}{
		"processed_lines": result.ProcessedLines,
		"duration_ms":     result.Duration.Milliseconds(),
		"memory_used_mb":  result.MemoryUsed / 1024 / 1024,
		"results_count":   len(result.Results),
	})

	// 如果沒有使用自定義範圍，使用實際的數據範圍
	if !useCustomRange {
		if result.ProcessedLines > 1 {
			// 使用處理結果中的時間範圍
			if len(result.Results) > 0 {
				// 從結果中計算實際的開始和結束時間
				startRange = result.Results[0].StartTime
				endRange = result.Results[0].EndTime
				for _, res := range result.Results {
					if res.StartTime < startRange {
						startRange = res.StartTime
					}
					if res.EndTime > endRange {
						endRange = res.EndTime
					}
				}
			}
		}
	}

	// 轉換結果為 CSV 格式
	outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(result.Headers, result.Results, startRange, endRange)
	outputFile := fmt.Sprintf("%s_最大平均值計算.csv", originalFileName)

	// 使用流式寫入保存結果
	if len(outputData) > 1000 {
		// 對於大量輸出數據，使用流式寫入
		return a.csvHandler.WriteLargeCSVStreaming(filepath.Join(a.config.OutputDir, outputFile), outputData, progressCallback)
	} else {
		// 對於少量輸出數據，使用標準寫入
		return a.csvHandler.WriteCSVToOutput(outputFile, outputData)
	}
}

// processBatchLargeFile 批處理大文件
func (a *App) processBatchLargeFile(filePath, dirName, fileBaseName string, windowSize int, startRange, endRange float64, useCustomRange bool) error {
	a.logger.Info("開始批處理大文件", map[string]interface{}{
		"file_path":      filePath,
		"dir_name":       dirName,
		"file_base_name": fileBaseName,
	})

	// 設置進度回調
	progressCallback := func(processed, total int64, percentage float64) {
		a.updateStatus(fmt.Sprintf("批處理大文件 %s... %.1f%% (%d/%d)",
			fileBaseName, percentage, processed, total))
	}

	// 使用流式處理計算最大平均值
	result, err := a.csvHandler.ProcessLargeFile(filePath, windowSize, progressCallback)
	if err != nil {
		return fmt.Errorf("大文件批處理失敗: %w", err)
	}

	// 如果沒有使用自定義範圍，使用實際的數據範圍
	actualStartRange, actualEndRange := startRange, endRange
	if !useCustomRange {
		if result.ProcessedLines > 1 && len(result.Results) > 0 {
			// 從結果中計算實際的開始和結束時間
			actualStartRange = result.Results[0].StartTime
			actualEndRange = result.Results[0].EndTime
			for _, res := range result.Results {
				if res.StartTime < actualStartRange {
					actualStartRange = res.StartTime
				}
				if res.EndTime > actualEndRange {
					actualEndRange = res.EndTime
				}
			}
		}
	}

	// 轉換結果為 CSV 格式
	outputData := a.csvHandler.ConvertMaxMeanResultsToCSV(result.Headers, result.Results, actualStartRange, actualEndRange)
	outputFile := fmt.Sprintf("%s_最大平均值計算.csv", fileBaseName)

	// 保存到對應的目錄
	return a.csvHandler.WriteCSVToOutputDirectory(dirName, outputFile, outputData)
}

// checkAndHandleLargeFile 檢查並處理大文件
func (a *App) checkAndHandleLargeFile(filePath string, windowSize int, startRange, endRange float64, useCustomRange bool) (bool, error) {
	// 檢查是否為大文件
	fileInfo, err := a.csvHandler.GetFileInfo(filePath)
	if err != nil {
		a.logger.Warn("無法獲取文件信息，使用標準處理方式", map[string]interface{}{
			"file_path": filePath,
			"error":     err.Error(),
		})
		return false, nil
	}

	if fileInfo.IsLarge {
		a.logger.Info("檢測到大文件，使用流式處理", map[string]interface{}{
			"file_path":  filePath,
			"file_size":  fileInfo.Size,
			"line_count": fileInfo.LineCount,
		})
		return true, a.processLargeFile(filePath, windowSize, startRange, endRange, useCustomRange)
	}

	return false, nil
}

// handleLargeFileError 處理大文件錯誤
func (a *App) handleLargeFileError(err error, filePath string, windowSize int, startRange, endRange float64, useCustomRange bool) error {
	// 如果是大文件錯誤，嘗試使用大文件處理
	if appErr, ok := err.(*errors.AppError); ok && appErr.Code == errors.ErrCodeFileTooLarge {
		a.logger.Info("檢測到大文件錯誤，切換到大文件處理模式", map[string]interface{}{
			"file_path": filePath,
		})
		return a.processLargeFile(filePath, windowSize, startRange, endRange, useCustomRange)
	}
	return err
}
