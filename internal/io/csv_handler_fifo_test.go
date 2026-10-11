//go:build !windows

package io

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"count_mean/internal/i18n"
)

// TestReadCSV_RejectsFIFO 釘住:名為 *.csv 的 FIFO 在 open 前就被擋下,不會阻塞。
func TestReadCSV_RejectsFIFO(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	handler := newReadCSVHandler(dir)

	p := filepath.Join(dir, "pipe.csv")
	require.NoError(t, syscall.Mkfifo(p, 0o600))

	done := make(chan error, 1)
	go func() {
		_, err := handler.ReadCSV(p)
		done <- err
	}()

	select {
	case err := <-done:
		var keyed *i18n.Error
		require.True(t, errors.As(err, &keyed), "應為 *i18n.Error,實際 %T", err)
		require.Equal(t, i18n.KeyErrorIONotRegularFile, keyed.Key)
	case <-time.After(5 * time.Second):
		t.Fatal("ReadCSV 對 FIFO 阻塞")
	}
}
