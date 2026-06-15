package gui

import (
	"fmt"

	"count_mean/internal/calculator"
	"count_mean/internal/io"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security/redact"
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

// AnalyzeNormalizedPhaseSync 執行「先標準化、再分期同步分析」的組合工作流。
//
// 流程：
//  1. PhaseSyncAnalyzer.Load 載入 manifest 與 EMG 資料（不算範圍）
//  2. ResolvePhaseRange 兩次：分別解析 Norm 視窗與 Stats 視窗
//  3. 以每條肌肉在 Norm 視窗內的最大值為除數，對整段資料做標準化
//  4. 將標準化資料輸出為 Output 1：{subject}_normalized.csv
//  5. 對標準化後的資料於 Stats 視窗內計算統計（mean/max）
//  6. 將統計結果輸出為 Output 2：
//     {subject}_normalized_norm-{normStart}-{normEnd}_stats-{statsStart}-{statsEnd}.csv
//     （欄位與既有「分期同步分析」相同；檔名同時帶兩組分期點以避免不同設定的輸出混淆）
//
// 錯誤通道契約（Wave 3 Batch R）：
//
//	AnalyzeNormalizedPhaseSync 永遠回傳 non-nil *NormalizedPhaseSyncResult；
//	所有可預期失敗（參數驗證、路徑驗證、load、phase range、normalize、stats、
//	檔案寫入）都包成 result.Success=false + result.Message。Go err 只在
//	`recoverHandlerPanic` 透過 named return 灌入 panic 時才為 non-nil。
//	前端因此可以單一路徑檢查 result.success / result.message。
func (a *App) AnalyzeNormalizedPhaseSync(params NormalizedPhaseSyncParams) (result *NormalizedPhaseSyncResult, err error) {
	// 形狀不入 [[Analysis pipeline family]] (多 step、雙 output、跨 step ctx
	// 檢查),直接走 Tier 1 [[HandlerRun]] 享有 panic-safety + entry/exit log
	// 紅利,但不被 family Validate/Execute/WriteCSV 三 closure 形狀束縛。
	// body 維持原有「load → resolve×2 → normalize → write1 → range → stats →
	// write2」多 step 流程與跨 step ctx.Err() 檢查切點;single-channel 錯誤通道
	// 契約(永遠回 (result, nil),失敗包成 result.Success=false + result.Message)
	// 也由 body 自行維護。
	a.logger.Info("標準化分期同步分析參數", map[string]any{"params": params})
	return HandlerRun(a.logger, "標準化分期同步分析", func() (*NormalizedPhaseSyncResult, error) {
		s := a.state.Load()

		// 抓取 Wails lifecycle ctx,讓 Shutdown / 使用者中止可在
		// step 之間早停(analyzer.Load / ResolvePhaseRange 內部目前還沒 ctx,
		// 但這條 handler 在 step boundary 顯式 ctx.Err() 檢查 — 對 norm window
		// 大 dataset 的 NormalizeByRangeMax 後特別重要)。
		ctx := a.context()

		if validationErr := validateNormalizedPhaseSyncParams(params); validationErr != nil {
			// redact 對 path-free sentinel 不變,防未來帶路徑的 Validate error 洩漏 PHI。
			return failedNormalizedPhaseSyncResult(redact.RedactForMessage(validationErr)), nil
		}

		// 1. 載入 manifest 與 EMG（共用兩組區間的前置步驟）
		baseParams := &models.AnalysisParams{
			ManifestFile: params.ManifestFile,
			DataFolder:   params.DataFolder,
			StartPhase:   params.NormStartPhase,
			EndPhase:     params.NormEndPhase,
			SubjectIndex: params.SubjectIndex,
		}

		loaded, loadErr := a.phaseSyncAnalyzer.Load(baseParams)
		if loadErr != nil {
			a.logger.Error("載入分期同步資料失敗", loadErr, map[string]any{})
			// 同 CCI handler — error 字面可能含 absolute path,過 redact 才塞 message。
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("載入資料失敗: %s", redact.RedactForMessage(loadErr))), nil
		}

		// 2. 分別解析標準化視窗與統計視窗（兩組獨立、不互相驗證）
		normRange, normRangeErr := a.phaseSyncAnalyzer.ResolvePhaseRange(loaded, params.NormStartPhase, params.NormEndPhase)
		if normRangeErr != nil {
			a.logger.Error("標準化區間解析失敗", normRangeErr, map[string]any{})
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("標準化區間: %s", redact.RedactForMessage(normRangeErr))), nil
		}

		statsRange, statsRangeErr := a.phaseSyncAnalyzer.ResolvePhaseRange(loaded, params.StatsStartPhase, params.StatsEndPhase)
		if statsRangeErr != nil {
			a.logger.Error("統計區間解析失敗", statsRangeErr, map[string]any{})
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("統計區間: %s", redact.RedactForMessage(statsRangeErr))), nil
		}

		// 3. 用 normRange 做標準化（除數來自此區間每條肌肉的最大值）
		normalizer := calculator.NewRangeNormalizer()

		normalizedData, channelMaxes, normErr := normalizer.NormalizeByRangeMax(
			loaded.EMGData,
			normRange.StartTime,
			normRange.EndTime,
		)
		if normErr != nil {
			a.logger.Error("區間最大值標準化失敗", normErr, map[string]any{})
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("標準化失敗: %s", redact.RedactForMessage(normErr))), nil
		}

		// 4. 撰寫 Output 1：標準化後的 EMG CSV
		// CSVHandler.WriteNormalizedPhaseSyncEMG 持有 Sanitize + 路徑拼接 + boundary
		// validation + atomic write，呼叫方只需傳 subject；不再本地拼路徑。

		// 在重 IO step 之前檢查 ctx,讓 Shutdown 能及時 cancel(NormalizeByRangeMax
		// 已跑完,寫檔即將開始 — 此處取消對使用者體驗最有感)。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("分析已取消: %s", redact.RedactForMessage(ctxErr))), nil
		}

		normalizedEMGPath, csvWriteErr := s.csvHandler.WriteNormalizedPhaseSyncEMG(io.WriteRequest{}, normalizedData, loaded.Manifest.Subject)
		if csvWriteErr != nil {
			a.logger.Error("寫入標準化 EMG 失敗", csvWriteErr, map[string]any{})
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("寫入標準化 EMG 失敗: %s", redact.RedactForMessage(csvWriteErr))), nil
		}

		// 5. 用 statsRange 擷取標準化後的資料 + 計算統計
		rangeResult, rangeErr := parsers.GetEMGDataInTimeRange(
			normalizedData,
			statsRange.StartTime,
			statsRange.EndTime,
		)
		if rangeErr != nil {
			a.logger.Error("擷取標準化資料統計區間失敗", rangeErr, map[string]any{})
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("擷取統計區間失敗: %s", redact.RedactForMessage(rangeErr))), nil
		}

		statsCalc := calculator.NewEMGStatisticsCalculator()

		stats, statsErr := statsCalc.CalculateStatistics(
			rangeResult.Data,
			calculator.StatisticsParams{
				Subject:    loaded.Manifest.Subject,
				StartPhase: params.StatsStartPhase,
				StartTime:  rangeResult.ActualStartTime,
				EndPhase:   params.StatsEndPhase,
				EndTime:    rangeResult.ActualEndTime,
			},
		)
		if statsErr != nil {
			a.logger.Error("計算標準化統計失敗", statsErr, map[string]any{})
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("計算統計失敗: %s", redact.RedactForMessage(statsErr))), nil
		}

		// 6. 撰寫 Output 2：Subject-based atomic write;filename + 路徑守門由 CSVHandler 持有。
		// 第二輪 ctx 檢查,寫檔前再給一次 cancel 機會。
		if ctxErr := ctx.Err(); ctxErr != nil {
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("分析已取消: %s", redact.RedactForMessage(ctxErr))), nil
		}

		phaseSyncCSVPath, statsWriteErr := s.csvHandler.WriteNormalizedPhaseSyncResult(
			io.WriteRequest{}, stats, params.NormStartPhase, params.NormEndPhase,
		)
		if statsWriteErr != nil {
			// 對齊 CCI/muscle_ratio sibling:atomic 寫入失敗時不另記 path 欄位
			// (writePhaseSyncAtomic 失敗回空 path);statsWriteErr 已 wrap「PhaseSync
			// 輸出...」帶 context,redact 後進 result.Message。
			a.logger.Error("寫入標準化統計 CSV 失敗", statsWriteErr, map[string]any{})
			return failedNormalizedPhaseSyncResult(fmt.Sprintf("寫入統計失敗: %s", redact.RedactForMessage(statsWriteErr))), nil
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
			NormStartTime:     normRange.StartTime,
			NormEndTime:       normRange.EndTime,
			StatsStartPhase:   stats.StartPhase,
			StatsEndPhase:     stats.EndPhase,
			StatsStartTime:    stats.StartTime,
			StatsEndTime:      stats.EndTime,
			ChannelNames:      stats.ChannelNames,
			ChannelMaxes:      channelMaxes,
			ChannelMeans:      stats.ChannelMeans,
			Report:            calculator.FormatStatisticsReport(stats),
			Success:           true,
			Message:           "分析完成",
		}, nil
	})
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
