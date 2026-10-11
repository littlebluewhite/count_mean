package muscle_ratio

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/i18n"
	"count_mean/internal/io"
)

// TestMain 初始化 i18n global singleton,locale 釘 zh-TW。
//
// muscle_ratio 的錯誤是帶 key 的 i18n.Error,Error() 固定以內建 zh-TW catalog 渲染、
// 不依賴 global(ADR-0048);global 供測試切 locale 驗證 i18n.Localize ——
// globalI18n==nil 時 SetLocale 是 no-op。InitI18n 內部會 DetectSystemLocale 後
// SetLocale,CI runner 的 LANG=en_US.UTF-8 會蓋成 LocaleEnUS,故再顯式 SetLocale(LocaleZhTW)。
func TestMain(m *testing.M) {
	_ = i18n.InitI18n("./nonexistent") // 不存在路徑 → fallback 走內建 translationData
	i18n.SetLocale(i18n.LocaleZhTW)    // 覆寫 InitI18n 內部的 system-locale detection
	os.Exit(m.Run())
}

// newTestCSVHandler 建立一個以 outDir 為輸出根的 CSVHandler,供 analyzer_test 注入 Params.CSVHandler。
func newTestCSVHandler(outDir string) *io.CSVHandler {
	return io.NewCSVHandler(&config.AppConfig{
		OutputDir:     outDir,
		ScalingFactor: 3,
		Precision:     6,
	})
}

// emgHeaderLine 是 8 通道 R.* EMG CSV 的標準標題列（與 input/NSF_*_RMS*.csv 對齊）。
const emgHeaderLine = `X [],R.RA: EMG 1 ->Filter->RMS [],R.ES: EMG 2 ->Filter->RMS [],` +
	`R.IL: EMG 3 ->Filter->RMS [],R.GMax: EMG 4 ->Filter->RMS [],` +
	`R.RF: EMG 5 ->Filter->RMS [],R.BF: EMG 6 ->Filter->RMS [],` +
	`R.TA&IO: EMG 7 ->Filter->RMS [],R.MF: EMG 8 ->Filter->RMS []`

// emgHeaderLine7 缺第 8 通道（R.MF），用於 missing-channel fail-fast 測試。
const emgHeaderLine7 = `X [],R.RA: EMG 1 ->Filter->RMS [],R.ES: EMG 2 ->Filter->RMS [],` +
	`R.IL: EMG 3 ->Filter->RMS [],R.GMax: EMG 4 ->Filter->RMS [],` +
	`R.RF: EMG 5 ->Filter->RMS [],R.BF: EMG 6 ->Filter->RMS [],` +
	`R.TA&IO: EMG 7 ->Filter->RMS []`

const manifestHeaderLine = `Subject,motion file,Force Plate file,EMG file,EMG第一筆時間對應Motion的時間index值,` +
	`P0,P1,P2,S,C,D,T0,T,O,L`

// writeEMG8 寫一個 8 通道 EMG CSV，sample 數為 nSamples，dt=0.001s（1000Hz）。
// 每個通道的值是固定的（chValue[i]）以便驗證計算結果。
func writeEMG8(t *testing.T, path string, nSamples int, chValue [8]float64) {
	t.Helper()

	var b strings.Builder
	b.WriteString(emgHeaderLine)
	b.WriteByte('\n')

	for i := 0; i < nSamples; i++ {
		time := float64(i) * 0.001
		fmt.Fprintf(&b, "%.4f", time)
		for _, v := range chValue {
			fmt.Fprintf(&b, ",%.6f", v)
		}
		b.WriteByte('\n')
	}

	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
}

// writeEMG7 寫一個只有 7 通道的 EMG CSV（缺 R.MF），用於 fail-fast 測試。
func writeEMG7(t *testing.T, path string, nSamples int) {
	t.Helper()

	var b strings.Builder
	b.WriteString(emgHeaderLine7)
	b.WriteByte('\n')

	for i := 0; i < nSamples; i++ {
		time := float64(i) * 0.001
		fmt.Fprintf(&b, "%.4f,1.0,1.0,1.0,1.0,1.0,1.0,1.0\n", time)
	}

	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
}

