package parsers

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"os"

	"count_mean/internal/csvutil"
	"count_mean/internal/security/fsperm"
)

// ReadCSVDirect reads a CSV file directly without path validation.
// Used for phase synchronization analysis.
//
// Thin wrapper: opens with fsperm.ReadFlags (O_NOFOLLOW, symmetric with the
// write side) then delegates to ReadCSVRecords for the BOM-aware parse. The
// reader-based core lets the Phase-D validated-open door hand an already-open
// *os.File straight to ReadCSVRecords without re-opening by path.
func ReadCSVDirect(filepath string) ([][]string, error) {
	file, err := os.OpenFile(filepath, fsperm.ReadFlags, 0) //nolint:gosec // filepath validated by caller; fsperm.ReadFlags adds O_NOFOLLOW (symmetric with write-side)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			_ = closeErr
		}
	}()

	return ReadCSVRecords(file)
}

// ReadCSVRecords reads BOM-aware CSV records from an io.Reader.
//
// Shared core for ReadCSVDirect (path-based wrapper) and the reader-based
// Parse entry points. BOM handling + csv.Reader settings (TrimLeadingSpace /
// LazyQuotes / FieldsPerRecord=-1) are load-bearing and identical to the
// historical ReadCSVDirect behavior.
func ReadCSVRecords(r io.Reader) ([][]string, error) {
	// Buffered single-pass：bufio.Reader 提供 Peek/Discard 介面給 PeekBOM，
	// csv.Reader 直接吃 bufio.Reader 省下「ReadAll 整檔到 []byte 後再 bytes.NewReader」
	// 的中間複本（記憶體峰值 ↓50%）。
	// 注意：reader.ReadAll() 仍會把 [][]string 一次性 materialize 在記憶體裡 — 不是
	// 真正的 row-by-row 流式（cross-compare review Claude P2）。如果要支援多 GB 檔，
	// 必須改成 reader.Read() loop + 即時 process per-row；目前 Motion / Phase CSV
	// 規模在 MB 量級，buffered single-pass 已是合適的成本/複雜度平衡。
	bufReader := bufio.NewReaderSize(r, csvReaderBufSize)
	if _, err := csvutil.PeekBOM(bufReader); err != nil {
		return nil, fmt.Errorf("peek BOM: %w", err)
	}

	reader := csv.NewReader(bufReader)
	reader.TrimLeadingSpace = true
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1 // Allow different number of fields per row

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse CSV: %w", err)
	}

	return records, nil
}

// csvReaderBufSize 是 bufio.Reader 的初始 buffer 大小。
// 64KB 與 Go 標準 io 多處預設一致，足以容納絕大多數 CSV 列且不至於浪費；
// bufio 在遇到更大行時會自動擴展。
const csvReaderBufSize = 64 * 1024
