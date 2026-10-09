package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/logging"
	"count_mean/internal/models"
	"count_mean/internal/phase_sync"
)

// setupNormalizedPhaseSyncTestApp 構造一個僅含 AnalyzeNormalizedPhaseSync 所需依賴
// 的最小 App：logger、config（OutputDir）、phaseSyncAnalyzer、csvHandler。
// csvHandler 由 buildAppState 統一建立，確保 Output 2 的 WriteNormalizedPhaseSyncResult 呼叫不 nil-deref。
// 其他 field 留 nil，被該 handler 觸及時會立刻 panic 暴露問題。
func setupNormalizedPhaseSyncTestApp(t *testing.T) *App {
	t.Helper()

	cfg := &config.AppConfig{
		OutputDir: t.TempDir(),
	}

	app := &App{
		logger:            logging.GetLogger("normalized_phase_sync_test"),
		phaseSyncAnalyzer: phase_sync.NewPhaseSyncAnalyzer(),
	}
	app.state.Store(buildAppState(cfg))
	return app
}

// writeMinimalMotionFile 建立最小 Motion CSV（4 行 metadata + 指定行數資料）。
// 採用既有 internal/phase_sync 內 createTestMotionFileN 的格式。
func writeMinimalMotionFile(t *testing.T, dir string, numRows int) {
	t.Helper()
	content := "Line 1: Metadata\nLine 2: More metadata\nLine 3: Additional info\nIndex,X,Y,Z\n"
	for i := 1; i <= numRows; i++ {
		content += fmt.Sprintf("%d,10.5,20.3,30.8\n", i)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "motion.csv"), []byte(content), 0o644))
}

// writeMinimalForceFile 建立最小 Force .anc 檔（1000Hz 採樣，指定秒數）。
func writeMinimalForceFile(t *testing.T, dir string, durationSec float64) {
	t.Helper()
	numRows := int(durationSec * 1000)
	content := fmt.Sprintf(
		"File_Type:\tAnalog R/C ASCII\tGeneration#:\t2\n"+
			"Board_Type:\tUSB-6225\tPolarity:\tBipolar\n"+
			"Trial_Name:\tTest_Trial\tTrial#:\t1\tDuration(Sec.):\t%.6f\t#Channels:\t2\n"+
			"BitDepth:\t16\tPreciseRate:\t1000.000000\n\n\n\n\n"+
			"Name\tF1X\tF1Y\nRate\t1000\t1000\nRange\t10000\t10000\n",
		durationSec,
	)
	for i := 0; i < numRows; i++ {
		content += fmt.Sprintf("%.6f\t100\t200\n", float64(i)/1000.0)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "force.anc"), []byte(content), 0o644))
}

// writeMinimalEMGFile 建立最小 EMG CSV（常數值，足以讓 normalize 跑通）。
func writeMinimalEMGFile(t *testing.T, dir string, startTime, endTime float64) {
	t.Helper()
	content := "Time,Ch1,Ch2\n"
	numRows := int((endTime - startTime) * 1000)
	for i := 0; i <= numRows; i++ {
		time := startTime + float64(i)/1000.0
		content += fmt.Sprintf("%.6f,100.5,200.3\n", time)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "emg.csv"), []byte(content), 0o644))
}

