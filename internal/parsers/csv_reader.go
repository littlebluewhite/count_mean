package parsers

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
)

// BOMBytes UTF-8 BOM constant.
//
//nolint:gochecknoglobals // immutable byte constant
var BOMBytes = []byte{0xEF, 0xBB, 0xBF}

// ReadCSVDirect reads a CSV file directly without path validation.
// Used for phase synchronization analysis.
func ReadCSVDirect(filepath string) ([][]string, error) {
	file, err := os.Open(filepath) //nolint:gosec // filepath is validated by caller
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	// Read file content and check for BOM
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	// Remove BOM character if present
	content = bytes.TrimPrefix(content, BOMBytes)

	// Create CSV reader with processed content
	reader := csv.NewReader(bytes.NewReader(content))
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // Allow different number of fields per row

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}

	return records, nil
}
