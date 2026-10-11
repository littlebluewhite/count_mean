package gui

import (
	"fmt"

	"count_mean/internal/i18n"
	"count_mean/internal/manifest"
)

// ManifestSubjectsResult Wails RPC result:subject 下拉選單的名稱清單(ADR-0057)。
type ManifestSubjectsResult struct {
	Subjects []string `json:"subjects"`
	Success  bool     `json:"success"`
	Message  string   `json:"message"`
}

// failedManifestSubjectsResult builds a subject-list result indicating failure.
func failedManifestSubjectsResult(message string) *ManifestSubjectsResult {
	return &ManifestSubjectsResult{
		Success: false,
		Message: message,
	}
}

// LoadManifestSubjects 回 [[Manifest]] 內 [[Subject]] 的名稱清單,給 CCI / PhaseSync /
// NormalizedPhaseSync / [[Chart Composer]] 四個 panel 的 subject 下拉共用(ADR-0057)。
// 清單來自 manifest.LoadSubjectNames:非空 Subject、manifest 順序;選定的名稱原樣送回各分析
// handler,由 manifest.LoadSubject 取 row。只讀分期總檔案,不吃資料夾、不開資料檔。
//
// 錯誤通道與 manifest handler 家族相同(ADR-0051):可預期失敗回 failed result + nil err,
// err 只在 panic 時由首句 defer recoverHandlerPanic 灌入。
func (a *App) LoadManifestSubjects(manifestPath string) (result *ManifestSubjectsResult, err error) {
	defer recoverHandlerPanic("載入主題", a.logger, &err)

	a.logger.Info("開始載入主題", map[string]any{"path": manifestPath})

	if manifestPath == "" {
		return failedManifestSubjectsResult(inputMessage(ErrNoManifestFile)), nil
	}
	if vErr := validateExternalPathInputs("分期總檔案", manifestPath); vErr != nil {
		return failedManifestSubjectsResult(inputMessage(vErr)), nil
	}

	subjects, loadErr := manifest.LoadSubjectNames(manifestPath)
	if loadErr != nil {
		return failedManifestSubjectsResult(a.failMessage(i18n.KeyErrorHandlerLoadManifestFailed, loadErr)), nil
	}

	a.logger.Info("載入主題完成", map[string]any{"subjects": len(subjects)})

	return &ManifestSubjectsResult{
		Subjects: subjects,
		Success:  true,
		Message:  fmt.Sprintf("已載入 %d 個主題", len(subjects)),
	}, nil
}
