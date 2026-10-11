package fsperm

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// TestOpenRegularLimited 釘住讀檔門的四種結果(ADR-0056):上限內整檔可讀、開檔時超限回
// *TooLargeError(Size 為 fstat 大小)、目錄回 ErrNotRegular、不存在原樣回 *os.PathError。
func TestOpenRegularLimited(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	t.Run("上限內整檔可讀", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(dir, "ok.csv")
		if err := os.WriteFile(p, []byte("0123456789"), FilePerm); err != nil {
			t.Fatal(err)
		}
		f, err := OpenRegularLimited(p, 10)
		if err != nil {
			t.Fatalf("OpenRegularLimited: %v", err)
		}
		defer f.Close()
		got, err := io.ReadAll(f)
		if err != nil || string(got) != "0123456789" {
			t.Fatalf("ReadAll = %q, %v; want 整檔、nil", got, err)
		}
	})

	t.Run("開檔時超限", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(dir, "big.csv")
		if err := os.WriteFile(p, []byte("01234567890"), FilePerm); err != nil {
			t.Fatal(err)
		}
		_, err := OpenRegularLimited(p, 10)
		tooLarge, ok := errors.AsType[*TooLargeError](err)
		if !ok || !errors.Is(err, ErrTooLarge) || tooLarge.Size != 11 || tooLarge.Limit != 10 {
			t.Fatalf("err = %#v; want *TooLargeError{Size: 11, Limit: 10}", err)
		}
	})

	t.Run("目錄", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(dir, "folder.csv")
		if err := os.Mkdir(p, DirPerm); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenRegularLimited(p, 10); !errors.Is(err, ErrNotRegular) {
			t.Fatalf("err = %v; want ErrNotRegular", err)
		}
	})

	t.Run("不存在", func(t *testing.T) {
		t.Parallel()
		_, err := OpenRegularLimited(filepath.Join(dir, "missing.csv"), 10)
		if _, ok := errors.AsType[*os.PathError](err); !ok || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("err = %#v; want 原樣的 *os.PathError(ENOENT)", err)
		}
	})
}

// TestOpenRegularLimited_GrowsAfterOpen 釘住:開檔時在上限內、之後檔案變大,讀到第 limit+1
// byte 時回 *TooLargeError(Size = limit+1),不會無界讀進記憶體。
func TestOpenRegularLimited_GrowsAfterOpen(t *testing.T) {
	t.Parallel()

	p := filepath.Join(t.TempDir(), "grow.csv")
	if err := os.WriteFile(p, []byte("01234"), FilePerm); err != nil {
		t.Fatal(err)
	}
	f, err := OpenRegularLimited(p, 10)
	if err != nil {
		t.Fatalf("OpenRegularLimited: %v", err)
	}
	defer f.Close()

	if err := os.WriteFile(p, []byte("0123456789ABCDEF"), FilePerm); err != nil {
		t.Fatal(err)
	}

	got, err := io.ReadAll(f)
	tooLarge, ok := errors.AsType[*TooLargeError](err)
	if !ok || tooLarge.Size != 11 {
		t.Fatalf("ReadAll err = %#v; want *TooLargeError{Size: 11}", err)
	}
	if len(got) > 11 {
		t.Errorf("讀了 %d bytes;最多只該讀到第 limit+1 byte", len(got))
	}
}

// TestCheckRegularSize 釘住 OpenDataFile 用的 fd 檢查:一般檔案上限內 nil、超限
// *TooLargeError、目錄 ErrNotRegular。
func TestCheckRegularSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "emg.csv")
	if err := os.WriteFile(p, []byte("0123456789"), FilePerm); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p) //nolint:gosec // p 位於 t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if err := CheckRegularSize(f, 10); err != nil {
		t.Errorf("CheckRegularSize(10 bytes, 10) = %v; want nil", err)
	}
	if err := CheckRegularSize(f, 9); !errors.Is(err, ErrTooLarge) {
		t.Errorf("CheckRegularSize(10 bytes, 9) = %v; want ErrTooLarge", err)
	}

	d, err := os.Open(dir) //nolint:gosec // t.TempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := CheckRegularSize(d, 10); !errors.Is(err, ErrNotRegular) {
		t.Errorf("CheckRegularSize(dir) = %v; want ErrNotRegular", err)
	}
}
