package gui

import (
	"context"
	"errors"

	"count_mean/internal/calculator"
	"count_mean/internal/i18n"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"count_mean/internal/phase_sync"
)

// NormalizedPhaseSyncParams 標準化分期同步分析參數。
//
// 標準化視窗（Norm*）與統計視窗（Stats*）為兩組獨立的分期區間：
//   - Norm 視窗用於計算每條肌肉的最大值（標準化的除數）
//   - Stats 視窗用於擷取最終要輸出統計的時間範圍
//
// 兩組區間可以重疊或完全分離，由使用者自行選擇；後端不檢查互相關係。
type NormalizedPhaseSyncParams struct {
	ManifestFile    string            `json:"manifestFile"`
	DataFolder      string            `json:"dataFolder"`
	SubjectIndex    int               `json:"subjectIndex"`
	NormStartPhase  models.PhasePoint `json:"normStartPhase"`
	NormEndPhase    models.PhasePoint `json:"normEndPhase"`
	StatsStartPhase models.PhasePoint `json:"statsStartPhase"`
	StatsEndPhase   models.PhasePoint `json:"statsEndPhase"`
}

// NormalizedPhaseSyncResult 標準化分期同步分析結果。
//
// Norm* 與 Stats* 分別反映標準化視窗與統計視窗的分期點與實際時間，
// 兩組獨立顯示供 UI 區分。
type NormalizedPhaseSyncResult struct {
	NormalizedEMGPath string             `json:"normalizedEMGPath"`
	PhaseSyncCSVPath  string             `json:"phaseSyncCSVPath"`
	Subject           string             `json:"subject"`
	NormStartPhase    models.PhasePoint  `json:"normStartPhase"`
	NormEndPhase      models.PhasePoint  `json:"normEndPhase"`
	NormStartTime     float64            `json:"normStartTime"`
	NormEndTime       float64            `json:"normEndTime"`
	StatsStartPhase   models.PhasePoint  `json:"statsStartPhase"`
	StatsEndPhase     models.PhasePoint  `json:"statsEndPhase"`
	StatsStartTime    float64            `json:"statsStartTime"`
	StatsEndTime      float64            `json:"statsEndTime"`
	ChannelNames      []string           `json:"channelNames"`
	ChannelMaxes      map[string]float64 `json:"channelMaxes"` // 標準化前的最大值（供 UI 顯示）
	ChannelMeans      map[string]float64 `json:"channelMeans"` // 標準化後區間平均
	Report            string             `json:"report"`
	Success           bool               `json:"success"`
	Message           string             `json:"message"`
}

