// Package security provides secure path validation functionality
// to prevent path traversal attacks and ensure file operations
// are restricted to allowed directories.
package security

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
)

// Path validation error definitions.
var (
	ErrPathTraversal      = errors.New("路徑包含可疑的遍歷模式")
	ErrPathTraversalAbs   = errors.New("絕對路徑仍包含遍歷字符")
	ErrPathOutOfScope     = errors.New("路徑超出允許範圍")
	ErrFilenameTraversal  = errors.New("文件名包含路徑遍歷字符")
	ErrFilenameInvalid    = errors.New("文件名無效或被清理後為空")
	ErrSensitiveDirectory = errors.New("路徑指向系統敏感目錄")
	ErrPathTooLong        = errors.New("路徑長度超過限制")
	ErrFilenameTooLong    = errors.New("文件名長度超過限制")
)

// Path length constants.
const (
	maxPathLength     = 4096
	maxFilenameLength = 255
	nonASCIIVisible   = 0x00A0 // Non-ASCII but visible characters start here
)

// PathValidator provides secure path validation functionality.
// The allowed base paths are protected by an RWMutex so that callers may
// safely invoke SetAllowedBasePaths from one goroutine while another reads
// the list via ValidateFilePath or GetAllowedBasePaths.
type PathValidator struct {
	mu               sync.RWMutex
	allowedBasePaths []string
}

// NewPathValidator creates a new path validator with allowed base paths.
func NewPathValidator(allowedBasePaths []string) *PathValidator {
	// Convert all base paths to absolute paths
	absPaths := make([]string, len(allowedBasePaths))

	for i, path := range allowedBasePaths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			// If we can't get absolute path, use the original
			absPath = path
		}

		absPaths[i] = filepath.Clean(absPath)
	}

	return &PathValidator{
		allowedBasePaths: absPaths,
	}
}

// ValidateFilePath validates that a file path is within allowed directories.
//
// 用於受控的內部讀寫路徑 (InputDir / OutputDir / OperateDir)。對於使用者選的
// 任意檔（GUI file dialog 回來的絕對路徑），改用 ValidateExternalPath，否則
// 路徑落在 allowedBasePaths 外會被 reject。
func (pv *PathValidator) ValidateFilePath(path string) error {
	if path == "" {
		return nil
	}

	absPath, decodedPath, err := pv.validatePathFormat(path)
	if err != nil {
		return err
	}

	// 取得允許路徑的快照，避免在驗證過程中與 SetAllowedBasePaths 競爭
	pv.mu.RLock()
	allowed := pv.allowedBasePaths
	pv.mu.RUnlock()

	// 靈活的白名單驗證機制 - 避免絕對路徑長度限制
	if len(allowed) == 0 {
		// 如果沒有設定允許路徑，只進行基本安全檢查
		return performBasicSecurityChecks(absPath)
	}

	// 檢查路徑是否在允許的基礎路徑內
	for _, basePath := range allowed {
		if isPathWithinBase(absPath, basePath) {
			return nil
		}
	}

	return fmt.Errorf("%w: %s", ErrPathOutOfScope, decodedPath)
}

// ValidateExternalPath validates an externally-selected path (e.g. from a GUI
// file dialog) without enforcing the allowed-base-paths whitelist. The same
// path-format prechecks as ValidateFilePath are applied (URL decode, traversal
// patterns, absolute resolution) followed by performBasicSecurityChecks which
// blocks system-sensitive prefixes (/etc, /root, C:\Windows, ...) and enforces
// path-length limits. Use this for GetCSVHeaders / chart preview / any reader
// where the user has explicitly chosen the file.
func (pv *PathValidator) ValidateExternalPath(path string) error {
	if path == "" {
		return nil
	}

	absPath, _, err := pv.validatePathFormat(path)
	if err != nil {
		return err
	}

	return performBasicSecurityChecks(absPath)
}

