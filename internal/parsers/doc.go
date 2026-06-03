// Package parsers provides file parsing functionality for EMG data analysis.
// It includes parsers for various file formats:
//   - ANC (Analog channel) force plate data (.anc tab-separated text and .xlsx Excel)
//   - EMG (Electromyography) data in CSV format
//   - Motion capture data in CSV format
//   - Phase manifest files in CSV format (15+ columns: Subject / MotionFile /
//     ForceFile / EMGFile / EMGMotionOffset / 10 phase points). Not JSON —
//     past internal docs referenced a JSON manifest, but the current and only
//     supported format is CSV (see PhaseManifestParser.ParseFile).
//
// In addition to format-specific parsers, the package exposes DataParser, a
// shared helper that turns already-tokenised CSV records into EMGDataset for
// MaxMeanCalculator, Normalizer and PhaseAnalyzer.
//
// Each parser handles format-specific details like headers, sampling rates,
// and data structure while providing a consistent interface for data access.
//
// # Path validation contract
//
// Parsers in this package do NOT validate caller-supplied filepaths themselves.
// They open files via internal/security/fsperm flags (which add O_NOFOLLOW
// on unix to defeat symlink-based TOCTOU), but path traversal / allow-listing
// is the caller's responsibility. GUI and CLI entry points must run inputs
// through internal/security.PathValidator (or the lenient_path helper for
// BTS-style filenames containing literal "%") before calling any ParseFile
// here. See gui/normalized_phase_sync_handlers.go for the production wiring.
package parsers
