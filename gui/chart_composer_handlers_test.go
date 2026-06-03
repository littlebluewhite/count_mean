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
	"count_mean/internal/logging"
	"count_mean/internal/models"
)

// TestComposerPhaseTimesEMG 鎖定 Chart Composer「少了 D / O 分期點」的修正。
//
// manifest 有 10 個分期欄,其中 D(下蹲結束)、O(展體)在 manifest 內存的是
// **motion frame index**(整數),其餘 8 個是**力板時間**秒值。修正前 handler 只把
// 8 個力板欄位送進 phaseTimes,D/O 因 domain 不同被略過 → 前端 checkbox 與 markLine
// 都看不到。修正後 composerPhaseTimesEMG 統一換算全部 10 個到 EMG 時間 domain:
// 力板走 ForceTimeToEMGTime、motion-index 走 MotionIndexToEMGTime。
//
// 用 NSF1 真實數字(EMGMotionOffset=966)驗算,並斷言 D/O 落在生物力學該有的順序
// 位置(C < D < T0、T < O < L)—— 這同時佐證 motion-index → EMG 秒數的換算正確。
func TestComposerPhaseTimesEMG(t *testing.T) {
	const offset = 966 // NSF1 EMGMotionOffset

	t.Run("V14 manifest 換算全部 10 個分期點含 D/O", func(t *testing.T) {
		src := models.PhasePoints{
			P0: models.MakeOpt(16.645),
			P1: models.MakeOpt(16.780001),
			P2: models.MakeOpt(17.224001),
			S:  models.MakeOpt(17.440001),
			C:  models.MakeOpt(17.809999),
			D:  4469, // motion-index
			T0: models.MakeOpt(18.134001),
			T:  models.MakeOpt(18.164),
			O:  4645, // motion-index
			L:  models.MakeOpt(18.775999),
		}

		got := composerPhaseTimesEMG(src, offset)

		// 10 個分期點全到齊,特別是過去被略過的 D / O。
		require.Len(t, got, 10, "全部 10 個分期點都應換算到 phaseTimes")
		for _, name := range []string{"P0", "P1", "P2", "S", "C", "D", "T0", "T", "O", "L"} {
			_, ok := got[name]
			assert.Truef(t, ok, "分期點 %s 必須出現在 phaseTimes", name)
		}

		// 力板欄位:emgTime = forceTime - (offset-1)/250 = forceTime - 3.86
		assert.InDelta(t, 12.785, got["P0"], 0.0001)
		assert.InDelta(t, 14.915999, got["L"], 0.0001)
		// motion-index 欄位:emgTime = (idx - offset)/250
		assert.InDelta(t, 14.012, got["D"], 0.0001) // (4469-966)/250
		assert.InDelta(t, 14.716, got["O"], 0.0001) // (4645-966)/250

		// 換算後落點符合生物力學順序 —— D 在 C 之後 T0 之前;O 在 T 之後 L 之前。
		assert.Less(t, got["C"], got["D"], "D(下蹲結束)應在 C 之後")
		assert.Less(t, got["D"], got["T0"], "D 應在 T0 之前")
		assert.Less(t, got["T"], got["O"], "O(展體)應在 T 之後")
		assert.Less(t, got["O"], got["L"], "O 應在 L 之前")
	})

	t.Run("motion-index sentinel 0 與未設 OptFloat 都 skip", func(t *testing.T) {
		src := models.PhasePoints{
			P0: models.MakeOpt(16.645),
			// P1..L 未設(NoOpt)
			D: 0, // 未提供 sentinel
			O: 0,
		}

		got := composerPhaseTimesEMG(src, offset)

		require.Len(t, got, 1, "只有 P0 被換算")
		_, hasP0 := got["P0"]
		assert.True(t, hasP0)
		_, hasD := got["D"]
		assert.False(t, hasD, "D=0 sentinel 不該進 phaseTimes")
		_, hasO := got["O"]
		assert.False(t, hasO, "O=0 sentinel 不該進 phaseTimes")
	})
}

// setupChartComposerTestApp 構造僅含 Chart Composer handler 所需依賴的最小 App.
// 與 sibling setupMuscleRatioTestApp / setupCCITestApp 對稱 — 不啟動真實 ctx /
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
// EMG header 內含 R.RA / R.ES 等;GenerateChartComposer 對空 SelectedChannels
// 走 composer fallback 全選,會把這些 channel 都渲染進圖表。
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