// validatePathFormat performs the URL-decode + traversal + absolute-resolution
// prechecks shared by ValidateFilePath and ValidateExternalPath. Returns the
// cleaned absolute path and the URL-decoded original (used for error messages).
//
// Traversal 檢查改為 element-based（filepath.Clean 後 split 比對 `..` element）
// 取代過去的 substring scan：substring `..` 會誤拒含雙點的合法檔名，例如
// `report..v2.csv`、`backup..2024.csv`、`/Users/foo..bar/data.csv`
// （Wave 6 review P2 — codex + professional/debugger/security 三個 agent 收斂）。
func (*PathValidator) validatePathFormat(path string) (absPath, decodedPath string, err error) {
	// URL 解碼 — loop until idempotent (cap 4 層深度) 以擋雙重編碼繞過：
	// 單層 decode 對 `..%252Fetc%252Fpasswd` 只變成 `..%2Fetc%2Fpasswd`，
	// HasTraversalElement 依 / 與 \ split 不會切到 `%2F`，整段被當成單一 element
	// `..%2Fetc...` 比對 `..` 失敗，攻擊 bypass。loop decode 直到不變、或設深度
	// 上限（防超長 encoded payload 形成 DoS 與無限解碼），然後拒絕仍含 % 的 path。
	decoded := path
	for i := 0; i < 4; i++ {
		next, decodeErr := url.QueryUnescape(decoded)
		if decodeErr != nil || next == decoded {
			break
		}
		decoded = next
	}
	// 若仍含 literal `%`，視為「無法完全 decode 的可疑 input」直接拒。合法檔名
	// 不會含 raw `%`（URL-encoded form 寫進 path）；同時 4 層 loop 上限避免
	// DoS。Wave 7 security review P3 — codex / security-specialist 收斂。
	if strings.Contains(decoded, "%") {
		return "", decoded, fmt.Errorf("%w: 路徑含 URL-encoded 殘留：%s", ErrPathTraversal, decoded)
	}

	// Pre-Clean element check：interior `..`（例如 `./input/../output/test.csv`）
	// 在 Clean 後會被解析消去，但這種跨目錄寫法本身就值得擋，因此在 Clean 之前
	// 先 split 比對 `..` element。filename 含字面雙點（`report..v2.csv`）的 path
	// element 不會被誤拒。
	if HasTraversalElement(decoded) {
		return "", decoded, fmt.Errorf("%w: %s", ErrPathTraversal, decoded)
	}

	cleanPath := filepath.Clean(decoded)

	abs, absErr := filepath.Abs(cleanPath)
	if absErr != nil {
		return "", decoded, fmt.Errorf("無法解析路徑 '%s': %w", decoded, absErr)
	}

	if HasTraversalElement(abs) {
		return "", decoded, fmt.Errorf("%w: %s", ErrPathTraversalAbs, abs)
	}

	return abs, decoded, nil
}

// HasTraversalElement reports whether any path element equals "..".
// Splits on both `/` and `\` so cross-platform paths receive a consistent check
// regardless of which separator the caller passed in.
//
// Exported so downstream packages (e.g. internal/io) can apply the same
// element-aware semantics instead of substring `..` checks that misclassify
// legitimate filenames like `report..v2.csv`.
func HasTraversalElement(path string) bool {
	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}

// filterTraversalElements rebuilds path stripping any path element that is
// literally `..`. Filenames that merely contain `..` as a substring (e.g.
// `report..v2.csv`) are preserved.
//
// Replaces the previous `strings.ReplaceAll(path, "..", "")` in SanitizePath,
// which silently mangled legitimate filenames into different paths — turning
// GetSafePath into a vector that could read or overwrite the wrong file once
// the mangled name happened to exist on disk (codex Wave 6 second-pass P2).
func filterTraversalElements(path string) string {
	if path == "" {
		return path
	}

	leading := ""
	switch {
	case strings.HasPrefix(path, "/"):
		leading = "/"
	case strings.HasPrefix(path, "\\"):
		leading = "\\"
	}

	parts := strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	filtered := parts[:0]
	for _, part := range parts {
		if part != ".." {
			filtered = append(filtered, part)
		}
	}

	if len(filtered) == 0 {
		return leading
	}
	return leading + strings.Join(filtered, string(filepath.Separator))
}

