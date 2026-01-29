package parsers

import "errors"

// Error definitions for the parsers package.
// All errors use wrapped static errors as required by err113 linter.
var (
	// ErrFileEmpty indicates an empty input file.
	ErrFileEmpty = errors.New("file is empty")
	// ErrInsufficientData indicates insufficient data rows.
	ErrInsufficientData = errors.New("insufficient data rows")
	// ErrInvalidFormat indicates invalid file format.
	ErrInvalidFormat = errors.New("invalid file format")
	// ErrNoChannels indicates no valid channels found.
	ErrNoChannels = errors.New("no valid channels found")
	// ErrNoValidData indicates no valid data rows.
	ErrNoValidData = errors.New("no valid data rows")
	// ErrInconsistentLength indicates data length inconsistency.
	ErrInconsistentLength = errors.New("data length inconsistent")
	// ErrInvalidTimeRange indicates invalid time range.
	ErrInvalidTimeRange = errors.New("invalid time range")
	// ErrTimeRangeNotFound indicates no data in time range.
	ErrTimeRangeNotFound = errors.New("no data found in time range")
	// ErrIndexNotFound indicates index not found.
	ErrIndexNotFound = errors.New("index not found")
	// ErrInvalidIndexRange indicates invalid index range.
	ErrInvalidIndexRange = errors.New("invalid index range")
	// ErrIndexRangeNotFound indicates no data in index range.
	ErrIndexRangeNotFound = errors.New("no data found in index range")
	// ErrInsufficientFields indicates row has insufficient fields.
	ErrInsufficientFields = errors.New("insufficient fields in row")
	// ErrUnknownPhasePoint indicates unknown phase point.
	ErrUnknownPhasePoint = errors.New("unknown phase point")
	// ErrNilData indicates nil data pointer.
	ErrNilData = errors.New("data is nil")
	// ErrTimeSequenceEmpty indicates empty time sequence.
	ErrTimeSequenceEmpty = errors.New("time sequence is empty")
	// ErrTimeNotIncreasing indicates non-increasing sequence.
	ErrTimeNotIncreasing = errors.New("sequence is not increasing")
	// ErrHeaderNotFound indicates header row not found.
	ErrHeaderNotFound = errors.New("header row not found")
	// ErrInsufficientHeaders indicates not enough headers.
	ErrInsufficientHeaders = errors.New("insufficient headers")
	// ErrNoWorksheet indicates no worksheet in Excel.
	ErrNoWorksheet = errors.New("no worksheet found")
	// ErrChannelNameFormat indicates bad channel name format.
	ErrChannelNameFormat = errors.New("channel name format error")
)
