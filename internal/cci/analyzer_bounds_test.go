package cci

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
	"count_mean/internal/models"
)

// TestValidateEMGBounds_AcceptsULPDriftEpsilon 釘住 修補:
// boundsEpsilon 從 1e-9 放寬到 1e-6,涵蓋 1000Hz EMG / 力板 ULP 飄移。
//
// 修補前 boundsEpsilon = 1e-9 比 sample interval (~1e-3) 還細 6 個量級,
// 等於沒有 tolerance — 1000Hz force plate vs EMG 經 sync 後的 1e-7 ULP
// 飄移會被誤判 out-of-range,讓真實資料報錯。
//
// 新 epsilon = 1e-6 為「比 sample interval 細 3 個量級」的安全餘量:
//   - 真實 out-of-range (>= sample_interval ≈ 1e-3) 仍會被擋下
//   - 浮點 ULP 飄移 (<= 1e-6) 被吸收
func TestValidateEMGBounds_AcceptsULPDriftEpsilon(t *testing.T) {
	emgData := &models.PhaseSyncEMGData{
		Time: []float64{0.0, 0.001, 0.002, 0.003},
	}

	tests := []struct {
		name      string
		gaitStart float64
		gaitEnd   float64
		wantErr   bool
	}{
		{
			// 1e-8 ULP 飄移 — 修補前 1e-9 epsilon 會擋下,1e-6 必須放行
			name:      "tiny_ulp_drift_at_start_within_epsilon",
			gaitStart: 0.0 - 1e-8,
			gaitEnd:   0.003,
			wantErr:   false,
		},
		{
			name:      "tiny_ulp_drift_at_end_within_epsilon",
			gaitStart: 0.0,
			gaitEnd:   0.003 + 1e-8,
			wantErr:   false,
		},
		{
			// real out-of-range (>= 1ms) 仍然必須被擋下,1e-6 epsilon 不能掩蓋
			name:      "real_out_of_range_at_start_still_rejected",
			gaitStart: -0.01,
			gaitEnd:   0.003,
			wantErr:   true,
		},
		{
			name:      "real_out_of_range_at_end_still_rejected",
			gaitStart: 0.0,
			gaitEnd:   0.013,
			wantErr:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEMGBounds(emgData, tc.gaitStart, tc.gaitEnd)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "unexpected error: %v", err)
			}
		})
	}
}

// TestCalculateGaitCycle_RejectsTinyDuration 釘住 修補:
// duration ≈ 0 邊界 — 當所有有效分期點時間幾乎相同 (差距 < 10 個 sample interval),
// 計算的 phase percent 會放大浮點誤差,實質無意義。
//
// 修補前只擋 duration <= 0,但 duration ≈ 1e-9 sec 仍會通過,
// pct = (t - start) / 1e-9 * 100 會放大噪音。修補後 reject。
//
// Sample interval 假設 1000Hz (BTS 力板 default) = 0.001s,門檻 10 個 interval = 0.01s。
func TestCalculateGaitCycle_RejectsTinyDuration(t *testing.T) {
	emgData := &models.PhaseSyncEMGData{
		Time: makeBoundsTimeSeq(0.0, 0.001, 301),
	}

	// Manifest: P0 = 0.05, P1 = P0 + 1e-9 → duration ≈ 1e-9s,遠小於 10 個 sample interval (0.01s)
	a := NewCCIAnalyzer()
	manifest := newBoundsTestManifest()
	manifest.PhasePoints.P0 = models.MakeOpt(0.05)
	manifest.PhasePoints.P1 = models.MakeOpt(0.05 + 1e-9)
	manifest.PhasePoints.P2 = models.MakeOpt(0.05 + 2e-9)

	_, _, _, _, err := a.calculateGaitCycle(manifest, emgData)
	require.Error(t, err, "expected error for tiny duration")
	assert.Contains(t, err.Error(), "步態週期長度", "error should mention gait cycle duration")
}

// TestCalculateGaitCycle_AcceptsNormalDuration 釘住 normal duration 不會被誤擋。
func TestCalculateGaitCycle_AcceptsNormalDuration(t *testing.T) {
	emgData := &models.PhaseSyncEMGData{
		Time: makeBoundsTimeSeq(0.0, 0.001, 1001), // 0..1.0s
	}

	a := NewCCIAnalyzer()
	manifest := newBoundsTestManifest()
	// duration = 0.5 - 0.05 = 0.45s,遠大於 10 × 1ms = 0.01s 門檻
	manifest.PhasePoints.P0 = models.MakeOpt(0.05)
	manifest.PhasePoints.P1 = models.MakeOpt(0.3)
	manifest.PhasePoints.P2 = models.MakeOpt(0.5)

	_, _, _, _, err := a.calculateGaitCycle(manifest, emgData)
	require.NoError(t, err, "valid duration should not be rejected: %v", err)
}

