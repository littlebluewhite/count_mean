//go:build integration_realdata

// 本檔依賴私有 fixtures (EMG_TEST_FIXTURES_DIR),
// 透過 build tag `integration_realdata` 明確 opt-in,避免預設 test 假覆蓋。
package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/parsers"
)

func TestMotionParser_RealNSF2Data(t *testing.T) {
	// fixtures 路徑改走 env var + filepath.Join,
	// CI 與其他 dev 機器可選擇性提供;build tag 已過濾預設 test 路徑。
	fixturesDir := os.Getenv("EMG_TEST_FIXTURES_DIR")
	if fixturesDir == "" {
		t.Skip("EMG_TEST_FIXTURES_DIR not set; skipping real-data integration test")
	}
	filePath := filepath.Join(fixturesDir, "TEAT_NSF2_20250716", "NSF2_BTS_4_ok_20250419.data.csv")

	// 檢查檔案是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Skip("Skipping test: real NSF2 Motion file not found at", filePath)
	}

	f, err := os.Open(filePath)
	require.NoError(t, err, "Failed to open real Motion file")
	defer f.Close()

	parser := parsers.NewMotionParser()
	data, err := parser.Parse(f, filePath)
	require.NoError(t, err, "Failed to parse real Motion file")

	// 基本驗證
	assert.NotNil(t, data)
	assert.Greater(t, len(data.Indices), 0, "Should have data points")
	assert.Greater(t, len(data.Headers), 0, "Should have headers")

	// 驗證欄位名稱是唯一的（沒有重複的 "Series"）
	headerSet := make(map[string]bool)
	for _, header := range data.Headers {
		assert.False(t, headerSet[header], "Duplicate header found: %s", header)
		headerSet[header] = true
	}

	// 驗證應該包含 Trunk Angle, Hip Angle, Knee Angle
	t.Logf("Parsed headers: %v", data.Headers)
	t.Logf("Number of data points: %d", len(data.Indices))

	// 確認數據完整性
	for _, header := range data.Headers {
		assert.Len(t, data.Data[header], len(data.Indices),
			"Column %s should have same length as indices", header)
	}

	// 驗證 Motion 數據
	err = parsers.ValidateMotionData(data)
	assert.NoError(t, err, "Motion data validation should pass")
}
