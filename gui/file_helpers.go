package gui

import (
	"context"
	"fmt"
	"strings"

	"count_mean/internal/io"
	"count_mean/internal/models"
)

// File type constants for SelectFile dialog.
const (
	FileTypeInput   = "input"
	FileTypeOutput  = "output"
	FileTypeOperate = "operate"
)

// Output filename suffix constants.
const (
	SuffixMaxMean       = "_最大平均值計算"
	SuffixNormalized    = "_標準化"
	SuffixPhaseAnalysis = "_階段分析"
)

// TrimCSVExtension removes .csv extension from filename (case-insensitive).
func TrimCSVExtension(fileName string) string {
	return io.StripCSVExt(fileName)
}

// buildOutputFilename creates an output filename with the given suffix.
func buildOutputFilename(baseName, suffix string) string {
	return fmt.Sprintf("%s%s.csv", baseName, suffix)
}

// calculateWithTimeRange performs MaxMean calculation with optional time range.
//
// 接 *appState snapshot 而非自行 a.state.Load(),保證與 entry method 看到的
// maxMeanCalc 為同一實例 — 避免 SaveConfig 觸發後 entry / helper 用到不同
// ScalingFactor 配置（snapshot 撕裂）。
//
// ctx 由 entry methods 透過 a.context() 取得 Wails Startup 設定的 lifecycle
// context — Wails Shutdown 時會 cancel 該 ctx，maxmean 的 worker / collect /
// WaitForCapacity 會收到取消信號並中止長計算。
func (*App) calculateWithTimeRange(
	ctx context.Context,
	s *appState,
	records [][]string,
	windowSize int,
	startRange, endRange float64,
) ([]models.MaxMeanResult, error) {
	if startRange == 0 && endRange == 0 {
		results, err := s.maxMeanCalc.CalculateFromRawData(ctx, records, windowSize)
		if err != nil {
			return nil, fmt.Errorf("計算最大平均值失敗: %w", err)
		}

		return results, nil
	}

	results, err := s.maxMeanCalc.CalculateFromRawDataWithRange(ctx, records, windowSize, startRange, endRange)
	if err != nil {
		return nil, fmt.Errorf("計算指定範圍最大平均值失敗: %w", err)
	}

	return results, nil
}

// resolveOutputName determines the output filename, applying suffix and .csv extension.
func resolveOutputName(outputPath, baseName, suffix string) string {
	if outputPath != "" {
		if strings.HasSuffix(outputPath, ".csv") {
			return outputPath
		}

		return outputPath + ".csv"
	}

	return buildOutputFilename(baseName, suffix)
}
