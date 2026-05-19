package security

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPathValidator_ValidateFilePath(t *testing.T) {
	// Create temporary test directories
	allowedPaths := []string{"/tmp/test", "./input", "./output"}
	validator := NewPathValidator(allowedPaths)

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
	validator := NewPathValidator([]string{"."})

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

// 釘住 SanitizePath 的 (string, error) 契約:含 `\x00` / `\n` 等控制字元或
// silent-rewrite 攻擊 pattern(如 `report\x01..csv`,silent strip 後通過
// element-based traversal check 卻 OS 開到別的檔)一律 reject,caller 必須處理。
func TestPathValidator_SanitizePath(t *testing.T) {
	validator := NewPathValidator([]string{"."})

	t.Run("clean path passes through unchanged", func(t *testing.T) {
		got, err := validator.SanitizePath("test.csv")
		if err != nil {
			t.Fatalf("clean path should not error: %v", err)
		}
		if got != "test.csv" {
			t.Errorf("clean path mutated: got %q, want %q", got, "test.csv")
		}
	})

	t.Run("clean path with double-dot filename passes through", func(t *testing.T) {
		got, err := validator.SanitizePath("report..v2.csv")
		if err != nil {
			t.Fatalf("legitimate double-dot filename should not error: %v", err)
		}
		if got != "report..v2.csv" {
			t.Errorf("double-dot filename mutated: got %q, want %q", got, "report..v2.csv")
		}
	})

	rejected := []struct {
		name string
		path string
	}{
		{"embedded null byte", "test\x00.csv"},
		{"embedded newline", "test\n.csv"},
		{"embedded carriage return", "test\r.csv"},
		// canonical silent-rewrite case:`...//etc/passwd` 經 `../`/`./` 替換 +
		// filterTraversalElements 後 output 看似乾淨,但與 input 語意不同 —
		// caller 拿著 sanitized 結果做 ValidateFilePath 會通過,實際 OS 開到
		// 另一條檔。改為「移除字元 != 0」就 reject。
		{"traversal followed by double slash", "...//etc/passwd"},
		{"report with embedded SOH and double dot", "report\x01..csv"},
		// 額外的常見 control char。
		{"tab in name", "test\t.csv"},
		{"vertical tab", "test\x0B.csv"},
		{"unit separator", "test\x1F.csv"},
	}

	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validator.SanitizePath(tt.path)
			if err == nil {
				t.Fatalf("SanitizePath(%q) should reject silent rewrite, got cleaned=%q err=nil",
					tt.path, got)
			}
			if !errors.Is(err, ErrPathSanitizationRequired) {
				t.Errorf("SanitizePath(%q) should return ErrPathSanitizationRequired, got %v",
					tt.path, err)
			}
		})
	}
}

func TestPathValidator_GetSafePath(t *testing.T) {
	allowedPaths := []string{"./input", "./output"}
	validator := NewPathValidator(allowedPaths)

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

// DefaultValidator() 回傳的 process-wide singleton 必須 immutable — 任何
// SetAllowedBasePaths 呼叫都要回 ErrValidatorFrozen,避免「init 階段設定好的
// allow-list 被任意 caller 後續改寫」形成 trust boundary 破口。
//
// 設計選擇:不從 public API 移除 SetAllowedBasePaths(NewPathValidator 建構的
// instance 仍合理需要設定 base paths);用 frozen flag 在入口處檢查即可。
func TestDefaultValidator_IsImmutable(t *testing.T) {
	t.Parallel()

	v := DefaultValidator()

	err := v.SetAllowedBasePaths([]string{"/tmp/should-not-apply"})
	if err == nil {
		t.Fatalf("DefaultValidator().SetAllowedBasePaths 應 reject,got nil error")
	}
	if !errors.Is(err, ErrValidatorFrozen) {
		t.Errorf("expected ErrValidatorFrozen, got %v", err)
	}

	// 確保 allow-list 沒被修改 — DefaultValidator 預設為 nil whitelist。
	if got := v.GetAllowedBasePaths(); len(got) != 0 {
		t.Errorf("DefaultValidator allow-list 應為空,被修改為 %v", got)
	}
}

// 確認 frozen flag 只 apply 到 default singleton — 一般 NewPathValidator(...)
// 建構的 instance 仍可呼叫 SetAllowedBasePaths,可變性是合約。
func TestNewPathValidator_StillMutable(t *testing.T) {
	t.Parallel()

	v := NewPathValidator([]string{"/tmp/a"})
	if err := v.SetAllowedBasePaths([]string{"/tmp/b"}); err != nil {
		t.Fatalf("非 default validator 應允許 SetAllowedBasePaths,got %v", err)
	}
}

// ValidateFilePath 對含 NUL byte 的 input 必須回 ErrPathContainsNUL,與
// lenient_path 契約對稱。NUL 是 POSIX / Windows 檔案 API 的字串 truncation
// 字元 — 攻擊者送 `/legit/foo.csv\x00/etc/passwd`,部分 syscall 在 NUL 截斷後
// 開到 `/legit/foo.csv`,其他 OS / locale 可能跟到 NUL 後的 suffix。在 boundary
// 早期 reject 是最強的 input contract。
func TestValidateFilePath_RejectsNUL(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil) // nil 走無白名單模式,只跑 path-format check

	cases := []string{
		"foo\x00bar.csv",
		"/legit/foo.csv\x00/etc/passwd",
		"\x00leading.csv",
		"trailing\x00",
	}

	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			err := validator.ValidateFilePath(path)
			if err == nil {
				t.Fatalf("ValidateFilePath(%q) 應 reject NUL byte,got nil error", path)
			}
			if !errors.Is(err, ErrPathContainsNUL) {
				t.Errorf("ValidateFilePath(%q) 應回 ErrPathContainsNUL,got %v", path, err)
			}
		})
	}
}