// ValidateDirectoryPath validates that a directory path is within allowed directories.
func (pv *PathValidator) ValidateDirectoryPath(path string) error {
	return pv.ValidateFilePath(path)
}

// IsCSVFile checks if the file has a .csv extension.
// This is a package-level function for use without a PathValidator instance.
func IsCSVFile(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".csv"
}

// IsCSVFile checks if the file has a .csv extension (method wrapper).
func (*PathValidator) IsCSVFile(path string) bool {
	return IsCSVFile(path)
}

// SanitizePath sanitizes a file path by removing dangerous characters.
// This is a package-level function for use without a PathValidator instance.
func SanitizePath(path string) string {
	if path == "" {
		return ""
	}

	// URL 解碼防止編碼繞過
	decodedPath, err := url.QueryUnescape(path)
	if err != nil {
		// 如果解碼失敗，使用原始路徑
		decodedPath = path
	}

	// 定義危險字符和模式
	dangerousChars := map[string]string{
		"\x00": "",   // Null bytes
		"\r":   "",   // Carriage return
		"\n":   "",   // Newline
		"\t":   "",   // Tab
		"\x0B": "",   // Vertical tab
		"\x0C": "",   // Form feed
		"\x1C": "",   // File separator
		"\x1D": "",   // Group separator
		"\x1E": "",   // Record separator
		"\x1F": "",   // Unit separator
		"../":  "",   // 路徑遍歷
		"..\\": "",   // Windows 路徑遍歷
		"./":   "",   // 當前目錄引用
		".\\":  "",   // Windows 當前目錄引用
		"//":   "/",  // 多重斜線規範化
		"\\\\": "\\", // 多重反斜線規範化
	}

	// 移除危險字符
	sanitized := decodedPath
	for dangerous, replacement := range dangerousChars {
		sanitized = strings.ReplaceAll(sanitized, dangerous, replacement)
	}

	// 移除 Unicode 控制字符 (U+0000 到 U+001F 和 U+007F 到 U+009F)
	var result strings.Builder

	for _, r := range sanitized {
		if (r >= 0x0020 && r <= 0x007E) || // ASCII 可見字符
			(r >= nonASCIIVisible) || // 非 ASCII 但可見的字符
			r == '/' || r == '\\' || r == '.' || r == '-' || r == '_' { // 路徑相關的特殊字符
			_, _ = result.WriteRune(r) // WriteRune on strings.Builder never fails
		}
	}
	// 最終清理路徑
	finalPath := filepath.Clean(result.String())

	// 額外安全檢查：以 element-based 過濾移除剩餘的 `..` element。
	// 不使用 strings.ReplaceAll(..., "..", "") 因為 substring 替換會把
	// `report..v2.csv` 改寫成 `reportv2.csv`，造成 validation 通過但實際
	// 讀寫到不同檔案的 silent misroute（codex Wave 6 second-pass P2）。
	finalPath = filterTraversalElements(finalPath)

	return finalPath
}

// SanitizePath sanitizes a file path (method wrapper).
func (*PathValidator) SanitizePath(path string) string {
	return SanitizePath(path)
}

// GetSafePath returns a safe path within the allowed directories.
func (pv *PathValidator) GetSafePath(basePath, filename string) (string, error) {
	if err := pv.ValidateDirectoryPath(basePath); err != nil {
		return "", fmt.Errorf("基礎路徑無效: %w", err)
	}

	// 在清理之前檢查文件名是否包含路徑遍歷攻擊；改為 element-based 比對，
	// 與 validatePathFormat 同步（substring `..` 會誤拒 `report..v2.csv` 等合法檔名）。
	// 先 URL-decode 才比對：攻擊者常用 `..%2F..%2F` 繞過原始字串 check。
	decodedFilename, decodeErr := url.QueryUnescape(filename)
	if decodeErr != nil {
		decodedFilename = filename
	}

	if HasTraversalElement(decodedFilename) {
		return "", fmt.Errorf("%w: %s", ErrFilenameTraversal, filename)
	}

	// Sanitize filename
	safeFilename := SanitizePath(filename)

	// 檢查清理後的文件名是否為空或只包含無效字符
	if safeFilename == "" || safeFilename == "." {
		return "", fmt.Errorf("%w: %s", ErrFilenameInvalid, filename)
	}

	// Join paths safely
	fullPath := filepath.Join(basePath, safeFilename)

	// Validate the final path
	if err := pv.ValidateFilePath(fullPath); err != nil {
		return "", fmt.Errorf("最終路徑無效: %w", err)
	}

	return fullPath, nil
}

