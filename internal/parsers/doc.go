// Package parsers provides file parsing functionality for EMG data analysis.
// It includes parsers for various file formats:
//   - ANC (Analog channel) force plate data (.anc and .xlsx formats)
//   - EMG (Electromyography) data in CSV format
//   - Motion capture data in CSV format
//   - Phase manifest files for phase synchronization analysis
//
// Each parser handles format-specific details like headers, sampling rates,
// and data structure while providing a consistent interface for data access.
package parsers
