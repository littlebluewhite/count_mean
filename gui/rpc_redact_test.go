package gui

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/security"
)

// plantedDirMarker 是植入目錄的辨識段。每列觸發的路徑都把它放在「非末段」位置
// (planted/missing.csv、planted/blocker…):redact.Paths 保證砍掉 basename 之前
// 的所有目錄段,但會留下末段,所以只對非末段斷言才是 redact.Paths 真正的保證。
const plantedDirMarker = "PatientAlice_PHI_7f3c"

// plantDir 在 t.TempDir() 下建立帶 plantedDirMarker 的目錄,模擬病患資料夾。
func plantDir(t *testing.T) string {
	t.Helper()

	planted := filepath.Join(t.TempDir(), plantedDirMarker)
	require.NoError(t, os.Mkdir(planted, 0o750))

	return planted
}

// leakyPrefixes 是各平台常見的 system-root 前綴(macOS t.TempDir 在 /var/folders/、
// Linux CI 在 /tmp/、Windows 在 C:\Users\…\Temp\),出現任一個即視為目錄外洩。
var leakyPrefixes = []string{
	"/Users/",
	"/home/",
	"/var/folders/",
	"/private/",
	"/Volumes/",
	"/mnt/",
	"/tmp/",
	`C:\Users\`,
	`C:\`,
}

// requireNoDirLeak 斷言送進 webview 的文字(err 文字或 result 字串欄位)不洩漏目錄段:
//
//   - 不含任何 leakyPrefixes
//   - planted 非空(呼叫端刻意觸發帶植入目錄的錯誤)時,另斷言不含 planted 絕對路徑與
//     plantedDirMarker,且帶 `<redacted-path>` 標記 —— 反向保險:錯誤若根本沒帶路徑,
//     前兩條恆真,失去守門作用。
func requireNoDirLeak(t *testing.T, text, planted string) {
	t.Helper()

	for _, prefix := range leakyPrefixes {
		require.NotContains(t, text, prefix, "webview 文字不可含 system-root 前綴")
	}

	if planted == "" {
		return
	}

	require.NotContains(t, text, plantedDirMarker, "webview 文字不可含植入的病患目錄段")
	require.NotContains(t, text, planted, "webview 文字不可含植入目錄的絕對路徑")
	require.Contains(t, text, "<redacted-path>", "觸發的錯誤應帶路徑並被 redact")
}

// newRPCRedactTestApp 走 NewAppWithConfigPath 建完整 App(buildAppState 全套依賴),
// OutputDir 與 configPath 由 caller 指定,InputDir / OperateDir 各用一個 t.TempDir(),
// 避免測試寫進 CWD。
func newRPCRedactTestApp(t *testing.T, outputDir, configPath string) *App {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.InputDir = t.TempDir()
	cfg.OperateDir = t.TempDir()
	cfg.OutputDir = outputDir

	return NewAppWithConfigPath(cfg, "test", configPath)
}

// TestRPCErrChannel_NoAbsolutePath 守 err 通道出口:每個「Go err 可能帶使用者路徑」
// 的 bound method 各一列,植入病患目錄、觸發帶路徑的錯誤,斷言
//
//  1. err 文字不含植入目錄(Wails dispatcher 以 err.Error() 送進 webview,
//     frontend main.js 原樣顯示)
//  2. errors.Is(err, 原 sentinel) 仍成立(redact 只改文字,不斷 chain)
//
// 名單:SaveConfig、CalculateMaxMean、NormalizeData、AnalyzePhases、GetCSVHeaders、
// LoadPhaseManifest、AnalyzePhaseSync(validate gate)、DownloadCCIChart、
// DownloadChartComposerImage。其餘回 error 的 bound method 不列:AnalyzeCCI /
// AnalyzeMuscleRatio / AnalyzeNormalizedPhaseSync / LoadChartComposerSubjects /
// GenerateChartComposer 走單一通道,err 只在 panic 時非 nil(ErrInternalPanic,
// 不含路徑);SetLanguage 只回 locale 字串;SelectFile / SelectDirectory 的錯誤
// 只來自 ErrAppNotReady 或 Wails 對話框 runtime(單元測試無法觸發),handler 本身
// 不把路徑組進 err。出口 redact 對它們一樣生效,只是不在此表內。
func TestRPCErrChannel_NoAbsolutePath(t *testing.T) {
	cases := []struct {
		name     string
		sentinel error
		call     func(t *testing.T, planted string) error
	}{
		{
			name:     "SaveConfig",
			sentinel: syscall.ENOTDIR,
			call: func(t *testing.T, planted string) error {
				// configPath 的 parent 是一般檔案 → MkdirAll 回 ENOTDIR,錯誤帶 parent 路徑。
				blocker := filepath.Join(planted, "blocker")
				require.NoError(t, os.WriteFile(blocker, nil, 0o600))
				app := newRPCRedactTestApp(t, t.TempDir(), filepath.Join(blocker, "config.json"))

				return app.SaveConfig(config.DefaultConfig())
			},
		},
		{
			name:     "CalculateMaxMean",
			sentinel: fs.ErrNotExist,
			call: func(t *testing.T, planted string) error {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				_, err := app.CalculateMaxMean(MaxMeanParams{
					InputPath:  filepath.Join(planted, "missing.csv"),
					WindowSize: 10,
				})

				return err
			},
		},
		{
			name:     "NormalizeData",
			sentinel: fs.ErrNotExist,
			call: func(t *testing.T, planted string) error {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				_, err := app.NormalizeData(NormalizeParams{
					MainFile:      filepath.Join(planted, "missing.csv"),
					ReferenceFile: filepath.Join(planted, "ref.csv"),
				})

				return err
			},
		},
		{
			name:     "AnalyzePhases",
			sentinel: fs.ErrNotExist,
			call: func(t *testing.T, planted string) error {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				_, err := app.AnalyzePhases(PhaseParams{
					InputFile: filepath.Join(planted, "missing.csv"),
					Phases:    []PhaseSpec{{Name: "P1", StartTime: "0", EndTime: "1"}},
				})

				return err
			},
		},
		{
			name:     "GetCSVHeaders",
			sentinel: fs.ErrNotExist,
			call: func(t *testing.T, planted string) error {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				_, err := app.GetCSVHeaders(CSVHeadersParams{FilePath: filepath.Join(planted, "missing.csv")})

				return err
			},
		},
		{
			name:     "LoadPhaseManifest",
			sentinel: fs.ErrNotExist,
			call: func(t *testing.T, planted string) error {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				_, err := app.LoadPhaseManifest(filepath.Join(planted, "missing_manifest.csv"))

				return err
			},
		},
		{
			name:     "AnalyzePhaseSync",
			sentinel: security.ErrPathTraversal,
			call: func(t *testing.T, planted string) error {
				// validate gate:含 `..` element 的 manifest 路徑 → ErrPathTraversal,
				// 錯誤文字帶原始路徑。
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				_, err := app.AnalyzePhaseSync(PhaseSyncParams{
					ManifestFile: planted + "/../manifest.csv",
					DataFolder:   planted,
					StartPhase:   "P0",
					EndPhase:     "P2",
				})

				return err
			},
		},
		{
			name:     "DownloadCCIChart",
			sentinel: fs.ErrNotExist,
			call: func(t *testing.T, planted string) error {
				// OutputDir 不存在 → WriteFileNoFollow 回 ENOENT,錯誤帶輸出路徑。
				app := newRPCRedactTestApp(t, filepath.Join(planted, "out"), "")
				_, err := app.DownloadCCIChart(CCIDownloadParams{
					ImageData: validPNGBase64DataURL(),
					Subject:   "S1",
				})

				return err
			},
		},
		{
			name:     "DownloadChartComposerImage",
			sentinel: fs.ErrNotExist,
			call: func(t *testing.T, planted string) error {
				app := newRPCRedactTestApp(t, filepath.Join(planted, "out"), "")
				_, err := app.DownloadChartComposerImage(&DownloadChartComposerImageParams{
					Base64Data: validPNGBase64DataURL(),
					Subject:    "S1",
				})

				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			planted := plantDir(t)

			err := tc.call(t, planted)
			require.Error(t, err, "觸發條件應讓 handler 走 err 通道")

			requireNoDirLeak(t, err.Error(), planted)
			assert.ErrorIs(t, err, tc.sentinel, "redact 後 errors.Is 仍須命中原 sentinel")
		})
	}
}

// TestRPCMessage_NoAbsolutePath 守 Message 通道出口:每個把可預期失敗包進 result
// 字串欄位的 handler 各一列,植入病患目錄、觸發帶路徑的錯誤,斷言送進 webview 的
// 字串不洩漏目錄段。
//
// 名單:AnalyzeCCI、AnalyzeNormalizedPhaseSync、LoadChartComposerSubjects、
// GenerateChartComposer、AnalyzePhaseSync(分析分支)走 result.Message;
// AnalyzeMuscleRatio 的逐 subject 失敗走 Subjects[i].Error(analyzer 回的字串,
// 不經 Message)。
func TestRPCMessage_NoAbsolutePath(t *testing.T) {
	missingManifest := func(planted string) string {
		return filepath.Join(planted, "missing_manifest.csv")
	}

	cases := []struct {
		name string
		call func(t *testing.T, planted string) string
	}{
		{
			name: "AnalyzeCCI",
			call: func(t *testing.T, planted string) string {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.AnalyzeCCI(CCIParams{
					ManifestFile: missingManifest(planted),
					DataFolder:   planted,
				})
				require.NoError(t, err)
				require.False(t, result.Success)

				return result.Message
			},
		},
		{
			name: "AnalyzeNormalizedPhaseSync",
			call: func(t *testing.T, planted string) string {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.AnalyzeNormalizedPhaseSync(NormalizedPhaseSyncParams{
					ManifestFile:    missingManifest(planted),
					DataFolder:      planted,
					NormStartPhase:  "P0",
					NormEndPhase:    "P2",
					StatsStartPhase: "P0",
					StatsEndPhase:   "P2",
				})
				require.NoError(t, err)
				require.False(t, result.Success)

				return result.Message
			},
		},
		{
			name: "LoadChartComposerSubjects",
			call: func(t *testing.T, planted string) string {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.LoadChartComposerSubjects(&LoadChartComposerSubjectsParams{
					ManifestPath: missingManifest(planted),
					DataFolder:   planted,
				})
				require.NoError(t, err)
				require.False(t, result.Success)

				return result.Message
			},
		},
		{
			name: "GenerateChartComposer",
			call: func(t *testing.T, planted string) string {
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.GenerateChartComposer(&GenerateChartComposerParams{
					ManifestPath: missingManifest(planted),
					DataFolder:   planted,
					Subject:      "S1",
				})
				require.NoError(t, err)
				require.False(t, result.Success)

				return result.Message
			},
		},
		{
			name: "AnalyzePhaseSync",
			call: func(t *testing.T, planted string) string {
				// 分析分支(validate 通過後)走 failed-result;validate 分支走 err 通道,
				// 由 TestRPCErrChannel_NoAbsolutePath 守。
				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.AnalyzePhaseSync(PhaseSyncParams{
					ManifestFile: missingManifest(planted),
					DataFolder:   planted,
					StartPhase:   "P0",
					EndPhase:     "P2",
				})
				require.NoError(t, err)
				require.False(t, result.Success)

				return result.Message
			},
		},
		{
			name: "AnalyzeMuscleRatio_SubjectError",
			call: func(t *testing.T, planted string) string {
				// manifest 合法、EMG 檔不存在 → 該 subject 失敗,開檔錯誤帶完整 EMG 路徑。
				manifestPath := filepath.Join(planted, "manifest.csv")
				require.NoError(t, os.WriteFile(manifestPath, []byte(
					"Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n"+
						"S1,motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8",
				), 0o600))

				app := newRPCRedactTestApp(t, t.TempDir(), "")
				result, err := app.AnalyzeMuscleRatio(MuscleRatioParams{
					ManifestFile: manifestPath,
					DataFolder:   planted,
				})
				require.NoError(t, err)
				require.Len(t, result.Subjects, 1)
				require.False(t, result.Subjects[0].Success)

				return result.Subjects[0].Error
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			planted := plantDir(t)

			requireNoDirLeak(t, tc.call(t, planted), planted)
		})
	}
}
