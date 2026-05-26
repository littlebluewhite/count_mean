package gui

import (
	"context"
	"errors"
	"testing"

	"count_mean/internal/io"
)

// testParams / testResult 是樣板測試用的 P / R 替身 type — 樣板對 P / R 是
// type-parameter unconstrained,實際 handler 各自決定具體型別。
type testParams struct {
	id int
}

type testResult struct {
	value int
}

// fakeCalls 用 closure 內遞增 counter 驗「Validate / Execute / WriteCSV 各被呼叫過幾次」。
// 樣板的短路語意核心是「fail-fast 不再走後續 step」,counter 是檢查它的最直接方式;
// 不檢查 logger 字串內部細節(那是 implementation detail)。
type fakeCalls struct {
	validate int
	execute  int
	writeCSV int
}

// 完整 happy path:Validate ok → Execute ok → WriteCSV ok → (result, outputPath, nil)。
func TestAnalysisHandler_FullPath_ReturnsResultAndOutputPath(t *testing.T) {
	logger, _ := newTestLogger()
	calls := &fakeCalls{}

	h := &AnalysisHandler[testParams, testResult]{
		Name:   "TestHandler",
		Logger: logger,
		CSV:    nil, // 樣板不 dereference CSV,只當 sentinel 傳給 WriteCSV closure
		Validate: func(p testParams) error {
			calls.validate++
			return nil
		},
		Execute: func(_ context.Context, p testParams) (testResult, error) {
			calls.execute++
			return testResult{value: p.id * 2}, nil
		},
		WriteCSV: func(_ *io.CSVHandler, r testResult) (string, error) {
			calls.writeCSV++
			return "out/file.csv", nil
		},
	}

	result, outputPath, err := h.Run(context.Background(), testParams{id: 21})

	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if result.value != 42 {
		t.Errorf("expected result.value=42, got %d", result.value)
	}
	if outputPath != "out/file.csv" {
		t.Errorf("expected outputPath=out/file.csv, got %q", outputPath)
	}
	if calls.validate != 1 || calls.execute != 1 || calls.writeCSV != 1 {
		t.Errorf("expected 1/1/1 calls, got V=%d E=%d W=%d",
			calls.validate, calls.execute, calls.writeCSV)
	}
}

// Validate 短路:Execute / WriteCSV 沒被呼叫;返回 (zero R, "", err)。
func TestAnalysisHandler_ValidateFails_ShortCircuitsExecuteAndWriteCSV(t *testing.T) {
	logger, _ := newTestLogger()
	calls := &fakeCalls{}
	wantErr := errors.New("invalid param")

	h := &AnalysisHandler[testParams, testResult]{
		Name:   "TestHandler",
		Logger: logger,
		Validate: func(p testParams) error {
			calls.validate++
			return wantErr
		},
		Execute: func(_ context.Context, _ testParams) (testResult, error) {
			calls.execute++
			return testResult{}, nil
		},
		WriteCSV: func(_ *io.CSVHandler, _ testResult) (string, error) {
			calls.writeCSV++
			return "", nil
		},
	}

	result, outputPath, err := h.Run(context.Background(), testParams{})

	if !errors.Is(err, wantErr) {
		t.Errorf("expected validate err, got %v", err)
	}
	if result.value != 0 {
		t.Errorf("expected zero R on validate failure, got %+v", result)
	}
	if outputPath != "" {
		t.Errorf("expected empty outputPath, got %q", outputPath)
	}
	if calls.validate != 1 {
		t.Errorf("expected Validate called once, got %d", calls.validate)
	}
	if calls.execute != 0 || calls.writeCSV != 0 {
		t.Errorf("expected Execute/WriteCSV not called on validate fail, got E=%d W=%d",
			calls.execute, calls.writeCSV)
	}
}

// Execute 短路:WriteCSV 沒被呼叫;返回 (zero R, "", err)。
func TestAnalysisHandler_ExecuteFails_ShortCircuitsWriteCSV(t *testing.T) {
	logger, _ := newTestLogger()
	calls := &fakeCalls{}
	wantErr := errors.New("execute failed")

	h := &AnalysisHandler[testParams, testResult]{
		Name:   "TestHandler",
		Logger: logger,
		Validate: func(p testParams) error {
			calls.validate++
			return nil
		},
		Execute: func(_ context.Context, _ testParams) (testResult, error) {
			calls.execute++
			return testResult{value: 99}, wantErr
		},
		WriteCSV: func(_ *io.CSVHandler, _ testResult) (string, error) {
			calls.writeCSV++
			return "", nil
		},
	}

	result, outputPath, err := h.Run(context.Background(), testParams{})

	if !errors.Is(err, wantErr) {
		t.Errorf("expected execute err, got %v", err)
	}
	if result.value != 0 {
		t.Errorf("expected zero R on execute failure, got %+v", result)
	}
	if outputPath != "" {
		t.Errorf("expected empty outputPath, got %q", outputPath)
	}
	if calls.writeCSV != 0 {
		t.Errorf("expected WriteCSV not called on execute fail, got %d", calls.writeCSV)
	}
}

