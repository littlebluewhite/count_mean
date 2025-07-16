package security

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// PathValidator provides secure path validation functionality
type PathValidator struct {
	allowedBasePaths []string
}

// NewPathValidator creates a new path validator with allowed base paths
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

// ValidateFilePath validates that a file path is within allowed directories
func (pv *PathValidator) ValidateFilePath(path string) error {
	if path == "" {
		return fmt.Errorf("路徑不能為空")
	}

	// URL 解碼以防止編碼繞過攻擊
	decodedPath, err := url.QueryUnescape(path)
	if err != nil {
		return fmt.Errorf("路徑解碼失敗: %w", err)
	}

	// 檢查多種路徑遍歷模式，包含編碼變體
	suspiciousPatterns := []string{
		"..",     // 標準路徑遍歷
		"..\\",   // Windows 風格
		"../",    // Unix 風格
		"..%2F",  // URL 編碼的 ../
		"..%5C",  // URL 編碼的 ..\
		"%2E%2E", // URL 編碼的 ..
		"%2e%2e", // 小寫 URL 編碼
		"\\..\\", // 反斜線變體
		"/..",    // 絕對路徑變體
		"...//",  // 三點變體
		"....//", // 四點變體
	}

	// 檢查原始路徑和解碼後的路徑
	pathsToCheck := []string{path, decodedPath}
	for _, checkPath := range pathsToCheck {
		checkPathLower := strings.ToLower(checkPath)
		for _, pattern := range suspiciousPatterns {
			if strings.Contains(checkPathLower, strings.ToLower(pattern)) {
				return fmt.Errorf("路徑包含可疑的遍歷模式: %s", pattern)
			}
		}
	}

	// 額外檢查：防止多層編碼攻擊
	doubleDecoded, err := url.QueryUnescape(decodedPath)
	if err == nil && doubleDecoded != decodedPath {
		// 發現雙重編碼，再次檢查
		for _, pattern := range suspiciousPatterns {
			if strings.Contains(strings.ToLower(doubleDecoded), strings.ToLower(pattern)) {
				return fmt.Errorf("路徑包含雙重編碼的遍歷攻擊")
			}
		}
	}

	// 使用解碼後的路徑進行清理，防止編碼繞過
	cleanPath := filepath.Clean(decodedPath)

	// Get absolute path using decoded and cleaned path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return fmt.Errorf("無法解析路徑 '%s': %w", decodedPath, err)
	}

	// 最終檢查：確保絕對路徑不包含遍歷模式
	if strings.Contains(absPath, "..") {
		return fmt.Errorf("絕對路徑仍包含遍歷字符: %s", absPath)
	}

	// 靈活的白名單驗證機制 - 避免絕對路徑長度限制
	if len(pv.allowedBasePaths) == 0 {
		// 如果沒有設定允許路徑，只進行基本安全檢查
		return pv.performBasicSecurityChecks(absPath)
	}

	// 檢查路徑是否在允許的基礎路徑內
	for _, basePath := range pv.allowedBasePaths {
		if pv.isPathWithinBase(absPath, basePath) {
			return nil
		}
	}

	return fmt.Errorf("路徑 '%s' 超出允許範圍", decodedPath)
}

// ValidateDirectoryPath validates that a directory path is within allowed directories
func (pv *PathValidator) ValidateDirectoryPath(path string) error {
	return pv.ValidateFilePath(path)
}

// IsCSVFile checks if the file has a .csv extension
func (pv *PathValidator) IsCSVFile(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".csv"
}

// SanitizePath sanitizes a file path by removing dangerous characters
func (pv *PathValidator) SanitizePath(path string) string {
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
			(r >= 0x00A0) || // 非 ASCII 但可見的字符
			r == '/' || r == '\\' || r == '.' || r == '-' || r == '_' { // 路徑相關的特殊字符
			result.WriteRune(r)
		}
		// 忽略控制字符
	}

	// 最終清理路徑
	finalPath := filepath.Clean(result.String())

	// 額外安全檢查：移除任何剩餘的路徑遍歷模式
	finalPath = strings.ReplaceAll(finalPath, "..", "")

	return finalPath
}

// GetSafePath returns a safe path within the allowed directories
func (pv *PathValidator) GetSafePath(basePath, filename string) (string, error) {
	if err := pv.ValidateDirectoryPath(basePath); err != nil {
		return "", fmt.Errorf("基礎路徑無效: %w", err)
	}

	// 在清理之前檢查文件名是否包含路徑遍歷攻擊
	if strings.Contains(filename, "..") {
		return "", fmt.Errorf("文件名包含路徑遍歷字符: %s", filename)
	}

	// Sanitize filename
	safeFilename := pv.SanitizePath(filename)

	// 檢查清理後的文件名是否為空或只包含無效字符
	if safeFilename == "" || safeFilename == "." {
		return "", fmt.Errorf("文件名無效或被清理後為空: %s", filename)
	}

	// Join paths safely
	fullPath := filepath.Join(basePath, safeFilename)

	// Validate the final path
	if err := pv.ValidateFilePath(fullPath); err != nil {
		return "", fmt.Errorf("最終路徑無效: %w", err)
	}

	return fullPath, nil
}

// isPathWithinBase 檢查目標路徑是否在基礎路徑內，支援長絕對路徑
func (pv *PathValidator) isPathWithinBase(targetPath, basePath string) bool {
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

// performBasicSecurityChecks 執行基本安全檢查，適用於無白名單限制的情況
func (pv *PathValidator) performBasicSecurityChecks(absPath string) error {
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
			return fmt.Errorf("路徑指向系統敏感目錄: %s", pattern)
		}
	}

	// 檢查路徑長度（防止過長路徑攻擊）
	maxPathLength := 4096 // 合理的路徑長度限制
	if len(absPath) > maxPathLength {
		return fmt.Errorf("路徑長度超過限制 (%d 字符): %d", maxPathLength, len(absPath))
	}

	// 檢查文件名長度
	filename := filepath.Base(absPath)
	maxFilenameLength := 255 // 大多數文件系統的限制
	if len(filename) > maxFilenameLength {
		return fmt.Errorf("文件名長度超過限制 (%d 字符): %d", maxFilenameLength, len(filename))
	}

	return nil
}

// SetAllowedBasePaths 動態設置允許的基礎路徑（支援長路徑）
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
	pv.allowedBasePaths = absPaths
}

// GetAllowedBasePaths 獲取當前允許的基礎路徑
func (pv *PathValidator) GetAllowedBasePaths() []string {
	// 返回副本以防止外部修改
	result := make([]string, len(pv.allowedBasePaths))
	copy(result, pv.allowedBasePaths)
	return result
}
