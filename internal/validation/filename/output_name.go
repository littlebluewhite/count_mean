package filename

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// SubjectOutputName 把使用者可控的 raw subject 與 caller 的 suffix 組成單一
// path segment 的檔名 stem(不含副檔名),定義為 Sanitize(subject) + "_" + suffix。
//
// 收 raw subject、內部強制 Sanitize:caller 不再持有 safeSubject local,Subject
// 只透過此 primitive 碰到檔名,拿不到「跳過 sanitize」的選項(ADR-0019 Why #3)。
// 空 / 全 unsafe 的 subject 走 Sanitize 既有的 "untitled" fallback,恆回非空單一
// segment。回 stem(非含 ext):caller 自接 ".csv" / ".png"(ADR-0019)。
func SubjectOutputName(subject, suffix string) string {
	return Sanitize(subject) + "_" + suffix
}

// SubjectOutputKey 回 subject 在磁碟上的輸出檔名身分:NFC(ToLower(Sanitize(subject)))。
// 兩個 subject 的 key 相同,SubjectOutputName 產出的檔名在 macOS / Windows 上就是同一個檔,
// 後寫的會覆蓋先寫的。manifest.LoadManifests 用它擋撞名(ADR-0056)。
//
//   - Sanitize:`A/B` 與 `A:B` 都成 `A_B`;空 subject 都成 `untitled`。
//   - ToLower:macOS / Windows 預設 case-insensitive,`SF8` 與 `sf8` 是同一檔。
//   - NFC:APFS / HFS+ 視 NFC 的 `café`(U+00E9)與 NFD 的 `café`(e + U+0301)為同一檔。
func SubjectOutputKey(subject string) string {
	return norm.NFC.String(strings.ToLower(Sanitize(subject)))
}
