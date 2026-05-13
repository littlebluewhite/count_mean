// Package fsperm centralizes filesystem permission and OpenFile flag constants
// for application-created files (CSV, HTML, log, JSON, PNG, translations).
//
// Carved out of the original util/ catch-all so callers depend on the narrow
// API they need — symmetric with util/csvutil/. Build-tag-separated
// flags_unix.go / flags_windows.go cover platform-specific O_NOFOLLOW behavior.
package fsperm

import "os"

// FilePerm 為 0o600 — 應用程式建立檔案的標準權限（僅 owner 可讀寫）。
// 此前同一常數以 filePermission / csvFilePermission / chartFileMode / logFilePermission
// 等不同名稱散落 8+ 檔案；統一在這便於日後資安政策一致調整。
const FilePerm os.FileMode = 0o600

// DirPerm 為 0o750 — 應用程式建立目錄的標準權限（owner 完整存取，group 唯讀，others 無）。
const DirPerm os.FileMode = 0o750
