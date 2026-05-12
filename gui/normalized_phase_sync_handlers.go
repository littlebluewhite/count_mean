package gui

import (
	"fmt"
	"path/filepath"

	"count_mean/internal/calculator"
	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// normalizedPhaseSyncPrecision 為 Output 1 (標準化 EMG CSV) 的小數位數，
// 與分期同步分析統計輸出 (defaultEMGStatsPrecision = 6) 保持一致。
const normalizedPhaseSyncPrecision = 6

// NormalizedPhaseSyncParams 標準化分期同步分析參數。
//
// 標準化視窗（Norm*）與統計視窗（Stats*）為兩組獨立的分期區間：
//   - Norm 視窗用於計算每條肌肉的最大值（標準化的除數）
//   - Stats 視窗用於擷取最終要輸出統計的時間範圍
//
// 兩組區間可以重疊或完全分離，由使用者自行選擇；後端不檢查互相關係。
type NormalizedPhaseSyncParams struct {
	ManifestFile    string `json:"manifestFile"`
	DataFolder      string `json:"dataFolder"`
	SubjectIndex    int    `json:"subjectIndex"`
	NormStartPhase  string `json:"normStartPhase"`
	NormEndPhase    string `json:"normEndPhase"`
	StatsStartPhase string `json:"statsStartPhase"`
	StatsEndPhase   string `json:"statsEndPhase"`
}

// NormalizedPhaseSyncResult 標準化分期同步分析結果。
//
// Norm* 與 Stats* 分別反映標準化視窗與統計視窗的分期點與實際時間，
// 兩組獨立顯示供 UI 區分。
type NormalizedPhaseSyncResult struct {
	NormalizedEMGPath string             `json:"normalizedEMGPath"`
	PhaseSyncCSVPath  string             `json:"phaseSyncCSVPath"`
	Subject           string             `json:"subject"`
	NormStartPhase    string             `json:"normStartPhase"`
	NormEndPhase      string             `json:"normEndPhase"`
	NormStartTime     float64            `json:"normStartTime"`
	NormEndTime       float64            `json:"normEndTime"`
	StatsStartPhase   string             `json:"statsStartPhase"`
	StatsEndPhase     string             `json:"statsEndPhase"`
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
func (a *App) AnalyzeNormalizedPhaseSync(params NormalizedPhaseSyncParams) (*NormalizedPhaseSyncResult, error) {
	a.logger.Info("開始標準化分期同步分析", map[string]interface{}{"params": params})

	if err := validateNormalizedPhaseSyncParams(params); err != nil {
		return nil, err
	}

	// 1. 載入 manifest 與 EMG（共用兩組區間的前置步驟）
	baseParams := &models.AnalysisParams{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		StartPhase:   params.NormStartPhase,
		EndPhase:     params.NormEndPhase,
		SubjectIndex: params.SubjectIndex,
	}

	loaded, err := a.phaseSyncAnalyzer.Load(baseParams)
	if err != nil {
		a.logger.Error("載入分期同步資料失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("載入資料失敗: %v", err)), nil
	}

	// 2. 分別解析標準化視窗與統計視窗（兩組獨立、不互相驗證）
	normRange, err := a.phaseSyncAnalyzer.ResolvePhaseRange(loaded, params.NormStartPhase, params.NormEndPhase)
	if err != nil {
		a.logger.Error("標準化區間解析失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("標準化區間: %v", err)), nil
	}

	statsRange, err := a.phaseSyncAnalyzer.ResolvePhaseRange(loaded, params.StatsStartPhase, params.StatsEndPhase)
	if err != nil {
		a.logger.Error("統計區間解析失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("統計區間: %v", err)), nil
	}

	// 3. 用 normRange 做標準化（除數來自此區間每條肌肉的最大值）
	normalizer := calculator.NewRangeNormalizer()

	normalizedData, channelMaxes, err := normalizer.NormalizeByRangeMax(
		loaded.EMGData,
		normRange.StartTime,
		normRange.EndTime,
	)
	if err != nil {
		a.logger.Error("區間最大值標準化失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("標準化失敗: %v", err)), nil
	}

	// 4. 撰寫 Output 1：標準化後的 EMG CSV
	safeSubject := safeSubjectName(loaded.Manifest.Subject)
	normalizedEMGPath := filepath.Join(
		a.config.OutputDir,
		fmt.Sprintf("%s_normalized.csv", safeSubject),
	)

	if err := parsers.ExportPhaseSyncDataToCSV(normalizedData, normalizedEMGPath, normalizedPhaseSyncPrecision); err != nil {
		a.logger.Error("寫入標準化 EMG 失敗", err, map[string]interface{}{"path": normalizedEMGPath})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("寫入標準化 EMG 失敗: %v", err)), nil
	}

	// 5. 用 statsRange 擷取標準化後的資料 + 計算統計
	emgParser := parsers.NewEMGParser()

	rangeResult, err := emgParser.GetDataInTimeRange(
		normalizedData,
		statsRange.StartTime,
		statsRange.EndTime,
	)
	if err != nil {
		a.logger.Error("擷取標準化資料統計區間失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("擷取統計區間失敗: %v", err)), nil
	}

	statsCalc := calculator.NewEMGStatisticsCalculator(normalizedPhaseSyncPrecision)

	stats, err := statsCalc.CalculateStatistics(
		rangeResult.Data,
		params.StatsStartPhase,
		rangeResult.ActualStartTime,
		params.StatsEndPhase,
		rangeResult.ActualEndTime,
		loaded.Manifest.Subject,
	)
	if err != nil {
		a.logger.Error("計算標準化統計失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("計算統計失敗: %v", err)), nil
	}

	// 6. 撰寫 Output 2：標準化資料的分期同步統計 CSV
	// 檔名同時帶 norm 與 stats 兩組分期點，避免不同設定的輸出互相覆蓋或混淆。
	phaseSyncCSVPath := filepath.Join(
		a.config.OutputDir,
		fmt.Sprintf(
			"%s_normalized_norm-%s-%s_stats-%s-%s.csv",
			safeSubject,
			params.NormStartPhase, params.NormEndPhase,
			params.StatsStartPhase, params.StatsEndPhase,
		),
	)

	if err := statsCalc.ExportToCSV(stats, phaseSyncCSVPath); err != nil {
		a.logger.Error("寫入標準化統計 CSV 失敗", err, map[string]interface{}{"path": phaseSyncCSVPath})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("寫入統計失敗: %v", err)), nil
	}

	result := &NormalizedPhaseSyncResult{
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
	}

	a.logger.Info("標準化分期同步分析完成", map[string]interface{}{
		"normalizedEMG": normalizedEMGPath,
		"phaseSyncCSV":  phaseSyncCSVPath,
	})

	return result, nil
}

// validateNormalizedPhaseSyncParams 檢查必填欄位。沿用既有錯誤型別保持訊息一致。
// 兩組分期點（Norm 與 Stats）只要任一組未填齊就回 ErrNoPhaseSelection。
func validateNormalizedPhaseSyncParams(params NormalizedPhaseSyncParams) error {
	if params.ManifestFile == "" {
		return ErrNoManifestFile
	}

	if params.DataFolder == "" {
		return ErrNoDataFolder
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

// safeSubjectName 將 subject 名稱中可能造成檔案系統問題的字元換成底線。
// 與 calculator.sanitizeFileName 邏輯一致，但保留在 gui 套件內避免改動 calculator API。
func safeSubjectName(name string) string {
	replacements := map[rune]rune{
		'/':  '_',
		'\\': '_',
		':':  '_',
		'*':  '_',
		'?':  '_',
		'"':  '_',
		'<':  '_',
		'>':  '_',
		'|':  '_',
		' ':  '_',
	}

	result := make([]rune, 0, len(name))

	for _, ch := range name {
		if replacement, ok := replacements[ch]; ok {
			result = append(result, replacement)
		} else {
			result = append(result, ch)
		}
	}

	return string(result)
}
