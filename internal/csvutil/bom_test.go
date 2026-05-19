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

// TestPeekBOM_UTF16BOM_NotDetected 鎖定 修法：BOM 偵測**僅支援 UTF-8** (0xEF 0xBB 0xBF)。
// UTF-16 LE BOM (0xFF 0xFE) 與 UTF-16 BE BOM (0xFE 0xFF) 在此 package 視為「無 BOM」，
// reader 不會 discard 任何 byte，原始 byte 保留供後續 reader 自行處理（或失敗）。
//
// 此測試把當前行為**鎖在規約裡**，避免日後有人嘗試「順手支援」UTF-16 BOM 而破壞 caller
// 對 byte-preserving 的期待。若真要支援 UTF-16，需要 golang.org/x/text/transform 做
// encoding 轉換，是另一個 ticket。
//
// L20:此 test 原本還列 utf32_le_bom / utf32_be_bom case,但 PeekBOM 只 Peek 3 byte,
// UTF-32 BOM 是 4 byte,實際只看前 3 byte 與 UTF-16 LE/BE 雷同(0xFF 0xFE / 0x00 0x00 0xFE)
// — UTF-32 的「未被誤判」事實上是由 UTF-16 LE / "前 3 byte 不等於 0xEF 0xBB 0xBF" 邏輯
// 順帶覆蓋,**不是** 真正測 UTF-32-specific 行為。label 誤導:看起來「支援 UTF-32 偵測」,
// 實際上只是 byte sequence 巧合不命中 UTF-8 BOM。為避免誤導,L20 刪除 utf32 cases —
// 保留 UTF-16 case 是真正的 "非 UTF-8 BOM 不該被吞" 契約;UTF-32 case 若以後要加應
// 另開獨立 test func 並寫清楚 PeekBOM 的 3-byte window 限制。
func TestPeekBOM_UTF16BOM_NotDetected(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload []byte
	}{
		{"utf16_le_bom", []byte{0xFF, 0xFE, 'h', 0x00}},
		{"utf16_be_bom", []byte{0xFE, 0xFF, 0x00, 'h'}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := bufio.NewReader(bytes.NewReader(tc.payload))

			found, err := PeekBOM(r)
			if err != nil {
				t.Fatalf("PeekBOM returned error for non-UTF-8 BOM payload: %v", err)
			}
			if found {
				t.Fatalf("PeekBOM falsely detected %s as UTF-8 BOM", tc.name)
			}

			// 全部 bytes 必須仍在 reader 中（未被 Discard）
			rest, err := io.ReadAll(r)
			if err != nil {
				t.Fatalf("read remainder: %v", err)
			}
			if !bytes.Equal(rest, tc.payload) {
				t.Fatalf("non-UTF-8 BOM bytes consumed: got % x want % x", rest, tc.payload)
			}
		})
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
