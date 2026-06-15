package gui

import (
	"context"
	"errors"

	"count_mean/internal/i18n"
	"count_mean/internal/muscle_ratio"
	"count_mean/internal/security/redact"
)

// MuscleRatioParams 是肌肉比值分析的前端參數（兩步驟：選 manifest + 選資料夾）。
type MuscleRatioParams struct {
	ManifestFile string `json:"manifestFile"`
	DataFolder   string `json:"dataFolder"`
}

// MuscleRatioSubjectDTO 是單一 subject 結果的 JSON 傳輸物件。
//
// DurationMs：per-subject 耗時（millisecond），供前端表格顯示 diagnostic 資訊
// （哪個 subject 拖慢整批、哪個 subject 提早 fail）。
type MuscleRatioSubjectDTO struct {
	Subject         string `json:"subject"`
	OutputAllPath   string `json:"outputAllPath"`
	OutputPhasePath string `json:"outputPhasePath"`
	Success         bool   `json:"success"`
	Error           string `json:"error"`
	DurationMs      int64  `json:"durationMs"`
}

// MuscleRatioResult 是肌肉比值分析的整體回傳。
//
// 三種狀態語意（與 cci / normalized_phase_sync peer 對稱）：
//
//  1. Pre-analysis fail（manifest 解析失敗、OutputDir 驗證失敗、subject 衝突等）：
//     Success=false, Subjects=nil, Message=描述失敗原因。
//     前端依此呈現「整批未啟動」。Go error 返回 nil（避免雙通道 error 與 message 重複）。
//
//  2. Partial fail（部分 subject 失敗、部分成功）：
//     Success=false, Subjects 為 non-nil 含 per-subject 結果（Subjects[i].Success
//     標記個別狀態）。Message 含「部分主題未完成」。前端 iterate Subjects 顯示每行。
//
//  3. All success：
//     Success=true, Subjects 為 non-nil 全部 Subjects[i].Success=true，
//     Message="已處理 N 個主題"。
//
// 注意：Subjects==nil 永遠代表狀態 (1)；caller 必須先檢 Success 與 Subjects 是否
// nil 再讀 Subjects 元素，否則 nil-deref。
type MuscleRatioResult struct {
	Subjects []MuscleRatioSubjectDTO `json:"subjects"`
	Success  bool                    `json:"success"`
	Message  string                  `json:"message"`
}

