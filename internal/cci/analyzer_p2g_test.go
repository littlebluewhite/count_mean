package cci

import (
	"bytes"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/logging"
	"count_mean/internal/models"
)

// TestValidateEMGBounds_RejectsNonFinite 守護 (a):
//
// gaitStart/gaitEnd 上游從 manifest 推導,若 manifest 帶 NaN/Inf (parse failure /
// corrupted data),原本 epsilon 比較對 NaN 永遠回 false (浮點規則:NaN cmp anything = false),
// validateEMGBounds 會 PASS 但 downstream phase percent 計算全變 NaN 污染結果。
//
// 此 test 守住三條 invariant:
//
//  1. NaN gaitStart / gaitEnd → return error (含 "非有限值" 標誌)
//  2. ±Inf gaitStart / gaitEnd → return error
//  3. NaN/Inf 在 emgData.Time 首末筆同樣被擋
//  4. 控制組:正常有限值仍 PASS
func TestValidateEMGBounds_RejectsNonFinite(t *testing.T) {
	emgData := &models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.001, 0.002, 0.003},
	}

	cases := []struct {
		name      string
		gaitStart float64
		gaitEnd   float64
		wantErr   bool
		wantFrag  string
	}{
		{"nan_gaitStart", math.NaN(), 0.003, true, "gaitStart"},
		{"nan_gaitEnd", 0.0, math.NaN(), true, "gaitEnd"},
		{"pos_inf_gaitStart", math.Inf(1), 0.003, true, "gaitStart"},
		{"neg_inf_gaitStart", math.Inf(-1), 0.003, true, "gaitStart"},
		{"pos_inf_gaitEnd", 0.0, math.Inf(1), true, "gaitEnd"},
		{"neg_inf_gaitEnd", 0.0, math.Inf(-1), true, "gaitEnd"},
		{"both_nan", math.NaN(), math.NaN(), true, "gaitStart"},
		// 控制組:有限值正常通過
		{"finite_in_range_passes", 0.0, 0.003, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEMGBounds(emgData, tc.gaitStart, tc.gaitEnd)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "非有限值",
					"err 應指明是 NaN/Inf 守門失敗: %v", err)
				assert.Contains(t, err.Error(), tc.wantFrag,
					"err 應指明哪個欄位: %v", err)
			} else {
				require.NoError(t, err)
			}
		})
	}

	t.Run("nan_in_emgMin", func(t *testing.T) {
		bad := &models.PhaseSyncEMGData{
			Time: []float64{math.NaN(), 0.001, 0.002},
		}
		err := validateEMGBounds(bad, 0.0, 0.002)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "EMG 時間軸首筆")
	})

	t.Run("inf_in_emgMax", func(t *testing.T) {
		bad := &models.PhaseSyncEMGData{
			Time: []float64{0.0, 0.001, math.Inf(1)},
		}
		err := validateEMGBounds(bad, 0.0, 0.001)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "EMG 時間軸末筆")
	})
}

// TestEstimateSampleInterval_FallbackLogsWarn 守護 (c):
//
// estimateSampleInterval 在資料不足或差值非正時 fallback 至 defaultInterval (0.001s)。
// 過去 silently fallback — caller 看到正常數值,downstream duration guard
// 仍會跑通,但實際 sample interval 是 default 不是真實估算,結果統計失真。
//
// 修法後 fallback 觸發時 logger.Warn,讓 audit log 能標記「sample interval 是
// 估算 vs 預設」的歧異。此 test 用 in-memory bytes.Buffer logger 驗證 Warn 觸發。
func TestEstimateSampleInterval_FallbackLogsWarn(t *testing.T) {
	t.Run("insufficient_data_warns", func(t *testing.T) {
		var buf bytes.Buffer
		logger := logging.NewLogger(logging.LevelDebug, &buf, false)

		got := estimateSampleInterval([]float64{0.001}, logger)
		assert.InDelta(t, 0.001, got, 1e-12, "fallback 應為 0.001s")

		out := buf.String()
		assert.Contains(t, out, "WARN", "fallback 應觸發 WARN level log")
		assert.Contains(t, out, "資料不足",
			"warn 訊息應指明資料不足: %s", out)
	})

	t.Run("non_positive_dt_warns", func(t *testing.T) {
		var buf bytes.Buffer
		logger := logging.NewLogger(logging.LevelDebug, &buf, false)

		// 倒退的時間戳:dt = -0.001 ≤ 0,觸發 fallback
		got := estimateSampleInterval([]float64{0.002, 0.001}, logger)
		assert.InDelta(t, 0.001, got, 1e-12, "fallback 應為 0.001s")

		out := buf.String()
		assert.Contains(t, out, "WARN")
		assert.Contains(t, out, "非正",
			"warn 訊息應指明 dt 非正: %s", out)
	})

	t.Run("normal_data_no_warn", func(t *testing.T) {
		var buf bytes.Buffer
		logger := logging.NewLogger(logging.LevelDebug, &buf, false)

		got := estimateSampleInterval([]float64{0.0, 0.001}, logger)
		assert.InDelta(t, 0.001, got, 1e-12)

		// 正常路徑不該有 WARN
		out := buf.String()
		assert.NotContains(t, out, "WARN",
			"正常 path 不該觸發 warn: %s", out)
	})

	t.Run("nil_logger_safe", func(t *testing.T) {
		// nil logger 路徑必須安全 (不 panic)
		require.NotPanics(t, func() {
			got := estimateSampleInterval([]float64{0.0}, nil)
			assert.InDelta(t, 0.001, got, 1e-12)
		})
	})
}