// writeChartComposerMinimalMuscleRatio 建立最小 muscle_ratio CSV(對齊
// CSVHandler.WriteMuscleRatioOutputAll 的 layout — Time + 4 ratio columns)。
func writeChartComposerMinimalMuscleRatio(t *testing.T, path string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("Time (s),RA/ES,IL/GMax,RF/BF,TAIO/MF\n")
	for i := 0; i <= 100; i++ {
		fmt.Fprintf(&b, "%.4f,1.2,0.8,1.5,0.5\n", float64(i)/100.0)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
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
// 不再傳 SelectedChannels(空 → composer fallback 全選);EMGMotionOffset 由
// handler 內部從 row 讀取(ADR-0013)。兩個 EMG channel 應都渲染進 HTML。
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
	// 空 SelectedChannels → composer fallback 全選,兩個 EMG channel 都應出現。
	assert.Contains(t, result.HTML, "R.RA",
		"空 SelectedChannels 應 fallback 全選 — R.RA 必須渲染")
	assert.Contains(t, result.HTML, "R.ES",
		"空 SelectedChannels 應 fallback 全選 — R.ES 必須渲染")
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

// TestGenerateChartComposer_UnknownSubject 釘住 manifest 找不到 subject 時的
// failure path — 應走 failed result + nil err,而非 panic。
func TestGenerateChartComposer_UnknownSubject(t *testing.T) {
	app := setupChartComposerTestApp(t)
	manifestPath, dataFolder := setupChartComposerV10Fixture(t)

	result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
		ManifestPath: manifestPath,
		DataFolder:   dataFolder,
		Subject:      "Nope",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Nope")
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
// Panic recovery — 跨 4 個 handler 共用 contract
// ---------------------------------------------------------------------------

// TestChartComposerHandlers_PanicRecovery 釘住 panic safety:
// 4 個 handler 的 defer recoverHandlerPanic / HandlerRun 應把任何 panic
// 轉成 ErrInternalPanic-wrapped err(named return)。
//
// 強制 panic 的最簡單方式:把 app.logger 設成 nil,handler body 進入後第一條
// logger.Info 會 nil-deref panic。recoverHandlerPanic 內部對 nil logger 已
// graceful 處理 (logPanic check),但 caller path 仍然有 panic 觸發。
//
// 注意:HandlerRun 本身呼叫 logger.Info,nil logger 會在 entry log 階段 panic,
// 仍由 recoverHandlerPanic 接住。
func TestChartComposerHandlers_PanicRecovery(t *testing.T) {
	// 4 個 handler 都用同一個構造方式驗證 panic safety;
	// 個別測試只關心 errors.Is(err, ErrInternalPanic)。
	t.Run("LoadChartComposerSubjects", func(t *testing.T) {
		app := &App{logger: nil} // nil logger 走 HandlerRun 內 logger.Info 必 panic
		app.state.Store(&appState{config: &config.AppConfig{OutputDir: t.TempDir()}})

		result, err := app.LoadChartComposerSubjects(&LoadChartComposerSubjectsParams{
			ManifestPath: filepath.Join(t.TempDir(), "x.csv"),
			DataFolder:   t.TempDir(),
		})
		// HandlerRun 內部對 panic 走 recoverHandlerPanic → ErrInternalPanic-wrap;
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
// Codex review fix — P1 + 4 個 P2 regression tests
// ---------------------------------------------------------------------------

// writeChartComposerSingleChannelMotion 建立**單一資料 channel** 的 motion CSV。
// 用於 P2 #3 regression — 確認 handler 不再 silently 把第一個資料 channel 當成
// Index column skip 掉。
//
// CSV 結構同 writeChartComposerMinimalMotion(3 行 metadata + 1 行 header +
// data),但 header 只有 Index + 1 個 channel "OnlyChannel"。motion parser
// 經 initializeMotionData → motion.Headers = ["OnlyChannel"];handler 若還
// 帶舊有 `if k == 0 continue` 行為,整個 motion grid 將為空,HTML 不會含
// "OnlyChannel" 字串。
func writeChartComposerSingleChannelMotion(t *testing.T, path string, rows int) {
	t.Helper()

	var b strings.Builder
	b.WriteString("Line 1: Metadata\nLine 2: More metadata\nLine 3: Additional info\nIndex,OnlyChannel\n")
	for i := 1; i <= rows; i++ {
		fmt.Fprintf(&b, "%d,10.5\n", i)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
}

// writeChartComposerMuscleRatioWithBlank 建立**含空 cell** 的 muscle_ratio CSV。
// 用於 P2 #4 regression — 確認 handler 把空 cell 換成 NaN 而非靜默 0。
//
// 第 2 列 ratio 全空(空 cell),其餘列正常填值。composer 對 NaN 走
// buildComposerLineData line 692-695:`Value: nil`;
// go-echarts LineData.Value 是 `interface{} \`json:"value,omitempty"\``,
// `nil` 觸發 omitempty → 整個 value field 被省略(空 LineData object {})。
//
// 唯一 time 用 0.0123(4 位小數)避免與 echarts 預設 option 字串的 `[0.01,0]`
// 撞;此時間值只在本 fixture 出現,assertion 對「該 time 是否還夾帶數值對 pair」
// 是穩健的 differentiator。
//
// 修法前(bug):空 cell → 0 → HTML 第 2 列 series 含 `[0.0123,0]` 共 4 處(4 條 ratio)
// 修法後:空 cell → NaN → composer Value:nil → omitempty 省略 value;HTML 不再
// 含 `[0.0123,0]` 字串。
func writeChartComposerMuscleRatioWithBlank(t *testing.T, path string) {
	t.Helper()

	var b strings.Builder
	b.WriteString("Time (s),RA/ES,IL/GMax,RF/BF,TAIO/MF\n")
	b.WriteString("0.0000,1.2,0.8,1.5,0.5\n")
	// 第 2 列 ratio 欄全空 — 模擬 muscle_ratio writer 對 NaN/Inf 寫成空 cell 的行為
	b.WriteString("0.0123,,,,\n")
	for i := 2; i <= 100; i++ {
		fmt.Fprintf(&b, "%.4f,1.2,0.8,1.5,0.5\n", float64(i)/100.0)
	}
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o644))
}

// TestGenerateChartComposer_PhaseMarkersConvertedToEMGTime 釘住 P1 finding:
// PhasePoints (P0/P1/.../T0/T/L) 是 force-time domain 秒值,handler 必須透過
// EMGMotionOffset 換算成 EMG-time domain 才能讓 markLine 正確落在 EMG grid。
//
// 換算公式來自 synchronizer.TimeSynchronizer.ForceTimeToEMGTime:
//
//	emgTime = forceTime - (emgMotionOffset - 1) / FrequencyMotion
//
// fixture: EMGMotionOffset=251 → offset 對應 (251-1)/250 = 1.0 秒;
// P0=0.5 (force-time) → 換算後 emgTime = 0.5 - 1.0 = -0.5 秒。
//
// 修法前(bug):HTML markLine.xAxis 含 `"xAxis":0.5`(原 force-time)
// 修法後:HTML markLine.xAxis 含 `"xAxis":-0.5`(EMG-time)
//
// 也要確保 force-time=0.5 不再出現在 markLine items —— 避免「修了但漏改某一條 path」。
func TestGenerateChartComposer_PhaseMarkersConvertedToEMGTime(t *testing.T) {
	app := setupChartComposerTestApp(t)

	dataFolder := t.TempDir()
	writeChartComposerMinimalMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000)
	writeChartComposerMinimalForce(t, filepath.Join(dataFolder, "force.anc"), 2.0)
	writeChartComposerMinimalEMG(t, filepath.Join(dataFolder, "emg.csv"))

	// EMGMotionOffset=251 → (251-1)/250 = 1.0 s 偏移;
	// P0=0.5 (force-time) → emgTime = 0.5 - 1.0 = -0.5
	// P1=1.5 (force-time) → emgTime = 1.5 - 1.0 = 0.5
	// (P2/S/C/T0/T/L 全部設 NA / 0 不影響 markLine,只測 P0/P1 二點即可)
	manifestContent := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		"PhaseConvertSubject,motion.csv,force.anc,emg.csv,251,0.5,1.5,NA,NA,NA,0,NA,NA,0,NA"
	manifestPath := filepath.Join(dataFolder, "manifest_p1.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))

	result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
		ManifestPath: manifestPath,
		DataFolder:   dataFolder,
		Subject:      "PhaseConvertSubject",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "Message: %s", result.Message)

	// 換算後的 EMG-time 值必須出現在 markLine
	assert.Contains(t, result.HTML, `"xAxis":-0.5`,
		"P0=0.5 (force-time) 換算後 EMG-time = -0.5;若仍為原始 force-time 表示 P1 換算未生效")
	assert.Contains(t, result.HTML, `"xAxis":0.5`,
		"P1=1.5 (force-time) 換算後 EMG-time = 0.5")

	// 換算前的 force-time 值不該出現在 markLine
	assert.NotContains(t, result.HTML, `"xAxis":1.5`,
		"P1 原始 force-time=1.5 不該漏進 markLine — 表示 P1 fix 仍在 force-time domain")
}

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

// TestGenerateChartComposer_MotionFirstChannelRendered 釘住 P2 finding #3:
// motion.Headers 已是純 channel 名(parser initializeMotionData strip 過 Index),
// handler 不該再 `if k == 0 continue` 把第一個真資料 channel 跳過。
//
// 用 single-channel motion fixture("OnlyChannel" 為唯一資料 channel);
// 修法前(bug):k=0 被 skip → motion.Order 為空 → HTML 不含 "OnlyChannel"
// 修法後:HTML series 名單含 "OnlyChannel"
func TestGenerateChartComposer_MotionFirstChannelRendered(t *testing.T) {
	app := setupChartComposerTestApp(t)

	dataFolder := t.TempDir()
	writeChartComposerSingleChannelMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000)
	writeChartComposerMinimalForce(t, filepath.Join(dataFolder, "force.anc"), 2.0)
	writeChartComposerMinimalEMG(t, filepath.Join(dataFolder, "emg.csv"))

	manifestContent := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		"MotionSingleChannelSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8"
	manifestPath := filepath.Join(dataFolder, "manifest_motion.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))

	result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
		ManifestPath: manifestPath,
		DataFolder:   dataFolder,
		Subject:      "MotionSingleChannelSubject",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "Message: %s", result.Message)
	assert.Contains(t, result.HTML, "OnlyChannel",
		"motion 唯一資料 channel 必須出現在 series 名單;若被 k==0 skip 邏輯吃掉則 HTML 無此字串")
}

// TestGenerateChartComposer_EmptyMuscleRatioCellIsNaN 釘住 P2 finding #4:
// muscle_ratio CSV 空 cell 必須 append 為 NaN(下游 composer 轉成
// LineData.Value=nil → JSON null,line gap),而非靜默變 0(會在圖上畫出
// 假的「實值為 0」資料點)。
//
// fixture 在 muscle_ratio CSV 第 2 列把所有 ratio 欄留空;composer
// (buildComposerLineData line 692-695)對 NaN 走 `Value: nil` path,
// 序列化進 HTML 必含 `null` 字串(opts.LineData{Value: nil} → JSON null)。
func TestGenerateChartComposer_EmptyMuscleRatioCellIsNaN(t *testing.T) {
	app := setupChartComposerTestApp(t)

	dataFolder := t.TempDir()
	writeChartComposerMinimalMotion(t, filepath.Join(dataFolder, "motion.csv"), 2000)
	writeChartComposerMinimalForce(t, filepath.Join(dataFolder, "force.anc"), 2.0)
	writeChartComposerMinimalEMG(t, filepath.Join(dataFolder, "emg.csv"))
	writeChartComposerMuscleRatioWithBlank(t, filepath.Join(dataFolder, "muscle_ratio.csv"))

	manifestContent := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L,MuscleRatioFile\n" +
		"MuscleRatioNaNSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8,muscle_ratio.csv"
	manifestPath := filepath.Join(dataFolder, "manifest_nan.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))

	result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
		ManifestPath: manifestPath,
		DataFolder:   dataFolder,
		Subject:      "MuscleRatioNaNSubject",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Success, "Message: %s", result.Message)
	// 修法前(bug):空 cell → 0 真值 → muscle_ratio 第 2 列(time=0.0123)4 條
	// ratio series 各序列化出 `[0.0123,0]` 字串;
	// 修法後:空 cell → NaN → composer Value:nil → opts.LineData omitempty 省略
	// 整個 value field → HTML 不再含 `[0.0123,0]`。
	// 用 4 位小數時間值 0.0123 避免與 echarts 預設 option 字串撞點。
	assert.NotContains(t, result.HTML, "[0.0123,0]",
		"muscle_ratio 空 cell 必須轉 NaN(序列化省略 value)而非靜默 0 真值")
}
