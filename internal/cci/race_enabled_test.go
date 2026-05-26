//go:build race

package cci

import "time"

// cancellationBudget 是 cancellation timing test 允許的最長 elapsed。
//
// -race build 下 race detector 開銷 10-20x,對 12 個 pair × 300k point 的並行
// CCI 計算尤其顯著。原 2s budget 在 CI -race 實測會跑到 11.77s
// (cancellation_test.go TestAnalyzeCCI_InFlightCancelReturnsQuickly)。
// 30s 維持「in-flight cancel 不卡死」的契約,放寬到吃 race detector + CI
// shared CPU 的抖動。
//
// 對稱:internal/calculator/race_enabled_test.go 同形態。
const cancellationBudget = 30 * time.Second
