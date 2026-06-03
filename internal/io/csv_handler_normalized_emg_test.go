package io

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/models"
	"count_mean/internal/parsers"
)

// newNormalizedEMGTestData 回傳 3 時間點 + MuscleA/MuscleB 的測試資料，
// 與 parsers.newWriterTestData() 形狀對齊。
func newNormalizedEMGTestData() *models.PhaseSyncEMGData {
	return &models.PhaseSyncEMGData{
		Time: []float64{0.000, 0.001, 0.002},
		Channels: map[string][]float64{
			"MuscleA": {0.1, 0.2, 0.3},
			"MuscleB": {0.5, 0.4, 0.3},
		},
		Headers: []string{"MuscleA", "MuscleB"},
	}
}

// TestWriteNormalizedPhaseSyncEMG_WritesBOM 驗證輸出以 UTF-8 BOM 開頭。
func TestWriteNormalizedPhaseSyncEMG_WritesBOM(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)
	data := newNormalizedEMGTestData()

	outputPath, err := handler.WriteNormalizedPhaseSyncEMG(WriteRequest{}, data, "subject_01")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tempDir, "subject_01_normalized.csv"), outputPath)

	content, err := os.ReadFile(outputPath) //nolint:gosec // test file in t.TempDir
	require.NoError(t, err)

	require.True(t, len(content) >= 3 &&
		content[0] == 0xEF && content[1] == 0xBB && content[2] == 0xBF,
		"output must start with UTF-8 BOM")
}

// TestWriteNormalizedPhaseSyncEMG_HeaderOrderPreserved 驗證 header 為 Time,MuscleA,MuscleB。
func TestWriteNormalizedPhaseSyncEMG_HeaderOrderPreserved(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)
	data := newNormalizedEMGTestData()

	outputPath, err := handler.WriteNormalizedPhaseSyncEMG(WriteRequest{}, data, "subject_01")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tempDir, "subject_01_normalized.csv"), outputPath)

	rows := readRows(t, outputPath)
	require.GreaterOrEqual(t, len(rows), 1)
	require.Equal(t, "Time,MuscleA,MuscleB", rows[0])
}

// TestWriteNormalizedPhaseSyncEMG_NilDataReturnsError 驗證 nil data 回明確 error，
// 且 error 含 i18n 中文訊息「EMG 數據為空」。
func TestWriteNormalizedPhaseSyncEMG_NilDataReturnsError(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	_, err := handler.WriteNormalizedPhaseSyncEMG(WriteRequest{}, nil, "subject_01")
	require.Error(t, err)
	require.ErrorIs(t, err, errEmptyPhaseSyncEMGData)
	require.Contains(t, err.Error(), "EMG 數據為空")
}

// TestWriteNormalizedPhaseSyncEMG_AtomicWrite_NoTmpLeftover 驗證成功路徑不留 .tmp 殘檔。
func TestWriteNormalizedPhaseSyncEMG_AtomicWrite_NoTmpLeftover(t *testing.T) {
	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)
	data := newNormalizedEMGTestData()

	outputPath, err := handler.WriteNormalizedPhaseSyncEMG(WriteRequest{}, data, "subject_01")
	require.NoError(t, err)

	// outputPath 必須存在。
	_, err = os.Stat(outputPath)
	require.NoError(t, err)

	// outputPath + ".tmp" 必須不存在（WriteCSVAtomic rename 之後 tmp 已不在）。
	_, err = os.Stat(outputPath + ".tmp")
	require.True(t, os.IsNotExist(err), "atomic write 完成後 tmp 不該殘留: %v", err)

	// 目錄內也不該有任何 .tmp. 檔。
	entries, readErr := os.ReadDir(tempDir)
	require.NoError(t, readErr)
	for _, e := range entries {
		require.False(t, strings.Contains(e.Name(), ".tmp."),
			"no stray tmp file should remain, found: %s", e.Name())
	}
}

// TestWriteNormalizedPhaseSyncEMG_AtomicWrite_LeavesFinalUntouchedOnEmitError
// 驗證目錄不可寫時 WriteCSVAtomic 失敗，final path 不被改寫。
func TestWriteNormalizedPhaseSyncEMG_AtomicWrite_LeavesFinalUntouchedOnEmitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics; Windows 行為不同")
	}

	handler, tempDir := newFormatAwareTestHandler(t)
	outPath := filepath.Join(tempDir, "subject_01_normalized.csv")

	// 預放一份檔案，失敗時內容必須維持原樣。
	require.NoError(t, os.WriteFile(outPath, []byte("pre-existing"), 0o600)) //nolint:gosec // t.TempDir owned

	// 鎖目錄為 read-only，O_CREATE|O_EXCL 開 tmp 會 fail。
	require.NoError(t, os.Chmod(tempDir, 0o500))
	defer func() { _ = os.Chmod(tempDir, 0o700) }() //nolint:errcheck // restore for t.TempDir cleanup

	data := newNormalizedEMGTestData()
	_, err := handler.WriteNormalizedPhaseSyncEMG(WriteRequest{}, data, "subject_01")
	require.Error(t, err, "目錄不可寫時應 fail")

	// final path 還在，內容沒被覆寫。
	require.NoError(t, os.Chmod(tempDir, 0o700))
	content, readErr := os.ReadFile(outPath) //nolint:gosec // t.TempDir
	require.NoError(t, readErr)
	require.Equal(t, "pre-existing", string(content),
		"atomic-write 失敗時 final path 不該被改寫")
}

