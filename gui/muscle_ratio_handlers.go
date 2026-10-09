package gui

import (
	"count_mean/internal/i18n"
	"count_mean/internal/muscle_ratio"
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
	defer recoverHandlerPanic("肌肉比值分析", a.logger, &err)

	a.logger.Info("肌肉比值分析參數", map[string]any{"params": params})

	s := a.state.Load()

	a.logger.Info("開始肌肉比值分析", nil)

	// 1 validate:失敗回原 sentinel 文字(對齊 TestAnalyzeMuscleRatio_EmptyParamsUnifiedChannel
	// 對 ErrNoManifestFile / ErrNoDataFolder.Error() 的字面期望)。
	if validateErr := validateManifestHandlerParams(params.ManifestFile, params.DataFolder); validateErr != nil {
		return failedMuscleRatioResult(inputMessage(validateErr)), nil
	}

	// 2 analyze:batch unit-of-work,per-subject 兩個 CSV 由 muscle_ratio.Analyzer 內呼叫
	// csvHandler.WriteMuscleRatioOutputAll / WriteMuscleRatioOutputPhases 寫出
	// (ADR-0012 compute+write),outputPath 回填到 SubjectResult.OutputAllPath / OutputPhasePath。
	// ctx 支援 Wails Shutdown / 使用者中止:Analyzer 內 subject 迴圈會 poll ctx.Done(),
	// Shutdown 時批次立即停,partial results 仍會被回。
	subjectResults, analyzeErr := a.muscleRatioAnalyzer.Analyze(a.context(), &muscle_ratio.Params{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		OutputDir:    s.config.OutputDir,
		CSVHandler:   s.csvHandler,
	})
	if analyzeErr != nil {
		return failedMuscleRatioResult(a.failMessage(i18n.KeyErrorHandlerAnalysisFailed, analyzeErr)), nil
	}

	a.logger.Info("肌肉比值分析完成", nil)

	// 3 result transform:Subjects DTO 組裝 + allSuccess 判斷 + i18n status message 組裝
	// (KeyStatusMuscleRatioProcessedCount + PartialWarning)。
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
			Error:           redactText(sr.Error), // analyzer 的逐 subject 錯誤字串常帶 EMG 檔絕對路徑
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
