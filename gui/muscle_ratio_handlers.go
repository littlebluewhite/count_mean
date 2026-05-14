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
// 失敗策略：
//   - 整體性錯誤（manifest 解析失敗、輸出目錄無法建立、subject 名稱衝突）→ 回 Go error
//   - 單一 subject 失敗（檔案不存在、缺通道、phase 時間越界等）→ 包進對應的 SubjectDTO.Error，
//     不阻斷其他 subject 處理；前端依 Subjects[i].Success 判斷是否該行成功
func (a *App) AnalyzeMuscleRatio(params MuscleRatioParams) (*MuscleRatioResult, error) {
	a.logger.Info("開始肌肉比值分析", map[string]interface{}{"params": params})

	if err := validateMuscleRatioParams(params); err != nil {
		return nil, err
	}

	s := a.state.Load()

	subjectResults, err := a.muscleRatioAnalyzer.Analyze(&muscle_ratio.Params{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		OutputDir:    s.config.OutputDir,
	})
	if err != nil {
		return failedMuscleRatioResult(i18n.T(i18n.KeyErrorMuscleRatioHandlerAnalysisFailed, err)), nil
	}

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

	a.logger.Info("肌肉比值分析完成", map[string]interface{}{
		"subjects": len(subjects),
		"success":  allSuccess,
	})

	return &MuscleRatioResult{
		Subjects: subjects,
		Success:  allSuccess,
		Message:  message,
	}, nil
}

// validateMuscleRatioParams checks required muscle-ratio parameters.
func validateMuscleRatioParams(params MuscleRatioParams) error {
	if params.ManifestFile == "" {
		return ErrNoManifestFile
	}

	if params.DataFolder == "" {
		return ErrNoDataFolder
	}

	return nil
}

// failedMuscleRatioResult builds a muscle-ratio result indicating overall failure.
func failedMuscleRatioResult(message string) *MuscleRatioResult {
	return &MuscleRatioResult{
		Success: false,
		Message: message,
	}
}
