package csvutil

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
)

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

// RowEmitter is the iterator callback passed to CSVPayload. Caller iterates
// over rows and invokes emit per row. Returning a non-nil error from emit aborts
// the write and bubbles back; returning a non-nil error from the RowEmitter
// itself has the same effect.
//
// 由 CSVPayload 傳入的 emit 已自動套用 SanitizeCellForWrite — caller 不需要
// （也不應該）在 emit 前對 row 預先 sanitize。Sanitize 是 idempotent（重複呼叫
// 不會多加前綴），但減少 caller 心智負擔仍以「在 CSVPayload 邊界做」為佳。
type RowEmitter func(emit func(row []string) error) error

// SafeWriteOptions configures CSVPayload.
type SafeWriteOptions struct {
	// Header is mandatory; CSVPayload runs SanitizeHeaderRow before writing.
	Header []string
	// Emit is mandatory; CSVPayload drives the emit callback for each row.
	// 注意：傳入 emit 的 row 會自動經 SanitizeCellForWrite 過 — 阻擋 CSV 公式
	// 注入。Caller 不需要自己呼叫 SanitizeBodyRow。
	Emit RowEmitter
	// OmitBOM 為 true 時不寫 UTF-8 BOM。零值照舊寫 BOM，既有 caller 與 byte-identity
	// golden 不受影響。CSV 輸出的 BOM 開關只由 internal/io 依 config.BOMEnabled 設定。
	OmitBOM bool
}

// CSVPayload 回傳把 opts 描述的 CSV 寫進 w 的 payload，給 fsperm.AtomicWriteFile 使用;
// 落檔位置與 atomic 協定屬 internal/io 的 Output placement(ADR-0055)。
// 它持有 CSV 的位元組：UTF-8 BOM（OmitBOM 為 false 時）、SanitizeHeaderRow、逐列
// SanitizeBodyRow，以及 mid-write Flush/Error 探測。
// 回傳前會 Flush 並檢查 writer.Error()，符合 AtomicWriteFile「write 回傳前自行 flush」的契約。
//
//nolint:err113 // dynamic errors for user-facing output
func CSVPayload(opts SafeWriteOptions) func(io.Writer) error {
	return func(w io.Writer) error {
		if opts.Header == nil {
			return errors.New("SafeWriteOptions.Header 不可為 nil")
		}
		if opts.Emit == nil {
			return errors.New("SafeWriteOptions.Emit 不可為 nil")
		}

		if !opts.OmitBOM {
			if err := WriteBOM(w); err != nil {
				return fmt.Errorf("寫入 BOM 失敗: %w", err)
			}
		}

		// csv.Writer 內部 buffered,真正 error 只在 Flush 或下次 Write 才 surface。
		writer := csv.NewWriter(w)

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
	}
}