// 對稱 ValidateFilePath_RejectsNUL — ValidateExternalPath 同樣必須 reject NUL byte。
func TestValidateExternalPath_RejectsNUL(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	cases := []string{
		"foo\x00bar.csv",
		"/legit/foo.csv\x00/etc/passwd",
		"\x00leading.csv",
		"trailing\x00",
	}

	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			err := validator.ValidateExternalPath(path)
			if err == nil {
				t.Fatalf("ValidateExternalPath(%q) 應 reject NUL byte,got nil error", path)
			}
			if !errors.Is(err, ErrPathContainsNUL) {
				t.Errorf("ValidateExternalPath(%q) 應回 ErrPathContainsNUL,got %v", path, err)
			}
		})
	}
}

// Regression:子字串 `..` 檢查會誤拒合法檔名(`report..v2.csv`、`backup..2024.csv`)。
// 改為 filepath.Clean 後做 element-level 比對,element == ".." 才視為 traversal —
// 既擋穿越也容納雙點檔名。
func TestPathValidator_AcceptsLegitimateDotsInFilename(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

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

// Regression:substring-based `..` 替換會把 `report..v2.csv` 改寫成
// `reportv2.csv`,而 GetSafePath 的 element-based check 又會放它過 — validation
// 接受、sanitization 改寫、caller 讀/寫到完全不同的檔案。統一改 element-based
// 過濾 (`filterTraversalElements`)。
func TestPathValidator_SanitizePath_PreservesDoubleDotFilename(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator([]string{"."})

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
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := validator.SanitizePath(tt.path)
			if err != nil {
				t.Fatalf("SanitizePath(%q) unexpected error: %v", tt.path, err)
			}
			if got != tt.want {
				t.Errorf("SanitizePath(%q) = %q, want %q (literal `..` substring must not be stripped)", tt.path, got, tt.want)
			}
		})
	}
}

