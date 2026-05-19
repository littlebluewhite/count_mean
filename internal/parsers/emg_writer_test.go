package parsers

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/csvutil"
	"count_mean/internal/models"
)

func newWriterTestData() *models.PhaseSyncEMGData {
	return &models.PhaseSyncEMGData{
		Time: []float64{0.000, 0.001, 0.002},
		Channels: map[string][]float64{
			"MuscleA": {0.1, 0.2, 0.3},
			"MuscleB": {0.5, 0.4, 0.3},
		},
		Headers: []string{"MuscleA", "MuscleB"},
	}
}

func TestExportPhaseSyncDataToCSV_WritesBOM(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	err := ExportPhaseSyncDataToCSV(newWriterTestData(), outPath, 6)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)

	bom := csvutil.BOMBytes()
	require.True(t, bytes.HasPrefix(content, bom), "output must start with UTF-8 BOM")
}

func TestExportPhaseSyncDataToCSV_HeaderOrderPreserved(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	data := newWriterTestData()
	err := ExportPhaseSyncDataToCSV(data, outPath, 6)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)

	// 去掉 BOM 後，第一列應為 Time,MuscleA,MuscleB
	body := strings.TrimPrefix(string(content), string(csvutil.BOMBytes()))
	firstLine := strings.SplitN(body, "\n", 2)[0]
	require.Equal(t, "Time,MuscleA,MuscleB", strings.TrimSpace(firstLine))
}

func TestExportPhaseSyncDataToCSV_PrecisionApplied(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	err := ExportPhaseSyncDataToCSV(newWriterTestData(), outPath, 3)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)

	body := strings.TrimPrefix(string(content), string(csvutil.BOMBytes()))
	lines := strings.Split(strings.TrimSpace(body), "\n")
	require.GreaterOrEqual(t, len(lines), 4) // header + 3 data rows

	// 第二列為第一筆資料：Time=0.000, MuscleA=0.100, MuscleB=0.500
	require.Equal(t, "0.000,0.100,0.500", strings.TrimSpace(lines[1]))
}

func TestExportPhaseSyncDataToCSV_DefaultPrecisionWhenZero(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	err := ExportPhaseSyncDataToCSV(newWriterTestData(), outPath, 0)
	require.NoError(t, err)

	content, err := os.ReadFile(outPath) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)

	// 預設精度 6，第一筆值 0.1 應寫成 0.100000
	body := strings.TrimPrefix(string(content), string(csvutil.BOMBytes()))
	lines := strings.Split(strings.TrimSpace(body), "\n")
	require.Contains(t, lines[1], "0.100000")
}

func TestExportPhaseSyncDataToCSV_NilDataReturnsError(t *testing.T) {
	err := ExportPhaseSyncDataToCSV(nil, "/tmp/should-not-be-created.csv", 6)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNilData)
	// 守護 i18n contract：中文 codebase 的前端會直接顯示 err.Error() 給使用者，
	// 不能丟失「EMG 數據為空」這個 domain-specific 中文訊息（codex review a5cf041 P3）。
	require.Contains(t, err.Error(), "EMG 數據為空")
}

// TestExportPhaseSyncDataToCSV_RejectsSymlinkTarget pins P1-A4-4's
// path-safety contract: ExportPhaseSyncDataToCSV does NOT itself validate
// the supplied outputPath (per doc.go the caller must), but it opens the
// target with fsperm.WriteFlags which includes O_NOFOLLOW on unix. So a
// caller mistake that lets a pre-planted symlink land in outputPath (e.g.
// attacker symlinks {outputDir}/{subject}_normalized.csv → /etc/passwd
// between path-validate and open — a TOCTOU window) will surface as an
// open error rather than silently overwriting the symlink target.
//
// This locks down the "defense in depth" promise. Removing O_NOFOLLOW from
// fsperm.WriteFlags would break this test, forcing the breaking change to
// be intentional.
func TestExportPhaseSyncDataToCSV_RejectsSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("O_NOFOLLOW is unix-only; Windows symlink protection is tracked separately (TODO_2 A7-4)")
	}

	dir := t.TempDir()
	realTarget := filepath.Join(dir, "real_target.csv")
	require.NoError(t, os.WriteFile(realTarget, []byte("untouched"), 0o600)) //nolint:gosec // t.TempDir() owned

	symlinkPath := filepath.Join(dir, "out.csv")
	require.NoError(t, os.Symlink(realTarget, symlinkPath))

	err := ExportPhaseSyncDataToCSV(newWriterTestData(), symlinkPath, 6)
	require.Error(t, err, "writer must refuse to open a symlink (O_NOFOLLOW)")

	// 原檔內容沒被覆寫 — 證明 symlink 沒被跟到底。
	got, err := os.ReadFile(realTarget) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)
	require.Equal(t, "untouched", string(got),
		"symlink target must not be overwritten; the open must have failed before any write")
}

