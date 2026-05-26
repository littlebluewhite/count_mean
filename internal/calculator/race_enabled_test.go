//go:build race

package calculator

import "time"

// cancellationBudget 是 cancellation timing test 允許的最長 elapsed。
//
// -race build 下 race detector 為每次 memory access 插 instrumentation,
// 對 goroutine 重的 hot loop 開銷 10-20 倍。1s budget(race_disabled_test.go)
// 在 -race + CI shared CPU 下實測會跑到 14s+(maxmean_cancellation_test.go
// TestCalculate_MidFlightCancel_LargeDataset)。30s 維持「不卡死」的契約檢查,
// 但放寬到能吃 race detector + CI runner 抖動的常見區間。
//
// 對稱:internal/cci/race_enabled_test.go 同形態,參考 gui/race_enabled.go
// 的 build-tag 常數 pattern。
const cancellationBudget = 30 * time.Second