// 端到端 validation + sanitization 對 `report..v2.csv` 路徑保持一致 —
// GetSafePath 必須回傳「含原始檔名」的 full path,不可 silently 改寫到
// `reportv2.csv` 之後 caller 讀錯檔案。
func TestPathValidator_GetSafePath_RoundTripsDoubleDotFilename(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator([]string{"./input"})

	cases := []string{
		"report..v2.csv",
		"backup..2024.csv",
	}

	for _, filename := range cases {
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

// SetAllowedBasePaths(nil) 與空 slice 都必須 reject 並維持原 allow-list 不變,
// 避免 silently 退化到「無白名單」模式;若確實要無白名單請改用 NewPathValidator(nil)。
func TestPathValidator_SetAllowedBasePaths_RejectsEmpty(t *testing.T) {
	t.Parallel()

	initial := []string{"/tmp/safe-zone"}
	validator := NewPathValidator(initial)

	t.Run("nil slice is rejected", func(t *testing.T) {
		t.Parallel()
		err := validator.SetAllowedBasePaths(nil)
		if err == nil {
			t.Fatalf("SetAllowedBasePaths(nil) should reject empty input, got nil error")
		}
	})

	t.Run("empty slice is rejected", func(t *testing.T) {
		t.Parallel()
		err := validator.SetAllowedBasePaths([]string{})
		if err == nil {
			t.Fatalf("SetAllowedBasePaths([]) should reject empty input, got nil error")
		}
	})

	t.Run("slice of only blank strings is rejected", func(t *testing.T) {
		t.Parallel()
		err := validator.SetAllowedBasePaths([]string{"", "  "})
		if err == nil {
			t.Fatalf("SetAllowedBasePaths(blanks) should reject empty-after-filter input, got nil error")
		}
	})

	t.Run("allow-list is preserved when rejected", func(t *testing.T) {
		t.Parallel()
		v := NewPathValidator(initial)
		_ = v.SetAllowedBasePaths(nil)
		got := v.GetAllowedBasePaths()
		if len(got) != 1 {
			t.Fatalf("allow-list mutated after rejected SetAllowedBasePaths(nil): got %v, want 1 entry", got)
		}
	})

	t.Run("non-empty slice is still accepted", func(t *testing.T) {
		t.Parallel()
		v := NewPathValidator(nil)
		if err := v.SetAllowedBasePaths([]string{"/tmp/new-zone"}); err != nil {
			t.Fatalf("SetAllowedBasePaths(non-empty) returned error: %v", err)
		}
		if len(v.GetAllowedBasePaths()) != 1 {
			t.Fatalf("non-empty SetAllowedBasePaths did not update allow-list")
		}
	})
}

// Reject 契約:silent rewrite 後語意偏離的 path(例 `....//foo.csv` → `.foo.csv`、
// `../etc/passwd` → `etc/passwd`、`foo\x00//bar.csv` → `foo/bar.csv`)會讓
// caller 拿著 sanitized 結果做 ValidateFilePath / OS open 落到非預期檔,
// 一律回 ErrPathSanitizationRequired。
func TestSanitizePath_RejectsDangerousInputs(t *testing.T) {
	t.Parallel()

	rejected := []string{
		"../etc/passwd",
		"....//foo.csv",
		"...///bar.txt",
		"./../baz.csv",
		"..//..//qux.csv",
		"\\\\..\\\\quux.csv",
		"././/etc/passwd",
		"....\\\\corge.csv",
		"./../.././grault.csv",
		"./foo.csv",            // 含 `./` ref
		"foo\x00//bar.csv",     // 含 NUL
		"//report..v2.csv",     // 起首 //
		".\\windows.csv",       // Windows 端 `.\` ref
		"foo\r\nbar.csv",       // 含 CRLF
	}

	for _, input := range rejected {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			got, err := SanitizePath(input)
			if err == nil {
				t.Fatalf("SanitizePath(%q) should reject silent rewrite, got cleaned=%q err=nil",
					input, got)
			}
			if !errors.Is(err, ErrPathSanitizationRequired) {
				t.Errorf("SanitizePath(%q) should return ErrPathSanitizationRequired, got %v",
					input, err)
			}
		})
	}
}

// TestSanitizePath_DeterministicForCleanInputs 確認新 contract 下,clean input
// 重複呼叫仍 deterministic — 沒有 random map iteration / global mutable state。
func TestSanitizePath_DeterministicForCleanInputs(t *testing.T) {
	t.Parallel()

	clean := []string{
		"foo.csv",
		"report..v2.csv",
		"input/backup..2024.csv",
		"data..v3..final.csv",
		"a/b/c/d.csv",
	}

	for _, input := range clean {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			first, firstErr := SanitizePath(input)
			if firstErr != nil {
				t.Fatalf("SanitizePath(%q) unexpected error on clean input: %v", input, firstErr)
			}
			for i := 0; i < 100; i++ {
				got, err := SanitizePath(input)
				if err != nil {
					t.Fatalf("SanitizePath(%q) call %d unexpected error: %v", input, i, err)
				}
				if got != first {
					t.Fatalf("SanitizePath(%q) non-deterministic: call 0 = %q, call %d = %q",
						input, first, i, got)
				}
			}
		})
	}
}

