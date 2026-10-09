package gui

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/io"
	"count_mean/internal/logging"
)

// setupChartComposerTestApp 構造僅含 Chart Composer handler 所需依賴的最小 App.
// 與 sibling setupMuscleRatioTestApp 對稱 — 不啟動真實 ctx /
// 其他 analyzer,只專注於 handler 邊界行為。
//
// OutputDir 與其他 sibling test 一致用 t.TempDir(),DownloadChartComposerImage
// 走 config.OutputDir + SubjectOutputName 推導檔名(對稱 DownloadCCIChart)。
func setupChartComposerTestApp(t *testing.T) *App {
	t.Helper()

	cfg := &config.AppConfig{OutputDir: t.TempDir()}

	app := &App{
		logger: logging.GetLogger("chart_composer_test"),
	}
	app.state.Store(&appState{config: cfg})

	return app
}

// writeChartComposerMinimalEMG 建立最小 EMG CSV(2 sec, 2 channels)。
// EMG header 內含 R.RA / R.ES 等;chart composer 依 EMG.Headers 渲染全部通道,
// 會把這些 channel 都渲染進圖表。
func writeChartComposerMinimalEMG(t *testing.T, path string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("Time,R.RA,R.ES\n")
	for i := 0; i <= 2000; i++ {
		fmt.Fprintf(&b, "%.6f,%.4f,%.4f\n", float64(i)/1000.0, 100.0, 50.0)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
}

// writeChartComposerMinimalMotion 建立最小 motion CSV(對齊 Motion parser 期待的
// 4-line metadata + Index, X, Y, Z 結構)。
func writeChartComposerMinimalMotion(t *testing.T, path string, rows int) {
	t.Helper()

	var b strings.Builder
	b.WriteString("Line 1: Metadata\nLine 2: More metadata\nLine 3: Additional info\nIndex,X,Y,Z\n")
	for i := 1; i <= rows; i++ {
		fmt.Fprintf(&b, "%d,10.5,20.3,30.8\n", i)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
}

// writeChartComposerMinimalForce 建立最小 force .anc 檔(供 manifest validation;
// 與 normalized_phase_sync_handlers_test 對稱)。
func writeChartComposerMinimalForce(t *testing.T, path string, durationSec float64) {
	t.Helper()

	numRows := int(durationSec * 1000)
	var b strings.Builder
	fmt.Fprintf(&b,
		"File_Type:\tAnalog R/C ASCII\tGeneration#:\t2\n"+
			"Board_Type:\tUSB-6225\tPolarity:\tBipolar\n"+
			"Trial_Name:\tTest_Trial\tTrial#:\t1\tDuration(Sec.):\t%.6f\t#Channels:\t2\n"+
			"BitDepth:\t16\tPreciseRate:\t1000.000000\n\n\n\n\n"+
			"Name\tF1X\tF1Y\nRate\t1000\t1000\nRange\t10000\t10000\n",
		durationSec,
	)
	for i := 0; i < numRows; i++ {
		fmt.Fprintf(&b, "%.6f\t100\t200\n", float64(i)/1000.0)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
}

// writeChartComposerMuscleRatioFile 以 CSVHandler.WriteMuscleRatioOutputAll(真 writer,
// 非手寫 CSV 字串)產出 muscle_ratio fixture 並搬到 path。times 為 EMG 時間軸,
// ratios 為 4 個 pair(RA/ES, IL/GMax, RF/BF, TAIO/MF)的值,NaN 由 writer 寫成空 cell。
func writeChartComposerMuscleRatioFile(t *testing.T, path string, times []float64, ratios [][]float64) {
	t.Helper()

	dir := filepath.Dir(path)
	cfg := config.DefaultConfig()
	cfg.InputDir, cfg.OutputDir, cfg.OperateDir = dir, dir, dir
	written, err := io.NewCSVHandler(cfg).WriteMuscleRatioOutputAll(io.WriteRequest{}, io.MuscleRatioOutputAllPayload{
		Subject:    "fixture",
		PairLabels: []string{"RA/ES", "IL/GMax", "RF/BF", "TAIO/MF"},
		Times:      times,
		Ratios:     ratios,
	})
	require.NoError(t, err)
	require.NoError(t, os.Rename(written, path))
}

// writeChartComposerMinimalMuscleRatio 建立最小 muscle_ratio CSV(0..1s、101 點,
// 4 個 ratio 欄為常數)。
func writeChartComposerMinimalMuscleRatio(t *testing.T, path string) {
	t.Helper()

	times := make([]float64, 0, 101)
	for i := 0; i <= 100; i++ {
		times = append(times, float64(i)/100.0)
	}
	ratios := make([][]float64, 4)
	for k, v := range []float64{1.2, 0.8, 1.5, 0.5} {
		ratios[k] = make([]float64, len(times))
		for i := range times {
			ratios[k][i] = v
		}
	}
	writeChartComposerMuscleRatioFile(t, path, times, ratios)
}

// setupChartComposerV10Fixture 建 V.10 manifest fixture(15 欄,無 MuscleRatioFile)。
// 回傳 manifest path + data folder。
func setupChartComposerV10Fixture(t *testing.T) (manifestPath, dataFolder string) {
	t.Helper()
	dataFolder = t.TempDir()

	writeChartComposerMinimalMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000)
	writeChartComposerMinimalForce(t, filepath.Join(dataFolder, "force.anc"), 2.0)
	writeChartComposerMinimalEMG(t, filepath.Join(dataFolder, "emg.csv"))

	manifestContent := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		"TestSubjectV10,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8"
	manifestPath = filepath.Join(dataFolder, "manifest_v10.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))
	return manifestPath, dataFolder
}

// setupChartComposerV14Fixture 建 V.14 manifest fixture(16 欄,帶 MuscleRatioFile)。
func setupChartComposerV14Fixture(t *testing.T) (manifestPath, dataFolder string) {
	t.Helper()
	dataFolder = t.TempDir()

	writeChartComposerMinimalMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000)
	writeChartComposerMinimalForce(t, filepath.Join(dataFolder, "force.anc"), 2.0)
	writeChartComposerMinimalEMG(t, filepath.Join(dataFolder, "emg.csv"))
	writeChartComposerMinimalMuscleRatio(t, filepath.Join(dataFolder, "muscle_ratio.csv"))

	manifestContent := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L,MuscleRatioFile\n" +
		"TestSubjectV14,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8,muscle_ratio.csv"
	manifestPath = filepath.Join(dataFolder, "manifest_v14.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))
	return manifestPath, dataFolder
}

// ---------------------------------------------------------------------------
// LoadChartComposerSubjects
// ---------------------------------------------------------------------------

func TestLoadChartComposerSubjects_HappyPath(t *testing.T) {
	app := setupChartComposerTestApp(t)
	manifestPath, dataFolder := setupChartComposerV10Fixture(t)

	result, err := app.LoadChartComposerSubjects(&LoadChartComposerSubjectsParams{
		ManifestPath: manifestPath,
		DataFolder:   dataFolder,
	})

	require.NoError(t, err, "expected non-nil err only via panic recovery")
	require.NotNil(t, result)
	assert.True(t, result.Success, "Message: %s", result.Message)
	assert.Equal(t, []string{"TestSubjectV10"}, result.Subjects)
}

// TestLoadChartComposerSubjects_NilParams 釘住 nil params guard:
// 不可 panic,回 failedResult 結構。
func TestLoadChartComposerSubjects_NilParams(t *testing.T) {
	app := setupChartComposerTestApp(t)

	result, err := app.LoadChartComposerSubjects(nil)

	require.NoError(t, err, "nil params 應走單一通道契約 — err 留 nil,result 帶 failure")
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "參數為空")
}

// TestLoadChartComposerSubjects_RejectsTraversalPath 釘住 boundary path validation:
// "../etc/passwd" 之類 traversal 路徑必須在 manifest 解析前 reject。
func TestLoadChartComposerSubjects_RejectsTraversalPath(t *testing.T) {
	app := setupChartComposerTestApp(t)

	result, err := app.LoadChartComposerSubjects(&LoadChartComposerSubjectsParams{
		ManifestPath: "/etc/passwd",
		DataFolder:   t.TempDir(),
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "路徑驗證失敗")
}

// TestLoadChartComposerSubjects_SurfacesMissingEMGFiles 釘住 Bug 1 整合:
// LoadChartComposerSubjects 在 Load 階段呼叫 manifest.ValidateAllEMGFiles
// 掃過所有 EMG 檔,把不存在 row 收集到 result.MissingFiles surface 給 user。
// Success 仍為 true(non-blocking) — user 仍可在 dropdown 選其他 OK subject
// 繼續分析,只是有 missing 的會被警告。
//
// 動機:V.14 manifest 升級時 NSF 系列 EMGFile 欄誤改,user 在 Chart Composer
// Generate 階段才看到「EMG 檔案不存在」錯誤;此整合讓 Load 階段就 surface,
// 避免 user 進 dropdown 選 subject 後才在最後一步炸。
func TestLoadChartComposerSubjects_SurfacesMissingEMGFiles(t *testing.T) {
	app := setupChartComposerTestApp(t)
	dataFolder := t.TempDir()

	// 只建 ok subject 對應的 EMG;broken subject 的 EMG 故意不建。
	writeChartComposerMinimalEMG(t, filepath.Join(dataFolder, "exists.csv"))

	// 注意:LoadChartComposerSubjects 只 LoadManifests + dedup,**不會** 嘗試
	// parse motion/force(那是 GenerateChartComposer 的事),所以 fixture 不必建
	// motion.csv / force.anc。Subject dropdown 邏輯只看 EMGFile 欄是否 disk 上存在。
	manifestContent := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		"OkSubject,motion.csv,force.anc,exists.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8\n" +
		"BrokenSubject,motion.csv,force.anc,missing.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8"
	manifestPath := filepath.Join(dataFolder, "manifest.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))

	result, err := app.LoadChartComposerSubjects(&LoadChartComposerSubjectsParams{
		ManifestPath: manifestPath,
		DataFolder:   dataFolder,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success,
		"missing EMG 不該阻擋 Load (non-blocking surface),Message=%s", result.Message)
	assert.Equal(t, []string{"OkSubject", "BrokenSubject"}, result.Subjects,
		"dropdown 仍應列出兩個 subject — missing 的 user 仍可選看到 warning")

	require.Len(t, result.MissingFiles, 1,
		"應 surface 1 個 missing EMG row,實際 %d:%+v", len(result.MissingFiles), result.MissingFiles)
	assert.Equal(t, "BrokenSubject", result.MissingFiles[0].Subject)
	assert.Equal(t, "missing.csv", result.MissingFiles[0].EMGFile)
	assert.Contains(t, result.MissingFiles[0].ErrMessage, "missing.csv",
		"ErrMessage 應含 missing.csv 名稱 / path 讓 user 知道期待的檔位置")
}

// ---------------------------------------------------------------------------
// GenerateChartComposer
// ---------------------------------------------------------------------------

// TestGenerateChartComposer_V10_TwoGrid 驗 V.10(無 muscle_ratio)一鍵生成:
// 預設全通道,EMGMotionOffset 從 manifest row 讀取(ADR-0013)。兩個 EMG channel
// 應都渲染進 HTML。
func TestGenerateChartComposer_V10_TwoGrid(t *testing.T) {
	app := setupChartComposerTestApp(t)
	manifestPath, dataFolder := setupChartComposerV10Fixture(t)

	result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
		ManifestPath: manifestPath,
		DataFolder:   dataFolder,
		Subject:      "TestSubjectV10",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "Message: %s", result.Message)
	assert.NotEmpty(t, result.HTML, "HTML 應包含 echarts 渲染後內容")
	// 對齊 chart.RenderComposer 渲染後典型內容 — title "Chart Composer" subtitle 帶 subject。
	assert.Contains(t, result.HTML, "Chart Composer")
	// 預設全通道,兩個 EMG channel 都應出現。
	assert.Contains(t, result.HTML, "R.RA", "預設全通道 — R.RA 必須渲染")
	assert.Contains(t, result.HTML, "R.ES", "預設全通道 — R.ES 必須渲染")
}

func TestGenerateChartComposer_V14_ThreeGrid(t *testing.T) {
	app := setupChartComposerTestApp(t)
	manifestPath, dataFolder := setupChartComposerV14Fixture(t)

	result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
		ManifestPath: manifestPath,
		DataFolder:   dataFolder,
		Subject:      "TestSubjectV14",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "Message: %s", result.Message)
	assert.NotEmpty(t, result.HTML)
}

func TestGenerateChartComposer_NilParams(t *testing.T) {
	app := setupChartComposerTestApp(t)

	result, err := app.GenerateChartComposer(nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "參數為空")
}

func TestGenerateChartComposer_RejectsTraversalPath(t *testing.T) {
	app := setupChartComposerTestApp(t)

	result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
		ManifestPath: "/etc/passwd",
		DataFolder:   t.TempDir(),
		Subject:      "Whatever",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "路徑驗證失敗")
}

// TestGenerateChartComposer_LoadFailureMessagePerStage 釘住 adapter 的 envelope 對映
// (Ruling 23):composer.Load 每個失敗 Stage 各用自己的 i18n 前綴(failMessage),
// Subject 不在 manifest 走 inputMessage(無前綴)。Message 文字與資料組裝搬進
// internal/composer 前逐字相同。
func TestGenerateChartComposer_LoadFailureMessagePerStage(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(t *testing.T, params *GenerateChartComposerParams)
		wantPrefix string
	}{
		{
			name: "manifest 載入失敗",
			mutate: func(_ *testing.T, p *GenerateChartComposerParams) {
				p.ManifestPath = filepath.Join(p.DataFolder, "missing_manifest.csv")
			},
			wantPrefix: i18n.T(i18n.KeyErrorHandlerLoadManifestFailed) + ": ",
		},
		{
			name:       "Subject 不在 manifest(輸入錯誤)",
			mutate:     func(_ *testing.T, p *GenerateChartComposerParams) { p.Subject = "Nope" },
			wantPrefix: `Subject "Nope" 不存在於分期總檔案`,
		},
		{
			name: "EMG 開檔失敗",
			mutate: func(t *testing.T, p *GenerateChartComposerParams) {
				require.NoError(t, os.Remove(filepath.Join(p.DataFolder, "emg.csv")))
			},
			wantPrefix: i18n.T(i18n.KeyErrorHandlerResolveEMGPathFailed) + ": ",
		},
		{
			name: "EMG 解析失敗",
			mutate: func(t *testing.T, p *GenerateChartComposerParams) {
				require.NoError(t, os.WriteFile(filepath.Join(p.DataFolder, "emg.csv"), []byte("Time,R.RA\n"), 0o600))
			},
			wantPrefix: i18n.T(i18n.KeyErrorHandlerParseEMGFailed) + ": ",
		},
		{
			name: "motion 失敗",
			mutate: func(t *testing.T, p *GenerateChartComposerParams) {
				require.NoError(t, os.Remove(filepath.Join(p.DataFolder, "motion.csv")))
			},
			wantPrefix: i18n.T(i18n.KeyErrorHandlerParseMotionFailed) + ": Motion 路徑解析失敗: ",
		},
		{
			name: "muscle_ratio 失敗",
			mutate: func(t *testing.T, p *GenerateChartComposerParams) {
				require.NoError(t, os.Remove(filepath.Join(p.DataFolder, "muscle_ratio.csv")))
			},
			wantPrefix: i18n.T(i18n.KeyErrorHandlerParseMuscleRatioFailed) + ": muscle_ratio 路徑解析失敗: ",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupChartComposerTestApp(t)
			manifestPath, dataFolder := setupChartComposerV14Fixture(t)
			params := &GenerateChartComposerParams{
				ManifestPath: manifestPath,
				DataFolder:   dataFolder,
				Subject:      "TestSubjectV14",
			}
			tc.mutate(t, params)

			result, err := app.GenerateChartComposer(params)

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.False(t, result.Success)
			assert.True(t, strings.HasPrefix(result.Message, tc.wantPrefix),
				"Message 應以 %q 開頭,實際 %q", tc.wantPrefix, result.Message)
		})
	}
}

