package io

import "strings"

// csvExtension is the file extension for CSV files.
const csvExtension = ".csv"

// ensureCSVExtension adds .csv extension if not present.
func ensureCSVExtension(filename string) string {
	if !strings.HasSuffix(strings.ToLower(filename), csvExtension) {
		return filename + csvExtension
	}

	return filename
}

// stripCSVExtension removes .csv extension if present (case-insensitive).
func stripCSVExtension(filename string) string {
	if strings.HasSuffix(strings.ToLower(filename), csvExtension) {
		return strings.TrimSuffix(filename, filename[len(filename)-len(csvExtension):])
	}

	return filename
}

// StripCSVExt removes .csv extension (case-insensitive). Package-level function.
func StripCSVExt(filename string) string {
	return stripCSVExtension(filename)
}
