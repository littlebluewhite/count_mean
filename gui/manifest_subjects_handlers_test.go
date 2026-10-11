package gui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
)

// writeSubjectsManifest 在 TempDir 寫一份只有 manifest 的 fixture(不建任何資料檔)。
func writeSubjectsManifest(t *testing.T, rows ...string) string {
	t.Helper()

	content := "Subject,Motion,Force,EMG,EMGMotionOffset,P0,P1,P2,S,C,D,T0,T,O,L\n"
	for _, subject := range rows {
		content += subject + ",motion.csv,force.anc,emg.csv,1,0.1,0.2,0.3,0.4,0.5,400,0.6,0.7,600,0.8\n"
	}
	path := filepath.Join(t.TempDir(), "manifest.csv")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// TestLoadManifestSubjects_ListsSubjectNames 釘住四個有 subject 的 panel 共用的下拉來源
// (ADR-0057):非空 Subject 依 manifest 順序列出;不吃資料夾,資料檔不存在也照樣列出。
func TestLoadManifestSubjects_ListsSubjectNames(t *testing.T) {
	app := setupHandlerTestApp(t)

	result, err := app.LoadManifestSubjects(writeSubjectsManifest(t, "SF2", "", "SF1"))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success, "Message: %s", result.Message)
	assert.Equal(t, []string{"SF2", "SF1"}, result.Subjects)
}

// TestLoadManifestSubjects_LoadFailureOnResultChannel 釘住讀檔失敗走 result 通道:
// LoadManifests 拒絕撞名的 manifest → Success=false、Message 帶「載入分期總檔案失敗」前綴,
// err 為 nil(err 通道只留給 panic)。
func TestLoadManifestSubjects_LoadFailureOnResultChannel(t *testing.T) {
	app := setupHandlerTestApp(t)

	result, err := app.LoadManifestSubjects(writeSubjectsManifest(t, "A/B", "A:B"))

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.False(t, result.Success)
	assert.Nil(t, result.Subjects)
	assert.Equal(t,
		`載入分期總檔案失敗: 輸出檔名衝突: subject "A/B" 與 "A:B" 經檔名安全化後同為 "A_B" (case-insensitive)`+
			`；若 Subject 是中文且分期總檔案為 Big5 編碼，請另存為 UTF-8 或改用英數 Subject`,
		result.Message)
}

// TestLoadManifestSubjects_EmptyPathUnifiedChannel 釘住空路徑回 ErrNoManifestFile,走 result 通道。
func TestLoadManifestSubjects_EmptyPathUnifiedChannel(t *testing.T) {
	app := setupHandlerTestApp(t)

	assertUnifiedFailureChannel(t,
		app.LoadManifestSubjects,
		resultSuccessAccessor[ManifestSubjectsResult]{
			Success: func(r *ManifestSubjectsResult) bool { return r.Success },
			Message: func(r *ManifestSubjectsResult) string { return r.Message },
		},
		[]unifiedChannelCase[string, ManifestSubjectsResult]{
			{name: "no_manifest", params: "", expectInMsg: ErrNoManifestFile.Error()},
		},
	)
}

// TestLoadManifestSubjects_PanicRecovery 釘住首句 defer recoverHandlerPanic:nil logger 讓
// entry log panic,handler 回 (nil, ErrInternalPanic)。
func TestLoadManifestSubjects_PanicRecovery(t *testing.T) {
	app := &App{logger: nil}
	app.state.Store(&appState{config: &config.AppConfig{OutputDir: t.TempDir()}})

	result, err := app.LoadManifestSubjects(filepath.Join(t.TempDir(), "x.csv"))

	require.ErrorIs(t, err, ErrInternalPanic)
	assert.Nil(t, result)
}