// setupNormalizedPhaseSyncFixture 建立完整測試 fixture（manifest + motion + force + emg），
// 回傳 manifest 檔案路徑與 data folder。
// Phase 點配置：P0=0.1, P1=0.2, P2=0.3, S=0.4, C=0.5, D=400, T0=0.6, T=0.7, O=600, L=0.8
// EMGMotionOffset=1，motion 1000 列、force 1 秒、EMG 0~1 秒。
func setupNormalizedPhaseSyncFixture(t *testing.T) (manifestPath, dataFolder string) {
	t.Helper()
	dataFolder = t.TempDir()

	writeMinimalMotionFile(t, dataFolder, 1000)
	writeMinimalForceFile(t, dataFolder, 1.0)
	writeMinimalEMGFile(t, dataFolder, 0, 1.0)

	manifestContent := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		"TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8"
	manifestPath = filepath.Join(dataFolder, "manifest.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))
	return manifestPath, dataFolder
}

// TestAnalyzeNormalizedPhaseSync_SameRangeBackwardCompat 驗證當 Norm 與 Stats 兩組
// 範圍指向同一對分期點時，結果欄位反映同一時間範圍——這是重構前的單一範圍行為的
// 等價驗證（regression）。
func TestAnalyzeNormalizedPhaseSync_SameRangeBackwardCompat(t *testing.T) {
	app := setupNormalizedPhaseSyncTestApp(t)
	manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)

	params := NormalizedPhaseSyncParams{
		ManifestFile:    manifestPath,
		DataFolder:      dataFolder,
		SubjectIndex:    0,
		NormStartPhase:  "P0",
		NormEndPhase:    "P2",
		StatsStartPhase: "P0",
		StatsEndPhase:   "P2",
	}

	result, err := app.AnalyzeNormalizedPhaseSync(params)
	require.NoError(t, err)
	require.True(t, result.Success, "Message: %s", result.Message)

	assert.FileExists(t, result.NormalizedEMGPath)
	assert.FileExists(t, result.PhaseSyncCSVPath)
	assert.Equal(t, "P0", string(result.NormStartPhase))
	assert.Equal(t, "P2", string(result.NormEndPhase))
	assert.Equal(t, "P0", string(result.StatsStartPhase))
	assert.Equal(t, "P2", string(result.StatsEndPhase))
	assert.InDelta(t, result.NormStartTime, result.StatsStartTime, 1e-9,
		"同範圍時 NormStartTime 與 StatsStartTime 應相同")
	assert.InDelta(t, result.NormEndTime, result.StatsEndTime, 1e-9,
		"同範圍時 NormEndTime 與 StatsEndTime 應相同")
	assert.Contains(t, result.PhaseSyncCSVPath, "norm-P0-P2")
	assert.Contains(t, result.PhaseSyncCSVPath, "stats-P0-P2")
}

// TestAnalyzeNormalizedPhaseSync_DifferentRanges 驗證 Norm 與 Stats 為不同分期區間
// 時，handler 確實分別解析兩組範圍並反映在結果欄位與輸出檔名上。
func TestAnalyzeNormalizedPhaseSync_DifferentRanges(t *testing.T) {
	app := setupNormalizedPhaseSyncTestApp(t)
	manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)

	// Norm 寬區間 P0(0.1)→P2(0.3)，Stats 窄區間 P1(0.2)→P2(0.3)
	params := NormalizedPhaseSyncParams{
		ManifestFile:    manifestPath,
		DataFolder:      dataFolder,
		SubjectIndex:    0,
		NormStartPhase:  "P0",
		NormEndPhase:    "P2",
		StatsStartPhase: "P1",
		StatsEndPhase:   "P2",
	}

	result, err := app.AnalyzeNormalizedPhaseSync(params)
	require.NoError(t, err)
	require.True(t, result.Success, "Message: %s", result.Message)

	assert.Equal(t, "P0", string(result.NormStartPhase))
	assert.Equal(t, "P2", string(result.NormEndPhase))
	assert.Equal(t, "P1", string(result.StatsStartPhase))
	assert.Equal(t, "P2", string(result.StatsEndPhase))

	assert.Less(t, result.NormStartTime, result.StatsStartTime,
		"Norm 起始時間（P0=0.1）應早於 Stats 起始時間（P1=0.2）")
	assert.InDelta(t, result.NormEndTime, result.StatsEndTime, 1e-9,
		"兩組都以 P2 結束，結束時間應相同")

	assert.Contains(t, result.PhaseSyncCSVPath, "norm-P0-P2")
	assert.Contains(t, result.PhaseSyncCSVPath, "stats-P1-P2")
}

// TestAnalyzeNormalizedPhaseSync_StatsPhaseOrderError 驗證統計區間順序錯誤時，
// handler 回 Success=false 且錯誤訊息明確指出是「統計區間」而非「標準化區間」。
func TestAnalyzeNormalizedPhaseSync_StatsPhaseOrderError(t *testing.T) {
	app := setupNormalizedPhaseSyncTestApp(t)
	manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)

	params := NormalizedPhaseSyncParams{
		ManifestFile:    manifestPath,
		DataFolder:      dataFolder,
		SubjectIndex:    0,
		NormStartPhase:  "P0",
		NormEndPhase:    "P2",
		StatsStartPhase: "P2", // 故意反序
		StatsEndPhase:   "P0",
	}

	result, err := app.AnalyzeNormalizedPhaseSync(params)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "統計區間",
		"錯誤訊息應指明是統計區間")
}

