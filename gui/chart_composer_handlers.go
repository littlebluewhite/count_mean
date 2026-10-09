// Package gui — Chart Composer handler family.
//
// Slice C of the Chart Composer PRD (#15) — 3 個 Wails RPC handler（依序為
// LoadChartComposerSubjects / GenerateChartComposer / DownloadChartComposerImage），串接前端
// Composer panel 與 backend `internal/chart` composer engine。
//
// 設計取捨摘要：
//   - 每個 handler 與其他 Wails bound method 同形（ADR-0035）：首句
//     `defer recoverHandlerPanic`，其餘直列在 body。Chart Composer 不寫 CSV、
//     無多步驟 pipeline（ADR-0002），只做 manifest load / EMG load / chart render。
//   - Handler 3（DownloadChartComposerImage）鏡像既有 `DownloadCCIChart` 模式
//     — adapter 端從 params.Subject 經 filename.SubjectOutputName 推導 config.OutputDir
//     內的 `{subject}_chart_composer.png`,再把
//     共用 PNG 安全管線（base64 → DecodeAndValidatePNG → validateExternalPathInputs
//     → fsperm.WriteFileNoFollow）委派給 downloadValidatedPNG（ADR-0009）。
//
// 錯誤通道契約：
//
//   - Handler 1-2 永遠回 non-nil `*XxxResult`，所有可預期失敗都包成
//     result.Success=false + result.Message；Go err 只在 panic 經由
//     recoverHandlerPanic 灌入 named return 時才為 non-nil。前端可單一路徑
//     檢查 result.success / result.message。
//   - Handler 3 鏡像 DownloadCCIChart 的 dual-channel 契約（path validation
//     failure / PNG decode 失敗都走 err channel）— frontend 對 download
//     按鈕 binding 已假設此契約，不可變更。

package gui

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"count_mean/internal/chart"
	"count_mean/internal/i18n"
	"count_mean/internal/io"
	"count_mean/internal/manifest"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/synchronizer"
	"count_mean/internal/validation/filename"
)

// err113 sentinel — 純承載 user-facing 訊息(caller 把 err.Error() 灌入
// result.Message,不做 errors.Is 比對;test 走 substring assertion)。
var (
	ErrChartComposerNilParams       = errors.New("參數為空")
	ErrChartComposerMotionFileEmpty = errors.New("manifest 內 MotionFile 為空")
	ErrChartComposerSubjectNotFound = errors.New("不存在於分期總檔案")
)

// LoadChartComposerSubjectsParams Wails RPC params for subject list lookup.
type LoadChartComposerSubjectsParams struct {
	ManifestPath string `json:"manifestPath"`
	DataFolder   string `json:"dataFolder"`
}

// GenerateChartComposerParams Wails RPC params for composer chart generation.
//
// ADR-0013:一鍵生成、預設全通道。前端不再先打 LoadChartComposerEMGChannels
// 取 channel 清單 / EMGMotionOffset 再回傳;handler 自己從 manifest row 讀
// EMGMotionOffset(單一來源),EMG channel 交給 chart composer 的「空 → fallback
// 全選」行為。
type GenerateChartComposerParams struct {
	ManifestPath string `json:"manifestPath"`
	DataFolder   string `json:"dataFolder"`
	Subject      string `json:"subject"`
}

// DownloadChartComposerImageParams Wails RPC params for PNG download.
//
// Base64Data 為前端 canvas.toDataURL() 產出的 `data:image/png;base64,...` 字串;
// Subject 為前端傳入的 subject 字串,後端用 SubjectOutputName 推導
// {Subject}_chart_composer.png 落在 config.OutputDir(對稱 DownloadCCIChart)。
type DownloadChartComposerImageParams struct {
	Base64Data string `json:"base64Data"`
	Subject    string `json:"subject"`
}

