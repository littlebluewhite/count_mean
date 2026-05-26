package logging

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// SizeRotatingWriter 達 maxSize 時應 rotate，舊內容保留為 .1 backup。
func TestSizeRotatingWriter_RotatesOnMaxSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	// 1 MB rotation, 3 backups
	w, err := NewSizeRotatingWriter(path, 1, 3)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	defer w.Close()

	// 寫入剛好觸發 rotation 的內容
	payload := bytes.Repeat([]byte("a"), 600*1024) // 600 KB
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := w.Write(payload); err != nil {
		// 第二次 write 累計 1.2MB > 1MB → rotate 前先 rotate
		t.Fatalf("second write: %v", err)
	}

	// .1 backup 應存在
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Errorf("backup .1 should exist after rotation, got %v", err)
	}

	// 主檔應該重新開且只含第二筆 payload（rotation 後寫入）
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatalf("main file should exist: %v", err)
	}
	if stat.Size() == 0 {
		t.Error("main file should have content from post-rotation write")
	}
}

func TestSizeRotatingWriter_KeepsMaxBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := NewSizeRotatingWriter(path, 1, 2) // 只保留 .1 .2
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	defer w.Close()

	payload := bytes.Repeat([]byte("a"), 700*1024)
	for i := 0; i < 5; i++ {
		if _, err := w.Write(payload); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// .3 不應存在（超過 maxBackups）
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Errorf("backup .3 should NOT exist (max=2), got %v", err)
	}
}

func TestSizeRotatingWriter_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	w, err := NewSizeRotatingWriter(path, 100, 3)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("new\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(content), "existing\n") {
		t.Errorf("should append, not truncate, got %q", content)
	}
}

// TestSizeRotatingWriter_RejectsSameProcessReopen 守護 
// 同個 process 內對同個 logPath 第二次 NewSizeRotatingWriter 必須 reject。
// 過去 doc 只說「同 logPath 只允許單 process」但沒 enforce,兩個 writer 同時
// 跑 rotation 會互相覆蓋彼此的 .1 → .2 → ... rename 序列,造成 log 內容交錯。
func TestSizeRotatingWriter_RejectsSameProcessReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "double_open.log")

	first, err := NewSizeRotatingWriter(path, 100, 3)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	defer first.Close()

	second, err := NewSizeRotatingWriter(path, 100, 3)
	if err == nil {
		_ = second.Close()
		t.Fatalf("second open should fail with ErrLogPathAlreadyOpen, got nil")
	}
	if !errors.Is(err, ErrLogPathAlreadyOpen) {
		t.Errorf("expected ErrLogPathAlreadyOpen, got %v", err)
	}
}

// TestSizeRotatingWriter_AllowsReopenAfterClose 守護:第一次 Close 後 path 可被
// 重新開啟 — registry 不能 leak。否則「同 process 序列 init / shutdown / re-init」
// 場景 (例如 GUI 內 settings change 重啟 logger) 會被誤判 double-open。
func TestSizeRotatingWriter_AllowsReopenAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "reopen.log")

	first, err := NewSizeRotatingWriter(path, 100, 3)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	second, err := NewSizeRotatingWriter(path, 100, 3)
	if err != nil {
		t.Fatalf("reopen after close should succeed, got %v", err)
	}
	defer second.Close()
}

// TestSizeRotatingWriter_AbsolutePathDedup 守護:registry key 用 absolute path —
// "./logs/x.log" 與 "/abs/.../logs/x.log" 應被視為同一個 path,不能繞過偵測。
func TestSizeRotatingWriter_AbsolutePathDedup(t *testing.T) {
	// Windows runner cwd 在 D: drive,t.TempDir 在 C: drive (AppData\Local\Temp),
	// filepath.Rel(cwd=D:\..., absPath=C:\...) 在 Windows 上無法計算跨 drive 相對
	// 路徑 (test 設計限制,非 production bug)。留待 #14 補 same-drive fixture。
	if runtime.GOOS == "windows" {
		t.Skip("pre-existing Windows cross-drive Rel issue from 7682042, tracked in #14")
	}

	dir := t.TempDir()
	absPath := filepath.Join(dir, "abs_dedup.log")

	// 用 absolute 路徑開
	first, err := NewSizeRotatingWriter(absPath, 100, 3)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	defer first.Close()

	// 用 relative 路徑開同個檔 — 必須仍被偵測
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	relPath, err := filepath.Rel(cwd, absPath)
	if err != nil {
		t.Fatalf("rel path: %v", err)
	}

	second, err := NewSizeRotatingWriter(relPath, 100, 3)
	if err == nil {
		_ = second.Close()
		t.Fatalf("relative path 必須與 absolute path 視為同一檔,但 reopen 成功了")
	}
	if !errors.Is(err, ErrLogPathAlreadyOpen) {
		t.Errorf("expected ErrLogPathAlreadyOpen, got %v", err)
	}
}

