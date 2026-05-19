package phase_sync //nolint:revive // underscore in package name matches directory structure

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

// TestExportResults_SubjectWithRTLAndControl_FilenameSanitized 釘住 
// ExportResults 把 stats.Subject 直接交給 GenerateOutputFileName -> SanitizeFileName，
// 後者過去只替換路徑分隔符，未處理 Unicode 控制 / 雙向書寫覆寫（U+202E）/ ASCII
// 控制碼。RTL override 可在 Finder/Explorer 製造視覺檔名欺騙（"evil.exe.csv" 顯示為
// "evilcsv.exe"），NUL/Bell 等 ASCII 控制碼可破壞下游處理或 log，必須在寫入檔案前
// 清除。本 test 在修復前會 fail（檔名仍含 U+202E / NUL / 控制碼）。
//
// Unicode literals 一律走 \uXXXX 表記，符合 staticcheck ST1018 要求且避免
// editor 把 RTL override 真的渲染到原始碼上造成 review 困擾。
func TestExportResults_SubjectWithRTLAndControl_FilenameSanitized(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()

	cases := []struct {
		name    string
		subject string
		mustNot []rune
	}{
		{
			name:    "rtl_override",
			subject: "evil\u202egsp.csv",
			mustNot: []rune{0x202E},
		},
		{
			name:    "null_byte",
			subject: "Sub\x00ject",
			mustNot: []rune{0x00},
		},
		{
			name:    "ascii_bell",
			subject: "Sub\x07ject",
			mustNot: []rune{0x07},
		},
		{
			name:    "zero_width_space",
			subject: "Sub\u200bject",
			mustNot: []rune{0x200B},
		},
		{
			name:    "bidi_iso_pop",
			subject: "Sub\u2068x\u2069ject",
			mustNot: []rune{0x2068, 0x2069},
		},
		{
			name:    "crlf_in_subject",
			subject: "Sub\r\nject",
			mustNot: []rune{0x0D, 0x0A},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stats := &models.EMGStatistics{
				Subject:      tc.subject,
				StartPhase:   "P0",
				EndPhase:     "P2",
				StartTime:    0.0,
				EndTime:      2.0,
				ChannelNames: []string{"Ch1"},
				ChannelMeans: map[string]float64{"Ch1": 1.0},
				ChannelMaxes: map[string]float64{"Ch1": 2.0},
			}

			outputDir := t.TempDir()
			outputPath, err := analyzer.ExportResults(stats, outputDir)
			require.NoError(t, err)

			base := filepath.Base(outputPath)
			for _, ch := range tc.mustNot {
				if strings.ContainsRune(base, ch) {
					t.Errorf("output filename %q still contains forbidden rune U+%04X (subject=%q)",
						base, ch, tc.subject)
				}
			}

			// 額外保證：sanitize 後組合路徑不會逸出 outputDir。
			cleaned := filepath.Clean(outputPath)
			require.True(t, strings.HasPrefix(cleaned, outputDir),
				"sanitized output path %q must stay inside %q", cleaned, outputDir)

			// 確認檔案真的被寫到清理後的路徑（symlink / mkdir 副作用測試）。
			_, statErr := os.Stat(outputPath)
			require.NoError(t, statErr)
		})
	}
}

// TestValidateEMGFilePath_NonExistentBaseFolder 釘住 當 baseFolder 不
// 存在時，EvalSymlinks 會回傳 *PathError；舊版只 silently 落回原始字串（baseFolder
// 不變），然後 PathValidator 才用一個不存在的 base 做 isPathWithinBase 比較，得到
// 含糊的 "EMG 檔案路徑驗證失敗"。修復後需在 EvalSymlinks 失敗且原因非 ENOENT 之外的
// 情況下顯式 return 「資料夾不存在」class 錯誤，方便使用者快速定位設定錯誤。
func TestValidateEMGFilePath_NonExistentBaseFolder(t *testing.T) {
	analyzer := NewPhaseSyncAnalyzer()

	manifestContent := `Subject,Motion檔案,力板檔案,EMG檔案,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L
TestSubject,motion.csv,force.csv,emg.csv,100,1.0,2.0,3.0,4.0,5.0,250,6.0,7.0,350,8.0`
	manifestFile := createTempFile(t, manifestContent)

	// 確保 baseFolder 真的不存在
	nonExistent := filepath.Join(t.TempDir(), "definitely_missing_subdir")
	_, err := os.Stat(nonExistent)
	require.True(t, os.IsNotExist(err), "test precondition: baseFolder must not exist")

	params := &models.AnalysisParams{
		ManifestFile: manifestFile,
		DataFolder:   nonExistent,
		StartPhase:   "P0",
		EndPhase:     "P2",
		SubjectIndex: 0,
	}

	stats, err := analyzer.AnalyzePhaseSync(context.Background(), params)
	assert.Error(t, err)
	assert.Nil(t, stats)
	assert.ErrorIs(t, err, ErrBaseFolderNotFound,
		"non-existent baseFolder should wrap ErrBaseFolderNotFound; got: %v", err)
	assert.Contains(t, err.Error(), "資料夾不存在",
		"error message should be human-readable; got: %v", err)
	assert.Contains(t, err.Error(), nonExistent,
		"error should include the offending baseFolder path for debuggability")
}
