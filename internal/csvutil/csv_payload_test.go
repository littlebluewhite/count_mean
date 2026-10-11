package csvutil

import (
	"bytes"
	"errors"
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
