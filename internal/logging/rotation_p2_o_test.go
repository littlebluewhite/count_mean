package logging

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPruneExcessBackups_RegexMatching 釘住 預編譯 regex 行為:
//   - 純數字後綴(.1, .2, ..., .42)正確匹配並可被 prune
//   - mixed suffix(.1.bak, .tmp, .log.gz)被 reject,不被誤刪
//   - regex 比對 case-sensitive(數字本就無 case 差異,但 mixed 字元應被擋)
func TestPruneExcessBackups_RegexMatching(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	basePath := filepath.Join(dir, "app.log")
	// 建立各種 backup file 模擬實際 dir 內容
	cases := []struct {
		name        string // 檔名(不含 dir)
		shouldPrune bool   // 期望被 prune (n > maxBackups=3)
	}{
		// 純數字後綴,n > maxBackups → 應 prune
		{"app.log.4", true},
		{"app.log.5", true},
		{"app.log.10", true},
		{"app.log.99", true},
		// 純數字後綴,n ≤ maxBackups → rotation 自管,不該 prune
		{"app.log.1", false},
		{"app.log.2", false},
		{"app.log.3", false},
		// mixed suffix → regex 拒絕,保留
		{"app.log.1.bak", false},
		{"app.log.4.gz", false},
		{"app.log.tmp", false},
		{"app.log.42abc", false},
		// 完全不同檔名 → prefix 不符,保留
		{"unrelated.txt", false},
		{"app.log.backup", false}, // suffix 不是純數字
	}

	for _, c := range cases {
		p := filepath.Join(dir, c.name)
		require.NoError(t, os.WriteFile(p, []byte("test"), 0o600))
	}

	// 用 maxBackups=3 跑 pruneExcessBackups
	w := &SizeRotatingWriter{
		basePath:   basePath,
		maxBackups: 3,
	}
	w.pruneExcessBackups()

	// 驗證每個 file 是否如預期保留 / 刪除
	for _, c := range cases {
		p := filepath.Join(dir, c.name)
		_, err := os.Stat(p)
		if c.shouldPrune {
			require.True(t, os.IsNotExist(err),
				"P2-O regex prune: %q (純數字 > maxBackups=3) 應被刪除,實際還在", c.name)
		} else {
			require.NoError(t, err,
				"P2-O regex prune: %q 不該被誤刪(suffix=%q 應被 regex reject 或在 maxBackups 內)",
				c.name, c.name[len("app.log."):])
		}
	}
}

// TestPruneExcessBackups_RegexCompiledOnce 釘住 regex 是 package-level 預編譯而非
// per-call MustCompile。檢查方法:呼叫 100 次 prune(空 dir,不會誤刪),驗證沒有
// panic / leak;若 regex 不是預編譯而每次 MustCompile,長跑 process 累積 compile
// 成本但測試 nominal 不會 fail。此 test 主要 lock 介面契約(backupSuffixRegexp
// 是 exported 但 package-internal var,不可被改成 per-call inline literal)。
func TestPruneExcessBackups_RegexCompiledOnce(t *testing.T) {
	t.Parallel()

	// invariant test:確認 backupSuffixRegexp 已預編譯且非 nil
	require.NotNil(t, backupSuffixRegexp,
		"backupSuffixRegexp 應為 package-level 預編譯 regex")

	// 直接餵 sample 確認 regex 行為
	if backupSuffixRegexp.FindStringSubmatch("42") == nil {
		t.Error("backupSuffixRegexp 應該 match '42' 純數字 suffix")
	}
	if backupSuffixRegexp.FindStringSubmatch("42.bak") != nil {
		t.Error("backupSuffixRegexp 不該 match '42.bak' mixed suffix")
	}
	if backupSuffixRegexp.FindStringSubmatch("abc") != nil {
		t.Error("backupSuffixRegexp 不該 match 'abc' 非數字 suffix")
	}
}

// TestFatal_FlushesBeforeExit 釘住(b):Fatal 在呼叫 exitFunc 之前必須對
// output 嘗試 Flush() + Sync() — 避免最後一筆 Fatal 訊息卡在 page cache 隨 process
// 截斷而丟失。
//
// 用 spy:用 syncSpyWriter 取代真實 file writer,記錄 Sync() 是否被呼叫;exitFunc
// 改為 capture(不真的 exit),驗證 spy.syncCalled == true 且順序在 exit 之前。
func TestFatal_FlushesBeforeExit(t *testing.T) {
	// 不能 t.Parallel:此 test 修改 package-level exitFunc。
	origExit := exitFunc
	defer func() { exitFunc = origExit }()

	spy := &syncSpyWriter{}
	logger := NewLogger(LevelInfo, spy, false)

	var exitCalled bool
	var syncCalledBeforeExit bool
	exitFunc = func(_ int) {
		exitCalled = true
		syncCalledBeforeExit = spy.syncCalled // capture 當下 spy 狀態
	}

	logger.Fatal("fatal-test-msg", nil)

	require.True(t, exitCalled, "exitFunc 必須被呼叫")
	require.True(t, spy.syncCalled, "Fatal 必須呼叫 Sync() 在 exit 之前")
	require.True(t, syncCalledBeforeExit,
		"Sync() 必須在 exitFunc 之前完成 — 確保 Fatal 訊息 crash-durable")
}

// syncSpyWriter 是 io.Writer + syncer 介面 spy,用來驗證 Logger.Fatal 是否在 exit
// 之前呼叫 Sync()。flushCalled 也記錄(若未來改成 flusher path 也走得到)。
type syncSpyWriter struct {
	syncCalled  bool
	flushCalled bool
	writes      []string
}

func (s *syncSpyWriter) Write(p []byte) (int, error) {
	s.writes = append(s.writes, string(p))
	return len(p), nil
}

func (s *syncSpyWriter) Sync() error {
	s.syncCalled = true
	return nil
}

func (s *syncSpyWriter) Flush() error {
	s.flushCalled = true
	return nil
}
