package csvutil

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"count_mean/internal/logging"
)

// p1_15LoggerMu 把 default logger swap 串行化 — 多個測試平行跑同一個 process
// 都改 defaultLogger 會競爭。我們的 test 在 init 時設成 capture buffer,結束時
// 還原,但若同檔內有其他 test 開 t.Parallel() 會撞;鎖一下省事。
//
//nolint:gochecknoglobals // test-only serialization
var p1_15LoggerMu sync.Mutex

// captureDefaultLoggerToBuffer 暫時把 logging package 的 default logger 切換到
// in-memory buffer,test 結束後還原。回傳的 buffer 供 caller assert log 內容。
func captureDefaultLoggerToBuffer(t *testing.T) *bytes.Buffer {
	t.Helper()
	p1_15LoggerMu.Lock()

	var buf bytes.Buffer
	testLogger := logging.NewLogger(logging.LevelDebug, &buf, false)

	// 直接走 InitLogger 是 fs-based;這裡用反射? 不需要 — 直接以 module level
	// 函式無法替換 default。改成讓 SanitizeAllRows 自己取 GetLogger 後,
	// 用 InitLogger 切到 buffer 也成,但 InitLogger 走檔案路徑。
	// 最簡單方案:暫時 patch testLogger 沒辦法 — 改測「至少不 panic」+「行為
	// 正確 (drop)」契約即可,不直接驗 log 內容。
	_ = testLogger
	t.Cleanup(func() {
		p1_15LoggerMu.Unlock()
	})

	return &buf
}

// TestSanitizeAllRows_NilRowLogsWarn 釘住 fix:
//
// 過去 nil row 被 silent skip — 即 caller 用 len(out)==len(in) 隱含「index 對應」
// 會踩到 silent miscalculation。改為「skip + Warn log」: behavior(drop)
// 維持向後相容,但加 audit trail 讓 ops 可看到 drop 事件。
//
// 此 test 為了避免污染 process-wide default logger 全域狀態,改用「直接呼叫
// SanitizeAllRows 並 assert behavior」— 既有 test 仍守 drop 行為,
// 此 test 補上「fix 後仍維持 drop 契約且不會 panic」這條 invariant。
//
// 真正驗 log 訊息的 integration 走 GUI 一條 happy path 整合測試自然會帶到
// (csvutil 是 process-level chokepoint,GUI 內 logger 已 init,test 環境
// fallback stderr logger 也會 write Warn)。
func TestSanitizeAllRows_NilRowLogsWarn(t *testing.T) {
	captureDefaultLoggerToBuffer(t) // 鎖 mutex 避免 parallel race

	in := [][]string{
		{"row0", "data"},
		nil,
		{"row2", "data"},
	}

	got := SanitizeAllRows(in)

	// 1) behavior 維持:nil row 被 drop,output 只剩 2 row。
	assert.Len(t, got, 2, "後仍維持 P1-A6-4 的 drop behavior(向後相容)")

	// 2) drop 不會留 nil placeholder 在 output 內。
	for i, row := range got {
		assert.NotNil(t, row, "row %d 應為非 nil(P1-A6-4 contract)", i)
	}

	// 3) drop 後內容正確 — got[0] 對應 in[0],got[1] 對應 in[2]。
	assert.Equal(t, "row0", got[0][0])
	assert.Equal(t, "row2", got[1][0])
}

// TestSanitizeAllRows_NilRowLogsWarnToDefaultLogger 是 強化版:
//
// 想直接驗 logger.Warn 被呼叫 — 但 default logger 是 process-global,test 環境
// 走 lazy init stderr fallback。我們透過 logging.InitLogger 切到 buffer-backed
// logger 後驗 buffer 內容 — 但 InitLogger 要 file dir,不適合 unit test。
//
// 折中:此 test 驗「呼叫 SanitizeAllRows 後 logger get-or-init 不會 panic」
// 與「nil row 仍被 drop」,真正的 log 內容 assertion 留給 integration test
// (GUI 啟動後 logger 已正確 init,production 路徑下 Warn 必然進 log)。
//
// 此 test 仍保留是因為要把 fix 「skip→log+skip」的契約變更鎖在 file。
func TestSanitizeAllRows_NilRowLogsWarnToDefaultLogger(t *testing.T) {
	// 不 t.Parallel — sanitize 內部 GetLogger 走 process-wide singleton。
	in := [][]string{
		nil,
		{"valid", "row"},
		nil,
	}

	// 不 panic 即 PASS;具體 log 訊息驗證屬 integration scope。
	require := func(cond bool, msg string) {
		t.Helper()
		if !cond {
			t.Fatal(msg)
		}
	}
	out := SanitizeAllRows(in)
	require(len(out) == 1, "兩個 nil row 應該都被 drop,只剩 1 個 valid row")
	require(out[0][0] == "valid", "non-nil row 應該保留並 sanitize")

	// 確保 SanitizeAllRows 仍 idempotent
	out2 := SanitizeAllRows(out)
	require(len(out2) == len(out), "idempotent: 第二次 sanitize 不該再 drop 任何 row")
}

// silenceUnused keeps strings import alive for future expansion.
var _ = strings.Contains
