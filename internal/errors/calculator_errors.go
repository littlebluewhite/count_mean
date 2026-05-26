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

	// ErrNaNReference indicates the reference value is NaN; val / NaN = NaN
	// would silently pollute the entire normalized dataset. Split from
	// ErrZeroReferenceso callers can distinguish "data quality issue,
	// retry/clean" from "real zero MVC, refuse run" — mirrors the sibling
	// range_normalizer.ErrChannelMaxNaN pattern.
	ErrNaNReference = errors.New("reference value is NaN")

	// ErrInfReference indicates the reference value is ±Inf; val / Inf yields
	// 0 or ±Inf and the result is unusable. Split from ErrZeroReference
	// for the same reason as ErrNaNReference — mirrors range_normalizer.ErrChannelMaxInf.
	ErrInfReference = errors.New("reference value is Inf")

	// ErrPhaseMismatch indicates phase count does not match labels.
	ErrPhaseMismatch = errors.New("phase count does not match labels")

	// ErrInvalidReferenceData indicates the reference data is invalid.
	ErrInvalidReferenceData = errors.New("invalid reference data")

	// ErrReferenceMultipleRows indicates the reference dataset contains more
	// than one data row. MVC/MAX reference is a single-row contract (one peak
	// per muscle); silently dropping extra rows in Normalize hides operator
	// mistakes.
	ErrReferenceMultipleRows = errors.New("reference dataset must contain exactly one row")

	// ErrNaNInChannel indicates an EMG channel sample is NaN. The sliding-window
	// MaxMean uses an incremental sum trick; once a NaN sample enters the window,
	// windowSum becomes NaN and every NaN > maxMean comparison evaluates false,
	// so the calculator silently returns the initial-window mean while masking
	// the contamination. Fail-fast surfaces the data-quality issue to
	// callers rather than returning a misleading finite result.
	ErrNaNInChannel = errors.New("channel data contains NaN")

	// ErrInfInChannel indicates an EMG channel sample is ±Inf. +Inf propagates
	// through the sliding-window sum and yields a +Inf result, while -Inf
	// triggers the same NaN > maxMean masking as ErrNaNInChannel (e.g. the
	// initial valid window's mean wins despite -Inf contamination later). Both
	// directions are rejected up front so MaxMean cannot silently mis-compute
	//.
	ErrInfInChannel = errors.New("channel data contains Inf")
)

// CalculatorError wraps a sentinel error with additional context.
type CalculatorError struct {
	Sentinel error          // The underlying sentinel error for errors.Is() matching
	Message  string         // Localized/contextual message
	Context  map[string]any // Additional context information
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
func NewCalculatorErrorWithContext(sentinel error, message string, context map[string]any) *CalculatorError {
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