// TestAnalyzeNormalizedPhaseSync_StatsZeroDurationRejected 驗證統計區間使用同一個
// 分期點（start == end，例如 P1 → P1）會被擋下，回 Success=false。
//
// Regression：重構前單一範圍時，runValidationPipeline 內 validatePhaseOrder 會以
// startOrder >= endOrder 為條件擋住「同 phase」case；拆分後 Stats 那組原本只走
// 分期時間計算，該處只擋 start > end，導致 P1 → P1 會穿透並產生 zero-duration
// stats CSV。現在 phase_sync 對兩組分期點都在 I/O 之前呼叫 ValidatePhaseOrder
// (validatePhasePair,ADR-0047)。
func TestAnalyzeNormalizedPhaseSync_StatsZeroDurationRejected(t *testing.T) {
	app := setupNormalizedPhaseSyncTestApp(t)
	manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)

	params := NormalizedPhaseSyncParams{
		ManifestFile:    manifestPath,
		DataFolder:      dataFolder,
		SubjectIndex:    0,
		NormStartPhase:  "P0",
		NormEndPhase:    "P2",
		StatsStartPhase: "P1", // 同 phase
		StatsEndPhase:   "P1",
	}

	result, err := app.AnalyzeNormalizedPhaseSync(params)
	require.NoError(t, err)
	assert.False(t, result.Success,
		"Stats 區間 P1 → P1 應該被擋下，避免產生 zero-duration 輸出")
	assert.Contains(t, result.Message, "統計區間",
		"錯誤訊息應指明是統計區間")
	assert.Contains(t, result.Message, "start phase must be before end phase",
		"錯誤訊息應反映 ValidatePhaseOrder 的根本原因")
}

// TestAnalyzeNormalizedPhaseSync_RejectsInvalidExternalPath 釘住 (b):
// 兩條 CSV 輸出路徑(normalizedEMGPath / phaseSyncCSVPath)由 outputDir +
// subject 拼出來,subject 雖經 filename.Sanitize 仍可能拼出意外路徑。Output 1
// 路徑驗證現屬 CSVHandler.WriteNormalizedPhaseSyncEMG(ADR-0020);Output 2 由
// WriteNormalizedPhaseSyncResult 守門,兩者均在寫檔前 reject 落入系統敏感目錄的路徑。
//
// 此 test 透過注入 OutputDir = "/etc/..."(系統敏感前綴)強迫 boundary check
// 觸發。修法前:Output 1 由 GUI 層自行拼路徑並直接寫檔,OS perm/ENOENT 錯誤
// 訊息洩漏完整 absolute path 給 patient。
// 修法後:result.Message 應含「輸出路徑無效」(CSVHandler 的 wrap
// prefix)而非 OS-level error,anti-PII-leak 意圖保持。
func TestAnalyzeNormalizedPhaseSync_RejectsInvalidExternalPath(t *testing.T) {
	app := setupNormalizedPhaseSyncTestApp(t)

	// 把 OutputDir 改成 /etc 子目錄(系統敏感前綴,security.ValidateExternalPath
	// 必擋),強迫 boundary 觸發。/etc 的子目錄即使 ENOENT 也應該被 path
	// validator 提早 reject,不該交給 OS 寫到一半才 fail。
	// 用 buildAppState(非裸 &appState{})確保 csvHandler 非 nil — 即使日後 Output 1
	// 的 boundary guard 放寬,Output 2 的 s.csvHandler 路徑也不會 nil-deref。
	app.state.Store(buildAppState(&config.AppConfig{OutputDir: "/etc/normalized_phase_sync_invalid"}))

	manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)

	params := NormalizedPhaseSyncParams{
		ManifestFile:    manifestPath,
		DataFolder:      dataFolder,
		SubjectIndex:    0,
		NormStartPhase:  "P0",
		NormEndPhase:    "P2",
		StatsStartPhase: "P0",
		StatsEndPhase:   "P2",
	}

	result, err := app.AnalyzeNormalizedPhaseSync(params)
	require.NoError(t, err, "預期失敗走 result.Success=false 路徑,Go err 應為 nil")
	require.NotNil(t, result)
	assert.False(t, result.Success,
		"OutputDir 落在 /etc 系統敏感目錄,應在寫檔前被 boundary validate 攔下")

	// boundary validation 觸發後 message 含「輸出路徑無效」(CSVHandler
	// WriteNormalizedPhaseSyncEMG 的 wrap prefix);若 fix 未生效,訊息會帶 OS-level
	// error 字面如 "open /etc/...: no such file or directory",含完整 absolute path PII。
	// anti-PII-leak 驗證:訊息不應含完整路徑 /etc/normalized_phase_sync_invalid/...。
	assert.Contains(t, result.Message, "輸出路徑無效",
		"boundary path validation 應在 OS write 前 reject,error message 應走 CSVHandler 的 wrap 格式")
	assert.NotContains(t, result.Message, "/etc/normalized_phase_sync_invalid",
		"完整的 /etc 子目錄路徑不應洩漏進 result.Message(anti-PII-leak)")
}

