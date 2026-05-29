package gui

import (
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
			{Name: "站立期", StartTime: "0.0", EndTime: "0.05"},
			{Name: "擺動期", StartTime: "0.05", EndTime: "0.099"},
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

// TestValidatePhaseParams_RejectsBadInput 釘住新 contract 的驗證邊界。邊界以原始字串
// 收入,後端用 strconv.ParseFloat 嚴格解析整串(拒空字串 / 部分解析 / 非數值),另擋
// ParseFloat 仍接受的 ±Inf / NaN。
func TestValidatePhaseParams_RejectsBadInput(t *testing.T) {
	t.Run("no_input_file", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			Phases: []PhaseSpec{{Name: "a", StartTime: "0", EndTime: "1"}},
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
			Phases:    []PhaseSpec{{Name: "  ", StartTime: "0", EndTime: "1"}},
		})
		assert.ErrorIs(t, err, ErrNoValidPhaseLabels)
	})
	t.Run("start_not_before_end", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: "1", EndTime: "1"}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	// 非有限邊界(±Inf / NaN):strconv.ParseFloat 接受 "Inf"/"NaN"(回 ±Inf/NaN、無 error),
	// 故須在解析後另以 IsInf / `!(start<end)` 擋下,否則無邊界區間會產出「成功但誤導」結果。
	t.Run("inf_end", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: "0", EndTime: "Inf"}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	t.Run("neg_inf_start", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: "-Inf", EndTime: "1"}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	t.Run("both_inf", func(t *testing.T) {
		// -Inf < +Inf 為 true → 繞過 start<end 檢查;必須由顯式 IsInf 守門擋下。
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: "-Inf", EndTime: "Inf"}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	t.Run("nan_start", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: "NaN", EndTime: "1"}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	// 空字串(前端未填 / 清空):ParseFloat 回 error → 拒絕,不可當成 0。
	t.Run("empty_start", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: "", EndTime: "1"}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	t.Run("empty_end", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: "1", EndTime: ""}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	// 部分解析:前端 parseFloat 會把 "1.2foo" 截成 1.2、"2.5bar" 截成 2.5(有限數、
	// 通過驗證 → 截斷的誤輸入被當合法值)。後端嚴格解析整串必須拒絕(codex round-5)。
	t.Run("partial_start", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: "1.2foo", EndTime: "2"}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
	t.Run("partial_end", func(t *testing.T) {
		_, _, err := validatePhaseParams(PhaseParams{
			InputFile: "x.csv",
			Phases:    []PhaseSpec{{Name: "a", StartTime: "0", EndTime: "2.5bar"}},
		})
		assert.ErrorIs(t, err, ErrInvalidPhaseRange)
	})
}

// TestValidatePhaseParams_AcceptsValidNumericStrings 釘住合法數值字串(含負數 /
// 科學記號 / 前後空白)正常解析、產出對應 ranges。
func TestValidatePhaseParams_AcceptsValidNumericStrings(t *testing.T) {
	labels, ranges, err := validatePhaseParams(PhaseParams{
		InputFile: "x.csv",
		Phases: []PhaseSpec{
			{Name: "a", StartTime: " 0 ", EndTime: "0.5"},
			{Name: "b", StartTime: "0.5", EndTime: "1e0"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, labels)
	require.Len(t, ranges, 2)
	assert.InDelta(t, 0.0, ranges[0].Start, 1e-9)
	assert.InDelta(t, 0.5, ranges[0].End, 1e-9)
	assert.InDelta(t, 1.0, ranges[1].End, 1e-9)
}