// Lexical Clean+Abs 直接 substring 比對 sensitivePatterns 而不 resolve symlink
// 是真實安全破口 — `/tmp/foo → /etc` 之類 symlink 攻擊路徑 `/tmp/foo/passwd`
// 通過 boundary 後,os.Open 跟著 symlink 讀到 /etc/passwd。ValidateExternalPath
// 內必須先 EvalSymlinks 到 nearest existing parent,resolved 路徑同樣跑
// performBasicSecurityChecks。
//
// 用 t.TempDir() 在 /tmp 之下建立 symlink，再驗證 ValidateExternalPath 對指向 /etc
// 的 symlink reject。
func TestPathValidator_ValidateExternalPath_RejectsSymlinkToSensitive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink 需 admin 權限，且 EvalSymlinks 對 junction 行為與 Unix 不同；" +
			"Windows 等價 case 由 fsperm/flags_windows_contract_test.go 覆蓋")
	}

	t.Parallel()

	tmpRoot := t.TempDir()
	linkPath := filepath.Join(tmpRoot, "etc_link")

	// 建立 symlink 指向 /etc — 一個 performBasicSecurityChecks 明文擋的敏感目錄
	if err := os.Symlink("/etc", linkPath); err != nil {
		t.Fatalf("建立 symlink 失敗（測試環境問題）: %v", err)
	}

	validator := NewPathValidator(nil)

	// Case 1：穿透 symlink 抵達 /etc/passwd（最典型的攻擊形式：caller 把
	// `<link>/passwd` 送進來，lexical 看起來只是 `/tmp/xxx/etc_link/passwd`
	// 通過所有 lexical check，但 EvalSymlinks 解析後變 /etc/passwd → /private/etc/passwd
	// 命中 sensitive prefix）。
	childPath := filepath.Join(linkPath, "passwd")
	if err := validator.ValidateExternalPath(childPath); err == nil {
		t.Errorf("ValidateExternalPath(%q) 應 reject 穿透 symlink 抵達 /etc/passwd，got nil error",
			childPath)
	} else if !errors.Is(err, ErrSensitiveDirectory) {
		t.Errorf("ValidateExternalPath(%q) 應回 ErrSensitiveDirectory，got %v", childPath, err)
	}

	// Case 2：典型 GUI / config 場景 — caller 用 `filepath.Join(linkPath, "_validation_marker")`
	// 構造 dummy child 來驗 OutputDir 是否安全。即便 linkPath 還沒建立 marker，
	// fallback 邏輯應走 parent symlink resolve 到 /private/etc，再 join "_validation_marker"
	// 後命中 sensitive prefix。釘住 config.Validate / cci.ExportToCSV / muscle_ratio.Analyze
	// 對此 path 仍能擋下 symlink 偽裝的 OutputDir。
	dummyChild := filepath.Join(linkPath, "_validation_marker")
	if err := validator.ValidateExternalPath(dummyChild); err == nil {
		t.Errorf("ValidateExternalPath(%q) 應 reject symlink-to-sensitive 的 dummy-child 驗證形式，got nil error",
			dummyChild)
	} else if !errors.Is(err, ErrSensitiveDirectory) {
		t.Errorf("ValidateExternalPath(%q) 應回 ErrSensitiveDirectory，got %v", dummyChild, err)
	}
}

// TestPathValidator_ValidateExternalPath_AllowsNonExistentChildOfSafeParent 確認
// 修法後對「未存在的 output path」仍能正常通過 — typical case 是 config
// validation 用 `filepath.Join(OutputDir, "_validation_marker")` 構造 dummy
// child path 來驗證 OutputDir 的 sensitive prefix。若 fallback 邏輯壞掉、把
// 未存在的 child 視為 error，會把所有 config validation 都打掛。
func TestPathValidator_ValidateExternalPath_AllowsNonExistentChildOfSafeParent(t *testing.T) {
	t.Parallel()

	tmpRoot := t.TempDir()
	nonExistent := filepath.Join(tmpRoot, "does_not_exist", "and_neither_does_this", "file.csv")

	validator := NewPathValidator(nil)

	if err := validator.ValidateExternalPath(nonExistent); err != nil {
		t.Errorf("ValidateExternalPath(%q) on non-existent child of safe parent should pass, got: %v",
			nonExistent, err)
	}
}

// TestPathValidator_ValidateExternalPath_AcceptsRegularPath 是 sanity check：
// 修法不能改變既有合法 case 的 happy path（在 /tmp 或 t.TempDir() 下的一般檔案路徑）。
func TestPathValidator_ValidateExternalPath_AcceptsRegularPath(t *testing.T) {
	t.Parallel()

	tmpRoot := t.TempDir()
	regular := filepath.Join(tmpRoot, "data.csv")

	validator := NewPathValidator(nil)

	if err := validator.ValidateExternalPath(regular); err != nil {
		t.Errorf("ValidateExternalPath(%q) on regular path should pass, got: %v", regular, err)
	}
}

