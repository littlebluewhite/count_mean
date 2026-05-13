package calculator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"count_mean/internal/calculator"
	"count_mean/internal/models"
)

// buildLargeDataset constructs an EMG dataset sized to keep workers busy long
// enough that mid-flight cancellation is observable.
func buildLargeDataset(rows, channels int) *models.EMGDataset {
	headers := make([]string, channels+1)
	headers[0] = "Time"

	for i := 0; i < channels; i++ {
		headers[i+1] = "Ch"
	}

	data := make([]models.EMGData, rows)

	for i := 0; i < rows; i++ {
		chans := make([]float64, channels)
		for c := 0; c < channels; c++ {
			chans[c] = float64(i+c) * 0.1
		}
		data[i] = models.EMGData{Time: float64(i) * 0.001, Channels: chans}
	}

	return &models.EMGDataset{Headers: headers, Data: data}
}

// TestCalculate_PreCancelledContext_ReturnsCanceled verifies the new ctx
// cancellation contract: when the caller passes an already-cancelled context,
// Calculate must abort without running any per-channel work.
func TestCalculate_PreCancelledContext_ReturnsCanceled(t *testing.T) {
	calc := calculator.NewMaxMeanCalculator(10)
	dataset := buildLargeDataset(10000, 8)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel before Calculate runs.

	start := time.Now()
	_, err := calc.Calculate(ctx, dataset, 100)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled),
		"expected ctx.Canceled, got %v", err)
	assert.Less(t, elapsed, 200*time.Millisecond,
		"pre-cancelled Calculate should return quickly (got %v)", elapsed)
}

// TestCalculate_TimeoutContextPropagates exercises a deadline-bounded context.
// 直接用 0 timeout 等同於 pre-cancel，但走 DeadlineExceeded 路徑——驗證
// Calculate 對 context.DeadlineExceeded 與 context.Canceled 兩種取消形式
// 都會正確傳播。比 mid-flight cancel 更穩定，因為 deadline check 不依賴
// worker 與 cancel 之間的賽跑（過去的 timing-dependent 寫法在快機器上
// 8ms 跑完，cancel 來不及插入）。
func TestCalculate_TimeoutContextPropagates(t *testing.T) {
	calc := calculator.NewMaxMeanCalculator(10)
	dataset := buildLargeDataset(10000, 8)

	ctx, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()

	start := time.Now()
	_, err := calc.Calculate(ctx, dataset, 100)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.True(t,
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
		"expected DeadlineExceeded or Canceled, got %v", err)
	assert.Less(t, elapsed, 500*time.Millisecond,
		"Calculate with expired deadline should return quickly (got %v)", elapsed)
}
