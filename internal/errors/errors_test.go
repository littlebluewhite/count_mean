package errors

import (
	"errors"
	"testing"
)

// Test sentinel errors for err113 compliance.
var errParsing = errors.New("parsing failed")

// Tests for calculator sentinel errors.
func TestCalculatorError_Is(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		target   error
		expected bool
	}{
		{
			name:     "empty dataset error matches sentinel",
			err:      NewCalculatorError(ErrEmptyDataset, "數據集為空"),
			target:   ErrEmptyDataset,
			expected: true,
		},
		{
			name:     "window too large error matches sentinel",
			err:      NewCalculatorError(ErrWindowTooLarge, "窗口過大"),
			target:   ErrWindowTooLarge,
			expected: true,
		},
		{
			name:     "invalid window size error matches sentinel",
			err:      NewCalculatorError(ErrInvalidWindowSize, "窗口大小無效"),
			target:   ErrInvalidWindowSize,
			expected: true,
		},
		{
			name:     "channel mismatch error matches sentinel",
			err:      NewCalculatorError(ErrChannelMismatch, "通道不匹配"),
			target:   ErrChannelMismatch,
			expected: true,
		},
		{
			name:     "zero reference error matches sentinel",
			err:      NewCalculatorError(ErrZeroReference, "參考值為零"),
			target:   ErrZeroReference,
			expected: true,
		},
		{
			name:     "phase mismatch error matches sentinel",
			err:      NewCalculatorError(ErrPhaseMismatch, "階段不匹配"),
			target:   ErrPhaseMismatch,
			expected: true,
		},
		{
			name:     "empty dataset error does not match window too large",
			err:      NewCalculatorError(ErrEmptyDataset, "數據集為空"),
			target:   ErrWindowTooLarge,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.expected {
				t.Errorf("errors.Is() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCalculatorError_Message(t *testing.T) {
	err := NewCalculatorError(ErrEmptyDataset, "數據集為空")

	if err.Error() != "數據集為空" {
		t.Errorf("Error() = %v, want 數據集為空", err.Error())
	}
}

func TestCalculatorError_WithContext(t *testing.T) {
	ctx := map[string]any{
		"data_length": 0,
		"window_size": 100,
	}
	err := NewCalculatorErrorWithContext(ErrWindowTooLarge, "窗口過大", ctx)

	if err.Context["data_length"] != 0 {
		t.Errorf("Context[data_length] = %v, want 0", err.Context["data_length"])
	}

	if err.Context["window_size"] != 100 {
		t.Errorf("Context[window_size] = %v, want 100", err.Context["window_size"])
	}
}

func TestWrapParseError(t *testing.T) {
	wrappedErr := WrapParseError(10, 5, errParsing)

	expectedMsg := "parsing error at row 10, col 5: parsing failed"
	if wrappedErr.Error() != expectedMsg {
		t.Errorf("WrapParseError() = %v, want %v", wrappedErr.Error(), expectedMsg)
	}

	if !errors.Is(wrappedErr, errParsing) {
		t.Error("WrapParseError should wrap the original error")
	}
}
