package filename

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"count_mean/internal/validation/patterns"
)

// Sanitize 把可能造成路徑穿越或檔名衝突的字元替換為底線。
//
// 補強：除了原本字元替換外，加：
//   - 空字串 fallback 為 "untitled"
//   - 超長 (> 200 chars，預留 prefix/suffix 空間) 截斷
//   - Windows reserved name (CON / PRN / COMx / LPTx) 加 "_safe" 後綴
//
// 補強：在路徑分隔符替換之外，**移除**下列 rune 以防止視覺欺騙 /
// 終端機 escape / 檔系統 reject：
//   - ASCII control chars (除 `\t` 仍 strip — filename 不應含 tab)
//   - Unicode `Cc` (control, 含 U+0000-U+001F / U+007F-U+009F)
//   - Unicode `Cf` (format, 含 RTL override U+202E、bidi controls U+2066-U+2069、
//     ZWSP/ZWJ U+200B-U+200D、BOM U+FEFF 等)
//   - Unicode `Cs` (surrogate; UTF-8 內若出現代表 mis-encoded UTF-16)
//
// 直接 drop 而非替換為底線，因為這些 rune 沒有合理的「看得到的等價字元」，
// 替換成 `_` 會在檔名中製造無意義底線；drop 才是預期 UX。
func Sanitize(name string) string {
	replacements := map[rune]rune{
		'/':  '_',
		'\\': '_',
		':':  '_',
		'*':  '_',
		'?':  '_',
		'"':  '_',
		'<':  '_',
		'>':  '_',
		'|':  '_',
		' ':  '_',
	}

	result := make([]rune, 0, len(name))

	for _, ch := range name {
		if isUnsafeFilenameRune(ch) {
			// drop 控制 / format / surrogate / NUL — 不留底線佔位
			continue
		}
		if replacement, ok := replacements[ch]; ok {
			result = append(result, replacement)
		} else {
			result = append(result, ch)
		}
	}

	cleaned := string(result)
	if cleaned == "" {
		return "untitled"
	}

	// Windows reserved base name → 加 _safe 後綴避開。
	//
	// Windows reserved name 規則是「stem (first dot 前的 segment)」匹配,
	// 不是「整個 filename」匹配。"CON.csv" / "CON.tar.gz" / "PRN.log" 在 Windows
	// 上仍會被當成 reserved device file 而 reject (或更糟,寫入打開實體裝置)。
	// 過去用 `strings.ToUpper(cleaned)` 比對整段,只擋住 "CON" alone,
	// 把 "CON.csv" 放過 — 在 macOS/Linux 開發跑 PASS 但 Windows 部署炸開。
	// 改成 strings.SplitN 取 stem,涵蓋多副檔名 (.tar.gz) 情境。
	//
	// leading-dot bypass 加固。past stem 取法 `SplitN(".CON.csv", ".", 2)[0]`
	// 會回 "" — 空 stem 跑進 switch 永遠不命中任何 reserved name,讓 ".CON.csv" /
	// ".NUL.txt" 之類 leading-dot 變形直接逃過 reserved-name 守門。Windows NT
	// path parser 在比對 reserved name table 之前會 strip leading dot,所以
	// ".CON.csv" 在 Windows 上仍會被當成 device。修法:用 strings.TrimLeft 把
	// leading dot 先剝掉再取 stem 比對,保留原 cleaned 作為 splice 基準以保留
	// dot prefix 在輸出 (".CON.csv" → ".CON_safe.csv")。
	const reservedSuffix = "_safe"
	dotPrefixLen := len(cleaned) - len(strings.TrimLeft(cleaned, "."))
	afterDots := cleaned[dotPrefixLen:]
	stemAfterDots := strings.SplitN(afterDots, ".", 2)[0]
	if patterns.IsReservedName(stemAfterDots) {
		// 在 stem 末尾插入 _safe,保留原本副檔名 (含多副檔名) 與 leading dot:
		// "CON.csv"    → "CON_safe.csv"
		// "CON.tar.gz" → "CON_safe.tar.gz"
		// ".CON.csv"   → ".CON_safe.csv" (P2-C:保留 dot prefix,僅插入 _safe)
		// "..NUL.txt"  → "..NUL_safe.txt"
		stemEnd := dotPrefixLen + len(stemAfterDots)
		if rest := cleaned[stemEnd:]; rest != "" {
			cleaned = cleaned[:stemEnd] + reservedSuffix + rest
		} else {
			cleaned += reservedSuffix
		}
	}

	// 截斷過長 filename — 保留前 200 個 **byte**（預留 prefix / extension / index 空間）。
	//
	// 用 byte truncation 可能切到 multi-byte UTF-8 rune 中間,產生
	// invalid UTF-8 sequence (例如 0xE4 0xB8 0x80 = "一",切在 byte 1 後得到
	// 0xE4 lone 是 invalid)。Windows/macOS file API 對 invalid UTF-8 行為各異
	// (FAT32 直接 reject、APFS 容忍但 ls 顯示亂碼)。改成 rune-aware:
	// scan 到 maxLen 邊界時退回到最後一個 valid rune boundary,確保截斷結果仍是
	// 完整 UTF-8 sequence。
	//
	// 為什麼仍用 byte 限額 (200) 而非 rune 數: filesystem 規格 (例 ext4 NAME_MAX=255)
	// 是 byte 上限,不是 rune 數。中文 filename 每 rune 3 bytes,200 bytes ≈ 66 中文字,
	// 對使用者命名足夠。
	const maxLen = 200
	if len(cleaned) > maxLen {
		cleaned = truncateAtRuneBoundary(cleaned, maxLen)
	}

	return cleaned
}

