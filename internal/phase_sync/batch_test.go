package phase_sync //nolint:revive // underscore in package name matches directory structure

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
)

// TestExportResults_SubjectWithRTLAndControl_FilenameSanitized 隨 ADR-0001
// 把 PhaseSync 寫檔職責搬到 csvHandler 同步移除。對應的 RTL / NUL / ZWSP /
// bidi-iso / CRLF subject sanitization 契約由
// io.TestWritePhaseSyncResult_SubjectSanitization 釘住 — 內部仍走
// calculator.GenerateOutputFileName -> SanitizeFileName 同一份過濾規則,
// migrate 前後行為對等。

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
