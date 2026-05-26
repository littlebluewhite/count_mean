package errors

import (
	"fmt"
	"strings"
)

// ErrorCode represents different types of errors.
type ErrorCode string

const (
	// ErrCodeFileNotFound 表示檔案未找到錯誤。
	ErrCodeFileNotFound ErrorCode = "FILE_NOT_FOUND"
	// ErrCodeFilePermission 表示檔案權限錯誤。
	ErrCodeFilePermission ErrorCode = "FILE_PERMISSION"
	// ErrCodeFileFormat 表示檔案格式錯誤。
	ErrCodeFileFormat ErrorCode = "FILE_FORMAT"
	// ErrCodePathValidation 表示路徑驗證錯誤。
	ErrCodePathValidation ErrorCode = "PATH_VALIDATION"
	// ErrCodeFileTooLarge 表示檔案過大錯誤。
	ErrCodeFileTooLarge ErrorCode = "FILE_TOO_LARGE"

	// ErrCodeDataParsing 表示資料解析錯誤。
	ErrCodeDataParsing ErrorCode = "DATA_PARSING"
	// ErrCodeDataValidation 表示資料驗證錯誤。
	ErrCodeDataValidation ErrorCode = "DATA_VALIDATION"
	// ErrCodeCalculation 表示計算錯誤。
	ErrCodeCalculation ErrorCode = "CALCULATION"
	// ErrCodeInsufficientData 表示資料不足錯誤。
	ErrCodeInsufficientData ErrorCode = "INSUFFICIENT_DATA"

	// ErrCodeConfigValidation 表示配置驗證錯誤。
	ErrCodeConfigValidation ErrorCode = "CONFIG_VALIDATION"
	// ErrCodeConfigLoad 表示配置載入錯誤。
	ErrCodeConfigLoad ErrorCode = "CONFIG_LOAD"

	// ErrCodeGUIOperation 表示 GUI 操作錯誤。
	ErrCodeGUIOperation ErrorCode = "GUI_OPERATION"
	// ErrCodeUserInput 表示使用者輸入錯誤。
	ErrCodeUserInput ErrorCode = "USER_INPUT"

	// ErrCodeMemory 表示記憶體錯誤。
	ErrCodeMemory ErrorCode = "MEMORY"
	// ErrCodeNetwork 表示網路錯誤。
	ErrCodeNetwork ErrorCode = "NETWORK"
	// ErrCodeUnknown 表示未知錯誤。
	ErrCodeUnknown ErrorCode = "UNKNOWN"
)

// AppError represents a structured application error.
type AppError struct {
	Code        ErrorCode      `json:"code"`
	Message     string         `json:"message"`
	Details     string         `json:"details,omitempty"`
	Cause       error          `json:"-"`
	Context     map[string]any `json:"context,omitempty"`
	Recoverable bool           `json:"recoverable"`
}

// Error implements the error interface.
func (e *AppError) Error() string {
	var parts []string

	// Start with code and message without separator
	result := fmt.Sprintf("[%s] %s", e.Code, e.Message)

	// Add details if present
	if e.Details != "" {
		parts = append(parts, fmt.Sprintf("詳細: %s", e.Details))
	}

	// Add cause if present
	if e.Cause != nil {
		parts = append(parts, fmt.Sprintf("原因: %s", e.Cause.Error()))
	}

	// Join additional parts with " - " separator
	if len(parts) > 0 {
		result += " - " + strings.Join(parts, " - ")
	}

	return result
}

// Unwrap returns the underlying cause error.
func (e *AppError) Unwrap() error {
	return e.Cause
}

// Is checks if the error matches the target error code.
func (e *AppError) Is(target error) bool {
	if targetErr, ok := target.(*AppError); ok {
		return e.Code == targetErr.Code
	}

	return false
}

// WithContext adds context information to the error.
func (e *AppError) WithContext(key string, value any) *AppError {
	if e.Context == nil {
		e.Context = make(map[string]any)
	}

	e.Context[key] = value

	return e
}

// NewAppError creates a new application error.
func NewAppError(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Recoverable: IsRecoverable(code),
	}
}

// NewAppErrorWithDetails creates a new application error with details.
func NewAppErrorWithDetails(code ErrorCode, message, details string) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Details:     details,
		Recoverable: IsRecoverable(code),
	}
}

// WrapError wraps an existing error with application error context.
func WrapError(err error, code ErrorCode, message string) *AppError {
	return &AppError{
		Code:        code,
		Message:     message,
		Cause:       err,
		Recoverable: IsRecoverable(code),
	}
}

// IsRecoverable determines if an error type is recoverable.
func IsRecoverable(code ErrorCode) bool {
	switch code {
	case ErrCodeFileNotFound, ErrCodeFileFormat, ErrCodeDataValidation,
		ErrCodeUserInput, ErrCodeConfigValidation, ErrCodeInsufficientData,
		ErrCodeFileTooLarge, ErrCodePathValidation, ErrCodeDataParsing,
		ErrCodeCalculation, ErrCodeConfigLoad, ErrCodeGUIOperation, ErrCodeUnknown:
		return true
	case ErrCodeMemory, ErrCodeFilePermission, ErrCodeNetwork:
		return false
	}

	return true
}

// ValidationError represents input validation errors.
type ValidationError struct {
	*AppError
	Field string `json:"field,omitempty"`
	Value any    `json:"value,omitempty"`
}

// NewValidationError creates a new validation error.
func NewValidationError(field string, value any, message string) *ValidationError {
	return &ValidationError{
		AppError: &AppError{
			Code:        ErrCodeDataValidation,
			Message:     message,
			Recoverable: true,
			Context: map[string]any{
				"field": field,
				"value": value,
			},
		},
		Field: field,
		Value: value,
	}
}

// Error returns a formatted error message for ValidationError.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("[%s] 欄位 '%s' 驗證失敗: %s (值: %v)",
		e.Code, e.Field, e.Message, e.Value)
}
