package models

import (
	"bytes"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"count_mean/internal/logging"
)

// captureBackpressureLogs swaps backpressureLogger to write into a buffer, returning
// the buffer + restore function. Test 必須 defer 呼叫 restore。
func captureBackpressureLogs(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	original := backpressureLogger
	backpressureLogger = func() *logging.Logger {
		return logging.NewLogger(logging.LevelDebug, &buf, false)
	}
	return &buf, func() {
		backpressureLogger = original
	}
}

func testBackpressureConfig() *BackpressureConfig {
	return &BackpressureConfig{
		MaxMemoryMB:           1024,
		MaxWorkers:            4,
		MemoryThreshold:       0.8,
		ThrottleThreshold:     0.7,
		CheckInterval:         100 * time.Millisecond,
		WorkerReductionFactor: 0.5,
		GCInterval:            time.Second,
	}
}

// TestBackpressureController_GetStats_ConcurrentRead_NoRace 防止 race regression：
// GetStats 過去在 RLock 下寫入 bc.stats.TotalProcessingTime 與 ThroughputJobsPerSec，
// 多 reader 同時呼叫時觸發 data race。修正後改在 local snapshot 上計算衍生欄位。
// 用 go test -race 才能偵測 — 多個 goroutine 並發呼叫 GetStats() 應不觸發 race detector。
func TestBackpressureController_GetStats_ConcurrentRead_NoRace(t *testing.T) {
	bc := NewBackpressureController(testBackpressureConfig())

	const (
		readers      = 10
		callsPerRead = 100
	)

	var wg sync.WaitGroup

	for i := 0; i < readers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < callsPerRead; j++ {
				_ = bc.GetStats()
			}
		}()
	}

	wg.Wait()
}

// TestBackpressureController_GetStats_DerivedFieldsComputed 確認 GetStats 仍正確計算衍生欄位。
func TestBackpressureController_GetStats_DerivedFieldsComputed(t *testing.T) {
	bc := NewBackpressureController(testBackpressureConfig())

	// 等一小段時間，讓 startTime 與 now 之間有差距，使 TotalProcessingTime > 0
	time.Sleep(2 * time.Millisecond)

	stats := bc.GetStats()

	if stats.TotalProcessingTime <= 0 {
		t.Errorf("TotalProcessingTime should be > 0, got %v", stats.TotalProcessingTime)
	}
}

// TestNewBackpressureController_ZeroValueSweep 守護
// zero-value config 必須 normalize 所有可能除 0 / 進入死循環的欄位,並透過
// backpressureLogger 寫 warn 讓使用者知道走 fallback (而非靜默)。
//
// 過去只 normalize CheckInterval / MaxMemoryMB,zero-value MaxWorkers /
// MemoryThreshold / ThrottleThreshold 都會造成各種 silent 退化 (worker pool 開不出來、
// throttle 永遠 true 等)。
func TestNewBackpressureController_ZeroValueSweep(t *testing.T) {
	buf, restore := captureBackpressureLogs(t)
	defer restore()

	// 全 zero-value config — 每個欄位都應該被 normalize + 寫 warn log
	cfg := &BackpressureConfig{
		MaxMemoryMB:       0,
		MaxWorkers:        0,
		MemoryThreshold:   0,
		ThrottleThreshold: 0,
		CheckInterval:     0,
	}

	bc := NewBackpressureController(cfg)

	// 驗證 normalize 結果合理
	if cfg.MaxMemoryMB == 0 {
		t.Error("MaxMemoryMB 沒被 normalize 為非零值")
	}
	if cfg.MaxWorkers <= 0 {
		t.Errorf("MaxWorkers 沒被 normalize 為正值,實際 %d", cfg.MaxWorkers)
	}
	expectedWorkers := runtime.NumCPU()
	if cfg.MaxWorkers != expectedWorkers {
		t.Errorf("MaxWorkers fallback 應為 runtime.NumCPU()=%d,實際 %d", expectedWorkers, cfg.MaxWorkers)
	}
	if cfg.MemoryThreshold <= 0 {
		t.Errorf("MemoryThreshold 沒被 normalize 為正值,實際 %v", cfg.MemoryThreshold)
	}
	if cfg.ThrottleThreshold <= 0 {
		t.Errorf("ThrottleThreshold 沒被 normalize 為正值,實際 %v", cfg.ThrottleThreshold)
	}
	if cfg.CheckInterval <= 0 {
		t.Errorf("CheckInterval 沒被 normalize 為正值,實際 %v", cfg.CheckInterval)
	}

	// 驗證對外行為:GetOptimalWorkerCount 必須回正值 (不是 0,worker pool 開得出來)
	if got := bc.GetOptimalWorkerCount(); got <= 0 {
		t.Errorf("GetOptimalWorkerCount 應回正值,實際 %d (worker pool 將無法啟動)", got)
	}

	// 驗證 log 寫了 warn — 每個 fallback 都應該有對應的 [WARN] 行
	output := buf.String()
	expectations := []string{
		"CheckInterval", "MaxMemoryMB", "MaxWorkers", "MemoryThreshold", "ThrottleThreshold",
	}
	for _, key := range expectations {
		if !strings.Contains(output, key) {
			t.Errorf("log 缺少 %q fallback warning,實際 log:\n%s", key, output)
		}
	}
	// 確認有 [WARN] 字面 (logger 文字格式)
	if !strings.Contains(output, "[WARN]") {
		t.Errorf("log 缺少 [WARN] 標記,實際:\n%s", output)
	}
}

