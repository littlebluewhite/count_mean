package csvutil

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteCSVAtomic_ByteIdenticalGolden 鎖死 CSV byte-identity 契約:對 benign input
// (cells 不觸發 SanitizeHeaderRow/SanitizeBodyRow 的公式前綴),WriteCSVAtomic 委派
// fsperm.AtomicWriteFile 後寫出的檔案必須是固定的 BOM + LF-terminated CSV bytes。
//
// 為何補這條:既有 safe_writer_test.go 用 strings.Contains(子字串)+ BOM 前綴,
// 抓不到「多出位元組 / 行尾改 CRLF / 漏換行」;safe_writer_dirfd_test.go 的 byte
// 對比是「新碼 dirfd 分支 vs 新碼 legacy 分支」,若重構在共用 closure 引入位元組
// 回歸,兩分支會同錯仍 PASS。對固定 golden 比較才抓得到此盲點。
// Go encoding/csv.Writer 預設 UseCRLF=false → 行尾為 LF。codex review round 3 [P3]。
func TestWriteCSVAtomic_ByteIdenticalGolden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"name", "value"},
		Emit: func(emit func([]string) error) error {
			if err := emit([]string{"alpha", "1"}); err != nil {
				return err
			}
			return emit([]string{"beta", "2"})
		},
	})
	if err != nil {
		t.Fatalf("WriteCSVAtomic: %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read final: %v", err)
	}

	// UTF-8 BOM(EF BB BF）+ 純內容（benign cells 不被 sanitize 改寫）+ LF 行尾。
	const want = "\xEF\xBB\xBFname,value\nalpha,1\nbeta,2\n"
	if string(got) != want {
		t.Errorf("CSV bytes 不符 byte-identity golden\n got=%q\nwant=%q", got, want)
	}
}
