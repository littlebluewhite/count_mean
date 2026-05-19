package io

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/logging"
	"count_mean/internal/security/fsperm"
)

// TestLargeFileHandler_ScanFileStructure_CountsSkippedRows 鎖定 
// scanFileStructure 對 malformed CSV row（field count 與 header 不符）使用 continue
// 跳過，但原本只回 (lineCount, columnCount, error) — operator 無法察覺檔案
// 含 silently skipped data，下游分析結果可能因「missing rows」而偏誤。
//
// 修法：新增 skippedRows 回傳值，讓 caller（GetFileInfo）能 log warning。
// 行為契約：3 valid data rows + 2 malformed rows → lineCount=4 (header + 3 valid),
// skippedRows=2。
func TestLargeFileHandler_ScanFileStructure_CountsSkippedRows(t *testing.T) {
	cfg := config.DefaultConfig()
	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "with_malformed.csv")
	// Header: 3 columns. Valid rows: 3. Malformed: 2 (rows with wrong field count).
	content := "Time,Ch1,Ch2\n" +
		"0.1,1.0,2.0\n" +
		"0.2,1.5,2.5\n" +
		"0.3,bad_only_two\n" + // malformed: 2 fields
		"0.4,2.0,3.0\n" +
		"0.5,too,many,fields,here\n" // malformed: 5 fields
	require.NoError(t, os.WriteFile(testFile, []byte(content), fsperm.FilePerm))

	lineCount, skippedRows, columnCount, err := handler.scanFileStructure(testFile)
	require.NoError(t, err)
	require.Equal(t, 3, columnCount, "columnCount = header field count")
	require.Equal(t, int64(4), lineCount, "lineCount = header + 3 valid data rows")
	require.Equal(t, int64(2), skippedRows, "skippedRows = 2 malformed rows")
}

// TestLargeFileHandler_ScanFileStructure_NoSkippedForValidFile 確保乾淨檔案
// 不會誤報 skippedRows > 0。
func TestLargeFileHandler_ScanFileStructure_NoSkippedForValidFile(t *testing.T) {
	cfg := config.DefaultConfig()
	testDir := t.TempDir()
	cfg.InputDir = testDir
	handler := NewLargeFileHandler(cfg)

	testFile := filepath.Join(testDir, "clean.csv")
	content := "Time,Ch1,Ch2\n0.1,1.0,2.0\n0.2,1.5,2.5\n0.3,2.0,3.0\n"
	require.NoError(t, os.WriteFile(testFile, []byte(content), fsperm.FilePerm))

	lineCount, skippedRows, columnCount, err := handler.scanFileStructure(testFile)
	require.NoError(t, err)
	require.Equal(t, 3, columnCount)
	require.Equal(t, int64(4), lineCount)
	require.Equal(t, int64(0), skippedRows)
}

// TestLargeFileHandler_ScanFileStructure_SuppressesWarnLogAfterLimit釘住:
// scanFileStructure 對 malformed row 改 first-N + suppress + summary,避免惡意
// 輸入觸發 log 爆炸。
//
// 行為契約:
//   - 前 scanWarnSampleLimit(10)筆 malformed row 仍個別 Warn(含 error / line);
//   - 第 11+ 筆不再個別 Warn(suppress);
//   - 結束時 emit 一筆 summary Warn 含 skipped_total / suppressed_after_n。
//
// 構造:用未配對引號(LazyQuotes=false 下 csv reader 會丟 ErrQuote)生成大量
// malformed row。15 筆 malformed → 應該看到 10 筆 individual + 1 筆 summary。
func TestLargeFileHandler_ScanFileStructure_SuppressesWarnLogAfterLimit(t *testing.T) {
	cfg := config.DefaultConfig()
	testDir := t.TempDir()
	cfg.InputDir = testDir

	handler := NewLargeFileHandler(cfg)
	// 注入 buffer-backed logger 捕捉 warn 行;JSON 格式才好穩定 assert key-value。
	var logBuf bytes.Buffer
	handler.logger = logging.NewLogger(logging.LevelWarn, &logBuf, true)

	// header(3 欄)+ 15 行 malformed:每行欄位數 != header 欄位數,
	// FieldsPerRecord=0(自動鎖定為 header 欄數)下 reader.Read 丟 ErrFieldCount。
	// 改用此 mechanism 而非 quote-mismatch:quote-mismatch 在 csv.Reader 內會
	// 連續 consume 多行直到 EOF/平衡,1 個 error 而非 15 個。
	var sb strings.Builder
	sb.WriteString("Time,Ch1,Ch2\n")
	for i := 0; i < 15; i++ {
		// 一個 malformed row 只有 2 欄而 header 有 3 欄
		sb.WriteString("0.1,bad\n")
	}
	testFile := filepath.Join(testDir, "malicious.csv")
	require.NoError(t, os.WriteFile(testFile, []byte(sb.String()), fsperm.FilePerm))

	_, skippedRows, _, err := handler.scanFileStructure(testFile)
	require.NoError(t, err)
	require.Equal(t, int64(15), skippedRows, "15 筆 malformed row 應該全部計入 skippedRows")

	logOut := logBuf.String()

	// 前 10 筆 individual warn:count `"掃描文件時遇到錯誤,繼續處理"` 出現次數。
	const individualMsg = "掃描文件時遇到錯誤"
	individualCount := strings.Count(logOut, individualMsg)
	require.Equal(t, scanWarnSampleLimit, individualCount,
		"individual warn 應該 emit 恰好 %d 筆(其餘 suppress),實際 %d 筆\n%s",
		scanWarnSampleLimit, individualCount, logOut)

	// summary warn 必須出現一次,且帶 skipped_total / suppressed_after_n 數字。
	require.Contains(t, logOut, "malformed 行 sample 已達上限",
		"必須 emit summary warn")
	require.Contains(t, logOut, `"skipped_total":"15"`,
		"summary warn 必須含 skipped_total(JSON sanitize 把 int 轉字串)")
	require.Contains(t, logOut, `"suppressed_after_n":"5"`,
		"summary warn 必須含 suppressed_after_n (15 - 10 = 5)")
}

// TestLargeFileHandler_ScanFileStructure_SuppressionThreshold_BoundaryNoSummary
// (邊界):malformed row 數**正好** == scanWarnSampleLimit 時不該 emit summary
// (沒有 suppress 任何東西,不需要 summary 提醒)。
func TestLargeFileHandler_ScanFileStructure_SuppressionThreshold_BoundaryNoSummary(t *testing.T) {
	cfg := config.DefaultConfig()
	testDir := t.TempDir()
	cfg.InputDir = testDir

	handler := NewLargeFileHandler(cfg)
	var logBuf bytes.Buffer
	handler.logger = logging.NewLogger(logging.LevelWarn, &logBuf, true)

	var sb strings.Builder
	sb.WriteString("Time,Ch1,Ch2\n")
	for i := 0; i < scanWarnSampleLimit; i++ {
		sb.WriteString("0.1,bad\n")
	}
	testFile := filepath.Join(testDir, "boundary.csv")
	require.NoError(t, os.WriteFile(testFile, []byte(sb.String()), fsperm.FilePerm))

	_, skippedRows, _, err := handler.scanFileStructure(testFile)
	require.NoError(t, err)
	require.Equal(t, int64(scanWarnSampleLimit), skippedRows)

	logOut := logBuf.String()
	require.NotContains(t, logOut, "malformed 行 sample 已達上限",
		"剛好 == limit 時不該 emit summary(沒 suppress 任何東西)")
}
