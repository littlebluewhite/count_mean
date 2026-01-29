package parsers

// Sampling frequency constants (Hz).
const (
	// FrequencyEMG is the default EMG sampling frequency (1000Hz).
	FrequencyEMG = 1000.0
	// FrequencyANC is the default ANC (force plate) sampling frequency (1000Hz).
	FrequencyANC = 1000.0
	// FrequencyMotion is the default Motion capture sampling frequency (250Hz).
	FrequencyMotion = 250.0
)

// Buffer size constants.
const (
	// BufferInitKB is the initial buffer size in kilobytes.
	BufferInitKB = 64
	// BufferMaxBytes is the maximum buffer size in bytes (1MB).
	BufferMaxBytes = 1024 * 1024
	// KilobyteMultiplier converts kilobytes to bytes.
	KilobyteMultiplier = 1024
)

// ANC file format constants.
const (
	// ANCHeaderRows is the number of header rows in ANC files.
	ANCHeaderRows = 11
	// ANCDataStartLine is the 1-indexed line where data starts.
	ANCDataStartLine = 12
	// ANCChannelNameRow is the 0-indexed row for channel names.
	ANCChannelNameRow = 8
)

// Motion file format constants.
const (
	// MotionCategoryRow is the 0-indexed row for category names.
	MotionCategoryRow = 1
	// MotionSubcatRow is the 0-indexed row for subcategory names.
	MotionSubcatRow = 2
	// MotionHeaderRow is the 0-indexed row for headers.
	MotionHeaderRow = 3
	// MotionDataRow is the 0-indexed row where data starts.
	MotionDataRow = 4
)

// Phase manifest constants.
const (
	// PhaseManifestMinFields is the minimum fields per row.
	PhaseManifestMinFields = 15
)

// Time conversion constants.
const (
	// RoundingOffset for nearest integer rounding (0.5).
	RoundingOffset = 0.5
)
