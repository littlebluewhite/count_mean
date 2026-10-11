//go:build !windows

package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"count_mean/internal/security/fsperm"
)

// TestLoadManifests_RejectsFIFO 釘住 ADR-0056:名為 *.csv 的 FIFO 分期總檔案在開檔門被擋下
// (fsperm.ErrNotRegular),不會阻塞,也不會被當成空檔解析。
func TestLoadManifests_RejectsFIFO(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "manifest.csv")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("setup: Mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := LoadManifests(path)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, fsperm.ErrNotRegular) {
			t.Fatalf("err = %v; want errors.Is(fsperm.ErrNotRegular)", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadManifests 對 FIFO 阻塞")
	}
}

// TestOpenDataFile_RejectsFIFO 釘住 ADR-0056:OpenDataFile 開檔後 fstat,FIFO 資料檔回
// ErrManifestDataFilePathInvalid + fsperm.ErrNotRegular,且不交出 fd。
func TestOpenDataFile_RejectsFIFO(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(base, "pipe.csv"), 0o600); err != nil {
		t.Fatalf("setup: Mkfifo: %v", err)
	}

	type result struct {
		f   *os.File
		err error
	}
	done := make(chan result, 1)
	go func() {
		f, err := OpenDataFile(base, "pipe.csv")
		done <- result{f, err}
	}()

	select {
	case r := <-done:
		if r.f != nil {
			_ = r.f.Close()
			t.Error("OpenDataFile 失敗時必須回 nil file")
		}
		if !errors.Is(r.err, ErrManifestDataFilePathInvalid) || !errors.Is(r.err, fsperm.ErrNotRegular) {
			t.Fatalf("err = %v; want ErrManifestDataFilePathInvalid + fsperm.ErrNotRegular", r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OpenDataFile 對 FIFO 阻塞")
	}
}