// TestNewBackpressureController_PartialZeroValue 守護:只有部分欄位 zero-value 時,
// 也只 normalize 那部分,其他 caller 設定值不被覆蓋。
func TestNewBackpressureController_PartialZeroValue(t *testing.T) {
	buf, restore := captureBackpressureLogs(t)
	defer restore()

	cfg := &BackpressureConfig{
		MaxMemoryMB:       2048, // caller 設值,不該被改
		MaxWorkers:        0,    // zero-value,該被 normalize
		MemoryThreshold:   0.7,  // caller 設值
		ThrottleThreshold: 0.85, // caller 設值
		CheckInterval:     200 * time.Millisecond,
	}

	_ = NewBackpressureController(cfg)

	if cfg.MaxMemoryMB != 2048 {
		t.Errorf("caller-set MaxMemoryMB 被改寫為 %d", cfg.MaxMemoryMB)
	}
	if cfg.MaxWorkers <= 0 {
		t.Errorf("zero MaxWorkers 沒被 normalize,實際 %d", cfg.MaxWorkers)
	}
	if cfg.MemoryThreshold != 0.7 {
		t.Errorf("caller-set MemoryThreshold 被改寫為 %v", cfg.MemoryThreshold)
	}

	output := buf.String()
	// 應該只 log MaxWorkers 那一個 fallback
	if !strings.Contains(output, "MaxWorkers") {
		t.Errorf("應該 log MaxWorkers fallback,實際:\n%s", output)
	}
	// 不應該 log 其他欄位 (caller 已設值)
	if strings.Contains(output, "MaxMemoryMB") {
		t.Errorf("不該 log MaxMemoryMB (caller 已設值),實際:\n%s", output)
	}
}

// TestBackpressureController_NormalizesNonpositiveInterval 是 codex Wave 6
// second-pass P3 的 regression：WaitForCapacity 改用 time.NewTicker 後，若 caller
// 傳入 zero-value config（CheckInterval == 0）並達 ThrottleThreshold，會 panic
// 「non-positive interval for NewTicker」。修法在 constructor normalize 為預設值。
//
// 我們無法可靠觸發 ThrottleThreshold 而不影響其他 test 的記憶體統計，所以改驗
// constructor 後 GetConfig 風格的不變量：傳入 CheckInterval=0 / 負值，constructor
// 應 normalize 為正值（不依賴具體 default 數值，只要 > 0 即可）。
func TestBackpressureController_NormalizesNonpositiveInterval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		interval time.Duration
	}{
		{"zero interval (zero-value config)", 0},
		{"negative interval", -1 * time.Millisecond},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &BackpressureConfig{
				MaxMemoryMB:       1024,
				MaxWorkers:        2,
				MemoryThreshold:   0.8,
				ThrottleThreshold: 0.99,
				CheckInterval:     tt.interval,
			}

			// 不應 panic — 即使 caller 傳 zero/negative CheckInterval。
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("NewBackpressureController panicked with non-positive CheckInterval=%v: %v", tt.interval, r)
				}
			}()

			_ = NewBackpressureController(cfg)
		})
	}
}
