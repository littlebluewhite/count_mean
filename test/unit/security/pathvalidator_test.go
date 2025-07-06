package security_test

import (
	"testing"

	"count_mean/internal/security"
)

func TestPathValidator_ValidateFilePath(t *testing.T) {
	// Create temporary test directories
	allowedPaths := []string{"/tmp/test", "./input", "./output"}
	validator := security.NewPathValidator(allowedPaths)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "valid relative path",
			path:    "./input/test.csv",
			wantErr: false,
		},
		{
			name:    "empty path",
			path:    "",
			wantErr: true,
		},
		{
			name:    "path traversal attempt",
			path:    "../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path with double dots",
			path:    "./input/../output/test.csv",
			wantErr: true,
		},
		{
			name:    "absolute path outside allowed",
			path:    "/etc/passwd",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateFilePath(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateFilePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPathValidator_IsCSVFile(t *testing.T) {
	validator := security.NewPathValidator([]string{"."})

	tests := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "csv file",
			path: "test.csv",
			want: true,
		},
		{
			name: "CSV file uppercase",
			path: "test.CSV",
			want: true,
		},
		{
			name: "not csv file",
			path: "test.txt",
			want: false,
		},
		{
			name: "no extension",
			path: "test",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validator.IsCSVFile(tt.path); got != tt.want {
				t.Errorf("IsCSVFile() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathValidator_SanitizePath(t *testing.T) {
	validator := security.NewPathValidator([]string{"."})

	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "clean path",
			path: "test.csv",
			want: "test.csv",
		},
		{
			name: "path with null byte",
			path: "test\x00.csv",
			want: "test.csv",
		},
		{
			name: "path with newline",
			path: "test\n.csv",
			want: "test.csv",
		},
		{
			name: "path with carriage return",
			path: "test\r.csv",
			want: "test.csv",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validator.SanitizePath(tt.path); got != tt.want {
				t.Errorf("SanitizePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPathValidator_GetSafePath(t *testing.T) {
	allowedPaths := []string{"./input", "./output"}
	validator := security.NewPathValidator(allowedPaths)

	tests := []struct {
		name     string
		basePath string
		filename string
		wantErr  bool
	}{
		{
			name:     "valid combination",
			basePath: "./input",
			filename: "test.csv",
			wantErr:  false,
		},
		{
			name:     "invalid base path",
			basePath: "../forbidden",
			filename: "test.csv",
			wantErr:  true,
		},
		{
			name:     "filename with path traversal",
			basePath: "./input",
			filename: "../test.csv",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.GetSafePath(tt.basePath, tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetSafePath() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
