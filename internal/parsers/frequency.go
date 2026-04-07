package parsers

import (
	"fmt"
	"math"
)

// computeFrequencyFromTime 從前兩個時間點的差值計算採樣頻率.
//
//nolint:err113 // dynamic errors with Chinese messages for user-facing output
func computeFrequencyFromTime(times []float64) (float64, error) {
	if len(times) < 2 {
		return 0, fmt.Errorf("需要至少兩個數據點來計算採樣頻率，目前只有 %d 個", len(times))
	}

	delta := times[1] - times[0]
	if delta <= 0 {
		return 0, fmt.Errorf("時間差值必須為正數，但得到 %.6f", delta)
	}

	return math.Round(1.0 / delta), nil
}
