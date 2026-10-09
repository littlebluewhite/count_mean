package gui

import (
	"fmt"

	"count_mean/internal/calculator"
	"count_mean/internal/i18n"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"count_mean/internal/synchronizer"
)

// PhaseSyncParams 分期同步分析參數.
type PhaseSyncParams struct {
	ManifestFile string            `json:"manifestFile"`
	DataFolder   string            `json:"dataFolder"`
	StartPhase   models.PhasePoint `json:"startPhase"`
	EndPhase     models.PhasePoint `json:"endPhase"`
	SubjectIndex int               `json:"subjectIndex"`
}

// PhaseSyncResult 分期同步分析結果.
type PhaseSyncResult struct {
	OutputPath   string             `json:"outputPath"`
	Subject      string             `json:"subject"`
	StartPhase   models.PhasePoint  `json:"startPhase"`
	StartTime    float64            `json:"startTime"`
	EndPhase     models.PhasePoint  `json:"endPhase"`
	EndTime      float64            `json:"endTime"`
	ChannelNames []string           `json:"channelNames"`
	ChannelMeans map[string]float64 `json:"channelMeans"`
	ChannelMaxes map[string]float64 `json:"channelMaxes"`
	Report       string             `json:"report"`
	Success      bool               `json:"success"`
	Message      string             `json:"message"`
}

// LoadPhaseManifest 載入分期總檔案的主題列表.
func (a *App) LoadPhaseManifest(manifestPath string) (subjects []string, err error) {
	defer recoverHandlerPanic("LoadPhaseManifest", a.logger, &err)

	a.logger.Info("載入分期總檔案", map[string]any{"path": manifestPath})

	// 邊界路徑驗證,擋掉 "../etc/passwd" 之類的 traversal / 系統敏感目錄。
	if err := validateExternalPathInputs("分期總檔案", manifestPath); err != nil {
		return nil, err
	}

	subjects, err = a.phaseSyncAnalyzer.LoadManifestSubjects(manifestPath)
	if err != nil {
		a.logger.Error("載入分期總檔案失敗", err, map[string]any{})
		return nil, fmt.Errorf("載入分期總檔案失敗: %w", err)
	}

	a.logger.Info("成功載入分期總檔案", map[string]any{"subjects": len(subjects)})

	return subjects, nil
}

// GetAvailablePhases 獲取可用的分期點列表。
//
// 回傳 []string 而非 []models.PhasePoint:Wails v2 TypeScript binding generator
// 不會為 named string type emit type alias,直接回 PhasePoint 會讓生成的 App.d.ts
// 引用未定義的 models.PhasePoint 型別,破壞前端 build。內部以 PhasePoint 計算後
// 在邊界 cast 為 string,前端 wire 結構零變動。
func (a *App) GetAvailablePhases() (out map[string][]string) {
	defer recoverHandlerPanicValue("GetAvailablePhases", a.logger, &out)

	return map[string][]string{
		"start": phasePointSliceToString(synchronizer.GetAvailableStartPhases()),
		"end":   phasePointSliceToString(synchronizer.GetAvailableEndPhases()),
	}
}

// phasePointSliceToString 在 Wails 邊界把 PhasePoint slice cast 為 string slice。
func phasePointSliceToString(phases []models.PhasePoint) []string {
	out := make([]string, len(phases))
	for i, p := range phases {
		out[i] = string(p)
	}

	return out
}

// AnalyzePhaseSync 執行分期同步分析.
//
// 錯誤通道契約 (dual / mixed):validate 失敗 → `(nil, err)` 走 err channel
// (path_validation_test 期待 traversal manifest / data folder 回 (nil, err));
// 分析 / 寫檔失敗 → `(failedResult, nil)` 走 failed-result channel;panic 由首句
// defer recoverHandlerPanic 轉成 ErrInternalPanic → `(nil, err)` 走 err channel。
func (a *App) AnalyzePhaseSync(params PhaseSyncParams) (result *PhaseSyncResult, err error) {
	defer recoverHandlerPanic("分期同步分析", a.logger, &err)

	s := a.state.Load()
	a.logger.Info("分期同步分析參數", map[string]any{"params": params})
	a.logger.Info("開始分期同步分析", nil)

	// 1 validate → err channel
	if validateErr := validateManifestHandlerParams(params.ManifestFile, params.DataFolder); validateErr != nil {
		return nil, validateErr
	}
	if params.StartPhase == "" || params.EndPhase == "" {
		return nil, ErrNoPhaseSelection
	}
	if !params.StartPhase.IsValid() || !params.EndPhase.IsValid() {
		return nil, fmt.Errorf(
			"StartPhase=%q EndPhase=%q: %w",
			params.StartPhase, params.EndPhase, ErrInvalidPhasePoint,
		)
	}

	// 2 分析(domain analyzer)→ failed-result channel
	stats, analyzeErr := a.phaseSyncAnalyzer.AnalyzePhaseSync(a.context(), &models.AnalysisParams{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		StartPhase:   params.StartPhase,
		EndPhase:     params.EndPhase,
		SubjectIndex: params.SubjectIndex,
	})
	if analyzeErr != nil {
		return failedPhaseSyncResult(a.failMessage(i18n.KeyErrorHandlerAnalysisFailed, analyzeErr)), nil
	}

	// 3 導出結果 → failed-result channel。ADR-0001: 寫檔職責由 PhaseSyncAnalyzer
	// 搬到 CSVHandler,走同一條 format-aware write 路徑。
	outputPath, writeErr := s.csvHandler.WritePhaseSyncResult(io.WriteRequest{}, stats)
	if writeErr != nil {
		return failedPhaseSyncResult(a.failMessage(i18n.KeyErrorHandlerExportFailed, writeErr)), nil
	}

	a.logger.Info("分期同步分析完成", nil)

	// 生成報告
	report := calculator.FormatStatisticsReport(stats)

	a.logger.Info("分期同步分析輸出", map[string]any{"outputPath": outputPath})

	return &PhaseSyncResult{
		OutputPath:   outputPath,
		Subject:      stats.Subject,
		StartPhase:   stats.StartPhase,
		StartTime:    stats.StartTime,
		EndPhase:     stats.EndPhase,
		EndTime:      stats.EndTime,
		ChannelNames: stats.ChannelNames,
		ChannelMeans: stats.ChannelMeans,
		ChannelMaxes: stats.ChannelMaxes,
		Report:       report,
		Success:      true,
		Message:      "分析完成",
	}, nil
}

// failedPhaseSyncResult builds a phase-sync result indicating failure
// (分析 / 寫檔分支;validate 分支走 err 通道)。
func failedPhaseSyncResult(message string) *PhaseSyncResult {
	return &PhaseSyncResult{
		Success: false,
		Message: message,
	}
}
