package csvutil

import (
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

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

// tmpRandSuffixLen 為 atomic-write tmp 檔名的 crypto/rand 後綴長度 (bytes,hex 後翻倍)。
// 8 bytes → 16 hex chars,2^64 entropy 足以擋 birthday attack。
const tmpRandSuffixLen = 8

// fsNameMaxBytes 為單一路徑元件 (basename) 的保守 byte 上限。ext4 / APFS / XFS /
// NTFS 皆為 255 bytes。atomic tmp basename = final basename + ".tmp." + 16 hex;
// 即使 final basename 合法 (caller + filename.Sanitize 截到 200 bytes),tmp 疊上
// 隨機後綴後仍可能跨越此限 → ENAMETOOLONG。makeTmpPath 對 tmp basename 做預算
// 截斷,避免「最終檔名合法但 atomic 寫入失敗」—— 長 subject + 長後綴 writer
// (如 normalized PhaseSync,後綴 ~38 bytes) 在 ~196+ byte subject 下會踩到。
const fsNameMaxBytes = 255

// makeTmpPath 為 atomic-write 流程產生不可預測的 tmp 路徑。
//
// 釘住:舊版 tmp = `path + ".tmp"` 完全可預測,攻擊者可在 OpenFile 前 plant
// 同名 file (race) 或單純 DoS final commit。crypto/rand 後綴讓 attacker 無法
// pre-compute,且 O_EXCL 仍保留 (撞名極小機率下安全 fail)。
//
// 失敗回 error,caller 應 surface — 沒有 crypto/rand 的環境屬於異常狀態,
// 不該 silently fall back 到 deterministic 名稱 (那等於 disable 此防護)。
//
// tmp basename = base + suffix 可能跨越 NAME_MAX (見 fsNameMaxBytes):截 base
// 前綴讓 tmp 元件 fit (截的是 base 而非 suffix — 後者是 collision-resistance
// 隨機熵,削了會降低撞名抵抗);final os.Rename 仍用完整 path,對外檔名與
// 目錄不變。用 filepath.Split 而非字串拼接,確保 dir+base==path 不被 clean 改寫。
func makeTmpPath(path string) (string, error) {
	b := make([]byte, tmpRandSuffixLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand 失敗,無法為 tmp 產生 random suffix: %w", err)
	}

	suffix := ".tmp." + hex.EncodeToString(b)
	dir, base := filepath.Split(path)
	if len(base)+len(suffix) > fsNameMaxBytes {
		base = truncateToBytes(base, fsNameMaxBytes-len(suffix))
	}

	return dir + base + suffix, nil
}

// truncateToBytes 把 s 截到最多 maxBytes,且不切斷 multi-byte UTF-8 rune
// (退回最後一個完整 rune 邊界) — 避免 tmp basename 含半個 rune 被 strict
// filesystem (FAT32 reject / APFS 顯示亂碼) 拒絕。
func truncateToBytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	// maxBytes 落在 s 內部;若切到 multi-byte rune 中段,退到 rune 邊界。
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

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
}

// WriteCSVAtomic writes a CSV to path using a tmp+rename atomic flow:
//
//  1. Open `path + ".tmp." + <crypto/rand hex>` with O_EXCL — tmp filename 含
//     random suffix,attacker 無法預先 plant 同名檔。O_EXCL 仍保留作為
//     extra collision guard
//  2. Write UTF-8 BOM
//  3. Write SanitizeHeaderRow(Header)
//  4. Drive Emit with the row writer
//  5. csv.Writer Flush + file Sync + Close
//  6. os.Rename(tmp, path)
//
// 任一步驟失敗 → defer 刪 tmp（best-effort），final path 不變動。
//
// 後行為變化:legacy `path + ".tmp"` (前一次 crash 留下) 不再 block 新寫入,
// 因 tmp filename 已 random — 但也意味 legacy stale 不會被自動清理,屬已知
// trade-off (random tmp 安全收益 > stale file noise)。
//
//nolint:err113 // dynamic errors for user-facing output
func WriteCSVAtomic(path string, opts SafeWriteOptions) (err error) {
	if opts.Header == nil {
		return errors.New("SafeWriteOptions.Header 不可為 nil")
	}
	if opts.Emit == nil {
		return errors.New("SafeWriteOptions.Emit 不可為 nil")
	}

	// tmp filename 改用 crypto/rand 後綴,不可預測 — 擋掉「attacker 預先
	// plant `.tmp` 撞名 DoS / race」攻擊。O_EXCL (見 fsperm.TmpCreateFlags) 仍保留,
	// 撞名極小機率下安全 fail。
	tmp, err := makeTmpPath(path)
	if err != nil {
		return err
	}

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

	// test hook 可包裝 underlying writer 模擬 disk full / pipe broken,
	// production 路徑 hook=nil 直接走 raw file。csv.Writer 內部 buffered,
	// 真正 error 只在 Flush 或下次 Write 才 surface。
	var underlying io.Writer = file
	if writerWrapHook != nil {
		underlying = writerWrapHook(file)
	}
	writer := csv.NewWriter(underlying)

	if err := writer.Write(SanitizeHeaderRow(opts.Header)); err != nil {
		_ = file.Close() //nolint:errcheck // cleanup path; outer error being returned
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
