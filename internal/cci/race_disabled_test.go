//go:build !race

package cci

import "time"

// cancellationBudget 在非 race build 下維持 2s 嚴格邊界,
// 捕捉 AnalyzeCCI 在 step 邊界 / hot-loop cciCancelCheckInterval 的 cancellation
// 路徑效能退化。-race build 用 30s,詳見 race_enabled_test.go。
const cancellationBudget = 2 * time.Second