// TestExportPhaseSyncDataToCSV_DocContract_PathValidationIsCallerResponsibility
// codifies — in test form — the doc.go statement that traversal-path rejection
// is the caller's job. The writer itself will happily write to "../foo.csv" if
// the OS permits, because every production call site (gui/normalized_phase_sync_handlers.go,
// muscle_ratio/analyzer.go, cci/analyzer.go) joins a config-derived outputDir
// with a sanitized subject filename before reaching here. If a future refactor
// adds writer-side validation that REJECTS traversal, this test should be
// updated together with the doc.go contract — both must move together.
func TestExportPhaseSyncDataToCSV_DocContract_PathValidationIsCallerResponsibility(t *testing.T) {
	dir := t.TempDir()
	// 用 ../ 但仍指向 tmpDir 之內合法檔；目的不是測穿越，而是測「writer 不會自行 reject」。
	traversalLooking := filepath.Join(dir, "subdir", "..", "out.csv")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "subdir"), 0o700))

	err := ExportPhaseSyncDataToCSV(newWriterTestData(), traversalLooking, 6)
	require.NoError(t, err,
		"writer must NOT reject paths containing '..' segments — caller is responsible")

	// 檔案實際被寫到 lexical join 後的 dir/out.csv，證明 writer 跟 OS 行為。
	cleaned := filepath.Clean(traversalLooking)
	_, err = os.Stat(cleaned)
	require.NoError(t, err, "writer wrote to OS-resolved path %s", cleaned)
}

// TestExportPhaseSyncDataToCSV_AtomicWrite_NoTmpLeftover鎖定遷移至 WriteCSVAtomic 後
// 成功路徑不會在 outputPath 旁留下 .tmp 檔(commit 階段 os.Rename 把 tmp 改名為 final)。
// 若日後 atomic-write contract 在 success path 漏 cleanup,此 test 立刻會發現。
func TestExportPhaseSyncDataToCSV_AtomicWrite_NoTmpLeftover(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")

	err := ExportPhaseSyncDataToCSV(newWriterTestData(), outPath, 6)
	require.NoError(t, err)

	// outputPath 必須存在。
	_, err = os.Stat(outPath)
	require.NoError(t, err)

	// outputPath + ".tmp" 必須不存在(WriteCSVAtomic rename 之後 tmp 已不在)。
	_, err = os.Stat(outPath + ".tmp")
	require.True(t, os.IsNotExist(err), "atomic write 完成後 tmp 不該殘留:%v", err)
}

// TestExportPhaseSyncDataToCSV_AtomicWrite_LeavesFinalUntouchedOnEmitError
// 鎖定 atomic-write 失敗路徑的契約:caller 傳入有問題的資料導致中途 fail 時,
// outputPath 既不該被建立(若不存在)、也不該被覆寫(若存在)。
//
// 觸發方式:用 outputPath 的目錄不可寫(設 mode 0o500 — 可讀可進入但不能 create),
// WriteCSVAtomic 在嘗試開 tmp 檔時應該失敗,final path 不被觸碰。
func TestExportPhaseSyncDataToCSV_AtomicWrite_LeavesFinalUntouchedOnEmitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics; Windows 行為不同")
	}

	dir := t.TempDir()
	outPath := filepath.Join(dir, "out.csv")
	// 預放一份檔案 — 失敗時內容必須維持原樣。
	require.NoError(t, os.WriteFile(outPath, []byte("pre-existing"), 0o600)) //nolint:gosec // t.TempDir owned

	// 鎖目錄為 read-only,O_CREATE|O_EXCL 開 tmp 會 fail。
	require.NoError(t, os.Chmod(dir, 0o500))
	defer func() { _ = os.Chmod(dir, 0o700) }() //nolint:errcheck // restore for t.TempDir cleanup

	err := ExportPhaseSyncDataToCSV(newWriterTestData(), outPath, 6)
	require.Error(t, err, "目錄不可寫時應 fail")

	// final path 還在,內容沒被覆寫。
	require.NoError(t, os.Chmod(dir, 0o700))
	content, readErr := os.ReadFile(outPath) //nolint:gosec // t.TempDir
	require.NoError(t, readErr)
	require.Equal(t, "pre-existing", string(content),
		"atomic-write 失敗時 final path 不該被改寫")
}

func TestExportPhaseSyncDataToCSV_RoundTripParseAndWrite(t *testing.T) {
	// 寫出後再用 EMGParser 解析，應該還原相同的資料結構。
	dir := t.TempDir()
	outPath := filepath.Join(dir, "roundtrip.csv")

	original := newWriterTestData()
	require.NoError(t, ExportPhaseSyncDataToCSV(original, outPath, 6))

	parser := NewEMGParser()
	parsed, _, err := parser.ParseFile(outPath)
	require.NoError(t, err)

	require.Equal(t, original.Headers, parsed.Headers)
	require.Len(t, parsed.Time, len(original.Time))

	for i, want := range original.Time {
		require.InDelta(t, want, parsed.Time[i], 1e-9)
	}

	for _, name := range original.Headers {
		wantCh := original.Channels[name]
		gotCh := parsed.Channels[name]
		require.Len(t, gotCh, len(wantCh), "channel %s length", name)

		for i, v := range wantCh {
			require.InDelta(t, v, gotCh[i], 1e-9, "channel %s index %d", name, i)
		}
	}
}
