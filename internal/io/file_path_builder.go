package io

import (
	"fmt"
	"path/filepath"
	"strings"

	"count_mean/internal/config"
	"count_mean/internal/security"
)

// FilePathBuilder handles path construction for CSV file operations.
type FilePathBuilder struct {
	config        *config.AppConfig
	pathValidator *security.PathValidator
}

// NewFilePathBuilder creates a new FilePathBuilder instance.
func NewFilePathBuilder(cfg *config.AppConfig, pv *security.PathValidator) *FilePathBuilder {
	return &FilePathBuilder{
		config:        cfg,
		pathValidator: pv,
	}
}

// BuildInputPath constructs a safe path within the input directory.
// If subDir is provided, it will be joined with the input directory.
func (b *FilePathBuilder) BuildInputPath(filename string, subDir ...string) (string, error) {
	safeFilename := ensureCSVExtension(filename)
	basePath := b.config.InputDir

	if len(subDir) > 0 && subDir[0] != "" {
		basePath = filepath.Join(basePath, subDir[0])
	}

	path, err := b.pathValidator.GetSafePath(basePath, safeFilename)
	if err != nil {
		return "", fmt.Errorf("無法構建輸入路徑: %w", err)
	}

	return path, nil
}

// BuildOutputPath constructs a safe path within the output directory.
// If subDir is provided, it will be joined with the output directory.
func (b *FilePathBuilder) BuildOutputPath(filename string, subDir ...string) (string, error) {
	safeFilename := ensureCSVExtension(filename)
	basePath := b.config.OutputDir

	if len(subDir) > 0 && subDir[0] != "" {
		basePath = filepath.Join(basePath, subDir[0])
	}

	path, err := b.pathValidator.GetSafePath(basePath, safeFilename)
	if err != nil {
		return "", fmt.Errorf("無法構建輸出路徑: %w", err)
	}

	return path, nil
}

// EnsureCSVExtension adds .csv extension if not present.
func (*FilePathBuilder) EnsureCSVExtension(filename string) string {
	return ensureCSVExtension(filename)
}

// StripCSVExtension removes .csv extension if present.
func (*FilePathBuilder) StripCSVExtension(filename string) string {
	return stripCSVExtension(filename)
}

// ensureCSVExtension adds .csv extension if not present.
func ensureCSVExtension(filename string) string {
	if !strings.HasSuffix(strings.ToLower(filename), ".csv") {
		return filename + ".csv"
	}

	return filename
}

// csvExtension is the file extension for CSV files.
const csvExtension = ".csv"

// stripCSVExtension removes .csv extension if present (case-insensitive).
func stripCSVExtension(filename string) string {
	if strings.HasSuffix(strings.ToLower(filename), csvExtension) {
		return strings.TrimSuffix(filename, filename[len(filename)-len(csvExtension):])
	}

	return filename
}

// StripCSVExt removes .csv extension (case-insensitive). Package-level function.
func StripCSVExt(filename string) string {
	return stripCSVExtension(filename)
}