// ---------------------------------------------------------------------------
// DownloadChartComposerImage
// ---------------------------------------------------------------------------

func TestDownloadChartComposerImage_HappyPath(t *testing.T) {
	outputDir := t.TempDir()
	app := setupChartComposerTestApp(t)
	app.state.Store(&appState{config: &config.AppConfig{OutputDir: outputDir}})

	result, err := app.DownloadChartComposerImage(&DownloadChartComposerImageParams{
		Base64Data: validPNGDataURLForComposer(),
		Subject:    "NSF1",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	want := filepath.Join(outputDir, "NSF1_chart_composer.png")
	assert.Equal(t, want, result.OutputPath)

	_, statErr := os.Stat(want)
	require.NoError(t, statErr, "PNG 必須真的被寫入 disk")
}

func TestDownloadChartComposerImage_NilParams(t *testing.T) {
	app := setupChartComposerTestApp(t)

	result, err := app.DownloadChartComposerImage(nil)

	require.Error(t, err, "nil params 應走 err channel — 鏡像 DownloadCCIChart 的 dual-channel 契約")
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "參數")
}

// 註:bad-prefix→ErrInvalidImageFormat 與 sensitive-path rejection 這兩個原本
// 在此的 case，已隨 ADR-0009 Phase 2 把共用 PNG 管線抽到 downloadValidatedPNG，
// 改由 helper 的 seam test（png_download_test.go:
// TestDownloadValidatedPNG_RejectsBadPrefix /
// TestDownloadValidatedPNG_RejectsSensitivePath_NoDoubleLabel）擁有,避免重複覆蓋。
// 此處僅保留 Composer adapter 行為:nil params guard、從 Subject 經
// SubjectOutputName 推導、寫到 config.OutputDir。

// TestDownloadChartComposerImage_EmptySubjectFallsBackToUntitled 釘住 ADR-0019
// 唯一的行為變更:對稱化後空 subject 走 Sanitize 的 untitled fallback,得
// untitled_chart_composer.png(取代舊的 degenerate chart_composer_chart_composer.png)。
func TestDownloadChartComposerImage_EmptySubjectFallsBackToUntitled(t *testing.T) {
	outputDir := t.TempDir()
	app := setupChartComposerTestApp(t)
	app.state.Store(&appState{config: &config.AppConfig{OutputDir: outputDir}})

	result, err := app.DownloadChartComposerImage(&DownloadChartComposerImageParams{
		Base64Data: validPNGDataURLForComposer(),
		Subject:    "",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, filepath.Join(outputDir, "untitled_chart_composer.png"), result.OutputPath)
}

// ---------------------------------------------------------------------------
// Panic recovery — 跨 3 個 handler 共用 contract
// ---------------------------------------------------------------------------

// TestChartComposerHandlers_PanicRecovery 釘住 panic safety:
// 每個 handler 首句的 defer recoverHandlerPanic 應把任何 panic
// 轉成 ErrInternalPanic-wrapped err(named return)。
//
// 強制 panic 的最簡單方式:把 app.logger 設成 nil,handler body 進入後第一條
// logger.Info 會 nil-deref panic。recoverHandlerPanic 內部對 nil logger 已
// graceful 處理 (logPanic check),但 caller path 仍然有 panic 觸發。
func TestChartComposerHandlers_PanicRecovery(t *testing.T) {
	// 每個 handler 都用同一個構造方式驗證 panic safety;
	// 個別測試只關心 errors.Is(err, ErrInternalPanic)。
	t.Run("LoadChartComposerSubjects", func(t *testing.T) {
		app := &App{logger: nil} // nil logger → entry log 必 panic
		app.state.Store(&appState{config: &config.AppConfig{OutputDir: t.TempDir()}})

		result, err := app.LoadChartComposerSubjects(&LoadChartComposerSubjectsParams{
			ManifestPath: filepath.Join(t.TempDir(), "x.csv"),
			DataFolder:   t.TempDir(),
		})
		// recoverHandlerPanic 把 panic 包成 ErrInternalPanic;
		// result 為 zero value(*ChartComposerSubjectsResult nil pointer)。
		require.Error(t, err, "panic 應透過 named return 灌入 err")
		assert.ErrorIs(t, err, ErrInternalPanic)
		assert.Nil(t, result)
	})

	t.Run("GenerateChartComposer", func(t *testing.T) {
		app := &App{logger: nil}
		app.state.Store(&appState{config: &config.AppConfig{OutputDir: t.TempDir()}})

		result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
			ManifestPath: filepath.Join(t.TempDir(), "x.csv"),
			DataFolder:   t.TempDir(),
			Subject:      "S1",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInternalPanic)
		assert.Nil(t, result)
	})

	t.Run("DownloadChartComposerImage", func(t *testing.T) {
		app := &App{logger: nil}
		app.state.Store(&appState{config: &config.AppConfig{OutputDir: t.TempDir()}})

		result, err := app.DownloadChartComposerImage(&DownloadChartComposerImageParams{
			Base64Data: validPNGDataURLForComposer(),
			Subject:    "S1",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInternalPanic)
		assert.Nil(t, result)
	})
}

// validPNGDataURLForComposer 回傳「data:image/png;base64,...」字串,
// reuse png_validation_test.go 的 validPNGBytes(1x1 minimal valid PNG)。
func validPNGDataURLForComposer() string {
	png := validPNGBytes(1, 1)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// ---------------------------------------------------------------------------
// Codex review fix — P2 regression tests(P1 phase 換算、P2 #3 motion 首通道、
// P2 #4 muscle_ratio 空 cell 的資料組裝 test 已隨 ADR-0046 移到 internal/composer)
// ---------------------------------------------------------------------------

// TestLoadChartComposerSubjects_RejectsEmptyDataFolder 釘住 P2 finding #2:
// DataFolder 為空字串時應在 manifest 解析前 short-circuit 回 ErrNoDataFolder,
// 對齊 cci_handlers.validateCCIParams 慣例;不可走到 manifest 解析然後 silently
// 用 process cwd 解析相對 EMG 路徑。
func TestLoadChartComposerSubjects_RejectsEmptyDataFolder(t *testing.T) {
	app := setupChartComposerTestApp(t)
	manifestPath, _ := setupChartComposerV10Fixture(t)

	result, err := app.LoadChartComposerSubjects(&LoadChartComposerSubjectsParams{
		ManifestPath: manifestPath,
		DataFolder:   "", // empty — 觸發 P2 #2 finding
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, ErrNoDataFolder.Error(), result.Message,
		"DataFolder 空字串必須對齊 ErrNoDataFolder 訊息(請選擇數據資料夾)")
}

// TestGenerateChartComposer_RejectsEmptyDataFolder P2 #2 sibling
func TestGenerateChartComposer_RejectsEmptyDataFolder(t *testing.T) {
	app := setupChartComposerTestApp(t)
	manifestPath, _ := setupChartComposerV10Fixture(t)

	result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
		ManifestPath: manifestPath,
		DataFolder:   "",
		Subject:      "TestSubjectV10",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Equal(t, ErrNoDataFolder.Error(), result.Message)
}
