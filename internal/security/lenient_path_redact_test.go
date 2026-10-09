package security

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/security/redact"
)

const (
	plantedBaseDir = "PatientAlice_PHI_base"
	plantedSubDir  = "SubjSecret_PHI_sub"
)

// assertNoDirSegmentsLeak 釘住 webview sink 契約:redact.Paths 無法遮蔽相對路徑、
// 絕對路徑只會保留最後一段,因此 lenient 錯誤必須在來源端就只留 basename,
// base 資料夾與 manifest 子目錄名(subject / patient 資料夾)不得出現在訊息中。
func assertNoDirSegmentsLeak(t *testing.T, err error) {
	t.Helper()

	require.Error(t, err)

	got := redact.Paths(err.Error())
	require.NotContains(t, got, plantedBaseDir, "base 資料夾名外洩: %s", got)
	require.NotContains(t, got, plantedSubDir, "manifest 子目錄名外洩: %s", got)
}

func newPlantedBase(t *testing.T) string {
	t.Helper()

	base := filepath.Join(t.TempDir(), plantedBaseDir)
	require.NoError(t, os.MkdirAll(filepath.Join(base, plantedSubDir), 0o755))

	return base
}

// TestResolveLenientPath_ErrorsDoNotLeakDirSegments 涵蓋 resolveLenientPath 每個
// 會格式化 filename 的拒絕分支。
func TestResolveLenientPath_ErrorsDoNotLeakDirSegments(t *testing.T) {
	base := newPlantedBase(t)

	cases := map[string]string{
		"null byte":    plantedSubDir + "/emg\x00.csv",
		"dot-only":     plantedSubDir + "/..",
		"absolute":     "/" + plantedSubDir + "/emg.csv",
		"traversal":    plantedSubDir + "/../x/emg.csv",
		"backslash":    plantedSubDir + `\..\x\emg.csv`,
		"win absolute": `\` + plantedSubDir + `\emg.csv`,
	}

	for name, filename := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := resolveLenientPath(base, filename)
			assertNoDirSegmentsLeak(t, err)
		})
	}
}

// symlink 指向 base 外 → 「落在資料夾外」分支。
func TestResolveLenientPath_SymlinkEscapeDoesNotLeakDirSegments(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink 建立需特權")
	}

	base := newPlantedBase(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "emg.csv"), []byte("x"), 0o600))

	require.NoError(t, os.RemoveAll(filepath.Join(base, plantedSubDir)))
	require.NoError(t, os.Symlink(outside, filepath.Join(base, plantedSubDir)))

	_, err := resolveLenientPath(base, plantedSubDir+"/emg.csv")
	assertNoDirSegmentsLeak(t, err)
}

// 不存在的路徑層數超過 EvalSymlinksWithFallback 上限 → 「無法解析路徑」分支
// (包著底層錯誤)。
func TestResolveLenientPath_UnresolvableDoesNotLeakDirSegments(t *testing.T) {
	base := newPlantedBase(t)

	_, err := resolveLenientPath(base, plantedSubDir+"/a/b/c/d/e/f/g/h/i/emg.csv")
	assertNoDirSegmentsLeak(t, err)
}

// 檔案不存在 → OpenLenientValidated 的「開啟失敗」分支。
func TestOpenLenientValidated_ErrorsDoNotLeakDirSegments(t *testing.T) {
	base := newPlantedBase(t)

	_, err := OpenLenientValidated(base, plantedSubDir+"/missing.csv")
	assertNoDirSegmentsLeak(t, err)
	require.True(t, strings.Contains(redact.Paths(err.Error()), "missing.csv"),
		"basename 應保留供除錯: %v", err)
}