// MissingFileDTO 把 manifest.MissingRow 轉成 JSON-marshalable form(error type
// 不可直接被 Wails JSON encoder 處理,改用 ErrMessage 字串)。
//
// 前端可透過 ChartComposerSubjectsResult.MissingFiles 列出在 Load 階段就偵測到
// 缺 EMG 檔的 row,UI 顯示警告 banner / 表格給 user 看「期待的檔在哪、為何沒找到」,
// 而非等 Generate 階段才炸。
type MissingFileDTO struct {
	Subject    string `json:"subject"`    // 對應 PhaseManifest.Subject
	EMGFile    string `json:"emgFile"`    // manifest 內字面的 EMGFile 欄
	ErrMessage string `json:"errMessage"` // OpenDataFile error 訊息(含期待路徑)
}

// ChartComposerSubjectsResult Wails RPC result for subject list lookup.
//
// MissingFiles 是 Bug 1 整合產物:LoadChartComposerSubjects 在 Load 階段
// validator-pass 掃過所有 EMG 檔,把缺檔 row 收集進來。non-blocking — Success
// 仍可能為 true,user 看到 missing list 後決定下一步(改 manifest / data folder
// 或忽略 missing 跑健康的 subject)。
type ChartComposerSubjectsResult struct {
	Subjects     []string         `json:"subjects"`
	MissingFiles []MissingFileDTO `json:"missingFiles"`
	Success      bool             `json:"success"`
	Message      string           `json:"message"`
}

// ChartComposerResult Wails RPC result for chart generation.
//
// HTML 為 echarts.NewLine().Render() 的完整 HTML 串（含 inline JS），前端塞進
// iframe srcdoc 顯示。
//
// PhaseTimes 為已換算到 EMG 時間 domain 的 phase 名 → 秒數 map(只含 Set=true
// 的 phase)。前端 `_renderComposerPhaseCheckboxes` 直接讀此欄位 render checkbox,
// 不再嘗試從 iframe.contentWindow.echarts 反推 markLine.data — iframe sandbox=
// allow-scripts(無 allow-same-origin)下跨 frame access 為 opaque origin,實機
// 直接失敗(對齊 CCI 既有 `result.phaseTimes` RPC return pattern)。
type ChartComposerResult struct {
	HTML       string             `json:"html"`
	PhaseTimes map[string]float64 `json:"phaseTimes"`
	Success    bool               `json:"success"`
	Message    string             `json:"message"`
}

// failedChartComposerResult builds a composer-generate result indicating failure.
// 命名對齊 sibling `failedCCIResult` / `failedMuscleRatioResult`。
func failedChartComposerResult(message string) *ChartComposerResult {
	return &ChartComposerResult{
		Success: false,
		Message: message,
	}
}

// failedChartComposerSubjectsResult builds a subjects result indicating failure.
func failedChartComposerSubjectsResult(message string) *ChartComposerSubjectsResult {
	return &ChartComposerSubjectsResult{
		Success: false,
		Message: message,
	}
}