// AnalyzeMuscleRatio 批次計算 manifest 內所有 subject 的肌肉比值，每 subject 產出兩個 CSV。
//
// 錯誤通道契約（Wave 3 Batch R）：
//
//	AnalyzeMuscleRatio 永遠回傳 non-nil *MuscleRatioResult；所有可預期失敗
//	（參數驗證、路徑驗證、整體 analyze 失敗）都包成 result.Success=false + result.Message
//	（Subjects 為 nil 代表整批未啟動）。partial fail 仍由 Subjects[i].Success 表達。
//	Go err 只在 `recoverHandlerPanic` 透過 named return 灌入 panic 時才為 non-nil。
//	前端因此可以單一路徑檢查 result.success / result.subjects。
//
// 失敗策略：
//   - 整體性錯誤（參數驗證失敗、manifest 解析失敗、輸出目錄無法建立、subject 名稱衝突）
//     → result.Success=false, result.Subjects=nil, result.Message=描述失敗原因
//   - 單一 subject 失敗（檔案不存在、缺通道、phase 時間越界等）→ 包進對應的 SubjectDTO.Error，
//     不阻斷其他 subject 處理；前端依 Subjects[i].Success 判斷是否該行成功
func (a *App) AnalyzeMuscleRatio(params MuscleRatioParams) (result *MuscleRatioResult, err error) {
	a.logger.Info("肌肉比值分析參數", map[string]any{"params": params})

	s := a.state.Load()
	outputDir := s.config.OutputDir

	handler := &AnalysisHandler[MuscleRatioParams, []muscle_ratio.SubjectResult]{
		Name:   "肌肉比值分析",
		Logger: a.logger,
		CSV:    s.csvHandler,
		Validate: func(p MuscleRatioParams) error {
			return validateManifestHandlerParams(p.ManifestFile, p.DataFolder)
		},
		Execute: func(ctx context.Context, p MuscleRatioParams) ([]muscle_ratio.SubjectResult, error) {
			// ctx 由樣板注入,沿用原 a.context() 行為:支援 Wails Shutdown /
			// 使用者中止取消批次掃描。Analyzer 內 subject 迴圈會 poll
			// ctx.Done(),Shutdown 時批次立即停,partial results 仍會被回。
			subjectResults, analyzeErr := a.muscleRatioAnalyzer.Analyze(ctx, &muscle_ratio.Params{
				ManifestFile: p.ManifestFile,
				DataFolder:   p.DataFolder,
				OutputDir:    outputDir,
				CSVHandler:   s.csvHandler,
			})
			if analyzeErr != nil {
				// error 字面可能含 absolute path(downstream parser 用 %w wrap),
				// 前端 user-visible message 必須先過 redact 把 system-root prefix
				// 砍掉,並走 i18n.T 取得 locale 化的「分析失敗」prefix(對齊
				// TestAnalyzeMuscleRatio_LocaleSwitchAffectsMessage 對 zh-TW /
				// zh-CN / en-US / ja-JP 各 locale 的 catalog 翻譯期望)。
				// UIError 包裝:Error() 仍回 i18n 翻譯訊息(維持 result.Message
				// 契約),sentinel 對接 errors.Is(err, ErrMuscleRatioAnalysisFailed)。
				return nil, newUIError(ErrMuscleRatioAnalysisFailed,
					i18n.T(i18n.KeyErrorMuscleRatioHandlerAnalysisFailed, redact.RedactForMessage(analyzeErr)))
			}
			return subjectResults, nil
		},
		// WriteCSV = nil:batch unit-of-work 不適用 single-path closure (ADR-0004 Boundary 3)。
		// per-subject row layout 已透過 muscle_ratio.Analyzer 內呼叫 csvHandler.WriteMuscleRatioOutputAll
		// 與 WriteMuscleRatioOutputPhases 落實;outputPath 由 analyzer 回填到
		// SubjectResult.OutputAllPath / OutputPhasePath。
		// 詳見 docs/adr/0004-format-aware-write-collapse-boundaries.md。
		WriteCSV: nil,
	}

	subjectResults, _, runErr := handler.Run(a.context(), params)
	if runErr != nil {
		// panic 路徑:HandlerRun 的 recoverHandlerPanic 已將 panic 包成
		// ErrInternalPanic chain;err 走 named return 上拋,result=nil(對齊
		// 原 single-channel 契約對 panic 的處理 — Subjects=nil 永遠代表
		// pre-analysis fail 整批未啟動)。
		if errors.Is(runErr, ErrInternalPanic) {
			return nil, runErr
		}
		// expected err(Validate / Execute 任一回的 err)走 single-channel
		// envelope:failedMuscleRatioResult(message) + nil err。Execute closure
		// 已內含 i18n + redact wrap,Validate 失敗則回原 sentinel(對齊既有
		// TestAnalyzeMuscleRatio_EmptyParamsUnifiedChannel 對 ErrNoManifestFile /
		// ErrNoDataFolder.Error() 的字面期望)。redact 對 path-free sentinel 不變,
		// 防未來帶路徑的 Validate error 洩漏 PHI。
		return failedMuscleRatioResult(redact.RedactForMessage(runErr)), nil
	}

	// Result transform — Run 外:Subjects DTO 組裝 + allSuccess 判斷 + i18n status
	// message 組裝。對應 PRD「Caller 在 Run 外仍處理的事」(Subjects DTO /
	// allSuccess / KeyStatusMuscleRatioProcessedCount + PartialWarning)。
	subjects := make([]MuscleRatioSubjectDTO, 0, len(subjectResults))
	allSuccess := true

	for _, sr := range subjectResults {
		if !sr.Success {
			allSuccess = false
		}

		subjects = append(subjects, MuscleRatioSubjectDTO{
			Subject:         sr.Subject,
			OutputAllPath:   sr.OutputAllPath,
			OutputPhasePath: sr.OutputPhasePath,
			Success:         sr.Success,
			Error:           sr.Error,
			DurationMs:      sr.DurationMs,
		})
	}

	message := i18n.T(i18n.KeyStatusMuscleRatioProcessedCount, len(subjects))
	if !allSuccess {
		message += i18n.T(i18n.KeyStatusMuscleRatioPartialWarning)
	}

	a.logger.Info("肌肉比值分析輸出", map[string]any{
		"subjects": len(subjects),
		"success":  allSuccess,
	})

	return &MuscleRatioResult{
		Subjects: subjects,
		Success:  allSuccess,
		Message:  message,
	}, nil
}

// failedMuscleRatioResult builds a muscle-ratio result indicating overall failure.
func failedMuscleRatioResult(message string) *MuscleRatioResult {
	return &MuscleRatioResult{
		Success: false,
		Message: message,
	}
}
