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
		return "", fmt.Errorf("evalSymlinks: 路徑遞迴層數超過上限 (%d): %s", maxDepth, path)
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