// isPathWithinBase 檢查目標路徑是否在基礎路徑內，支援長絕對路徑.
func isPathWithinBase(targetPath, basePath string) bool {
	// 獲取基礎路徑的絕對路徑
	absBasePath, err := filepath.Abs(basePath)
	if err != nil {
		return false
	}

	// 標準化路徑分隔符
	absBasePath = filepath.Clean(absBasePath)
	targetPath = filepath.Clean(targetPath)

	// 使用 filepath.Rel 檢查相對關係
	rel, err := filepath.Rel(absBasePath, targetPath)
	if err != nil {
		return false
	}

	// 檢查相對路徑是否有效（不包含 .. 且不是絕對路徑）
	return !strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, string(filepath.Separator))
}

// performBasicSecurityChecks 執行基本安全檢查，適用於無白名單限制的情況.
func performBasicSecurityChecks(absPath string) error {
	// 檢查是否包含系統敏感路徑（跨平台）
	sensitivePatterns := []string{
		"/etc/",               // Unix 系統配置
		"/root/",              // Unix root 目錄
		"/proc/",              // Unix 進程文件系統
		"/sys/",               // Unix 系統文件系統
		"/dev/",               // Unix 設備文件
		"/boot/",              // Unix 啟動文件
		"C:\\Windows\\",       // Windows 系統目錄
		"C:\\System32\\",      // Windows 系統32
		"C:\\Program Files\\", // Windows 程式文件
		"\\Windows\\",         // 相對 Windows 路徑
		"\\System32\\",        // 相對 Windows 系統路徑
	}

	absPathLower := strings.ToLower(absPath)
	for _, pattern := range sensitivePatterns {
		if strings.Contains(absPathLower, strings.ToLower(pattern)) {
			return fmt.Errorf("%w: %s", ErrSensitiveDirectory, pattern)
		}
	}

	// 檢查路徑長度（防止過長路徑攻擊）
	if len(absPath) > maxPathLength {
		return fmt.Errorf("%w (%d 字符): %d", ErrPathTooLong, maxPathLength, len(absPath))
	}

	// 檢查文件名長度
	filename := filepath.Base(absPath)

	if len(filename) > maxFilenameLength {
		return fmt.Errorf("%w (%d 字符): %d", ErrFilenameTooLong, maxFilenameLength, len(filename))
	}

	return nil
}

// SetAllowedBasePaths 動態設置允許的基礎路徑（支援長路徑）.
// 寫鎖保護避免並行呼叫者覆蓋彼此設定的清單。
func (pv *PathValidator) SetAllowedBasePaths(paths []string) {
	absPaths := make([]string, 0, len(paths))

	for _, path := range paths {
		if path == "" {
			continue
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			// 如果無法獲取絕對路徑，使用清理後的原始路徑
			absPath = filepath.Clean(path)
		}

		absPaths = append(absPaths, absPath)
	}

	pv.mu.Lock()
	pv.allowedBasePaths = absPaths
	pv.mu.Unlock()
}

// GetAllowedBasePaths 獲取當前允許的基礎路徑.
func (pv *PathValidator) GetAllowedBasePaths() []string {
	pv.mu.RLock()
	defer pv.mu.RUnlock()

	// 返回副本以防止外部修改
	result := make([]string, len(pv.allowedBasePaths))
	copy(result, pv.allowedBasePaths)

	return result
}