// TestWriteNormalizedPhaseSyncEMG_RoundTripParseAndWrite 寫出後用 EMGParser 解析，
// 應還原相同的資料結構（byte-parity：BOM + Time,… header + precision-6 + NaN→""）。
func TestWriteNormalizedPhaseSyncEMG_RoundTripParseAndWrite(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)
	original := newNormalizedEMGTestData()

	outputPath, err := handler.WriteNormalizedPhaseSyncEMG(WriteRequest{}, original, "subject_01")
	require.NoError(t, err)

	f, openErr := os.Open(outputPath)
	require.NoError(t, openErr)
	defer f.Close()

	parser := parsers.NewEMGParser()
	parsed, _, parseErr := parser.Parse(f, outputPath)
	require.NoError(t, parseErr)

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

// TestWriteNormalizedPhaseSyncEMG_NaNCell 鎖定 NaN/Inf → "" 慣例（Output 1 missing-data 契約）。
// 不得寫出 "NaN" 字面（Output 2 ConvertPhaseSyncResult 的 Sprintf 陷阱）。
func TestWriteNormalizedPhaseSyncEMG_NaNCell(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)

	data := &models.PhaseSyncEMGData{
		Time: []float64{math.NaN(), 0.001, math.Inf(1)},
		Channels: map[string][]float64{
			"MuscleA": {math.NaN(), 0.2, math.Inf(-1)},
		},
		Headers: []string{"MuscleA"},
	}

	outputPath, err := handler.WriteNormalizedPhaseSyncEMG(WriteRequest{}, data, "subject_nan")
	require.NoError(t, err)

	rows := readRows(t, outputPath)
	require.Len(t, rows, 4, "header + 3 data rows")

	// NaN/Inf 欄位必須是空字串，不得出現 "NaN" / "+Inf" 字面。
	for _, row := range rows[1:] {
		require.NotContains(t, row, "NaN", "NaN literal must not appear in output")
		require.NotContains(t, row, "Inf", "+Inf/-Inf literal must not appear in output")
	}
}

// TestWriteNormalizedPhaseSyncEMG_Precision6 鎖定 precision-6 輸出格式。
func TestWriteNormalizedPhaseSyncEMG_Precision6(t *testing.T) {
	t.Parallel()

	handler, _ := newFormatAwareTestHandler(t)
	data := newNormalizedEMGTestData()

	outputPath, err := handler.WriteNormalizedPhaseSyncEMG(WriteRequest{}, data, "subject_01")
	require.NoError(t, err)

	rows := readRows(t, outputPath)
	require.GreaterOrEqual(t, len(rows), 2)

	// 第一筆資料列：Time=0.000000, MuscleA=0.100000, MuscleB=0.500000
	require.Equal(t, "0.000000,0.100000,0.500000", rows[1])
}

// TestWriteNormalizedPhaseSyncEMG_ReplaceContract_SymlinkWithinBase
// 鎖定 phaseSyncAtomicWrite 的 replace-contract：base 內部 symlink 指向 base 內目標時，
// WriteNormalizedPhaseSyncEMG 成功、symlink entry 被替換成 regular file、原 target 不被覆寫。
//
// ADR-0020 symlink disposition：
//   - TestCSVHandler_WriteAllowsSymlinkWithinBase（symlink_test.go:67）只覆蓋 WriteCSV 路徑，
//     不覆蓋 phaseSyncAtomicWrite seam → 新增本 test 補上 replace-contract 釘住。
//   - WriteCSVAtomic 的 tmp+rename 語意：rename 替換 symlink entry（非跟到 target），
//     target 內容不變，symlink 被換成 regular file — 此為 atomic write 的設計意圖。
func TestWriteNormalizedPhaseSyncEMG_ReplaceContract_SymlinkWithinBase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink replace-contract is unix-only (O_NOFOLLOW / rename semantics differ on Windows)")
	}

	t.Parallel()

	handler, tempDir := newFormatAwareTestHandler(t)

	// 在 base 內建立真實 target。
	realTarget := filepath.Join(tempDir, "real_target.csv")
	require.NoError(t, os.WriteFile(realTarget, []byte("untouched"), 0o600)) //nolint:gosec // t.TempDir() owned

	// subject_01_normalized.csv → real_target.csv（symlink 在 base 內）。
	symlinkPath := filepath.Join(tempDir, "subject_01_normalized.csv")
	require.NoError(t, os.Symlink(realTarget, symlinkPath))

	data := newNormalizedEMGTestData()
	outputPath, err := handler.WriteNormalizedPhaseSyncEMG(WriteRequest{}, data, "subject_01")
	require.NoError(t, err, "base 內部 symlink 應被 replace，write 應成功")
	require.Equal(t, symlinkPath, outputPath)

	// symlink entry 被 rename 替換成 regular file（不再是 symlink）。
	info, statErr := os.Lstat(symlinkPath)
	require.NoError(t, statErr)
	require.False(t, info.Mode()&os.ModeSymlink != 0,
		"symlink entry 應已被替換成 regular file (Lstat 不應看到 ModeSymlink)")

	// 原 target 內容沒被覆寫。
	got, readErr := os.ReadFile(realTarget) //nolint:gosec // t.TempDir
	require.NoError(t, readErr)
	require.Equal(t, "untouched", string(got),
		"symlink target 不應被覆寫；rename 替換了 entry，target 不變")
}
