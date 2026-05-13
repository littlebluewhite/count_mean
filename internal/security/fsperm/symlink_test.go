//go:build !windows

package fsperm_test

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestFsperm_FlagsRejectSymlink 驗證 WriteFlags / AppendFlags / ReadFlags 三組
// flag 都會在 OpenFile 階段被 kernel 以 ELOOP 拒絕含 symlink 的路徑。
//
// 攻擊場景：攻擊者在 OutputDir / logDir / inputDir 預先植入指向 /etc/passwd
// 或 cron 配置檔的 symlink；若程式 OpenFile 不帶 O_NOFOLLOW 就會跟到底，
// 對寫入端造成 silent overwrite、對讀取端造成資料外洩。加上 O_NOFOLLOW 後
// kernel 直接拒絕。
//
// 此測試與 test/unit/csv/csv_handler_symlink_test.go 對 WriteCSV 高階 API 的
// 對稱驗證 — 這裡保證底層 flag 本身正確；後者保證 wrapper 沒漏接 flag。
func TestFsperm_FlagsRejectSymlink(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		flags int
	}{
		{"WriteFlags", fsperm.WriteFlags},
		{"AppendFlags", fsperm.AppendFlags},
		{"ReadFlags", fsperm.ReadFlags},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			target := filepath.Join(tempDir, "real.txt")
			if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
				t.Fatalf("setup: WriteFile failed: %v", err)
			}
			symlinkPath := filepath.Join(tempDir, "link.txt")
			if err := os.Symlink(target, symlinkPath); err != nil {
				t.Fatalf("setup: Symlink failed: %v", err)
			}

			f, err := os.OpenFile(symlinkPath, tc.flags, fsperm.FilePerm)
			if err == nil {
				_ = f.Close()
				t.Fatalf("%s: OpenFile through symlink should fail, got nil error", tc.name)
			}

			// 期待 ELOOP（darwin/linux 對 O_NOFOLLOW + symlink 的回應）。
			if !errors.Is(err, syscall.ELOOP) {
				t.Errorf("%s: expected ELOOP, got: %v", tc.name, err)
			}

			// 對 read / append flag，原檔案不該被任何寫入動作改動。
			got, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatalf("verify: ReadFile failed: %v", readErr)
			}
			if string(got) != "untouched" {
				t.Errorf("%s: target file was modified; expected 'untouched', got %q", tc.name, string(got))
			}
		})
	}
}

// TestWriteFileNoFollow_RejectsSymlink 驗證 WriteFileNoFollow helper 在 symlink
// 路徑下會 fail（不會 silently 跟 symlink 寫入目標檔案）。helper 是
// chart/cci/i18n 三處 WriteFile callsite 的統一封裝，必須帶 NOFOLLOW。
func TestWriteFileNoFollow_RejectsSymlink(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	target := filepath.Join(tempDir, "victim.json")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatalf("setup: WriteFile failed: %v", err)
	}
	symlinkPath := filepath.Join(tempDir, "out.json")
	if err := os.Symlink(target, symlinkPath); err != nil {
		t.Fatalf("setup: Symlink failed: %v", err)
	}

	if err := fsperm.WriteFileNoFollow(symlinkPath, []byte("overwrite-attempt")); err == nil {
		t.Fatalf("WriteFileNoFollow through symlink should fail, got nil error")
	} else if !errors.Is(err, syscall.ELOOP) {
		t.Errorf("expected ELOOP, got: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("verify: ReadFile failed: %v", err)
	}
	if string(got) != "untouched" {
		t.Errorf("victim file was overwritten; expected 'untouched', got %q", string(got))
	}
}