// TestAnalyzeNormalizedPhaseSync_NormPhaseNotFound 驗證標準化區間 endPhase 不存在
// 於 manifest 時，handler 回 Success=false 且錯誤訊息明確指出不存在的 phase 名稱。
//
// 分期點名稱與順序在任何 I/O 之前驗證(ADR-0047),錯誤訊息前綴為
// 「標準化區間: 分期點順序驗證失敗: …」(搬移前是「載入資料失敗: …」)。
func TestAnalyzeNormalizedPhaseSync_NormPhaseNotFound(t *testing.T) {
	app := setupNormalizedPhaseSyncTestApp(t)
	manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)

	params := NormalizedPhaseSyncParams{
		ManifestFile:    manifestPath,
		DataFolder:      dataFolder,
		SubjectIndex:    0,
		NormStartPhase:  "P0",
		NormEndPhase:    "P99", // 不存在
		StatsStartPhase: "P0",
		StatsEndPhase:   "P2",
	}

	result, err := app.AnalyzeNormalizedPhaseSync(params)
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "P99",
		"錯誤訊息應指明不存在的 phase 名稱以利使用者定位")
	assert.Contains(t, result.Message, "unknown phase point",
		"錯誤訊息應指明 phase 未知")
	assert.True(t, strings.HasPrefix(result.Message, i18n.T(i18n.KeyErrorHandlerNormRange)+": 分期點順序驗證失敗: "),
		"Message 應以標準化區間前綴開頭,實際 %q", result.Message)
}

