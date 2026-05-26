//go:build !race

package calculator

import "time"

// cancellationBudget 在非 race build 下維持 1s 嚴格邊界,
// 捕捉 cancellation 路徑(maxmean hot loop ctx 檢查、worker pool drain)的效能退化。
// -race build 用 30s,詳見 race_enabled_test.go。
const cancellationBudget = 1 * time.Second
