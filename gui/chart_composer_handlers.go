// Package gui — Chart Composer handler family.
//
// Slice C of the Chart Composer PRD (#15) — 2 個 Wails RPC handler（依序為
// GenerateChartComposer / DownloadChartComposerImage），串接前端 Composer panel 與 backend
// `internal/chart` composer engine。subject 下拉與其他 panel 共用 LoadManifestSubjects（ADR-0057）。
//
// 設計取捨摘要：
//   - 每個 handler 與其他 Wails bound method 同形（ADR-0035）：首句
//     `defer recoverHandlerPanic`，其餘直列在 body。Chart Composer 不寫 CSV、
//     無多步驟 pipeline（ADR-0002）；Generate 的資料組裝委派 internal/composer
//     （ADR-0046），handler 只做驗證 / chart render / envelope。
//   - Handler 2（DownloadChartComposerImage）鏡像既有 `DownloadCCIChart` 模式
//     — adapter 端只給 params.Subject 與 suffix "chart_composer",把共用 PNG 管線
//     （base64 → DecodeAndValidatePNG → CSVHandler.WriteSubjectPNG 落在 config.OutputDir
//     內的 `{subject}_chart_composer.png`）委派給 downloadValidatedPNG（ADR-0009 / ADR-0055）。
//
// 錯誤通道契約：
//
//   - Handler 1 永遠回 non-nil `*ChartComposerResult`，所有可預期失敗都包成
//     result.Success=false + result.Message；Go err 只在 panic 經由
//     recoverHandlerPanic 灌入 named return 時才為 non-nil。前端可單一路徑
//     檢查 result.success / result.message。
//   - Handler 2 鏡像 DownloadCCIChart 的 dual-channel 契約（path validation
//     failure / PNG decode 失敗都走 err channel）— frontend 對 download
//     按鈕 binding 已假設此契約，不可變更。

package gui

import (
	"bytes"
	"errors"

	"count_mean/internal/chart"
	"count_mean/internal/composer"
	"count_mean/internal/i18n"
	"count_mean/internal/manifest"
)

// ErrChartComposerNilParams err113 sentinel — 純承載 user-facing 訊息
// (DownloadChartComposerImage 的 nil params guard,不做 errors.Is 比對;test 走 substring assertion)。
var ErrChartComposerNilParams = errors.New("參數為空")

// GenerateChartComposerParams Wails RPC params for composer chart generation.
//
// ADR-0013:一鍵生成、預設全通道。前端不再先打 LoadChartComposerEMGChannels
// 取 channel 清單 / EMGMotionOffset 再回傳;composer.Load 自己從 manifest row 讀
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
		return failedChartComposerResult(inputMessage(ErrNoSubject)), nil
	}

	composerInput, loadErr := composer.Load(params.ManifestPath, params.DataFolder, params.Subject)
	if loadErr != nil {
		if errors.Is(loadErr, manifest.ErrSubjectNotFound) {
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

// DownloadChartComposerImage 接前端 base64 PNG dataURL,寫成 config.OutputDir 內的固定檔名
// ({SubjectOutputName(subject, "chart_composer")}.png,對稱 DownloadCCIChart)。
//
// short-circuit 順序敏感(PNG decode 先於落檔,避免在合法路徑寫進非 PNG 內容),
// 鏡像 `DownloadCCIChart`。
//
// adapter 職責:給 suffix "chart_composer";檔名由 CSVHandler.WriteSubjectPNG 經
// SubjectOutputName 推導(內部強制 Sanitize 防 traversal)。PNG 管線(prefix 檢查 →
// DecodeAndValidatePNG → Output placement)在 downloadValidatedPNG(ADR-0009 / ADR-0055)
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
	result, err = a.downloadValidatedPNG(params.Base64Data, params.Subject, "chart_composer")
	if err != nil {
		a.logger.Error("Chart Composer 圖表下載失敗", err)
		return nil, err
	}

	a.logger.Info("Chart Composer 圖表下載完成", map[string]any{
		"output": result.OutputPath,
	})

	return result, nil
}