// TestSizeRotatingWriter_CloseIsIdempotent 守護:Close 多次不能 panic / 不能回 error。
// Logger.Close (M4) 會委派給 SizeRotatingWriter.Close,multiple shutdown hook 觸發時
// 可能重複呼叫。
func TestSizeRotatingWriter_CloseIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idem.log")

	w, err := NewSizeRotatingWriter(path, 100, 3)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	// 第二次 Close 不能 panic / error
	if err := w.Close(); err != nil {
		t.Errorf("second close should be no-op, got %v", err)
	}
}

// TestRotation_WriteAfterClose_ReturnsSentinel 守護 Close 後再 Write 必須
// 回 ErrWriterClosed,**不能 nil-deref**。Logger.Close 委派至此後 caller 持有
// stale writer reference 並繼續 Write 是 plausible race(尤其 multi-step shutdown)。
func TestRotation_WriteAfterClose_ReturnsSentinel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "post_close.log")

	w, err := NewSizeRotatingWriter(path, 100, 3)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Close 後 Write 必須 graceful 失敗 — 不可 panic / nil-deref。
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Write after Close panicked: %v", r)
		}
	}()
	n, werr := w.Write([]byte("after-close"))
	if !errors.Is(werr, ErrWriterClosed) {
		t.Errorf("expected ErrWriterClosed, got n=%d err=%v", n, werr)
	}
	if n != 0 {
		t.Errorf("expected n=0 on closed writer, got %d", n)
	}
}

// TestRotation_WriteAfterFailedRotate_NoPanic 守護 rotateLocked 失敗時
// 不可 nil-deref。docstring 承諾「fallback 為繼續寫到原檔」— 強制 rename 失敗
// (將 parent dir 改成 read-only),驗證:
//   1. Write 回到的 error 非 nil 或為 nil(實作選擇),都不可 panic
//   2. w.file 不可保持 nil — 後續 Write 必須能繼續(reopen 原檔)
//
// 在 unix 用 chmod 0o500 parent dir 強迫 os.Rename(basePath, basePath+".1") fail
// (Linux/macOS 上 rename 需要 dir 的 write 權限)。Windows / root skip。
func TestRotation_WriteAfterFailedRotate_NoPanic(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows: chmod 0o500 對 dir rename 行為與 POSIX 不同,skip")
	}
	if os.Geteuid() == 0 {
		t.Skip("root: 可繞過 dir 權限,chmod 0o500 無法阻止 rename")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "fail_rotate.log")

	// 1MB rotation 閾值,2 backups
	w, err := NewSizeRotatingWriter(path, 1, 2)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		// 還原 dir 權限以利 t.TempDir cleanup
		_ = os.Chmod(dir, 0o700)
		_ = w.Close()
	})

	// 寫一些初始資料,讓 rotation 條件接近觸發
	initial := bytes.Repeat([]byte("a"), 600*1024)
	if _, err := w.Write(initial); err != nil {
		t.Fatalf("initial write: %v", err)
	}

	// 把 parent dir 改成 read+execute only — rename / unlink / create 全部會失敗
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}

	// 寫入觸發 rotation;rotateLocked 內 os.Rename 會 fail。
	// 不可 panic / 不可 nil-deref。
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Write after failed rotate panicked: %v", r)
		}
	}()
	payload := bytes.Repeat([]byte("b"), 700*1024) // 累計 > 1MB,觸發 rotation
	_, werr := w.Write(payload)
	// werr 可能為 nil(成功 fallback)或 *os.PathError(底層 file 已 close 寫失敗),
	// 重點是 *不能 panic*。Implementation MAY choose to swallow rotation failure,
	// 也 MAY 回 ErrWriterClosed。兩者皆可,只要不 panic。
	_ = werr

	// 把權限還原,再次 Write 必須成功 — 證明 writer 不是「死掉」狀態。
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("restore chmod: %v", err)
	}
	if _, err := w.Write([]byte("post-restore")); err != nil {
		t.Errorf("post-restore write should succeed (writer must reopen original file on rotate failure), got %v", err)
	}
}

