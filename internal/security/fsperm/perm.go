// Package fsperm centralizes filesystem permission and OpenFile flag constants
// for application-created files (CSV, HTML, log, JSON, PNG, translations).
//
// Carved out of the original util/ catch-all so callers depend on the narrow
// API they need — symmetric with util/csvutil/. Build-tag-separated
// flags_unix.go / flags_windows.go cover platform-specific O_NOFOLLOW behavior.
package fsperm

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FilePerm 為 0o600 — 應用程式建立檔案的標準權限（僅 owner 可讀寫）。
// 此前同一常數以 filePermission / csvFilePermission / chartFileMode / logFilePermission
// 等不同名稱散落 8+ 檔案；統一在這便於日後資安政策一致調整。
const FilePerm os.FileMode = 0o600

// DirPerm 為 0o750 — 應用程式建立目錄的標準權限（owner 完整存取，group 唯讀，others 無）。
const DirPerm os.FileMode = 0o750

// EvalSymlinksWithFallback 解析 path 上所有 symlink,回傳完全解析後的絕對路徑。
// 若 path 本身不存在(典型 case:caller 傳「未來寫入路徑的 prefix + dummy child」
// 做 sensitive-prefix 驗證,child 尚未建立),沿 parent 逐層往上找到 nearest existing
// dir,EvalSymlinks 解析後再接回原本 non-existent 的 child suffix。
//
// 本函式統一原先散在 security/lenient_path.go (evalSymlinksLenient)、
// security/pathvalidator.go (resolveSymlinksWithFallback) 與本套件 validated_open.go
// (evalSymlinksWithFallback) 的三份 byte-identical 實作 (ADR-0028);三具差異僅 depth
// cap 與空字串契約 — 收斂為一個 maxDepth 參數 + 統一 error-on-empty。
//
//	maxDepth <= 0:unbounded(仍由 root 終止:filepath.Dir(p) == p)
//	maxDepth >  0:parent-walk 超過 maxDepth 層即 fail-closed error
//	              (對抗性 manifest 路徑的 defense-in-depth;lenient path 傳 8)
//
// 空字串回 error(fail-closed)。抵達 root 仍無法解析回 error — 正常系統不會發生
// (除非檔系統爛掉),保守 fail-closed。
//
//nolint:err113 // dynamic errors for caller-facing output
func EvalSymlinksWithFallback(path string, maxDepth int) (string, error) {
	if path == "" {
		return "", errors.New("empty path")
	}
	return evalSymlinksWithFallbackDepth(path, maxDepth, maxDepth, maxDepth > 0)
}

// evalSymlinksWithFallbackDepth 是 EvalSymlinksWithFallback 的 inner helper:bounded 時
// depth 逐層遞減、抵 0 即 fail-closed error;maxDepth 僅供超限訊息嵌入 cap 值。
//
//nolint:err113 // dynamic errors for caller-facing output
func evalSymlinksWithFallbackDepth(path string, depth, maxDepth int, bounded bool) (string, error) {
	if bounded && depth <= 0 {
		// 此層 path 必為原路徑的上層目錄 → 整條脫敏,末段可能是病患目錄名。
		return "", fmt.Errorf("evalSymlinks: 路徑遞迴層數超過上限 (%d): %s", maxDepth, redactDir(path))
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", fmt.Errorf("路徑根目錄無法解析: %s", path)
	}
	resolvedParent, err := evalSymlinksWithFallbackDepth(parent, depth-1, maxDepth, bounded)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

// IsWithin 回報 target 是否落在 base 之內(含 target 等於 base)。**純詞法比對**:不碰
// 檔系統、不解析 symlink、不做 Abs,兩者需同為絕對或同為相對,否則(filepath.Rel 報錯)
// 回 false。要擋 symlink 逸出請用 IsWithinResolved。
//
// 判定以 filepath.Rel(base, target) 的結果為準,逐 element 而非字串前綴:
//   - rel == "." (target == base) → within
//   - rel 為 ".." 或以 ".."+Separator 開頭 → 不在內(`base/../x`、`base2` 皆在此列)
//   - rel 為絕對路徑(Windows 跨 volume / UNC 的 defense-in-depth)→ 不在內
//   - `base/..foo`、`base/foo..bar` 的第一個 element 不是 ".." → within
//
// 大小寫與 Windows drive/volume 處理完全沿用 filepath.Rel 在執行 OS 上的行為。
func IsWithin(base, target string) bool {
	// filepath.Rel 把 "" 當 "."(→ true);空路徑一律 fail-closed。
	if base == "" || target == "" {
		return false
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}

// IsWithinResolved 是 IsWithin 的 symlink-aware 版:base 與 target 先各自絕對化,再以
// EvalSymlinksWithFallback(ADR-0028,不限深度)解析,於解析後的路徑比對。不存在的
// 尾段(「即將建立」的 dir / 檔)沿 parent 解析後接回,故 base 或 target 尚未存在也可判定。
//
// 回傳解析後的 base,讓需要它的 caller(如 atomic write 的 dirfd anchor)不必再解析一次。
// 任一路徑為空、無法絕對化或解析失敗 → ("", false)(fail-closed);ok 為 false 時
// resolvedBase 一律為 ""。
//
// target 含 ".." element 一律 fail-closed:filepath.Abs 會先詞法 Clean,把
// `base/link/../x` 折成 `base/x`,但 kernel 是先跟 link 再退上一層(實際落在 link 目標的
// 上層),詞法折疊後再解析會誤判在內。呼叫端應傳已 Clean / 已解析的路徑。
func IsWithinResolved(base, target string) (resolvedBase string, ok bool) {
	if base == "" || target == "" {
		return "", false
	}
	if hasDotDotElement(target) {
		return "", false
	}
	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", false
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", false
	}
	resolvedBase, err = EvalSymlinksWithFallback(absBase, 0)
	if err != nil {
		return "", false
	}
	resolvedTarget, err := EvalSymlinksWithFallback(absTarget, 0)
	if err != nil {
		return "", false
	}
	if !IsWithin(resolvedBase, resolvedTarget) {
		return "", false
	}
	return resolvedBase, true
}

// hasDotDotElement 回報 path 是否含 ".." element(以 / 與 filepath.Separator 切分,
// 不是子字串比對:`..foo`、`foo..bar` 不算)。
func hasDotDotElement(path string) bool {
	return strings.Contains("/"+filepath.ToSlash(path)+"/", "/../")
}
