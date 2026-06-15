package util

import (
	"testing"
)

func TestGetDecimalPrecision(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "two decimal places",
			input:    "1.23",
			expected: 2,
		},
		{
			name:     "three decimal places",
			input:    "0.123",
			expected: 3,
		},
		{
			name:     "no decimal point",
			input:    "123",
			expected: 0,
		},
		{
			name:     "one decimal place",
			input:    "1.5",
			expected: 1,
		},
		{
			name:     "six decimal places",
			input:    "0.123456",
			expected: 6,
		},
		{
			name:     "with whitespace",
			input:    "  1.23  ",
			expected: 2,
		},
		{
			name:     "zero value",
			input:    "0.00",
			expected: 2,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			// 含指數的數字:只數 mantissa 的小數位,指數段不計入。
			// 否則 "1.5e-3" 會被算成 4(把 "e-3" 也當小數)。
			name:     "exponent lowercase e",
			input:    "1.5e-3",
			expected: 1,
		},
		{
			name:     "exponent uppercase E",
			input:    "2.250E10",
			expected: 3,
		},
		{
			name:     "exponent no decimal in mantissa",
			input:    "5e-7",
			expected: 0,
		},
		{
			name:     "exponent positive sign",
			input:    "1.23e+4",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetDecimalPrecision(tt.input)
			if result != tt.expected {
				t.Errorf("GetDecimalPrecision(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDetectTimePrecision(t *testing.T) {
	tests := []struct {
		name     string
		records  [][]string
		expected int
	}{
		{
			name:     "empty records",
			records:  [][]string{},
			expected: 2,
		},
		{
			name:     "only header",
			records:  [][]string{{"Time", "Value"}},
			expected: 2,
		},
		{
			name: "two decimal precision",
			records: [][]string{
				{"Time", "Value"},
				{"0.01", "100"},
				{"0.02", "200"},
			},
			expected: 2,
		},
		{
			name: "three decimal precision",
			records: [][]string{
				{"Time", "Value"},
				{"0.001", "100"},
				{"0.002", "200"},
			},
			expected: 3,
		},
		{
			name: "mixed precision - takes max",
			records: [][]string{
				{"Time", "Value"},
				{"0.1", "100"},
				{"0.02", "200"},
				{"0.003", "300"},
			},
			expected: 3,
		},
		{
			name: "no decimal places - defaults to 2",
			records: [][]string{
				{"Time", "Value"},
				{"1", "100"},
				{"2", "200"},
			},
			expected: 2,
		},
		{
			name: "more than 10 rows - only checks first 10",
			records: [][]string{
				{"Time", "Value"},
				{"0.1", "100"},
				{"0.1", "100"},
				{"0.1", "100"},
				{"0.1", "100"},
				{"0.1", "100"},
				{"0.1", "100"},
				{"0.1", "100"},
				{"0.1", "100"},
				{"0.1", "100"},
				{"0.1", "100"},
				{"0.123456", "100"}, // Row 12 (index 11), should not be checked
			},
			expected: 1,
		},
		{
			name: "empty rows are skipped",
			records: [][]string{
				{"Time", "Value"},
				{},
				{"0.123", "100"},
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectTimePrecision(tt.records)
			if result != tt.expected {
				t.Errorf("DetectTimePrecision() = %d, want %d", result, tt.expected)
			}
		})
	}
}