// TestAnalyzeNormalizedPhaseSync_StatsFailureWritesNoOutput 釘住 ADR-0047 的行為改變:
// 統計步驟失敗時不寫任何輸出(搬移前 Output 1 在統計之前就已寫出)。EMG 在統計區間
// (S=0.4 → T=0.7)內、標準化區間(P0=0.1 → P2=0.3)外有一筆 NaN:標準化通過(NaN
// 不在取最大值的區間內),統計的 NaN 檢查失敗。
func TestAnalyzeNormalizedPhaseSync_StatsFailureWritesNoOutput(t *testing.T) {
	app := setupNormalizedPhaseSyncTestApp(t)
	outDir := t.TempDir()
	app.state.Store(buildAppState(&config.AppConfig{OutputDir: outDir}))
	manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)

	var emg strings.Builder
	emg.WriteString("Time,Ch1,Ch2\n")
	for i := 0; i <= 1000; i++ {
		ch1 := "100.5"
		if i == 500 {
			ch1 = "NaN"
		}
		fmt.Fprintf(&emg, "%.6f,%s,200.3\n", float64(i)/1000.0, ch1)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dataFolder, "emg.csv"), []byte(emg.String()), 0o600))

	result, err := app.AnalyzeNormalizedPhaseSync(NormalizedPhaseSyncParams{
		ManifestFile:    manifestPath,
		DataFolder:      dataFolder,
		SubjectIndex:    0,
		NormStartPhase:  "P0",
		NormEndPhase:    "P2",
		StatsStartPhase: "S",
		StatsEndPhase:   "T",
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.True(t, strings.HasPrefix(result.Message, i18n.T(i18n.KeyErrorHandlerCalcStatsFailed)+": "),
		"Message 應以統計失敗前綴開頭,實際 %q", result.Message)
	entries, readErr := os.ReadDir(outDir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "統計失敗時不得寫出 Output 1 / Output 2")
}

// TestAnalyzeNormalizedPhaseSync_PhaseOrderCheckedBeforeIO 釘住 ADR-0047 的行為改變:
// 兩組分期點的順序錯誤在任何 I/O 之前回報(manifest 不存在也不會先報載入失敗),
// 各帶自己區間的前綴。
func TestAnalyzeNormalizedPhaseSync_PhaseOrderCheckedBeforeIO(t *testing.T) {
	const orderErr = "分期點順序驗證失敗: 開始分期點 P2 與結束分期點 P0: start phase must be before end phase"
	cases := []struct {
		name        string
		norm, stats [2]models.PhasePoint
		wantMessage string
	}{
		{
			name:        "標準化區間反序",
			norm:        [2]models.PhasePoint{models.PhaseP2, models.PhaseP0},
			stats:       [2]models.PhasePoint{models.PhaseP0, models.PhaseP2},
			wantMessage: i18n.T(i18n.KeyErrorHandlerNormRange) + ": " + orderErr,
		},
		{
			name:        "統計區間反序",
			norm:        [2]models.PhasePoint{models.PhaseP0, models.PhaseP2},
			stats:       [2]models.PhasePoint{models.PhaseP2, models.PhaseP0},
			wantMessage: i18n.T(i18n.KeyErrorHandlerStatsRange) + ": " + orderErr,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupNormalizedPhaseSyncTestApp(t)
			dataFolder := t.TempDir()

			result, err := app.AnalyzeNormalizedPhaseSync(NormalizedPhaseSyncParams{
				ManifestFile:    filepath.Join(dataFolder, "missing_manifest.csv"),
				DataFolder:      dataFolder,
				SubjectIndex:    0,
				NormStartPhase:  tc.norm[0],
				NormEndPhase:    tc.norm[1],
				StatsStartPhase: tc.stats[0],
				StatsEndPhase:   tc.stats[1],
			})

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.False(t, result.Success)
			assert.Equal(t, tc.wantMessage, result.Message)
		})
	}
}

// TestNormalizedPhaseSyncFailKey 釘住 adapter 的 stage → i18n key 對映(Ruling 24):
// 每個 phase_sync.Stage 用搬移前 handler 那一步的 key;ctx 取消 →「分析已取消」。
func TestNormalizedPhaseSyncFailKey(t *testing.T) {
	cause := errors.New("cause")
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"load", &phase_sync.AnalysisError{Stage: phase_sync.StageLoad, Err: cause}, i18n.KeyErrorHandlerLoadDataFailed},
		{"norm range", &phase_sync.AnalysisError{Stage: phase_sync.StageNormRange, Err: cause}, i18n.KeyErrorHandlerNormRange},
		{"stats range", &phase_sync.AnalysisError{Stage: phase_sync.StageStatsRange, Err: cause}, i18n.KeyErrorHandlerStatsRange},
		{"normalize", &phase_sync.AnalysisError{Stage: phase_sync.StageNormalize, Err: cause}, i18n.KeyErrorHandlerNormalizeFailed},
		{"stats slice", &phase_sync.AnalysisError{Stage: phase_sync.StageStatsSlice, Err: cause}, i18n.KeyErrorHandlerExtractStatsRangeFailed},
		{"statistics", &phase_sync.AnalysisError{Stage: phase_sync.StageStatistics, Err: cause}, i18n.KeyErrorHandlerCalcStatsFailed},
		{"canceled", context.Canceled, i18n.KeyErrorHandlerCancelled},
		{"deadline", fmt.Errorf("wrapped: %w", context.DeadlineExceeded), i18n.KeyErrorHandlerCancelled},
		{"untagged", cause, i18n.KeyErrorHandlerLoadDataFailed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizedPhaseSyncFailKey(tc.err))
		})
	}
}

