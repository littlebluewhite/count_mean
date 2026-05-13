package calculator_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/calculator"
	"count_mean/internal/csvutil"
	"count_mean/internal/models"
	"count_mean/internal/security/fsperm"
)

// TestExportToCSV_RoundTrip 是 cross-compare review 補的 Wave 2 P1 fix
// (Flush/Close error propagation) 的 happy-path regression test。
// 過去 ExportToCSV 的 defer file.Close() 與 writer.Flush() 都靜默丟棄錯誤，
// 修法後 Flush 用 writer.Error() 檢查、Close 用 named-return 把 closeErr 帶回。
// 這個測試確保整段 export 順利完成、檔案內容完整、且含 BOM。
func TestExportToCSV_RoundTrip(t *testing.T) {
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "stats.csv")

	calc := calculator.NewEMGStatisticsCalculator(6)
	stats := &models.EMGStatistics{
		Subject:      "subject_01",
		StartPhase:   models.PhaseP0,
		StartTime:    1.0,
		EndPhase:     models.PhaseL,
		EndTime:      2.5,
		ChannelNames: []string{"RA", "ES"},
		ChannelMeans: map[string]float64{"RA": 0.123456, "ES": 0.234567},
		ChannelMaxes: map[string]float64{"RA": 1.5, "ES": 2.0},
	}

	err := calc.ExportToCSV(stats, outputPath)
	require.NoError(t, err)

	content, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	// BOM 必須存在於檔頭（writeCSVWithBOM 寫的）
	require.True(t, len(content) >= len(csvutil.BOMBytes()),
		"file shorter than BOM length, Flush/Close 可能靜默吞錯")
	assert.Equal(t, csvutil.BOMBytes(), content[:len(csvutil.BOMBytes())],
		"BOM 必須完整保留")

	body := string(content[len(csvutil.BOMBytes()):])
	// 至少 8 列（標題 + 7 row 結構）
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	assert.GreaterOrEqual(t, len(lines), 8,
		"all rows 必須寫完才表示 Flush 真的 commit；不足 8 列表示 buffer 未完整 flush")

	// 至少看到一個通道名 + 一個明顯的 phase 標籤
	assert.Contains(t, body, "RA")
	assert.Contains(t, body, "P0")
}

// TestExportToCSV_InvalidDir 確認當 output dir 不可寫時錯誤會 propagate
// （而非靜默 close 然後回 nil）。
func TestExportToCSV_InvalidDir(t *testing.T) {
	// 一定不存在且不可建立的 path（中間段是檔案而非目錄）
	tempDir := t.TempDir()
	blocker := filepath.Join(tempDir, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), fsperm.FilePerm))
	bad := filepath.Join(blocker, "subdir", "stats.csv") // blocker 是檔案，無法當父目錄

	calc := calculator.NewEMGStatisticsCalculator(6)
	stats := &models.EMGStatistics{
		Subject:      "s",
		StartPhase:   models.PhaseP0,
		EndPhase:     models.PhaseL,
		ChannelNames: []string{"RA"},
		ChannelMeans: map[string]float64{"RA": 1.0},
		ChannelMaxes: map[string]float64{"RA": 2.0},
	}

	err := calc.ExportToCSV(stats, bad)
	require.Error(t, err, "write to path with file-as-parent should error, not silently succeed")
}
