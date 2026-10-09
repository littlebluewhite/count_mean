//nolint:revive // package name with underscore maintained for backward compatibility
package phase_sync

import (
	"context"

	"count_mean/internal/models"
)

// Stage 標示分析在哪個步驟失敗,caller 依此選 user-facing 訊息前綴(ADR-0047)。
type Stage int

const (
	StageLoad       Stage = iota + 1 // manifest / 資料檔驗證與 EMG 載入
	StageNormRange                   // 標準化區間:分期點順序或時間範圍
	StageStatsRange                  // 統計區間:分期點順序或時間範圍
	StageNormalize                   // 以標準化區間各通道最大值標準化
	StageStatsSlice                  // 擷取統計區間的 EMG
	StageStatistics                  // 計算統計
)

// AnalysisError 是 AnalyzeNormalizedPhaseSync(與 computePhaseSync)的步驟失敗:Stage 標示
// 步驟。Error() 與 Err 逐字相同、Unwrap 回 Err(同 composer.LoadError 的形狀),不改變
// 任何輸出文字。
type AnalysisError struct {
	Stage Stage
	Err   error
}

func (e *AnalysisError) Error() string { return e.Err.Error() }

func (e *AnalysisError) Unwrap() error { return e.Err }

// NormalizedParams 是 AnalyzeNormalizedPhaseSync 的輸入:一個 [[Subject]] 與兩組獨立的
// 分期區間 —— Norm 區間決定每條通道的標準化除數,Stats 區間決定輸出統計的時間範圍。
// 兩組區間可重疊或完全分離,不互相檢查。
type NormalizedParams struct {
	ManifestFile    string
	DataFolder      string
	SubjectIndex    int
	NormStartPhase  models.PhasePoint
	NormEndPhase    models.PhasePoint
	StatsStartPhase models.PhasePoint
	StatsEndPhase   models.PhasePoint
}

// NormalizedResult 是 AnalyzeNormalizedPhaseSync 的計算結果;寫檔(Output 1 / Output 2)
// 由 caller 經 CSVHandler 負責(ADR-0020)。
type NormalizedResult struct {
	Subject       string                   // manifest row 的 Subject
	NormalizedEMG *models.PhaseSyncEMGData // 整段 EMG 除以各通道在 Norm 區間內的最大值(Output 1 的內容)
	ChannelMaxes  map[string]float64       // 標準化前、Norm 區間內各通道最大值(除數)
	NormRange     models.PhaseTimeRange    // Norm 區間的 EMG 秒數([[Phase timeline]] 解析值,非切片後的 sample 時間)
	StatsRange    models.PhaseTimeRange    // Stats 區間的 EMG 秒數(同上);唯一讀者是 Phase timeline agreement 測試(gui/phase_timeline_agreement_test.go,Ruling 25)
	Stats         *models.EMGStatistics    // 標準化資料在 Stats 區間內的統計(Output 2 的內容)
}

// AnalyzeNormalizedPhaseSync 執行「先標準化、再分期同步分析」(ADR-0047):
//
//  1. 兩組分期點順序 —— 任何 I/O 之前
//  2. load:manifest 與 EMG(與 AnalyzePhaseSync 共用)
//  3. 解析 Norm 區間與 Stats 區間
//  4. 以每條通道在 Norm 區間內的最大值為除數,對整段 EMG 標準化
//  5. computePhaseSync:標準化資料在 Stats 區間內的統計(與 AnalyzePhaseSync 共用)
//
// 全部算完才回傳,所以任一步失敗都不會有部分結果 —— caller 只在成功時寫 Output 1 / 2。
//
// 錯誤:ctx 取消原樣回 ctx.Err()(進入點、load 後、computePhaseSync 內檢查);其餘都是
// *AnalysisError,Stage 標示步驟,Err 是 cause 原文。
func (analyzer *PhaseSyncAnalyzer) AnalyzeNormalizedPhaseSync(
	ctx context.Context, p *NormalizedParams,
) (*NormalizedResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := analyzer.validatePhasePair(p.NormStartPhase, p.NormEndPhase); err != nil {
		return nil, &AnalysisError{Stage: StageNormRange, Err: err}
	}
	if err := analyzer.validatePhasePair(p.StatsStartPhase, p.StatsEndPhase); err != nil {
		return nil, &AnalysisError{Stage: StageStatsRange, Err: err}
	}

	m, emgData, err := analyzer.load(p.ManifestFile, p.DataFolder, p.SubjectIndex)
	if err != nil {
		return nil, &AnalysisError{Stage: StageLoad, Err: err}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	normRange, err := resolvePhaseRange(emgData, m, p.NormStartPhase, p.NormEndPhase)
	if err != nil {
		return nil, &AnalysisError{Stage: StageNormRange, Err: err}
	}

	// Stats 區間在標準化之前先解析:失敗的先後與搬移前相同(Stats 區間錯誤先於標準化
	// 錯誤),解析值也要回給 caller。computePhaseSync 會在標準化資料上再解析一次 ——
	// 同一份 Phase timeline、標準化資料的 Time 是原資料的拷貝,結果相同。
	statsRange, err := resolvePhaseRange(emgData, m, p.StatsStartPhase, p.StatsEndPhase)
	if err != nil {
		return nil, &AnalysisError{Stage: StageStatsRange, Err: err}
	}

	normalized, channelMaxes, err := analyzer.rangeNormalizer.NormalizeByRangeMax(
		emgData, normRange.StartTime, normRange.EndTime,
	)
	if err != nil {
		return nil, &AnalysisError{Stage: StageNormalize, Err: err}
	}

	stats, err := analyzer.computePhaseSync(ctx, normalized, m, p.StatsStartPhase, p.StatsEndPhase)
	if err != nil {
		return nil, err
	}

	return &NormalizedResult{
		Subject:       m.Subject,
		NormalizedEMG: normalized,
		ChannelMaxes:  channelMaxes,
		NormRange:     *normRange,
		StatsRange:    *statsRange,
		Stats:         stats,
	}, nil
}
