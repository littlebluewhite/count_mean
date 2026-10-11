package gui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/i18n"
)

// TestLegacyPanelErrors_KeyedAndLocalized 釘住 legacy panel(MaxMean / Normalize / Phase 與
// MaxMean 批次 adapter)err 通道錯誤的最外層是 handler 的 error.handler.* key:
//
//   - err.Error() 是 zh-TW,前綴與遷移前硬編碼逐位元組相同
//   - 下游也帶 key 時(io / validation),i18n.Localize 在 en-US 下整條鏈為英文
//   - 下游仍是硬編碼中文的(calculator、Output placement 前綴、os.ReadDir 包裝)只斷言
//     en-US 前綴;路徑經 webview 出口脫敏的錯誤只斷言 key(出口在地化屬 ADR-0058)
//
// 計算相對路徑失敗(maxmean_batch_adapter)不可達:IsWithin 為 true 蘊含 Rel 成功。
func TestLegacyPanelErrors_KeyedAndLocalized(t *testing.T) {
	prevLocale := i18n.GetLocale()
	t.Cleanup(func() { i18n.SetLocale(prevLocale) })

	inDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = inDir
	cfg.OutputDir = t.TempDir()
	app := NewApp(cfg, "test")

	write := func(name, content string) string {
		p := filepath.Join(inDir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}

	// 檔名驗證在開檔前,CON.csv 不必存在。
	reserved := filepath.Join(inDir, "CON.csv")
	valid := filepath.Join(inDir, "sample.csv")
	writeEMGCSVForMaxMean(t, valid, 50)
	short := write("short.csv", "Time,Ch1\n0.000,1\n0.001,2\n")
	oneRowRef := write("ref1.csv", "Time,Ch1,Ch2\n0.0,1,2\n")
	twoRowRef := write("ref2.csv", "Time,Ch1,Ch2\n0.0,1,2\n0.1,3,4\n")
	nonNumeric := write("text.csv", "Time,Ch1\n0.0,abc\n")
	// OutputDir 的父層是一般檔案 → placement 的 MkdirAll 失敗(只包 errno,不帶路徑)。
	blockedCfg := config.DefaultConfig()
	blockedCfg.InputDir = inDir
	blockedCfg.OutputDir = blockedOutputDir(t, t.TempDir())
	blockedApp := NewApp(blockedCfg, "test")
	phases := []PhaseSpec{{Name: "P1", StartTime: "0", EndTime: "0.01"}}

	const reservedZh = "檔案名稱驗證失敗: 檔案名稱不能使用保留字: CON"
	const reservedEn = "File name validation failed: File name must not use a reserved name: CON"

	cases := []struct {
		name     string
		call     func() error
		key      string
		zhTW     string // 完整文字;空字串時只斷言前綴
		enUS     string
		zhPrefix string
		enPrefix string
	}{
		{
			name: "MaxMean 讀檔",
			call: func() error {
				_, err := app.CalculateMaxMean(MaxMeanParams{InputPath: reserved, WindowSize: 5})
				return err
			},
			key:  i18n.KeyErrorHandlerReadFileFailed,
			zhTW: "讀取檔案失敗: " + reservedZh,
			enUS: "Failed to read file: " + reservedEn,
		},
		{
			name: "MaxMean 計算",
			call: func() error {
				_, err := app.CalculateMaxMean(MaxMeanParams{InputPath: short, WindowSize: 100})
				return err
			},
			key:      i18n.KeyErrorHandlerCalculateFailed,
			zhPrefix: "計算失敗: ",
			enPrefix: "Calculation failed: ",
		},
		{
			name: "MaxMean 寫檔",
			call: func() error {
				_, err := blockedApp.CalculateMaxMean(MaxMeanParams{InputPath: valid, WindowSize: 5})
				return err
			},
			key:      i18n.KeyErrorHandlerWriteOutputFailed,
			zhPrefix: "寫入輸出檔案失敗: ",
			enPrefix: "Failed to write output file: ",
		},
		{
			name: "Normalize 讀主要檔",
			call: func() error {
				_, err := app.NormalizeData(NormalizeParams{MainFile: reserved, ReferenceFile: valid})
				return err
			},
			key:  i18n.KeyErrorHandlerReadMainFileFailed,
			zhTW: "讀取主要資料檔案失敗: " + reservedZh,
			enUS: "Failed to read main data file: " + reservedEn,
		},
		{
			name: "Normalize 讀參考檔",
			call: func() error {
				_, err := app.NormalizeData(NormalizeParams{MainFile: valid, ReferenceFile: reserved})
				return err
			},
			key:  i18n.KeyErrorHandlerReadReferenceFileFailed,
			zhTW: "讀取參考資料檔案失敗: " + reservedZh,
			enUS: "Failed to read reference data file: " + reservedEn,
		},
		{
			name: "Normalize 計算",
			call: func() error {
				_, err := app.NormalizeData(NormalizeParams{MainFile: valid, ReferenceFile: twoRowRef})
				return err
			},
			key:      i18n.KeyErrorHandlerNormalizeCalcFailed,
			zhPrefix: "標準化計算失敗: ",
			enPrefix: "Normalisation calculation failed: ",
		},
		{
			name: "Normalize 保存",
			call: func() error {
				_, err := app.NormalizeData(NormalizeParams{MainFile: valid, ReferenceFile: oneRowRef, OutputPath: "CON"})
				return err
			},
			key:      i18n.KeyErrorHandlerSaveResultFailed,
			zhPrefix: "保存結果失敗: ",
			enPrefix: "Failed to save result: ",
		},
		{
			name: "Phase 讀檔",
			call: func() error {
				_, err := app.AnalyzePhases(PhaseParams{InputFile: reserved, Phases: phases})
				return err
			},
			key:  i18n.KeyErrorHandlerReadDataFileFailed,
			zhTW: "讀取資料檔案失敗: " + reservedZh,
			enUS: "Failed to read data file: " + reservedEn,
		},
		{
			name: "Phase 分析",
			call: func() error {
				_, err := app.AnalyzePhases(PhaseParams{InputFile: nonNumeric, Phases: phases})
				return err
			},
			key:      i18n.KeyErrorHandlerPhaseAnalysisFailed,
			zhPrefix: "階段分析失敗: ",
			enPrefix: "Phase analysis failed: ",
		},
		{
			name: "Phase 保存",
			call: func() error {
				_, err := app.AnalyzePhases(PhaseParams{InputFile: valid, Phases: phases, OutputPath: "CON"})
				return err
			},
			key:      i18n.KeyErrorHandlerSaveResultFailed,
			zhPrefix: "保存結果失敗: ",
			enPrefix: "Failed to save result: ",
		},
		{
			name: "批次空路徑(producer 本身也帶 key)",
			call: func() error {
				_, err := app.CalculateMaxMean(MaxMeanParams{InputPath: "", WindowSize: 5, IsBatch: true})
				return err
			},
			key:  i18n.KeyErrorHandlerDirPathInvalid,
			zhTW: "目錄路徑驗證失敗: 目錄路徑不能為空",
			enUS: "Directory path validation failed: Directory path cannot be empty",
		},
		{
			name: "批次搜尋 CSV(外部路徑,Glob pattern 錯誤)",
			call: func() error {
				_, err := app.CalculateMaxMean(MaxMeanParams{
					InputPath: filepath.Join(t.TempDir(), "bad["), WindowSize: 5, IsBatch: true,
				})
				return err
			},
			key:  i18n.KeyErrorHandlerGlobCSVFailed,
			zhTW: "搜尋CSV文件失敗: syntax error in pattern",
			enUS: "Failed to search for CSV files: syntax error in pattern",
		},
		{
			name: "批次列出 CSV(InputDir 下不存在的目錄;錯誤帶路徑、出口脫敏)",
			call: func() error {
				_, err := app.CalculateMaxMean(MaxMeanParams{InputPath: "missing_dir", WindowSize: 5, IsBatch: true})
				return err
			},
			key:      i18n.KeyErrorHandlerListCSVFailed,
			zhPrefix: "列出CSV文件失敗: ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i18n.SetLocale(i18n.LocaleZhTW)
			err := tc.call()
			require.Error(t, err)

			var keyed *i18n.Error
			require.True(t, errors.As(err, &keyed), "應為 *i18n.Error,實際 %T: %v", err, err)
			assert.Equal(t, tc.key, keyed.Key)

			i18n.SetLocale(i18n.LocaleEnUS)
			defer i18n.SetLocale(i18n.LocaleZhTW)

			if tc.zhTW != "" {
				assert.Equal(t, tc.zhTW, err.Error(), "Error() 固定是 zh-TW")
				assert.Equal(t, tc.enUS, i18n.Localize(err), "Localize 依目前 locale(en-US)")

				return
			}

			assert.True(t, strings.HasPrefix(err.Error(), tc.zhPrefix), "got %q", err.Error())
			if tc.enPrefix != "" {
				assert.True(t, strings.HasPrefix(i18n.Localize(err), tc.enPrefix), "got %q", i18n.Localize(err))
			}
		})
	}
}

