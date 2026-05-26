package gui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"count_mean/internal/calculator"
	"count_mean/internal/cci"
	"count_mean/internal/io"
	"count_mean/internal/security/fsperm"
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
	a.logger.Info("CCI 分析參數", map[string]interface{}{"params": params})

	s := a.state.Load()
	outputDir := s.config.OutputDir

	handler := &AnalysisHandler[CCIParams, *cci.CCIAnalysisResult]{
		Name:   "CCI 分析",
		Logger: a.logger,
		CSV:    s.csvHandler,
		Validate: func(p CCIParams) error {
			if validationErr := validateCCIParams(p); validationErr != nil {
				return validationErr
			}
			// 邊界路徑驗證 — 在 analyzer 內部 defense-in-depth 之前先擋
			// traversal / 系統敏感目錄。downstream cci.Analyzer 仍會二次擋，
			// 但 GUI 邊界提早 reject 對前端 UX 與 audit 較友善（不會等中途
			// 某個 reader 才失敗）。
			return validateExternalPathInputs(
				"分期總檔案", p.ManifestFile,
				"資料夾", p.DataFolder,
			)
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
				return nil, fmt.Errorf("分析失敗: %s", redact.RedactForMessage(analyzeErr))
			}
			return analysisResult, nil
		},
		// WriteCSV 暫保留 cciAnalyzer.ExportToCSV — Candidate 2 推進時把 closure
		// 內容換成 csvHandler.WriteCCIResult(...) 即可，handler 本身不再動。
		// `*io.CSVHandler` 參數此版本還用不到（走 cciAnalyzer 內部 export），
		// 是 Candidate 2 前的暫保留設計。outputDir 透過 closure capture caller
		// 端的 local var（state.Load 已在 Run 外做）。
		WriteCSV: func(_ *io.CSVHandler, analysisResult *cci.CCIAnalysisResult) (string, error) {
			// 帶 ctx 讓大 dataset CSV 匯出也能配合 Wails Shutdown / 使用者中止
			// 取消;closure 內走 a.context() 與 Execute 一致。
			csvPath, exportErr := a.cciAnalyzer.ExportToCSV(a.context(), analysisResult, outputDir)
			if exportErr != nil {
				return "", fmt.Errorf("CSV 導出失敗: %s", redact.RedactForMessage(exportErr))
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

	a.logger.Info("CCI 分析輸出", map[string]interface{}{"csv": csvPath})

	return result, nil
}

// DownloadCCIChart 下載 CCI 圖表為 PNG 檔案.
func (a *App) DownloadCCIChart(params CCIDownloadParams) (result *ChartResult, err error) {
	defer recoverHandlerPanic("DownloadCCIChart", a.logger, &err)

	a.logger.Info("開始下載 CCI 圖表", nil)

	dataURL := params.ImageData
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		return nil, ErrInvalidImageFormat
	}

	base64Data := strings.TrimPrefix(dataURL, "data:image/png;base64,")

	// base64 decode 前先擋 size,decode 後驗 PNG signature + IHDR
	// dimension。詳見 gui/png_validation.go DecodeAndValidatePNG。
	pngData, err := DecodeAndValidatePNG(base64Data)
	if err != nil {
		return nil, fmt.Errorf("PNG 驗證失敗: %w", err)
	}

	// params.Subject 來自前端，需先 sanitize 避免路徑穿越（"../x" 之類）。
	safeSubject := calculator.SanitizeFileName(params.Subject)
	s := a.state.Load()
	outputPath := filepath.Join(
		s.config.OutputDir,
		fmt.Sprintf("%s_CCI_Rudolph.png", safeSubject),
	)

	// M23(P2)：boundary 路徑驗證 — 對齊 AnalyzeCCI / AnalyzeMuscleRatio /
	// AnalyzeNormalizedPhaseSync 的 defense-in-depth 模式。
	//
	// 為何需要:雖然 OutputDir 來自 config 而非前端直接控,但 config 可能
	// 被惡意修改(直接編輯 config.json 或舊版 bug 讓 traversal 寫進去),
	// 而 SanitizeFileName 對 Subject 已做基本處理仍不能保證最終 outputPath
	// 不落到系統敏感目錄。fsperm.WriteFileNoFollow 雖會擋 symlink follow,
	// 但 boundary 提早 reject 對 audit log 與錯誤訊息都更友善。
	//
	// 為何擋的是組合後的 outputPath:外部攻擊面其實是「OutputDir + Subject」
	// 的合成結果,單獨驗 OutputDir 不能完全覆蓋(Subject 雖經 sanitize 仍
	// 可能拼出意外路徑),驗合成後路徑是 strongest invariant。
	//
	// label 直接帶 "PNG 輸出路徑" 一次到位,validateExternalPathInputs 已會
	// 在後面附上「路徑驗證失敗: <reason>」。原本還在 caller 端再 wrap 一層
	// fmt.Errorf("PNG 輸出 %w", pathErr) 會產生重複 label
	// 「PNG 輸出 輸出路徑 路徑驗證失敗: ...」— 同樣資訊出現兩次,使用者看了
	// 還會以為是兩個獨立步驟都失敗。集中在 label 參數,wrap chain 只一次。
	if pathErr := validateExternalPathInputs("PNG 輸出路徑", outputPath); pathErr != nil {
		return nil, pathErr
	}

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
