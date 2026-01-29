// Package errors provides error types and utilities for the EMG data analysis application.
// It includes calculator-specific sentinel errors, application errors, processing errors,
// and validation errors with support for error wrapping and context information.
package errors

import (
	"errors"
	"fmt"
)

// Calculator sentinel errors - use errors.Is() to check.
var (
	// ErrEmptyDataset indicates the dataset is empty or nil.
	ErrEmptyDataset = errors.New("dataset is empty")

	// ErrWindowTooLarge indicates the window size exceeds data length.
	ErrWindowTooLarge = errors.New("window size exceeds data length")

	// ErrInvalidWindowSize indicates the window size is invalid (e.g., <= 0).
	ErrInvalidWindowSize = errors.New("window size must be greater than 0")

	// ErrInvalidTimeRange indicates the time range is invalid or has insufficient data.
	ErrInvalidTimeRange = errors.New("invalid time range")

	// ErrInsufficientData indicates there is not enough data for the requested analysis.
	ErrInsufficientData = errors.New("insufficient data for analysis")

	// ErrChannelMismatch indicates channel count mismatch between datasets.
	ErrChannelMismatch = errors.New("channel count mismatch")

	// ErrZeroReference indicates the reference value is zero (division by zero).
	ErrZeroReference = errors.New("reference value is zero")

	// ErrPhaseMismatch indicates phase count does not match labels.
	ErrPhaseMismatch = errors.New("phase count does not match labels")

	// ErrInvalidReferenceData indicates the reference data is invalid.
	ErrInvalidReferenceData = errors.New("invalid reference data")
)

// CalculatorError wraps a sentinel error with additional context.
type CalculatorError struct {
	Sentinel error                  // The underlying sentinel error for errors.Is() matching
	Message  string                 // Localized/contextual message
	Context  map[string]interface{} // Additional context information
}

// Error implements the error interface.
func (e *CalculatorError) Error() string {
	if e.Message != "" {
		return e.Message
	}

	return e.Sentinel.Error()
}

// Unwrap returns the underlying sentinel error for errors.Is() compatibility.
func (e *CalculatorError) Unwrap() error {
	return e.Sentinel
}

// Is checks if the error matches the target.
func (e *CalculatorError) Is(target error) bool {
	return errors.Is(e.Sentinel, target)
}

// NewCalculatorError creates a new CalculatorError with a sentinel error and message.
func NewCalculatorError(sentinel error, message string) *CalculatorError {
	return &CalculatorError{
		Sentinel: sentinel,
		Message:  message,
	}
}

// NewCalculatorErrorWithContext creates a new CalculatorError with context.
func NewCalculatorErrorWithContext(sentinel error, message string, context map[string]interface{}) *CalculatorError {
	return &CalculatorError{
		Sentinel: sentinel,
		Message:  message,
		Context:  context,
	}
}

// WrapParseError wraps a parsing error with row/column context.
func WrapParseError(row, col int, err error) error {
	return fmt.Errorf("parsing error at row %d, col %d: %w", row, col, err)
}

// IsEmptyDataset checks if the error is an empty dataset error.
func IsEmptyDataset(err error) bool {
	return errors.Is(err, ErrEmptyDataset)
}

// IsWindowTooLarge checks if the error is a window too large error.
func IsWindowTooLarge(err error) bool {
	return errors.Is(err, ErrWindowTooLarge)
}

// IsInvalidWindowSize checks if the error is an invalid window size error.
func IsInvalidWindowSize(err error) bool {
	return errors.Is(err, ErrInvalidWindowSize)
}

// IsInvalidTimeRange checks if the error is an invalid time range error.
func IsInvalidTimeRange(err error) bool {
	return errors.Is(err, ErrInvalidTimeRange)
}

// IsInsufficientData checks if the error is an insufficient data error.
func IsInsufficientData(err error) bool {
	return errors.Is(err, ErrInsufficientData)
}

// IsChannelMismatch checks if the error is a channel mismatch error.
func IsChannelMismatch(err error) bool {
	return errors.Is(err, ErrChannelMismatch)
}

// IsZeroReference checks if the error is a zero reference error.
func IsZeroReference(err error) bool {
	return errors.Is(err, ErrZeroReference)
}

// IsPhaseMismatch checks if the error is a phase mismatch error.
func IsPhaseMismatch(err error) bool {
	return errors.Is(err, ErrPhaseMismatch)
}

// IsInvalidReferenceData checks if the error is an invalid reference data error.
func IsInvalidReferenceData(err error) bool {
	return errors.Is(err, ErrInvalidReferenceData)
}