// TestCalculateGaitCycle_PhasePercentClampLogsWarn 守護 (b):
//
// phase percent clamp 從 silent → 帶 Warn。當 manifest 帶的 phase point
// EMG time 落在 gait cycle [start, end] 外時,過去 silently clamp 至 [0, 100]
// 把使用者的資料品質問題藏住。修法後在 clamp 觸發時 Warn (>1e-6 容忍邊界 ULP)。
//
// 此 test 構造一個 phase 落在 gait cycle 外的場景,驗證 logger 收到 Warn。
func TestCalculateGaitCycle_PhasePercentClampLogsWarn(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelDebug, &buf, false)
	a := NewCCIAnalyzer()
	a.logger = logger

	// EMG 時間軸 0..1.0s
	emgData := &models.PhaseSyncEMGData{
		Time: makeBoundsTimeSeq(0.0, 0.001, 1001),
	}

	// ADR-0018:步態週期錨定 0%=S、100%=L,gaitStart=S、gaitEnd=L。週期保留的
	// 7 個點 (S, C, D, T0, T, O, L) 依定義都落在 [S, L] 內,raw_pct ∈ [0, 100],
	// clamp 在正常 production path 不會觸發 — clamp 是 audit-aware dead-code
	// defense,只有未來重構若 bypass S/L 錨定 (例如直接注入越界的 gaitStart/End)
	// 才會 trigger。P2-G (b) 的修法把 silent clamp 改為「觸發時 Warn」(>1e-6 容忍
	// 邊界 ULP),確保異常立刻被 audit log 捕獲。
	//
	// 此 test 守的是「正常 path 不破壞」這條 invariant:構造一組全部落在 [S, L]
	// 內的分期點,驗證 (a) 所有 pct 為 finite 且在 [0, 100],(b) 不觸發 clamp warn。
	//
	// 分期點 (EMGMotionOffset=0,force-time 經 +0.004 偏移):
	//   S=0.1 → 0.104 (0%)、C=0.3 → 0.304、T=0.7 → 0.704、L=0.9 → 0.904 (100%)
	// gaitStart=0.104、gaitEnd=0.904,C/T 落在週期內 → 無 clamp。
	manifest := newBoundsTestManifest()
	manifest.PhasePoints.S = models.MakeOpt(0.1)
	manifest.PhasePoints.C = models.MakeOpt(0.3)
	manifest.PhasePoints.T = models.MakeOpt(0.7)
	manifest.PhasePoints.L = models.MakeOpt(0.9)

	_, _, percents, _, err := a.calculateGaitCycle(manifest, emgData)
	require.NoError(t, err)
	// 所有 phase 都在 gait cycle 範圍內,pct 都該是 finite 且在 [0, 100]
	// (gaitStart/gaitEnd 由 EMGMotionOffset 轉換後推導,不對 raw 0.1/0.9 比對)
	for name, p := range percents {
		assert.False(t, math.IsNaN(p), "phase %s pct is NaN", name)
		assert.False(t, math.IsInf(p, 0), "phase %s pct is Inf", name)
		assert.GreaterOrEqual(t, p, 0.0, "phase %s pct < 0", name)
		assert.LessOrEqual(t, p, 100.0, "phase %s pct > 100", name)
	}

	// 正常路徑下 clamp warn 不該觸發
	out := buf.String()
	assert.NotContains(t, out, "phase percent 被 clamp",
		"正常 path (phase 都在 gait cycle 內) 不該觸發 clamp warn: %s", out)

	// estimateSampleInterval 正常 path 也不該觸發 warn (1ms 間距,dt > 0)
	assert.NotContains(t, out, "fallback 至預設 sample interval",
		"正常 EMG 時間軸不該觸發 sample interval fallback warn")
}

