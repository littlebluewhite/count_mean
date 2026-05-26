package csvutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// midWriteFailingWriter 模擬 disk full / pipe broken 等 underlying IO 失敗 — 寫了
// failAfter bytes 後所有 Write 都返回 errInjected,但保留 nWritten 計數讓 test
// 驗證 的 mid-write 探測有觸發。
type midWriteFailingWriter struct {
	inner       io.Writer
	written     int
	failAfter   int
	errInjected error
}

func (w *midWriteFailingWriter) Write(p []byte) (int, error) {
	if w.written >= w.failAfter {
		return 0, w.errInjected
	}
	// 寫入直到達到 failAfter,然後下次 Write 返回 error。
	allowed := w.failAfter - w.written
	if allowed > len(p) {
		allowed = len(p)
	}
	n, err := w.inner.Write(p[:allowed])
	w.written += n
	if err != nil {
		return n, err
	}
	// 如果還沒寫完整個 p,返回部分寫入 + error 模擬 short-write 引發失敗。
	if n < len(p) {
		return n, w.errInjected
	}
	return n, nil
}

// TestWriteCSVAtomic_AbortsOnMidWriteError 是 主守:
//
// 過去 WriteCSVAtomic 只在末段 writer.Flush + writer.Error 探測 underlying IO error,
// 若 caller 一口氣 emit 萬筆 row 中途 disk 滿,error 只在 final Flush 才察覺,
// 大量 row 已寫進半成品 tmp。修法是每 1024 row 主動 Flush + Error() 探測,
// 一旦失敗立刻 abort + defer cleanup 刪 tmp。
//
// 此 test 注入 writerWrapHook 把 underlying *os.File 包成 midWriteFailingWriter,
// 寫了 100 bytes 後返回固定 error。然後 emit 超過 midWriteErrorCheckInterval
// (1024) 筆 row — fix 前:emit 結束才察覺,但 tmp 已寫一堆;
// fix 後:第 1024 row 處 Flush+Error 探測到 error,立即 return + defer 刪 tmp。
//
// 主要 assertion:
//  1. WriteCSVAtomic 返回 non-nil error,且 error 是 mid-write 引發
//  2. final path 不存在(沒成功 rename)
//  3. tmp file 不殘留(defer cleanup 成功)
func TestWriteCSVAtomic_AbortsOnMidWriteError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	injected := errors.New("simulated disk full")

	// 註冊 hook 包裝 underlying writer — 100 bytes 後寫入失敗。
	// 100 bytes 內可以容納 header + BOM,但 emit 大量 row 後必撞線。
	t.Cleanup(func() { writerWrapHook = nil })
	writerWrapHook = func(w io.Writer) io.Writer {
		return &midWriteFailingWriter{
			inner:       w,
			failAfter:   100,
			errInjected: injected,
		}
	}

	// emit 遠超過 midWriteErrorCheckInterval (1024) 筆 row。
	// 每 row 大約 10-20 bytes,1500 row 約 15-30KB,遠超 100 bytes 失敗門檻,
	// 必然在 mid-write 探測時觸發 abort。
	const emitCount = 1500
	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"col"},
		Emit: func(emit func([]string) error) error {
			for i := 0; i < emitCount; i++ {
				if emitErr := emit([]string{"value"}); emitErr != nil {
					return emitErr
				}
			}
			return nil
		},
	})

	if err == nil {
		t.Fatal("WriteCSVAtomic 應在 underlying IO error 時 abort,return non-nil error")
	}
	// 不要求 err 直接 == injected,因為各層 wrap 後可能帶不同 path。但 error
	// chain 上應該能找到 injected 或至少有 mid-write / disk 字樣。
	if !errors.Is(err, injected) && !strings.Contains(err.Error(), "mid-write") && !strings.Contains(err.Error(), "simulated disk full") {
		t.Errorf("error 應反映 mid-write 探測失敗或 underlying injected error,got: %v", err)
	}

	// 1) Final path 不該存在 — atomic write 在失敗時不會 commit。
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("失敗時 final path 不該存在,stat err=%v", statErr)
	}

	// 2) 不該有 tmp file 殘留 — defer cleanup 應該刪掉。
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("ReadDir 失敗: %v", readErr)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), filepath.Base(path)) {
			t.Errorf("失敗 path 後不該有 tmp orphan: %s", e.Name())
		}
	}
}
