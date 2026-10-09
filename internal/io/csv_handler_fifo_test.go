//go:build !windows

package io

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	apperrors "count_mean/internal/errors"
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
		var appErr *apperrors.AppError
		require.True(t, errors.As(err, &appErr), "應為 AppError,實際 %T", err)
		require.Equal(t, apperrors.ErrCodeFileFormat, appErr.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("ReadCSV 對 FIFO 阻塞")
	}
}
