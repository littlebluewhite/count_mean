package csvutil

import (
	"encoding/csv"
	"errors"
	"fmt"
	"os"

	"count_mean/internal/security/fsperm"
)

// RowEmitter is the iterator callback passed to WriteCSVAtomic. Caller iterates
// over rows and invokes emit per row. Returning a non-nil error from emit aborts
// the write and bubbles back; returning a non-nil error from the RowEmitter
// itself has the same effect.
type RowEmitter func(emit func(row []string) error) error

// SafeWriteOptions configures WriteCSVAtomic.
type SafeWriteOptions struct {
	// Header is mandatory; WriteCSVAtomic runs SanitizeHeaderRow before writing.
	Header []string
	// Emit is mandatory; WriteCSVAtomic drives the emit callback for each row.
	Emit RowEmitter
}

// WriteCSVAtomic writes a CSV to path using a tmp+rename atomic flow:
//
//  1. Open path+".tmp" with O_EXCL — stale tmp from a prior crash surfaces as
//     error rather than silent overwrite
//  2. Write UTF-8 BOM
//  3. Write SanitizeHeaderRow(Header)
//  4. Drive Emit with the row writer
//  5. csv.Writer Flush + file Sync + Close
//  6. os.Rename(tmp, path)
//
// 任一步驟失敗 → defer 刪 tmp（best-effort），final path 不變動。
//
//nolint:err113 // dynamic errors for user-facing output
func WriteCSVAtomic(path string, opts SafeWriteOptions) (err error) {
	if opts.Header == nil {
		return errors.New("SafeWriteOptions.Header 不可為 nil")
	}
	if opts.Emit == nil {
		return errors.New("SafeWriteOptions.Emit 不可為 nil")
	}

	tmp := path + ".tmp"

	//nolint:gosec // G304: path validated by caller (e.g. config.Validate + PathValidator)
	file, err := os.OpenFile(tmp, fsperm.TmpCreateFlags, fsperm.FilePerm)
	if err != nil {
		return fmt.Errorf("建立 tmp 檔案失敗: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmp) //nolint:errcheck // best-effort cleanup of orphan tmp
		}
	}()

	if err := WriteBOM(file); err != nil {
		_ = file.Close() //nolint:errcheck // cleanup path; outer error being returned
		return fmt.Errorf("寫入 BOM 失敗: %w", err)
	}

	writer := csv.NewWriter(file)

	if err := writer.Write(SanitizeHeaderRow(opts.Header)); err != nil {
		_ = file.Close() //nolint:errcheck // cleanup path; outer error being returned
		return fmt.Errorf("寫入標題列失敗: %w", err)
	}

	if err := opts.Emit(writer.Write); err != nil {
		_ = file.Close() //nolint:errcheck // cleanup path; outer error being returned
		return err
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = file.Close() //nolint:errcheck // cleanup path; outer error being returned
		return fmt.Errorf("CSV flush 失敗: %w", err)
	}

	if err := file.Sync(); err != nil {
		_ = file.Close() //nolint:errcheck // cleanup path; outer error being returned
		return fmt.Errorf("fsync 失敗: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("關閉 tmp 檔案失敗: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename tmp → final 失敗: %w", err)
	}

	committed = true
	return nil
}
