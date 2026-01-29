package integration

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/parsers"
)

func TestMotionParser_RealNSF2Data(t *testing.T) {
	// 使用真實的 NSF2 Motion 檔案進行整合測試
	filePath := "/Users/wilson08/IdeaProjects/count_mean/input/TEAT_NSF2_20250716/NSF2_BTS_4_ok_20250419.data.csv"

	// 檢查檔案是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Skip("Skipping test: real NSF2 Motion file not found at", filePath)
	}

	parser := parsers.NewMotionParser()
	data, err := parser.ParseFile(filePath)
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