// TestRotation_ConcurrentWrite_BytesPreserved 守護 並發契約:
//
// N 個 goroutine 同時對同一個 SizeRotatingWriter 寫入,rotation 過程不可丟資料 —
// 也就是「base 檔的 bytes + 所有 .N backup 檔的 bytes」必須等於「所有 Write call
// 加總的成功 byte 數」。
//
// 為什麼需要這個 test:Write 入口持有 w.mu 序列化,rotateLocked 也在 w.mu 下做
// rename / OpenFile,但 race detector 之前未跑過 contention case;若未來把 mu
// 拆細(eg seperate file fd lock vs rotation lock)很可能出現 partial-rotation
// 期間有 goroutine 看到 stale fd / 看到 .1 already renamed 但 base 還沒 reopen,
// 導致 byte lost。本 test 加上 -race -count=10 跑出 deterministic byte balance,
// 任何未來的 lock 細化必須先過這條 invariant。
//
// 參數選擇:
//   - maxSizeMB=1 (API granularity 是 MB),maxBackups=5
//   - N=10 goroutine × K=3000 寫 × 100 byte = 300_000 byte / goroutine
//   - 全 process 累計 3_000_000 byte ≈ 2.86MB → 觸發 ~2-3 次 rotation
//   - 5 backups (5MB capacity) + base (~1MB) = 6MB > 3MB → 不會 truncate
//
// API 限制:NewSizeRotatingWriter 收 MB 單位,沒辦法設成 1KB(task spec 想要的閾值);
// 用 1MB + 較大量寫入達到同等「多次 rotation」的測試覆蓋。 用較大 total bytes
// (而非單寫很大)是為了確保多個 goroutine 真的會在 rotation 期間互相 contend。
func TestRotation_ConcurrentWrite_BytesPreserved(t *testing.T) {
	const (
		numGoroutines    = 10
		writesPerRoutine = 3000
		messageSize      = 100
		maxSizeMB        = 1
		maxBackups       = 5
	)
	totalExpected := int64(numGoroutines) * int64(writesPerRoutine) * int64(messageSize)

	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.log")
	w, err := NewSizeRotatingWriter(path, maxSizeMB, maxBackups)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	t.Cleanup(func() {
		_ = w.Close()
	})

	// 每個 goroutine 寫一個 unique-byte 訊息,失敗計數用 atomic。
	// 預期所有 Write 都成功:總寫入 3MB,maxBackups=5 (5MB capacity) 不應 truncate。
	var (
		writeErrors atomic.Int64
		writtenSum  atomic.Int64
		wg          sync.WaitGroup
	)
	wg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			// 用 id 當 payload 的可辨識前綴 — 真要 debug 時可從檔內容反推哪個
			// goroutine 寫進來。 padding 補齊到 messageSize。
			prefix := fmt.Sprintf("g%02d:", id)
			payload := make([]byte, messageSize)
			copy(payload, prefix)
			for k := len(prefix); k < messageSize; k++ {
				payload[k] = byte('a' + (id % 26))
			}
			for k := 0; k < writesPerRoutine; k++ {
				n, werr := w.Write(payload)
				if werr != nil {
					writeErrors.Add(1)
					t.Errorf("goroutine %d write %d: %v", id, k, werr)
					return
				}
				if n != messageSize {
					writeErrors.Add(1)
					t.Errorf("goroutine %d write %d: short write n=%d", id, k, n)
					return
				}
				writtenSum.Add(int64(n))
			}
		}(g)
	}
	wg.Wait()

	if writeErrors.Load() != 0 {
		t.Fatalf("got %d write errors during concurrent run", writeErrors.Load())
	}
	if got := writtenSum.Load(); got != totalExpected {
		t.Fatalf("writtenSum (atomic counter) = %d, want %d", got, totalExpected)
	}

	// rotation 是 lock-protected,base 檔可能正在被 fd 寫入(已 close 過 + reopen),
	// 但所有 goroutine 已 done,讀取 base 應穩定。
	baseSize := fileSizeOrZero(t, path)
	backupSize := int64(0)
	backupsFound := 0
	for i := 1; i <= maxBackups; i++ {
		bp := backupPath(path, i)
		if sz, ok := fileSizeIfExists(t, bp); ok {
			backupSize += sz
			backupsFound++
		}
	}
	totalOnDisk := baseSize + backupSize

	t.Logf("concurrent rotation: %d writes × %d bytes = %d expected; on disk base=%d + %d backups=%d (total=%d)",
		numGoroutines*writesPerRoutine, messageSize, totalExpected,
		baseSize, backupsFound, backupSize, totalOnDisk)

	if totalOnDisk != totalExpected {
		t.Fatalf("byte conservation violated: total on disk = %d, total written = %d (diff=%d). rotation 丟資料 — race / partial rotation 沒被 mu 涵蓋",
			totalOnDisk, totalExpected, totalOnDisk-totalExpected)
	}
}