// writeManifest 寫一個 manifest CSV，每 row 是一個 subject。
// row 由 [Subject, EMGFile, EMGMotionOffset, P0..L] 共 15 個欄位構成。
func writeManifest(t *testing.T, path string, rows [][]string) {
	t.Helper()

	var b strings.Builder
	b.WriteString(manifestHeaderLine)
	b.WriteByte('\n')

	for _, row := range rows {
		require.Equal(t, 15, len(row), "manifest row must have 15 fields")
		b.WriteString(strings.Join(row, ","))
		b.WriteByte('\n')
	}

	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
}

// makeRow15 構造一個 manifest row（15 欄）。其中分期點 P0..L 是 string，
// 用空字串 "" 表示空（parser 會轉成 0）。
func makeRow15(subject, emgFile, offset string, phases [10]string) []string {
	// 順序：P0 P1 P2 S C D T0 T O L
	return []string{
		subject,
		"unused_motion.csv",
		"unused_force.anc",
		emgFile,
		offset,
		phases[0], phases[1], phases[2], phases[3], phases[4],
		phases[5], phases[6], phases[7], phases[8], phases[9],
	}
}

// ------------------ Tests ------------------

// TestAnalyze_ConcurrentCallsNoRace 釘住「並行 Analyze 不應觸發 race」這個 invariant。
// 之後 EMGParser 改 stateless，但 Wails 並行 RPC 仍可能在未來 refactor 引入新的 shared
// mutable state — 此 test 用 race detector 守門。
//
// 注意：此 test 必須在 `go test -race` 下跑才會觸發 race detector；普通 `go test` 看不到。
// CI 跑 `make test-race` 是專案標準流程。
//
// 穩定性：外層 for k 迴圈把整個 concurrent batch 跑 10 次，提升 race detector 在 CI 上的觸發機率
// （單次跑可能因為 goroutine 排程剛好不重疊而 false-pass）。
func TestAnalyze_ConcurrentCallsNoRace(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S", "s.csv", "1", [10]string{
			"0.01", "0.02", "0.03", "0.04", "0.05",
			"20", "0.07", "0.08", "30", "0.09",
		}),
	})

	a := NewAnalyzer()

	for k := 0; k < 10; k++ {
		// 並行 8 個 Analyze 呼叫共用同個 Analyzer instance — 模擬 Wails 並行 RPC
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := a.Analyze(context.Background(), &Params{
					ManifestFile: manifestPath,
					DataFolder:   dataDir,
					CSVHandler:   newTestCSVHandler(outDir),
				})
				// 不 assert err — concurrent writes 到同個 outDir 可能有檔案 race（O_TRUNC），
				// 但本 test 是 race detector 守門：只要 -race 不報 race 就行
				_ = err
			}()
		}
		wg.Wait()
	}
}

// TestAnalyze_EMGFileWithLiteralPercent 釘住 codex review P1 (post-impl)：
// 真實 BTS EMG 匯出檔名常含 literal "%" (例 "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv")。
// 早期實作走 security.PathValidator.GetSafePath，後者把 "%" 視為 URL-encoded 殘留並拒絕
// (pathvalidator.go:144)，導致每個此類 subject 都報 path-validation 失敗，feature 對
// standard project data 整批不可用。修法：muscle_ratio 自己的 resolveEMGPath 接受 "%"，
// 仍防 ".." traversal 與絕對路徑。
func TestAnalyze_EMGFileWithLiteralPercent(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// 模擬真實 BTS 匯出檔名（含 literal '%'）
	emgFile := "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv"
	writeEMG8(t, filepath.Join(dataDir, emgFile), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("SF8", emgFile, "1", [10]string{
			"0.01", "0.02", "0.03", "0.04", "0.05",
			"20", "0.07", "0.08", "30", "0.09",
		}),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	require.Len(t, result, 1)

	sr := result[0]
	assert.True(t, sr.Success, "含 '%%' 的 EMG 檔名應該被接受；Err=%v", sr.Err)
	assert.FileExists(t, sr.OutputAllPath)
	assert.FileExists(t, sr.OutputPhasePath)
}

// TestAnalyze_EMGFileTraversalRejected 確認 P1 fix 仍防 path traversal：
// EMG 檔名包含 ".." 路徑元素時必須被拒，即使檔案是「相對 basePath」也不行。
func TestAnalyze_EMGFileTraversalRejected(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// 故意放一個 ../ 試圖讀外部檔
	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S1", "../etc/passwd", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	require.Len(t, result, 1)

	sr := result[0]
	assert.False(t, sr.Success, "含 '../' 的 EMG 路徑應被拒")
	assert.Error(t, sr.Err)
}

// TestAnalyze_CaseOnlySubjectCollision_FailFast 釘住 codex review P2 (post-impl)：
// macOS / Windows 預設 case-insensitive filesystem，"SF8" 與 "sf8" 經 filename.Sanitize
// 後在 case-sensitive map 不衝突，但寫入磁碟時實為同檔，後者 O_TRUNC 覆寫前者輸出。
// 修法：collision key 用 strings.ToLower 做 case-insensitive normalization。
func TestAnalyze_CaseOnlySubjectCollision_FailFast(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s1.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})
	writeEMG8(t, filepath.Join(dataDir, "s2.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("SF8", "s1.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
		makeRow15("sf8", "s2.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
	})

	_, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.Error(t, err, "case-only 不同的 subject 在 case-insensitive FS 上會互覆蓋，應 fail-fast")
	assert.Contains(t, err.Error(), "輸出檔名衝突")

	// fail-fast 必須在寫任何輸出檔之前發生 — outDir 應該為空。
	// 若 fail-fast 時機延後到「跑完第一個 subject 才發現衝突」，第一個 subject 的 CSV
	// 會已寫入磁碟、覆寫了使用者既有檔案。outDir empty 是這個契約的關鍵 assertion。
	if entries, statErr := os.ReadDir(outDir); statErr == nil {
		assert.Empty(t, entries, "fail-fast 應在寫檔前發生，outDir 不該有任何檔")
	}
}

