package parsers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperrors "count_mean/internal/errors"
	"count_mean/internal/logging"
)

func TestNewDataParser(t *testing.T) {
	p := NewDataParser(1)
	assert.NotNil(t, p)
	assert.Equal(t, 1, p.GetScalingFactor())
}

func TestNewDataParserWithLogger(t *testing.T) {
	t.Run("with valid logger", func(t *testing.T) {
		logger := logging.GetLogger("test")
		p := NewDataParserWithLogger(1, logger)
		assert.NotNil(t, p)
		assert.Equal(t, 1, p.GetScalingFactor())
	})

	t.Run("with nil logger falls back to default", func(t *testing.T) {
		p := NewDataParserWithLogger(1, nil)
		assert.NotNil(t, p)
		assert.Equal(t, 1, p.GetScalingFactor())
	})
}

func TestParseRawData_NilInput(t *testing.T) {
	p := NewDataParser(1)
	result, err := p.ParseRawData(nil)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestParseRawData_EmptyRecords(t *testing.T) {
	p := NewDataParser(1)
	result, err := p.ParseRawData([][]string{})
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "標題行")
}

func TestParseRawData_HeaderOnly(t *testing.T) {
	p := NewDataParser(1)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
	}
	result, err := p.ParseRawData(records)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "標題行")
}

func TestParseRawData_ValidData(t *testing.T) {
	// scalingFactor=0 means multiply by 10^0 = 1, so values stay unchanged
	p := NewDataParser(0)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"0.001", "100.5", "200.3"},
		{"0.002", "101.2", "199.7"},
		{"0.003", "99.8", "201.1"},
	}

	result, err := p.ParseRawData(records)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, []string{"Time", "Ch1", "Ch2"}, result.Headers)
	assert.Len(t, result.Data, 3)

	assert.Equal(t, 0.001, result.Data[0].Time)
	assert.Equal(t, 0.002, result.Data[1].Time)
	assert.Equal(t, 0.003, result.Data[2].Time)

	assert.Equal(t, []float64{100.5, 200.3}, result.Data[0].Channels)
	assert.Equal(t, []float64{101.2, 199.7}, result.Data[1].Channels)
	assert.Equal(t, []float64{99.8, 201.1}, result.Data[2].Channels)
}

func TestParseRawData_InvalidTimeValues(t *testing.T) {
	p := NewDataParser(0)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"invalid", "100.5", "200.3"},
		{"0.002", "101.2", "199.7"},
		{"not_a_number", "99.8", "201.1"},
	}

	result, err := p.ParseRawData(records)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Only the valid row should be parsed
	assert.Len(t, result.Data, 1)
	assert.Equal(t, 0.002, result.Data[0].Time)
}

func TestParseRawData_InvalidChannelValues(t *testing.T) {
	p := NewDataParser(0)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"0.001", "100.5", "invalid"},
	}

	result, err := p.ParseRawData(records)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "解析數據失敗")
}

func TestParseRawData_EmptyTimeValue(t *testing.T) {
	p := NewDataParser(0)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"", "100.5", "200.3"},
		{"0.002", "101.2", "199.7"},
	}

	result, err := p.ParseRawData(records)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Row with empty time should be skipped
	assert.Len(t, result.Data, 1)
	assert.Equal(t, 0.002, result.Data[0].Time)
}

func TestParseRawData_AllRowsSkipped(t *testing.T) {
	p := NewDataParser(0)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"invalid", "100.5", "200.3"},
		{"", "101.2", "199.7"},
	}

	result, err := p.ParseRawData(records)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "數據集為空")
}

// TestParseRawData_EmptyRow_NoPanic 防止 panic regression：
// parseDataRow 原本在 `len(row) < 2` 分支內仍讀 row[0] 做 debug log，
// 對 []string{} 會 panic（index out of range）。修正後先排除空 row。
func TestParseRawData_EmptyRow_NoPanic(t *testing.T) {
	p := NewDataParser(0)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{}, // 空 row
		{"0.002", "101.2", "199.7"},
	}

	// 不該 panic
	result, err := p.ParseRawData(records)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Data, 1)
	assert.Equal(t, 0.002, result.Data[0].Time)
}

func TestParseRawData_ShortRows(t *testing.T) {
	p := NewDataParser(0)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"0.001"},
		{"0.002", "101.2", "199.7"},
	}

	result, err := p.ParseRawData(records)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Row with less than 2 columns should be skipped
	assert.Len(t, result.Data, 1)
	assert.Equal(t, 0.002, result.Data[0].Time)
}

