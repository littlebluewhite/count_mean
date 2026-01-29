package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/logging"
	"count_mean/internal/parser"
)

func TestNewDataParser(t *testing.T) {
	p := parser.NewDataParser(1)
	assert.NotNil(t, p)
	assert.Equal(t, 1, p.GetScalingFactor())
}

func TestNewDataParserWithLogger(t *testing.T) {
	t.Run("with valid logger", func(t *testing.T) {
		logger := logging.GetLogger("test")
		p := parser.NewDataParserWithLogger(1, logger)
		assert.NotNil(t, p)
		assert.Equal(t, 1, p.GetScalingFactor())
	})

	t.Run("with nil logger falls back to default", func(t *testing.T) {
		p := parser.NewDataParserWithLogger(1, nil)
		assert.NotNil(t, p)
		assert.Equal(t, 1, p.GetScalingFactor())
	})
}

func TestParseRawData_NilInput(t *testing.T) {
	p := parser.NewDataParser(1)
	result, err := p.ParseRawData(nil)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestParseRawData_EmptyRecords(t *testing.T) {
	p := parser.NewDataParser(1)
	result, err := p.ParseRawData([][]string{})
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "標題行")
}

func TestParseRawData_HeaderOnly(t *testing.T) {
	p := parser.NewDataParser(1)
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
	p := parser.NewDataParser(0)
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
	p := parser.NewDataParser(0)
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
	p := parser.NewDataParser(0)
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
	p := parser.NewDataParser(0)
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
	p := parser.NewDataParser(0)
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

func TestParseRawData_ShortRows(t *testing.T) {
	p := parser.NewDataParser(0)
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
	p := parser.NewDataParser(1)
	opts := parser.ParseOptions{
		DetectTimePrecision: false,
		LogVerbose:          false,
	}
	result, err := p.ParseRawDataWithOptions(nil, opts)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestParseRawDataWithOptions_DetectTimePrecision(t *testing.T) {
	p := parser.NewDataParser(1)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"0.00100", "100.5", "200.3"},
		{"0.00200", "101.2", "199.7"},
	}
	opts := parser.ParseOptions{
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
	p := parser.NewDataParser(1)
	records := [][]string{
		{"Time", "Ch1", "Ch2"},
		{"0.001", "100.5", "200.3"},
	}
	opts := parser.ParseOptions{
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
	p := parser.NewDataParser(-3)
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
			p := parser.NewDataParser(tt.scalingFactor)
			assert.Equal(t, tt.scalingFactor, p.GetScalingFactor())
		})
	}
}
