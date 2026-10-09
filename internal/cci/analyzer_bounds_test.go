package cci

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
	"count_mean/internal/models"
)

// TestValidateEMGBounds_AcceptsULPDriftEpsilon 釘住 修補:
// 邊界容差從 1e-9 放寬到 1e-6,涵蓋 1000Hz EMG / 力板 ULP 飄移(現為
// synchronizer.OutsideEMG 的共用容差,ADR-0043)。
//
// 修補前容差 = 1e-9 比 sample interval (~1e-3) 還細 6 個量級,
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

	// ADR-0018:步態週期錨定 0%=S、100%=L,duration = L − S。把 tiny gap 放到
	// S 與 L 上 — S=0.05、L=0.05+1e-9 → duration ≈ 1e-9s,遠小於 10 個 sample
	// interval (0.01s),duration 守門必須 reject。(P0/P1/P2 已被排除在週期外,
	// 不能再用它們撐 duration。)兩者皆落在 EMG [0, 0.3] 內,validateEMGBounds 先放行。
	a := NewCCIAnalyzer()
	manifest := newBoundsTestManifest()
	manifest.PhasePoints.S = models.MakeOpt(0.05)
	manifest.PhasePoints.L = models.MakeOpt(0.05 + 1e-9)

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
	// ADR-0018:duration = L − S = 0.5 − 0.05 = 0.45s,遠大於 10 × 1ms = 0.01s 門檻。
	// 用 S/L 錨定週期(非 P0/P1/P2 — 後者已排除在週期外)。
	manifest.PhasePoints.S = models.MakeOpt(0.05)
	manifest.PhasePoints.L = models.MakeOpt(0.5)

	_, _, _, _, err := a.calculateGaitCycle(manifest, emgData)
	require.NoError(t, err, "valid duration should not be rejected: %v", err)
}

// TestBuildChannelMap_MissingChannel_ZhTW 釘住缺失肌肉錯誤被轉成 cci 既有 zh-TW 訊息 (含肌肉名)。
func TestBuildChannelMap_MissingChannel_ZhTW(t *testing.T) {
	_, err := BuildChannelMap([]string{"L.RA: EMG 1", "R.RA: EMG 1"})
	require.Error(t, err)
	assert.Equal(t, "缺少必要的肌肉通道: ES", err.Error())
}

// TestCCIErrors_ZhTWTextLocalizedAtHandler 釘住 ADR-0048:cci 回帶 i18n key 的錯誤,
// 不在建構時決定語言。
//
//   - err.Error() 一律是 zh-TW 文字,與 key 化之前逐位元組相同 —— 建構時的 locale 是
//     zh-TW 或 en-US 都一樣(log / byte-pinned 測試不隨 locale 變)
//   - i18n.Localize(err)(handler 層的 envelope 用它)依呼叫當下的 locale 渲染:建構時
//     是 zh-TW、呈現時切到 en-US,得到英文
//
// 各列涵蓋兩種形狀:只有訊息(有 / 無 Args,無 Args 時 catalog 字面 % 原樣)與
// 「前綴: cause」(cause 也是 *i18n.Error 時一併在地化)。
func TestCCIErrors_ZhTWTextLocalizedAtHandler(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale(i18n.LocaleZhTW) })

	cases := []struct {
		name  string
		build func() error
		zhTW  string
		enUS  string
	}{
		{
			name: "分期點不足(訊息、無 Args)",
			build: func() error {
				m := newBoundsTestManifest()
				m.PhasePoints.P0 = models.MakeOpt(0.05)
				_, _, _, _, err := NewCCIAnalyzer().calculateGaitCycle(
					m, &models.PhaseSyncEMGData{Time: makeBoundsTimeSeq(0.0, 0.001, 301)})
				return err
			},
			zhTW: "分期點不足，至少需要 2 個有效分期點",
			enUS: "Insufficient phase points; at least 2 valid points are required",
		},
		{
			name: "缺 S / L 錨點(catalog 字面 %)",
			build: func() error {
				m := newBoundsTestManifest()
				m.PhasePoints.C = models.MakeOpt(0.1)
				m.PhasePoints.T0 = models.MakeOpt(0.2)
				_, _, _, _, err := NewCCIAnalyzer().calculateGaitCycle(
					m, &models.PhaseSyncEMGData{Time: makeBoundsTimeSeq(0.0, 0.001, 301)})
				return err
			},
			zhTW: "缺少 S 或 L 分期點，無法錨定步態週期（0%=S、100%=L）",
			enUS: "Missing S or L phase point; cannot anchor the gait cycle (0%=S, 100%=L)",
		},
		{
			name: "通道長度不符(訊息、有 Args)",
			build: func() error {
				_, err := CalculateCCITimeSeries(context.Background(), []float64{0.1, 0.2}, []float64{0.3})
				return err
			},
			zhTW: "通道數據長度不一致: 2 vs 1",
			enUS: "Channel data length mismatch: 2 vs 1",
		},
		{
			name: "步態起點早於 EMG(%.3f Args)",
			build: func() error {
				return validateEMGBounds(&models.PhaseSyncEMGData{Time: []float64{1.0, 2.0}}, 0.5, 1.5)
			},
			zhTW: "步態週期開始時間 0.500 小於 EMG 數據最小時間 1.000",
			enUS: "Gait cycle start time 0.500 is below EMG min time 1.000",
		},
		{
			name: "建立通道映射失敗(前綴: cause,cause 也在地化)",
			build: func() error {
				_, err := NewCCIAnalyzer().computeCCI(context.Background(),
					&models.PhaseSyncEMGData{Headers: []string{"L.RA: EMG 1", "R.RA: EMG 1"}},
					newBoundsTestManifest())
				return err
			},
			zhTW: "建立通道映射失敗: 缺少必要的肌肉通道: ES",
			enUS: "Failed to build channel map: Missing required muscle channel: ES",
		},
	}

	for _, tc := range cases {
		for _, buildLocale := range []i18n.Locale{i18n.LocaleZhTW, i18n.LocaleEnUS} {
			t.Run(tc.name+"/built_under_"+string(buildLocale), func(t *testing.T) {
				i18n.SetLocale(buildLocale)
				err := tc.build()
				require.Error(t, err)

				i18n.SetLocale(i18n.LocaleEnUS)
				assert.Equal(t, tc.zhTW, err.Error(), "Error() 固定是 zh-TW")
				assert.Equal(t, tc.enUS, i18n.Localize(err), "Localize 依目前 locale(en-US)")
			})
		}
	}
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
