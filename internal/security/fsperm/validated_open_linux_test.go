//go:build linux

package fsperm_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"

	"count_mean/internal/security/fsperm"
)

// probeOpenat2Supported 檢查當前 kernel 是否實作 openat2 (Linux 5.6+)。
// 對 "/" 開 dirfd 後試呼一次 openat2;若 ENOSYS 表示舊 kernel 或 LSM 拒絕,
// 此時 openValidated / openReadValidated 會 fallback 到 os.OpenFile,
// 而 os.OpenFile 已由 Go stdlib 自動設 CLOEXEC — 本 test 守的是 raw syscall
// 路徑,故在 ENOSYS 機台直接 skip。
func probeOpenat2Supported(t *testing.T) {
	t.Helper()
	dirfd, err := unix.Open("/", unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("openat2 probe: open root dirfd failed: %v", err)
	}
	defer func() { _ = unix.Close(dirfd) }()

	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_CLOEXEC),
		Resolve: unix.RESOLVE_BENEATH,
	}
	fd, err := unix.Openat2(dirfd, ".", how)
	if err != nil {
		if errors.Is(err, unix.ENOSYS) {
			t.Skip("openat2 not supported on this kernel (< 5.6 or LSM blocked); " +
				"CLOEXEC contract on the openat2 happy-path cannot be exercised here")
		}
		t.Fatalf("openat2 probe failed unexpectedly: %v", err)
	}
	_ = unix.Close(fd)
}

// TestOpenat2_SetsCloexec 釘住 修補:Linux 5.6+ openat2 happy path 開出
// 的資料檔 fd **必須**設 FD_CLOEXEC,避免 fork-exec (例如 Wails spawn webview /
// helper) 時子行程 inherit 該 fd 後繞過 0600 owner-only 權限改寫應用私有檔。
//
// 跨平台殘餘風險:darwin / windows / other_unix / Linux ENOSYS fallback 都走
// os.OpenFile,Go stdlib 已自動設 CLOEXEC — 本 test 守的就是 openat2 raw syscall
// 這條唯一漏網路徑。Test 透過公開 API (OpenWriteValidated / OpenReadValidated)
// 開檔以模擬真實 caller,而非直接呼 unix.Openat2 (那只會 test syscall 本身)。
func TestOpenat2_SetsCloexec(t *testing.T) {
	t.Parallel()
	probeOpenat2Supported(t)

	baseDir := t.TempDir()

	t.Run("write_fd_has_FD_CLOEXEC", func(t *testing.T) {
		t.Parallel()
		target := filepath.Join(baseDir, "write_cloexec.csv")
		f, err := fsperm.OpenWriteValidated(target, []string{baseDir})
		if err != nil {
			t.Fatalf("OpenWriteValidated failed: %v", err)
		}
		defer func() {
			_ = f.Close()
			_ = os.Remove(target)
		}()

		flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFD, 0)
		if err != nil {
			t.Fatalf("FcntlInt(F_GETFD) failed: %v", err)
		}
		if flags&unix.FD_CLOEXEC == 0 {
			t.Fatalf("write fd 缺 FD_CLOEXEC (flags=0x%x);openat2 happy-path 必須 OR "+
				"O_CLOEXEC 進 OpenHow.Flags 以避免 fork-exec 洩漏資料 fd 給子行程", flags)
		}
	})

	t.Run("read_fd_has_FD_CLOEXEC", func(t *testing.T) {
		// read 端對稱:先在 baseDir 預植檔,再以 OpenReadValidated 開,assert CLOEXEC。
		target := filepath.Join(baseDir, "read_cloexec.csv")
		if err := os.WriteFile(target, []byte("seed"), 0o600); err != nil {
			t.Fatalf("setup: WriteFile failed: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(target) })

		f, err := fsperm.OpenReadValidated(target, []string{baseDir})
		if err != nil {
			t.Fatalf("OpenReadValidated failed: %v", err)
		}
		defer func() { _ = f.Close() }()

		flags, err := unix.FcntlInt(f.Fd(), unix.F_GETFD, 0)
		if err != nil {
			t.Fatalf("FcntlInt(F_GETFD) failed: %v", err)
		}
		if flags&unix.FD_CLOEXEC == 0 {
			t.Fatalf("read fd 缺 FD_CLOEXEC (flags=0x%x);openat2 read happy-path 必須 OR "+
				"O_CLOEXEC 進 OpenHow.Flags 以與 write 端對稱守住 fork-exec 漏洞", flags)
		}
	})
}
