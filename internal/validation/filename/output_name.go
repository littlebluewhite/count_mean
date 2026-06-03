package filename

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
