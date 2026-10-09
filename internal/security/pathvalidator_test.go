package security

import (
	"errors"
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

func TestIsCSVFile(t *testing.T) {
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
			if got := IsCSVFile(tt.path); got != tt.want {
				t.Errorf("IsCSVFile() = %v, want %v", got, tt.want)
			}
		})
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

	// Case 2：symlink 目錄下尚未存在的 child path。
	// fallback 邏輯應走 parent symlink resolve 到 /private/etc，再 join "child_dir"
	// 後命中 sensitive prefix。(目錄本身的驗證見 ValidateExternalDir 的測試。)
	dummyChild := filepath.Join(linkPath, "child_dir")
	if err := validator.ValidateExternalPath(dummyChild); err == nil {
		t.Errorf("ValidateExternalPath(%q) 應 reject symlink-to-sensitive 的 dummy-child 驗證形式，got nil error",
			dummyChild)
	} else if !errors.Is(err, ErrSensitiveDirectory) {
		t.Errorf("ValidateExternalPath(%q) 應回 ErrSensitiveDirectory，got %v", dummyChild, err)
	}
}

// TestPathValidator_ValidateExternalPath_AllowsNonExistentChildOfSafeParent 確認
// 修法後對「未存在的 output path」仍能正常通過 — typical case 是 config
// output 寫檔目標常是尚未存在的 child path。
// 若 fallback 邏輯壞掉、把未存在的 child 視為 error，會把所有寫檔驗證都打掛。
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

// 含字面 `%` 的合法檔名(`report 50%.csv` / BTS 匯出檔)必須放行。其他守門
// (element traversal、敏感目錄、symlink resolve)仍生效。
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

// 路徑不做 URL-decode:檔名裡的編碼序列(`%2E%2E`、`%2F`、`%65tc`、`+`)一律是字面字元。
// 它們不會被還原成 `..` 或 `/etc`,驗證通過的路徑仍落在原目錄內;真正的 `..` element
// 與敏感目錄仍然擋。
func TestValidateExternalPath_EncodedSequencesAreLiteral(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)
	tmpRoot := t.TempDir()

	accepted := []string{
		"%2e%2e%2fetc%2fpasswd.csv",
		"%2E%2E.csv",
		"..%2Fetc%2Fpasswd.csv",
		"%65tc%2Fpasswd.csv",
		"a%252Fb.csv",
		"a+b.csv",
	}
	for _, name := range accepted {
		t.Run("accept/"+name, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(tmpRoot, name)
			if err := validator.ValidateExternalPath(p); err != nil {
				t.Fatalf("ValidateExternalPath(%q) 編碼序列應視為字面字元放行，實際 err=%v", p, err)
			}
			abs, err := validator.validatePathFormat(p)
			if err != nil {
				t.Fatalf("validatePathFormat(%q) err=%v", p, err)
			}
			if filepath.Dir(abs) != tmpRoot || filepath.Base(abs) != name {
				t.Errorf("驗證後的路徑應仍是 %q 下的字面檔名 %q，實際 %q", tmpRoot, name, abs)
			}
		})
	}

	// 編碼序列作為「目錄」元素同樣是字面。
	t.Run("accept/encoded_dir_element", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tmpRoot, "%2E%2E", "%65tc", "data.csv")
		if err := validator.ValidateExternalPath(p); err != nil {
			t.Errorf("ValidateExternalPath(%q) 應放行，實際 err=%v", p, err)
		}
	})

	rejected := []string{
		tmpRoot + "/../data.csv",
		"/etc/passwd",
	}
	for _, p := range rejected {
		t.Run("reject/"+p, func(t *testing.T) {
			t.Parallel()
			if err := validator.ValidateExternalPath(p); err == nil {
				t.Errorf("ValidateExternalPath(%q) 仍應拒絕，實際通過", p)
			}
		})
	}
}

// 放行字面 `%` 不可順便放走 traversal — 即使 path 含字面 %,含 `..` 路徑元素
// 仍必須擋。
func TestPathValidator_ExternalPath_StillRejectsTraversalEvenWithPercent(t *testing.T) {
	t.Parallel()

	validator := NewPathValidator(nil)

	cases := []string{
		"/tmp/../etc/50% target.csv",
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

// Fuzz target:用 URL-encoded traversal payload + unicode mutation 持續攻擊
// validatePathFormat(經 ValidateFilePath / ValidateExternalPath 兩條入口)。
//
// 不變式:
//  1. 任意 input 不得 panic
//  2. 路徑不做 URL-decode;input 本身含 `..` 路徑元素時,兩條入口都必須擋下。
//     編碼過的 `%2E%2E` 是字面字元,不在此不變式內(見
//     TestValidateExternalPath_EncodedSequencesAreLiteral)。
func FuzzValidatePathFormat(f *testing.F) {
	// Seed corpus:URL-encoded traversal 變體(現在應視為字面)與真正的 `..`。
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
		// 兩條入口都跑 — 確保都不會 panic、都不會放走 traversal
		errStrict := v.ValidateFilePath(s)
		errExternal := v.ValidateExternalPath(s)

		if HasTraversalElement(s) {
			if errStrict == nil {
				t.Fatalf("ValidateFilePath 通過了含 `..` element 的 input：%q", s)
			}
			if errExternal == nil {
				t.Fatalf("ValidateExternalPath 通過了含 `..` element 的 input：%q", s)
			}
		}
	})
}

