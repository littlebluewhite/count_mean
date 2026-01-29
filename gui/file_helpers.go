package gui

import (
	"fmt"
	"path/filepath"
	"strconv"
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

// ReadCSVWithPathValidation reads a CSV file with automatic path validation.
func (a *App) ReadCSVWithPathValidation(filePath, baseDir string) ([][]string, error) {
	filename := filepath.Base(filePath)
	if err := a.validator.ValidateFilename(filename); err != nil {
		return nil, fmt.Errorf("檔案名稱驗證失敗: %w", err)
	}

	if isExternalPath(filePath, baseDir) {
		records, err := a.csvHandler.ReadCSVExternal(filePath)
		if err != nil {
			return nil, fmt.Errorf("讀取外部檔案失敗: %w", err)
		}

		return records, nil
	}

	records, err := a.csvHandler.ReadCSV(filePath)
	if err != nil {
		return nil, fmt.Errorf("讀取檔案失敗: %w", err)
	}

	return records, nil
}

// isExternalPath checks if the file path is outside the base directory.
func isExternalPath(filePath, baseDir string) bool {
	if !filepath.IsAbs(filePath) {
		return false
	}

	fileDir := filepath.Dir(filePath)

	relPath, err := filepath.Rel(baseDir, fileDir)

	return err != nil || strings.HasPrefix(relPath, "..")
}

// ResolveTimeRange determines the actual time range from records.
func ResolveTimeRange(records [][]string, startTime, endTime float64) (float64, float64) {
	if startTime != 0 || endTime != 0 {
		return startTime, endTime
	}

	var start, end float64

	if len(records) > 1 && len(records[1]) > 0 {
		if val, err := strconv.ParseFloat(records[1][0], 64); err == nil {
			start = val
		}
	}

	if len(records) > 1 && len(records[len(records)-1]) > 0 {
		if val, err := strconv.ParseFloat(records[len(records)-1][0], 64); err == nil {
			end = val
		}
	}

	return start, end
}

// TrimCSVExtension removes .csv extension from filename (case-insensitive).
func TrimCSVExtension(fileName string) string {
	return io.StripCSVExt(fileName)
}

// buildOutputFilename creates an output filename with the given suffix.
func buildOutputFilename(baseName, suffix string) string {
	return fmt.Sprintf("%s%s.csv", baseName, suffix)
}

// convertMaxMeanResultsToArray converts MaxMeanResult slice to [][]float64 format.
func convertMaxMeanResultsToArray(results []models.MaxMeanResult) [][]float64 {
	resultData := make([][]float64, 0, len(results))

	for _, result := range results {
		row := []float64{result.MaxMean, result.StartTime, result.EndTime}
		resultData = append(resultData, row)
	}

	return resultData
}

// calculateWithTimeRange performs MaxMean calculation with optional time range.
func (a *App) calculateWithTimeRange(
	records [][]string,
	windowSize int,
	startRange, endRange float64,
) ([]models.MaxMeanResult, error) {
	if startRange == 0 && endRange == 0 {
		results, err := a.maxMeanCalc.CalculateFromRawData(records, windowSize)
		if err != nil {
			return nil, fmt.Errorf("計算最大平均值失敗: %w", err)
		}

		return results, nil
	}

	results, err := a.maxMeanCalc.CalculateFromRawDataWithRange(records, windowSize, startRange, endRange)
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

// convertNormalizedDataToArray converts EMGDataset to [][]float64 format.
func convertNormalizedDataToArray(data *models.EMGDataset) [][]float64 {
	result := make([][]float64, 0, len(data.Data))

	for _, row := range data.Data {
		floatRow := make([]float64, 0, 1+len(row.Channels))
		floatRow = append(floatRow, row.Time)
		floatRow = append(floatRow, row.Channels...)
		result = append(result, floatRow)
	}

	return result
}