// TestAnalyze_NFCvsNFDSubjectCollision_FailFast 釘住
// macOS APFS/HFS+ 對 "café" (NFC, U+00E9) 與 "café" (NFD, e+U+0301) hash 同 on-disk name，
// 但 strings.ToLower 視為相異。若不 NFC normalize，會放行兩筆 manifest 然後第二筆覆寫前者。
// 修法：collision key 用 norm.NFC.String 包裹 strings.ToLower。
func TestAnalyze_NFCvsNFDSubjectCollision_FailFast(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s1.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})
	writeEMG8(t, filepath.Join(dataDir, "s2.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		// NFC: "café" = "caf" + U+00E9（precomposed）— 5 bytes
		makeRow15("café", "s1.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
		// NFD: "café" = "cafe" + U+0301（combining acute）
		makeRow15("café", "s2.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
	})

	_, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.Error(t, err, "NFC 與 NFD 的 \"café\" 在 macOS HFS+/APFS 上為同檔，應 fail-fast")
	assert.Contains(t, err.Error(), "輸出檔名衝突")
}

// TestAnalyze_FormulaInjectionSubject_Sanitized 釘住測試補洞：
// Subject 含 "=SUM(A1:B2)" 之類試算表公式 — filename.Sanitize 應移除路徑分隔字元（`:`、`/` 等），
// 使最終 output 檔名不含這類字元；pipeline 不應 panic 或 fail。
// 註：CSV injection 字元（=、( 等）由 csvutil.SanitizeHeaderRow 在寫檔層保護，非本 helper 職責。
func TestAnalyze_FormulaInjectionSubject_Sanitized(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("=SUM(A1:B2)", "s.csv", "1", [10]string{
			"0.01", "0.02", "0.03", "0.04", "0.05",
			"20", "0.07", "0.08", "30", "0.09",
		}),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.True(t, result[0].Success, "Subject 含公式不該讓 pipeline fail：%v", result[0].Err)

	outName := filepath.Base(result[0].OutputAllPath)
	assert.NotContains(t, outName, ":", "output 檔名不該含 `:` (路徑分隔字元)")
	assert.NotContains(t, outName, "/", "output 檔名不該含 `/`")
	assert.NotContains(t, outName, "\\", "output 檔名不該含 `\\`")
}

func TestAnalyze_DuplicateSanitizedSubjects_FailFast(t *testing.T) {
	// "A/B" 和 "A:B" 經 filename.Sanitize 都成 "A_B"，整批應 fail
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s1.csv"), 100, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})
	writeEMG8(t, filepath.Join(dataDir, "s2.csv"), 100, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("A/B", "s1.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
		makeRow15("A:B", "s2.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
	})

	_, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.Error(t, err, "重複 sanitized subject 應 fail-fast")
	assert.Contains(t, err.Error(), "輸出檔名衝突")
	assert.Contains(t, err.Error(), "A_B")
}

func TestAnalyze_TwoSubjects_BothExported(t *testing.T) {
	// 兩個 subject sanitize 後唯一 → 兩個 subject 都產出 2 個 CSV (共 4 個檔)
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s1.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})
	writeEMG8(t, filepath.Join(dataDir, "s2.csv"), 200, [8]float64{4, 2, 6, 3, 2, 4, 8, 4})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S1", "s1.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
		makeRow15("S2", "s2.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	require.Len(t, result, 2)

	for _, sr := range result {
		assert.True(t, sr.Success, "subject %s failed: %v", sr.Subject, sr.Err)
		assert.FileExists(t, sr.OutputAllPath)
		assert.FileExists(t, sr.OutputPhasePath)
	}

	// 確認 4 個檔案都在 outDir 內
	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)

	csvCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".csv") {
			csvCount++
		}
	}

	assert.Equal(t, 4, csvCount, "expected 4 CSV files (2 subjects × 2 outputs)")

	// 不只 FileExists — 實際讀檔內容確認 4 個 CSV 都有合理 header + 至少一筆 data row。
	// FileExists 對「寫到一半失敗」的截斷檔也會回 true（雖然 atomic write 後不該發生）。
	for _, sr := range result {
		for _, p := range []string{sr.OutputAllPath, sr.OutputPhasePath} {
			content, readErr := os.ReadFile(p)
			require.NoError(t, readErr, "read %s", p)
			require.NotEmpty(t, content, "%s 不該為空", p)
			// 至少包含 BOM 後的 header（"Time"）與一筆 data row（換行）
			assert.Contains(t, string(content), "Time", "%s 應含 Time header", p)
			assert.Contains(t, string(content), "\n", "%s 應有換行（至少一筆資料）", p)
		}
	}

	// Output 2 的 "(11pt avg)" 欄名由 analyzeSubject 組出(寫檔端),compute core 的測試看不到,在此釘住。
	assert.Equal(t,
		[]string{"Phase", "Time (s)", "RA/ES (11pt avg)", "IL/GMax (11pt avg)", "RF/BF (11pt avg)", "TAIO/MF (11pt avg)"},
		readCSV(t, result[0].OutputPhasePath)[0])
}

