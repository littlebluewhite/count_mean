package errors_test

import (
	"errors"
	"testing"

	"count_mean/internal/errors"
)

func TestAppError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *errors.AppError
		want string
	}{
		{
			name: "simple error",
			err: &errors.AppError{
				Code:    errors.ErrCodeFileNotFound,
				Message: "檔案未找到",
			},
			want: "[FILE_NOT_FOUND] 檔案未找到",
		},
		{
			name: "error with details",
			err: &errors.AppError{
				Code:    errors.ErrCodeFileNotFound,
				Message: "檔案未找到",
				Details: "檔案可能已被刪除",
			},
			want: "[FILE_NOT_FOUND] 檔案未找到 - 詳細: 檔案可能已被刪除",
		},
		{
			name: "error with cause",
			err: &errors.AppError{
				Code:    errors.ErrCodeFileNotFound,
				Message: "檔案未找到",
				Cause:   errors.New("system error"),
			},
			want: "[FILE_NOT_FOUND] 檔案未找到 - 原因: system error",
		},
		{
			name: "error with details and cause",
			err: &errors.AppError{
				Code:    errors.ErrCodeFileNotFound,
				Message: "檔案未找到",
				Details: "檔案可能已被刪除",
				Cause:   errors.New("system error"),
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
	err1 := &errors.AppError{Code: errors.ErrCodeFileNotFound}
	err2 := &errors.AppError{Code: errors.ErrCodeFileNotFound}
	err3 := &errors.AppError{Code: errors.errors.errors.ErrCodeDataParsing}
	otherErr := errors.New("other error")

	tests := []struct {
		name   string
		err    *errors.AppError
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
			target: otherErr,
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
	err := &errors.AppError{
		Code:    errors.ErrCodeFileNotFound,
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
	err := errors.NewAppError(errors.ErrCodeFileNotFound, "檔案未找到")

	if err.Code != errors.ErrCodeFileNotFound {
		t.Errorf("Code = %v, want %v", err.Code, errors.ErrCodeFileNotFound)
	}

	if err.Message != "檔案未找到" {
		t.Errorf("Message = %v, want 檔案未找到", err.Message)
	}

	if !err.Recoverable {
		t.Error("FileNotFound should be recoverable")
	}
}

func TestNewAppErrorWithCause(t *testing.T) {
	cause := errors.New("underlying error")
	err := errors.NewAppErrorWithCause(errors.ErrCodeFileNotFound, "檔案未找到", cause)

	if err.Cause != cause {
		t.Errorf("Cause = %v, want %v", err.Cause, cause)
	}

	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
	}
}

func TestIsRecoverable(t *testing.T) {
	tests := []struct {
		name string
		code errors.errors.errors.ErrorCode
		want bool
	}{
		{
			name: "file not found is recoverable",
			code: errors.ErrCodeFileNotFound,
			want: true,
		},
		{
			name: "memory error is not recoverable",
			code: errors.errors.errors.ErrCodeMemory,
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
			if got := isRecoverable(tt.code); got != tt.want {
				t.Errorf("isRecoverable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestProcessingError_Error(t *testing.T) {
	cause := errors.New("parse error")
	err := NewProcessingError(
		errors.errors.errors.ErrCodeDataParsing,
		"解析失敗",
		"test.csv",
		"read_csv",
		"data_validation",
		cause,
	)

	expectedPattern := "[DATA_PARSING] - 檔案: test.csv - 操作: read_csv - 步驟: data_validation - 解析失敗 - 原因: parse error"
	if got := err.Error(); got != expectedPattern {
		t.Errorf("ProcessingError.Error() = %v, want %v", got, expectedPattern)
	}
}

func TestValidationError_Error(t *testing.T) {
	err := NewValidationError("filename", "test.txt", "無效的檔案格式")

	expectedPattern := "[DATA_VALIDATION] 欄位 'filename' 驗證失敗: 無效的檔案格式 (值: test.txt)"
	if got := err.Error(); got != expectedPattern {
		t.Errorf("ValidationError.Error() = %v, want %v", got, expectedPattern)
	}
}