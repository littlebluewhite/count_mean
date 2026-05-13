package security_test

import (
	"strings"
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
			wantErr: false, // 允許空路徑
		},
		{
			name:    "path with percent sign",
			path:    "./input/test%20file.csv",
			wantErr: false, // 允許包含 % 符號的路徑
		},
		{
			name:    "path with spaces",
			path:    "./input/test file.csv",
			wantErr: false, // 允許包含空格的路徑
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

// TestPathValidator_AcceptsLegitimateDotsInFilename 是 P2-B 的 regression：
// 過去 validatePathFormat 用 strings.Contains(decoded, "..") 子字串檢查，會誤拒
// 合法檔名（report..v2.csv、backup..2024.csv）。修法改為 filepath.Clean 後做
// element-level 比對，element == ".." 才視為 traversal — 既擋穿越也容納雙點檔名。
//
// codex review + professional/code-debugger/security 三個 agent 在 commit 874b792
// 都標出此缺口；rule of three 觸發 fix。
func TestPathValidator_AcceptsLegitimateDotsInFilename(t *testing.T) {
	t.Parallel()

	validator := security.NewPathValidator(nil)

	t.Run("legitimate filenames with double dots are accepted", func(t *testing.T) {
		t.Parallel()
		legitimate := []string{
			"/tmp/report..v2.csv",
			"/tmp/backup..2024.csv",
			"/tmp/my..backup.csv",
			"/var/tmp/data..2025-05-13.csv",
		}
		for _, path := range legitimate {
			if err := validator.ValidateExternalPath(path); err != nil {
				t.Errorf("ValidateExternalPath(%q) should accept legitimate filename, got: %v", path, err)
			}
		}
	})

	t.Run("true traversal elements are still rejected", func(t *testing.T) {
		t.Parallel()
		traversal := []string{
			"../etc/passwd",
			"foo/../bar/../etc/passwd",
			"..",
		}
		for _, path := range traversal {
			if err := validator.ValidateExternalPath(path); err == nil {
				t.Errorf("ValidateExternalPath(%q) should reject real traversal, got nil error", path)
			}
		}
	})
}

// TestPathValidator_SanitizePath_PreservesDoubleDotFilename 是 codex Wave 6
// second-pass P2 的 regression：先前 SanitizePath 內部用
// strings.ReplaceAll(path, "..", "") 把 `report..v2.csv` 改寫成 `reportv2.csv`，
// 而 GetSafePath 的 element-based traversal check 卻會放它過，於是 validation
// 接受 → sanitization 改寫 → caller 讀/寫到完全不同的檔案。修法統一改 element-based
// 過濾 (`filterTraversalElements`)。
func TestPathValidator_SanitizePath_PreservesDoubleDotFilename(t *testing.T) {
	t.Parallel()

	validator := security.NewPathValidator([]string{"."})

	preserved := []struct {
		name string
		path string
		want string
	}{
		{"single dir, literal double dots", "report..v2.csv", "report..v2.csv"},
		{"nested dir, literal double dots in filename", "input/backup..2024.csv", "input/backup..2024.csv"},
		{"trailing version with double dots", "data..v3..final.csv", "data..v3..final.csv"},
	}

	for _, tt := range preserved {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := validator.SanitizePath(tt.path); got != tt.want {
				t.Errorf("SanitizePath(%q) = %q, want %q (literal `..` substring must not be stripped)", tt.path, got, tt.want)
			}
		})
	}
}

// TestPathValidator_GetSafePath_RoundTripsDoubleDotFilename 確認端到端的
// validation + sanitization 對 `report..v2.csv` 路徑保持一致 — GetSafePath
// 必須回傳「含原始檔名」的 full path，而不是 silently 改寫到 `reportv2.csv`
// 之後 caller 讀錯檔案（codex Wave 6 second-pass P2）。
func TestPathValidator_GetSafePath_RoundTripsDoubleDotFilename(t *testing.T) {
	t.Parallel()

	validator := security.NewPathValidator([]string{"./input"})

	cases := []string{
		"report..v2.csv",
		"backup..2024.csv",
	}

	for _, filename := range cases {
		filename := filename
		t.Run(filename, func(t *testing.T) {
			t.Parallel()
			fullPath, err := validator.GetSafePath("./input", filename)
			if err != nil {
				t.Fatalf("GetSafePath(./input, %q) unexpected error: %v", filename, err)
			}
			if !strings.HasSuffix(fullPath, filename) {
				t.Errorf("GetSafePath(./input, %q) = %q, expected suffix %q (filename must round-trip without `..` stripping)",
					filename, fullPath, filename)
			}
		})
	}
}