func TestAnalyze_NaNCellWrittenAsEmpty(t *testing.T) {
	// R.ES = 0 → RA/ES = NaN → CSV 該 cell 為空字串
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// R.ES = 0 在所有 samples → RA/ES NaN
	writeEMG8(t, filepath.Join(dataDir, "s1.csv"), 50, [8]float64{2, 0, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S1", "s1.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.005", "20", "0.025", "0.03", "30", "0.04"}),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	sr := result[0]
	require.True(t, sr.Success, "Err=%v", sr.Err)

	rows := readCSV(t, sr.OutputAllPath)
	require.Greater(t, len(rows), 1)
	// row 結構：Time, RA/ES, IL/GMax, RF/BF, TAIO/MF
	// 第一筆資料列：RA/ES 應為空
	assert.Equal(t, "", rows[1][1], "RA/ES should be empty when R.ES=0 (NaN)")
	assert.NotEqual(t, "", rows[1][2], "IL/GMax 應有正常數值")
}

// TestAnalyze_EMGUpstreamNaN_OutputCellEmpty 釘住 EMG 原始 cell 字面 "NaN" 時，經 parser →
// calculator → writer 整條 path 後 ratio cell 寫成空字串。與 TestAnalyze_NaNCellWrittenAsEmpty
// 互補：那邊在 calculator 層 (Ratio(x,0)) 觸發 NaN；此處在 parser 層 (字面 NaN) 觸發。
func TestAnalyze_EMGUpstreamNaN_OutputCellEmpty(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// EMG: 第一筆 R.RA 字面 "NaN"，其餘 R.RA=2.0；50 sample dt=1ms
	var b strings.Builder
	b.WriteString(emgHeaderLine + "\n")
	b.WriteString("0.0000,NaN,1.0,3.0,1.5,1.0,2.0,4.0,2.0\n")
	for i := 1; i < 50; i++ {
		fmt.Fprintf(&b, "%.4f,2.000000,1.000000,3.000000,1.500000,1.000000,2.000000,4.000000,2.000000\n",
			float64(i)*0.001)
	}
	emgPath := filepath.Join(dataDir, "s1.csv")
	require.NoError(t, os.WriteFile(emgPath, []byte(b.String()), 0o600))

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S1", "s1.csv", "1", [10]string{
			"0.01", "0.02", "0.03", "0.04", "0.005", "20", "0.025", "0.03", "30", "0.04",
		}),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	sr := result[0]
	require.True(t, sr.Success, "Err=%v", sr.Err)

	rows := readCSV(t, sr.OutputAllPath)
	require.Greater(t, len(rows), 2, "expected header + at least 2 data rows")

	// row 結構：Time, RA/ES, IL/GMax, RF/BF, TAIO/MF
	assert.Equal(t, "", rows[1][1], "RA/ES 應為空 cell when EMG R.RA cell 字面為 NaN")
	assert.NotEqual(t, "", rows[1][2], "IL/GMax 同 row 應有正常數值（NaN 不污染相鄰欄）")
	assert.Equal(t, "2.000000", rows[2][1], "下一 sample R.RA 恢復正常 → RA/ES = 2.000000")
}