// LoadChartComposerSubjects 解析 manifest，回 subject dropdown 列表。
//
//   - panic safety 由首句 defer recoverHandlerPanic 收乾（panic → named return err，result=nil）
//   - 非 panic 路徑永遠回 non-nil result + nil err（單一通道契約）
//
// nil params guard 在 body 入口先處理，避免裸 deref。
func (a *App) LoadChartComposerSubjects(
	params *LoadChartComposerSubjectsParams,
) (result *ChartComposerSubjectsResult, err error) {
	defer recoverHandlerPanic("Chart Composer 載入主題", a.logger, &err)

	a.logger.Info("開始Chart Composer 載入主題", nil)
	// exit log 只在正常返回時打:單一通道下正常返回的 result 必 non-nil,panic
	// 路徑 result 仍為 nil。
	defer func() {
		if result != nil {
			a.logger.Info("Chart Composer 載入主題完成", nil)
		}
	}()

	// nil params guard — Wails RPC 入口若被惡意呼叫（或前端 bug）傳 nil，
	// 不該 panic。回 failedResult + nil err（單一通道契約）。
	if params == nil {
		return failedChartComposerSubjectsResult("參數為空"), nil
	}

	if vErr := validateManifestHandlerParams(params.ManifestPath, params.DataFolder); vErr != nil {
		return failedChartComposerSubjectsResult(inputMessage(vErr)), nil
	}

	manifests, parseErr := manifest.LoadManifests(params.ManifestPath)
	if parseErr != nil {
		return failedChartComposerSubjectsResult(a.failMessage(i18n.KeyErrorHandlerLoadManifestFailed, parseErr)), nil
	}

	// 收集 unique subject — 與 LoadPhaseManifest 對稱:回 dedup 過的 subject
	// list,避免相同 subject 在 manifest 內出現多次時 dropdown 重複。
	seen := make(map[string]struct{}, len(manifests))
	subjects := make([]string, 0, len(manifests))
	for i := range manifests {
		s := manifests[i].Subject
		if _, exists := seen[s]; exists {
			continue
		}
		seen[s] = struct{}{}
		subjects = append(subjects, s)
	}

	// Bug 1 整合:Load 階段先掃過所有 EMG 檔,把不存在的 row surface 給前端,
	// 避免 user 進 dropdown 後在 Generate 階段才看到「EMG 檔案不存在」錯誤。
	// non-blocking — 即使有 missing,dropdown 仍列出全部 subject,user 自行
	// 決定下一步(改 manifest / data folder 或忽略 missing 跑其他 OK subject)。
	missing := manifest.ValidateAllEMGFiles(manifests, params.DataFolder)
	missingDTOs := make([]MissingFileDTO, len(missing))
	for i, m := range missing {
		missingDTOs[i] = MissingFileDTO{
			Subject: m.Subject,
			EMGFile: m.EMGFile,
			// OpenDataFile error 含期待路徑 → 過 redact 防 PHI 洩漏(保留 basename)。
			ErrMessage: redactText(m.Err.Error()),
		}
	}

	return &ChartComposerSubjectsResult{
		Subjects:     subjects,
		MissingFiles: missingDTOs,
		Success:      true,
		Message:      fmt.Sprintf("已載入 %d 個主題", len(subjects)),
	}, nil
}