// TestCCIAnalyzer_I18n_InsufficientPhasesEnUSLocale 釘住 修補:
// CCI errors 改走 i18n.T() catalog,不再 hardcode 中文字面。
//
// 切換 en-US locale,確認 error 字串走的是英文 catalog 字面,不是中文 hardcode,
// 也不是 raw i18n key 字面。
func TestCCIAnalyzer_I18n_InsufficientPhasesEnUSLocale(t *testing.T) {
	defer i18n.SetLocale(i18n.LocaleZhTW)

	i18n.SetLocale(i18n.LocaleEnUS)

	emgData := &models.PhaseSyncEMGData{
		Time: makeBoundsTimeSeq(0.0, 0.001, 301),
	}
	a := NewCCIAnalyzer()
	manifest := newBoundsTestManifest()
	// 只給 1 個有效 phase point — 觸發「分期點不足」
	manifest.PhasePoints.P0 = models.MakeOpt(0.05)

	_, _, _, _, err := a.calculateGaitCycle(manifest, emgData)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "分期點不足", "en-US locale 不應出現中文字面")
	assert.NotContains(t, err.Error(), "error.cci.insufficient_phase_points",
		"raw i18n key 不應 leak")
	assert.Contains(t, strings.ToLower(err.Error()), "phase",
		"en-US locale 應出現英文 phase keyword")
}

// TestCalculateCCITimeSeries_I18n_LengthMismatch_EnUS 釘住 calculator.go 的 i18n。
func TestCalculateCCITimeSeries_I18n_LengthMismatch_EnUS(t *testing.T) {
	defer i18n.SetLocale(i18n.LocaleZhTW)

	i18n.SetLocale(i18n.LocaleEnUS)

	_, err := CalculateCCITimeSeries(context.Background(), []float64{0.1, 0.2}, []float64{0.3})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "通道數據長度不一致", "en-US locale 不應出現中文字面")
	assert.Contains(t, strings.ToLower(err.Error()), "channel",
		"en-US locale 應出現英文 channel keyword")
}

// TestBuildChannelMap_I18n_MissingChannel_EnUS 釘住 BuildChannelMap 的 i18n。
func TestBuildChannelMap_I18n_MissingChannel_EnUS(t *testing.T) {
	defer i18n.SetLocale(i18n.LocaleZhTW)

	i18n.SetLocale(i18n.LocaleEnUS)

	// 缺少所有 R.* channel,headers 只有時間欄
	_, err := BuildChannelMap([]string{"X []"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "缺少必要的肌肉通道", "en-US locale 不應出現中文字面")
	assert.Contains(t, strings.ToLower(err.Error()), "muscle",
		"en-US locale 應出現英文 muscle keyword")
}

// TestCCIAnalyzer_I18n_ZhTW_PreservesChineseStrings 確認 zh-TW locale 下,catalog
// 解析依然回中文字面 — 既有測試 (含 "步態週期" 子字串比對) 不會被 i18n 化破壞。
func TestCCIAnalyzer_I18n_ZhTW_PreservesChineseStrings(t *testing.T) {
	// TestMain 已 set zh-TW,此 test 在 en-US test 後若有未還原情形仍應通過
	i18n.SetLocale(i18n.LocaleZhTW)

	emgData := &models.PhaseSyncEMGData{
		Time: makeBoundsTimeSeq(0.0, 0.001, 301),
	}
	a := NewCCIAnalyzer()
	manifest := newBoundsTestManifest()
	manifest.PhasePoints.P0 = models.MakeOpt(0.05)

	_, _, _, _, err := a.calculateGaitCycle(manifest, emgData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "分期點不足", "zh-TW locale 應保留中文字面")
}

// --- helpers ---

// makeBoundsTimeSeq 構造 [start, start+step, ..., start + (n-1)*step] 的時間序列。
func makeBoundsTimeSeq(start, step float64, n int) []float64 {
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = start + float64(i)*step
	}
	return out
}

// newBoundsTestManifest 構造最小 PhaseManifest,所有 phase 預設 NoOpt。
func newBoundsTestManifest() *models.PhaseManifest {
	return &models.PhaseManifest{
		Subject:         "BoundsTest",
		EMGMotionOffset: 0,
		PhasePoints:     models.PhasePoints{},
	}
}
