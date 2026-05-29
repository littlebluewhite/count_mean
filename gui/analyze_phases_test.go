package gui

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// TestAnalyzePhases_HonorsFrontendPhasesAndNames 釘住 fe_core 完整修(whole-project
// review P1)。前端「階段分析」panel 送 {inputFile, phases:[{name,startTime,endTime}]}。
// 後端 PhaseParams 先前只讀 phaseLabels(且被當時間點解析、phase 名來自 config),與
// 前端 payload 不對盤 → 每次必中 ErrNoPhaseLabels,panel 全壞。
//
// 完整修:PhaseParams 改收 Phases []PhaseSpec,honor 前端名稱與邊界、去除 config 耦合。
func TestAnalyzePhases_HonorsFrontendPhasesAndNames(t *testing.T) {
	inDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = inDir
	cfg.OutputDir = t.TempDir()
	app := NewApp(cfg, "test")

	csvPath := filepath.Join(inDir, "phase.csv")
	writeEMGCSVForMaxMean(t, csvPath, 100) // 0~0.099s,100 列

	result, err := app.AnalyzePhases(PhaseParams{
		InputFile: csvPath,
		Phases: []PhaseSpec{
			{Name: "站立期", StartTime: 0.0, EndTime: 0.05},
			{Name: "擺動期", StartTime: 0.05, EndTime: 0.099},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "Message: %s", result.Message)
	require.Len(t, result.Results, 2, "應產出 2 個 phase 結果")
	assert.Equal(t, "站立期", result.Results[0].PhaseLabel,
		"phase 名稱必須來自前端傳入,不耦合 config.PhaseLabels")
	assert.Equal(t, "擺動期", result.Results[1].PhaseLabel)
	assert.NotEmpty(t, result.OutputPath)

	// 前端 ranges 是「秒」,但 parsed-data 的時間欄被 Str2Number scale 過(×10^ScalingFactor)。
	// ranges 必須 scale 到同域,否則沒有任何樣本落入 phase → 統計全為 0(panel 看似成功卻空)。
	assert.NotZero(t, result.Results[0].MaxValues[0],
		"phase 區間內應有樣本 → 統計非零;ranges 未 scale 到縮放時間域會導致全 0")
	assert.NotZero(t, result.Results[0].Average[0],
		"phase 區間內 mean 應非零")
}

// TestValidatePhaseParams_RejectsBadInput 釘住新 contract 的驗證邊界。
func TestValidatePhaseParams_RejectsBadInput(t *testing.T) {
	t.Run("no_input_file", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			Phases: []PhaseSpec{{Name: "a", StartTime: 0, EndTime: 1}},
		})
		assert.ErrorIs(t, err, ErrNoInputFile)
	})
	t.Run("no_phases", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{InputFile: "x.csv"})
		assert.ErrorIs(t, err, ErrNoPhaseLabels)
	})
	t.Run("empty_name", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "  ", StartTime: 0, EndTime: 1}},
		})
		assert.ErrorIs(t, err, ErrNoValidPhaseLabels)
	})
	t.Run("start_not_before_end", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: 1, EndTime: 1}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	// 非有限邊界(±Inf):ErrInvalidPhaseRange 訊息承諾「為有限值」,且舊 phaseStrings
	// 路徑經 Str2Number 會拒 Inf。新 front-end-ranges 路徑必須同樣拒絕,否則 Inf 被
	// scale 後當無邊界區間分析 → 產出「成功但誤導」的結果(codex round-2 finding)。
	t.Run("inf_end", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: 0, EndTime: math.Inf(1)}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	t.Run("neg_inf_start", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: math.Inf(-1), EndTime: 1}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	t.Run("both_inf", func(t *testing.T) {
		// -Inf < +Inf 為 true → 繞過 start<end 檢查;必須由顯式有限值守門擋下。
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: math.Inf(-1), EndTime: math.Inf(1)}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
}