// GenerateChartComposer 呼叫 chart.RenderComposer 並回 HTML preview。
//
// 工作流程:
//  1. 邊界路徑驗證(manifest / data folder)
//  2. 從 manifest 找指定 Subject 的 row
//  3. 載入 EMG(必要)、motion(必要)、muscle_ratio(若 MuscleRatioFile 非空)
//  4. 把 PhaseSyncEMGData / MotionData / muscle_ratio CSV 轉成 chart.ComposerInput
//  5. 呼叫 chart.RenderComposer 渲染成 HTML
//
// ADR-0013:EMGMotionOffset 直接讀 row.EMGMotionOffset(本函式已 load manifest +
// findManifestBySubject,offset 當場可得,不再經前端往返 — 消除 stale-offset 風險)。
// SelectedChannels 一律傳 nil(空),交給 chart composer 的「空 → fallback 全選」,
// 達成「預設全通道」。motion-index 轉時間透過 TimeSynchronizer.MotionIndexToEMGTime。
func (a *App) GenerateChartComposer(
	params *GenerateChartComposerParams,
) (result *ChartComposerResult, err error) {
	defer recoverHandlerPanic("Chart Composer 圖表生成", a.logger, &err)

	a.logger.Info("開始Chart Composer 圖表生成", nil)
	// exit log 只在正常返回時打:單一通道下正常返回的 result 必 non-nil,panic
	// 路徑 result 仍為 nil。
	defer func() {
		if result != nil {
			a.logger.Info("Chart Composer 圖表生成完成", nil)
		}
	}()

	if params == nil {
		return failedChartComposerResult("參數為空"), nil
	}

	if vErr := validateManifestHandlerParams(params.ManifestPath, params.DataFolder); vErr != nil {
		return failedChartComposerResult(inputMessage(vErr)), nil
	}

	if params.Subject == "" {
		return failedChartComposerResult("Subject 不可為空"), nil
	}

	manifests, parseErr := manifest.LoadManifests(params.ManifestPath)
	if parseErr != nil {
		return failedChartComposerResult(a.failMessage(i18n.KeyErrorHandlerLoadManifestFailed, parseErr)), nil
	}

	row, found := findManifestBySubject(manifests, params.Subject)
	if !found {
		return failedChartComposerResult(inputMessage(
			fmt.Errorf("Subject %q %w", params.Subject, ErrChartComposerSubjectNotFound),
		)), nil
	}

	// 載入 EMG(必要)— 走 manifest.LoadEMG([[Subject source]]),開檔失敗與解析失敗各用自己的 key。
	emgPhaseSync, emgErr := manifest.LoadEMG(params.DataFolder, &row)
	if emgErr != nil {
		var parseErr *manifest.EMGParseError
		if errors.As(emgErr, &parseErr) {
			return failedChartComposerResult(a.failMessage(i18n.KeyErrorHandlerParseEMGFailed, emgErr)), nil
		}
		return failedChartComposerResult(a.failMessage(i18n.KeyErrorHandlerResolveEMGPathFailed, emgErr)), nil
	}
	emgDataset := phaseSyncEMGToDataset(emgPhaseSync)

	// 載入 motion(必要 — composer 至少 2-grid,motion 是其中一個 grid)
	composerMotion, motionErr := loadComposerMotion(
		params.DataFolder, row.MotionFile, row.EMGMotionOffset,
	)
	if motionErr != nil {
		return failedChartComposerResult(a.failMessage(i18n.KeyErrorHandlerParseMotionFailed, motionErr)), nil
	}

	// 載入 muscle_ratio(可選 — 僅 V.14 manifest 帶 MuscleRatioFile)
	var muscleRatioData *chart.MuscleRatioData
	if strings.TrimSpace(row.MuscleRatioFile) != "" {
		// 開檔走 manifest.OpenDataFile 硬化讀檔門(內部 lenient resolve + atomic
		// validated-open),支援 BTS 匯出含字面 "%" 的檔名;CSV 解析委派
		// io.ReadMuscleRatioOutputAll(WriteMuscleRatioOutputAll 的反函式)。
		mrFile, mrErr := manifest.OpenDataFile(params.DataFolder, row.MuscleRatioFile)
		if mrErr != nil {
			return failedChartComposerResult(a.failMessage(i18n.KeyErrorHandlerParseMuscleRatioFailed,
				fmt.Errorf("muscle_ratio 路徑解析失敗: %w", mrErr))), nil
		}
		mrPayload, mrErr := io.ReadMuscleRatioOutputAll(mrFile)
		_ = mrFile.Close() //nolint:errcheck // read-only fd; close error not actionable
		if mrErr != nil {
			if !errors.Is(mrErr, io.ErrMuscleRatioCSVEmpty) && !errors.Is(mrErr, io.ErrMuscleRatioCSVNoHeader) {
				mrErr = fmt.Errorf("讀取 muscle_ratio CSV 失敗: %w", mrErr)
			}
			return failedChartComposerResult(a.failMessage(i18n.KeyErrorHandlerParseMuscleRatioFailed, mrErr)), nil
		}
		mrSeries := make(map[string][]float64, len(mrPayload.PairLabels))
		for k, name := range mrPayload.PairLabels {
			mrSeries[name] = mrPayload.Ratios[k]
		}
		muscleRatioData = &chart.MuscleRatioData{
			Time:   mrPayload.Times,
			Series: mrSeries,
			Order:  mrPayload.PairLabels,
		}
	}

	// manifest row 的 [[Phase timeline]] 轉成「phase 名 → EMG 秒數」單一份 map。
	// Chart Composer 的所有 grid X 軸是 **EMG 時間** domain;若不換算直接 attach,
	// markLine 會早 / 晚整個 sync offset — silent visual bug。
	// 這份 map 同時供後端預設 markLine(composerInput.PhaseTimesEMG)與前端 checkbox
	// /動態 markLine(回傳的 PhaseTimes)使用,兩端共用來源不會分歧。
	phaseTimes := composerPhaseTimesEMG(&row)

	// SelectedChannels 傳 nil(空)— chart composer 對空走「fallback 全選」
	// (composer.go:267-273),達成 ADR-0013 的「預設全通道」。
	composerInput := chart.ComposerInput{
		Subject:          row.Subject,
		EMGDataset:       emgDataset,
		SelectedChannels: nil,
		MuscleRatioData:  muscleRatioData,
		MotionData:       composerMotion,
		PhaseTimesEMG:    phaseTimes,
		EMGMotionOffset:  row.EMGMotionOffset,
	}

	var buf bytes.Buffer
	if renderErr := chart.RenderComposer(a.context(), composerInput, &buf); renderErr != nil {
		// EMG-required 是 caller bug(我們上面剛確保 emgDataset 非 nil)而非
		// user-visible 路徑,直接走通用錯誤訊息;ctx cancel 走相同 path。
		if errors.Is(renderErr, chart.ErrComposerEMGRequired) {
			return failedChartComposerResult("內部錯誤: EMG dataset 為空"), nil
		}
		return failedChartComposerResult(a.failMessage(i18n.KeyErrorHandlerChartRenderFailed, renderErr)), nil
	}

	return &ChartComposerResult{
		HTML:       buf.String(),
		PhaseTimes: phaseTimes,
		Success:    true,
		Message:    "圖表生成完成",
	}, nil
}