func TestParseRawDataWithOptions_NilInput(t *testing.T) {
	p := NewDataParser(1)
	opts := ParseOptions{
		DetectTimePrecision: false,
		LogVerbose:          false,
	}
	result, err := p.ParseRawDataWithOptions(nil, opts)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestParseRawDataWithOptions_DetectTimePrecision(t *testing.T) {
	p := NewDataParser(1)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"0.00100", "100.5", "200.3"},
		{"0.00200", "101.2", "199.7"},
	}
	opts := ParseOptions{
		DetectTimePrecision: true,
		LogVerbose:          false,
	}

	result, err := p.ParseRawDataWithOptions(records, opts)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should detect 5 decimal places
	assert.Equal(t, 5, result.OriginalTimePrecision)
}

func TestParseRawDataWithOptions_LogVerboseFalse(t *testing.T) {
	p := NewDataParser(1)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"0.001", "100.5", "200.3"},
	}
	opts := ParseOptions{
		DetectTimePrecision: false,
		LogVerbose:          false,
	}

	result, err := p.ParseRawDataWithOptions(records, opts)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Data, 1)
}

func TestParseRawData_WithScalingFactor(t *testing.T) {
	// scalingFactor=-3 means multiply by 10^(-3) = 0.001
	p := NewDataParser(-3)
	records := [][]string{
		{"Time", "Ch1"},
		{"1", "500"},
	}

	result, err := p.ParseRawData(records)
	require.NoError(t, err)
	require.NotNil(t, result)

	// With scaling factor of -3, 1 becomes 1 times 10 to the power of -3, which equals 0.001
	assert.Equal(t, 0.001, result.Data[0].Time)
	// 500 times 10 to the power of -3 equals 0.5
	assert.Equal(t, 0.5, result.Data[0].Channels[0])
}

func TestGetScalingFactor(t *testing.T) {
	tests := []struct {
		name          string
		scalingFactor int
	}{
		{"scaling factor 1", 1},
		{"scaling factor 1000", 1000},
		{"scaling factor 0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewDataParser(tt.scalingFactor)
			assert.Equal(t, tt.scalingFactor, p.GetScalingFactor())
		})
	}
}

// TestErrInsufficientData_BridgesToAppErrors 防止 parser 的 ErrInsufficientData
// 與 internal/errors 的同名 sentinel 失去語意連結
// 已做 alias，本 commit 補 parser 套件的 wrap 橋接。
//
// 用 errors.Is 跨套件比對：上游 caller 只認得 apperrors.ErrInsufficientData，
// 也能 match 由 internal/parsers 拋出的同類錯誤。
func TestErrInsufficientData_BridgesToAppErrors(t *testing.T) {
	t.Run("sentinel itself bridges", func(t *testing.T) {
		require.True(t, errors.Is(ErrInsufficientData, apperrors.ErrInsufficientData),
			"parsers.ErrInsufficientData 應該 wrap apperrors.ErrInsufficientData")
	})

	t.Run("error returned by parser also bridges", func(t *testing.T) {
		p := NewDataParser(1)
		_, err := p.ParseRawData([][]string{{"header"}}) // 只 1 row → trigger insufficient data
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrInsufficientData),
			"從 ParseRawData 取得的 error 應該能用 apperrors sentinel 比對")
		assert.True(t, errors.Is(err, ErrInsufficientData),
			"從 ParseRawData 取得的 error 應該能用 parser sentinel 比對")
		assert.Contains(t, err.Error(), "數據至少需要包含標題行和一行數據",
			"原本中文訊息應保留（maxmean_test 也依賴此字串）")
	})

	// Regression: errors.Is 非對稱性 — 若 parsers.ErrInsufficientData 變成 wrap 而非 alias，
	// 上游用 parsers.ErrInsufficientData 匹配其他套件包裝的同源錯誤會失效（codex Wave 4 PR-F2 P2）。
	t.Run("cross-package CalculatorError bridges via parsers sentinel", func(t *testing.T) {
		calcErr := apperrors.NewCalculatorError(apperrors.ErrInsufficientData, "phase 樣式錯誤")
		assert.True(t, errors.Is(calcErr, ErrInsufficientData),
			"parsers.ErrInsufficientData 必須匹配 CalculatorError(apperrors.ErrInsufficientData) — 避免 errors.Is 非對稱性破壞跨套件契約")
	})
}
