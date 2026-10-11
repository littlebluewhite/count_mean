// Package parsers provides file parsing functionality for EMG data analysis.
// It includes parsers for various file formats:
//   - ANC (Analog channel) force plate data (.anc tab-separated text and .xlsx Excel)
//   - EMG (Electromyography) data in CSV format
//   - Motion capture data in CSV format
//   - Phase manifest files in CSV format (15+ columns: Subject / MotionFile /
//     ForceFile / EMGFile / EMGMotionOffset / 10 phase points). Not JSON —
//     past internal docs referenced a JSON manifest, but the current and only
//     supported format is CSV (see PhaseManifestParser.Parse).
//
// In addition to format-specific parsers, the package exposes DataParser, a
// shared helper that turns already-tokenised CSV records into EMGDataset for
// MaxMeanCalculator, Normalizer and PhaseAnalyzer.
//
// Each parser handles format-specific details like headers, sampling rates,
// and data structure while providing a consistent interface for data access.
//
// # Reader 契約
//
// 各格式 parser 一律收 io.Reader 加 name(Parse(r io.Reader, name string),ADR-0017 D4),
// 本套件不開任何檔案。開檔、路徑驗證與大小上限屬於 caller 的讀檔門:manifest 引用的資料檔
// (EMG / Motion / Force / muscle_ratio)走 manifest.OpenDataFile,分期總檔案本身走
// manifest.LoadManifests(ADR-0056)。name 只帶副檔名(ANC 以 .anc / .xlsx 分流)與錯誤 context。
package parsers
