package csvutil

import (
	"strconv"
	"strings"
	"unicode"

	"count_mean/internal/logging"
)

// SanitizeCellForWrite escapes a single CSV cell that will be written to disk
// so that downstream spreadsheet readers (Excel / LibreOffice / Numbers) do not
// interpret attacker-controlled headers or values as formulas.
//
// Detection runs against the cell with leading Unicode whitespace trimmed
// (unicode.IsSpace, which covers NBSP U+00A0 / zero-width spaces / 全形空格
// 等 ASCII TrimLeft 漏掉的 vector), so attackers can't smuggle formulas behind
// non-ASCII whitespace.
//
// Triggered prefixes (on trimmed view):
//   - "="  / "＝" (formula start; "＝" U+FF1D 為全形等號，部分 Excel locale
//     會 normalize 後當公式起手)
//   - "@"  (Excel name reference, e.g. "@SUM(1+1)")
//   - "+" / "-" / "−" prefix when the trimmed content is NOT a valid number.
//     "−" U+2212 (Unicode minus) 同樣會被某些 spreadsheet 視為運算符。
//     Pure numeric "+1.5" / "-3" 不動。"−1" 不會 ParseFloat 成功（U+2212 不是
//     ASCII '-'），故會被 escape — 接受此 trade-off：偽裝成數字的 Unicode 字
//     元一律當作可疑 input。
//
// Round-trip caveat: SanitizeCellForWrite is **not** perfectly invertible.
// When a sanitized cell `'=BAD()` is read back from disk by a generic CSV reader,
// the cell value is literally `'=BAD()` — the leading `'` is part of the data, not
// a separate marker. Callers that need to recover the pre-sanitize representation
// must use [UnsanitizeCell]. Note that UnsanitizeCell cannot disambiguate a real
// user input that began with `'` (e.g. `'hello`) from a sanitize artifact, so the
// strip is conditional: only sanitized prefixes that would re-trigger sanitize are
// reversed. See [UnsanitizeCell] doc for the exact rule.
func SanitizeCellForWrite(cell string) string {
	if cell == "" {
		return cell
	}

	trimmed := strings.TrimLeftFunc(cell, isInvisibleLeading)
	if trimmed == "" {
		return cell
	}

	// defense-in-depth: 同時比對 `cell`（raw, untrimmed）與 `trimmed` 兩個 view。
	// 主要偵測仍走 trimmed view（涵蓋 NBSP / 全形空格 / 零寬等所有 invisible-leading
	// 形式），但 formulaStarters 內額外列出 `\t=` / `\r=` / `\n=` / `\t@` / `\r@` /
	// `\n@` / `\t+` / `\r+` / `\n+` / `\t-` / `\r-` / `\n-` 的字面 prefix 變體 — 萬一
	// `isInvisibleLeading` 邏輯被未來 refactor 移除 `unicode.IsSpace` 對 ASCII control
	// (`\t` `\r` `\n`) 的涵蓋（or someone narrows it to only `unicode.White_Space`
	// 而漏掉 ASCII control whitespace），這層 raw-view 比對仍能擋下 `"\t=cmd"` /
	// `"\r=cmd"` / `"\n=cmd"` 形式的 smuggle。Trade-off：多 1 次 HasPrefix loop，但
	// formulaStarters slice 短（< 20 entries），cost 可忽略。
	for _, starter := range formulaStarters {
		if strings.HasPrefix(trimmed, starter) || strings.HasPrefix(cell, starter) {
			return "'" + cell
		}
	}

	if strings.HasPrefix(trimmed, "+") ||
		strings.HasPrefix(trimmed, "-") ||
		strings.HasPrefix(trimmed, "−") {
		if _, err := strconv.ParseFloat(trimmed, 64); err != nil {
			return "'" + cell
		}
	}

	return cell
}

// SanitizeHeaderRow returns a fresh slice with SanitizeCellForWrite applied to
// every cell. Headers are the highest-risk surface because they often round-trip
// from user-uploaded files into exported CSVs without ever passing through the
// read-side cell validator.
//
// Header vs body 兩種 caller 邏輯一致(都是 per-cell sanitize),但 caller
// 意圖不同。SanitizeBodyRow 是 alias,讓 caller 端 code read 出來更明確 —
// 「我在 sanitize body row」(不是 header row 的 round-trip 情境)。
func SanitizeHeaderRow(row []string) []string {
	return sanitizeRowCells(row)
}

