// Package gui — Chart Composer handler family.
//
// Slice C of the Chart Composer PRD (#15) — 3 個 Wails RPC handler（依序為
// LoadChartComposerSubjects / GenerateChartComposer / DownloadChartComposerImage），串接前端
// Composer panel 與 backend `internal/chart` composer engine。
//
// 設計取捨摘要：
//   - 每個 handler 與其他 Wails bound method 同形（ADR-0035）：首句
//     `defer recoverHandlerPanic`，其餘直列在 body。Chart Composer 不寫 CSV、
//     無多步驟 pipeline（ADR-0002）；Generate 的資料組裝委派 internal/composer
//     （ADR-0046），handler 只做驗證 / chart render / envelope。
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

	"count_mean/internal/chart"
	"count_mean/internal/composer"
	"count_mean/internal/i18n"
	"count_mean/internal/manifest"
	"count_mean/internal/validation/filename"
)

// ErrChartComposerNilParams err113 sentinel — 純承載 user-facing 訊息
// (DownloadChartComposerImage 的 nil params guard,不做 errors.Is 比對;test 走 substring assertion)。
var ErrChartComposerNilParams = errors.New("參數為空")

// LoadChartComposerSubjectsParams Wails RPC params for subject list lookup.
type LoadChartComposerSubjectsParams struct {
	ManifestPath string `json:"manifestPath"`
	DataFolder   string `json:"dataFolder"`
}

// GenerateChartComposerParams Wails RPC params for composer chart generation.
//
// ADR-0013:一鍵生成、預設全通道。前端不再先打 LoadChartComposerEMGChannels
// 取 channel 清單 / EMGMotionOffset 再回傳;handler 自己從 manifest row 讀
// EMGMotionOffset(單一來源),EMG 通道由 chart composer 依 EMG.Headers 全部渲染。
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
// 工作流程(adapter):
//  1. 邊界路徑驗證(manifest / data folder)與 Subject 非空
//  2. composer.Load 組出 chart.ComposerInput(manifest row → EMG / motion / muscle_ratio /
//     phase 秒數;ADR-0013 的 EMGMotionOffset 由它從 row 讀,EMG 預設全通道)
//  3. 呼叫 chart.RenderComposer 渲染成 HTML
//  4. envelope:載入失敗依 composer.Stage 選 i18n 前綴(chartComposerLoadFailKey),
//     Subject 不在 manifest 是輸入錯誤(inputMessage)
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

	composerInput, loadErr := composer.Load(params.ManifestPath, params.DataFolder, params.Subject)
	if loadErr != nil {
		if errors.Is(loadErr, composer.ErrSubjectNotFound) {
			return failedChartComposerResult(inputMessage(loadErr)), nil
		}
		return failedChartComposerResult(a.failMessage(chartComposerLoadFailKey(loadErr), loadErr)), nil
	}

	var buf bytes.Buffer
	if renderErr := chart.RenderComposer(a.context(), *composerInput, &buf); renderErr != nil {
		// EMG-required 是 caller bug(composer.Load 成功必帶 EMG)而非
		// user-visible 路徑,直接走通用錯誤訊息;ctx cancel 走相同 path。
		if errors.Is(renderErr, chart.ErrComposerEMGRequired) {
			return failedChartComposerResult("內部錯誤: EMG dataset 為空"), nil
		}
		return failedChartComposerResult(a.failMessage(i18n.KeyErrorHandlerChartRenderFailed, renderErr)), nil
	}

	// PhaseTimes 與 chart 預設 markLine 共用 composer.Load 產出的同一份 map,
	// 前端 checkbox / 動態 markLine 與後端不會分歧。
	return &ChartComposerResult{
		HTML:       buf.String(),
		PhaseTimes: composerInput.PhaseTimesEMG,
		Success:    true,
		Message:    "圖表生成完成",
	}, nil
}

// chartComposerLoadFailKey 依 composer.Load 失敗的 Stage 選 failMessage 前綴:每個載入
// 階段各用自己的 i18n key。Load 的非輸入失敗都是 *composer.LoadError;取不到 Stage 時
// 退回通用的「載入資料失敗」。
func chartComposerLoadFailKey(err error) string {
	var loadErr *composer.LoadError
	if errors.As(err, &loadErr) {
		switch loadErr.Stage {
		case composer.StageManifest:
			return i18n.KeyErrorHandlerLoadManifestFailed
		case composer.StageEMGOpen:
			return i18n.KeyErrorHandlerResolveEMGPathFailed
		case composer.StageEMGParse:
			return i18n.KeyErrorHandlerParseEMGFailed
		case composer.StageMotion:
			return i18n.KeyErrorHandlerParseMotionFailed
		case composer.StageMuscleRatio:
			return i18n.KeyErrorHandlerParseMuscleRatioFailed
		}
	}
	return i18n.KeyErrorHandlerLoadDataFailed
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