// readCSV reads a CSV file and returns all rows. Drops UTF-8 BOM if present.
func readCSV(t *testing.T, path string) [][]string {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // test fixture path
	require.NoError(t, err)

	// 去 UTF-8 BOM (EF BB BF) — 用 escape sequence 避免 source 內出現 raw BOM
	content := strings.TrimPrefix(string(data), "\ufeff")

	r := csv.NewReader(strings.NewReader(content))
	rows, err := r.ReadAll()
	require.NoError(t, err)
	return rows
}

// TestAnalyze_EmptyManifest_FailFast 釘住 analyzer.go:88 的 "沒有任何主題記錄" branch：
// 只有 header 沒有 row 的 manifest 應該整體 fail，而不是回傳 zero-subjects success。
func TestAnalyze_EmptyManifest_FailFast(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestHeaderLine+"\n"), 0o600))

	_, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "沒有任何主題記錄")
}

// TestAnalyze_EmptySubject_Rejected 釘住 analyzeSubject 的 Subject 空字串守門：
// 沒擋下時 filename.Sanitize("") 產生 "_muscle_ratio.csv"，多個空 Subject 會 silently 共寫同檔。
func TestAnalyze_EmptySubject_Rejected(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	// Analyze 不再預先建輸出目錄(ADR-0052);先建好,下方才能斷言目錄內沒有 CSV。
	require.NoError(t, os.MkdirAll(outDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s1.csv"), 100, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("   ", "s1.csv", "1", [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	require.Len(t, result, 1)

	sr := result[0]
	assert.False(t, sr.Success, "Subject 為空時應失敗")
	assert.ErrorContains(t, sr.Err, "Subject 名稱為空")

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasSuffix(e.Name(), ".csv"),
			"empty-Subject 不該寫出 CSV，但找到 %s", e.Name())
	}
}

// TestAnalyze_EMGFileNotFound 釘住 analyzer.go:OpenDataFile missing 分支：
// 若 typo 檔名，回的 error 必須是「資料檔案不存在」(ErrManifestDataFileMissing)
// 而非更模糊的下游錯誤。
func TestAnalyze_EMGFileNotFound(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S1", "missing_emg.csv", "1",
			[10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	require.Len(t, result, 1)

	sr := result[0]
	assert.False(t, sr.Success)
	assert.ErrorContains(t, sr.Err, "資料檔案不存在")
}

// TestAnalyze_Output2WriteFailure_StickyOutput1Success 釘住 SubjectResult doc 契約：
// 當 Output 1 已成功寫入但 Output 2 寫入失敗，Success 必須保持 true，Error 解釋為何 Output 2 跳過。
//
// 強制 Output 2 失敗的招式：預先把 phases.csv 路徑建成「目錄」，OpenFile 會 EISDIR。
func TestAnalyze_Output2WriteFailure_StickyOutput1Success(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.MkdirAll(outDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s1.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	phasesPath := filepath.Join(outDir, "S1_muscle_ratio_phases_avg11.csv")
	require.NoError(t, os.MkdirAll(phasesPath, 0o755))

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S1", "s1.csv", "1", [10]string{
			"0.01", "0.02", "0.03", "0.04", "0.05",
			"20", "0.07", "0.08", "30", "0.09",
		}),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	require.Len(t, result, 1)

	sr := result[0]
	assert.True(t, sr.Success, "Output 1 成功後 Output 2 失敗時 Success 應為 sticky-true")
	assert.NotEmpty(t, sr.OutputAllPath, "Output 1 路徑應已設定")
	assert.FileExists(t, sr.OutputAllPath, "Output 1 檔案應存在")
	assert.Empty(t, sr.OutputPhasePath, "Output 2 失敗時 OutputPhasePath 應為空")
	assert.ErrorContains(t, sr.Err, "寫入 Output 2 失敗", "Err 應解釋 Output 2 跳過原因")
}