// WriteCSV=nil:Execute 成功後返回 (result, "", nil);Execute 仍被呼叫一次。
func TestAnalysisHandler_WriteCSVNil_SkipsWriteAndReturnsEmptyPath(t *testing.T) {
	logger, _ := newTestLogger()
	calls := &fakeCalls{}

	h := &AnalysisHandler[testParams, testResult]{
		Name:   "TestHandler",
		Logger: logger,
		Validate: func(_ testParams) error {
			calls.validate++
			return nil
		},
		Execute: func(_ context.Context, _ testParams) (testResult, error) {
			calls.execute++
			return testResult{value: 7}, nil
		},
		WriteCSV: nil, // batch 變體(如 AnalyzeMuscleRatio)把 write 摺進 execute
	}

	result, outputPath, err := h.Run(context.Background(), testParams{})

	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if result.value != 7 {
		t.Errorf("expected result.value=7 (透傳 Execute output), got %d", result.value)
	}
	if outputPath != "" {
		t.Errorf("expected empty outputPath when WriteCSV=nil, got %q", outputPath)
	}
	if calls.execute != 1 {
		t.Errorf("expected Execute called once, got %d", calls.execute)
	}
}

// WriteCSV 回 err:返回 (result, "", err)。result 仍透傳(對應 PRD spec 第 86 行
// 「err 非 nil → 返回 (result, "", err)」)。
func TestAnalysisHandler_WriteCSVFails_ReturnsResultEmptyPathAndErr(t *testing.T) {
	logger, _ := newTestLogger()
	wantErr := errors.New("csv write failed")

	h := &AnalysisHandler[testParams, testResult]{
		Name:   "TestHandler",
		Logger: logger,
		Validate: func(_ testParams) error { return nil },
		Execute: func(_ context.Context, _ testParams) (testResult, error) {
			return testResult{value: 13}, nil
		},
		WriteCSV: func(_ *io.CSVHandler, _ testResult) (string, error) {
			return "should-be-discarded", wantErr
		},
	}

	result, outputPath, err := h.Run(context.Background(), testParams{})

	if !errors.Is(err, wantErr) {
		t.Errorf("expected writeCSV err, got %v", err)
	}
	if result.value != 13 {
		t.Errorf("expected result.value=13 (透傳 Execute output even on write err), got %d", result.value)
	}
	if outputPath != "" {
		t.Errorf("expected empty outputPath on write err, got %q", outputPath)
	}
}

// ctx identity:Execute closure 拿到的 ctx 必須是 caller 傳進 Run 的同一個。
// 用 context.WithValue 塞 sentinel 驗 — 不 sniff ctx 內部 fields。
func TestAnalysisHandler_ContextIdentity_ExecuteReceivesCallerCtx(t *testing.T) {
	logger, _ := newTestLogger()

	type ctxKey struct{}
	const sentinel = "ctx-passed-through"

	callerCtx := context.WithValue(context.Background(), ctxKey{}, sentinel)
	var seenCtx context.Context

	h := &AnalysisHandler[testParams, testResult]{
		Name:   "TestHandler",
		Logger: logger,
		Validate: func(_ testParams) error { return nil },
		Execute: func(ctx context.Context, _ testParams) (testResult, error) {
			seenCtx = ctx
			return testResult{}, nil
		},
		WriteCSV: nil,
	}

	if _, _, err := h.Run(callerCtx, testParams{}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if seenCtx == nil {
		t.Fatal("Execute did not receive ctx")
	}
	got, ok := seenCtx.Value(ctxKey{}).(string)
	if !ok || got != sentinel {
		t.Errorf("expected Execute to receive caller ctx with sentinel %q, got %v", sentinel, got)
	}
}

// Execute 內 panic:HandlerRun 的 recoverHandlerPanic 必須攔到,返回 err 為
// ErrInternalPanic wrap。Validate / Execute / WriteCSV 任何 closure panic 都應走
// 同一條 panic recovery 路徑(沿用既有 recover.go,不在樣板層重發明)。
func TestAnalysisHandler_ExecutePanics_CaughtAsInternalPanic(t *testing.T) {
	logger, _ := newTestLogger()

	h := &AnalysisHandler[testParams, testResult]{
		Name:   "PanicHandler",
		Logger: logger,
		Validate: func(_ testParams) error { return nil },
		Execute: func(_ context.Context, _ testParams) (testResult, error) {
			panic("simulated panic in execute")
		},
	}

	_, _, err := h.Run(context.Background(), testParams{})

	if err == nil {
		t.Fatal("expected non-nil err after panic, got nil")
	}
	if !errors.Is(err, ErrInternalPanic) {
		t.Errorf("expected ErrInternalPanic, got %v", err)
	}
}