// TestPathValidator_ValidateExternalPath_RejectsSymlinkChainToSensitive 釘住
// chained symlink 攻擊：`<tmp>/a → <tmp>/b → /etc`。filepath.EvalSymlinks 會
// 遞迴解析到最終 target，所以中間多層 symlink 不該繞過 sensitive prefix check。
func TestPathValidator_ValidateExternalPath_RejectsSymlinkChainToSensitive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink 需 admin 權限，跳過")
	}

	t.Parallel()

	tmpRoot := t.TempDir()
	linkB := filepath.Join(tmpRoot, "b")
	linkA := filepath.Join(tmpRoot, "a")

	// chain: a → b → /etc
	if err := os.Symlink("/etc", linkB); err != nil {
		t.Fatalf("symlink b → /etc 失敗: %v", err)
	}
	if err := os.Symlink(linkB, linkA); err != nil {
		t.Fatalf("symlink a → b 失敗: %v", err)
	}

	validator := NewPathValidator(nil)

	// 用 child path 形式驗證（符合 caller 實際使用模式 — 見上一個測試的 Case 2 註解）。
	attackPath := filepath.Join(linkA, "passwd")
	if err := validator.ValidateExternalPath(attackPath); err == nil {
		t.Errorf("chained symlink 指向 /etc 應被擋，got nil error")
	} else if !errors.Is(err, ErrSensitiveDirectory) {
		t.Errorf("chained symlink reject 應回 ErrSensitiveDirectory，got %v", err)
	}
}

// TestPathValidator_ValidateExternalPath_EmptyPathPasses 維持原 behavior：
// empty path 是 caller convention 的 "no value"（GUI dialog 取消 etc.），
// 不該被 symlink resolve 改寫。
func TestPathValidator_ValidateExternalPath_EmptyPathPasses(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	if err := validator.ValidateExternalPath(""); err != nil {
		t.Errorf("ValidateExternalPath(\"\") 應 pass (caller-side empty check)，got: %v", err)
	}
}

// ValidateExternalPath(GUI dialog 等 user-confirmed 場景)對「URL-decode 4 層後
// 仍含 `%`」不 reject,放行合法檔名(`report 50%.csv` / BTS 匯出檔)。其他守門
// (element traversal、敏感目錄、symlink resolve)仍生效。ValidateFilePath
// 保留嚴格 % 守門 — 受控路徑來源可信度較低,典型 config/API string 直接送入。
func TestPathValidator_ExternalPath_AcceptsLiteralPercentInFilename(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	tmpRoot := t.TempDir()
	cases := []string{
		filepath.Join(tmpRoot, "report 50%.csv"),
		filepath.Join(tmpRoot, "Q4 50% target.csv"),
		filepath.Join(tmpRoot, "100% complete data.csv"),
		filepath.Join(tmpRoot, "SF_8_BTS%_6.10_BP30450_RMS0.5_0.49.csv"), // BTS 匯出檔名
	}

	for _, p := range cases {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()
			if err := validator.ValidateExternalPath(p); err != nil {
				t.Errorf("ValidateExternalPath(%q) 應放行含字面 %% 的合法檔名，實際 err=%v", p, err)
			}
		})
	}
}

// 確認「% 放寬只限 ExternalPath」不會 leak 到 strict 路徑 — ValidateFilePath
// 在受控路徑(InputDir / OutputDir / config 直接送入)仍視殘留 `%` 為可疑。
func TestPathValidator_FilePath_StillRejectsLiteralPercent(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	// 純 lexical 含 `%` — ValidateFilePath 仍應擋（防 double-encoding 攻擊）
	if err := validator.ValidateFilePath("./input/report 50%.csv"); err == nil {
		t.Error("ValidateFilePath 仍應拒絕含字面 %% 的 path（嚴格守門），實際通過")
	}
}

// 放寬「殘留 %」不可順便放走 traversal — 即使 path 含字面 %,含 `..` 路徑元素
// 仍必須擋。
func TestPathValidator_ExternalPath_StillRejectsTraversalEvenWithPercent(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	cases := []string{
		"/tmp/../etc/50% target.csv",
		"/tmp/.%2E/etc/data.csv", // %2E = `.`，decode 後變成 `..`
	}

	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			if err := validator.ValidateExternalPath(p); err == nil {
				t.Errorf("ValidateExternalPath(%q) 仍應拒絕 traversal（即使含 %%），實際通過", p)
			}
		})
	}
}