// TestAnalyze_SubjectOutcomes_RoutedPerSubject 釘住 analyzeSubject 怎麼處理 computeSubject 的
// 三種結果(ADR-0052),三個 subject 同一批、互不影響:
//   - core err(缺通道)→ Success=false,不寫任何檔
//   - warn(分期點落在 EMG 範圍外)→ Output 1 照寫、Output 2 跳過,Success 仍為 true(sticky-success)
//   - 兩者皆無 → 兩個 CSV 都寫
//
// core 何時回 err / warn 由 computeSubject_test.go 負責,這裡只看 adapter 的分流。
func TestAnalyze_SubjectOutcomes_RoutedPerSubject(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "ok.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})
	writeEMG7(t, filepath.Join(dataDir, "missing.csv"), 100)
	writeEMG8(t, filepath.Join(dataDir, "warn.csv"), 100, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	phases := [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}
	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("OK", "ok.csv", "1", phases),
		makeRow15("Missing", "missing.csv", "1", phases),
		// offset=10000 → 力板時間換算成 EMG 秒數約 -40s,落在 EMG [0, 0.099] 外 → warn
		makeRow15("Warn", "warn.csv", "10000", phases),
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err)
	require.Len(t, result, 3)

	ok, missing, warn := result[0], result[1], result[2]

	assert.True(t, ok.Success, "Err=%v", ok.Err)
	assert.NoError(t, ok.Err)
	assert.FileExists(t, ok.OutputAllPath)
	assert.FileExists(t, ok.OutputPhasePath)

	assert.False(t, missing.Success, "core err 時 subject 應失敗")
	assert.ErrorContains(t, missing.Err, "缺少必要的肌肉通道")
	assert.Empty(t, missing.OutputAllPath, "core err 時不該寫 Output 1")
	assert.Empty(t, missing.OutputPhasePath)

	assert.True(t, warn.Success, "warn 只跳過 Output 2,Output 1 仍算成功")
	assert.FileExists(t, warn.OutputAllPath, "warn 時 Output 1 照寫")
	assert.Empty(t, warn.OutputPhasePath, "warn 時 Output 2 應跳過")
	assert.ErrorContains(t, warn.Err, "落在 EMG 範圍")
}

// TestAnalyze_OutputDirUnwritable_PerSubjectOutput1Failure 釘住 ADR-0052 的行為變更:Analyze
// 不再預先驗證 / 建立輸出目錄,輸出目錄不可寫時不再是一筆整批錯誤,而是每個 subject 各一筆
// 「寫入 Output 1 失敗」。S2 的分期點落在 EMG 範圍外(core 會回 warn),仍應回 Output 1
// 寫檔錯誤 —— Output 1 寫檔錯誤優先於 warn。
//
// 讓輸出目錄不可寫的招式:輸出目錄指向一般檔案,寫檔時 MkdirAll 必失敗。
func TestAnalyze_OutputDirUnwritable_PerSubjectOutput1Failure(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	outDir := filepath.Join(tempDir, "not_a_dir")
	require.NoError(t, os.WriteFile(outDir, []byte("x"), 0o600))

	writeEMG8(t, filepath.Join(dataDir, "s1.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})
	writeEMG8(t, filepath.Join(dataDir, "s2.csv"), 100, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	phases := [10]string{"0.01", "0.02", "0.03", "0.04", "0.05", "20", "0.07", "0.08", "30", "0.09"}
	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S1", "s1.csv", "1", phases),
		makeRow15("S2", "s2.csv", "10000", phases), // 分期點落在 EMG 範圍外 → core 回 warn
	})

	result, err := NewAnalyzer().Analyze(context.Background(), &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})
	require.NoError(t, err, "輸出目錄不可寫不再是整批錯誤")
	require.Len(t, result, 2)

	for _, sr := range result {
		assert.False(t, sr.Success, "%s:Output 1 寫不出來應失敗", sr.Subject)
		assert.Empty(t, sr.OutputAllPath, sr.Subject)
		assert.Empty(t, sr.OutputPhasePath, sr.Subject)
		assert.ErrorContains(t, sr.Err, "寫入 Output 1 失敗", "%s:Output 1 寫檔錯誤優先於 warn", sr.Subject)
		assert.ErrorContains(t, sr.Err, "輸出目錄建立失敗", "%s:失敗原因應是輸出目錄不可寫", sr.Subject)
	}
}