// DownloadChartComposerImage 接前端 base64 PNG dataURL,從 params.Subject 推導
// config.OutputDir 內的固定檔名({SubjectOutputName(subject, "chart_composer")}.png)
// 並寫入(對稱 DownloadCCIChart)。
//
// short-circuit 順序敏感(PNG decode 先於 path validation,避免在合法路徑寫進
// 非 PNG 內容),鏡像 `DownloadCCIChart`。
//
// adapter 職責:從 params.Subject 經 filename.SubjectOutputName 推導檔名(內部強制
// Sanitize 防 traversal)。共用的 PNG 安全管線(prefix 檢查 → DecodeAndValidatePNG
// → boundary 路徑驗證 → WriteFileNoFollow)已抽到 downloadValidatedPNG(ADR-0009)
// — 此 handler 只負責 adapter 邏輯與 logging。
func (a *App) DownloadChartComposerImage(
	params *DownloadChartComposerImageParams,
) (result *ChartResult, err error) {
	defer recoverHandlerPanic("DownloadChartComposerImage", a.logger, &err)

	a.logger.Info("開始下載 Chart Composer 圖表", nil)

	if params == nil {
		return nil, ErrChartComposerNilParams
	}

	// params.Subject 來自前端;sanitize 防路徑穿越("../x" 之類)由 SubjectOutputName 內部強制。
	s := a.state.Load()
	outputPath := filepath.Join(
		s.config.OutputDir,
		filename.SubjectOutputName(params.Subject, "chart_composer")+".png",
	)

	result, err = a.downloadValidatedPNG(params.Base64Data, outputPath)
	if err != nil {
		a.logger.Error("Chart Composer 圖表下載失敗", err, map[string]any{
			"output": outputPath,
		})
		return nil, err
	}

	a.logger.Info("Chart Composer 圖表下載完成", map[string]any{
		"output": outputPath,
	})

	return result, nil
}

// findManifestBySubject 在 manifest list 內找第一個 Subject 等於 target 的 row。
// 第二個回傳值 false 表示沒找到。比對為 case-sensitive exact match。
func findManifestBySubject(
	manifests []models.PhaseManifest, target string,
) (models.PhaseManifest, bool) {
	for i := range manifests {
		if manifests[i].Subject == target {
			return manifests[i], true
		}
	}
	return models.PhaseManifest{}, false
}

