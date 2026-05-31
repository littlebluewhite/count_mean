//go:build integration_realdata

// Package integration contains integration tests for the EMG data analysis tool.
// 本檔內 test 依賴 `EMG_TEST_FIXTURES_DIR` 提供的真實私有資料夾
// (個資/論文資料,不便 commit)。為避免 `go test ./test/integration/...`
// 預設靜默跳過造成 CI 假覆蓋,改用 build tag `integration_realdata` 明確
// opt-in;CI 由 secret `EMG_TEST_FIXTURES_DIR` 控制是否真的跑這組 test。
package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/parsers"
)

// 用 env var 取代寫死 dev 機器絕對路徑,CI 與其他 dev 機器都能跑;
// 路徑統一走 `filepath.Join` 避免字串串接造成跨平台分隔符問題。
// 未設定 env var 或檔案不存在皆 t.Skip — build tag 已隔離 default test 路徑,
// 故 skip 在這裡只是為了「opt-in 但缺 fixture」這個邊緣情境留逃生口。
func TestANCParser_RealXLSXFile(t *testing.T) {
	fixturesDir := os.Getenv("EMG_TEST_FIXTURES_DIR")
	if fixturesDir == "" {
		t.Skip("EMG_TEST_FIXTURES_DIR not set; skipping real-data integration test")
	}
	testFilePath := filepath.Join(fixturesDir, "SF2", "SF2_BTS_4.anc.xlsx")

	// 檢查檔案是否存在
	if _, err := os.Stat(testFilePath); os.IsNotExist(err) {
		t.Skipf("Test file not found: %s", testFilePath)
	}

	f, err := os.Open(testFilePath)
	require.NoError(t, err, "Failed to open xlsx file")
	defer f.Close()

	parser := parsers.NewANCParser()

	// 解析檔案
	forceData, err := parser.Parse(f, testFilePath)
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
// 原本寫死 macOS 絕對路徑 `/Users/wilson08/pCloud Drive/...`,
// 已改走 `EMG_TEST_FIXTURES_DIR` 與 `filepath.Join`,跨平台與 CI 友善。
func TestANCParser_RealXLSXFile_TimeRange(t *testing.T) {
	fixturesDir := os.Getenv("EMG_TEST_FIXTURES_DIR")
	if fixturesDir == "" {
		t.Skip("EMG_TEST_FIXTURES_DIR not set; skipping real-data integration test")
	}
	testFilePath := filepath.Join(fixturesDir, "SF2", "SF2_BTS_4.anc.xlsx")

	if _, err := os.Stat(testFilePath); os.IsNotExist(err) {
		t.Skipf("Test file not found: %s", testFilePath)
	}

	f2, err := os.Open(testFilePath)
	require.NoError(t, err)
	defer f2.Close()

	parser := parsers.NewANCParser()

	forceData, err := parser.Parse(f2, testFilePath)
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
