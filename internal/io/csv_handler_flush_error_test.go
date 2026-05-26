package io

import (
	stderrors "errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

// failingWriter 是一個總是回 io.ErrShortWrite 的 io.Writer，用來模擬
// underlying writer 在 csv.Writer Flush 時失敗。csv.Writer 內建 bufio
// buffer (4 KiB)，所以小資料下 WriteAll 的 Flush 才會把錯誤暴露出來。
type failingWriter struct {
	err error
}

func (f *failingWriter) Write(_ []byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return 0, io.ErrShortWrite
}

// TestWriteCSVPayload_PropagatesFlushError 鎖定
// csv.Writer 的 Flush 錯誤先前依賴 named-return + defer 邏輯處理，缺少
// 顯式 writer.Error() check 容易在未來修改時被誤刪。此 test 透過 failingWriter
// 強制 Flush 失敗，驗證 writeCSVPayload 確實傳回該錯誤。
func TestWriteCSVPayload_PropagatesFlushError(t *testing.T) {
	t.Parallel()

	t.Run("FlushFailureSurfacesAsError", func(t *testing.T) {
		fw := &failingWriter{err: io.ErrShortWrite}
		data := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
		}

		err := writeCSVPayload(fw, data, false)
		require.Error(t, err, "writeCSVPayload 必須回傳 underlying writer 錯誤，不可吞掉")
		require.True(t,
			stderrors.Is(err, io.ErrShortWrite),
			"error chain 必須包含 io.ErrShortWrite，實際: %v", err)
	})

	t.Run("BOMWriteFailureSurfacesAsError", func(t *testing.T) {
		fw := &failingWriter{err: io.ErrShortWrite}
		data := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
		}

		err := writeCSVPayload(fw, data, true)
		require.Error(t, err, "BOM 寫入失敗必須傳回錯誤")
	})

	t.Run("SuccessOnNormalWriter", func(t *testing.T) {
		var buf countingWriter
		data := [][]string{
			{"Time", "Ch1"},
			{"1.0", "100"},
		}

		err := writeCSVPayload(&buf, data, false)
		require.NoError(t, err)
		require.Positive(t, buf.bytesWritten, "成功時應寫入資料")
	})
}

// countingWriter 是一個成功的 io.Writer，僅統計寫入位元組數。
type countingWriter struct {
	bytesWritten int
}

func (c *countingWriter) Write(p []byte) (int, error) {
	c.bytesWritten += len(p)
	return len(p), nil
}
