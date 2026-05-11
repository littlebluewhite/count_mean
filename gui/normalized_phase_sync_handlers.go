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
type NormalizedPhaseSyncParams struct {
	ManifestFile string `json:"manifestFile"`
	DataFolder   string `json:"dataFolder"`
	StartPhase   string `json:"startPhase"`
	EndPhase     string `json:"endPhase"`
	SubjectIndex int    `json:"subjectIndex"`
}

// NormalizedPhaseSyncResult 標準化分期同步分析結果。
type NormalizedPhaseSyncResult struct {
	NormalizedEMGPath string             `json:"normalizedEMGPath"`
	PhaseSyncCSVPath  string             `json:"phaseSyncCSVPath"`
	Subject           string             `json:"subject"`
	StartPhase        string             `json:"startPhase"`
	EndPhase          string             `json:"endPhase"`
	StartTime         float64            `json:"startTime"`
	EndTime           float64            `json:"endTime"`
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
//  1. 共用 PhaseSyncAnalyzer.LoadAndExtractRange 取得驗證後的 EMG 資料與分期時間區間
//  2. 以每條肌肉在 [startTime, endTime] 內的最大值為除數，對整段資料做標準化
//  3. 將標準化資料輸出為 Output 1：{subject}_normalized.csv
//  4. 對標準化後的資料於同一分期區間內計算統計（mean/max）
//  5. 將統計結果輸出為 Output 2：{subject}_normalized_{start}_{end}.csv
//     （欄位與既有「分期同步分析」相同）
func (a *App) AnalyzeNormalizedPhaseSync(params NormalizedPhaseSyncParams) (*NormalizedPhaseSyncResult, error) {
	a.logger.Info("開始標準化分期同步分析", map[string]interface{}{"params": params})

	if err := validateNormalizedPhaseSyncParams(params); err != nil {
		return nil, err
	}

	analysisParams := &models.AnalysisParams{
		ManifestFile: params.ManifestFile,
		DataFolder:   params.DataFolder,
		StartPhase:   params.StartPhase,
		EndPhase:     params.EndPhase,
		SubjectIndex: params.SubjectIndex,
	}

	loaded, err := a.phaseSyncAnalyzer.LoadAndExtractRange(analysisParams)
	if err != nil {
		a.logger.Error("載入分期同步資料失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("載入資料失敗: %v", err)), nil
	}

	normalizer := calculator.NewRangeNormalizer()

	normalizedData, channelMaxes, err := normalizer.NormalizeByRangeMax(
		loaded.EMGData,
		loaded.PhaseTimeRange.StartTime,
		loaded.PhaseTimeRange.EndTime,
	)
	if err != nil {
		a.logger.Error("區間最大值標準化失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("標準化失敗: %v", err)), nil
	}

	// 撰寫 Output 1：標準化後的 EMG CSV
	safeSubject := safeSubjectName(loaded.Manifest.Subject)
	normalizedEMGPath := filepath.Join(
		a.config.OutputDir,
		fmt.Sprintf("%s_normalized.csv", safeSubject),
	)

	if err := parsers.ExportPhaseSyncDataToCSV(normalizedData, normalizedEMGPath, normalizedPhaseSyncPrecision); err != nil {
		a.logger.Error("寫入標準化 EMG 失敗", err, map[string]interface{}{"path": normalizedEMGPath})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("寫入標準化 EMG 失敗: %v", err)), nil
	}

	// 對標準化後的資料於同一分期區間計算統計
	emgParser := parsers.NewEMGParser()

	rangeResult, err := emgParser.GetDataInTimeRange(
		normalizedData,
		loaded.PhaseTimeRange.StartTime,
		loaded.PhaseTimeRange.EndTime,
	)
	if err != nil {
		a.logger.Error("擷取標準化資料分期區間失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("擷取分期區間失敗: %v", err)), nil
	}

	statsCalc := calculator.NewEMGStatisticsCalculator(normalizedPhaseSyncPrecision)

	stats, err := statsCalc.CalculateStatistics(
		rangeResult.Data,
		params.StartPhase,
		rangeResult.ActualStartTime,
		params.EndPhase,
		rangeResult.ActualEndTime,
		loaded.Manifest.Subject,
	)
	if err != nil {
		a.logger.Error("計算標準化統計失敗", err, map[string]interface{}{})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("計算統計失敗: %v", err)), nil
	}

	// 撰寫 Output 2：標準化資料的分期同步統計 CSV
	phaseSyncCSVPath := filepath.Join(
		a.config.OutputDir,
		fmt.Sprintf("%s_normalized_%s_%s.csv", safeSubject, params.StartPhase, params.EndPhase),
	)

	if err := statsCalc.ExportToCSV(stats, phaseSyncCSVPath); err != nil {
		a.logger.Error("寫入標準化統計 CSV 失敗", err, map[string]interface{}{"path": phaseSyncCSVPath})
		return failedNormalizedPhaseSyncResult(fmt.Sprintf("寫入統計失敗: %v", err)), nil
	}

	result := &NormalizedPhaseSyncResult{
		NormalizedEMGPath: normalizedEMGPath,
		PhaseSyncCSVPath:  phaseSyncCSVPath,
		Subject:           stats.Subject,
		StartPhase:        stats.StartPhase,
		EndPhase:          stats.EndPhase,
		StartTime:         stats.StartTime,
		EndTime:           stats.EndTime,
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
func validateNormalizedPhaseSyncParams(params NormalizedPhaseSyncParams) error {
	if params.ManifestFile == "" {
		return ErrNoManifestFile
	}

	if params.DataFolder == "" {
		return ErrNoDataFolder
	}

	if params.StartPhase == "" || params.EndPhase == "" {
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