// SanitizeBodyRow 是 SanitizeHeaderRow 的 caller-意圖 alias。
// Body row 同樣需要 sanitize 因為:
//   - user-controllable subject 名稱會落到 row[0](muscle_ratio / cci output)
//   - phase label 從 config.json 進入 result.PhaseName 也會落到 row[0]
//   - channel header round-trip 後可能變成 body cell(轉置型輸出)
//
// 與 SanitizeHeaderRow 行為完全等價(per-cell SanitizeCellForWrite + 不修改 input),
// 拆兩個名字純粹是 readability — caller 不必擠 comment 解釋「為什麼 body row 也走
// HeaderRow 函式」。
func SanitizeBodyRow(row []string) []string {
	return sanitizeRowCells(row)
}

// sanitizeRowCells 是 SanitizeHeaderRow / SanitizeBodyRow 共用實作 — 對 row 每個
// cell 套 SanitizeCellForWrite,回傳 fresh slice(不修改 input)。
func sanitizeRowCells(row []string) []string {
	if row == nil {
		return nil
	}
	out := make([]string, len(row))
	for i, c := range row {
		out[i] = SanitizeCellForWrite(c)
	}
	return out
}

// SanitizeAllRows returns a fresh [][]string with SanitizeCellForWrite applied
// to every cell in every row. Used as the single chokepoint in csv_handler.WriteCSV
// to catch body-row formula injection (e.g. user-controllable PhaseName labels
// from config.json flowing into row[0] via csv_converter.buildRow). Idempotent
// — already-escaped cells with leading "'" stay unchanged on a second pass, so
// converter-level header sanitize remains safe as defense-in-depth.
//
// # Nil-row handling (P1-A6-4 + P1-15)
//
// If a row in `rows` is nil, it is **skipped + logged at Warn level**.
// encoding/csv.Writer panics when handed a nil row; propagating nils through this
// chokepoint was a latent NPE waiting for the wrong call site to hit it.
// 過去 fix 改為 silent skip,但 silent 是另一種坑 — caller 若計算
// `len(out) == len(in)` 隱含「index 對應」,silent drop 不會被 dev 發現,直到
// downstream data correlation 出錯。加 logger.Warn 把每筆 drop 記錄起來,
// 讓 ops / dev 能在 log 看到「哪些 row 被 drop 了」的 audit trail。
//
// Logger 是 process-default logger (logging.GetLogger()) — sanitize 屬 chokepoint
// 工具,不暴露 logger 注入點以免擾亂 hot-path 介面。Warn level 在 default Info
// production config 下會被輸出。
//
// # ⚠ Index invariant breakage (L19)
//
// **Output length may be less than `len(rows)`** — input index `i` does **not**
// necessarily map to output index `i`. Any caller that relies on positional
// alignment between the input row slice and the output row slice (for example
// to look up a parallel metadata slice by index) MUST validate / strip nil rows
// upstream rather than relying on this helper to preserve indexing.
//
// Alternative considered (rejected): replace nil rows with `[]string{}` to
// preserve indexing. Rejected because that produces zero-cell rows in the CSV
// output (an empty line), which is **less safe** than dropping them: empty CSV
// rows are still legitimate `encoding/csv` records that downstream parsers may
// interpret as "row with one empty field" — exactly the silent-miscalculation
// vector we want to avoid in EMG analysis pipelines. Skipping is the safer
// default; callers needing strict index-preservation have an upstream choice
// (filter / error / placeholder) that's contextually correct, not one we can
// guess here.
//
// See [TestSanitizeAllRows_NilRowInSlice] and [TestSanitizeAllRows_OnlyNilRows]
// for the locked-in behavior.
func SanitizeAllRows(rows [][]string) [][]string {
	if rows == nil {
		return nil
	}
	out := make([][]string, 0, len(rows))
	var logger *logging.Logger // lazy init,避免每次無 nil row 也呼叫 GetLogger
	for i, row := range rows {
		if row == nil {
			// 把 nil row drop 記錄到 Warn log,給 ops/dev audit trail。
			// 過去 silent skip 容易被 caller 用 len(out) == len(in) 隱含假設踩到 —
			// 計算後資料 row 數對不上沒人知道哪掉了。lazy init logger 避免無 nil
			// 時白做 GetLogger 呼叫(穩態下 99% rows 都非 nil)。
			if logger == nil {
				logger = logging.GetLogger("csvutil_sanitize")
			}
			logger.Warn("nil row dropped at index", map[string]any{
				"index": i,
				"total": len(rows),
			})
			continue
		}
		// 這裡逐 row 處理,header / body 混在一起(SanitizeAllRows 是 csv_handler
		// 整檔 sanitize chokepoint,首 row 是 header,其餘是 body)。SanitizeHeaderRow
		// 與 SanitizeBodyRow 行為等價(per-cell sanitize),用 SanitizeBodyRow 是因為
		// 此處呼叫 frequency 上 body row 居多 — caller 直觀。
		out = append(out, SanitizeBodyRow(row))
	}
	return out
}

