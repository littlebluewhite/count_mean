// Package filename provides filename validation functionality.
package filename

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"count_mean/internal/errors"
	"count_mean/internal/validation/patterns"
)

// Validator provides filename validation functionality.
type Validator struct {
	detector          *patterns.InjectionDetectorImpl
	allowedExtensions []string
}

// NewValidator creates a new filename validator with default extensions.
func NewValidator() *Validator {
	return &Validator{
		detector:          patterns.NewInjectionDetector(),
		allowedExtensions: []string{".csv"},
	}
}

// WithAllowedExtensions sets the allowed file extensions.
func (v *Validator) WithAllowedExtensions(extensions []string) {
	v.allowedExtensions = extensions
}

// GetAllowedExtensions returns the current allowed extensions.
func (v *Validator) GetAllowedExtensions() []string {
	return v.allowedExtensions
}

// ValidateFilename validates a filename for safety and correctness.
func (v *Validator) ValidateFilename(filename string) error {
	if filename == "" {
		return errors.NewValidationError("filename", filename, "檔案名稱不能為空")
	}

	filename = strings.TrimSpace(filename)

	// Check for null bytes and control characters
	if err := v.checkControlChars(filename); err != nil {
		return err
	}

	// Check for dangerous characters
	if detected, char := v.detector.DetectDangerousChars(filename); detected {
		return errors.NewValidationError("filename", filename,
			fmt.Sprintf("檔案名稱包含非法字符: %s", char))
	}

	// Check for reserved names (Windows)
	baseName := strings.ToUpper(strings.TrimSuffix(filename, filepath.Ext(filename)))
	if v.detector.IsReservedName(baseName) {
		return errors.NewValidationError("filename", filename,
			fmt.Sprintf("檔案名稱不能使用保留字: %s", baseName))
	}

	// Check length (max 255 characters, common filesystem limit)
	if len(filename) > 255 { //nolint:mnd // 255 is the standard max filename length
		return errors.NewValidationError("filename", filename, "檔案名稱過長 (最大 255 字符)")
	}

	// Check extension
	return v.checkExtension(filename)
}

// checkControlChars checks for null bytes and control characters.
func (*Validator) checkControlChars(filename string) error {
	for _, r := range filename {
		if r == 0 || (unicode.IsControl(r) && r != '\t') {
			return errors.NewValidationError("filename", filename, "檔案名稱包含非法字符")
		}
	}

	return nil
}

// checkExtension validates the file extension.
func (v *Validator) checkExtension(filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return nil
	}

	for _, allowedExt := range v.allowedExtensions {
		if ext == allowedExt {
			return nil
		}
	}

	return errors.NewValidationError("filename", filename,
		fmt.Sprintf("不支援的檔案副檔名: %s", ext))
}