// TestCalculateMaxMeanBatch_SummaryMessageLocalized 釘住批次結果 Message(全部失敗時前端以
// showError 顯示)由 handler 依目前 locale 以 status.maxmean.batch_summary 渲染。
func TestCalculateMaxMeanBatch_SummaryMessageLocalized(t *testing.T) {
	prevLocale := i18n.GetLocale()
	t.Cleanup(func() { i18n.SetLocale(prevLocale) })

	cfg := config.DefaultConfig()
	cfg.InputDir = t.TempDir()
	cfg.OutputDir = t.TempDir()
	app := NewApp(cfg, "test")

	batchDir := t.TempDir()
	for _, name := range []string{"a.csv", "b.csv"} {
		require.NoError(t, os.WriteFile(filepath.Join(batchDir, name), []byte("Time,Ch1\n"), 0o600))
	}

	for locale, want := range map[i18n.Locale]string{
		i18n.LocaleZhTW: "批次處理完成：成功 0 個檔案，失敗 2 個檔案",
		i18n.LocaleEnUS: "Batch processing finished: 0 files succeeded, 2 files failed",
	} {
		i18n.SetLocale(locale)
		result, err := app.CalculateMaxMean(MaxMeanParams{InputPath: batchDir, WindowSize: 5, IsBatch: true})
		require.NoError(t, err)
		assert.False(t, result.Success)
		assert.Equal(t, want, result.Message, "locale %s", locale)
	}
}