// UnsanitizeCell reverses SanitizeCellForWrite by stripping the leading `'`
// prefix when — and only when — the unsanitized remainder would itself be
// re-flagged as dangerous by SanitizeCellForWrite. This is the inverse used by
// callers that read back a sanitized CSV and want to recover the original cell
// representation (e.g. for re-display in a UI or for comparison with a source
// of truth).
//
// Ambiguity: SanitizeCellForWrite is NOT a perfectly invertible
// operation. A user-supplied cell `'hello` (intentional leading apostrophe,
// not a sanitize artifact) is indistinguishable from a sanitized cell whose
// original was `hello`. To avoid corrupting legitimate user data, UnsanitizeCell
// only strips the `'` when the remainder would have triggered sanitize in the
// first place — so `'=BAD()` reverts to `=BAD()`, but `'hello` stays `'hello`.
//
// This is intentionally conservative: any caller that needs a *guaranteed*
// round-trip must transport the pre-sanitize value out-of-band (e.g. keep
// the original in memory) rather than relying on this helper.
func UnsanitizeCell(cell string) string {
	if cell == "" || !strings.HasPrefix(cell, "'") {
		return cell
	}

	remainder := cell[len("'"):]
	if remainder == "" {
		// Lone "'" — can't be a sanitize artifact (sanitize never produces a
		// bare apostrophe). Treat as user data.
		return cell
	}

	// Only strip if the remainder would re-trigger sanitize: that proves the
	// `'` was the sanitize prefix and not part of the original user input.
	if SanitizeCellForWrite(remainder) != remainder {
		return remainder
	}

	return cell
}

// formulaStarters 偵測順序：先在 `trimmed`(unicode space + invisible leading 後)view
// 上比 base starter（`=` / `＝` / `@`）— 涵蓋 NBSP / 全形空格 / 零寬空格等 invisible
// leading + formula 的組合。同時加上 `\t=` / `\r=` / `\n=` / `\t@` / `\r@` / `\n@`
// 等 ASCII control leading 的字面 prefix 變體作為 defense-in-depth:
//
//   - 即使 `isInvisibleLeading` 邏輯被未來 refactor 收窄 (例如改為只看
//     unicode.White_Space 屬性, 而漏掉 `unicode.IsSpace` 涵蓋的 ASCII control
//     `\t` `\r` `\n`), 這些字面 prefix 仍能在 raw `cell` view 上命中。
//   - 雖然 +/- 形式的 control-leading 變體(`\t+` 等)由「ParseFloat 不過 → escape」
//     規則間接擋下, 列出 starter literal 讓行為更直白可審。
//
// "＝" U+FF1D 為全形等號(部分 Excel locale 會 normalize 後當 = 公式起手),
// 直接列在 starter set 比依賴下游 normalize 更可靠。
//
//nolint:gochecknoglobals // logical immutable rule table; documented above SanitizeCellForWrite
var formulaStarters = []string{
	// Base (in trimmed view)
	"=", "＝", "@",
	// defense-in-depth: ASCII control leading + formula starter 字面 prefix
	"\t=", "\r=", "\n=",
	"\t＝", "\r＝", "\n＝",
	"\t@", "\r@", "\n@",
	"\t+", "\r+", "\n+",
	"\t-", "\r-", "\n-",
}

// isInvisibleLeading reports whether a rune should be trimmed from the leading
// edge before formula-starter detection. unicode.IsSpace covers the standard
// White_Space property (含 ASCII 空白、NBSP U+00A0、ideographic space U+3000
// 等); 另外手動加入 zero-width 與 bidi-control 字元，這些不在 White_Space
// property 內但會被 spreadsheet 視覺忽略，可用來把 formula 藏在 cell 開頭。
func isInvisibleLeading(r rune) bool {
	if unicode.IsSpace(r) {
		return true
	}
	switch r {
	case 0x200B, 0x200C, 0x200D, 0xFEFF:
		// ZERO WIDTH SPACE / NON-JOINER / JOINER / BOM (U+FEFF)
		return true
	}
	// bidi controls: U+2066-U+2069 (LRI/RLI/FSI/PDI), U+202A-U+202E (LRE/RLE/PDF/LRO/RLO)
	if (r >= 0x2066 && r <= 0x2069) || (r >= 0x202A && r <= 0x202E) {
		return true
	}
	return false
}
