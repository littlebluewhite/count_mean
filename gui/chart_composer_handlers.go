// Package gui — Chart Composer handler family.
//
// Slice C of the Chart Composer PRD (#15) — 4 個 Wails RPC handler，串接前端
// Composer panel 與 backend `internal/chart` composer engine。
//
// 設計取捨摘要：
//   - Handler 1-3 走 [[HandlerRun[R]]] Tier 1 wrapper（panic safety + entry/exit
//     log + generic err wrapping）。Chart Composer 明確**不**屬於 Analysis pipeline
//     family（ADR-0002）— 不寫 CSV、無多步驟 pipeline，只做 manifest load / EMG
//     load / chart render，因此不走 [[AnalysisHandler[P, R]]] Tier 2。
//   - Handler 4（DownloadChartComposerImage）鏡像既有 `DownloadCCIChart` 模式
//     — base64 → DecodeAndValidatePNG → SanitizeFileName → validateExternalPathInputs
//     → fsperm.WriteFileNoFollow，自己加 recoverHandlerPanic defer。**不**走
//     HandlerRun 因為 download 路徑的特殊安全鏈（base64 + path validation）需要
//     精確控制 short-circuit 順序。
//
// 錯誤通道契約：
//
//   - Handler 1-3 永遠回 non-nil `*XxxResult`，所有可預期失敗都包成
//     result.Success=false + result.Message；Go err 只在 panic 經由
//     recoverHandlerPanic 灌入 named return 時才為 non-nil。前端可單一路徑
//     檢查 result.success / result.message。
//   - Handler 4 鏡像 DownloadCCIChart 的 dual-channel 契約（path validation
//     failure / PNG decode 失敗都走 err channel）— frontend 對 download
//     按鈕 binding 已假設此契約，不可變更。

package gui

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"count_mean/internal/calculator"
	"count_mean/internal/chart"
	"count_mean/internal/manifest"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
	"count_mean/internal/security"
	"count_mean/internal/security/fsperm"
	"count_mean/internal/security/redact"
	"count_mean/internal/synchronizer"
)

// LoadChartComposerSubjectsParams Wails RPC params for subject list lookup.
type LoadChartComposerSubjectsParams struct {
	ManifestPath string `json:"manifestPath"`
	DataFolder   string `json:"dataFolder"`
}

// LoadChartComposerEMGChannelsParams Wails RPC params for EMG channel lookup
// scoped to a specific Subject.
type LoadChartComposerEMGChannelsParams struct {
	ManifestPath string `json:"manifestPath"`
	DataFolder   string `json:"dataFolder"`
	Subject      string `json:"subject"`
}

// GenerateChartComposerParams Wails RPC params for composer chart generation.
//
// SelectedChannels 為前端 checkbox 選擇的 EMG channel name 清單；空 slice 代表
// 「不選任何 channel」（與 chart.RenderComposer 的 empty=fallback-all-channels
// 行為刻意分開 — 前端預設不勾就是不顯示，由 Slice D 控制 UI）。
//
// EMGMotionOffset 由前端從 manifest row metadata 帶過來；handler 不重新從
// manifest 解析（避免兩個來源不一致）。
type GenerateChartComposerParams struct {
	ManifestPath     string   `json:"manifestPath"`
	DataFolder       string   `json:"dataFolder"`
	Subject          string   `json:"subject"`
	SelectedChannels []string `json:"selectedChannels"`
	EMGMotionOffset  int      `json:"emgMotionOffset"`
}

// DownloadChartComposerImageParams Wails RPC params for PNG download.
//
// Base64Data 為前端 canvas.toDataURL() 產出的 `data:image/png;base64,...` 字串；
// OutputPath 為前端 file dialog 選擇的目的地路徑。
type DownloadChartComposerImageParams struct {
	Base64Data string `json:"base64Data"`
	OutputPath string `json:"outputPath"`
}

// ChartComposerSubjectsResult Wails RPC result for subject list lookup.
type ChartComposerSubjectsResult struct {
	Subjects []string `json:"subjects"`
	Success  bool     `json:"success"`
	Message  string   `json:"message"`
}

