package errors

import (
	"errors"
	"testing"
)

// Test sentinel errors for err113 compliance.
var (
	errSystem  = errors.New("system error")
	errOther   = errors.New("other error")
	errParsing = errors.New("parsing failed")
)

func TestAppError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *AppError
		want string
	}{
		{
			name: "simple error",
			err: &AppError{
				Code:    ErrCodeFileNotFound,
				Message: "檔案未找到",
			},
			want: "[FILE_NOT_FOUND] 檔案未找到",
		},
		{
			name: "error with details",
			err: &AppError{
				Code:    ErrCodeFileNotFound,
				Message: "檔案未找到",
				Details: "檔案可能已被刪除",
			},
			want: "[FILE_NOT_FOUND] 檔案未找到 - 詳細: 檔案可能已被刪除",
		},
		{
			name: "error with cause",
			err: &AppError{
				Code:    ErrCodeFileNotFound,
				Message: "檔案未找到",
				Cause:   errSystem,
			},
			want: "[FILE_NOT_FOUND] 檔案未找到 - 原因: system error",
		},
		{
			name: "error with details and cause",
			err: &AppError{
				Code:    ErrCodeFileNotFound,
				Message: "檔案未找到",
				Details: "檔案可能已被刪除",
				Cause:   errSystem,
			},
			want: "[FILE_NOT_FOUND] 檔案未找到 - 詳細: 檔案可能已被刪除 - 原因: system error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("AppError.Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAppError_Is(t *testing.T) {
	err1 := &AppError{Code: ErrCodeFileNotFound}
	err2 := &AppError{Code: ErrCodeFileNotFound}
	err3 := &AppError{Code: ErrCodeDataParsing}

	tests := []struct {
		name   string
		err    *AppError
		target error
		want   bool
	}{
		{
			name:   "same error code",
			err:    err1,
			target: err2,
			want:   true,
		},
		{
			name:   "different error code",
			err:    err1,
			target: err3,
			want:   false,
		},
		{
			name:   "not AppError",
			err:    err1,
			target: errOther,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Is(tt.target); got != tt.want {
				t.Errorf("AppError.Is() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAppError_WithContext(t *testing.T) {
	err := &AppError{
		Code:    ErrCodeFileNotFound,
		Message: "檔案未找到",
	}

	err = err.WithContext("filename", "test.csv")
	err = err.WithContext("operation", "read")

	if err.Context == nil {
		t.Error("Context should not be nil")
	}

	if err.Context["filename"] != "test.csv" {
		t.Errorf("Context filename = %v, want test.csv", err.Context["filename"])
	}

	if err.Context["operation"] != "read" {
		t.Errorf("Context operation = %v, want read", err.Context["operation"])
	}
}

func TestNewAppError(t *testing.T) {
	err := NewAppError(ErrCodeFileNotFound, "檔案未找到")

	if err.Code != ErrCodeFileNotFound {
		t.Errorf("Code = %v, want %v", err.Code, ErrCodeFileNotFound)
	}

	if err.Message != "檔案未找到" {
		t.Errorf("Message = %v, want 檔案未找到", err.Message)
	}

	if !err.Recoverable {
		t.Error("FileNotFound should be recoverable")
	}
}

func TestIsRecoverable(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		want bool
	}{
		{
			name: "file not found is recoverable",
			code: ErrCodeFileNotFound,
			want: true,
		},
		{
			name: "memory error is not recoverable",
			code: ErrCodeMemory,
			want: false,
		},
		{
			name: "validation error is recoverable",
			code: ErrCodeDataValidation,
			want: true,
		},
		{
			name: "unknown error is recoverable by default",
			code: ErrCodeUnknown,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRecoverable(tt.code); got != tt.want {
				t.Errorf("isRecoverable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	err := NewValidationError("filename", "test.txt", "無效的檔案格式")

	expectedPattern := "[DATA_VALIDATION] 欄位 'filename' 驗證失敗: 無效的檔案格式 (值: test.txt)"
	if got := err.Error(); got != expectedPattern {
		t.Errorf("ValidationError.Error() = %v, want %v", got, expectedPattern)
	}
}

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
