// Package integration contains integration tests for the EMG data analysis tool.
package integration

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/parsers"
)

// 此測試需要存在真實的測試檔案，如果檔案不存在會跳過.
func TestANCParser_RealXLSXFile(t *testing.T) {
	// 真實測試檔案路徑
	testFilePath := "/Users/wilson08/pCloud Drive/LED/研究所/0論文/1論文實驗/1實驗資料處理/" +
		"EMG&Motion&Force plate資料分析/SF2/SF2_BTS_4.anc.xlsx"

	// 檢查檔案是否存在
	if _, err := os.Stat(testFilePath); os.IsNotExist(err) {
		t.Skipf("Test file not found: %s", testFilePath)
	}

	parser := parsers.NewANCParser()

	// 解析檔案
	forceData, err := parser.ParseFile(testFilePath)
	require.NoError(t, err, "Failed to parse xlsx file")

	// 基本驗證
	assert.NotNil(t, forceData)
	assert.Greater(t, len(forceData.Time), 0, "Should have time data")
	assert.Greater(t, len(forceData.Headers), 0, "Should have headers")
	assert.Greater(t, len(forceData.Forces), 0, "Should have force data")

	// 輸出基本資訊
	t.Logf("Successfully parsed xlsx file!")
	t.Logf("Number of time points: %d", len(forceData.Time))
	t.Logf("Headers: %v", forceData.Headers)

	if len(forceData.Time) > 0 {
		t.Logf("First time: %.6f", forceData.Time[0])
		t.Logf("Last time: %.6f", forceData.Time[len(forceData.Time)-1])
		t.Logf("Time range: %.3f seconds", forceData.Time[len(forceData.Time)-1]-forceData.Time[0])
	}

	// 驗證數據完整性
	err = parsers.ValidateForceData(forceData)
	assert.NoError(t, err, "Force data validation should pass")

	// 驗證時間序列遞增
	for i := 1; i < len(forceData.Time); i++ {
		assert.Greater(t, forceData.Time[i], forceData.Time[i-1],
			"Time should be increasing at index %d", i)
	}

	// 驗證所有通道數據長度一致
	expectedLen := len(forceData.Time)
	for _, header := range forceData.Headers {
		assert.Len(t, forceData.Forces[header], expectedLen,
			"Channel %s should have same length as time", header)
	}
}

// TestANCParser_RealXLSXFile_TimeRange 測試真實 xlsx 檔案的時間範圍查詢.
func TestANCParser_RealXLSXFile_TimeRange(t *testing.T) {
	testFilePath := "/Users/wilson08/pCloud Drive/LED/研究所/0論文/1論文實驗/1實驗資料處理/" +
		"EMG&Motion&Force plate資料分析/SF2/SF2_BTS_4.anc.xlsx"

	if _, err := os.Stat(testFilePath); os.IsNotExist(err) {
		t.Skipf("Test file not found: %s", testFilePath)
	}

	parser := parsers.NewANCParser()

	forceData, err := parser.ParseFile(testFilePath)
	require.NoError(t, err)

	// 測試時間範圍查詢（使用前 1 秒的數據）
	if len(forceData.Time) > 0 {
		startTime := forceData.Time[0]
		endTime := startTime + 1.0 // 取前 1 秒

		if endTime <= forceData.Time[len(forceData.Time)-1] {
			rangeData, err := parsers.GetANCDataInTimeRange(forceData, startTime, endTime)
			require.NoError(t, err)

			assert.Greater(t, len(rangeData.Time), 0)
			assert.GreaterOrEqual(t, rangeData.Time[0], startTime)
			assert.LessOrEqual(t, rangeData.Time[len(rangeData.Time)-1], endTime)

			t.Logf("Time range query: %.3f to %.3f", startTime, endTime)
			t.Logf("Returned %d data points", len(rangeData.Time))
		}
	}
}