// AnalyzeNormalizedPhaseSync 執行「先標準化、再分期同步分析」的組合工作流。計算由
// phase_sync.AnalyzeNormalizedPhaseSync 持有(ADR-0047);這裡只剩 adapter:
//
//  1. 驗證必填欄位
//  2. phase_sync.AnalyzeNormalizedPhaseSync:載入、兩組區間、標準化、統計全部算完
//  3. 撰寫 Output 1:{subject}_normalized.csv
//  4. 撰寫 Output 2:
//     {subject}_normalized_norm-{normStart}-{normEnd}_stats-{statsStart}-{statsEnd}.csv
//     （欄位與既有「分期同步分析」相同；檔名同時帶兩組分期點以避免不同設定的輸出混淆）
//  5. envelope:分析失敗依 phase_sync.Stage 選 i18n 前綴(normalizedPhaseSyncFailKey)
//
// 分析任一步失敗都不寫任何輸出(兩個寫入都在分析成功之後)。
//
// 錯誤通道契約（Wave 3 Batch R）：
//
//	AnalyzeNormalizedPhaseSync 永遠回傳 non-nil *NormalizedPhaseSyncResult；
//	所有可預期失敗（參數驗證、路徑驗證、分析、檔案寫入）都包成
//	result.Success=false + result.Message。Go err 只在
//	`recoverHandlerPanic` 透過 named return 灌入 panic 時才為 non-nil。
//	前端因此可以單一路徑檢查 result.success / result.message。
func (a *App) AnalyzeNormalizedPhaseSync(params NormalizedPhaseSyncParams) (result *NormalizedPhaseSyncResult, err error) {
	defer recoverHandlerPanic("標準化分期同步分析", a.logger, &err)

	a.logger.Info("標準化分期同步分析參數", map[string]any{"params": params})
	a.logger.Info("開始標準化分期同步分析", nil)
	// exit log 只在正常返回時打:單一通道下正常返回的 result 必 non-nil,panic
	// 路徑 result 仍為 nil。
	defer func() {
		if result != nil {
			a.logger.Info("標準化分期同步分析完成", nil)
		}
	}()

	s := a.state.Load()

	// 抓取 Wails lifecycle ctx,讓 Shutdown / 使用者中止可在分析的 step 之間
	// (phase_sync 內)與兩次寫檔之前早停。
	ctx := a.context()

	if validationErr := validateNormalizedPhaseSyncParams(params); validationErr != nil {
		return failedNormalizedPhaseSyncResult(inputMessage(validationErr)), nil
	}

	analysis, analyzeErr := a.phaseSyncAnalyzer.AnalyzeNormalizedPhaseSync(ctx, &phase_sync.NormalizedParams{
		ManifestFile:    params.ManifestFile,
		DataFolder:      params.DataFolder,
		SubjectIndex:    params.SubjectIndex,
		NormStartPhase:  params.NormStartPhase,
		NormEndPhase:    params.NormEndPhase,
		StatsStartPhase: params.StatsStartPhase,
		StatsEndPhase:   params.StatsEndPhase,
	})
	if analyzeErr != nil {
		return failedNormalizedPhaseSyncResult(a.failMessage(normalizedPhaseSyncFailKey(analyzeErr), analyzeErr)), nil
	}

	stats := analysis.Stats

	// Output 1:標準化後的 EMG CSV。CSVHandler.WriteNormalizedPhaseSyncEMG 持有
	// Sanitize + 路徑拼接 + boundary validation + atomic write，呼叫方只需傳 subject。
	// 寫檔前檢查 ctx,讓 Shutdown 能及時 cancel。
	if ctxErr := ctx.Err(); ctxErr != nil {
		return failedNormalizedPhaseSyncResult(a.failMessage(i18n.KeyErrorHandlerCancelled, ctxErr)), nil
	}

	normalizedEMGPath, csvWriteErr := s.csvHandler.WriteNormalizedPhaseSyncEMG(io.WriteRequest{}, analysis.NormalizedEMG, analysis.Subject)
	if csvWriteErr != nil {
		return failedNormalizedPhaseSyncResult(a.failMessage(i18n.KeyErrorHandlerWriteNormalizedEMGFailed, csvWriteErr)), nil
	}

	// Output 2：Subject-based atomic write;filename + 路徑守門由 CSVHandler 持有。
	// 第二輪 ctx 檢查,寫檔前再給一次 cancel 機會。
	if ctxErr := ctx.Err(); ctxErr != nil {
		return failedNormalizedPhaseSyncResult(a.failMessage(i18n.KeyErrorHandlerCancelled, ctxErr)), nil
	}

	phaseSyncCSVPath, statsWriteErr := s.csvHandler.WriteNormalizedPhaseSyncResult(
		io.WriteRequest{}, stats, params.NormStartPhase, params.NormEndPhase,
	)
	if statsWriteErr != nil {
		// 對齊 CCI/muscle_ratio sibling:atomic 寫入失敗時不另記 path 欄位
		// (placeSubjectOutput 失敗回空 path);statsWriteErr 已 wrap「輸出路徑無效」/
		// 「輸出目錄建立失敗」帶 context,redact 後進 result.Message。
		return failedNormalizedPhaseSyncResult(a.failMessage(i18n.KeyErrorHandlerWriteStatsFailed, statsWriteErr)), nil
	}

	a.logger.Info("標準化分期同步分析輸出", map[string]any{
		"normalizedEMG": normalizedEMGPath,
		"phaseSyncCSV":  phaseSyncCSVPath,
	})

	return &NormalizedPhaseSyncResult{
		NormalizedEMGPath: normalizedEMGPath,
		PhaseSyncCSVPath:  phaseSyncCSVPath,
		Subject:           stats.Subject,
		NormStartPhase:    params.NormStartPhase,
		NormEndPhase:      params.NormEndPhase,
		NormStartTime:     analysis.NormRange.StartTime,
		NormEndTime:       analysis.NormRange.EndTime,
		StatsStartPhase:   stats.StartPhase,
		StatsEndPhase:     stats.EndPhase,
		StatsStartTime:    stats.StartTime,
		StatsEndTime:      stats.EndTime,
		ChannelNames:      stats.ChannelNames,
		ChannelMaxes:      analysis.ChannelMaxes,
		ChannelMeans:      stats.ChannelMeans,
		Report:            calculator.FormatStatisticsReport(stats),
		Success:           true,
		Message:           "分析完成",
	}, nil
}

// normalizedPhaseSyncFailKey 依 phase_sync.AnalyzeNormalizedPhaseSync 的失敗選 failMessage
// 前綴:ctx 取消 →「分析已取消」;其餘是 *phase_sync.AnalysisError,每個 Stage 各用自己的
// i18n key(與搬移前 handler 逐步選的 key 相同)。取不到 Stage 時退回「載入資料失敗」。
func normalizedPhaseSyncFailKey(err error) string {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return i18n.KeyErrorHandlerCancelled
	}

	var stageErr *phase_sync.AnalysisError
	if errors.As(err, &stageErr) {
		switch stageErr.Stage {
		case phase_sync.StageLoad:
			return i18n.KeyErrorHandlerLoadDataFailed
		case phase_sync.StageNormRange:
			return i18n.KeyErrorHandlerNormRange
		case phase_sync.StageStatsRange:
			return i18n.KeyErrorHandlerStatsRange
		case phase_sync.StageNormalize:
			return i18n.KeyErrorHandlerNormalizeFailed
		case phase_sync.StageStatsSlice:
			return i18n.KeyErrorHandlerExtractStatsRangeFailed
		case phase_sync.StageStatistics:
			return i18n.KeyErrorHandlerCalcStatsFailed
		}
	}

	return i18n.KeyErrorHandlerLoadDataFailed
}

// validateNormalizedPhaseSyncParams 檢查必填欄位。沿用既有錯誤型別保持訊息一致。
// 兩組分期點（Norm 與 Stats）只要任一組未填齊就回 ErrNoPhaseSelection。
func validateNormalizedPhaseSyncParams(params NormalizedPhaseSyncParams) error {
	if err := validateManifestHandlerParams(params.ManifestFile, params.DataFolder); err != nil {
		return err
	}
	if params.NormStartPhase == "" || params.NormEndPhase == "" {
		return ErrNoPhaseSelection
	}
	if params.StatsStartPhase == "" || params.StatsEndPhase == "" {
		return ErrNoPhaseSelection
	}
	return nil
}

// failedNormalizedPhaseSyncResult 回傳表示失敗的結果物件，
// 沿用 CCI handler 的「不丟錯而是回 Result.Success=false」模式，讓前端統一處理。
func failedNormalizedPhaseSyncResult(message string) *NormalizedPhaseSyncResult {
	return &NormalizedPhaseSyncResult{
		Success: false,
		Message: message,
	}
}
