//go:build unix

package logging

import (
	"bytes"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// TestRotation_ForcedDiskFull_NoPanic 守護 強制失敗契約:
//
// 當底層 file write 觸碰系統限制(模擬磁碟滿),SizeRotatingWriter.Write **不可
// panic**,必須 graceful 回 wrapped error。 的 TestRotation_WriteAfterFailedRotate
// 已涵蓋 "rename 失敗(rotateLocked 內 os.Rename 失敗)" 路徑;本 test 補上另一條
// fault path:**底層 file.Write 直接失敗**(write(2) 回 EFBIG / disk-full-like)。
//
// **Fault injection 方法**: 透過 syscall.Setrlimit(RLIMIT_FSIZE) 限制單一檔案的
// 最大 size。當寫入超過上限時:
//   - kernel 預設會送 SIGXFSZ 給 process(會被預設 handler 殺死)
//   - 配合 signal.Ignore(SIGXFSZ),改為 write(2) 回 EFBIG (errno) → *os.File.Write
//     回 *PathError,SizeRotatingWriter.Write 用 fmt.Errorf("%w") 包裝
//
// 為什麼選 RLIMIT_FSIZE 而非:
//   - chmod(file, 0o400):open 中的 fd 不會受影響(POSIX 語意),fault 無法觸發
//   - chmod(dir, 0o500):已被 用過(那個是 rename 失敗,跟 file write 失敗
//     是不同 path)
//   - filesystem mock:需動 production code 加 interface,task 約束「不要動 rotation.go」
//
// **Cross-platform**: RLIMIT_FSIZE / SIGXFSZ 是 unix-only;Windows 用 build tag
// 排除整個檔案。 root 跳過 — root 可繞過 rlimit(雖然 RLIMIT_FSIZE 對 root 仍生效,
// 但測試環境差異大且 CI 通常 non-root,直接 skip 比 false positive 安全)。
//
// **Test isolation**: RLIMIT_FSIZE 是 process-wide,defer 還原原 limit 避免影響
// 後續 test(尤其 -count=N 同 process 重跑)。 不使用 t.Parallel()。
func TestRotation_ForcedDiskFull_NoPanic(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root: 雖然 RLIMIT_FSIZE 對 root 也生效,但 CI 環境差異大,skip 避免 flaky")
	}

	// 1. 先把 SIGXFSZ 改成 ignore — 否則寫超過 RLIMIT_FSIZE 會 kill process。
	//    signal.Ignore 是 process-wide;defer signal.Reset 還原預設 handler。
	signal.Ignore(syscall.SIGXFSZ)
	t.Cleanup(func() {
		signal.Reset(syscall.SIGXFSZ)
	})

	// 2. 保存原 RLIMIT_FSIZE,defer 還原 — 確保 -count=N 不污染其他 test。
	var orig syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &orig); err != nil {
		t.Fatalf("getrlimit RLIMIT_FSIZE: %v", err)
	}
	t.Cleanup(func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &orig); err != nil {
			t.Errorf("restore RLIMIT_FSIZE: %v", err)
		}
	})

	// 3. 限制單檔最大 4096 byte。 **只調 soft limit**,hard limit 維持原值 —
	//    調降 hard limit 之後 non-root user 沒辦法調回(POSIX 限制),會讓
	//    Cleanup 的 Setrlimit(orig) 失敗。 soft limit 對 write(2) 一樣會觸發
	//    SIGXFSZ / EFBIG (kernel 用 RLIMIT_FSIZE 的 soft limit 判定),所以只調
	//    soft 也達到 fault injection 效果。
	//
	//    為什麼是 4096:足夠 rotation writer 開檔 + 寫一些初始 bytes,但小到我們
	//    一個 payload 就能撞到上限,且不會影響 testing framework 的 stdout / temp file。
	const fsizeLimit = 4096
	lim := syscall.Rlimit{Cur: fsizeLimit, Max: orig.Max}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
		t.Fatalf("setrlimit RLIMIT_FSIZE=%d: %v", fsizeLimit, err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "disk_full.log")

	// 4. maxSizeMB=100 (= 100MB) 遠大於 RLIMIT_FSIZE → 不會走 rotation path,
	//    fault 純粹來自 underlying file.Write 撞 fsize limit。 這樣 test 隔離出
	//    「file write 失敗」這個獨立 fault,不跟 rotation rename 失敗混。
	w, err := NewSizeRotatingWriter(path, 100, 2)
	if err != nil {
		// 開檔本身可能因 limit 太小失敗 — 那也算 graceful failure,不應到此。
		t.Fatalf("create writer (rlimit might be too tight?): %v", err)
	}
	t.Cleanup(func() {
		_ = w.Close()
	})

	// 5. 寫超過 fsizeLimit:預期 underlying write(2) 回 EFBIG,
	//    *os.File.Write 回 *PathError,SizeRotatingWriter.Write wrap 後回出來。
	//    **重點**:不可 panic。
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Write past RLIMIT_FSIZE panicked: %v", r)
		}
	}()

	// 寫一塊 8KB,確保超過 4KB limit。 預期會收到 wrapped error;n 可能是 partial。
	payload := bytes.Repeat([]byte("X"), 8*1024)
	n, werr := w.Write(payload)

	if werr == nil {
		t.Errorf("expected non-nil error when writing past RLIMIT_FSIZE, got n=%d err=nil; "+
			"either RLIMIT_FSIZE 在這個 OS / runner 不生效, 或 Write 默默吞了 error", n)
		return
	}

	// errno 通常會以 *os.PathError(syscall.EFBIG) 出現。 我們不嚴格綁定 errno 種類
	// (不同 OS / FS 可能回不同 errno;Linux 上是 EFBIG,某些 FS overlay 可能回 ENOSPC),
	// 重點是:error 非 nil 且 *被 fmt.Errorf 包過*(prefix "log file write")。
	if !looksLikeWriteError(werr) {
		t.Errorf("expected wrapped 'log file write:' error, got %v", werr)
	}

	t.Logf("disk-full fault injected via RLIMIT_FSIZE=%d: Write returned n=%d err=%v",
		fsizeLimit, n, werr)

	// 6. 還原 RLIMIT_FSIZE → 後續 Write 必須成功(writer 沒「死掉」,可重用)。
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &orig); err != nil {
		t.Fatalf("temp restore RLIMIT_FSIZE: %v", err)
	}
	if _, err := w.Write([]byte("post-restore\n")); err != nil {
		// rotation writer 在 file write 失敗後 *不會* 自動 reopen(失敗不會把 file
		// 設成 nil),所以 file fd 仍持有。 但是 EFBIG 之後同個 fd 再寫(若再次低於
		// 限制)應該成功。 若這條 fail 表示 writer 卡在 unrecoverable state。
		t.Errorf("post-restore write should succeed (writer must remain usable after EFBIG), got %v", err)
	}
}

// looksLikeWriteError 確認 err 是 SizeRotatingWriter.Write 包裝過的 file write 錯誤。
// 不綁定具體 errno (跨 OS 可能差異),只驗 wrap prefix + 非 sentinel close error。
// 為什麼不直接 errors.Is(err, syscall.EFBIG):跨平台 errno 名稱不一致,且
// rotation writer 不曝露底層 errno — 它只 wrap "log file write: %w";所以從外面
// 看只能驗 prefix + 非 ErrWriterClosed (因為 file 沒 nil)。
func looksLikeWriteError(err error) bool {
	if err == nil {
		return false
	}
	// 不應該是 ErrWriterClosed — file 沒被 nil 設掉,writer 還活著。
	if errors.Is(err, ErrWriterClosed) {
		return false
	}
	// SizeRotatingWriter.Write 用 "log file write: %w" 包裝。
	return strings.Contains(err.Error(), "log file write")
}
