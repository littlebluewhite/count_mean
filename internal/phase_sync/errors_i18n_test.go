package phase_sync //nolint:revive // underscore in package name matches directory structure

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
	"count_mean/internal/models"
)

// TestPhaseSyncErrors_ZhTWTextLocalizedAtHandler 釘住 ADR-0048:phase_sync 原本硬編碼的
// zh 錯誤改為帶 i18n key 的錯誤。
//
//   - err.Error() 與改前逐位元組相同(zh-TW),不隨 locale 變;sentinel 仍以 errors.Is 命中
//   - i18n.Localize(err) 在 en-US 下渲染英文:巢狀前綴各自在地化、sentinel 文字(英文)
//     原樣、*i18n.Error sentinel 之上的一般 wrapper 保留自己的文字、AnalysisError 透明
func TestPhaseSyncErrors_ZhTWTextLocalizedAtHandler(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale(i18n.LocaleZhTW) })

	missingFolder := filepath.Join(t.TempDir(), "missing")
	emgData := &models.PhaseSyncEMGData{Time: []float64{0.0, 1.0, 2.0}}

	cases := []struct {
		name     string
		build    func() error
		sentinel error
		zhTW     string
		enUS     string
	}{
		{
			name: "資料夾不存在(路徑 Arg)",
			build: func() error {
				return validateEMGFilePath(nil, &validationContext{dataFolder: missingFolder})
			},
			sentinel: ErrBaseFolderNotFound,
			zhTW:     "資料夾不存在 (" + missingFolder + "): base folder not found",
			enUS:     "Data folder does not exist (" + missingFolder + "): base folder not found",
		},
		{
			name: "D 分期點超出 Motion 範圍",
			build: func() error {
				return validateMotionPhasePoints(&models.PhaseManifest{PhasePoints: models.PhasePoints{D: 150}}, 100)
			},
			sentinel: ErrPhasePointOutOfRange,
			zhTW:     "D 分期點 index 150 超出 Motion 數據範圍 (最大: 100): phase point out of data range",
			enUS:     "D phase point index 150 exceeds Motion data range (max: 100): phase point out of data range",
		},
		{
			name: "開始分期點未提供(前綴: 前綴: sentinel)",
			build: func() error {
				m := &models.PhaseManifest{EMGMotionOffset: 26, PhasePoints: models.PhasePoints{L: models.MakeOpt(1.0)}}
				_, err := resolvePhaseRange(emgData, m, models.PhaseS, models.PhaseL)
				return err
			},
			sentinel: ErrPhaseValueZero,
			zhTW:     "計算分期時間範圍失敗: 開始分期點 S: phase value is zero or not set",
			enUS:     "Failed to calculate phase time range: Start phase S: phase value is zero or not set",
		},
		{
			name: "開始晚於結束(三層前綴)",
			build: func() error {
				m := &models.PhaseManifest{
					EMGMotionOffset: 26,
					PhasePoints:     models.PhasePoints{C: models.MakeOpt(1.0), D: 126},
				}
				_, err := resolvePhaseRange(emgData, m, models.PhaseC, models.PhaseD)
				return err
			},
			sentinel: ErrStartTimeAfterEnd,
			zhTW: "計算分期時間範圍失敗: 計算同步時間範圍失敗: " +
				"開始時間 (0.900) 大於結束時間 (0.400): start time is after end time",
			enUS: "Failed to calculate phase time range: Failed to calculate synchronized time range: " +
				"Start time (0.900) is after end time (0.400): start time is after end time",
		},
		{
			name: "負時間 sentinel 在一般 wrapper 之下",
			build: func() error {
				return rejectNegativeForceTime(&models.PhasePoints{S: models.MakeOpt(-0.5)}, models.PhaseS)
			},
			sentinel: ErrNegativePhaseTime,
			zhTW:     "phase point force-time 為負值,phase_sync 不接受: S = -0.5",
			enUS:     "phase point force-time is negative; phase_sync does not accept it: S = -0.5",
		},
		{
			name: "computePhaseSync 的 AnalysisError 包住",
			build: func() error {
				m := &models.PhaseManifest{EMGMotionOffset: 26, PhasePoints: models.PhasePoints{L: models.MakeOpt(1.0)}}
				_, err := NewPhaseSyncAnalyzer().computePhaseSync(context.Background(), emgData, m, models.PhaseS, models.PhaseL)
				var stageErr *AnalysisError
				require.ErrorAs(t, err, &stageErr)
				return err
			},
			sentinel: ErrPhaseValueZero,
			zhTW:     "計算分期時間範圍失敗: 開始分期點 S: phase value is zero or not set",
			enUS:     "Failed to calculate phase time range: Start phase S: phase value is zero or not set",
		},
		{
			name: "AnalyzePhaseSync 的統計前綴",
			build: func() error {
				return phaseSyncComputeError(&AnalysisError{Stage: StageStatistics, Err: errors.New("boom")})
			},
			zhTW: "計算統計信息失敗: boom",
			enUS: "Failed to calculate statistics: boom",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build()
			require.Error(t, err)

			i18n.SetLocale(i18n.LocaleEnUS)
			assert.Equal(t, tc.zhTW, err.Error(), "Error() 固定是 zh-TW")
			assert.Equal(t, tc.enUS, i18n.Localize(err), "Localize 依目前 locale(en-US)")
			if tc.sentinel != nil {
				assert.ErrorIs(t, err, tc.sentinel)
			}
			i18n.SetLocale(i18n.LocaleZhTW)
		})
	}
}
