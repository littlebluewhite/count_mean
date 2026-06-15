package csvutil

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"

	"count_mean/internal/security/fsperm"
)

// writerWrapHook 是 加的 test injection point:test 用此 hook 把 *os.File
// 包成失敗 writer 模擬 disk full / pipe broken。production code 不註冊此 hook,
// 直接走 raw *os.File。
//
// 為何用 hook 而非把 io.Writer 提到 public API:WriteCSVAtomic 對外仍只負責
// 「給定 path,做 atomic CSV 寫」契約;暴露 io.Writer 會破壞 atomic 流程
// (caller 沒辦法保證 rename 走 tmp+rename 邏輯)。test-only hook 保留契約純度
// 同時取得 testability。
//
//nolint:gochecknoglobals // test-only injection point
var writerWrapHook func(io.Writer) io.Writer

// midWriteErrorCheckInterval 是 加入的「每 N rows 檢查一次 writer.Error」
// 步長。csv.Writer 的 Write 內部 buffer flush 是 lazy,Write 自己幾乎不 return
// error(因為它返回的是 nil 直到 underlying writer 出問題後的下次 Flush)。
// 若 caller 一口氣 emit N 萬 row 中途 disk 滿 / pipe broken,目前只在末段
// Flush 才會察覺,大量 row 已寫進半成品 tmp 檔。
//
// 加 mid-loop check 後:每 1024 row 主動 Flush + Error() 探測,一旦失敗立刻 abort
// + 走 defer cleanup 刪 tmp。1024 平衡 throughput (太頻繁 syscall) 與「最壞
// case 多寫了多少 row」(<=1024 row 的浪費 vs N 萬 row 的浪費)。
const midWriteErrorCheckInterval = 1024

// RowEmitter is the iterator callback passed to WriteCSVAtomic. Caller iterates
// over rows and invokes emit per row. Returning a non-nil error from emit aborts
// the write and bubbles back; returning a non-nil error from the RowEmitter
// itself has the same effect.
//
// 由 WriteCSVAtomic 傳入的 emit 已自動套用 SanitizeCellForWrite — caller 不需要
// （也不應該）在 emit 前對 row 預先 sanitize。Sanitize 是 idempotent（重複呼叫
// 不會多加前綴），但減少 caller 心智負擔仍以「在 WriteCSVAtomic 邊界做」為佳。
type RowEmitter func(emit func(row []string) error) error

// SafeWriteOptions configures WriteCSVAtomic.
type SafeWriteOptions struct {
	// Header is mandatory; WriteCSVAtomic runs SanitizeHeaderRow before writing.
	Header []string
	// Emit is mandatory; WriteCSVAtomic drives the emit callback for each row.
	// 注意：傳入 emit 的 row 會自動經 SanitizeCellForWrite 過 — 阻擋 CSV 公式
	// 注入。Caller 不需要自己呼叫 SanitizeRow / SanitizeAllRows。
	Emit RowEmitter
	// BasePaths is optional. When non-empty, the tmp-create + rename are
	// dirfd-anchored via fsperm.OpenAtomicWriteValidated — the whole tmp →
	// rename sequence runs relative to a single dirfd of the *validated,
	// resolved* parent directory, closing the atomic-write parent-swap TOCTOU
	// (ADR-0017 Decision 6 / #34). The parent must resolve under one of these
	// bases or the write is rejected (ErrPathEscapesBase). When empty,
	// WriteCSVAtomic falls back to the legacy os.OpenFile + os.Rename path,
	// byte-identical to today.
	BasePaths []string
}

// WriteCSVAtomic writes a CSV to path atomically. It owns the CSV payload —
// UTF-8 BOM, SanitizeHeaderRow, per-row SanitizeBodyRow, the mid-write
// Flush/Error probe, and the writerWrapHook test seam — and delegates the
// durable-placement protocol (crypto-tmp → open → fsync → atomic rename →
// parent fsync, dirfd-anchored when BasePaths is set) to fsperm.AtomicWriteFile.
// Output is byte-identical to the pre-extraction flow.
//
// BasePaths semantics are unchanged: non-empty → dirfd-anchored validated write
// (parent-swap TOCTOU closed, ErrPathEscapesBase on a base miss); empty → legacy
// os.OpenFile + os.Rename fallback.
//
//nolint:err113 // dynamic errors for user-facing output
func WriteCSVAtomic(path string, opts SafeWriteOptions) error {
	if opts.Header == nil {
		return errors.New("SafeWriteOptions.Header 不可為 nil")
	}
	if opts.Emit == nil {
		return errors.New("SafeWriteOptions.Emit 不可為 nil")
	}

	return fsperm.AtomicWriteFile(path, opts.BasePaths, func(w io.Writer) error {
		if err := WriteBOM(w); err != nil {
			return fmt.Errorf("寫入 BOM 失敗: %w", err)
		}

		// test hook 可包裝 underlying writer 模擬 disk full / pipe broken,
		// production 路徑 hook=nil 直接走 raw writer。csv.Writer 內部 buffered,
		// 真正 error 只在 Flush 或下次 Write 才 surface。
		var underlying = w
		if writerWrapHook != nil {
			underlying = writerWrapHook(w)
		}
		writer := csv.NewWriter(underlying)

		if err := writer.Write(SanitizeHeaderRow(opts.Header)); err != nil {
			return fmt.Errorf("寫入標題列失敗: %w", err)
		}

		// Body rows 也必須過 sanitize — caller 的 Emit 寫的 row 可能含 user-controlled
		// data（subject 名稱、phase label、channel header round-trip 等），未 sanitize
		// 會把 CSV injection 直接洩漏給下游 Excel/Numbers。
		// 改用 SanitizeBodyRow alias(行為等價 SanitizeHeaderRow,但 caller 意圖
		// 更明確 — 這裡的 row 是 body row,不是 header round-trip)。
		//
		// 每 midWriteErrorCheckInterval 列做一次 Flush + writer.Error() 探測。
		// csv.Writer.Write 不直接 surface underlying IO error(buffered),若 caller
		// 一口氣 emit 萬筆 row 中途 disk 滿,目前只在末段 Flush 才察覺,大量 row
		// 已寫進半成品 tmp。mid-loop check 後立即 abort + defer cleanup 刪 tmp。
		rowCount := 0
		sanitizingEmit := func(row []string) error {
			if err := writer.Write(SanitizeBodyRow(row)); err != nil {
				return fmt.Errorf("csv write row: %w", err)
			}
			rowCount++
			if rowCount%midWriteErrorCheckInterval == 0 {
				writer.Flush()
				if err := writer.Error(); err != nil {
					// 包成 dedicated error 讓 caller (定 defer) 與 test 能精確識別。
					return fmt.Errorf("CSV mid-write 探測失敗 (%d 列後): %w", rowCount, err)
				}
			}
			return nil
		}
		if err := opts.Emit(sanitizingEmit); err != nil {
			return err
		}

		writer.Flush()
		if err := writer.Error(); err != nil {
			return fmt.Errorf("CSV flush 失敗: %w", err)
		}
		return nil
	})
}
