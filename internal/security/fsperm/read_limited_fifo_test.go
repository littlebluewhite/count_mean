//go:build !windows

package fsperm

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// TestOpenRegularLimited_RejectsFIFO 釘住:FIFO 在 pre-stat 就被擋(ErrNotRegular),不會阻塞。
func TestOpenRegularLimited_RejectsFIFO(t *testing.T) {
	t.Parallel()

	p := filepath.Join(t.TempDir(), "pipe.csv")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatalf("setup: Mkfifo: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := OpenRegularLimited(p, 10)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrNotRegular) {
			t.Fatalf("err = %v; want ErrNotRegular", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OpenRegularLimited 對 FIFO 阻塞")
	}
}
