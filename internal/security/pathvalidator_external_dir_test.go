package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestPathValidator_ValidateExternalDir 釘住目錄語意:與 ValidateExternalPath 擋同一組
// 系統敏感位置(含目錄根本身,結尾無 slash),且不把檔案專屬規則(Windows reserved
// device name)套到目錄名上。
func TestPathValidator_ValidateExternalDir(t *testing.T) {
	t.Parallel()

	v := NewPathValidator(nil)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	rejected := []struct {
		name string
		dir  string
		want error
	}{
		{"etc root", "/etc", ErrSensitiveDirectory},
		{"etc child", "/etc/ssl", ErrSensitiveDirectory},
		{"home .ssh root", filepath.Join(home, ".ssh"), ErrSensitiveDirectory},
		{"traversal", "../../etc", ErrPathTraversal},
		{"NUL byte", "out\x00put", ErrPathContainsNUL},
	}
	for _, tt := range rejected {
		t.Run("reject/"+tt.name, func(t *testing.T) {
			t.Parallel()
			if err := v.ValidateExternalDir(tt.dir); !errors.Is(err, tt.want) {
				t.Errorf("ValidateExternalDir(%q) = %v, want errors.Is %v", tt.dir, err, tt.want)
			}
		})
	}

	base := t.TempDir()
	accepted := []string{
		"",                             // caller 端自行處理空值
		base,                           // 一般目錄
		filepath.Join(base, "not-yet"), // 尚未存在的子目錄
		filepath.Join(base, "CON"),     // reserved device name 是檔名規則,不套用到目錄
		"/etc-backup",                  // 不與 `/etc/` 前綴誤命中
		filepath.Join(base, "a..b"),    // 含字面雙點的合法目錄名
	}
	for _, dir := range accepted {
		if err := v.ValidateExternalDir(dir); err != nil {
			t.Errorf("ValidateExternalDir(%q) 應放行,got %v", dir, err)
		}
	}
}

// TestPathValidator_ValidateExternalDir_RejectsSymlinkToSensitive:symlink 指向 /etc 的
// 目錄,resolve 後仍須被擋(目錄根無結尾 slash 也不得漏)。
func TestPathValidator_ValidateExternalDir_RejectsSymlinkToSensitive(t *testing.T) {
	t.Parallel()

	link := filepath.Join(t.TempDir(), "etc-link")
	if err := os.Symlink("/etc", link); err != nil {
		t.Skipf("無法建立 symlink: %v", err)
	}
	if err := NewPathValidator(nil).ValidateExternalDir(link); !errors.Is(err, ErrSensitiveDirectory) {
		t.Errorf("ValidateExternalDir(symlink→/etc) = %v, want ErrSensitiveDirectory", err)
	}
}
