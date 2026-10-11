package filename

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Sanitize 把可能造成路徑穿越或檔名衝突的字元替換為底線。
//
// 補強：除了原本字元替換外，加：
//   - 空字串 fallback 為 "untitled"
//   - 超長 (> 200 chars，預留 prefix/suffix 空間) 截斷
//   - Windows reserved name (CON / PRN / COMx / LPTx) 的點分段加 "_safe" 後綴
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

	// Windows reserved name → 在該點分段後補 _safe 避開。
	//
	// 與 ValidateName 用同一條規則:逐個點分段比對(ADR-0054)。只看 stem(第一段)
	// 會讓 "a.NUL.b" 原樣通過 Sanitize、卻被 ValidateName 拒絕 —— SubjectOutputName
	// 的產物必須過得了 ValidateName。逐段也自然涵蓋多副檔名與 leading dot
	// (Windows NT path parser 比對 reserved name 前會 strip leading dot):
	// "CON.csv"    → "CON_safe.csv"
	// "CON.tar.gz" → "CON_safe.tar.gz"
	// ".CON.csv"   → ".CON_safe.csv"(保留 dot prefix,僅插入 _safe)
	// "a.NUL.b"    → "a.NUL_safe.b"
	//
	// ValidateName 先 TrimSpace 再切段,這裡也只對去掉頭尾空白後的部分切段
	// (否則首段「全形空白 U+3000 + CON」不命中、ValidateName 卻命中);頭尾空白
	// 原樣留在輸出,不改變非保留名的結果。
	const reservedSuffix = "_safe"
	lead := len(cleaned) - len(strings.TrimLeftFunc(cleaned, unicode.IsSpace))
	core := strings.TrimRightFunc(cleaned[lead:], unicode.IsSpace)
	segments := strings.Split(core, ".")
	for i, segment := range segments {
		if isReservedName(segment) {
			segments[i] = segment + reservedSuffix
		}
	}
	cleaned = cleaned[:lead] + strings.Join(segments, ".") + cleaned[lead+len(core):]

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
