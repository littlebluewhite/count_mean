package gui

import (
	"fmt"
	"strings"

	"count_mean/internal/io"
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
