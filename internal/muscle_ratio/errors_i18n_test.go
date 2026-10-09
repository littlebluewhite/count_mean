package muscle_ratio

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
	"count_mean/internal/models"
)

// TestMuscleRatioErrors_ZhTWTextLocalizedAtHandler 釘住 ADR-0048:muscle_ratio 回帶 i18n
// key 的錯誤 —— 整批錯誤(Analyze 的 err)、逐 subject 錯誤(SubjectResult.Err)與 Output 2
// 跳過的 warning(collectPhasePoints)都一樣:
//
//   - err.Error() 一律是 zh-TW 文字,與 key 化之前逐位元組相同,不隨建構時的 locale 變
//   - i18n.Localize(err) 依呈現當下的 locale(en-US)渲染
func TestMuscleRatioErrors_ZhTWTextLocalizedAtHandler(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale(i18n.LocaleZhTW) })

	cases := []struct {
		name  string
		build func(t *testing.T) error
		zhTW  string
		enUS  string
	}{
		{
			name: "Analyze 已取消(前綴: cause)",
			build: func(t *testing.T) error {
				tempDir := t.TempDir()
				manifestPath := filepath.Join(tempDir, "manifest.csv")
				writeManifest(t, manifestPath, [][]string{
					makeRow15("S", "s.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
				})
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := NewAnalyzer().Analyze(ctx, &Params{
					ManifestFile: manifestPath,
					DataFolder:   tempDir,
					OutputDir:    tempDir,
					CSVHandler:   newTestCSVHandler(tempDir),
				})
				require.ErrorIs(t, err, context.Canceled)

				return err
			},
			zhTW: "肌肉比值批次已中止: context canceled",
			enUS: "Muscle ratio batch cancelled: context canceled",
		},
		{
			name: "輸出檔名衝突(%q Args)",
			build: func(*testing.T) error {
				return assertUniqueSanitizedSubjects([]models.PhaseManifest{{Subject: "A/B"}, {Subject: "A:B"}})
			},
			zhTW: `輸出檔名衝突: subject "A/B" 與 "A:B" 經檔名安全化後同為 "A_B" (case-insensitive)`,
			enUS: `Output filename collision: subjects "A/B" and "A:B" both sanitize to "A_B" (case-insensitive)`,
		},
		{
			name: "SubjectResult.Err:Subject 名稱為空",
			build: func(*testing.T) error {
				return NewAnalyzer().analyzeSubject(&models.PhaseManifest{Subject: "   "}, &Params{}).Err
			},
			zhTW: "Subject 名稱為空",
			enUS: "Subject name is empty",
		},
		{
			name: "Output 2 warning:phase 落在 EMG 範圍外(%s %.4f Args)",
			build: func(*testing.T) error {
				// EMGMotionOffset=1:力板時間即 EMG 時間。
				m := &models.PhaseManifest{
					Subject:         "S",
					EMGMotionOffset: 1,
					PhasePoints:     models.PhasePoints{S: models.MakeOpt(0.5), L: models.MakeOpt(5.0)},
				}
				_, warn := NewAnalyzer().collectPhasePoints(m, &models.PhaseSyncEMGData{Time: []float64{0.0, 1.0}})

				return warn
			},
			zhTW: "phase L 時間 5.0000 落在 EMG 範圍 [0.0000, 1.0000] 外，跳過 Output 2",
			enUS: "Phase L at time 5.0000 is outside EMG range [0.0000, 1.0000]; skipping Output 2",
		},
	}

	for _, tc := range cases {
		for _, buildLocale := range []i18n.Locale{i18n.LocaleZhTW, i18n.LocaleEnUS} {
			t.Run(tc.name+"/built_under_"+string(buildLocale), func(t *testing.T) {
				i18n.SetLocale(buildLocale)
				err := tc.build(t)
				require.Error(t, err)

				i18n.SetLocale(i18n.LocaleEnUS)
				assert.Equal(t, tc.zhTW, err.Error(), "Error() 固定是 zh-TW")
				assert.Equal(t, tc.enUS, i18n.Localize(err), "Localize 依目前 locale(en-US)")
			})
		}
	}
}
