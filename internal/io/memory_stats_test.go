package io

import (
	"errors"
	"io"
	"testing"

	"count_mean/internal/logging"
)

// silentLogger 回傳一個丟棄所有輸出的 Logger，避免 unit test 噴 log。
func silentLogger(t *testing.T) *logging.Logger {
	t.Helper()
	return logging.NewLogger(logging.LevelError, io.Discard, false)
}

// TestEvaluateMemoryThresholds_HardLimit_ReturnsError 驗證 stats.IsOverLimit==true
// 時必須 return errMemoryOverLimit — streaming loop 看到 error 才會 fail-fast。
func TestEvaluateMemoryThresholds_HardLimit_ReturnsError(t *testing.T) {
	logger := silentLogger(t)
	stats := &MemoryStats{
		Alloc:       600 * 1024 * 1024, // 600 MB
		UsageRatio:  600.0 / 512.0,     // > 1.0
		IsOverLimit: true,
		HeapSys:     1024 * 1024,
		HeapInuse:   1024 * 1024,
	}

	err := evaluateMemoryThresholds(stats, 512*1024*1024, logger)
	if err == nil {
		t.Fatal("expect error when IsOverLimit=true, got nil")
	}
	if !errors.Is(err, errMemoryOverLimit) {
		t.Errorf("expect errors.Is(err, errMemoryOverLimit), got %v", err)
	}
}

// TestEvaluateMemoryThresholds_CriticalButUnderLimit_NoError 是 cross-compare review
// P1 finding B 的 regression test：使用率 95-100% 區間（>memoryCriticalThreshold,
// IsOverLimit==false）必須 return nil。先前 d45ee1f 引入的修法在此區間回傳
// errMemoryUsageHigh，被 executeStreamingLoop 當 fatal abort — 在記憶體還未超過
// 設定 limit 時就讓 streaming 失敗，與舊版 handleMemoryPressure 的 warn-only 行為不符。
func TestEvaluateMemoryThresholds_CriticalButUnderLimit_NoError(t *testing.T) {
	cases := []struct {
		name       string
		usageRatio float64
	}{
		{"just_above_warning", 0.91},
		{"between_warning_and_critical", 0.94},
		{"just_above_critical", 0.96},
		{"near_limit", 0.99},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			logger := silentLogger(t)
			stats := &MemoryStats{
				Alloc:       uint64(float64(512*1024*1024) * tc.usageRatio),
				UsageRatio:  tc.usageRatio,
				IsOverLimit: false, // 關鍵：尚未超過 limit
				HeapSys:     1024 * 1024,
				HeapInuse:   1024 * 1024,
			}

			err := evaluateMemoryThresholds(stats, 512*1024*1024, logger)
			if err != nil {
				t.Errorf("usageRatio=%.2f IsOverLimit=false should NOT return error (regression: pre-fix returned errMemoryUsageHigh causing premature abort); got %v",
					tc.usageRatio, err)
			}
		})
	}
}

// TestEvaluateMemoryThresholds_BelowWarning_NoError 驗證 normal 區間（<90%）安靜通過。
func TestEvaluateMemoryThresholds_BelowWarning_NoError(t *testing.T) {
	logger := silentLogger(t)
	stats := &MemoryStats{
		Alloc:       100 * 1024 * 1024,
		UsageRatio:  100.0 / 512.0,
		IsOverLimit: false,
		HeapSys:     1024 * 1024,
		HeapInuse:   1024 * 1024,
	}

	err := evaluateMemoryThresholds(stats, 512*1024*1024, logger)
	if err != nil {
		t.Errorf("normal usage should return nil, got %v", err)
	}
}

// TestEvaluateMemoryThresholds_LowHeapEfficiency_NoError 驗證 heap 碎片化警告
// 是 log-only，不會 propagate error。
func TestEvaluateMemoryThresholds_LowHeapEfficiency_NoError(t *testing.T) {
	logger := silentLogger(t)
	stats := &MemoryStats{
		Alloc:       100 * 1024 * 1024,
		UsageRatio:  100.0 / 512.0,
		IsOverLimit: false,
		HeapSys:     100 * 1024 * 1024, // 大
		HeapInuse:   30 * 1024 * 1024,  // 只用 30%，遠低於 60% threshold
	}

	err := evaluateMemoryThresholds(stats, 512*1024*1024, logger)
	if err != nil {
		t.Errorf("low heap efficiency should warn-only, not error; got %v", err)
	}
}