// TestAnalyze_ConcurrentCallsNoRace_IsolatedFiles 強化原 race 測試：原版 8 goroutine 共寫
// 同一輸出檔，OS 層 file race 會壓過 EMGParser 內部欄位 race，讓 detector 噪音蓋過要釘住
// 的 bug。改為每 goroutine 獨立 outDir → 任何 -race 觸發都明確指向 parser internal write/write。
func TestAnalyze_ConcurrentCallsNoRace_IsolatedFiles(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S", "s.csv", "1", [10]string{
			"0.01", "0.02", "0.03", "0.04", "0.05",
			"20", "0.07", "0.08", "30", "0.09",
		}),
	})

	a := NewAnalyzer()

	const N = 8

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			perOut := filepath.Join(outDir, fmt.Sprintf("out%d", idx))
			_, err := a.Analyze(context.Background(), &Params{
				ManifestFile: manifestPath,
				DataFolder:   dataDir,
				CSVHandler:   newTestCSVHandler(perOut),
			})
			_ = err
		}(i)
	}
	wg.Wait()
}

// TestAnalyze_ContextCancelledBeforeStart 釘住 修法:ctx 已 Cancel 入場
// 必須在第一個 subject 前 break,returns ctx.Err()(包 i18n cancelled key)。
// partial results 為 empty(0 subject 處理完)。
func TestAnalyze_ContextCancelledBeforeStart(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	outDir := filepath.Join(tempDir, "output")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))

	writeEMG8(t, filepath.Join(dataDir, "s.csv"), 200, [8]float64{2, 1, 3, 1.5, 1, 2, 4, 2})

	manifestPath := filepath.Join(tempDir, "manifest.csv")
	writeManifest(t, manifestPath, [][]string{
		makeRow15("S", "s.csv", "1", [10]string{
			"0.01", "0.02", "0.03", "0.04", "0.05",
			"20", "0.07", "0.08", "30", "0.09",
		}),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 入場前已 cancel

	results, err := NewAnalyzer().Analyze(ctx, &Params{
		ManifestFile: manifestPath,
		DataFolder:   dataDir,
		CSVHandler:   newTestCSVHandler(outDir),
	})

	require.Error(t, err, "ctx 已 cancel,Analyze 必須回 error")
	assert.ErrorIs(t, err, context.Canceled, "err 必須包 context.Canceled (errors.Is 命中)")
	assert.Empty(t, results, "ctx 已 cancel,不應有 subject 處理完成")
}

// TestAnalyze_NilContext 釘住 入參防呆:ctx==nil 必須 fail-fast,
// 不能 panic(nil ctx select case 會 panic)。
func TestAnalyze_NilContext(t *testing.T) {
	tempDir := t.TempDir()
	_, err := NewAnalyzer().Analyze(nil, &Params{ //nolint:staticcheck // intentional nil ctx
		ManifestFile: filepath.Join(tempDir, "no-manifest.csv"),
		DataFolder:   tempDir,
		CSVHandler:   newTestCSVHandler(tempDir),
	})
	require.Error(t, err, "nil ctx 必須回 error")
	assert.Contains(t, err.Error(), "ctx", "錯誤訊息應指出 ctx 為 nil")
}

// TestAnalyze_NilCSVHandlerReturnsErr 驗證 codex review 抓的 P2 — 舊式 Params
// 構造若未填 CSVHandler 欄位,Analyze 必須回乾淨 error 而非 deep 入 analyzeSubject
// 內 nil-deref panic。對應 ADR-0004 Boundary 1 「Analyzer 依賴 CSVHandler 注入」。
func TestAnalyze_NilCSVHandlerReturnsErr(t *testing.T) {
	a := NewAnalyzer()
	_, err := a.Analyze(context.Background(), &Params{
		ManifestFile: "/nonexistent.csv", // 不會被讀到 — nil check 在讀 manifest 前
		DataFolder:   t.TempDir(),
		// CSVHandler 故意 omit
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "CSVHandler", "error 應點明 CSVHandler 欄位缺失")
}
