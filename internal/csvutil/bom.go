// Package csvutil provides CSV utilities shared across the codebase.
// 主要解決 BOM 序列 ([0xEF, 0xBB, 0xBF]) 與 CSV-with-BOM writer 設定散落多檔案的問題。
package csvutil

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

// bomArray is the canonical UTF-8 Byte-Order Mark, stored as an unexported
// fixed-size array. Arrays are value types in Go, so this storage cannot be
// mutated from outside the package — external callers obtain a fresh slice
// copy via BOMBytes().
//
// 此前以 `var BOM []byte` 暴露給 callers
// 出該 surface 可被任意 caller 透過 `csvutil.BOM[0] = 0xFF` 全域汙染 BOM 偵測/
// 寫入。改用 array + 拷貝 getter 後型別系統直接擋下這個 attack surface。
var bomArray = [3]byte{0xEF, 0xBB, 0xBF}

// BOMBytes returns a fresh copy of the UTF-8 BOM. Each call allocates 3 bytes;
// the cost is negligible (BOM writes are O(1) per file) and the trade-off buys
// guaranteed immutability — a caller cannot poison the package's view of the
// BOM by mutating the returned slice.
func BOMBytes() []byte {
	b := bomArray
	return b[:]
}

// WriteBOM writes the UTF-8 BOM to w.
//
// **Caveat：** WriteBOM 是無條件寫入，**不檢查 w 是否已含 BOM**。Caller 必須保證
// stream 是新的，否則會產生 double-BOM 讓下游 parser 把首列當成 garbage 開頭。
// 在 pipeline 中與 PeekBOM 互補使用時務必確認方向：input → PeekBOM (BOM 探測/吞除)
// → process → WriteBOM → output，而不是 input → WriteBOM 把已有 BOM 的 input 再寫一份。
// （cross-compare review P2）
func WriteBOM(w io.Writer) error {
	if _, err := w.Write(bomArray[:]); err != nil {
		return fmt.Errorf("write BOM: %w", err)
	}

	return nil
}

// PeekBOM detects whether r begins with a UTF-8 BOM and consumes it if present.
// 回傳 (true, nil) 並 Discard 3 bytes；無 BOM 時回 (false, nil) 且 reader 起點不動。
//
// 設計動機：取代「整檔 ReadAll → TrimBOM bytes」模式，使 BOM 偵測能在 buffered
// pipeline（bufio + csv.Reader）中運作，不需先把整檔載入 []byte。
//
// 檔案小於 3 bytes 時 Peek 回 (head, io.EOF)。視為「無 BOM」回 (false, nil)，
// 把實際 EOF 留給後續 reader.Read() 自然觸發，避免雙重 error path。
func PeekBOM(r *bufio.Reader) (bool, error) {
	head, err := r.Peek(len(bomArray))
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("PeekBOM: %w", err)
	}

	if len(head) >= len(bomArray) && bytes.Equal(head, bomArray[:]) {
		_, _ = r.Discard(len(bomArray)) //nolint:errcheck // Peek 已驗證 buffer，Discard 不會 fail

		return true, nil
	}

	return false, nil
}