// performBasicSecurityChecks 擋 Windows 端的 AppData 與 .ssh 路徑(credential
// 洩漏 / token theft 目標)。case-insensitive 路徑命中對齊 sensitivePatterns
// 的 ToSlash + ToLower 比對。
func TestPathValidator_ExternalPath_RejectsWindowsSensitivePaths(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	sensitive := []string{
		// AppData family — Roaming / Local / LocalLow 三條都該擋
		`C:\Users\victim\AppData\Roaming\Microsoft\Credentials\stored_token`,
		`C:\Users\victim\AppData\Local\Microsoft\Credentials\stored_token`,
		`C:\Users\victim\AppData\LocalLow\Mozilla\creds.db`,
		// .ssh — 跨平台都應視為敏感（Linux 也常用 ~/.ssh/id_rsa）
		`C:\Users\victim\.ssh\id_rsa`,
		`C:\Users\victim\.ssh\authorized_keys`,
		// 大小寫變體與 forward-slash 變體仍應命中（PathValidator 用 ToSlash+ToLower 比對）
		`c:/users/victim/appdata/roaming/secret`,
		`C:/Users/Victim/.SSH/id_rsa`,
	}

	for _, p := range sensitive {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			err := validator.ValidateExternalPath(p)
			if err == nil {
				t.Errorf("ValidateExternalPath(%q) 應擋下 Windows credential / .ssh 敏感路徑，got nil", p)
				return
			}
			if !errors.Is(err, ErrSensitiveDirectory) {
				t.Errorf("ValidateExternalPath(%q) 應回 ErrSensitiveDirectory，got %v", p, err)
			}
		})
	}
}

// Windows reserved device names(CON / PRN / AUX / NUL / COM1-9 / LPT1-9)即使
// 加副檔名(`CON.txt`)也會 redirect 到實體 device。caller 送
// `C:\Users\victim\Downloads\CON.csv` 進 GUI 而 OpenFile 開到 console device
// 會造成怪異 IO 行為。performBasicSecurityChecks 多檢 filepath.Base 去 ext 後
// 是否命中 reserved list,跨平台一致 reject(Unix 也擋,保守決策對使用者無害)。
func TestPathValidator_ExternalPath_RejectsWindowsReservedDeviceNames(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	tmpRoot := t.TempDir() // 在 safe parent 之下，確保不是因為 parent 敏感而被擋

	// 取一些代表性的 reserved names（不窮舉 COM1-9 / LPT1-9，省 CI 時間）
	reserved := []string{
		"CON.csv", "PRN.csv", "AUX.csv", "NUL.csv",
		"COM1.csv", "COM9.csv",
		"LPT1.csv", "LPT9.csv",
		// 大小寫變體
		"con.csv", "Prn.csv", "cOm3.txt",
		// 無副檔名也該擋（"CON" alone 也是 reserved）
		"CON",
		"lpt5",
	}

	for _, name := range reserved {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			full := filepath.Join(tmpRoot, name)
			err := validator.ValidateExternalPath(full)
			if err == nil {
				t.Errorf("ValidateExternalPath(%q) 應擋下 Windows reserved device name，got nil", name)
			}
		})
	}
}

// Sanity check:合法檔名含 reserved name 作為「子字串」(`econom_report.csv` 含
// `con`、`reconnect.csv` 含 `con`)必須放行 — 比對應基於 base filename 等於
// reserved name,而非 substring contains。
func TestPathValidator_ExternalPath_AcceptsNormalFilenameContainingReservedSubstring(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	tmpRoot := t.TempDir()

	normal := []string{
		"econom_report.csv", // 含 "con" 子字串
		"reconnect.csv",     // 含 "con"
		"prnt_log.csv",      // 含 "prn"
		"auxiliary.csv",     // 含 "aux"
		"nullable.csv",      // 含 "nul"
		"compass.csv",       // 含 "com" 但不是 COM[1-9]
		"slept.csv",         // 含 "lpt"
	}

	for _, name := range normal {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			full := filepath.Join(tmpRoot, name)
			if err := validator.ValidateExternalPath(full); err != nil {
				t.Errorf("ValidateExternalPath(%q) 應放行（reserved name 子字串不算命中），got %v", name, err)
			}
		})
	}
}

