// Package csvutil provides CSV utilities shared across the codebase.
//
// # BOM support scope
//
// The BOM helpers in this package — [BOMBytes], [WriteBOM], [PeekBOM] — operate
// **exclusively on the UTF-8 BOM** (0xEF 0xBB 0xBF). Other byte-order marks are
// out of scope and are treated as ordinary payload:
//
//   - UTF-16 LE BOM (0xFF 0xFE) — not detected; bytes preserved in the reader.
//   - UTF-16 BE BOM (0xFE 0xFF) — not detected; bytes preserved in the reader.
//   - UTF-32 BOMs (0xFF 0xFE 0x00 0x00 / 0x00 0x00 0xFE 0xFF) — not detected.
//
// Rationale: every CSV file produced by this codebase is UTF-8 (CSV/EMG/ANC/Motion
// parsers all assume UTF-8 byte semantics). Adding UTF-16 detection would imply
// transparent transcoding, which requires golang.org/x/text/transform and a much
// larger contract change (encoding negotiation across the pipeline). When/if the
// product needs to ingest UTF-16 CSVs, that work belongs in a dedicated decoder
// in `internal/parsers/`, not in this helper package.
//
// The unit test TestPeekBOM_UTF16BOM_NotDetected pins this behavior so a future
// well-meaning patch cannot silently start consuming UTF-16 BOM bytes without
// the corresponding decoding path.
//
// # Formula-injection sanitize round-trip
//
// [SanitizeCellForWrite] is **not** perfectly invertible (see [UnsanitizeCell]).
// Callers that read sanitized cells back from disk and need the pre-sanitize
// representation should use [UnsanitizeCell], but be aware that user input that
// genuinely begins with `'` (e.g. `'hello`) cannot be distinguished from a
// sanitize artifact and will pass through unchanged.
package csvutil
