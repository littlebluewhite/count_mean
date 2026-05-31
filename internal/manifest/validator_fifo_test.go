//go:build !windows

package manifest

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"count_mean/internal/models"
)

// TestValidateAllEMGFiles_FifoDoesNotHang 釘住 codex P2 (DoS):ValidateAllEMGFiles
// 對每筆 manifest row 走 OpenDataFile(read-only open + close)。若某 row 的
// EMGFile 是一個 FIFO,read-only open 在沒有 writer 連上時會「阻塞」— 一筆壞 row
// 就把整個 LoadChartComposerSubjects(subject dropdown)卡死。
//
// 修正:ReadFlags 加 O_NONBLOCK(對 regular file 是 no-op,只擋 FIFO/special 阻塞)
// + ValidateAllEMGFiles 對開出來的檔 fstat,非 regular file 直接列為 MissingRow。
//
// 紅綠:修正前此測試會在 OpenDataFile 開 FIFO 時 hang,被 time.After 守門轉成
// t.Fatal(紅);修正後 ValidateAllEMGFiles 立即返回、FIFO row 列入 missing、
// regular row 不在 missing(綠)。同 manifest 放一筆 regular row 釘住 happy path 不壞。
func TestValidateAllEMGFiles_FifoDoesNotHang(t *testing.T) {
	t.Parallel()

	dataFolder := t.TempDir()

	// FIFO row：read-only open 在無 writer 時會阻塞(修正前)。
	fifoPath := filepath.Join(dataFolder, "pipe.csv")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("setup: Mkfifo: %v", err)
	}

	// regular row：happy path 仍須通過(不在 missing)。
	if err := os.WriteFile(filepath.Join(dataFolder, "ok.csv"), []byte("time\n"), 0o600); err != nil {
		t.Fatalf("setup: WriteFile ok.csv: %v", err)
	}

	manifests := []models.PhaseManifest{
		{Subject: "Pipe", EMGFile: "pipe.csv"},
		{Subject: "Ok", EMGFile: "ok.csv"},
	}

	// time.After 守門:修正前 ValidateAllEMGFiles 在 FIFO open 處 hang,goroutine 永不
	// 送回 done,select 落到 timeout 分支 → t.Fatal(紅)。修正後立即返回。
	type result struct{ missing []MissingRow }
	done := make(chan result, 1)
	go func() {
		done <- result{missing: ValidateAllEMGFiles(manifests, dataFolder)}
	}()

	select {
	case r := <-done:
		// FIFO row 必須列為 missing(非 regular file)。
		var pipeMissing bool
		for _, m := range r.missing {
			if m.Subject == "Pipe" {
				pipeMissing = true
				if !errors.Is(m.Err, ErrManifestDataFilePathInvalid) {
					t.Errorf("FIFO row.Err 應命中 ErrManifestDataFilePathInvalid,實際 %v", m.Err)
				}
			}
			if m.Subject == "Ok" {
				t.Errorf("regular file row 不該列入 missing,實際 %+v", m)
			}
		}
		if !pipeMissing {
			t.Errorf("FIFO row 應列為 MissingRow(非一般檔案),實際 missing=%+v", r.missing)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ValidateAllEMGFiles 在 FIFO row 上 hang 了(O_NONBLOCK 缺失,codex P2 DoS)")
	}
}
