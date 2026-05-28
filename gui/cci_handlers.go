package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"count_mean/internal/calculator"
	"count_mean/internal/cci"
	"count_mean/internal/io"
	"count_mean/internal/security/redact"
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
//
// 錯誤通道契約（Wave 3 Batch R）：
//
//	AnalyzeCCI 永遠回傳 non-nil *CCIResult；所有可預期失敗（參數驗證、路徑驗證、
//	下游分析錯誤、CSV/圖表生成失敗）都包成 result.Success=false + result.Message。
//	Go err 只在 `recoverHandlerPanic` 透過 named return 灌入 panic 時才為 non-nil
//	（不可預期錯誤）。如此前端可以單一路徑檢查 `result.success`/`result.message`，
//	不必同時 try/catch + 檢 result.success（之前的雙通道設計）。
func (a *App) AnalyzeCCI(params CCIParams) (result *CCIResult, err error) {
	a.logger.Info("CCI 分析參數", map[string]any{"params": params})

	s := a.state.Load()

	handler := &AnalysisHandler[CCIParams, *cci.CCIAnalysisResult]{
		Name:   "CCI 分析",
		Logger: a.logger,
		CSV:    s.csvHandler,
		Validate: func(p CCIParams) error {
			return validateManifestHandlerParams(p.ManifestFile, p.DataFolder)
		},
		Execute: func(ctx context.Context, p CCIParams) (*cci.CCIAnalysisResult, error) {
			analysisParams := &cci.CCIParams{
				ManifestFile: p.ManifestFile,
				DataFolder:   p.DataFolder,
				SubjectIndex: p.SubjectIndex,
			}
			// ctx 由樣板注入,沿用原 a.context() 行為:支援 Shutdown / 使用者
			// 中止取消長 CCI 計算（12 個 pair × N 點的並行 hot loop）。
			analysisResult, analyzeErr := a.cciAnalyzer.AnalyzeCCI(ctx, analysisParams)
			if analyzeErr != nil {
				// error message 內可能含 absolute path(downstream parser 把
				// PathError 用 %w wrap 傳上來),前端不該看到
				// /Volumes/patient_xx/... 之類 PII。走 redact.RedactForMessage
				// 先處理再塞 result.Message。
				// UIError 包裝:Error() 仍回中文訊息(維持 result.Message 契約),
				// sentinel 對接 errors.Is(err, ErrCCIAnalysisFailed)。
				return nil, newUIError(ErrCCIAnalysisFailed,
					fmt.Sprintf("分析失敗: %s", redact.RedactForMessage(analyzeErr)))
			}
			return analysisResult, nil
		},
		// WriteCSV: ADR-0004 Boundary 2 — Subject-based write,CSVHandler 內部從
		// analysisResult.Subject 推導 filename;req.Filename 被忽略。SubDir 用 ""
		// (寫到 OutputDir 根)。outputDir capture 由 CSVHandler 自身的 h.config.OutputDir
		// 替代(state.Load 在 Run 外做時 csvHandler 已綁定當時 config)。
		WriteCSV: func(handler *io.CSVHandler, analysisResult *cci.CCIAnalysisResult) (string, error) {
			csvPath, exportErr := handler.WriteCCIResult(a.context(), io.WriteRequest{}, analysisResult)
			if exportErr != nil {
				// UIError 包裝:Error() 仍回中文訊息(維持 result.Message 契約),
				// sentinel 對接 errors.Is(err, ErrCCICSVExportFailed)。
				return "", newUIError(ErrCCICSVExportFailed,
					fmt.Sprintf("CSV 導出失敗: %s", redact.RedactForMessage(exportErr)))
			}
			return csvPath, nil
		},
	}

	analysisResult, csvPath, runErr := handler.Run(a.context(), params)
	if runErr != nil {
		// panic 路徑:HandlerRun 的 recoverHandlerPanic 已將 panic 包成
		// ErrInternalPanic chain；err 走 named return 上拋，result=nil（對齊
		// 原 single-channel 契約對 panic 的處理）。
		if errors.Is(runErr, ErrInternalPanic) {
			return nil, runErr
		}
		// expected err（Validate / Execute / WriteCSV 任一回的 err）走
		// single-channel envelope:failedCCIResult(message) + nil err。
		// Execute / WriteCSV closure 內已自行加 prefix + redact，Validate
		// 失敗則回原 sentinel（對齊既有 unified-channel 測試契約）。
		return failedCCIResult(runErr.Error()), nil
	}

	// Generate interactive chart HTML — Run 外:Execute 維持單一語意，render
	// 不擠進 closure。帶 ctx 讓大 dataset render 也能配合 Wails Shutdown /
	// 使用者中止取消。
	var buf bytes.Buffer
	if chartErr := cci.GenerateCCIInteractiveChart(a.context(), analysisResult, &buf); chartErr != nil {
		return failedCCIResult(fmt.Sprintf("圖表生成失敗: %s", redact.RedactForMessage(chartErr))), nil
	}

	// Result transform — Run 外:PairResults → PairNames slice、Report 字串
	// 各自留在 caller code 維持可讀性。
	pairNames := make([]string, len(analysisResult.PairResults))
	for i, pr := range analysisResult.PairResults {
		pairNames[i] = pr.PairName
	}

	report := cci.GenerateReport(analysisResult)

	result = &CCIResult{
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

	a.logger.Info("CCI 分析輸出", map[string]any{"csv": csvPath})

	return result, nil
}

// DownloadCCIChart 下載 CCI 圖表為 PNG 檔案.
//
// adapter 職責:從 params.Subject 推導 OutputDir 內的固定檔名
// ({safeSubject}_CCI_Rudolph.png),sanitize 防 traversal。共用的 PNG 安全
// 管線（prefix 檢查 → decode/validate → boundary 路徑驗證 → WriteFileNoFollow）
// 已抽到 downloadValidatedPNG（ADR-0009）— 此 handler 只負責 adapter 邏輯與
// handler-level logging,不再內聯管線。
func (a *App) DownloadCCIChart(params CCIDownloadParams) (result *ChartResult, err error) {
	defer recoverHandlerPanic("DownloadCCIChart", a.logger, &err)

	a.logger.Info("開始下載 CCI 圖表", nil)

	// params.Subject 來自前端，需先 sanitize 避免路徑穿越（"../x" 之類）。
	safeSubject := calculator.SanitizeFileName(params.Subject)
	s := a.state.Load()
	outputPath := filepath.Join(
		s.config.OutputDir,
		fmt.Sprintf("%s_CCI_Rudolph.png", safeSubject),
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
