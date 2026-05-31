package csvutil

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"count_mean/internal/security/fsperm"
)

// TestWriteCSVAtomic_BasePaths_HappyPath 釘住新的 dirfd-anchored 路徑:當
// SafeWriteOptions.BasePaths 非空,WriteCSVAtomic 改走 fsperm.OpenAtomicWriteValidated
// (dirfd-create + renameat) 而非 legacy os.OpenFile+os.Rename。本 test 驗 commit
// 後 target 含預期 BOM+header+rows、tmp 已消失,且輸出與 legacy 路徑 byte-identical
// (atomicity + content parity 跨兩 branch)。
//
// 在 Darwin host 上這條走 openat+O_NOFOLLOW_ANY+renameat 的 Darwin primitive。
func TestWriteCSVAtomic_BasePaths_HappyPath(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "out.csv")

	header := []string{"name", "value"}
	emit := func(emit func([]string) error) error {
		if err := emit([]string{"alpha", "1"}); err != nil {
			return err
		}
		return emit([]string{"beta", "2"})
	}

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header:    header,
		Emit:      emit,
		BasePaths: []string{base},
	})
	if err != nil {
		t.Fatalf("dirfd-path WriteCSVAtomic: %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	s := string(got)
	if !strings.HasPrefix(s, "\xEF\xBB\xBF") {
		t.Errorf("missing BOM, got %q", s[:min(20, len(s))])
	}
	if !strings.Contains(s, "name,value") || !strings.Contains(s, "alpha,1") || !strings.Contains(s, "beta,2") {
		t.Errorf("missing rows in %q", s)
	}

	// tmp 應已 rename 走 — dir 內除 final 外不該有 tmp orphan。
	assertNoTmpResidue(t, base, filepath.Base(path))

	// content parity:同 input 走 legacy 路徑 (BasePaths 空) 應產出 byte-identical 檔案。
	legacyDir := t.TempDir()
	legacyPath := filepath.Join(legacyDir, "out.csv")
	if err := WriteCSVAtomic(legacyPath, SafeWriteOptions{Header: header, Emit: emit}); err != nil {
		t.Fatalf("legacy-path WriteCSVAtomic: %v", err)
	}
	legacyBytes, err := os.ReadFile(legacyPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read legacy final: %v", err)
	}
	if string(got) != string(legacyBytes) {
		t.Errorf("dirfd-path output != legacy-path output:\n dirfd=%q\nlegacy=%q", got, legacyBytes)
	}
}

// TestWriteCSVAtomic_BasePaths_AbortsOnMidWriteError 釘住 dirfd 路徑的失敗清理:
// 用 writerWrapHook 注入 mid-write IO error,BasePaths 設好時 WriteCSVAtomic 必須
// (1) 返回 error、(2) 不建立 target、(3) base 內無 .tmp.* 殘留。這條驗的是 defer
// 走 handle.Abort() (unlinkat tmp + close dirfd) 而非 bare os.Remove — dirfd-leak guard。
func TestWriteCSVAtomic_BasePaths_AbortsOnMidWriteError(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "out.csv")

	injected := errors.New("simulated disk full (dirfd path)")
	t.Cleanup(func() { writerWrapHook = nil })
	writerWrapHook = func(w io.Writer) io.Writer {
		return &midWriteFailingWriter{
			inner:       w,
			failAfter:   100,
			errInjected: injected,
		}
	}

	const emitCount = 1500
	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header:    []string{"col"},
		BasePaths: []string{base},
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
		t.Fatal("dirfd-path WriteCSVAtomic 應在 underlying IO error 時 abort,return non-nil error")
	}
	if !errors.Is(err, injected) && !strings.Contains(err.Error(), "mid-write") &&
		!strings.Contains(err.Error(), "simulated disk full") {
		t.Errorf("error 應反映 mid-write 探測失敗或 injected error,got: %v", err)
	}

	// target 不該存在 — 失敗時不 commit。
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("失敗時 final path 不該存在,stat err=%v", statErr)
	}
	// 無 tmp orphan — handle.Abort() 的 unlinkat 應刪掉 tmp。
	assertNoTmpResidue(t, base, filepath.Base(path))
}

// TestWriteCSVAtomic_BasePaths_RejectsEscape 釘住 BasePaths 確實 gate dirfd 路徑:
// path 的父目錄不在任一 base 之下時,WriteCSVAtomic 應回 fsperm.ErrPathEscapesBase
// (primitive 的 boundary check 透出),且不建立任何檔案。
func TestWriteCSVAtomic_BasePaths_RejectsEscape(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir() // 不在 base 下
	path := filepath.Join(outside, "out.csv")

	err := WriteCSVAtomic(path, SafeWriteOptions{
		Header:    []string{"col"},
		BasePaths: []string{base}, // gate 只允許 base，但 path 落在 outside
		Emit:      func(emit func([]string) error) error { return emit([]string{"v"}) },
	})
	if err == nil {
		t.Fatal("path 父目錄在 BasePaths 外應被拒")
	}
	if !errors.Is(err, fsperm.ErrPathEscapesBase) {
		t.Errorf("expected ErrPathEscapesBase, got %v", err)
	}
	// 不該在 outside 留下任何 tmp / final。
	assertNoTmpResidue(t, outside, filepath.Base(path))
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("escape 拒絕時不該建立 final, stat err=%v", statErr)
	}
}

// assertNoTmpResidue 斷言 dir 內沒有以 finalName 為前綴但非 finalName 本身的檔案
// (即 atomic tmp `.tmp.<rand>` orphan)。
func assertNoTmpResidue(t *testing.T, dir, finalName string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	for _, e := range entries {
		if e.Name() == finalName {
			continue
		}
		if strings.HasPrefix(e.Name(), finalName) {
			t.Errorf("tmp orphan 殘留於 %s: %s", dir, e.Name())
		}
	}
}
