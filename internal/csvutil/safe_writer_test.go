package csvutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCSVAtomic_HappyPath_WritesBomHeaderRows(t *testing.T) {
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
		t.Fatalf("expected nil err, got %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "\xEF\xBB\xBF") {
		t.Errorf("missing BOM, got %q", s[:min(20, len(s))])
	}
	if !strings.Contains(s, "name,value") || !strings.Contains(s, "alpha,1") || !strings.Contains(s, "beta,2") {
		t.Errorf("missing rows in %q", s)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("expected tmp removed, stat err=%v", err)
	}
}

func TestWriteCSVAtomic_EmitFailMidRow_FinalPathUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	sentinel := []byte("old content\n")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	wantErr := errors.New("simulated emit failure")
	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"name"},
		Emit: func(emit func([]string) error) error {
			if err := emit([]string{"alpha"}); err != nil {
				return err
			}
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected wantErr propagated, got %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != string(sentinel) {
		t.Errorf("final path mutated: got %q want %q", got, sentinel)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("tmp leaked, stat err=%v", err)
	}
}

func TestWriteCSVAtomic_StaleTmp_Rejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"name"},
		Emit:   func(emit func([]string) error) error { return emit([]string{"x"}) },
	})
	if err == nil {
		t.Fatal("expected err on stale tmp, got nil")
	}
}

func TestWriteCSVAtomic_HeaderSanitized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"=SUM(A1)", "normal"},
		Emit:   func(emit func([]string) error) error { return emit([]string{"v1", "v2"}) },
	})
	if err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "'=SUM(A1)") {
		t.Errorf("header should be sanitized with leading apostrophe, got %q", b)
	}
}

func TestWriteCSVAtomic_NilHeader_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: nil,
		Emit:   func(emit func([]string) error) error { return nil },
	})
	if err == nil {
		t.Fatal("expected err on nil header")
	}
}

func TestWriteCSVAtomic_NilEmit_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.csv")
	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header: []string{"a"},
		Emit:   nil,
	})
	if err == nil {
		t.Fatal("expected err on nil Emit")
	}
}