// truncateAtRuneBoundary 把 s 截斷到 maxBytes 以內,且保證不會切到 UTF-8 multi-byte
// rune 中間。回傳的 string 長度 <= maxBytes 且必為 valid UTF-8。
//
// 演算法: 從 maxBytes 位置往前掃,找到第一個 utf8.RuneStart byte (高位 byte 結構為
// 0xxxxxxx (ASCII)、110xxxxx (2-byte leader)、1110xxxx (3-byte leader)、
// 11110xxx (4-byte leader);continuation byte 為 10xxxxxx 不會被 RuneStart 認可)。
// 用 DecodeRuneInString 邊掃邊累加 byte 長度,直到下一個 rune 會超過 maxBytes 即停。
func truncateAtRuneBoundary(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}

	// 遍歷 s 的 rune,累積 byte 長度。下一個 rune 加進來會超過 maxBytes 即停。
	totalBytes := 0
	for _, r := range s {
		runeBytes := utf8.RuneLen(r)
		// utf8.RuneLen 對 invalid rune 回 -1,把它當 1 byte 處理 (與 byte truncation 行為對齊)。
		if runeBytes < 0 {
			runeBytes = 1
		}
		if totalBytes+runeBytes > maxBytes {
			break
		}
		totalBytes += runeBytes
	}
	return s[:totalBytes]
}

// isUnsafeFilenameRune reports whether r should be dropped from a sanitized
// filename. 涵蓋 ASCII control / Unicode Cc(control) / Cf(format) / Cs(surrogate)。
//
// 為什麼用 unicode.In(r, Cf, Cs) 而非逐項列表：Cf 已涵蓋 U+202E (RTL OVERRIDE)、
// U+200B-U+200D (ZWSP/ZWJ)、U+2060 (WJ)、U+2066-U+2069 (bidi isolation)、
// U+FEFF (BOM) 等所有「視覺欺騙」vector，毋須維護白名單。Cc 涵蓋 U+0000-U+001F
// 以及 U+007F-U+009F，後者在歷史終端機上會被 interpret 為 escape，filename 不應
// 出現。Cs 為 UTF-16 surrogate，UTF-8 內出現即為 mis-encoded，全 drop。
func isUnsafeFilenameRune(r rune) bool {
	if r == 0 {
		return true
	}
	// unicode.IsControl 涵蓋 \p{Cc}（C0/C1 控制碼，含 \r \n \t \x07 NUL 等）。
	// 與 csvutil sanitize 不同的是，filename 也不容許 \t — tab 在多數檔系統呈現
	// 為灰色塊或空白，會干擾 listing；統一 drop。
	if unicode.IsControl(r) {
		return true
	}
	// \p{Cf}（format）：RTL override / ZWSP / BOM / bidi controls。
	// \p{Cs}（surrogate）：mis-encoded UTF-16。
	return unicode.In(r, unicode.Cf, unicode.Cs)
}
