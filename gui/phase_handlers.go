package gui

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"count_mean/internal/calculator"
	"count_mean/internal/io"
	"count_mean/internal/models"
)

// validatePhaseParams validates phase analysis parameters and returns the phase
// labels and time ranges parsed from params.Phases. 名稱與邊界皆來自前端傳入,
// 不再耦合 config.PhaseLabels。
func validatePhaseParams(params PhaseParams) (labels []string, ranges []models.TimeRange, err error) {
	if params.InputFile == "" {
		return nil, nil, ErrNoInputFile
	}

	if len(params.Phases) == 0 {
		return nil, nil, ErrNoPhaseLabels
	}

	labels = make([]string, 0, len(params.Phases))
	ranges = make([]models.TimeRange, 0, len(params.Phases))

	for _, ph := range params.Phases {
		name := strings.TrimSpace(ph.Name)
		if name == "" {
			return nil, nil, ErrNoValidPhaseLabels
		}

		// 邊界以原始字串收,用 strconv.ParseFloat 嚴格解析整串:拒空字串 / 部分解析
		// (前端 parseFloat 會把 "1.2foo" 截成 1.2、"0x10" 截成 0,這類截斷誤輸入若以
		// 數字傳入會被當合法值)/ 非數值。
		start, errStart := strconv.ParseFloat(strings.TrimSpace(ph.StartTime), 64)
		end, errEnd := strconv.ParseFloat(strings.TrimSpace(ph.EndTime), 64)
		if errStart != nil || errEnd != nil {
			return nil, nil, ErrInvalidPhaseRange
		}

		// 邊界須有限且 start < end:`!(start < end)` 同時擋 NaN(NaN 的任何比較皆為
		// false)與反序 / 零長度;另顯式擋 ±Inf —— ParseFloat 接受 "Inf",且
		// `-Inf < +Inf` 為 true 會繞過上式。
		if math.IsInf(start, 0) || math.IsInf(end, 0) || !(start < end) {
			return nil, nil, ErrInvalidPhaseRange
		}

		labels = append(labels, name)
		ranges = append(ranges, models.TimeRange{Start: start, End: end})
	}

	return labels, ranges, nil
}

// generatePhaseOutputName generates the output filename for phase analysis.
func generatePhaseOutputName(inputFile, outputPath string) string {
	baseName := TrimCSVExtension(filepath.Base(inputFile))
	return resolveOutputName(outputPath, baseName, SuffixPhaseAnalysis)
}

// AnalyzePhases performs phase analysis.
//
// 錯誤通道契約 (`(result, err)` dual channel):validate / 讀檔 / 分析 / 寫檔任一步
// 失敗都回 (nil, err);panic 由首句 defer recoverHandlerPanic 轉成 ErrInternalPanic
// 走 err。
func (a *App) AnalyzePhases(params PhaseParams) (result *PhaseResult, err error) {
	defer recoverHandlerPanic("階段分析", a.logger, &err)

	s := a.state.Load()

	a.logger.Info("階段分析參數", map[string]any{
		"input_file":  params.InputFile,
		"phase_count": len(params.Phases),
		"output_path": params.OutputPath,
	})
	a.logger.Info("開始階段分析", nil)

	// 1 validate:labels / ranges 皆來自前端 params.Phases(名稱與時間區間),
	// 不耦合 config.PhaseLabels。
	labels, ranges, validateErr := validatePhaseParams(params)
	if validateErr != nil {
		return nil, validateErr
	}

	// 2 讀檔
	records, readErr := a.readCSVWithPathValidation(s, params.InputFile, s.config.InputDir)
	if readErr != nil {
		return nil, fmt.Errorf("讀取資料檔案失敗: %w", readErr)
	}

	// 3 分析:per-call analyzer,phase 名稱用前端 labels、邊界用前端 ranges
	// (AnalyzeFromRawDataWithRanges 不從字串解析時間點),與 config.PhaseLabels 解耦。
	analyzer := calculator.NewPhaseAnalyzer(s.config.ScalingFactor, labels)

	analysisResult, analyzeErr := analyzer.AnalyzeFromRawDataWithRanges(records, ranges)
	if analyzeErr != nil {
		return nil, fmt.Errorf("階段分析失敗: %w", analyzeErr)
	}

	// 4 寫檔。Multi-phase merge 與 time-index dedup 由 CSVHandler.WritePhaseAnalysis
	// 吸進 io 套件 — 此前 caller 需要自己 phaseRows[1:] skip header 與 fullRows[3:]
	// dedup time row, 那層 row layout leakage 已經消失。
	outputName := generatePhaseOutputName(params.InputFile, params.OutputPath)

	outputPath, writeErr := s.csvHandler.WritePhaseAnalysis(
		io.WriteRequest{Filename: outputName}, records[0], analysisResult,
	)
	if writeErr != nil {
		return nil, fmt.Errorf("保存結果失敗: %w", writeErr)
	}

	a.logger.Info("階段分析完成", nil)

	a.logger.Info("階段分析輸出", map[string]any{
		"output_file":   outputPath,
		"phase_count":   len(analysisResult.PhaseResults),
		"channel_count": len(records[0]) - 1,
	})

	return &PhaseResult{
		OutputPath: outputPath,
		Success:    true,
		Message:    fmt.Sprintf("階段分析成功完成，結果已保存到: %s", filepath.Base(outputPath)),
	}, nil
}

// PhaseSpec 是單一分期的名稱與時間邊界,對應前端送出的 {name, startTime, endTime}。
//
// StartTime/EndTime 收「原始字串」:前端送使用者輸入的原字串,後端以 strconv.ParseFloat
// 嚴格解析整串。前端 parseFloat 會把 "1.2foo" 截成 1.2、"0x10" 截成 0、非數值成 NaN,
// 這些部分解析 / 截斷的誤輸入若以數字傳入會被當合法值;改收字串 + 嚴格解析可一律拒絕
// (含空字串 / "1.2foo" / "0x10")。validatePhaseParams 另擋 ParseFloat 仍接受的 ±Inf / NaN。
type PhaseSpec struct {
	Name      string `json:"name"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
}

// PhaseParams holds parameters for phase analysis. Phases 由前端提供(名稱 + 邊界),
// 後端據此分析並以該名稱標記結果,不再耦合 config.PhaseLabels。
type PhaseParams struct {
	InputFile  string      `json:"inputFile"`
	Phases     []PhaseSpec `json:"phases"`
	OutputPath string      `json:"outputPath"`
}

// PhaseResult holds the result of phase analysis.
type PhaseResult struct {
	OutputPath string `json:"outputPath"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
}
