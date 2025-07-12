package security

import (
	"fmt"
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

	// Check for suspicious patterns in the original path before cleaning
	if strings.Contains(path, "..") {
		return fmt.Errorf("路徑包含非法字符 '..'")
	}

	// Clean the path to resolve any ./ or ../ components
	cleanPath := filepath.Clean(path)

	// Get absolute path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return fmt.Errorf("無法解析路徑 '%s': %w", path, err)
	}

	// Verify the path is within allowed base paths
	for _, basePath := range pv.allowedBasePaths {
		// Get absolute base path
		absBasePath, err := filepath.Abs(basePath)
		if err != nil {
			continue
		}

		// Use filepath.Rel to check if the target is within the base
		rel, err := filepath.Rel(absBasePath, absPath)
		if err != nil {
			continue
		}

		// If the relative path doesn't start with ".." and doesn't start with "/",
		// it's within the base path (including subdirectories)
		if !strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, "/") {
			return nil
		}
	}

	return fmt.Errorf("路徑 '%s' 超出允許範圍", path)
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
	// Remove null bytes and other control characters
	sanitized := strings.ReplaceAll(path, "\x00", "")
	sanitized = strings.ReplaceAll(sanitized, "\r", "")
	sanitized = strings.ReplaceAll(sanitized, "\n", "")

	// Clean the path
	return filepath.Clean(sanitized)
}

// GetSafePath returns a safe path within the allowed directories
func (pv *PathValidator) GetSafePath(basePath, filename string) (string, error) {
	if err := pv.ValidateDirectoryPath(basePath); err != nil {
		return "", fmt.Errorf("基礎路徑無效: %w", err)
	}

	// Sanitize filename
	safeFilename := pv.SanitizePath(filename)

	// Join paths safely
	fullPath := filepath.Join(basePath, safeFilename)

	// Validate the final path
	if err := pv.ValidateFilePath(fullPath); err != nil {
		return "", fmt.Errorf("最終路徑無效: %w", err)
	}

	return fullPath, nil
}