// TestAnalyzeNormalizedPhaseSync_LoadAndNormalizeFailurePrefix 經 handler 端到端確認
// phase_sync 把載入與標準化兩步的失敗標上對的 Stage(Message 前綴與搬移前相同)。
func TestAnalyzeNormalizedPhaseSync_LoadAndNormalizeFailurePrefix(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(t *testing.T, dataFolder string)
		wantPrefix string
	}{
		{
			name: "EMG 檔不存在 → 載入資料失敗",
			mutate: func(t *testing.T, dataFolder string) {
				require.NoError(t, os.Remove(filepath.Join(dataFolder, "emg.csv")))
			},
			wantPrefix: i18n.T(i18n.KeyErrorHandlerLoadDataFailed) + ": EMG 檔案不存在 (emg.csv)",
		},
		{
			name: "Norm 區間內通道全為 0 → 標準化失敗",
			mutate: func(t *testing.T, dataFolder string) {
				var emg strings.Builder
				emg.WriteString("Time,Ch1,Ch2\n")
				for i := 0; i <= 1000; i++ {
					fmt.Fprintf(&emg, "%.6f,0,200.3\n", float64(i)/1000.0)
				}
				require.NoError(t, os.WriteFile(filepath.Join(dataFolder, "emg.csv"), []byte(emg.String()), 0o600))
			},
			wantPrefix: i18n.T(i18n.KeyErrorHandlerNormalizeFailed) + `: 肌肉 "Ch1"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := setupNormalizedPhaseSyncTestApp(t)
			manifestPath, dataFolder := setupNormalizedPhaseSyncFixture(t)
			tc.mutate(t, dataFolder)

			result, err := app.AnalyzeNormalizedPhaseSync(NormalizedPhaseSyncParams{
				ManifestFile:    manifestPath,
				DataFolder:      dataFolder,
				SubjectIndex:    0,
				NormStartPhase:  "P0",
				NormEndPhase:    "P2",
				StatsStartPhase: "P0",
				StatsEndPhase:   "P2",
			})

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.False(t, result.Success)
			assert.True(t, strings.HasPrefix(result.Message, tc.wantPrefix),
				"Message 應以 %q 開頭,實際 %q", tc.wantPrefix, result.Message)
		})
	}
}

// TestAnalyzeNormalizedPhaseSync_StatsRangeErrorPrecedesNormalizeError 釘住 ADR-0047
// 的錯誤優先序:Stats 區間在標準化之前先解析,所以兩者都會失敗時,回報的是「統計區間」
// 而不是「標準化失敗」(AnalyzeNormalizedPhaseSync 預先解析 Stats 區間的唯一理由)。
//
// 構造:EMG 所有通道恆為 0 → Norm 區間內最大值為 0,標準化會失敗(ErrZeroChannelMax);
// 同時 manifest 的 L 未提供(NA)→ Stats 區間 S → L 解析失敗(ErrPhaseValueZero)。
func TestAnalyzeNormalizedPhaseSync_StatsRangeErrorPrecedesNormalizeError(t *testing.T) {
	app := setupNormalizedPhaseSyncTestApp(t)

	dataFolder := t.TempDir()
	writeMinimalMotionFile(t, dataFolder, 1000)
	writeMinimalForceFile(t, dataFolder, 1.0)
	emg := "Time,Ch1,Ch2\n"
	for i := 0; i <= 1000; i++ {
		emg += fmt.Sprintf("%.6f,0,0\n", float64(i)/1000.0)
	}
	require.NoError(t, os.WriteFile(filepath.Join(dataFolder, "emg.csv"), []byte(emg), 0o644))

	manifestContent := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n" +
		"TestSubject,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,NA"
	manifestPath := filepath.Join(dataFolder, "manifest.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))

	result, err := app.AnalyzeNormalizedPhaseSync(NormalizedPhaseSyncParams{
		ManifestFile:    manifestPath,
		DataFolder:      dataFolder,
		SubjectIndex:    0,
		NormStartPhase:  "P0",
		NormEndPhase:    "P2",
		StatsStartPhase: "S",
		StatsEndPhase:   "L",
	})
	require.NoError(t, err)
	require.False(t, result.Success)
	assert.True(t, strings.HasPrefix(result.Message, i18n.T(i18n.KeyErrorHandlerStatsRange)+": "),
		"Stats 區間錯誤應先於標準化錯誤,實際 Message: %s", result.Message)
	assert.Contains(t, result.Message, "phase value is zero or not set")
	assert.NotContains(t, result.Message, i18n.T(i18n.KeyErrorHandlerNormalizeFailed))
}