// ChartComposerChannelsResult Wails RPC result for EMG channel lookup.
//
// HasMuscleRatio 為 V.14 manifest schema 信號：若指定 Subject 對應的 manifest
// row 內 MuscleRatioFile 非空，則前端應顯示 3-grid 模式（EMG + muscle_ratio +
// motion）；空 → 2-grid 模式（EMG + motion）。
type ChartComposerChannelsResult struct {
	Channels       []string `json:"channels"`
	HasMuscleRatio bool     `json:"hasMuscleRatio"`
	Success        bool     `json:"success"`
	Message        string   `json:"message"`
}

// ChartComposerResult Wails RPC result for chart generation.
//
// HTML 為 echarts.NewLine().Render() 的完整 HTML 串（含 inline JS），前端塞進
// iframe srcdoc 顯示。
type ChartComposerResult struct {
	HTML    string `json:"html"`
	Success bool   `json:"success"`
	Message string `json:"message"`
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

// failedChartComposerChannelsResult builds a channels result indicating failure.
func failedChartComposerChannelsResult(message string) *ChartComposerChannelsResult {
	return &ChartComposerChannelsResult{
		Success: false,
		Message: message,
	}
}

// LoadChartComposerSubjects 解析 manifest，回 subject dropdown 列表。
//
// 走 [[HandlerRun[R]]] Tier 1 wrapper：
//   - panic safety 由 recoverHandlerPanic 收乾（panic → named return err，result=nil）
//   - entry/exit log 由 HandlerRun 統一打
//   - body 內 `(result, nil)` 永遠回 non-nil result（單一通道契約）
//
// nil params guard 由 caller 端在 HandlerRun closure 入口先處理，避免裸 deref。
func (a *App) LoadChartComposerSubjects(
	params *LoadChartComposerSubjectsParams,
) (result *ChartComposerSubjectsResult, err error) {
	return HandlerRun(a.logger, "Chart Composer 載入主題", func() (*ChartComposerSubjectsResult, error) {
		// nil params guard — Wails RPC 入口若被惡意呼叫（或前端 bug）傳 nil，
		// 不該 panic。回 failedResult + nil err（單一通道契約）。
		if params == nil {
			return failedChartComposerSubjectsResult("參數為空"), nil
		}

		// DataFolder 必填短路 — validateExternalPathInputs 對空字串視為「跳過驗
		// 證」放行；若 manifest 內 EMG/Motion 走相對路徑，manifest.ResolveEMGFile
		// 會以 process cwd 接相對檔名，讀到 app cwd 的同名檔而非目標病患資料夾，
		// 是 silent visual / data bug。對齊 cci_handlers.validateCCIParams 的
		// ErrNoDataFolder 慣例，在入口 fail-fast。
		if params.DataFolder == "" {
			return failedChartComposerSubjectsResult(ErrNoDataFolder.Error()), nil
		}

		// 邊界路徑驗證 — manifest 與 data folder 都來自前端 file dialog。
		// 對齊 sibling handler (cci / muscle_ratio / phase_sync) 的 defense-in-depth 模式。
		if pathErr := validateExternalPathInputs(
			"分期總檔案", params.ManifestPath,
			"資料夾", params.DataFolder,
		); pathErr != nil {
			return failedChartComposerSubjectsResult(pathErr.Error()), nil
		}

		manifests, parseErr := manifest.LoadManifests(params.ManifestPath)
		if parseErr != nil {
			a.logger.Error("Chart Composer 載入 manifest 失敗", parseErr, map[string]any{})
			return failedChartComposerSubjectsResult(
				fmt.Sprintf("載入分期總檔案失敗: %s", redact.RedactForMessage(parseErr)),
			), nil
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

		return &ChartComposerSubjectsResult{
			Subjects: subjects,
			Success:  true,
			Message:  fmt.Sprintf("已載入 %d 個主題", len(subjects)),
		}, nil
	})
}

// LoadChartComposerEMGChannels 給定 Subject 回 EMG channel checkbox 列表。
//
// HasMuscleRatio 反映 V.14 manifest schema：若該 Subject 對應的 manifest row
// 內 MuscleRatioFile 非空,前端會切到 3-grid (EMG + muscle_ratio + motion) UI。
//
// channel 列表來自 EMG dataset Headers[1:](第一欄是時間)。Headers 順序由
// CSV 檔本身決定,前端不必再排序;預設前端不勾任何 channel(由 Slice D 控制)。
func (a *App) LoadChartComposerEMGChannels(
	params *LoadChartComposerEMGChannelsParams,
) (result *ChartComposerChannelsResult, err error) {
	return HandlerRun(a.logger, "Chart Composer 載入 EMG 通道", func() (*ChartComposerChannelsResult, error) {
		if params == nil {
			return failedChartComposerChannelsResult("參數為空"), nil
		}

		// DataFolder 必填短路（見 LoadChartComposerSubjects 同欄位註解）。
		if params.DataFolder == "" {
			return failedChartComposerChannelsResult(ErrNoDataFolder.Error()), nil
		}

		if pathErr := validateExternalPathInputs(
			"分期總檔案", params.ManifestPath,
			"資料夾", params.DataFolder,
		); pathErr != nil {
			return failedChartComposerChannelsResult(pathErr.Error()), nil
		}

		if params.Subject == "" {
			return failedChartComposerChannelsResult("Subject 不可為空"), nil
		}

		manifests, parseErr := manifest.LoadManifests(params.ManifestPath)
		if parseErr != nil {
			a.logger.Error("Chart Composer 載入 manifest 失敗", parseErr, map[string]any{})
			return failedChartComposerChannelsResult(
				fmt.Sprintf("載入分期總檔案失敗: %s", redact.RedactForMessage(parseErr)),
			), nil
		}

		row, found := findManifestBySubject(manifests, params.Subject)
		if !found {
			return failedChartComposerChannelsResult(
				fmt.Sprintf("Subject %q 不存在於分期總檔案", params.Subject),
			), nil
		}

		emgPath, resolveErr := manifest.ResolveEMGFile(params.DataFolder, row.EMGFile)
		if resolveErr != nil {
			a.logger.Error("Chart Composer 解析 EMG 路徑失敗", resolveErr, map[string]any{})
			return failedChartComposerChannelsResult(
				fmt.Sprintf("EMG 檔案路徑解析失敗: %s", redact.RedactForMessage(resolveErr)),
			), nil
		}

		emgData, _, emgErr := parsers.NewEMGParser().ParseFile(emgPath)
		if emgErr != nil {
			a.logger.Error("Chart Composer 解析 EMG 失敗", emgErr, map[string]any{})
			return failedChartComposerChannelsResult(
				fmt.Sprintf("解析 EMG 失敗: %s", redact.RedactForMessage(emgErr)),
			), nil
		}

		// emg.Headers 已是 channel name slice(parsers.initEMGData 已 strip
		// time header,直接給前端即可)。
		channels := make([]string, len(emgData.Headers))
		copy(channels, emgData.Headers)

		return &ChartComposerChannelsResult{
			Channels:       channels,
			HasMuscleRatio: strings.TrimSpace(row.MuscleRatioFile) != "",
			Success:        true,
			Message:        fmt.Sprintf("已載入 %d 個 EMG 通道", len(channels)),
		}, nil
	})
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
// EMGMotionOffset 由前端從 manifest row 傳入(避免 handler 與前端對同一 manifest
// row 解讀不一致)。motion-index 轉時間透過 TimeSynchronizer.MotionIndexToEMGTime。
func (a *App) GenerateChartComposer(
	params *GenerateChartComposerParams,
) (result *ChartComposerResult, err error) {
	return HandlerRun(a.logger, "Chart Composer 圖表生成", func() (*ChartComposerResult, error) {
		if params == nil {
			return failedChartComposerResult("參數為空"), nil
		}

		// DataFolder 必填短路（見 LoadChartComposerSubjects 同欄位註解）。
		// 順序：nil → DataFolder → boundary path → Subject → SelectedChannels；
		// 邊界路徑優先於業務參數對齊 sibling handler(cci / muscle_ratio)的
		// fail-fast 順序，避免 traversal-path test 被業務參數 short-circuit 蓋掉。
		if params.DataFolder == "" {
			return failedChartComposerResult(ErrNoDataFolder.Error()), nil
		}

		if pathErr := validateExternalPathInputs(
			"分期總檔案", params.ManifestPath,
			"資料夾", params.DataFolder,
		); pathErr != nil {
			return failedChartComposerResult(pathErr.Error()), nil
		}

		if params.Subject == "" {
			return failedChartComposerResult("Subject 不可為空"), nil
		}

		manifests, parseErr := manifest.LoadManifests(params.ManifestPath)
		if parseErr != nil {
			a.logger.Error("Chart Composer 載入 manifest 失敗", parseErr, map[string]any{})
			return failedChartComposerResult(
				fmt.Sprintf("載入分期總檔案失敗: %s", redact.RedactForMessage(parseErr)),
			), nil
		}

		row, found := findManifestBySubject(manifests, params.Subject)
		if !found {
			return failedChartComposerResult(
				fmt.Sprintf("Subject %q 不存在於分期總檔案", params.Subject),
			), nil
		}

		// SelectedChannels 空 slice → fail-fast。
		// chart.RenderComposer 內部對 empty SelectedChannels 走「fallback 全
		// channel」(buildEMGSeries line 235-241)是 composer 合理 default;但
		// Slice D frontend 預設「不勾」,使用者沒勾就不該渲染任何 EMG。handler
		// 是兩個語意之間的守門點 — 在這裡 fail-fast 讓 UI 行為一致。
		// 放在 manifest+subject 解析之後 — 維持「邊界路徑 → 結構性檢查 → 業務
		// 參數」的 fail-fast 順序;對 sibling test (UnknownSubject /
		// RejectsTraversalPath) 不形成 short-circuit 蓋過。
		if len(params.SelectedChannels) == 0 {
			return failedChartComposerResult("請至少選擇一個 EMG 通道"), nil
		}

		// 載入 EMG(必要)
		emgPath, emgPathErr := manifest.ResolveEMGFile(params.DataFolder, row.EMGFile)
		if emgPathErr != nil {
			a.logger.Error("Chart Composer 解析 EMG 路徑失敗", emgPathErr, map[string]any{})
			return failedChartComposerResult(
				fmt.Sprintf("EMG 檔案路徑解析失敗: %s", redact.RedactForMessage(emgPathErr)),
			), nil
		}
		emgPhaseSync, _, emgErr := parsers.NewEMGParser().ParseFile(emgPath)
		if emgErr != nil {
			a.logger.Error("Chart Composer 解析 EMG 失敗", emgErr, map[string]any{})
			return failedChartComposerResult(
				fmt.Sprintf("解析 EMG 失敗: %s", redact.RedactForMessage(emgErr)),
			), nil
		}
		emgDataset := phaseSyncEMGToDataset(emgPhaseSync)

		// 載入 motion(必要 — composer 至少 2-grid,motion 是其中一個 grid)
		composerMotion, motionErr := loadComposerMotion(
			params.DataFolder, row.MotionFile, params.EMGMotionOffset,
		)
		if motionErr != nil {
			a.logger.Error("Chart Composer 解析 Motion 失敗", motionErr, map[string]any{})
			return failedChartComposerResult(
				fmt.Sprintf("解析 Motion 失敗: %s", redact.RedactForMessage(motionErr)),
			), nil
		}

		// 載入 muscle_ratio(可選 — 僅 V.14 manifest 帶 MuscleRatioFile)
		var muscleRatioData *chart.MuscleRatioData
		if strings.TrimSpace(row.MuscleRatioFile) != "" {
			mr, mrErr := loadComposerMuscleRatio(params.DataFolder, row.MuscleRatioFile)
			if mrErr != nil {
				a.logger.Error("Chart Composer 解析 muscle_ratio 失敗", mrErr, map[string]any{})
				return failedChartComposerResult(
					fmt.Sprintf("解析 muscle_ratio 失敗: %s", redact.RedactForMessage(mrErr)),
				), nil
			}
			muscleRatioData = mr
		}

		// PhasePoints (P0/P1/P2/S/C/T0/T/L) 在 manifest 內為**力板時間** domain
		// 秒值（見 internal/models/phase_sync_models.go PhasePoints 註解）。Chart
		// Composer 的所有 grid X 軸是 **EMG 時間** domain；若不換算直接 attach,
		// markLine 會早 / 晚整個 sync offset(`(EMGMotionOffset - 1) / 250` 秒)—
		// silent visual bug。對齊 synchronizer.TimeSynchronizer.ForceTimeToEMGTime
		// 公式逐欄換算;Set=false 的 OptFloat 保持 NoOpt 不污染 markLine。
		emgPhase := convertPhasePointsToEMGTime(row.PhasePoints, params.EMGMotionOffset)

		composerInput := chart.ComposerInput{
			Subject:          row.Subject,
			EMGDataset:       emgDataset,
			SelectedChannels: params.SelectedChannels,
			MuscleRatioData:  muscleRatioData,
			MotionData:       composerMotion,
			PhasePoints:      emgPhase,
			EMGMotionOffset:  params.EMGMotionOffset,
		}

		var buf bytes.Buffer
		if renderErr := chart.RenderComposer(a.context(), composerInput, &buf); renderErr != nil {
			// EMG-required 是 caller bug(我們上面剛確保 emgDataset 非 nil)而非
			// user-visible 路徑,直接走通用錯誤訊息;ctx cancel 走相同 path。
			if errors.Is(renderErr, chart.ErrComposerEMGRequired) {
				return failedChartComposerResult("內部錯誤: EMG dataset 為空"), nil
			}
			return failedChartComposerResult(
				fmt.Sprintf("圖表生成失敗: %s", redact.RedactForMessage(renderErr)),
			), nil
		}

		return &ChartComposerResult{
			HTML:    buf.String(),
			Success: true,
			Message: "圖表生成完成",
		}, nil
	})
}

// DownloadChartComposerImage 接前端 base64 PNG dataURL 並寫到指定路徑。
//
// 不走 HandlerRun — 此 handler 的 short-circuit 順序敏感（PNG decode 先於
// path validation，避免在合法路徑寫進非 PNG 內容）,鏡像 `DownloadCCIChart`
// 既有獨立模式而非走 template wrap。
//
// 安全鏈(依序):
//  1. nil params guard
//  2. base64 → DecodeAndValidatePNG(三層守門:size cap / magic signature / IHDR)
//  3. SanitizeFileName 對 OutputPath 的 base 部分(防 traversal segment 滲入)
//  4. validateExternalPathInputs 對完整 OutputPath(防 sensitive dir prefix)
//  5. fsperm.WriteFileNoFollow(防 symlink follow)
//
// 與 DownloadCCIChart 的差異:OutputPath 由前端 file dialog 完整給出(包含
// 目錄 + 檔名),不是「OutputDir + sanitize(Subject) + ext」;因此 sanitize
// 只 apply 在 filepath.Base(OutputPath),保留目錄部分原樣交給 path validator。
func (a *App) DownloadChartComposerImage(
	params *DownloadChartComposerImageParams,
) (result *ChartResult, err error) {
	defer recoverHandlerPanic("DownloadChartComposerImage", a.logger, &err)

	a.logger.Info("開始下載 Chart Composer 圖表", nil)

	if params == nil {
		return nil, errors.New("參數為空")
	}

	dataURL := params.Base64Data
	if !strings.HasPrefix(dataURL, "data:image/png;base64,") {
		return nil, ErrInvalidImageFormat
	}

	base64Data := strings.TrimPrefix(dataURL, "data:image/png;base64,")

	pngData, decodeErr := DecodeAndValidatePNG(base64Data)
	if decodeErr != nil {
		return nil, fmt.Errorf("PNG 驗證失敗: %w", decodeErr)
	}

	// SanitizeFileName 只處理 base file 名稱,目錄部分保留原樣供 path validator
	// 二次驗證。鏡像 DownloadCCIChart:params.Subject 是「檔名片段」,DownloadCCIChart
	// 也是 calculator.SanitizeFileName(params.Subject)。
	outputDir := filepath.Dir(params.OutputPath)
	outputBase := calculator.SanitizeFileName(filepath.Base(params.OutputPath))
	if outputBase == "" {
		return nil, errors.New("輸出檔名不可為空")
	}
	// 確保副檔名 .png — Chart Composer 一律輸出 PNG(對齊前端 toDataURL)。
	if !strings.HasSuffix(strings.ToLower(outputBase), ".png") {
		outputBase += ".png"
	}
	outputPath := filepath.Join(outputDir, outputBase)

	// boundary 路徑驗證 — 對齊 DownloadCCIChart:label 一次到位帶
	// "PNG 輸出路徑",caller 不再外層 wrap,避免重複 label。
	if pathErr := validateExternalPathInputs("PNG 輸出路徑", outputPath); pathErr != nil {
		return nil, pathErr
	}

	if writeErr := fsperm.WriteFileNoFollow(outputPath, pngData); writeErr != nil {
		return nil, fmt.Errorf("保存圖片失敗: %w", writeErr)
	}

	a.logger.Info("Chart Composer 圖表下載完成", map[string]any{
		"output": outputPath,
	})

	return &ChartResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    fmt.Sprintf("圖表已下載至: %s", outputPath),
	}, nil
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
// 路徑驗證走 manifest.ResolveEMGFile 同樣的 lenient path resolver(對齊
// muscle_ratio analyzer 內的 motion file 處理風格)。
func loadComposerMotion(
	dataFolder, motionFile string, emgMotionOffset int,
) (*chart.MotionData, error) {
	if strings.TrimSpace(motionFile) == "" {
		// motion 是必要 grid;空檔名直接 fail。
		return nil, errors.New("manifest 內 MotionFile 為空")
	}

	motionPath, err := manifest.ResolveEMGFile(dataFolder, motionFile)
	if err != nil {
		return nil, fmt.Errorf("Motion 路徑解析失敗: %w", err)
	}

	motion, err := parsers.NewMotionParser().ParseFile(motionPath)
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

// loadComposerMuscleRatio 從 muscle_ratio CSV 載入並轉成 chart.MuscleRatioData。
//
// CSV layout(由 muscle_ratio.writeOutputAll 產出):
//
//	Time (s), RA/ES, IL/GMax, RF/BF, TAIO/MF
//	0.000000, 0.123456, ..., ...
//
// header 第一欄為時間,其餘為 ratio pair 名稱。caller 把這四欄 series 餵進
// `chart.MuscleRatioData` 即可在 muscle_ratio grid 顯示。
//
// 路徑解析走 security.ResolveLenientPath(透過 manifest.ResolveEMGFile)同樣
// 支援 BTS 匯出含字面 "%" 的檔名。
func loadComposerMuscleRatio(dataFolder, muscleRatioFile string) (*chart.MuscleRatioData, error) {
	if strings.TrimSpace(muscleRatioFile) == "" {
		// caller 應在呼叫前已檢查;這裡是 defense-in-depth。
		return nil, errors.New("manifest 內 MuscleRatioFile 為空")
	}

	// 解析 lenient path(對齊 manifest.ResolveEMGFile 但拒絕傳出 ErrManifestEMGFileMissing,
	// 這裡是 muscle_ratio 不是 EMG,錯誤訊息要對應)。
	resolvedBase := dataFolder
	if r, err := filepath.EvalSymlinks(dataFolder); err == nil {
		resolvedBase = r
	}
	muscleRatioPath, err := security.ResolveLenientPath(resolvedBase, muscleRatioFile)
	if err != nil {
		return nil, fmt.Errorf("muscle_ratio 路徑解析失敗: %w", err)
	}

	records, err := parsers.ReadCSVDirect(muscleRatioPath)
	if err != nil {
		return nil, fmt.Errorf("讀取 muscle_ratio CSV 失敗: %w", err)
	}
	if len(records) < 2 {
		return nil, errors.New("muscle_ratio CSV 為空或缺少資料行")
	}

	header := records[0]
	if len(header) < 2 {
		return nil, errors.New("muscle_ratio CSV 標題不足: 至少需要時間欄與一個 ratio 欄")
	}

	dataRows := records[1:]
	times := make([]float64, 0, len(dataRows))
	order := make([]string, 0, len(header)-1)
	series := make(map[string][]float64, len(header)-1)
	for j := 1; j < len(header); j++ {
		name := strings.TrimSpace(header[j])
		if name == "" {
			continue
		}
		order = append(order, name)
		series[name] = make([]float64, 0, len(dataRows))
	}

	for _, row := range dataRows {
		if len(row) < len(header) {
			// jagged row 直接 skip(對齊 EMG parser 風格)
			continue
		}
		t, ok := parseFloatCell(row[0])
		if !ok {
			continue
		}
		times = append(times, t)
		for j := 1; j < len(header); j++ {
			name := strings.TrimSpace(header[j])
			if name == "" {
				continue
			}
			// 空 / 不可解析 cell → NaN(不是 0)。muscle_ratio writer 對 NaN/Inf
			// 寫成空 cell;若 parse 失敗 silent 給 0,等於把缺值「畫成真實 0」,
			// 對共收縮比值研究是嚴重誤導。NaN 走 composer
			// buildComposerLineData line 692-695:`LineData{Value: nil}`,
			// 序列化(opts.LineData.Value `json:"value,omitempty"`)整個 value
			// 欄位 omit;echarts 渲染為 line gap,正確反映缺值。
			v, ok := parseFloatCell(row[j])
			if !ok {
				v = math.NaN()
			}
			series[name] = append(series[name], v)
		}
	}

	return &chart.MuscleRatioData{
		Time:   times,
		Series: series,
		Order:  order,
	}, nil
}

// convertPhasePointsToEMGTime 把 PhasePoints 內 8 個 OptFloat 力板時間欄位
// (P0/P1/P2/S/C/T0/T/L)換算成 EMG-time domain 秒值。
//
// 換算公式來自 synchronizer.TimeSynchronizer.ForceTimeToEMGTime:
//
//	emgTime = forceTime - (emgMotionOffset - 1) / FrequencyMotion
//
// EMGMotionOffset 在 manifest 內單位是「EMG 起點對應的 motion frame index」
// (1-based),非秒;FrequencyMotion = 250 Hz(parsers.FrequencyMotion)。
//
// Set=false 的 OptFloat 保持 NoOpt,不會出現在 markLine 上 — 換算
// 「未提供」會把它變成有限值並 inject 一條偽 marker,違反 OptFloat 契約。
//
// D / O(motion-index sentinel int 欄位)維持原樣回傳;composer 本來就
// 不對它們作 markLine,handler 也不負責 motion-index → 秒值的轉換(那是
// 上游 parser / synchronizer 的職責)。
//
// 為何 reuse synchronizer.TimeSynchronizer 而非寫死公式:換算規則是 cross-
// cutting domain knowledge,單一來源。
func convertPhasePointsToEMGTime(
	src models.PhasePoints, emgMotionOffset int,
) models.PhasePoints {
	ts := synchronizer.NewTimeSynchronizer()
	conv := func(o models.OptFloat) models.OptFloat {
		if v, ok := o.Get(); ok {
			return models.MakeOpt(ts.ForceTimeToEMGTime(v, emgMotionOffset))
		}
		return models.NoOpt()
	}
	return models.PhasePoints{
		P0: conv(src.P0),
		P1: conv(src.P1),
		P2: conv(src.P2),
		S:  conv(src.S),
		C:  conv(src.C),
		D:  src.D, // motion-index 不換算
		T0: conv(src.T0),
		T:  conv(src.T),
		O:  src.O, // motion-index 不換算
		L:  conv(src.L),
	}
}

// parseFloatCell parses a CSV cell as float64. Empty / unparseable cells become
// NaN-sentinel via `_, ok := false`. We accept "NaN" as NaN-sentinel(對齊
// muscle_ratio CSV writer 對 NaN/Inf 寫空字串的行為)。
//
// 不直接借用 parsers.ParseFloatCell 是因為其位於 parsers 套件 — gui 已對
// parsers 依賴一條;這個 thin wrapper 把 strconv.ParseFloat error 收成 bool,
// 與 ParseFloatCell signature 對齊,避免 caller 端散落 ParseFloat 樣板。
func parseFloatCell(cell string) (float64, bool) {
	s := strings.TrimSpace(cell)
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

