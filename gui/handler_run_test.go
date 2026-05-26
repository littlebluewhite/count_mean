package gui

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"count_mean/internal/logging"
)

// newTestLogger 建一個寫進 buf 的 INFO-level logger,用來 count Info call 次數。
// `[INFO]` token 由 logger.writeText 寫進 entry.Level,每筆 Info call 對應一筆。
func newTestLogger() (*logging.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return logging.NewLogger(logging.LevelInfo, &buf, false), &buf
}

func countInfoCalls(logOutput string) int {
	return strings.Count(logOutput, "[INFO]")
}

// HandlerRun 樣板正常 path:body 返回 (R, nil),透傳,且 entry + exit 各打一次 Info。
func TestHandlerRun_BodySucceeds_PassesThroughAndLogsTwice(t *testing.T) {
	logger, buf := newTestLogger()

	want := 42
	got, err := HandlerRun(logger, "TestHandler", func() (int, error) {
		return want, nil
	})

	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if got != want {
		t.Errorf("expected result %d, got %d", want, got)
	}

	calls := countInfoCalls(buf.String())
	if calls != 2 {
		t.Errorf("expected 2 Info calls (entry + exit), got %d.\nlog:\n%s", calls, buf.String())
	}
}

// HandlerRun 樣板錯誤 path:body 回 (R, err),透傳 err,只打 entry log(不打 exit log)。
func TestHandlerRun_BodyReturnsErr_PassesThroughAndLogsOnce(t *testing.T) {
	logger, buf := newTestLogger()

	wantErr := errors.New("ok-path failure")
	got, err := HandlerRun(logger, "TestHandler", func() (int, error) {
		return 7, wantErr
	})

	if !errors.Is(err, wantErr) {
		t.Errorf("expected err to wrap %v, got %v", wantErr, err)
	}
	// 透傳:body 回的 result(即使 err != nil)也要傳出 — contract-neutral
	if got != 7 {
		t.Errorf("expected result 7 (透傳), got %d", got)
	}

	calls := countInfoCalls(buf.String())
	if calls != 1 {
		t.Errorf("expected 1 Info call (entry only, no exit on err), got %d.\nlog:\n%s",
			calls, buf.String())
	}
}

// HandlerRun 樣板 panic path:body panic 被 recoverHandlerPanic 攔到,err 為
// ErrInternalPanic wrap;原 named-return err sentinel 仍可走 errors.Is 走訪
// (panicErrWithChain 雙 chain 沿用,不重寫 panic recovery)。
func TestHandlerRun_BodyPanics_CaughtAsInternalPanic(t *testing.T) {
	logger, _ := newTestLogger()

	_, err := HandlerRun(logger, "PanicHandler", func() (int, error) {
		panic("simulated panic in body")
	})

	if err == nil {
		t.Fatal("expected non-nil err after panic, got nil")
	}
	if !errors.Is(err, ErrInternalPanic) {
		t.Errorf("expected ErrInternalPanic wrap, got %v", err)
	}
}

// HandlerRun panic 後返回的 err 必須是 panicErrWithChain wrap 型別,確認樣板層
// 沒有破壞 recoverHandlerPanic 既有的雙 chain 機制(dual chain `Unwrap() []error`
// 由 gui/recover_test.go 在 native recoverHandlerPanic 路徑驗,此處只 spot-check
// HandlerRun 拿到的回傳沒有把 wrap 改寫成單 chain 形態)。
//
// 注意:HandlerRun 的 body 是 closure 沒有 named-return err exposure 給 outer,
// 「pre-panic err sentinel reachable」的 dual-chain 完整路徑在樣板層級不可達
// (recoverHandlerPanic 取的 *errPtr 是 HandlerRun 自己的 named err,在 panic
// 之前沒被 body 修改的機會,因此 original 必為 nil)。完整 dual-chain 詳細語意
// 由 gui/recover_test.go 守。
func TestHandlerRun_PanicReturnsChainWrapper(t *testing.T) {
	logger, _ := newTestLogger()

	_, err := HandlerRun(logger, "PanicChainHandler", func() (int, error) {
		panic(errors.New("panic with err value"))
	})

	if err == nil {
		t.Fatal("expected non-nil err after panic, got nil")
	}
	if !errors.Is(err, ErrInternalPanic) {
		t.Errorf("expected ErrInternalPanic in chain, got %v", err)
	}

	var chainErr *panicErrWithChain
	if !errors.As(err, &chainErr) {
		t.Errorf("expected err to wrap *panicErrWithChain (dual-chain wrapper preserved), got %T: %v",
			err, err)
	}
}