// TestCalculateGaitCycle_ReanchorsPercentToSL 鎖定 ADR-0018 headline 行為:
// phase percent 以 0%=S、100%=L 重錨 (gaitStart=emgTimes[S]、gaitEnd=emgTimes[L])。
//
// 這是本次重錨唯一 pin 具體百分比值的回歸守衛 — 純 [0,100] 範圍斷言在舊 min/max
// 推導下也會通過,鎖不住「pct 改錨到 S/L」這條語意變更 (code review 補強)。
//
// 分期點 (EMGMotionOffset=0,force-time 經 +0.004 偏移,pct 對 offset 不變):
//
//	S=0.1→0.104 (0%)、C=0.3→0.304 (25%)、T=0.7→0.704 (75%)、L=0.9→0.904 (100%)
//	duration = 0.904 − 0.104 = 0.8
func TestCalculateGaitCycle_ReanchorsPercentToSL(t *testing.T) {
	a := NewCCIAnalyzer()
	emgData := &models.PhaseSyncEMGData{
		Time: makeBoundsTimeSeq(0.0, 0.001, 1001),
	}
	manifest := newBoundsTestManifest()
	manifest.PhasePoints.S = models.MakeOpt(0.1)
	manifest.PhasePoints.C = models.MakeOpt(0.3)
	manifest.PhasePoints.T = models.MakeOpt(0.7)
	manifest.PhasePoints.L = models.MakeOpt(0.9)

	_, _, percents, _, err := a.calculateGaitCycle(manifest, emgData)
	require.NoError(t, err)

	assert.InDelta(t, 0.0, percents[string(models.PhaseS)], 1e-9, "S 必須錨定為 0 (gaitStart)")
	assert.InDelta(t, 25.0, percents[string(models.PhaseC)], 1e-9, "C = (0.304-0.104)/0.8*100 = 25")
	assert.InDelta(t, 75.0, percents[string(models.PhaseT)], 1e-9, "T = (0.704-0.104)/0.8*100 = 75")
	assert.InDelta(t, 100.0, percents[string(models.PhaseL)], 1e-9, "L 必須錨定為 100 (gaitEnd)")
}

// TestPhasePercentClampWarn_MotionIndexOOB 守護 (b) 的 audit-aware 行為:
//
// [16] 修法:舊測試手動呼叫 logger.Warn 再斷言同一字串 — 套套邏輯,只驗
// logger 寫入你給的串,未驅動生產 calculateGaitCycle。
// 此測試改為直接調用 a.calculateGaitCycle,構造 D(motion-index) 映射到
// gait cycle 之外以觸發生產 clamp warn。
//
// 構造邏輯:
//
//	S=0.1(force-time) → emgTimeS=0.104,L=0.9 → emgTimeL=0.904 (EMGMotionOffset=0,+0.004 偏移)
//	D=227(motion-index) → MotionIndexToEMGTime(227,0) = 227/250 = 0.908 > emgTimeL=0.904
//	raw_pctD = (0.908-0.104)/(0.904-0.104)*100 = 100.5 > 100+1e-6 → clamp warn 觸發
func TestPhasePercentClampWarn_MotionIndexOOB(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLogger(logging.LevelDebug, &buf, false)
	a := NewCCIAnalyzer()
	a.logger = logger

	// EMG 時間軸 0..1.0s (1001 筆,間距 1ms)
	emgData := &models.PhaseSyncEMGData{
		Time: makeBoundsTimeSeq(0.0, 0.001, 1001),
	}

	// S/C/T/L 使用 force-time(OptFloat);D 使用 motion-index(int)。
	// D=227 → emgTimeD=0.908 > emgTimeL=0.904 → raw_pctD=100.5 > 100+1e-6
	manifest := newBoundsTestManifest()
	manifest.PhasePoints.S = models.MakeOpt(0.1)
	manifest.PhasePoints.C = models.MakeOpt(0.3)
	manifest.PhasePoints.T = models.MakeOpt(0.7)
	manifest.PhasePoints.L = models.MakeOpt(0.9)
	manifest.PhasePoints.D = 227 // motion-index:227/250=0.908 > gaitEnd=0.904

	_, _, percents, _, err := a.calculateGaitCycle(manifest, emgData)
	require.NoError(t, err, "clamp warn 不應導致 error,僅 audit log")

	// [16-A] 斷言生產 warn 已觸發 — 用共享常量而非硬編碼字串,避免兩處漂移
	out := buf.String()
	assert.Contains(t, out, "WARN", "clamp 應走 WARN level")
	assert.Contains(t, out, phasePercentClampWarnMsg,
		"生產 calculateGaitCycle 應印出共享常量 warn 訊息")

	// D 的 pct 應被 clamp 至 100(越上界)
	dKey := string(models.PhaseD)
	dPct, hasDPct := percents[dKey]
	require.True(t, hasDPct, "D 應出現在 percents map")
	assert.InDelta(t, 100.0, dPct, 1e-9, "D pct 越上界應 clamp 至 100")
}
