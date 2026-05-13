package csv_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"count_mean/internal/config"
	"count_mean/internal/io"
)

// TestCSVHandler_WriteCSV_SanitizesBodyRowChokepoint 鎖定 Wave 7 review
// security/QA P0 修補：csv_handler.WriteCSV 內部 SanitizeAllRows chokepoint
// 必須 escape body-row 第一格的 user-controllable label（如 result.PhaseName
// 來自 config.json phaseLabels），堵住 converter 沒覆蓋的 body-row formula
// injection 路徑（=cmd|/c calc!A1 之類）。
//
// 設計：直接餵 WriteCSV [][]string 含有 header 安全 + body row 開頭 = / @ / +
// non-numeric 三種 vector，讀回檔案驗證每個 vector 都被 '<prefix> escape。
func TestCSVHandler_WriteCSV_SanitizesBodyRowChokepoint(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.InputDir = tempDir
	cfg.OutputDir = tempDir
	cfg.OperateDir = tempDir
	cfg.BOMEnabled = false // 簡化 substring 比對
	handler := io.NewCSVHandler(cfg)

	data := [][]string{
		{"Time", "Channel-A", "Channel-B"},
		{"=cmd|/c calc!A1 最大值", "1.0", "2.0"},          // PhaseName-as-label vector
		{"@SUM(1+1) 平均值", "3.0", "4.0"},                // @ prefix
		{"+HYPERLINK(\"http://e\",\"x\") 統計", "5", "6"}, // +non-numeric
	}

	output := filepath.Join(tempDir, "body_sanitize.csv")
	require.NoError(t, handler.WriteCSV(output, data))

	raw, err := os.ReadFile(output) //nolint:gosec // test path
	require.NoError(t, err)
	content := string(raw)

	// header 安全 — 不該被加 '
	require.Contains(t, content, "Time,Channel-A,Channel-B")

	// body rows 三種 vector 都要 '<prefix>
	require.Contains(t, content, "'=cmd|/c calc!A1", "body row 開頭 = 必須被 escape")
	require.Contains(t, content, "'@SUM(1+1)", "body row 開頭 @ 必須被 escape")
	require.Contains(t, content, "'+HYPERLINK", "body row 開頭 +非數字 必須被 escape")

	// 防止退步：不該出現原始未 escape 的 =cmd（單獨字串，無 '）
	require.False(t,
		strings.Contains(content, "\n=cmd") || strings.HasPrefix(content, "=cmd"),
		"應沒有未 escape 的 =cmd 出現在 row 開頭")
}
