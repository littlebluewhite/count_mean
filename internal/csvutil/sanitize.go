package csvutil

import (
	"strconv"
	"strings"
	"unicode"
)

// SanitizeCellForWrite escapes a single CSV cell that will be written to disk
// so that downstream spreadsheet readers (Excel / LibreOffice / Numbers) do not
// interpret attacker-controlled headers or values as formulas.
//
// Detection runs against the cell with leading Unicode whitespace trimmed
// (unicode.IsSpace, which covers NBSP U+00A0 / zero-width spaces / 全形空格
// 等 ASCII TrimLeft 漏掉的 vector), so attackers can't smuggle formulas behind
// non-ASCII whitespace (Wave 7 security review extension of Wave 6 codex C-2).
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
func SanitizeCellForWrite(cell string) string {
	if cell == "" {
		return cell
	}

	trimmed := strings.TrimLeftFunc(cell, isInvisibleLeading)
	if trimmed == "" {
		return cell
	}

	for _, starter := range formulaStarters {
		if strings.HasPrefix(trimmed, starter) {
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
func SanitizeHeaderRow(row []string) []string {
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
func SanitizeAllRows(rows [][]string) [][]string {
	if rows == nil {
		return nil
	}
	out := make([][]string, len(rows))
	for i, row := range rows {
		out[i] = SanitizeHeaderRow(row)
	}
	return out
}

// 不再列 `\t=` / `\r=` / `\n=` 與其他 Unicode 空白前綴 — 偵測是在 TrimLeftFunc
// (isInvisibleLeading) 後的 view 上跑，這些場景已自動轉成裸 starter 命中規則。
// "＝" U+FF1D 為全形等號（部分 Excel locale 會 normalize 後當 = 公式起手），
// 直接列在 starter set 比依賴下游 normalize 更可靠。
//
//nolint:gochecknoglobals // logical immutable rule table; documented above SanitizeCellForWrite
var formulaStarters = []string{"=", "＝", "@"}

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
