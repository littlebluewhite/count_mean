package csvutil

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"testing"
)

// errFakeWriter 是一個固定回固定錯誤的 io.Writer，用來驗證 WriteBOM 能把
// underlying writer error 透過 %w 包起來而非裸 return（wrapcheck regression）。
var errFakeWriter = errors.New("fake writer failure")

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) { return 0, errFakeWriter }

func TestWriteBOM_Success(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteBOM(&buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), BOMBytes()) {
		t.Fatalf("BOM not written verbatim, got % x want % x", buf.Bytes(), BOMBytes())
	}
}

// TestWriteBOM_WrapsUnderlyingError 釘住 cross-compare review P2 (wrapcheck):
// WriteBOM 必須把 io.Writer.Write 的 error 透過 %w 包起來，讓 caller 仍能用
// errors.Is 找回 sentinel。直接 `return err` 雖然能編譯，會在 make lint 跑
// wrapcheck 時 fail，且 caller 看不到「write BOM」的脈絡。
func TestWriteBOM_WrapsUnderlyingError(t *testing.T) {
	err := WriteBOM(failingWriter{})
	if err == nil {
		t.Fatal("expected error from failing writer, got nil")
	}
	if !errors.Is(err, errFakeWriter) {
		t.Fatalf("underlying error lost in wrap chain, got %v", err)
	}
}

func TestPeekBOM_WithBOM(t *testing.T) {
	payload := append([]byte{}, BOMBytes()...)
	payload = append(payload, []byte("header1,header2\n")...)
	r := bufio.NewReader(bytes.NewReader(payload))

	found, err := PeekBOM(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected BOM to be detected")
	}

	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read remainder: %v", err)
	}
	if string(rest) != "header1,header2\n" {
		t.Fatalf("BOM not consumed, got %q", rest)
	}
}

func TestPeekBOM_WithoutBOM(t *testing.T) {
	payload := []byte("header1,header2\n")
	r := bufio.NewReader(bytes.NewReader(payload))

	found, err := PeekBOM(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Fatal("expected no BOM")
	}

	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read remainder: %v", err)
	}
	if string(rest) != string(payload) {
		t.Fatalf("reader advanced unexpectedly, got %q want %q", rest, payload)
	}
}

// TestBOMBytes_Immutability 釘住 Wave 5 PR2 修法：BOMBytes() 必須每次回 fresh
// slice，caller 對其 mutate 不影響 package 內 canonical BOM 或其他 caller。
// 此前 `var BOM []byte` 暴露同一 backing array — 任何 caller 透過
// `csvutil.BOM[0] = 0xFF` 就能全域汙染 BOM 偵測/寫入。
func TestBOMBytes_Immutability(t *testing.T) {
	first := BOMBytes()
	second := BOMBytes()

	// 兩次呼叫得到的 slice header 必須指向不同 backing storage。
	if &first[0] == &second[0] {
		t.Fatal("BOMBytes returned aliasing slices; storage must be fresh per call")
	}

	// Mutate first；BOMBytes 再呼一次必須仍回正規 BOM。
	first[0] = 0xFF
	third := BOMBytes()
	if !bytes.Equal(third, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("canonical BOM corrupted by external mutation: got % x", third)
	}

	// WriteBOM 也必須仍寫正確 BOM (用 bomArray[:] 不受外部影響)。
	var buf bytes.Buffer
	if err := WriteBOM(&buf); err != nil {
		t.Fatalf("WriteBOM after external mutation: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("WriteBOM produced corrupted BOM after external mutation: got % x", buf.Bytes())
	}
}

func TestPeekBOM_ShorterThanBOM(t *testing.T) {
	// 檔案只有 2 bytes — Peek(3) 會回 io.EOF；應視為「無 BOM」、reader 仍可讀完。
	payload := []byte{0xEF, 0xBB}
	r := bufio.NewReader(bytes.NewReader(payload))

	found, err := PeekBOM(r)
	if err != nil {
		t.Fatalf("expected nil error for short input, got %v", err)
	}
	if found {
		t.Fatal("expected no BOM for short input")
	}

	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read remainder: %v", err)
	}
	if !bytes.Equal(rest, payload) {
		t.Fatalf("payload mutated, got % x want % x", rest, payload)
	}
}