// phaseSyncEMGToDataset 把 PhaseSyncEMGData (Time + Channels map) 轉成
// EMGDataset (Headers + []EMGData rows)。
//
// 為何需要轉:phase_sync 系列 parser 走 columnar (Time slice + map[name]values),
// 對 phase analyzer 友善;chart composer (`internal/chart`) 直接 reuse
// EMGDataset(Headers + per-row Time + Channels slice),對 chart series
// 渲染友善。本 helper 是兩個 representation 之間的 thin bridge。
//
// 排序:Headers[1:] 走 emg.Headers 順序(parsers.initEMGData 已 strip 時間
// header),整體 Headers 第一欄填入 "time"。
func phaseSyncEMGToDataset(emg *models.PhaseSyncEMGData) *models.EMGDataset {
	if emg == nil {
		return &models.EMGDataset{}
	}

	headers := make([]string, 0, 1+len(emg.Headers))
	headers = append(headers, "time")
	headers = append(headers, emg.Headers...)

	rows := make([]models.EMGData, len(emg.Time))
	for i, t := range emg.Time {
		channels := make([]float64, len(emg.Headers))
		for k, name := range emg.Headers {
			vals := emg.Channels[name]
			if i < len(vals) {
				channels[k] = vals[i]
			}
		}
		rows[i] = models.EMGData{Time: t, Channels: channels}
	}

	return &models.EMGDataset{
		Headers: headers,
		Data:    rows,
	}
}

// loadComposerMotion 從 motion CSV 載入後轉成 chart.MotionData
// (time-series + per-channel slice + 可預測 channel 順序)。
//
// motion-index 透過 TimeSynchronizer.MotionIndexToEMGTime 換算為 EMG 時間軸,
// 讓 motion grid 與 EMG grid 在同一條 X 軸上對齊。
//
// 開檔走 manifest.OpenDataFile 硬化讀檔門(內部 lenient resolve + atomic
// validated-open),對齊 muscle_ratio analyzer 內的 EMG file 處理風格。
func loadComposerMotion(
	dataFolder, motionFile string, emgMotionOffset int,
) (*chart.MotionData, error) {
	if strings.TrimSpace(motionFile) == "" {
		// motion 是必要 grid;空檔名直接 fail。
		return nil, ErrChartComposerMotionFileEmpty
	}

	motionFileHandle, err := manifest.OpenDataFile(dataFolder, motionFile)
	if err != nil {
		return nil, fmt.Errorf("Motion 路徑解析失敗: %w", err)
	}
	defer func() { _ = motionFileHandle.Close() }() //nolint:errcheck // read-only fd; close error not actionable

	motion, err := parsers.NewMotionParser().Parse(motionFileHandle, motionFile)
	if err != nil {
		return nil, fmt.Errorf("解析 Motion 失敗: %w", err)
	}

	ts := synchronizer.NewTimeSynchronizer()
	times := make([]float64, len(motion.Indices))
	for i, idx := range motion.Indices {
		times[i] = ts.MotionIndexToEMGTime(idx, emgMotionOffset)
	}

	// motion.Headers 已是純資料 channel 名 — parsers.MotionParser.initializeMotionData
	// 已透過 `Headers: headers[1:]` 把 Index 欄剝掉(motion_parser.go line 133)。
	// 過去這段對 `k == 0 continue` 是錯認 Index 仍在 Headers 內,結果把第一個真資料
	// channel 永遠不渲染(單 channel motion 整 grid 空白)。直接 iterate 即可。
	order := make([]string, 0, len(motion.Headers))
	series := make(map[string][]float64, len(motion.Headers))
	for _, name := range motion.Headers {
		vals := motion.Data[name]
		// 防呆:若 parser 因 jagged row 略 row 而 vals 不齊,直接 skip 該 series
		// 而非 panic;motion 資料量大,degraded mode 比 hard fail 對使用者好。
		if len(vals) != len(times) {
			continue
		}
		order = append(order, name)
		series[name] = vals
	}

	return &chart.MotionData{
		Time:   times,
		Series: series,
		Order:  order,
	}, nil
}

// composerPhaseTimesEMG 把 manifest row 的 [[Phase timeline]] 轉成「phase 名 → EMG 秒數」
// map(ADR-0042),供 Chart Composer 後端預設 markLine 與前端 phaseTimes RPC return
// 共用同一份來源。未提供的分期點不在 map 內 — 不會 inject 偽 markLine,前端也不渲染 checkbox。
func composerPhaseTimesEMG(m *models.PhaseManifest) map[string]float64 {
	timeline := synchronizer.NewPhaseTimeline(m)
	out := make(map[string]float64, len(timeline))
	for _, pt := range timeline {
		out[string(pt.Phase)] = pt.EMGTime
	}
	return out
}