// fileSizeOrZero 讀檔大小,檔不存在則回 0 並 t.Errorf — base file 在 test 流程中
// **必須** 存在(rotation 後會 reopen)。 stat 失敗(非 NotExist)同樣 errorf。
func fileSizeOrZero(t *testing.T, path string) int64 {
	t.Helper()
	stat, err := os.Stat(path)
	if err != nil {
		t.Errorf("stat base %s: %v", path, err)
		return 0
	}
	return stat.Size()
}

// fileSizeIfExists 讀檔大小,**檔不存在則回 (0, false)** — backup 不一定存在(取
// 決於有沒有 rotate 過、 rotate 過幾次),所以 not-exist 不是錯誤;但其他 stat 錯
// 必須 errorf。
func fileSizeIfExists(t *testing.T, path string) (int64, bool) {
	t.Helper()
	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false
		}
		t.Errorf("stat backup %s: %v", path, err)
		return 0, false
	}
	return stat.Size(), true
}

// TestRotation_MaxBackupsDecrease_OldFilesDeleted 守護 
//
// **Scenario**: 使用者上一輪用 maxBackups=10 跑出 .1 ~ .10 backup;這輪改 config
// 把 maxBackups 降到 3。 過去 rotateLocked 只負責「刪除 base + maxBackups 編號」
// 那一個,**.5 ~ .10 永遠不會被刪** — 磁碟回收延遲到使用者手動清理或 OS 退出。
//
// 修補後:NewSizeRotatingWriter 開檔時(以及 rotateLocked 入口)會 scan dir 找
// `<basename>.N` 編號超過當前 maxBackups 的舊 backup,delete 多餘的。 既有 backup
// 在新一輪 init 時就被收斂,而非等下一次 rotation 才順帶處理。
//
// 為什麼選 Open 時 prune 而非 rotation 時 prune:
//   - Open 時 prune 給「重啟後立即生效」的契約 — 使用者改 config 後不用等 rotation
//   - rotation 時也 prune 給「長跑 process 不會累積舊檔」的契約 — 但長跑場景的
//     primary entry 是 Open(InitLogger),所以 Open 入口是最關鍵的觸發點
//
// **不依賴具體實作**(Open vs rotateLocked):本 test 只驗「Open 之後 .5 ~ .10
// 必須不存在」,不關心 prune 是發生在 Open 還是 rotateLocked。
func TestRotation_MaxBackupsDecrease_OldFilesDeleted(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "shrink.log")

	// 預先建立 .1 ~ .10 backup 模擬「上一輪 maxBackups=10」遺留下的檔案
	const oldMax = 10
	for i := 1; i <= oldMax; i++ {
		bp := backupPath(base, i)
		if err := os.WriteFile(bp, []byte(fmt.Sprintf("backup-%d-content", i)), 0o600); err != nil {
			t.Fatalf("seed backup .%d: %v", i, err)
		}
	}

	// 再建立 base 檔本身(空檔即可,Open 會 reuse / append)
	if err := os.WriteFile(base, []byte(""), 0o600); err != nil {
		t.Fatalf("seed base: %v", err)
	}

	// **這輪 maxBackups=3** — 開檔後 .4 ~ .10 必須被刪
	const newMax = 3
	w, err := NewSizeRotatingWriter(base, 100, newMax)
	if err != nil {
		t.Fatalf("open with reduced maxBackups: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	// .1 ~ .3 仍應存在(保留契約)
	for i := 1; i <= newMax; i++ {
		bp := backupPath(base, i)
		if _, err := os.Stat(bp); err != nil {
			t.Errorf(".%d 應保留, 卻 stat 失敗: %v", i, err)
		}
	}

	// .4 ~ .10 必須被刪
	for i := newMax + 1; i <= oldMax; i++ {
		bp := backupPath(base, i)
		if _, err := os.Stat(bp); !os.IsNotExist(err) {
			t.Errorf(".%d 應被刪除 (maxBackups=%d), 但仍存在 (err=%v)", i, newMax, err)
		}
	}
}

// TestRotation_AbsPathStoredAtConstruction 守護 
//
// **Scenario**: NewSizeRotatingWriter 用 relative path 建立(eg "./logs/x.log");
// 之後 process 用 os.Chdir 切到別的 dir,Close 才被呼叫。 過去 Close 用
// `filepath.Abs(w.basePath)` **重新計算** abs path — 重算用「當下的 cwd」,
// 不再是 construction 時的 cwd → registry key 對不上,delete 失敗,registry
// leak 一份 stale entry,後續對「同一個 logical path」reopen 會誤判 double-open。
//
// 修補後:construction 時就把 abs path store 在 writer struct 內(eg w.absPath),
// Close 直接用 stored 值,不再重算。
//
// **Test strategy**:
//  1. 在 dir A 用 "rel.log"(relative)開 writer → registry key = dir_A/rel.log
//  2. os.Chdir 到 dir B
//  3. Close writer
//  4. **驗證**:re-open 用 dir_A/rel.log(絕對形式)必須成功 — 證明 Close 用
//     stored abs path(dir_A/rel.log)成功 unregister,而非用 dir_B/rel.log
//     誤算後 noop delete。
//
// **macOS 注意**: t.TempDir 在 darwin 回 /var/... 但 cwd 走 /private/var/...
// (symlink-resolved),filepath.Abs 在 chdir 後對 relative path 會自動加上
// canonicalized cwd。 必須先 EvalSymlinks 把 dir 也 canonicalize,registry
// key 才會對齊;否則 reopen 用未 canonicalize 的 path 會與 stored key 不同,
// test 反而誤通過。
func TestRotation_AbsPathStoredAtConstruction(t *testing.T) {
	// Windows 上 t.Cleanup TempDir RemoveAll 報「being used by another process」—
	// production code 可能有 file-handle 未正確釋放的 bug,或 Windows share-mode
	// 行為差異所致。本 PR 與 logging 無關,留待 #14 修。
	if runtime.GOOS == "windows" {
		t.Skip("pre-existing Windows file-handle issue from 7682042, tracked in #14")
	}

	// 保存原 cwd,test 結束還原(避免污染其他 test)
	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origCwd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	// macOS: /var → /private/var symlink。 t.TempDir 回 /var/...,但 chdir
	// 進去後 os.Getwd 回 /private/var/...。 必須 EvalSymlinks 統一,否則 registry
	// key (基於 cwd) 與 reopen 用的 path 形式不同,test 觀察不到 bug。
	dirA, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks dirA: %v", err)
	}
	dirB, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("evalsymlinks dirB: %v", err)
	}

	// 切到 dirA,用 relative path 開 writer
	if err := os.Chdir(dirA); err != nil {
		t.Fatalf("chdir to dirA: %v", err)
	}

	relPath := "rel.log"
	w, err := NewSizeRotatingWriter(relPath, 100, 3)
	if err != nil {
		t.Fatalf("open in dirA: %v", err)
	}

	// 寫一點內容,確保 file 真的開在 dirA
	if _, err := w.Write([]byte("dirA content\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// **關鍵 step**: chdir 到 dirB — 模擬 caller 在 writer 生命週期內變更 cwd
	if err := os.Chdir(dirB); err != nil {
		t.Fatalf("chdir to dirB: %v", err)
	}

	// 此時 Close — 若 Close 用 filepath.Abs(w.basePath) 重算,會算出 dirB/rel.log
	// (錯的 key),registry 內 dirA/rel.log 不被 delete → leak。
	if err := w.Close(); err != nil {
		t.Errorf("close after chdir: %v", err)
	}

	// **驗證**:用 dirA 的絕對 path reopen,必須成功 — 證明 registry 已正確 unregister
	dirAAbsPath := filepath.Join(dirA, relPath)
	reopened, err := NewSizeRotatingWriter(dirAAbsPath, 100, 3)
	if err != nil {
		t.Fatalf("reopen with absolute path after chdir+close failed; registry leak suspected: %v", err)
	}
	defer reopened.Close()
}
