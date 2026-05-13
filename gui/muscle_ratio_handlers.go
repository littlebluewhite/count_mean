package gui

import (
	"fmt"

	"count_mean/internal/muscle_ratio"
)

// MuscleRatioParams 是肌肉比值分析的前端參數（兩步驟：選 manifest + 選資料夾）。
type MuscleRatioParams struct {
	ManifestFile string `json:"manifestFile"`
	DataFolder   string `json:"dataFolder"`
}

// MuscleRatioSubjectDTO 是單一 subject 結果的 JSON 傳輸物件。
type MuscleRatioSubjectDTO struct {
	Subject         string `json:"subject"`
	OutputAllPath   string `json:"outputAllPath"`
	OutputPhasePath string `json:"outputPhasePath"`
	Success         bool   `json:"success"`
	Error           string `json:"error"`
}

// MuscleRatioResult 是肌肉比值分析的整體回傳。
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

	analysisResult, err := a.muscleRatioAnalyzer.Analyze(&muscle_ratio.Params{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		OutputDir:    s.config.OutputDir,
	})
	if err != nil {
		return failedMuscleRatioResult(fmt.Sprintf("分析失敗: %v", err)), nil
	}

	subjects := make([]MuscleRatioSubjectDTO, 0, len(analysisResult.Subjects))
	allSuccess := true

	for _, sr := range analysisResult.Subjects {
		if !sr.Success {
			allSuccess = false
		}

		subjects = append(subjects, MuscleRatioSubjectDTO{
			Subject:         sr.Subject,
			OutputAllPath:   sr.OutputAllPath,
			OutputPhasePath: sr.OutputPhasePath,
			Success:         sr.Success,
			Error:           sr.Error,
		})
	}

	message := fmt.Sprintf("已處理 %d 個主題", len(subjects))
	if !allSuccess {
		message += "（部分主題未完成，請查看各 row 的錯誤訊息）"
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
