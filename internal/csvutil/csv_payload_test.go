package csvutil

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func twoRowOptions(omitBOM bool) SafeWriteOptions {
	return SafeWriteOptions{
		Header:  []string{"name", "value"},
		OmitBOM: omitBOM,
		Emit: func(emit func([]string) error) error {
			return emit([]string{"alpha", "1"})
		},
	}
}

// CSVPayload 的位元組:OmitBOM 零值寫 BOM,true 不寫;其餘位元組相同(LF 行尾)。
func TestCSVPayload_BOMSwitch(t *testing.T) {
	for _, tc := range []struct {
		omitBOM bool
		want    string
	}{
		{omitBOM: false, want: "\xEF\xBB\xBFname,value\nalpha,1\n"},
		{omitBOM: true, want: "name,value\nalpha,1\n"},
	} {
		var buf bytes.Buffer
		if err := CSVPayload(twoRowOptions(tc.omitBOM))(&buf); err != nil {
			t.Fatalf("OmitBOM=%v: %v", tc.omitBOM, err)
		}
		if buf.String() != tc.want {
			t.Errorf("OmitBOM=%v\n got=%q\nwant=%q", tc.omitBOM, buf.String(), tc.want)
		}
	}
}

// underlying writer 的錯誤不可被吞掉:BOM 寫入失敗、或(不寫 BOM 時)csv.Writer
// 收尾 Flush 失敗,都要經 %w 傳回,errors.Is 找得到原始錯誤。
func TestCSVPayload_PropagatesWriterError(t *testing.T) {
	for _, omitBOM := range []bool{false, true} {
		err := CSVPayload(twoRowOptions(omitBOM))(failingWriter{})
		if !errors.Is(err, errFakeWriter) {
			t.Errorf("OmitBOM=%v: want errFakeWriter in chain, got %v", omitBOM, err)
		}
	}
}

// 以下承接 ADR-0055 刪除的 WriteCSVAtomic 測試中屬於 payload 位元組的部分;落檔(tmp、
// rename、既有檔不動、NAME_MAX)改由 fsperm 的 AtomicWriteFile 測試釘住。

// payloadString 跑 CSVPayload(opts) 寫進 buffer,回傳位元組。
func payloadString(t *testing.T, opts SafeWriteOptions) string {
	t.Helper()
	var buf bytes.Buffer
	if err := CSVPayload(opts)(&buf); err != nil {
		t.Fatalf("CSVPayload: %v", err)
	}
	return buf.String()
}

// header 含公式前綴時以單引號 escape。
func TestCSVPayload_HeaderSanitized(t *testing.T) {
	got := payloadString(t, SafeWriteOptions{
		Header: []string{"=SUM(A1)", "normal"},
		Emit:   func(emit func([]string) error) error { return emit([]string{"v1", "v2"}) },
	})
	if !strings.Contains(got, "'=SUM(A1)") {
		t.Errorf("header should be sanitized with leading apostrophe, got %q", got)
	}
}

// body row 也要 sanitize(不只 header):caller 提供的資料可能含 user-controlled 的
// phase label、subject 名稱或 round-trip 的 channel header,經 CSV injection 流到 Excel/Numbers。
func TestCSVPayload_BodySanitize(t *testing.T) {
	got := payloadString(t, SafeWriteOptions{
		Header: []string{"label", "value"},
		Emit: func(emit func([]string) error) error {
			for _, r := range [][]string{
				{"=cmd|'/c calc'!A1", "1"},
				{"+1+1", "2"},
				{"@SUM(A1)", "3"},
				{"-2+3", "4"},
				{"\t=danger", "5"},
				{"normal", "6"},
				{"-3.5", "7"},
			} {
				if err := emit(r); err != nil {
					return err
				}
			}
			return nil
		},
	})
	for _, want := range []string{"'=cmd|'/c calc'!A1", "'+1+1", "'@SUM(A1)", "'-2+3", "'\t=danger"} {
		if !strings.Contains(got, want) {
			t.Errorf("body row not sanitized: missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "'normal") {
		t.Errorf("safe cell should not be modified, got %q", got)
	}
	if !strings.Contains(got, "-3.5,7") {
		t.Errorf("pure numeric -3.5 should not be escaped, got %q", got)
	}
}

// Header / Emit 為 nil 是 caller 錯誤,必須回錯而不是寫出半個 CSV。
func TestCSVPayload_NilHeaderOrEmit_Errors(t *testing.T) {
	noop := func(func([]string) error) error { return nil }
	for name, opts := range map[string]SafeWriteOptions{
		"nil header": {Header: nil, Emit: noop},
		"nil emit":   {Header: []string{"a"}, Emit: nil},
	} {
		var buf bytes.Buffer
		if err := CSVPayload(opts)(&buf); err == nil {
			t.Errorf("%s: expected error", name)
		}
		if buf.Len() != 0 {
			t.Errorf("%s: 不應寫出任何位元組, got %q", name, buf.String())
		}
	}
}

// Emit 回的 error 原樣傳回(errors.Is 找得到),AtomicWriteFile 據此 abort。
func TestCSVPayload_EmitErrorPropagates(t *testing.T) {
	wantErr := errors.New("simulated emit failure")
	err := CSVPayload(SafeWriteOptions{
		Header: []string{"name"},
		Emit: func(emit func([]string) error) error {
			if err := emit([]string{"alpha"}); err != nil {
				return err
			}
			return wantErr
		},
	})(io.Discard)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr propagated, got %v", err)
	}
}

// midWriteFailingWriter 模擬 disk full / pipe broken 等 underlying IO 失敗 — 寫了
// failAfter bytes 後所有 Write 都返回 errInjected。
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
	allowed := min(w.failAfter-w.written, len(p))
	n, err := w.inner.Write(p[:allowed])
	w.written += n
	if err != nil {
		return n, err
	}
	// 沒寫完整個 p:回部分寫入 + error,模擬 short-write 引發失敗。
	if n < len(p) {
		return n, w.errInjected
	}
	return n, nil
}

// underlying writer 中途失敗時,CSVPayload 必須在 Emit 跑完之前就 abort(csv.Writer
// 的 buffer flush 失敗,或每 midWriteErrorCheckInterval 列的 Flush+Error 探測),
// 而不是一路 emit 到末段 Flush 才察覺 —— 否則大量 row 已寫進半成品 tmp。
func TestCSVPayload_AbortsOnMidWriteError(t *testing.T) {
	injected := errors.New("simulated disk full")
	w := &midWriteFailingWriter{inner: io.Discard, failAfter: 100, errInjected: injected}

	// 遠超過 midWriteErrorCheckInterval(1024)列,遠超 100 bytes 失敗門檻。
	const emitCount = 1500
	emitted := 0
	err := CSVPayload(SafeWriteOptions{
		Header: []string{"col"},
		Emit: func(emit func([]string) error) error {
			for range emitCount {
				if emitErr := emit([]string{"value"}); emitErr != nil {
					return emitErr
				}
				emitted++
			}
			return nil
		},
	})(w)

	if !errors.Is(err, injected) {
		t.Fatalf("error chain 應含 injected error, got %v", err)
	}
	if emitted >= emitCount {
		t.Errorf("underlying 失敗後應立即 abort, 實際 emit 了全部 %d 列", emitted)
	}
}