// TestPerformBasicSecurityChecks_AppDataPolicy_CrossPlatform 鎖死 Windows
// `\AppData\` 的 carve-out 策略:
//
//   - **預設擋** 整個 `\AppData\` (含 Roaming / LocalLow / Local\Microsoft\Credentials 等
//     真實 credential / persistence 入口)
//   - **唯一例外**:`\AppData\Local\Temp\` 子樹放行 (Windows runner t.TempDir() 落點,
//     等同 macOS /private/var/folders/,合法 test fixture + GUI 暫存)
//
// 對應 doc:performBasicSecurityChecks 註解中的設計權衡。
//
// 本 test 故意餵 hardcoded Windows-style 字串樣本,讓 macOS/Linux CI 也能驗到
// 此策略 — 利用 normalizePath 把 `\` 轉 `/` 後雙端正規化的設計(見
// pathvalidator.go 內 normalizePath 註解)。沒有此 test,fix 只有 Windows CI
// 能 catch regression,違反 7682042→PR #5 學到的「跨平台 test 必須在所有 OS 跑」教訓。
func TestPerformBasicSecurityChecks_AppDataPolicy_CrossPlatform(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		path      string
		wantBlock bool // true: 應被擋(ErrSensitiveDirectory),false: 應通過
	}{
		// **通過** — \AppData\Local\Temp\ 是 Windows runner t.TempDir() 落點,
		// 等同 macOS /private/var/folders/,合法 test fixture + GUI 暫存。
		{
			name:      "AppData_Local_Temp_runner_TempDir_allowed",
			path:      `C:\Users\RUNNER~1\AppData\Local\Temp\TestXxx\001\data.csv`,
			wantBlock: false,
		},
		// **通過** — Temp 子樹下任意深度的合法 fixture 都該放行。
		{
			name:      "AppData_Local_Temp_deep_subdir_allowed",
			path:      `C:\Users\wilson\AppData\Local\Temp\TestFoo\Bar\Baz\data.csv`,
			wantBlock: false,
		},
		// **擋** — \AppData\Local\Microsoft\ 下的 Credentials / Vault 才是真敏感區。
		{
			name:      "AppData_Local_Microsoft_Credentials_blocked",
			path:      `C:\Users\wilson\AppData\Local\Microsoft\Credentials\stored_token`,
			wantBlock: true,
		},
		// **擋** — \AppData\Roaming\ 是漫遊應用程式設定,malware persistence 入口。
		{
			name:      "AppData_Roaming_blocked",
			path:      `C:\Users\wilson\AppData\Roaming\Evil\persist.exe`,
			wantBlock: true,
		},
		// **擋** — \AppData\LocalLow\ 是 low-integrity sandbox 區(IE/Edge protected mode)。
		{
			name:      "AppData_LocalLow_blocked",
			path:      `C:\Users\wilson\AppData\LocalLow\Sandbox\leak.dat`,
			wantBlock: true,
		},
		// **擋** — 非 C: drive 的 Roaming(VM / 多 drive 配置)。
		{
			name:      "AppData_Roaming_non_C_drive_blocked",
			path:      `D:\Users\wilson\AppData\Roaming\Evil\persist.exe`,
			wantBlock: true,
		},
		// **擋** — 大小寫混用 + forward-slash 變體(對齊 normalizePath 的 ToLower + ToSlash)。
		{
			name:      "AppData_Roaming_case_and_slash_variant_blocked",
			path:      `c:/users/victim/AppData/Roaming/secret`,
			wantBlock: true,
		},
		// **通過** — forward-slash 變體的 Temp 子樹仍放行。
		{
			name:      "AppData_Local_Temp_forward_slash_allowed",
			path:      `C:/Users/runner/AppData/Local/Temp/test001/data.csv`,
			wantBlock: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := performBasicSecurityChecks(tc.path)
			gotBlock := err != nil && errors.Is(err, ErrSensitiveDirectory)
			if tc.wantBlock && !gotBlock {
				t.Errorf("path %q 應被 ErrSensitiveDirectory 擋,實際 err=%v", tc.path, err)
			}
			if !tc.wantBlock && err != nil {
				t.Errorf("path %q 應通過,實際被擋:err=%v", tc.path, err)
			}
		})
	}
}

// 名稱以 `..` 開頭但不是 traversal element 的子項(`..foo`、`foo..bar`)是 base 內的合法
// 子路徑。舊 isPathWithinBase 用 HasPrefix(rel, "..") 比對,把 `..foo` 誤拒。
func TestValidateFilePath_AcceptsChildNamedDotDotPrefix(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	validator := NewPathValidator([]string{base})

	for _, name := range []string{"..foo", "..foo/data.csv", "foo..bar"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := validator.ValidateFilePath(filepath.Join(base, name)); err != nil {
				t.Fatalf("ValidateFilePath(base/%s) 應通過,got %v", name, err)
			}
		})
	}
}