// 跨平台高價值敏感目錄擴充涵蓋:
//   - 非 C: drive (D:\Windows\, E:\Windows\, ...) — multi-OS / Bootcamp / VM 場景
//   - UNC paths (\\server\share, \\?\C:\Windows\) — Windows 網路 + device namespace
//   - ~/.aws/ (AWS credentials)
//   - ~/.kube/ (Kubernetes config + tokens)
//   - /Library/Keychains/ (macOS keychain)
//   - /var/log/ (Unix 系統 log)
func TestPathValidator_ExternalPath_RejectsExtendedSensitivePaths(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	cases := []struct {
		name string
		path string
	}{
		// Non-C: drives (Windows multi-OS / VM / Bootcamp 場景)
		{"D drive Windows", `D:\Windows\System32\config\SAM`},
		{"E drive Windows", `E:\Windows\notepad.exe`},
		{"D drive System32", `D:\System32\drivers\etc\hosts`},
		{"Z drive AppData", `Z:\Users\victim\AppData\Roaming\creds`},
		// UNC paths
		{"UNC server share", `\\server\share\secret.txt`},
		{"UNC long-namespace device", `\\?\C:\Windows\System32\config\SAM`},
		{"UNC global", `\\.\PhysicalDrive0`},
		// AWS credentials
		{"linux aws", "/home/victim/.aws/credentials"},
		{"macOS aws", "/Users/victim/.aws/credentials"},
		{"windows aws", `C:\Users\victim\.aws\credentials`},
		// Kubernetes config
		{"linux kube", "/home/victim/.kube/config"},
		{"windows kube", `C:\Users\victim\.kube\config`},
		// macOS keychain
		{"system keychain", "/Library/Keychains/System.keychain"},
		{"user keychain", "/Users/victim/Library/Keychains/login.keychain-db"},
		// Linux system log
		{"unix var log", "/var/log/auth.log"},
		{"unix var log subdir", "/var/log/apt/history.log"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := validator.ValidateExternalPath(c.path)
			if err == nil {
				t.Errorf("ValidateExternalPath(%q) 應擋下擴充的敏感路徑,got nil", c.path)
				return
			}
			if !errors.Is(err, ErrSensitiveDirectory) {
				t.Errorf("ValidateExternalPath(%q) 應回 ErrSensitiveDirectory，got %v", c.path, err)
			}
		})
	}
}

// 擴充的 sensitive patterns 不可誤擋合法 path — 比對應基於完整 path segment
// (如 `/.aws/` 帶左右斜線),而非子字串。
func TestPathValidator_ExternalPath_AllowsLookalikeButNotMatchingPaths(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	tmpRoot := t.TempDir()

	cases := []string{
		// 含 "aws" 但非 `.aws/` 目錄 — 例 user 命名的資料夾叫 `aws_results`
		filepath.Join(tmpRoot, "aws_results.csv"),
		filepath.Join(tmpRoot, "data_aws.csv"),
		// 含 "kube" 但非 `.kube/` 目錄
		filepath.Join(tmpRoot, "kubernetes_demo.csv"),
		// 含 "Keychains" 但 case 或路徑不同 (Library/Keychains 才是 macOS 系統目錄)
		filepath.Join(tmpRoot, "my_keychain_backup.csv"),
		// 含 "var" 但非 /var/log
		filepath.Join(tmpRoot, "varlog_analysis.csv"),
	}

	for _, p := range cases {
		t.Run(filepath.Base(p), func(t *testing.T) {
			t.Parallel()
			if err := validator.ValidateExternalPath(p); err != nil {
				t.Errorf("ValidateExternalPath(%q) 應放行（不該與 sensitive prefix 誤命中），got %v", p, err)
			}
		})
	}
}

