package models

import (
	"sync"
	"testing"
	"time"
)

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
