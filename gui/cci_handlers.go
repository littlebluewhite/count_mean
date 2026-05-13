package gui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"count_mean/internal/calculator"
	"count_mean/internal/cci"
	"count_mean/internal/security/fsperm"
)

// CCIParams 共同收縮分析參數.
type CCIParams struct {
	ManifestFile string `json:"manifestFile"`
	DataFolder   string `json:"dataFolder"`
	SubjectIndex int    `json:"subjectIndex"`
}

// CCIResult 共同收縮分析結果.
type CCIResult struct {
	OutputCSVPath string             `json:"outputCSVPath"`
	Subject       string             `json:"subject"`
	PairNames     []string           `json:"pairNames"`
	ChartHTML     string             `json:"chartHTML"`
	PhasePercents map[string]float64 `json:"phasePercents"`
	PhaseTimes    map[string]float64 `json:"phaseTimes"`
	Report        string             `json:"report"`
	Success       bool               `json:"success"`
	Message       string             `json:"message"`
}

// CCIDownloadParams 共同收縮圖表下載參數.
type CCIDownloadParams struct {
	ImageData string `json:"imageData"`
	Subject   string `json:"subject"`
}

// AnalyzeCCI 執行 CCI Rudolph 共同收縮分析.
func (a *App) AnalyzeCCI(params CCIParams) (*CCIResult, error) {
	a.logger.Info("開始 CCI 分析", map[string]interface{}{"params": params})

	if err := validateCCIParams(params); err != nil {
		return nil, err
	}

	analysisParams := &cci.CCIParams{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		SubjectIndex: params.SubjectIndex,
	}

	analysisResult, err := a.cciAnalyzer.AnalyzeCCI(analysisParams)
	if err != nil {
		return failedCCIResult(fmt.Sprintf("分析失敗: %v", err)), nil
	}

	// Export CSV
	s := a.state.Load()
	csvPath, err := a.cciAnalyzer.ExportToCSV(analysisResult, s.config.OutputDir)
	if err != nil {
		return failedCCIResult(fmt.Sprintf("CSV 導出失敗: %v", err)), nil
	}

	// Generate interactive chart HTML
	var buf bytes.Buffer
	if err := cci.GenerateCCIInteractiveChart(analysisResult, &buf); err != nil {
		return failedCCIResult(fmt.Sprintf("圖表生成失敗: %v", err)), nil
	}

	pairNames := make([]string, len(analysisResult.PairResults))
	for i, pr := range analysisResult.PairResults {
		pairNames[i] = pr.PairName
	}

	report := cci.GenerateReport(analysisResult)

	result := &CCIResult{
		OutputCSVPath: csvPath,
		Subject:       analysisResult.Subject,
		PairNames:     pairNames,
		ChartHTML:     buf.String(),
		PhasePercents: analysisResult.PhasePercents,
		PhaseTimes:    analysisResult.PhaseTimes,
		Report:        report,
		Success:       true,
		Message:       "分析完成",
	}

	a.logger.Info("CCI 分析完成", map[string]interface{}{"csv": csvPath})

	return result, nil
}

// DownloadCCIChart 下載 CCI 圖表為 PNG 檔案.
func (a *App) DownloadCCIChart(params CCIDownloadParams) (*ChartResult, error) {
	a.logger.Info("開始下載 CCI 圖表", nil)

	dataURL := params.ImageData
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		return nil, ErrInvalidImageFormat
	}

	base64Data := strings.TrimPrefix(dataURL, "data:image/png;base64,")

	pngData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, fmt.Errorf("解碼圖片數據失敗: %w", err)
	}

	// params.Subject 來自前端，需先 sanitize 避免路徑穿越（"../x" 之類）。
	safeSubject := calculator.SanitizeFileName(params.Subject)
	s := a.state.Load()
	outputPath := filepath.Join(
		s.config.OutputDir,
		fmt.Sprintf("%s_CCI_Rudolph.png", safeSubject),
	)

	if err := fsperm.WriteFileNoFollow(outputPath, pngData); err != nil {
		return nil, fmt.Errorf("保存圖片失敗: %w", err)
	}

	a.logger.Info("CCI 圖表下載完成", map[string]interface{}{
		"output": outputPath,
	})

	return &ChartResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    fmt.Sprintf("圖表已下載至: %s", outputPath),
	}, nil
}

// validateCCIParams checks required CCI parameters.
func validateCCIParams(params CCIParams) error {
	if params.ManifestFile == "" {
		return ErrNoManifestFile
	}

	if params.DataFolder == "" {
		return ErrNoDataFolder
	}

	return nil
}

// failedCCIResult returns a CCI result indicating failure.
func failedCCIResult(message string) *CCIResult {
	return &CCIResult{
		Success: false,
		Message: message,
	}
}