// 直接 filepath.Ext 後比對 ".csv" 會誤拒尾端有空白或點的合法檔(`filepath.Ext("file.csv ")`
// 回 `.csv ` 含後置空白)。Excel 匯出 / Windows 拖拉操作會留下 trailing space / dot,
// 這類檔名仍能 open,IsCSVFile 取 Ext 前先 TrimRight 把尾端 noise 剝乾淨。
func TestPathValidator_IsCSVFile_TrailingSpaceAndDot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"plain csv", "data.csv", true},
		{"csv with trailing space", "data.csv ", true},
		{"csv with multiple trailing spaces", "data.csv   ", true},
		{"csv with trailing dot", "data.csv.", true},
		{"csv with trailing space and dot mixed", "data.csv. .", true},
		{"CSV uppercase with trailing space", "DATA.CSV ", true},
		{"path with trailing space", "/tmp/data.csv ", true},
		// negatives should remain negative
		{"txt file", "data.txt", false},
		{"txt with trailing space", "data.txt ", false},
		{"no extension", "data", false},
		{"hidden file no ext", ".data", false},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsCSVFile(tt.path); got != tt.want {
				t.Errorf("IsCSVFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// Fuzz target:用 multi-layer URL-encoded traversal payload + unicode mutation
// 持續攻擊 validatePathFormat,strictPercent on/off 兩條都跑。
//
// 不變式:
//  1. 任意 input 不得 panic
//  2. 任意 input 若 URL-decode 4 層後含 `..` 路徑元素,validation 必須擋下,
//     否則 traversal 守門失效。
func FuzzValidatePathFormatMultiLayerURL(f *testing.F) {
	// Seed corpus：手動建構多層 URL-encoded traversal payload 與相關變體。
	// 每個 seed 都應該被 validatePathFormat 透過 element-level traversal 守門擋下；
	// 若 fuzz 發現「validation 通過 + decode 後 path 含 `..` element」即 bug。
	seeds := []string{
		"../etc/passwd",
		"..%2Fetc%2Fpasswd",                          // 1 層
		"..%252Fetc%252Fpasswd",                      // 2 層
		"..%25252Fetc%25252Fpasswd",                  // 3 層
		"..%2525252Fetc%2525252Fpasswd",              // 4 層
		"..%252525252Fetc%252525252Fpasswd",          // 5 層 (超出 cap)
		"%2E%2E/etc/passwd",                          // %2E = `.`
		"%2E%2E%2Fetc%2Fpasswd",                      // 全 encoded
		"%252E%252E%252Fetc%252Fpasswd",              // 2 層 dot+slash
		strings.Repeat("..%2F", 32) + "etc/passwd",   // 大量 .. 重複
		strings.Repeat("..%252F", 32) + "etc/passwd", // 大量 2 層 .. 重複
		"foo%00bar%2Fpasswd",                         // null byte + encoded slash
		"foo/.%2E/etc",                               // mixed literal + encoded
		"foo/..%2F..%2Fetc",                          // 多段 traversal
		"a/b/c/..%2F..%2F..%2Fetc%2Fpasswd",          // 多深度
		"\\..\\windows.csv",                          // Windows separator
		"%5C..%5Cwindows.csv",                        // encoded Windows separator
		strings.Repeat("%", 1024),                    // 大量 % 防 DoS
		strings.Repeat("..%2F", 1024) + "etc/passwd", // 大量 traversal + 編碼
		"foo/..%E2%80%8B/bar",                        // 含 zero-width-space encoded
	}
	for _, s := range seeds {
		f.Add(s)
	}

	v := NewPathValidator(nil)

	f.Fuzz(func(t *testing.T, s string) {
		// 兩條入口都跑 — 確保 strictPercent on/off 都不會 panic、都不會放走 traversal
		errStrict := v.ValidateFilePath(s)
		errExternal := v.ValidateExternalPath(s)

		// Decode loop 與 validatePathFormat 對齊（cap 4），驗證 invariant 2
		decoded := s
		for i := 0; i < 4; i++ {
			next, err := url.QueryUnescape(decoded)
			if err != nil || next == decoded {
				break
			}
			decoded = next
		}
		if HasTraversalElement(decoded) {
			// 任何一邊 validation 通過都是 bug
			if errStrict == nil {
				t.Fatalf("ValidateFilePath 通過了 multi-layer URL traversal 攻擊：input=%q decoded=%q",
					s, decoded)
			}
			if errExternal == nil {
				t.Fatalf("ValidateExternalPath 通過了 multi-layer URL traversal 攻擊：input=%q decoded=%q",
					s, decoded)
			}
		}
	})
}

// FuzzSanitizePath verifies two invariants on arbitrary inputs:
//
//  1. Determinism: calling SanitizePath twice on the same input always
//     produces the same output (ordered replacement table prevents map-
//     iteration drift).
//  2. No surviving traversal element: after sanitization, no path element of
//     the result is literally `..`. We use HasTraversalElement (element-based)
//     rather than strings.Contains(s, "..") because legitimate filenames may
//     contain `..` as a substring (see PreservesDoubleDotFilename).
func FuzzSanitizePath(f *testing.F) {
	// Seed corpus: cases known to exercise overlapping patterns.
	seeds := []string{
		"",
		"foo.csv",
		"./foo.csv",
		"../foo.csv",
		"....//foo.csv",
		"...///bar.txt",
		"///etc/passwd",
		"report..v2.csv",
		"input/backup..2024.csv",
		"\\..\\windows.csv",
		"foo\x00bar.csv",
		"foo\r\nbar.csv",
		".././../etc/passwd",
		"//\\\\..\\\\..//foo",
		strings.Repeat("../", 32) + "etc/passwd",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		a, aErr := SanitizePath(s)
		b, bErr := SanitizePath(s)
		// 必須 deterministic — clean / reject 結果跨呼叫不能漂移。
		if a != b || (aErr == nil) != (bErr == nil) {
			t.Fatalf("non-deterministic: %q → (%q, %v) vs (%q, %v)", s, a, aErr, b, bErr)
		}

		// 回 nil error 時 output 必須是「clean path」,不含 `..` element。
		// 回 error 時 output 必為 empty(silent rewrite 已 banned)。
		if aErr == nil {
			if HasTraversalElement(a) {
				t.Fatalf("traversal element survived clean output: input=%q output=%q", s, a)
			}
		} else if a != "" {
			t.Fatalf("rejected input must return empty path, got %q for input %q", a, s)
		}
	})
}
