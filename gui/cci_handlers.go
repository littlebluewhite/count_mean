package gui

import (
	"bytes"
	"fmt"
	"path/filepath"

	"count_mean/internal/cci"
	"count_mean/internal/io"
	"count_mean/internal/security/redact"
	"count_mean/internal/validation/filename"
)

// CCIParams 共同收縮分析參數.
type CCIParams struct {
	ManifestFile string `json:"manifestFile"`
	DataFolder   string `json:"dataFolder"`
	SubjectIndex int    `json:"subjectIndex"`
}

// CCIResult 共同收縮分析結果.
type CCIResult struct {
	OutputCSVPath    string             `json:"outputCSVPath"`
	OutputPhasesPath string             `json:"outputPhasesPath"`
	Subject          string             `json:"subject"`
	PairNames        []string           `json:"pairNames"`
	ChartHTML        string             `json:"chartHTML"`
	PhasePercents    map[string]float64 `json:"phasePercents"`
	PhaseTimes       map[string]float64 `json:"phaseTimes"`
	Report           string             `json:"report"`
	Success          bool               `json:"success"`
	Message          string             `json:"message"`
}

// CCIDownloadParams 共同收縮圖表下載參數.
type CCIDownloadParams struct {
	ImageData string `json:"imageData"`
	Subject   string `json:"subject"`
}

// AnalyzeCCI 執行 CCI Rudolph 共同收縮分析.
//
// 錯誤通道契約（Wave 3 Batch R）：
//
//	AnalyzeCCI 永遠回傳 non-nil *CCIResult；所有可預期失敗（參數驗證、路徑驗證、
//	下游分析錯誤、CSV/圖表生成失敗）都包成 result.Success=false + result.Message。
//	Go err 只在 `recoverHandlerPanic` 透過 named return 灌入 panic 時才為 non-nil
//	（不可預期錯誤）。如此前端可以單一路徑檢查 `result.success`/`result.message`，
//	不必同時 try/catch + 檢 result.success（之前的雙通道設計）。
func (a *App) AnalyzeCCI(params CCIParams) (result *CCIResult, err error) {
	// ⚠️ HandlerRun 之前不可有任何 a.logger.* 呼叫(維持 nil-logger panic 測試語意)
	return HandlerRun(a.logger, "CCI 分析", func() (*CCIResult, error) {
		a.logger.Info("CCI 分析參數", map[string]any{"params": params})
		s := a.state.Load()
		ctx := a.context()

		// 1 validate
		if vErr := validateManifestHandlerParams(params.ManifestFile, params.DataFolder); vErr != nil {
			return failedCCIResult(redact.RedactForMessage(vErr)), nil
		}
		// 2 execute（domain analyzer）
		analysisResult, aErr := a.cciAnalyzer.AnalyzeCCI(ctx, &cci.CCIParams{
			ManifestFile: params.ManifestFile, DataFolder: params.DataFolder, SubjectIndex: params.SubjectIndex,
		})
		if aErr != nil {
			return failedCCIResult(fmt.Sprintf("分析失敗: %s", redact.RedactForMessage(aErr))), nil
		}
		// 3 Output 1
		csvPath, e1 := s.csvHandler.WriteCCIResult(ctx, io.WriteRequest{}, analysisResult)
		if e1 != nil {
			return failedCCIResult(fmt.Sprintf("CSV 導出失敗: %s", redact.RedactForMessage(e1))), nil
		}
		// 4 chart
		var buf bytes.Buffer
		if cErr := cci.GenerateCCIInteractiveChart(ctx, analysisResult, &buf); cErr != nil {
			return failedCCIResult(fmt.Sprintf("圖表生成失敗: %s", redact.RedactForMessage(cErr))), nil
		}
		// 5 report + transform
		pairNames := make([]string, len(analysisResult.PairResults))
		for i, pr := range analysisResult.PairResults { pairNames[i] = pr.PairName }
		report := cci.GenerateReport(analysisResult)
		// 6 Output 2
		phasesPath, e2 := s.csvHandler.WriteCCIPhasesResult(ctx, io.WriteRequest{}, analysisResult)
		if e2 != nil {
			return failedCCIResult(fmt.Sprintf("分期統計導出失敗: %s", redact.RedactForMessage(e2))), nil
		}

		a.logger.Info("CCI 分析輸出", map[string]any{"csv": csvPath, "phases": phasesPath})
		return &CCIResult{
			OutputCSVPath: csvPath, OutputPhasesPath: phasesPath, Subject: analysisResult.Subject,
			PairNames: pairNames, ChartHTML: buf.String(),
			PhasePercents: analysisResult.PhasePercents, PhaseTimes: analysisResult.PhaseTimes,
			Report: report, Success: true, Message: "分析完成",
		}, nil
	})
}

// DownloadCCIChart 下載 CCI 圖表為 PNG 檔案.
//
// adapter 職責:從 params.Subject 經 SubjectOutputName 推導 OutputDir 內的固定
// 檔名({subject}_CCI_Rudolph.png,內部強制 Sanitize 防 traversal)。共用的 PNG 安全
// 管線（prefix 檢查 → decode/validate → boundary 路徑驗證 → WriteFileNoFollow）
// 已抽到 downloadValidatedPNG（ADR-0009）— 此 handler 只負責 adapter 邏輯與
// handler-level logging,不再內聯管線。
func (a *App) DownloadCCIChart(params CCIDownloadParams) (result *ChartResult, err error) {
	defer recoverHandlerPanic("DownloadCCIChart", a.logger, &err)

	a.logger.Info("開始下載 CCI 圖表", nil)

	// params.Subject 來自前端；sanitize 防路徑穿越（"../x" 之類）由 SubjectOutputName 內部強制。
	s := a.state.Load()
	outputPath := filepath.Join(
		s.config.OutputDir,
		filename.SubjectOutputName(params.Subject, "CCI_Rudolph")+".png",
	)

	result, err = a.downloadValidatedPNG(params.ImageData, outputPath)
	if err != nil {
		a.logger.Error("CCI 圖表下載失敗", err, map[string]any{
			"output": outputPath,
		})
		return nil, err
	}

	a.logger.Info("CCI 圖表下載完成", map[string]any{
		"output": outputPath,
	})

	return result, nil
}

// failedCCIResult returns a CCI result indicating failure.
func failedCCIResult(message string) *CCIResult {
	return &CCIResult{
		Success: false,
		Message: message,
	}
}
